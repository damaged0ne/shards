package collector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"

	"github.com/coroot/coroot/db"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
)

// Shards fork: WriteMetrics stores series produced by the server itself (e.g. synthetic probe results)
// in the project's metrics storage, exactly like the remote-write requests of the agents handled by Metrics:
// into ClickHouse when the project uses it for metrics, otherwise to the Prometheus remote-write endpoint.
// The written series are then queryable like any agent metric.
func (c *Collector) WriteMetrics(ctx context.Context, projectId db.ProjectId, req *prompb.WriteRequest) error {
	project, err := c.db.GetProject(projectId)
	if err != nil {
		return err
	}
	if project.Multicluster() {
		return fmt.Errorf("project %s is a multi-cluster project and has no metrics storage", projectId)
	}
	cfg := project.PrometheusConfig(c.globalPrometheus)
	if len(cfg.ExtraLabels) > 0 {
		for i := range req.Timeseries {
			for k, v := range cfg.ExtraLabels {
				req.Timeseries[i].Labels = append(req.Timeseries[i].Labels, prompb.Label{Name: k, Value: v})
			}
			ls := req.Timeseries[i].Labels
			sort.Slice(ls, func(a, b int) bool { return ls[a].Name < ls[b].Name })
		}
	}
	if cfg.UseClickHouse {
		c.getMetricsBatch(project).Add(req)
		return nil
	}

	var u *url.URL
	if cfg.RemoteWriteUrl != "" {
		u, err = url.Parse(cfg.RemoteWriteUrl)
	} else if cfg.Url != "" {
		u, err = url.Parse(cfg.Url)
		if err == nil {
			u = u.JoinPath("/api/v1/write")
		}
	} else {
		return fmt.Errorf("no metrics storage is configured for project %s", projectId)
	}
	if err != nil {
		return err
	}
	if cfg.BasicAuth != nil {
		u.User = url.UserPassword(cfg.BasicAuth.User, cfg.BasicAuth.Password)
	}

	data, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(snappy.Encode(nil, data)))
	if err != nil {
		return err
	}
	for _, h := range cfg.CustomHeaders {
		r.Header.Add(h.Key, h.Value)
	}
	r.Header.Set("Content-Type", "application/x-protobuf")
	r.Header.Set("Content-Encoding", "snappy")
	r.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	httpClient := secureClient
	if cfg.TlsSkipVerify {
		httpClient = insecureClient
	}
	res, err := httpClient.Do(r)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("remote write failed: %s: %s", res.Status, bytes.TrimSpace(body))
	}
	return nil
}

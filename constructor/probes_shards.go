package constructor

import (
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"k8s.io/klog"
)

// Shards fork: synthetic probes. The probe definitions come from the DB, the results from the
// shards_probe_* metrics the server writes into the project's metrics storage (see the probes package).

const (
	qShardsProbeUp                  = "shards_probe_up"
	qShardsProbeConsecutiveFailures = "shards_probe_consecutive_failures"
	qShardsProbeHTTPStatusCode      = "shards_probe_http_status_code"
	qShardsProbeDuration            = "shards_probe_duration_seconds"
	qShardsProbeCertExpiresIn       = "shards_probe_tls_cert_expires_in"
	qShardsProbeCertValid           = "shards_probe_tls_cert_valid"
	qShardsProbeCertInfo            = "shards_probe_tls_cert_info"
)

var shardsProbeQueries = []Query{
	Q(qShardsProbeUp, `shards_probe_up`, "probe_id"),
	Q(qShardsProbeConsecutiveFailures, `shards_probe_consecutive_failures`, "probe_id"),
	Q(qShardsProbeHTTPStatusCode, `shards_probe_http_status_code`, "probe_id"),
	Q(qShardsProbeDuration, `shards_probe_duration_seconds`, "probe_id", "phase"),
	// the expiry is a unix time that doesn't fit into float32, the remaining time is loaded instead
	Q(qShardsProbeCertExpiresIn, `shards_probe_tls_cert_expiry_seconds - time()`, "probe_id"),
	Q(qShardsProbeCertValid, `shards_probe_tls_cert_valid`, "probe_id"),
	Q(qShardsProbeCertInfo, `shards_probe_tls_cert_info`, "probe_id", "issuer", "subject", "not_after"),
}

func init() {
	QUERIES = append(QUERIES, shardsProbeQueries...)
}

type probesDB interface {
	GetProbes(projectId db.ProjectId) ([]*db.Probe, error)
}

func (c *Constructor) loadProbes(w *model.World, metrics map[string][]*model.MetricValues, project *db.Project) {
	pdb, ok := c.db.(probesDB)
	if !ok {
		return
	}
	defs, err := pdb.GetProbes(project.Id)
	if err != nil {
		klog.Errorln("failed to load probes:", err)
		return
	}
	if len(defs) == 0 {
		return
	}
	byId := map[string]*model.Probe{}
	for _, d := range defs {
		p := model.NewProbe(d.Id)
		p.Name = d.Name
		p.Type = string(d.Spec.Type)
		p.Target = d.Spec.Target
		p.Interval = d.Spec.Interval
		p.TlsSkipVerify = d.Spec.TlsSkipVerify
		p.Paused = d.Spec.Paused
		if d.State.LastRunAt > 0 && !d.State.LastRunAt.Before(w.Ctx.To.Add(-2*d.Spec.Interval-timeseries.Minute)) {
			p.LastRunAt = d.State.LastRunAt
			p.LastError = d.State.Error
			up := d.State.Up
			p.LastUp = &up
		}
		if d.Spec.ApplicationId != "" {
			if id, err := model.NewApplicationIdFromString(d.Spec.ApplicationId, project.ClusterId()); err == nil && w.GetApplication(id) != nil {
				p.ApplicationId = id
				p.Linked = true
			}
		}
		if !p.Linked {
			p.ApplicationId = model.NewApplicationId(project.ClusterId(), model.ProbeNamespace, model.ApplicationKindProbe, d.Name)
			app := w.GetOrCreateApplication(p.ApplicationId, false)
			app.Category = project.CalcApplicationCategory(app.Id)
		}
		byId[d.Id] = p
		w.Probes = append(w.Probes, p)
	}

	get := func(mv *model.MetricValues) *model.Probe {
		return byId[mv.Labels["probe_id"]]
	}
	for _, mv := range metrics[qShardsProbeUp] {
		if p := get(mv); p != nil {
			p.Up = merge(p.Up, mv.Values, timeseries.Any)
		}
	}
	for _, mv := range metrics[qShardsProbeConsecutiveFailures] {
		if p := get(mv); p != nil {
			p.ConsecutiveFailures = merge(p.ConsecutiveFailures, mv.Values, timeseries.Any)
		}
	}
	for _, mv := range metrics[qShardsProbeHTTPStatusCode] {
		if p := get(mv); p != nil {
			p.StatusCode = merge(p.StatusCode, mv.Values, timeseries.Any)
		}
	}
	for _, mv := range metrics[qShardsProbeDuration] {
		if p := get(mv); p != nil {
			phase := mv.Labels["phase"]
			p.Durations[phase] = merge(p.Durations[phase], mv.Values, timeseries.Any)
		}
	}
	for _, mv := range metrics[qShardsProbeCertExpiresIn] {
		if p := get(mv); p != nil {
			p.CertExpiresIn = merge(p.CertExpiresIn, mv.Values, timeseries.Any)
		}
	}
	for _, mv := range metrics[qShardsProbeCertValid] {
		if p := get(mv); p != nil {
			p.CertValid = merge(p.CertValid, mv.Values, timeseries.Any)
		}
	}
	certInfoAt := map[string]timeseries.Time{}
	for _, mv := range metrics[qShardsProbeCertInfo] {
		p := get(mv)
		if p == nil {
			continue
		}
		t, v := mv.Values.LastNotNull()
		if v == 0 || t < certInfoAt[p.Id] {
			continue
		}
		certInfoAt[p.Id] = t
		p.CertIssuer = mv.Labels["issuer"]
		p.CertSubject = mv.Labels["subject"]
		p.CertNotAfter = mv.Labels["not_after"]
	}
}

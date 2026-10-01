package probes

import (
	"sort"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/prometheus/prometheus/prompb"
)

const (
	MetricUp                  = "shards_probe_up"
	MetricDuration            = "shards_probe_duration_seconds"
	MetricHTTPStatusCode      = "shards_probe_http_status_code"
	MetricTLSCertExpiry       = "shards_probe_tls_cert_expiry_seconds"
	MetricTLSCertInfo         = "shards_probe_tls_cert_info"
	MetricTLSCertValid        = "shards_probe_tls_cert_valid"
	MetricConsecutiveFailures = "shards_probe_consecutive_failures"

	LabelProbeId   = "probe_id"
	LabelProbeName = "probe_name"
	LabelProbeType = "probe_type"
	LabelTarget    = "target"
	LabelPhase     = "phase"
)

var Phases = []string{PhaseDNS, PhaseConnect, PhaseTLS, PhaseTTFB, PhaseTotal}

// WriteRequest converts a probe result into Prometheus series:
//
//	shards_probe_up                                     1 if the probe succeeded
//	shards_probe_duration_seconds{phase}                dns, connect, tls, ttfb and total
//	shards_probe_http_status_code                       HTTP probes only
//	shards_probe_consecutive_failures                   the number of failed runs in a row
//	shards_probe_tls_cert_expiry_seconds                unix time the certificate (chain) expires at
//	shards_probe_tls_cert_info{issuer,subject,not_after} 1
//	shards_probe_tls_cert_valid                         1/0, only if the verification is enabled
func WriteRequest(p *db.Probe, r *Result, consecutiveFailures int) *prompb.WriteRequest {
	ts := r.Time.UnixMilli()
	base := []prompb.Label{
		{Name: LabelProbeId, Value: p.Id},
		{Name: LabelProbeName, Value: p.Name},
		{Name: LabelProbeType, Value: string(p.Spec.Type)},
		{Name: LabelTarget, Value: p.Spec.Target},
	}
	req := &prompb.WriteRequest{}
	add := func(name string, value float64, extra ...prompb.Label) {
		ls := make([]prompb.Label, 0, len(base)+len(extra)+1)
		ls = append(ls, prompb.Label{Name: "__name__", Value: name})
		ls = append(ls, base...)
		ls = append(ls, extra...)
		sort.Slice(ls, func(i, j int) bool { return ls[i].Name < ls[j].Name })
		req.Timeseries = append(req.Timeseries, prompb.TimeSeries{Labels: ls, Samples: []prompb.Sample{{Value: value, Timestamp: ts}}})
	}
	up := 0.
	if r.Up {
		up = 1
	}
	add(MetricUp, up)
	for _, phase := range Phases {
		if v, ok := r.Phases[phase]; ok {
			add(MetricDuration, v, prompb.Label{Name: LabelPhase, Value: phase})
		}
	}
	if p.Spec.Type == db.ProbeTypeHTTP && r.StatusCode > 0 {
		add(MetricHTTPStatusCode, float64(r.StatusCode))
	}
	add(MetricConsecutiveFailures, float64(consecutiveFailures))
	if c := r.Cert; c != nil {
		add(MetricTLSCertExpiry, float64(c.NotAfter.Unix()))
		add(MetricTLSCertInfo, 1,
			prompb.Label{Name: "issuer", Value: c.Issuer},
			prompb.Label{Name: "subject", Value: c.Subject},
			prompb.Label{Name: "not_after", Value: c.NotAfter.UTC().Format(time.RFC3339)},
		)
		if c.Verified {
			valid := 0.
			if c.Valid {
				valid = 1
			}
			add(MetricTLSCertValid, valid)
		}
	}
	return req
}

package model

import (
	"sort"

	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: synthetic probes (HTTP/TCP/TLS/DNS checks run by the server).

const (
	AuditReportUptime AuditReportName = "Uptime"

	// ApplicationKindProbe is the kind of the applications created for probes that are not linked
	// to an existing application: they get the Uptime report and the probe alerts.
	ApplicationKindProbe ApplicationKind = "Probe"
	ProbeNamespace                       = "probes"
)

type Probe struct {
	Id            string
	Name          string
	Type          string
	Target        string
	Interval      timeseries.Duration
	TlsSkipVerify bool
	Paused        bool

	// ApplicationId is the linked application or the synthetic application of the probe.
	ApplicationId ApplicationId
	Linked        bool

	Up                  *timeseries.TimeSeries
	ConsecutiveFailures *timeseries.TimeSeries
	StatusCode          *timeseries.TimeSeries
	CertExpiresIn       *timeseries.TimeSeries // seconds
	CertValid           *timeseries.TimeSeries
	Durations           map[string]*timeseries.TimeSeries // by phase

	CertIssuer   string
	CertSubject  string
	CertNotAfter string

	// the latest run (from the DB, it may be newer than the metrics)
	LastRunAt timeseries.Time
	LastError string
	LastUp    *bool
}

func NewProbe(id string) *Probe {
	return &Probe{Id: id, Durations: map[string]*timeseries.TimeSeries{}}
}

// UptimePercent is the share of successful runs within the window.
func (p *Probe) UptimePercent() float32 {
	if p.Up.IsEmpty() {
		return timeseries.NaN
	}
	var sum, count float32
	iter := p.Up.Iter()
	for iter.Next() {
		_, v := iter.Value()
		if timeseries.IsNaN(v) {
			continue
		}
		sum += v
		count++
	}
	if count == 0 {
		return timeseries.NaN
	}
	return sum / count * 100
}

// LatencyQuantile returns the q-quantile (0..1) of the total duration within the window, in seconds.
func (p *Probe) LatencyQuantile(q float32) float32 {
	ts := p.Durations["total"]
	if ts.IsEmpty() {
		return timeseries.NaN
	}
	var vs []float32
	iter := ts.Iter()
	for iter.Next() {
		_, v := iter.Value()
		if !timeseries.IsNaN(v) {
			vs = append(vs, v)
		}
	}
	if len(vs) == 0 {
		return timeseries.NaN
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i] < vs[j] })
	idx := int(q*float32(len(vs)-1) + 0.5)
	return vs[idx]
}

// IsUp reports the latest known status: nil if unknown.
func (p *Probe) IsUp() *bool {
	if p.LastUp != nil {
		return p.LastUp
	}
	if v := p.Up.Last(); !timeseries.IsNaN(v) {
		up := v > 0
		return &up
	}
	return nil
}

func (w *World) ProbesOf(appId ApplicationId) []*Probe {
	var res []*Probe
	for _, p := range w.Probes {
		if p.ApplicationId == appId {
			res = append(res, p)
		}
	}
	return res
}

// ThresholdDuration renders a threshold in seconds (used by the probe check messages).
func (c CheckContext) ThresholdDuration() string {
	return utils.FormatLatency(c.threshold)
}

// ThresholdValue renders a plain-number threshold.
func (c CheckContext) ThresholdValue() string {
	return utils.FormatFloat(c.threshold)
}

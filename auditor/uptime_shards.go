package auditor

import (
	"fmt"
	"sort"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

const secondsPerDay = 24 * 3600

// uptime builds the Uptime report from the synthetic probes linked to the app (shards fork).
// Applications created for unlinked probes (kind Probe) only get this report.
func (a *appAuditor) uptime() {
	probes := a.w.ProbesOf(a.app.Id)
	if a.app.Id.Kind == model.ApplicationKindProbe {
		a.reports = nil
	}
	if len(probes) == 0 {
		return
	}
	sort.Slice(probes, func(i, j int) bool { return probes[i].Name < probes[j].Name })

	report := a.addReport(model.AuditReportUptime)
	downCheck := report.CreateCheck(model.Checks.ProbeDown)
	latencyCheck := report.CreateCheck(model.Checks.ProbeLatency)
	certCheck := report.CreateCheck(model.Checks.ProbeTLSCertExpiry)
	certCriticalCheck := report.CreateCheck(model.Checks.ProbeTLSCertExpiryCritical)
	certInvalidCheck := report.CreateCheck(model.Checks.ProbeTLSCertInvalid)

	upChart := report.GetOrCreateChart("Availability, %", nil)
	latencyChart := report.GetOrCreateChart("Response time, seconds", nil)
	phasesChart := report.GetOrCreateChartGroup("Response time by phase <selector>, seconds", nil)
	downCheck.AddWidget(upChart.Widget())
	latencyCheck.AddWidget(latencyChart.Widget())
	if latencyChart != nil {
		latencyChart.SetThreshold("threshold", timeseries.New(a.w.Ctx.From, a.w.Ctx.PointsCount(), a.w.Ctx.Step).WithNewValue(latencyCheck.Threshold))
	}

	table := report.GetOrCreateTable("Probe", "Status", "Uptime", "p95 latency", "Certificate", "Last error")
	for _, p := range probes {
		if p.Paused {
			table.AddRow(
				model.NewTableCell(p.Name).AddTag("%s", p.Type).SetUnit(p.Target),
				model.NewTableCell().SetStatus(model.UNKNOWN, "paused"),
				model.NewTableCell(), model.NewTableCell(), model.NewTableCell(), model.NewTableCell(),
			)
			continue
		}
		status := model.NewTableCell().SetStatus(model.UNKNOWN, "no data")
		if up := p.IsUp(); up != nil {
			if *up {
				status.SetStatus(model.OK, "up")
			} else {
				status.SetStatus(model.CRITICAL, "down")
			}
		}

		if failures := lastValue(p.ConsecutiveFailures); !timeseries.IsNaN(failures) && failures >= downCheck.Threshold && downCheck.Threshold > 0 {
			downCheck.AddItem("%s", p.Name)
			msg := fmt.Sprintf("%s: %.0f failed runs in a row", p.Name, failures)
			if p.LastError != "" {
				msg += ": " + p.LastError
			}
			downCheck.AddDetail("%s", msg)
		}

		total := p.Durations["total"]
		if last := lastValue(total); !timeseries.IsNaN(last) && last > latencyCheck.Threshold {
			latencyCheck.AddItem("%s", p.Name)
			latencyCheck.AddDetail("%s: %s", p.Name, utils.FormatLatency(last))
		}

		cert := model.NewTableCell()
		if left := lastValue(p.CertExpiresIn); !timeseries.IsNaN(left) {
			days := left / secondsPerDay
			cert.SetValue(formatDaysLeft(days))
			if p.CertSubject != "" {
				cert.SetUnit(p.CertSubject)
			}
			switch {
			case days < certCriticalCheck.Threshold:
				cert.SetStatus(model.CRITICAL, formatDaysLeft(days))
				certCriticalCheck.AddItem("%s", p.Name)
				certCriticalCheck.AddDetail("%s: %s (%s)", p.Name, formatDaysLeft(days), p.CertNotAfter)
			case days < certCheck.Threshold:
				cert.SetStatus(model.WARNING, formatDaysLeft(days))
			}
			if days < certCheck.Threshold {
				certCheck.AddItem("%s", p.Name)
				certCheck.AddDetail("%s: %s (%s)", p.Name, formatDaysLeft(days), p.CertNotAfter)
			}
		}
		if valid := lastValue(p.CertValid); valid == 0 && !p.TlsSkipVerify {
			certInvalidCheck.AddItem("%s", p.Name)
			cert.SetStatus(model.CRITICAL, "invalid")
			if p.LastError != "" {
				certInvalidCheck.AddDetail("%s: %s", p.Name, p.LastError)
			}
		}

		uptime := model.NewTableCell()
		if u := p.UptimePercent(); !timeseries.IsNaN(u) {
			uptime.SetValue(utils.FormatPercentage(u))
			uptime.SetChart(p.Up)
		}
		latency := model.NewTableCell()
		if q := p.LatencyQuantile(0.95); !timeseries.IsNaN(q) {
			latency.SetValue(utils.FormatLatency(q))
		}
		lastErr := model.NewTableCell(p.LastError).SetMaxWidth(320)

		table.AddRow(
			model.NewTableCell(p.Name).AddTag("%s", p.Type).SetUnit(p.Target),
			status, uptime, latency, cert, lastErr,
		)

		if upChart != nil {
			upChart.AddSeries(p.Name, p.Up.Map(func(t timeseries.Time, v float32) float32 { return v * 100 }))
		}
		if latencyChart != nil {
			latencyChart.AddSeries(p.Name, total)
		}
		if phasesChart != nil {
			ch := phasesChart.GetOrCreateChart(p.Name).Stacked()
			for _, phase := range []string{"dns", "connect", "tls", "ttfb"} {
				ch.AddSeries(phase, p.Durations[phase])
			}
		}
	}

	checks := []*model.Check{downCheck, latencyCheck, certCheck, certCriticalCheck, certInvalidCheck}
	for _, ch := range checks {
		ch.Calc()
		if ch.Status > model.OK {
			status := model.WARNING
			if ch == downCheck || ch == certInvalidCheck || ch == certCriticalCheck {
				status = model.CRITICAL
			}
			if a.app.Status < status {
				a.app.Status = status
			}
		}
	}
}

func lastValue(ts *timeseries.TimeSeries) float32 {
	if ts.IsEmpty() {
		return timeseries.NaN
	}
	res := timeseries.NaN
	iter := ts.Iter()
	for iter.Next() {
		if _, v := iter.Value(); !timeseries.IsNaN(v) {
			res = v
		}
	}
	return res
}

func formatDaysLeft(days float32) string {
	switch {
	case days < 0:
		return "expired"
	case days < 1:
		return fmt.Sprintf("%.0fh left", days*24)
	}
	return fmt.Sprintf("%.0fd left", days)
}

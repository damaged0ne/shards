package auditor

import (
	"fmt"
	"sort"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/dustin/go-humanize"
	"golang.org/x/exp/maps"
)

// Shards fork: node report widgets for the metrics of the shards node agent.

const (
	nodeFsSpaceWarningPercent  = 90
	nodeFsInodeWarningPercent  = 90
	nodeAgentQueueWarningLen   = 10000
	nodeReportGroupFilesystems = "Filesystems"
	nodeReportGroupFirewall    = "Firewall"
	nodeReportGroupAgent       = "Agent"
)

func auditNodeShards(w *model.World, report *model.AuditReport, node *model.Node) {
	s := node.Shards
	if s == nil {
		return
	}
	step := w.Ctx.Step

	// load average and PSI next to the CPU/memory charts
	if !s.Load1.IsEmpty() || !s.Load5.IsEmpty() || !s.Load15.IsEmpty() {
		report.GetOrCreateChart("Load average", nil).
			Group("CPU", 1).
			AddSeries("load1", s.Load1).
			AddSeries("load5", s.Load5).
			AddSeries("load15", s.Load15).
			SetThreshold("cores", node.CpuCapacity)
	}
	if len(s.Pressure) > 0 {
		pct := func(ts *timeseries.TimeSeries) *timeseries.TimeSeries {
			return ts.Map(func(t timeseries.Time, v float32) float32 { return v * 100 })
		}
		psi := report.GetOrCreateChartGroup("Resource pressure (PSI) <selector>, % of time stalled", nil).Group("CPU", 1)
		for _, r := range []string{"cpu", "memory", "io"} {
			some, full := s.Pressure[r+"/some"], s.Pressure[r+"/full"]
			if some.IsEmpty() && full.IsEmpty() {
				continue
			}
			psi.GetOrCreateChart(r).
				AddSeries("some", pct(some), "amber").
				AddSeries("full", pct(full), "red")
		}
		if psi != nil && len(psi.Charts) > 0 {
			psi.Charts[0].Feature()
		}
	}

	if len(s.Filesystems) > 0 {
		table := report.GetOrCreateTable("Mount point", "Device", "FS", "Used", "Available", "Inodes used", "Mode").
			Group(nodeReportGroupFilesystems, 6)
		usage := report.GetOrCreateChartGroup("Filesystem usage <selector>, %", nil).Group(nodeReportGroupFilesystems, 6)
		for _, fs := range s.FilesystemsSorted() {
			used, inodes := fs.UsedPercent(), fs.InodesUsedPercent()
			usedCell := model.NewTableCell()
			if u := used.Last(); !timeseries.IsNaN(u) {
				st := model.OK
				if u > nodeFsSpaceWarningPercent {
					st = model.WARNING
					report.Status = max(report.Status, model.WARNING)
				}
				usedCell.SetStatus(st, fmt.Sprintf("%.0f%%", u)).SetProgress(int(u), progressColor(st))
				if size, avail := fs.SizeBytes.Last(), fs.AvailBytes.Last(); size > 0 && !timeseries.IsNaN(avail) {
					usedCell.AddTag("%s / %s", humanize.Bytes(uint64(size-avail)), humanize.Bytes(uint64(size)))
				}
			}
			availCell := model.NewTableCell()
			if avail := fs.AvailBytes.Last(); avail > 0 {
				v, unit := utils.FormatBytes(avail)
				availCell.SetValue(v).SetUnit(unit)
			}
			inodesCell := model.NewTableCell()
			if iu := inodes.Last(); !timeseries.IsNaN(iu) {
				st := model.OK
				if iu > nodeFsInodeWarningPercent {
					st = model.WARNING
					report.Status = max(report.Status, model.WARNING)
				}
				inodesCell.SetStatus(st, fmt.Sprintf("%.0f%%", iu))
			}
			mode := model.NewTableCell("rw")
			switch {
			case fs.BecameReadonly():
				mode.SetStatus(model.CRITICAL, "read-only (remounted)")
				report.Status = max(report.Status, model.WARNING)
			case fs.IsReadonly():
				mode.SetValue("ro")
			}
			table.AddRow(
				model.NewTableCell(fs.MountPoint),
				model.NewTableCell(fs.Device),
				model.NewTableCell(fs.FsType),
				usedCell,
				availCell,
				inodesCell,
				mode,
			)
			usage.GetOrCreateChart("overview").Feature().AddSeries(fs.MountPoint, used)
			usage.GetOrCreateChart(fs.MountPoint).
				AddSeries("space", used, "blue").
				AddSeries("inodes", inodes, "amber")
		}
	}

	if !s.F2bUp.IsEmpty() || len(s.F2bJails) > 0 {
		table := report.GetOrCreateTable("fail2ban jail", "Banned now", "Bans (1h)").Group(nodeReportGroupFirewall, 7)
		if up := s.F2bUp.Last(); up == 0 {
			table.AddRow(
				model.NewTableCell().SetStatus(model.WARNING, "fail2ban database can't be read"),
				model.NewTableCell(), model.NewTableCell(),
			)
		}
		var banned *model.Chart
		if len(s.F2bJails) > 0 {
			banned = report.GetOrCreateChart("fail2ban: banned IPs", nil).Group(nodeReportGroupFirewall, 7)
		}
		names := maps.Keys(s.F2bJails)
		sort.Strings(names)
		for _, name := range names {
			j := s.F2bJails[name]
			table.AddRow(model.NewTableCell(name), countCell(j.Banned.Last()), countCell(j.Bans1h.Last()))
			banned.AddSeries(name, j.Banned)
		}
	}

	if len(s.NftCounters) > 0 || len(s.NftRules) > 0 {
		table := report.GetOrCreateTable("nftables counter / rule", "Kind", "Traffic", "Packets").Group(nodeReportGroupFirewall, 7)
		bytesChart := report.GetOrCreateChart("nftables traffic, bits/second", nil).Group(nodeReportGroupFirewall, 7).Sorted()
		packetsChart := report.GetOrCreateChart("nftables packets, packets/second", nil).Group(nodeReportGroupFirewall, 7).Sorted()
		add := func(kind string, stats map[model.NftKey]*model.NftStat) {
			keys := maps.Keys(stats)
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			for _, k := range keys {
				st := stats[k]
				bits := st.Bytes.Map(func(t timeseries.Time, v float32) float32 { return v * 8 })
				traffic := model.NewTableCell()
				if v := bits.Last(); !timeseries.IsNaN(v) {
					traffic.SetValue(utils.FormatFloat(v)).SetUnit("bit/s")
				}
				packets := model.NewTableCell()
				if v := st.Packets.Last(); !timeseries.IsNaN(v) {
					packets.SetValue(utils.FormatFloat(v)).SetUnit("pps")
				}
				table.AddRow(model.NewTableCell(k.String()), model.NewTableCell(kind), traffic, packets)
				bytesChart.AddSeries(k.String(), bits)
				packetsChart.AddSeries(k.String(), st.Packets)
			}
		}
		add("counter", s.NftCounters)
		add("rule", s.NftRules)
	}

	if !s.Agent.IsEmpty() {
		table := report.GetOrCreateTable("Agent", "Version", "Remote write failures", "Lost eBPF samples", "eBPF decode errors", "L7 parse errors", "Events queue", "Recovered panics").
			Group(nodeReportGroupAgent, 8)
		total := func(ts *timeseries.TimeSeries, warn bool) *model.TableCell {
			c := model.NewTableCell()
			if ts.IsEmpty() {
				return c
			}
			v := ts.Reduce(timeseries.NanSum) * float32(step)
			if timeseries.IsNaN(v) {
				return c
			}
			c.SetEventsCount(uint64(v + 0.5))
			if c.Value == "" {
				c.SetValue("0")
			}
			if warn && v >= 1 {
				c.UpdateStatus(model.WARNING)
			}
			return c
		}
		queue := model.NewTableCell()
		if q := s.Agent.EventsQueueLength.Reduce(timeseries.Max); !timeseries.IsNaN(q) {
			queue.SetValue(fmt.Sprintf("%.0f", s.Agent.EventsQueueLength.Last())).AddTag("max: %.0f", q)
			if q > nodeAgentQueueWarningLen {
				queue.UpdateStatus(model.WARNING)
			}
		}
		version := node.AgentVersion.Value()
		if version == "" {
			version = "unknown"
		}
		table.AddRow(
			model.NewTableCell(node.GetName()),
			model.NewTableCell(version),
			total(s.Agent.RemoteWriteFailures, true),
			total(s.Agent.EbpfLostSamples, true),
			total(s.Agent.EbpfDecodeErrors, false),
			total(s.Agent.L7ParseErrors, false),
			queue,
			total(s.Agent.RecoveredPanics, true),
		)
		if s.Agent.EbpfLostSamples.IsEmpty() && s.Agent.EbpfDecodeErrors.IsEmpty() && s.Agent.L7ParseErrors.IsEmpty() &&
			s.Agent.RemoteWriteFailures.IsEmpty() && s.Agent.RecoveredPanics.IsEmpty() {
			return
		}
		report.GetOrCreateChart("Agent data loss, events/second", nil).
			Group(nodeReportGroupAgent, 8).
			AddSeries("lost eBPF samples", s.Agent.EbpfLostSamples, "red").
			AddSeries("eBPF decode errors", s.Agent.EbpfDecodeErrors, "amber").
			AddSeries("L7 parse errors", s.Agent.L7ParseErrors, "blue").
			AddSeries("remote write failures", s.Agent.RemoteWriteFailures, "black").
			AddSeries("recovered panics", s.Agent.RecoveredPanics, "purple")
		if !s.Agent.EventsQueueLength.IsEmpty() {
			report.GetOrCreateChart("Agent events queue length", nil).
				Group(nodeReportGroupAgent, 8).
				AddSeries("queue", s.Agent.EventsQueueLength)
		}
	}
}

func countCell(v float32) *model.TableCell {
	c := model.NewTableCell()
	if !timeseries.IsNaN(v) {
		c.SetValue(fmt.Sprintf("%.0f", v))
	}
	return c
}

func progressColor(s model.Status) string {
	if s >= model.WARNING {
		return "red"
	}
	return "blue"
}

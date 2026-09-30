package auditor

import (
	"fmt"
	"sort"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/dustin/go-humanize"
	"golang.org/x/exp/maps"
)

// nodeFilesystems checks the host filesystems (shards_fs_*) of the nodes the app runs on.
// It returns false if none of the nodes reports them.
func (a *appAuditor) nodeFilesystems(report *model.AuditReport) bool {
	nodes := map[string]*model.Node{}
	for _, i := range a.app.Instances {
		if i.Node == nil || i.Node.Shards == nil || len(i.Node.Shards.Filesystems) == 0 {
			continue
		}
		nodes[i.NodeName()] = i.Node
	}
	if len(nodes) == 0 {
		return false
	}

	spaceCheck := report.CreateCheck(model.Checks.NodeDiskSpace)
	roCheck := report.CreateCheck(model.Checks.NodeFilesystemReadonly)

	usageChart := report.GetOrCreateChartGroup("Node filesystem usage <selector>, %", nil)
	spaceCheck.AddWidget(usageChart.Widget())

	names := maps.Keys(nodes)
	sort.Strings(names)
	for _, name := range names {
		node := nodes[name]
		for _, fs := range node.Shards.FilesystemsSorted() {
			item := name + ":" + fs.MountPoint
			used, inodes := fs.UsedPercent(), fs.InodesUsedPercent()
			u, iu := used.Last(), inodes.Last()
			worst := u
			if timeseries.IsNaN(worst) || iu > worst {
				worst = iu
			}
			if !timeseries.IsNaN(worst) {
				if worst > spaceCheck.Value() {
					spaceCheck.SetValue(worst)
				}
				if worst > spaceCheck.Threshold {
					spaceCheck.AddItem("%s", item)
					spaceCheck.AddDetail("%s: %.0f%% of space, %.0f%% of inodes used", item, u, iu)
				}
			}
			if fs.BecameReadonly() {
				roCheck.AddItem("%s", item)
				roCheck.AddDetail("%s (%s) is read-only", item, fs.Device)
			}
			if usageChart != nil {
				usageChart.GetOrCreateChart(item).
					AddSeries("space", used, "blue").
					AddSeries("inodes", inodes, "amber")
			}
			space := model.NewTableCell()
			if size, avail := fs.SizeBytes.Last(), fs.AvailBytes.Last(); size > 0 && !timeseries.IsNaN(avail) {
				space.SetValue(fmt.Sprintf("%.0f%% (%s free of %s)", u, humanize.Bytes(uint64(avail)), humanize.Bytes(uint64(size))))
			}
			inodesCell := model.NewTableCell()
			if !timeseries.IsNaN(iu) {
				inodesCell.SetValue(fmt.Sprintf("%.0f%%", iu))
			}
			mode := model.NewTableCell("rw")
			if fs.IsReadonly() {
				mode.SetStatus(model.WARNING, "read-only")
				if !fs.BecameReadonly() {
					mode.UpdateStatus(model.UNKNOWN)
				}
			}
			report.GetOrCreateTable("Node filesystem", "Device", "Space", "Inodes", "Mode").AddRow(
				model.NewTableCell(item),
				model.NewTableCell(fs.Device).SetUnit(fs.FsType),
				space,
				inodesCell,
				mode,
			)
		}
	}
	return true
}

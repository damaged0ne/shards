package auditor

import (
	"sort"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: reports and checks for the MySQL metrics added by the shards cluster agent.

const mysqlUnusedIndexesLimit = 20

func (a *appAuditor) mysqlExt(report *model.AuditReport) {
	applierCheck := report.CreateCheck(model.DBExtChecks.MysqlApplierLag)
	applierTitle := "Replication applier lag <selector>, seconds"

	type idx struct {
		key  model.MysqlIndexKey
		size float32
	}
	unused := map[model.MysqlIndexKey]float32{}
	for _, i := range a.app.Instances {
		if i.Mysql == nil || i.Mysql.Ext == nil {
			continue
		}
		e := i.Mysql.Ext
		if len(e.WaitEventTime) > 0 {
			report.GetOrCreateChartInGroup("Wait time by event (performance_schema) <selector>, seconds/second", i.Name, nil).
				Group("Queries", 1).Stacked().Sorted().AddMany(toSeriesData(e.WaitEventTime), 10, timeseries.NanSum)
		}

		chart := report.GetOrCreateChartInGroup(applierTitle, i.Name, nil).Group("Replication", 3)
		for _, ch := range sortedKeys(e.ApplierLastTransactionLag) {
			chart.AddSeries(applierChannel(ch)+" last transaction", e.ApplierLastTransactionLag[ch])
		}
		for _, ch := range sortedKeys(e.ApplierCurrentLag) {
			chart.AddSeries(applierChannel(ch)+" current transaction", e.ApplierCurrentLag[ch])
		}
		if !i.IsObsolete() {
			for _, ch := range sortedKeys(e.ApplierLastTransactionLag) {
				lag := e.ApplierLastTransactionLag[ch].Last()
				if cur := e.ApplierCurrentLag[ch].Last(); timeseries.IsNaN(lag) || cur > lag {
					lag = cur
				}
				if !timeseries.IsNaN(lag) && lag > applierCheck.Threshold {
					applierCheck.AddItem("%s", i.Name)
					applierCheck.AddDetail("%s: the applier of the channel %s is %s behind the source", i.Name, applierChannel(ch), utils.FormatDuration(timeseries.Duration(lag), 1))
				}
			}
		}

		for k, ts := range e.UnusedIndexes {
			if ts.Last() != 1 {
				continue
			}
			size := e.UnusedIndexBytes[k].Last()
			if timeseries.IsNaN(size) {
				size = 0
			}
			if size >= unused[k] {
				unused[k] = size
			}
		}
	}
	applierCheck.AddWidget(report.GetOrCreateChartGroup(applierTitle, nil).Widget())

	var idxs []idx
	for k, size := range unused {
		idxs = append(idxs, idx{k, size})
	}
	sort.Slice(idxs, func(i, j int) bool {
		return idxs[i].size > idxs[j].size || idxs[i].size == idxs[j].size && idxs[i].key.String() < idxs[j].key.String()
	})
	for n, ix := range idxs {
		if n >= mysqlUnusedIndexesLimit {
			break
		}
		size := model.NewTableCell()
		if ix.size > 0 {
			val, unit := utils.FormatBytes(ix.size)
			size.SetValue(val).SetUnit(unit)
		}
		report.GetOrCreateTable("Unused index (not used since the server start)", "Size").
			Group("Indexes", 9).
			AddRow(model.NewTableCell(ix.key.String()), size)
	}
}

func applierChannel(ch string) string {
	if ch == "" {
		return "default"
	}
	return ch
}

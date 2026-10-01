package auditor

import (
	"fmt"
	"math"
	"sort"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: reports and checks for the Postgres metrics added by the shards cluster agent.

const (
	// pgCacheHitMinActivity is the block request rate (hits + reads per second) below which the cache hit ratio
	// isn't checked: on an idle database a few cold reads would make the ratio meaningless.
	pgCacheHitMinActivity = 50
	pgTopQueriesExtLimit  = 10
	pgUnusedIndexesLimit  = 20
)

// pointsFor returns the number of points covering the duration (at least 1).
func pointsFor(ctx timeseries.Context, d timeseries.Duration) int {
	if ctx.Step <= 0 {
		return 1
	}
	n := int(d / ctx.Step)
	if n < 1 {
		n = 1
	}
	if pc := ctx.PointsCount(); pc > 0 && n > pc {
		n = pc
	}
	return n
}

// recentCount converts the average rate over the last n points into the number of events.
func recentCount(ts *timeseries.TimeSeries, n int, step timeseries.Duration) float32 {
	avg := ts.LastNAvg(n, 0)
	return float32(math.Round(float64(avg * float32(n) * float32(step))))
}

// windowCount converts a rate into the number of events over the whole window.
func windowCount(ts *timeseries.TimeSeries, step timeseries.Duration) float32 {
	if ts.IsEmpty() {
		return 0
	}
	sum := ts.Reduce(timeseries.NanSum)
	if timeseries.IsNaN(sum) {
		return 0
	}
	return float32(math.Round(float64(sum * float32(step))))
}

func sortedKeys[V any](m map[string]V) []string {
	res := make([]string, 0, len(m))
	for k := range m {
		res = append(res, k)
	}
	sort.Strings(res)
	return res
}

func toSeriesData(m map[string]*timeseries.TimeSeries) map[string]model.SeriesData {
	res := make(map[string]model.SeriesData, len(m))
	for k, v := range m {
		res[k] = v
	}
	return res
}

func (a *appAuditor) postgresExt(report *model.AuditReport) {
	ctx := a.w.Ctx
	deadlocksCheck := report.CreateCheck(model.DBExtChecks.PostgresDeadlocks)
	checksumCheck := report.CreateCheck(model.DBExtChecks.PostgresChecksumFailures)
	checksumCheck.Escalate()
	logicalCheck := report.CreateCheck(model.DBExtChecks.PostgresLogicalReplication)
	cacheCheck := report.CreateCheck(model.DBExtChecks.PostgresCacheHitRatio)
	idleTxCheck := report.CreateCheck(model.DBExtChecks.PostgresIdleInTransaction)

	n5, n10 := pointsFor(ctx, 5*timeseries.Minute), pointsFor(ctx, 10*timeseries.Minute)

	unusedIndexes := map[string]float32{}
	for _, i := range a.app.Instances {
		if i.Postgres == nil {
			continue
		}
		e := i.Postgres.Ext

		// idle in transaction: the time spent idle in transaction per second is the average number of such sessions (PG14+),
		// older versions fall back to the pg_stat_activity snapshot
		idleTx := timeseries.NaN
		if e != nil && len(e.IdleInTxTime) > 0 {
			idleTx = model.SumSeries(e.IdleInTxTime).LastNAvg(n5, timeseries.NaN)
			report.GetOrCreateChartInGroup("Sessions idle in transaction (average) <selector>", i.Name, nil).
				Group("Connections", 2).Stacked().AddMany(toSeriesData(e.IdleInTxTime), 10, timeseries.Max)
		}
		if timeseries.IsNaN(idleTx) {
			idle := timeseries.NewAggregate(timeseries.NanSum)
			for k, ts := range i.Postgres.Connections {
				if k.State == "idle in transaction" {
					idle.Add(ts)
				}
			}
			idleTx = idle.Get().LastNAvg(n5, timeseries.NaN)
		}
		if !timeseries.IsNaN(idleTx) && idleTx > idleTxCheck.Threshold && !i.IsObsolete() {
			idleTxCheck.AddItem("%s", i.Name)
			idleTxCheck.AddDetail("%s: %.1f sessions idle in transaction on average over the last 5 minutes", i.Name, idleTx)
		}

		if e == nil {
			continue
		}

		// transactions, cache, errors
		report.GetOrCreateChartInGroup("Transactions <selector>, per second", i.Name, nil).
			Group("Queries", 1).Stacked().
			AddSeries("commit", model.SumSeries(e.XactCommit), "green").
			AddSeries("rollback", model.SumSeries(e.XactRollback), "red-lighten2")
		hitRatio := e.CacheHitRatio()
		report.GetOrCreateChart("Buffer cache hit ratio, %", nil).Group("Queries", 1).AddSeries(i.Name, hitRatio)
		report.GetOrCreateChartInGroup("Errors <selector>, per second", i.Name, nil).
			Group("Queries", 1).
			AddSeries("deadlocks", model.SumSeries(e.Deadlocks), "red").
			AddSeries("recovery conflicts", model.SumSeries(e.Conflicts), "orange").
			AddSeries("checksum failures", model.SumSeries(e.ChecksumFailures), "black").
			AddSeries("sessions abandoned", model.SumSeries(e.SessionsAbandoned), "grey").
			AddSeries("sessions fatal", model.SumSeries(e.SessionsFatal), "purple").
			AddSeries("sessions killed", model.SumSeries(e.SessionsKilled), "blue")
		report.GetOrCreateChartInGroup("Temporary files written <selector>, bytes/second", i.Name, nil).
			Group("Queries", 1).Stacked().AddMany(toSeriesData(e.TempBytes), 10, timeseries.Max)
		if len(e.WaitEvents) > 0 {
			report.GetOrCreateChartInGroup("Sessions by wait event <selector>", i.Name, nil).
				Group("Queries", 1).Stacked().Sorted().AddMany(toSeriesData(e.WaitEvents), 10, timeseries.NanSum)
		}

		for _, db := range sortedKeys(e.Deadlocks) {
			if n := recentCount(e.Deadlocks[db], n5, ctx.Step); n > deadlocksCheck.Threshold {
				deadlocksCheck.AddItem("%s", db)
				deadlocksCheck.AddDetail("%s: %.0f deadlocks in the database %s over the last 5 minutes", i.Name, n, db)
			}
		}
		for _, db := range sortedKeys(e.ChecksumFailures) {
			if n := windowCount(e.ChecksumFailures[db], ctx.Step); n > checksumCheck.Threshold {
				checksumCheck.AddItem("%s", db)
				checksumCheck.AddDetail("%s: %.0f data page checksum failures in the database %s - the data on disk is corrupted", i.Name, n, db)
			}
		}

		if !i.IsObsolete() {
			hit, reads := model.SumSeries(e.BlksHit).LastNAvg(n10, timeseries.NaN), model.SumSeries(e.BlksRead).LastNAvg(n10, timeseries.NaN)
			if !timeseries.IsNaN(hit) && !timeseries.IsNaN(reads) && hit+reads >= pgCacheHitMinActivity {
				ratio := hit / (hit + reads) * 100
				if ratio < cacheCheck.Threshold {
					cacheCheck.AddItem("%s", i.Name)
					cacheCheck.AddDetail("%s: %.1f%% of block reads were served from shared buffers over the last 10 minutes (%.0f blocks/s read from disk or the OS cache)", i.Name, ratio, reads)
				}
			}
		}

		// logical replication
		lagChart := report.GetOrCreateChartInGroup("Logical replication: time since the last confirmed position <selector>, seconds", i.Name, nil).Group("Replication", 6)
		for _, name := range sortedKeys(e.Subscriptions) {
			s := e.Subscriptions[name]
			lag := s.ApplyLag()
			lagChart.AddSeries(name, lag)
			item := i.Name + ": " + name
			switch {
			case s.WorkerUp.IsEmpty():
			case s.WorkerUp.Last() == 0:
				logicalCheck.AddItem("%s", item)
				logicalCheck.AddDetail("%s: the apply worker of the subscription is not running", item)
			case lag.Last() > logicalCheck.Threshold:
				logicalCheck.AddItem("%s", item)
				logicalCheck.AddDetail("%s: no position confirmed to the publisher for %s", item, utils.FormatDuration(timeseries.Duration(lag.Last()), 1))
			}
			if n := recentCount(timeseries.NewAggregate(timeseries.NanSum).Add(s.ApplyErrors, s.SyncErrors).Get(), n5, ctx.Step); n > 0 {
				logicalCheck.AddDetail("%s: %.0f apply/sync errors over the last 5 minutes", item, n)
			}
		}
		standbyChart := report.GetOrCreateChartInGroup("Standby replay lag as seen by the primary <selector>, seconds", i.Name, nil).Group("Replication", 6)
		for k, s := range e.Standbys {
			standbyChart.AddSeries(k.String(), s.ReplayLagSeconds)
		}

		// I/O (pg_stat_io)
		if len(e.IOReads) > 0 || len(e.IOWrites) > 0 {
			report.GetOrCreateChartInGroup("I/O operations (pg_stat_io) <selector>, per second", i.Name, nil).
				Group("I/O", 7).Stacked().
				AddSeries("reads", model.SumSeries(e.IOReads), "blue").
				AddSeries("writes", model.SumSeries(e.IOWrites), "amber").
				AddSeries("extends", model.SumSeries(e.IOExtends), "green").
				AddSeries("fsyncs", model.SumSeries(e.IOFsyncs), "red-lighten2")
			report.GetOrCreateChartInGroup("Buffer hits and evictions (pg_stat_io) <selector>, per second", i.Name, nil).
				Group("I/O", 7).
				AddSeries("hits", model.SumSeries(e.IOHits), "green").
				AddSeries("evictions", model.SumSeries(e.IOEvictions), "red-lighten2")
			report.GetOrCreateChartInGroup("I/O time by backend type <selector>, seconds/second", i.Name, nil).
				Group("I/O", 7).Stacked().AddMany(toSeriesData(e.IOTime), 10, timeseries.NanSum)
		}

		// WAL (pg_stat_wal)
		report.GetOrCreateChartInGroup("WAL records <selector>, per second", i.Name, nil).
			Group("WAL", 5).
			AddSeries("records", e.WalRecords, "blue").
			AddSeries("full page images", e.WalFpi, "amber").
			AddSeries("WAL buffers full", e.WalBuffersFull, "red-lighten2")

		// indexes
		for _, db := range sortedKeys(e.DbUnusedIndexes) {
			count := e.DbUnusedIndexes[db].Last()
			dups := e.DbDuplicateIndexes[db].Last()
			if (timeseries.IsNaN(count) || count == 0) && (timeseries.IsNaN(dups) || dups == 0) {
				continue
			}
			size := model.NewTableCell()
			if v := e.DbUnusedIndexesBytes[db].Last(); v > 0 {
				val, unit := utils.FormatBytes(v)
				size.SetValue(val).SetUnit(unit)
			}
			report.GetOrCreateTable("Instance: database", "Unused indexes", "Unused indexes size", "Duplicate indexes").
				Group("Indexes", 9).
				AddRow(model.NewTableCell(i.Name+": "+db), model.NewTableCell(pgFmtCount(count)), size, model.NewTableCell(pgFmtCount(dups)))
		}
		for k, ts := range e.UnusedIndexBytes {
			if v := ts.Last(); !timeseries.IsNaN(v) && v > unusedIndexes[k.String()] {
				unusedIndexes[k.String()] = v
			}
		}

		pgTopQueriesExt(report, i)
	}

	type idx struct {
		name string
		size float32
	}
	var idxs []idx
	for name, size := range unusedIndexes {
		idxs = append(idxs, idx{name, size})
	}
	sort.Slice(idxs, func(i, j int) bool {
		return idxs[i].size > idxs[j].size || idxs[i].size == idxs[j].size && idxs[i].name < idxs[j].name
	})
	for n, ix := range idxs {
		if n >= pgUnusedIndexesLimit {
			break
		}
		val, unit := utils.FormatBytes(ix.size)
		report.GetOrCreateTable("Unused index (never scanned since the stats reset)", "Size").
			Group("Indexes", 9).
			AddRow(model.NewTableCell(ix.name), model.NewTableCell(val).SetUnit(unit))
	}

	deadlocksCheck.AddWidget(report.GetOrCreateChartGroup("Errors <selector>, per second", nil).Widget())
	checksumCheck.AddWidget(report.GetOrCreateChartGroup("Errors <selector>, per second", nil).Widget())
	cacheCheck.AddWidget(report.GetOrCreateChart("Buffer cache hit ratio, %", nil).Widget())
	logicalCheck.AddWidget(report.GetOrCreateChartGroup("Logical replication: time since the last confirmed position <selector>, seconds", nil).Widget())
	idleTxCheck.AddWidget(report.GetOrCreateChartGroup("Sessions idle in transaction (average) <selector>", nil).Widget())
	idleTxCheck.AddWidget(report.GetOrCreateChartGroup(pgIdleTransactionsTitle, nil).Widget())
}

func pgFmtCount(v float32) string {
	if timeseries.IsNaN(v) {
		return ""
	}
	return fmt.Sprintf("%.0f", v)
}

func pgTopQueriesExt(report *model.AuditReport, i *model.Instance) {
	e := i.Postgres.Ext
	if len(e.PerQuery) == 0 {
		return
	}
	type q struct {
		key  model.QueryKey
		time float32
	}
	var qs []q
	for k := range e.PerQuery {
		var t float32
		if s := i.Postgres.PerQuery[k]; s != nil {
			if v := s.TotalTime.Reduce(timeseries.NanSum); !timeseries.IsNaN(v) {
				t = v
			}
		}
		qs = append(qs, q{k, t})
	}
	sort.Slice(qs, func(a, b int) bool {
		return qs[a].time > qs[b].time || qs[a].time == qs[b].time && qs[a].key.String() < qs[b].key.String()
	})
	avg := func(ts *timeseries.TimeSeries) float32 {
		if ts.IsEmpty() {
			return timeseries.NaN
		}
		return ts.Reduce(timeseries.NanSum) / float32(ts.Len())
	}
	num := func(v float32, unit string) *model.TableCell {
		c := model.NewTableCell()
		if !timeseries.IsNaN(v) {
			c.SetValue(utils.FormatFloat(v)).SetUnit(unit)
		}
		return c
	}
	latency := func(v float32) *model.TableCell {
		c := model.NewTableCell()
		if !timeseries.IsNaN(v) {
			c.SetValue(utils.FormatLatency(v))
		}
		return c
	}
	for n, item := range qs {
		if n >= pgTopQueriesExtLimit {
			break
		}
		s := e.PerQuery[item.key]
		var calls, totalTime float32 = timeseries.NaN, timeseries.NaN
		if base := i.Postgres.PerQuery[item.key]; base != nil {
			calls, totalTime = avg(base.Calls), avg(base.TotalTime)
		}
		hitRatio := timeseries.NaN
		if hit, read := avg(s.SharedBlksHit), avg(s.SharedBlksRead); !timeseries.IsNaN(hit) && !timeseries.IsNaN(read) && hit+read > 0 {
			hitRatio = hit / (hit + read) * 100
		}
		temp := timeseries.NewAggregate(timeseries.NanSum).Add(s.TempBlksRead, s.TempBlksWritten).Get()
		walCell := model.NewTableCell()
		if v := avg(s.WalBytes); !timeseries.IsNaN(v) {
			val, unit := utils.FormatBytes(v)
			walCell.SetValue(val).SetUnit(unit + "/s")
		}
		report.GetOrCreateTable("Instance", "Top query (by total time)", "Calls", "Time", "Mean", "Max", "Rows", "Cache hit", "Temp blocks", "WAL", "Planning").
			Group("Queries", 1).
			AddRow(
				model.NewTableCell(i.Name),
				model.NewTableCell(item.key.String()).SetMaxWidth(400),
				num(calls, "/s"),
				num(totalTime, "s/s"),
				latency(s.ExecTimeMean.Last()),
				latency(s.ExecTimeMax.Last()),
				num(avg(s.Rows), "/s"),
				num(hitRatio, "%"),
				num(avg(temp), "/s"),
				walCell,
				num(avg(s.PlanTime), "s/s"),
			)
	}
}

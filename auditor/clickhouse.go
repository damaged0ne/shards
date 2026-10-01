package auditor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: the ClickHouse report built from the metrics of the shards cluster agent.

const (
	chRecentWindow = 5 * timeseries.Minute
	// Since 23.6 ClickHouse delays INSERTs into a partition with more than parts_to_delay_insert=1000 active parts
	// (https://clickhouse.com/docs/operations/settings/merge-tree-settings#parts_to_delay_insert).
	chPartsToDelayInsert = 1000
	chTopQueries         = 20
	chChartTopN          = 10
)

func (a *appAuditor) clickhouse() {
	isClickHouse := a.app.ApplicationTypes()[model.ApplicationTypeClickHouse]
	if !isClickHouse && !a.app.IsClickHouse() {
		return
	}
	report := a.addReport(model.AuditReportClickHouse)
	report.Instrumentation = model.ApplicationTypeClickHouse
	if !a.app.IsClickHouse() {
		report.Status = model.UNKNOWN
		return
	}

	availabilityCheck := report.CreateCheck(model.Checks.ClickHouseAvailability)
	replicationCheck := report.CreateCheck(model.Checks.ClickHouseReplication)
	partsCheck := report.CreateCheck(model.Checks.ClickHouseTooManyParts)
	mutationsCheck := report.CreateCheck(model.Checks.ClickHouseStuckMutations)
	rejectedCheck := report.CreateCheck(model.Checks.ClickHouseRejectedInserts)

	instancesTable := report.GetOrCreateTable("Instance", "Status", "Uptime", "Queries", "Failed", "Running", "Memory", "Max parts / partition", "Replication delay")
	tablesTable := report.GetOrCreateTable("Table", "Size", "Parts", "Max parts / partition", "Replication", "Replication queue", "Mutations").Group("Tables", 4)
	queriesTable := report.GetOrCreateTable("Query", "Calls", "Total time", "Avg latency", "Rows read", "Errors").Group("Top queries", 2)

	qpsChart := report.GetOrCreateChart("Queries, per second", nil).Group("Queries", 1)
	failedChart := report.GetOrCreateChart("Failed queries, per second", nil).Group("Queries", 1)
	runningChart := report.GetOrCreateChart("Running queries", nil).Group("Queries", 1)
	queryTimeChart := report.GetOrCreateChartGroup("Queries by total time <selector>, query seconds/second", nil).Group("Top queries", 2)
	memoryChart := report.GetOrCreateChartGroup("Memory <selector>, bytes", nil).Group("Memory", 3)
	insertedRowsChart := report.GetOrCreateChart("Inserted rows, per second", nil).Group("Inserts", 3)
	insertedBytesChart := report.GetOrCreateChart("Inserted bytes, per second", nil).Group("Inserts", 3)
	throttledChart := report.GetOrCreateChartGroup("Delayed and rejected INSERTs <selector>, per second", nil).Group("Inserts", 3)
	partsChart := report.GetOrCreateChart("Max active parts in a partition", nil).Group("Merges and parts", 4)
	mergesChart := report.GetOrCreateChartGroup("Running merges and mutations <selector>", nil).Group("Merges and parts", 4)
	replDelayChart := report.GetOrCreateChart("Max replication delay, seconds", nil).Group("Replication", 5)
	replQueueChart := report.GetOrCreateChart("Replication queue size", nil).Group("Replication", 5)
	readonlyChart := report.GetOrCreateChart("Read-only replicated tables", nil).Group("Replication", 5)
	zkSessionsChart := report.GetOrCreateChart("ZooKeeper/Keeper sessions", nil).Group("ZooKeeper", 6)
	zkExceptionsChart := report.GetOrCreateChartGroup("ZooKeeper/Keeper exceptions <selector>, per second", nil).Group("ZooKeeper", 6)
	errorsChart := report.GetOrCreateChartGroup("Errors by name <selector>, per second", nil).Group("Errors", 7)

	availabilityCheck.AddWidget(instancesTable.Widget())
	replicationCheck.AddWidget(replDelayChart.Widget())
	replicationCheck.AddWidget(readonlyChart.Widget())
	replicationCheck.AddWidget(tablesTable.Widget())
	partsCheck.AddWidget(partsChart.Widget())
	partsCheck.AddWidget(tablesTable.Widget())
	mutationsCheck.AddWidget(tablesTable.Widget())
	rejectedCheck.AddWidget(throttledChart.Widget())

	recent := recentPoints(a.w.Ctx, chRecentWindow)
	var maxParts float32
	multi := 0
	for _, i := range a.app.Instances {
		if i.ClickHouse != nil && !i.IsObsolete() {
			multi++
		}
	}
	tableName := func(i *model.Instance, k model.DbTableKey) string {
		if multi > 1 {
			return i.Name + ": " + k.String()
		}
		return k.String()
	}

	for _, i := range a.app.Instances {
		ch := i.ClickHouse
		if ch == nil || i.IsObsolete() {
			continue
		}
		up := ch.IsUp()
		if !up {
			availabilityCheck.AddItem("%s", i.Name)
			if e := ch.Error.Value(); e != "" {
				availabilityCheck.AddDetail("%s: %s", i.Name, e)
			}
		}

		// replication
		var readonlyTables, delayedTables []string
		for k, t := range ch.Tables {
			if lastValue(t.ReplicaReadonly) > 0 || lastValue(t.ReplicaSessionExpired) > 0 {
				readonlyTables = append(readonlyTables, k.String())
			}
			if d := lastValue(t.ReplicaAbsoluteDelay); d > replicationCheck.Threshold {
				delayedTables = append(delayedTables, fmt.Sprintf("%s (%s)", k.String(), utils.FormatDuration(timeseries.Duration(d), 1)))
			}
		}
		sort.Strings(readonlyTables)
		sort.Strings(delayedTables)
		readonly := lastValue(ch.ReadonlyReplicas)
		if readonly > 0 || len(readonlyTables) > 0 {
			replicationCheck.AddItem("%s", i.Name)
			replicationCheck.SetCritical()
			msg := fmt.Sprintf("%s: %.0f replicated tables are read-only (INSERTs fail; usually a lost ZooKeeper/Keeper session)", i.Name, max(readonly, float32(len(readonlyTables))))
			if len(readonlyTables) > 0 {
				msg += ": " + strings.Join(readonlyTables, ", ")
			}
			replicationCheck.AddDetail("%s", msg)
		}
		if lost := recentSum(a.w.Ctx, ch.ReplDataLoss, recentPoints(a.w.Ctx, 15*timeseries.Minute)); lost > 0 {
			replicationCheck.AddItem("%s", i.Name)
			replicationCheck.SetCritical()
			replicationCheck.AddDetail("%s: %.0f data parts were not found on any replica (data loss)", i.Name, lost)
		}
		replDelay := lastValue(ch.ReplicasMaxDelay)
		if replDelay > replicationCheck.Threshold {
			replicationCheck.AddItem("%s", i.Name)
			msg := fmt.Sprintf("%s: replication delay %s", i.Name, utils.FormatDuration(timeseries.Duration(replDelay), 1))
			if len(delayedTables) > 0 {
				msg += ": " + strings.Join(delayedTables, ", ")
			}
			replicationCheck.AddDetail("%s", msg)
		}

		// parts
		instanceMaxParts := lastValue(ch.MaxPartsPerPartition)
		if instanceMaxParts > partsCheck.Threshold {
			found := false
			for k, t := range ch.Tables {
				if p := lastValue(t.MaxPartsPerPartition); p > partsCheck.Threshold {
					partsCheck.AddItem("%s", tableName(i, k))
					partsCheck.AddDetail("%s: %.0f active parts in a partition", tableName(i, k), p)
					found = true
				}
			}
			if !found { // the table isn't among the tables with per-table metrics (topTables)
				partsCheck.AddItem("%s", i.Name)
				partsCheck.AddDetail("%s: %.0f active parts in a partition", i.Name, instanceMaxParts)
			}
			maxParts = max(maxParts, instanceMaxParts)
			if instanceMaxParts > chPartsToDelayInsert {
				partsCheck.SetCritical()
			}
		}

		// mutations
		for k, t := range ch.Tables {
			stuck, failing := lastValue(t.MutationsStuck), lastValue(t.MutationsFailing)
			if stuck > 0 || failing > 0 {
				mutationsCheck.AddItem("%s", tableName(i, k))
				mutationsCheck.AddDetail("%s: %.0f failing, %.0f unfinished for more than an hour", tableName(i, k), max(failing, 0), max(stuck, 0))
			}
		}

		// rejected inserts
		if rejected := recentSum(a.w.Ctx, ch.RejectedInserts, recent); rejected >= 1 {
			rejectedCheck.Inc(int64(rejected))
			rejectedCheck.AddDetail("%s: %.0f INSERTs rejected with \"Too many parts\"", i.Name, rejected)
		}

		// the instances table
		memory := ch.MemoryTracking
		if memory.IsEmpty() {
			memory = ch.MemoryResident
		}
		memoryCell := bytesCell(lastValue(memory))
		if total := lastValue(ch.OSMemoryTotal); total > 0 && !timeseries.IsNaN(lastValue(memory)) {
			memoryCell.AddTag("%.0f%% of RAM", lastValue(memory)/total*100)
		}
		partsCell := floatCell(instanceMaxParts, "")
		if instanceMaxParts > partsCheck.Threshold {
			partsCell.SetStatus(model.WARNING, utils.FormatFloat(instanceMaxParts))
		}
		delayCell := model.NewTableCell()
		if !timeseries.IsNaN(replDelay) {
			delayCell.SetValue(utils.FormatFloat(replDelay)).SetUnit("s")
		}
		if readonly > 0 {
			delayCell.SetStatus(model.CRITICAL, fmt.Sprintf("%.0f read-only tables", readonly))
		}
		uptime := model.NewTableCell()
		if u := lastValue(ch.Uptime); !timeseries.IsNaN(u) {
			uptime.SetValue(utils.FormatDurationShort(timeseries.Duration(u), 2))
		}
		instancesTable.AddRow(
			model.NewTableCell(i.Name).AddTag("version: %s", ch.Version.Value()),
			targetStatusCell(up, ch.Error.Value(), ch.Warning.Value()),
			uptime,
			floatCell(recentAvg(ch.Queries["all"], recent), "/s"),
			floatCell(recentAvg(ch.FailedQueries["all"], recent), "/s"),
			floatCell(lastValue(ch.QueriesRunning), ""),
			memoryCell,
			partsCell,
			delayCell,
		)

		// tables
		for k, t := range ch.Tables {
			replication := model.NewTableCell()
			if !t.ReplicaAbsoluteDelay.IsEmpty() || !t.ReplicaReadonly.IsEmpty() {
				replication.SetStatus(model.OK, "ok")
				if d := lastValue(t.ReplicaAbsoluteDelay); d > 0 {
					replication.SetValue(fmt.Sprintf("delay %s", utils.FormatDurationShort(timeseries.Duration(d), 1)))
					if d > replicationCheck.Threshold {
						replication.SetStatus(model.WARNING, replication.Value)
					}
				}
				if lastValue(t.ReplicaReadonly) > 0 {
					replication.SetStatus(model.CRITICAL, "read-only")
				}
				if lastValue(t.ReplicaSessionExpired) > 0 {
					replication.SetStatus(model.CRITICAL, "session expired")
				}
			}
			mutations := model.NewTableCell()
			inProgress, failing, stuck := lastValue(t.MutationsInProgress), lastValue(t.MutationsFailing), lastValue(t.MutationsStuck)
			switch {
			case failing > 0:
				mutations.SetStatus(model.WARNING, fmt.Sprintf("%.0f failing", failing))
			case stuck > 0:
				mutations.SetStatus(model.WARNING, fmt.Sprintf("%.0f stuck", stuck))
			case inProgress > 0:
				mutations.SetValue(fmt.Sprintf("%.0f in progress", inProgress))
			}
			tablePartsCell := floatCell(lastValue(t.MaxPartsPerPartition), "")
			if p := lastValue(t.MaxPartsPerPartition); p > partsCheck.Threshold {
				tablePartsCell.SetStatus(model.WARNING, utils.FormatFloat(p))
			}
			tablesTable.AddRow(
				model.NewTableCell(tableName(i, k)),
				bytesCell(lastValue(t.SizeBytes)),
				floatCell(lastValue(t.Parts), ""),
				tablePartsCell,
				replication,
				floatCell(lastValue(t.ReplicaQueueSize), ""),
				mutations,
			)
		}

		chQueries(i, queriesTable, queryTimeChart)

		// charts
		qpsChart.AddSeries(i.Name, ch.Queries["all"])
		failedChart.AddSeries(i.Name, ch.FailedQueries["all"])
		runningChart.AddSeries(i.Name, ch.QueriesRunning)
		if memoryChart != nil {
			memoryChart.GetOrCreateChart(i.Name).
				AddSeries("tracked by ClickHouse", ch.MemoryTracking).
				AddSeries("resident", ch.MemoryResident).
				SetThreshold("RAM", ch.OSMemoryTotal)
		}
		insertedRowsChart.AddSeries(i.Name, ch.InsertedRows)
		insertedBytesChart.AddSeries(i.Name, ch.InsertedBytes)
		if throttledChart != nil {
			throttledChart.GetOrCreateChart(i.Name).Column().
				AddSeries("delayed", ch.DelayedInserts, "orange").
				AddSeries("rejected", ch.RejectedInserts, "red")
		}
		partsChart.AddSeries(i.Name, ch.MaxPartsPerPartition)
		if partsChart != nil && partsChart.Threshold == nil {
			partsChart.SetThreshold("threshold", ch.MaxPartsPerPartition.WithNewValue(partsCheck.Threshold))
		}
		if mergesChart != nil {
			mergesChart.GetOrCreateChart(i.Name).
				AddSeries("merges", ch.MergesRunning).
				AddSeries("mutations", ch.MutationsRunning).
				AddSeries("replicated fetches", ch.ReplFetchesRunning)
		}
		replDelayChart.AddSeries(i.Name, ch.ReplicasMaxDelay)
		replQueueChart.AddSeries(i.Name, ch.ReplicasSumQueueSize)
		readonlyChart.AddSeries(i.Name, ch.ReadonlyReplicas)
		zkSessionsChart.AddSeries(i.Name, ch.ZooKeeperSessions)
		if zkExceptionsChart != nil {
			exc := map[string]model.SeriesData{}
			for t, ts := range ch.ZooKeeperExceptions {
				exc[t] = ts
			}
			zkExceptionsChart.GetOrCreateChart(i.Name).Stacked().AddMany(exc, chChartTopN, timeseries.NanSum)
		}
		if errorsChart != nil {
			errs := map[string]model.SeriesData{}
			for name, ts := range ch.Errors {
				errs[name] = ts
			}
			errorsChart.GetOrCreateChart(i.Name).Stacked().Sorted().AddMany(errs, chChartTopN, timeseries.NanSum)
		}
	}
	if maxParts > 0 {
		partsCheck.SetValue(maxParts)
	}
}

func chQueries(i *model.Instance, table *model.Table, chart *model.ChartGroup) {
	type row struct {
		key  model.ClickHouseQueryKey
		stat *model.ClickHouseQueryStat
		time float32
	}
	var rows []row
	totalTime := map[string]model.SeriesData{}
	for k, s := range i.ClickHouse.PerQuery {
		totalTime[k.String()] = s.TotalTime
		t := s.TotalTime.Reduce(timeseries.NanSum)
		if timeseries.IsNaN(t) {
			t = 0
		}
		rows = append(rows, row{key: k, stat: s, time: t})
	}
	if chart != nil {
		chart.GetOrCreateChart(i.Name).Stacked().Sorted().AddMany(totalTime, 5, timeseries.NanSum)
	}
	if table == nil {
		return
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].time > rows[b].time })
	if len(rows) > chTopQueries {
		rows = rows[:chTopQueries]
	}
	table.SetSorted()
	for _, r := range rows {
		calls := r.stat.Calls.Average()
		time := r.stat.TotalTime.Average()
		latency := model.NewTableCell()
		if calls > 0 && !timeseries.IsNaN(time) {
			latency.SetValue(utils.FormatLatency(time / calls))
		}
		q := model.NewTableCell(r.key.Query).SetMaxWidth(80)
		if r.key.Db != "" {
			q.AddTag("db: %s", r.key.Db)
		}
		errorsCell := floatCell(r.stat.Errors.Average(), "/s")
		if e := r.stat.Errors.Average(); e > 0 {
			errorsCell.SetStatus(model.WARNING, utils.FormatFloat(e)+"/s")
		}
		table.AddRow(
			q,
			floatCell(calls, "/s"),
			floatCell(time, "s/s"),
			latency,
			floatCell(r.stat.ReadRows.Average(), "/s"),
			errorsCell,
		)
	}
}

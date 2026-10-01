package auditor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: reports and checks for PgBouncer, RabbitMQ, etcd and the managed services
// (Aurora, ElastiCache Serverless, MemoryDB, Azure) monitored by the shards cluster agent.

const (
	docsBase = "https://damaged0ne.github.io/shards/"

	// pgbouncerCriticalWaitFactor: the client wait check escalates to critical above this multiple of its threshold
	// (1s warning / 5s critical by default).
	pgbouncerCriticalWaitFactor = 5
	// etcdCommitThresholdFactor: the backend commit p99 recommendation (25ms) relative to the WAL fsync one (10ms).
	etcdCommitThresholdFactor = 2.5
	// cloudStorageCritical: the storage usage of a managed database above which the disk space check is critical.
	cloudStorageCritical = 90
)

func bytesCell(v float32) *model.TableCell {
	c := model.NewTableCell()
	if !timeseries.IsNaN(v) && v > 0 {
		val, unit := utils.FormatBytes(v)
		c.SetValue(val).SetUnit(unit)
	}
	return c
}

func floatCell(v float32, unit string) *model.TableCell {
	c := model.NewTableCell()
	if !timeseries.IsNaN(v) {
		c.SetValue(utils.FormatFloat(v)).SetUnit(unit)
	}
	return c
}

func percentCell(v float32) *model.TableCell {
	c := model.NewTableCell()
	if !timeseries.IsNaN(v) {
		c.SetValue(fmt.Sprintf("%.0f", v)).SetUnit("%")
	}
	return c
}

func latencyCell(v float32) *model.TableCell {
	c := model.NewTableCell()
	if !timeseries.IsNaN(v) {
		c.SetValue(utils.FormatLatency(v))
	}
	return c
}

func (a *appAuditor) hasType(t model.ApplicationType) bool {
	return a.app.ApplicationTypes()[t]
}

func (a *appAuditor) noMetricsReport(name model.AuditReportName, msg, doc string) {
	report := a.addReport(name)
	report.Status = model.UNKNOWN
	report.ConfigurationHint = &model.ConfigurationHint{Message: msg, ReadMoreLink: docsBase + doc}
}

// PgBouncer

func (a *appAuditor) pgbouncer() {
	var instances []*model.Instance
	for _, i := range a.app.Instances {
		if i.Pgbouncer != nil {
			instances = append(instances, i)
		}
	}
	if len(instances) == 0 {
		if a.hasType(model.ApplicationTypePgbouncer) {
			a.noMetricsReport(model.AuditReportPgbouncer,
				"Configure the shards cluster agent to collect PgBouncer metrics from its admin console (pools, client wait time, query stats).",
				"databases/pgbouncer")
		}
		return
	}
	report := a.addReport(model.AuditReportPgbouncer)
	ctx := a.w.Ctx
	availabilityCheck := report.CreateCheck(model.DBExtChecks.PgbouncerAvailability)
	waitCheck := report.CreateCheck(model.DBExtChecks.PgbouncerClientWaiting)
	saturationCheck := report.CreateCheck(model.DBExtChecks.PgbouncerPoolSaturation)
	n2, n5 := pointsFor(ctx, 2*timeseries.Minute), pointsFor(ctx, 5*timeseries.Minute)

	instanceTable := report.GetOrCreateTable("Instance", "Status", "Pools", "Clients active", "Clients waiting", "Servers active", "Servers idle", "Max wait", "Queries", "Avg query time")
	availabilityCheck.AddWidget(instanceTable.Widget())
	maxWaitTitle := "Max client wait time <selector>, seconds"

	for _, i := range instances {
		p := i.Pgbouncer
		clActive, clWaiting := timeseries.NewAggregate(timeseries.NanSum), timeseries.NewAggregate(timeseries.NanSum)
		svActive, svIdle, svUsed := timeseries.NewAggregate(timeseries.NanSum), timeseries.NewAggregate(timeseries.NanSum), timeseries.NewAggregate(timeseries.NanSum)
		maxWait := timeseries.NewAggregate(timeseries.Max)
		maxWaitByPool := map[string]model.SeriesData{}
		for _, k := range p.PoolsSorted() {
			pool := p.Pools[k]
			clActive.Add(pool.ClientActive)
			clWaiting.Add(pool.ClientWaiting)
			svActive.Add(pool.ServerActive)
			svIdle.Add(pool.ServerIdle)
			svUsed.Add(pool.ServerUsed)
			maxWait.Add(pool.MaxWait)
			maxWaitByPool[k.String()] = pool.MaxWait
			item := i.Name + ": " + k.String()
			if !i.IsObsolete() && p.IsUp() {
				if w := pool.MaxWait.LastNAvg(n2, 0); w > waitCheck.Threshold {
					waitCheck.AddItem("%s", item)
					waitCheck.AddDetail("%s: the oldest client has been waiting for %s for a server connection", item, utils.FormatLatency(w))
					if w > waitCheck.Threshold*pgbouncerCriticalWaitFactor {
						waitCheck.Escalate()
					}
					if w > waitCheck.Value() {
						waitCheck.SetValue(w)
					}
				}
				if pool.Saturated(n5) {
					saturationCheck.AddItem("%s", item)
					saturationCheck.AddDetail("%s: %.0f clients waiting, no idle server connection (%.0f active)", item, pool.ClientWaiting.Last(), pool.ServerActive.Last())
				}
			}
			report.GetOrCreateTable("Pool", "Clients active", "Clients waiting", "Servers active", "Servers idle", "Servers used", "Max wait").
				Group("Pools", 2).
				AddRow(
					model.NewTableCell(item),
					floatCell(pool.ClientActive.Last(), ""),
					floatCell(pool.ClientWaiting.Last(), ""),
					floatCell(pool.ServerActive.Last(), ""),
					floatCell(pool.ServerIdle.Last(), ""),
					floatCell(pool.ServerUsed.Last(), ""),
					latencyCell(pool.MaxWait.Last()),
				)
		}
		report.GetOrCreateChartInGroup("Client connections <selector>", i.Name, nil).Group("Pools", 2).Stacked().
			AddSeries("active", clActive.Get(), "green").
			AddSeries("waiting", clWaiting.Get(), "red-lighten2")
		report.GetOrCreateChartInGroup("Server connections <selector>", i.Name, nil).Group("Pools", 2).Stacked().
			AddSeries("active", svActive.Get(), "green").
			AddSeries("idle", svIdle.Get(), "grey-lighten1").
			AddSeries("used", svUsed.Get(), "blue-lighten2")
		report.GetOrCreateChartInGroup(maxWaitTitle, i.Name, nil).Group("Pools", 2).AddMany(maxWaitByPool, 10, timeseries.Max)

		queries, avgTime := timeseries.NewAggregate(timeseries.NanSum), map[string]model.SeriesData{}
		qps, tps, wait := map[string]model.SeriesData{}, map[string]model.SeriesData{}, map[string]model.SeriesData{}
		queryTime := timeseries.NewAggregate(timeseries.NanSum)
		for _, db := range sortedKeys(p.Stats) {
			s := p.Stats[db]
			queries.Add(s.Queries)
			queryTime.Add(s.QueryTime)
			qps[db], tps[db], wait[db] = s.Queries, s.Transactions, s.ClientWait
			avgTime[db] = s.AvgQueryDuration()
		}
		report.GetOrCreateChartInGroup("Queries <selector>, per second", i.Name, nil).Group("Queries", 1).Stacked().AddMany(qps, 10, timeseries.NanSum)
		report.GetOrCreateChartInGroup("Transactions <selector>, per second", i.Name, nil).Group("Queries", 1).Stacked().AddMany(tps, 10, timeseries.NanSum)
		report.GetOrCreateChartInGroup("Average query duration <selector>, seconds", i.Name, nil).Group("Queries", 1).AddMany(avgTime, 10, timeseries.Max)
		report.GetOrCreateChartInGroup("Client wait time <selector>, seconds/second", i.Name, nil).Group("Queries", 1).Stacked().AddMany(wait, 10, timeseries.NanSum)

		if i.IsObsolete() {
			continue
		}
		status := model.NewTableCell().SetStatus(model.OK, "up")
		if !p.IsUp() {
			availabilityCheck.AddItem("%s", i.Name)
			status.SetStatus(model.WARNING, "down (admin console unreachable)")
		}
		avgQuery := timeseries.NaN
		if q, t := queries.Get().Last(), queryTime.Get().Last(); q > 0 && !timeseries.IsNaN(t) {
			avgQuery = t / q
		}
		instanceTable.AddRow(
			model.NewTableCell(i.Name),
			status,
			floatCell(float32(len(p.Pools)), ""),
			floatCell(clActive.Get().Last(), ""),
			floatCell(clWaiting.Get().Last(), ""),
			floatCell(svActive.Get().Last(), ""),
			floatCell(svIdle.Get().Last(), ""),
			latencyCell(maxWait.Get().Last()),
			floatCell(queries.Get().Last(), "/s"),
			latencyCell(avgQuery),
		)
	}
	waitCheck.AddWidget(report.GetOrCreateChartGroup(maxWaitTitle, nil).Widget())
	saturationCheck.AddWidget(report.GetOrCreateChartGroup("Client connections <selector>", nil).Widget())
	saturationCheck.AddWidget(report.GetOrCreateChartGroup("Server connections <selector>", nil).Widget())
}

// RabbitMQ

func (a *appAuditor) rabbitmq() {
	var instances []*model.Instance
	for _, i := range a.app.Instances {
		if i.Rabbitmq != nil {
			instances = append(instances, i)
		}
	}
	if len(instances) == 0 {
		if a.hasType(model.ApplicationTypeRabbitmq) {
			a.noMetricsReport(model.AuditReportRabbitmq,
				"Configure the shards cluster agent to scrape the RabbitMQ Prometheus endpoint (rabbitmq_prometheus plugin, port 15692).",
				"databases/rabbitmq")
		}
		return
	}
	report := a.addReport(model.AuditReportRabbitmq)
	availabilityCheck := report.CreateCheck(model.DBExtChecks.RabbitmqAvailability)
	alarmsCheck := report.CreateCheck(model.DBExtChecks.RabbitmqAlarms)
	alarmsCheck.Escalate()
	fdCheck := report.CreateCheck(model.DBExtChecks.RabbitmqFileDescriptors)

	table := report.GetOrCreateTable("Node", "Status", "Messages ready", "Unacked", "Consumers", "Connections", "Memory", "Disk free", "File descriptors")
	availabilityCheck.AddWidget(table.Widget())
	alarmsCheck.AddWidget(table.Widget())

	for _, i := range instances {
		r := i.Rabbitmq
		report.GetOrCreateChartInGroup("Queued messages <selector>", i.Name, nil).Group("Messages", 1).Stacked().
			AddSeries("ready", r.MessagesReady, "blue").
			AddSeries("unacked", r.MessagesUnacked, "amber")
		report.GetOrCreateChartInGroup("Message rates <selector>, per second", i.Name, nil).Group("Messages", 1).
			AddSeries("published", r.Published, "blue").
			AddSeries("delivered", r.Delivered, "green").
			AddSeries("unroutable (dropped)", r.Unroutable, "red-lighten2")
		report.GetOrCreateChart("Consumers", nil).Group("Messages", 1).AddSeries(i.Name, r.Consumers)
		report.GetOrCreateChart("Connections", nil).Group("Messages", 1).AddSeries(i.Name, r.Connections)
		report.GetOrCreateChartInGroup("Memory usage <selector>, bytes", i.Name, nil).Group("Resources", 2).
			AddSeries("used", r.MemoryUsed).SetThreshold("high watermark", r.MemoryLimit)
		report.GetOrCreateChartInGroup("Free disk space <selector>, bytes", i.Name, nil).Group("Resources", 2).
			AddSeries("free", r.DiskAvailable).SetThreshold("low watermark", r.DiskLimit)
		report.GetOrCreateChartInGroup("File descriptors <selector>", i.Name, nil).Group("Resources", 2).
			AddSeries("open", r.OpenFds).SetThreshold("limit", r.MaxFds)

		if i.IsObsolete() {
			continue
		}
		name := i.Name
		if n := r.Node.Value(); n != "" && n != name {
			name = i.Name + " (" + n + ")"
		}
		status := model.NewTableCell().SetStatus(model.OK, "up")
		if !r.IsUp() {
			availabilityCheck.AddItem("%s", name)
			status.SetStatus(model.WARNING, "down (no metrics)")
		} else if alarms := r.ActiveAlarms(); len(alarms) > 0 {
			alarmsCheck.AddItem("%s", name)
			alarmsCheck.AddDetail("%s: %s alarm in effect, publishers are blocked", name, strings.Join(alarms, ", "))
			status.SetStatus(model.CRITICAL, "alarm: "+strings.Join(alarms, ", "))
		} else if p := r.UnreachablePeers.Last(); p > 0 {
			status.SetStatus(model.WARNING, fmt.Sprintf("%.0f cluster peers unreachable", p))
		}
		fdPercent := timeseries.NaN
		if open, max := r.OpenFds.Last(), r.MaxFds.Last(); max > 0 && !timeseries.IsNaN(open) {
			fdPercent = open / max * 100
			if fdPercent > fdCheck.Threshold {
				fdCheck.AddItem("%s", name)
				fdCheck.AddDetail("%s: %.0f of %.0f file descriptors are open", name, open, max)
			}
		}
		mem := model.NewTableCell()
		if used, limit := r.MemoryUsed.Last(), r.MemoryLimit.Last(); used > 0 {
			v, u := utils.FormatBytes(used)
			mem.SetValue(v).SetUnit(u)
			if limit > 0 {
				mem.AddTag("%.0f%% of the watermark", used/limit*100)
			}
		}
		table.AddRow(
			model.NewTableCell(name).AddTag("version: %s", r.Version.Value()),
			status,
			floatCell(r.MessagesReady.Last(), ""),
			floatCell(r.MessagesUnacked.Last(), ""),
			floatCell(r.Consumers.Last(), ""),
			floatCell(r.Connections.Last(), ""),
			mem,
			bytesCell(r.DiskAvailable.Last()),
			percentCell(fdPercent),
		)
	}
	fdCheck.AddWidget(report.GetOrCreateChartGroup("File descriptors <selector>", nil).Widget())
}

// etcd

func (a *appAuditor) etcd() {
	var instances []*model.Instance
	for _, i := range a.app.Instances {
		if i.Etcd != nil {
			instances = append(instances, i)
		}
	}
	if len(instances) == 0 {
		if a.hasType(model.ApplicationTypeEtcd) {
			a.noMetricsReport(model.AuditReportEtcd,
				"Configure the shards cluster agent to scrape the etcd metrics endpoint (--listen-metrics-urls, port 2381).",
				"databases/etcd")
		}
		return
	}
	report := a.addReport(model.AuditReportEtcd)
	ctx := a.w.Ctx
	availabilityCheck := report.CreateCheck(model.DBExtChecks.EtcdAvailability)
	leaderCheck := report.CreateCheck(model.DBExtChecks.EtcdNoLeader)
	leaderCheck.Escalate()
	changesCheck := report.CreateCheck(model.DBExtChecks.EtcdLeaderChanges)
	diskCheck := report.CreateCheck(model.DBExtChecks.EtcdDiskLatency)
	sizeCheck := report.CreateCheck(model.DBExtChecks.EtcdDbSize)
	n5, n10 := pointsFor(ctx, 5*timeseries.Minute), pointsFor(ctx, 10*timeseries.Minute)

	table := report.GetOrCreateTable("Member", "Status", "Role", "DB size", "WAL fsync p99", "Commit p99", "Leader changes")
	availabilityCheck.AddWidget(table.Widget())
	leaderCheck.AddWidget(table.Widget())
	fsyncTitle, commitTitle := "WAL fsync duration p99, seconds", "Backend commit duration p99, seconds"
	fsyncThreshold := diskCheck.Threshold
	commitThreshold := diskCheck.Threshold * etcdCommitThresholdFactor

	for _, i := range instances {
		e := i.Etcd
		report.GetOrCreateChart(fsyncTitle, nil).Group("Disk", 2).AddSeries(i.Name, e.WalFsyncP99).
			SetThreshold("recommended", e.WalFsyncP99.WithNewValue(fsyncThreshold))
		report.GetOrCreateChart(commitTitle, nil).Group("Disk", 2).AddSeries(i.Name, e.BackendCommitP99).
			SetThreshold("recommended", e.BackendCommitP99.WithNewValue(commitThreshold))
		report.GetOrCreateChart("Peer round-trip time p99, seconds", nil).Group("Cluster", 1).AddSeries(i.Name, e.PeerRttP99)
		report.GetOrCreateChart("Leader changes, per second", nil).Group("Cluster", 1).Column().AddSeries(i.Name, e.LeaderChanges)
		report.GetOrCreateChartInGroup("Proposals <selector>", i.Name, nil).Group("Cluster", 1).
			AddSeries("applied/s", e.ProposalsApplied, "green").
			AddSeries("failed/s", e.ProposalsFailed, "red").
			AddSeries("pending", e.ProposalsPending, "amber")
		quota := e.Quota
		if quota.IsEmpty() {
			quota = e.DbSize.WithNewValue(model.EtcdDefaultQuotaBytes)
		}
		report.GetOrCreateChartInGroup("Database size <selector>, bytes", i.Name, nil).Group("Disk", 2).
			AddSeries("total", e.DbSize, "blue").
			AddSeries("in use", e.DbSizeInUse, "green").
			SetThreshold("quota", quota)

		if i.IsObsolete() {
			continue
		}
		status := model.NewTableCell().SetStatus(model.OK, "up")
		if !e.IsUp() {
			availabilityCheck.AddItem("%s", i.Name)
			status.SetStatus(model.WARNING, "down (no metrics)")
		} else if e.HasLeader.Last() == 0 {
			leaderCheck.AddItem("%s", i.Name)
			leaderCheck.AddDetail("%s: etcd_server_has_leader = 0", i.Name)
			status.SetStatus(model.CRITICAL, "no leader")
		}
		changes := windowCount(e.LeaderChanges, ctx.Step)
		if changes > changesCheck.Threshold {
			changesCheck.AddItem("%s", i.Name)
			changesCheck.AddDetail("%s: %.0f leader changes over the selected period", i.Name, changes)
		}
		if failed := recentCount(e.ProposalsFailed, n5, ctx.Step); failed > 0 {
			changesCheck.AddItem("%s", i.Name)
			changesCheck.AddDetail("%s: %.0f failed proposals over the last 5 minutes", i.Name, failed)
		}
		fsync, commit := e.WalFsyncP99.LastNAvg(n10, timeseries.NaN), e.BackendCommitP99.LastNAvg(n10, timeseries.NaN)
		if fsync > fsyncThreshold || commit > commitThreshold {
			diskCheck.AddItem("%s", i.Name)
			diskCheck.AddDetail("%s: p99 WAL fsync %s (recommended < %s), p99 backend commit %s (recommended < %s)", i.Name,
				utils.FormatLatency(fsync), utils.FormatLatency(fsyncThreshold), utils.FormatLatency(commit), utils.FormatLatency(commitThreshold))
		}
		sizePercent := e.DbSizePercent().Last()
		if sizePercent > sizeCheck.Threshold {
			sizeCheck.AddItem("%s", i.Name)
			sizeCheck.AddDetail("%s: the database takes %.0f%% of the space quota", i.Name, sizePercent)
		}
		role := model.NewTableCell()
		switch e.IsLeader.Last() {
		case 1:
			role.SetValue("leader").SetIcon("mdi-database-edit-outline", "rgba(0,0,0,0.87)")
		case 0:
			role.SetValue("follower").SetIcon("mdi-database-import-outline", "grey")
		}
		size := bytesCell(e.DbSize.Last())
		if !timeseries.IsNaN(sizePercent) {
			size.AddTag("%.0f%% of quota", sizePercent)
		}
		table.AddRow(
			model.NewTableCell(i.Name).AddTag("version: %s", e.Version.Value()),
			status,
			role,
			size,
			latencyCell(e.WalFsyncP99.Last()),
			latencyCell(e.BackendCommitP99.Last()),
			floatCell(changes, ""),
		)
	}
	diskCheck.AddWidget(report.GetOrCreateChart(fsyncTitle, nil).Widget())
	diskCheck.AddWidget(report.GetOrCreateChart(commitTitle, nil).Widget())
	changesCheck.AddWidget(report.GetOrCreateChart("Leader changes, per second", nil).Widget())
	sizeCheck.AddWidget(report.GetOrCreateChartGroup("Database size <selector>, bytes", nil).Widget())
}

// Managed services

func cloudServiceStatus(c *model.CloudService) *model.TableCell {
	status := model.NewTableCell()
	switch {
	case timeseries.IsNaN(c.LifeSpan.Last()):
		status.SetStatus(model.WARNING, "down (no metrics)")
	case !c.IsUp():
		status.SetStatus(model.WARNING, c.Status.Value())
	default:
		status.SetStatus(model.OK, c.Status.Value())
	}
	return status
}

func (a *appAuditor) cloud() {
	var instances []*model.Instance
	for _, i := range a.app.Instances {
		if i.Cloud != nil || (i.Rds != nil && i.Rds.Aurora != nil) {
			instances = append(instances, i)
		}
	}
	if len(instances) == 0 {
		return
	}
	report := a.addReport(model.AuditReportCloud)
	lagCheck := report.CreateCheck(model.DBExtChecks.CloudReplicaLag)
	capacityCheck := report.CreateCheck(model.DBExtChecks.CloudCapacity)
	table := report.GetOrCreateTable("Instance", "Service", "Status", "Role", "CPU", "Memory", "Storage", "Connections", "Replica lag", "Capacity")
	lagTitle, capacityTitle := "Replica lag, seconds", "Capacity usage <selector>, %"

	sort.SliceStable(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	for _, i := range instances {
		var service, role string
		var lag *timeseries.TimeSeries
		capacityByName := map[string]*timeseries.TimeSeries{}
		var cpu, mem, storage, conns *timeseries.TimeSeries
		status := model.NewTableCell()
		if i.Rds != nil {
			au := i.Rds.Aurora
			service = "Aurora"
			role = au.Role.Value()
			lag = au.ReplicaLag
			if u := au.ServerlessUtilization(); !u.IsEmpty() {
				service = "Aurora Serverless v2"
				capacityByName["ACU"] = u
				report.GetOrCreateChartInGroup("Aurora Serverless capacity <selector>, ACU", i.Name, nil).Group("Capacity", 2).
					AddSeries("capacity", au.ServerlessCapacity).
					SetThreshold("max", au.ServerlessMaxCapacity)
			}
			cpu = i.Node.CpuUsagePercent
			if i.Rds.Status.Value() == "available" {
				status.SetStatus(model.OK, "available")
			} else {
				status.SetStatus(model.WARNING, i.Rds.Status.Value())
			}
		} else {
			c := i.Cloud
			service = c.Service
			role = c.Role.Value()
			lag = c.ReplicaLag
			cpu, mem, conns = c.CpuUsagePercent, c.MemoryUsagePercent, c.Connections
			storage = c.StorageUsage()
			status = cloudServiceStatus(c)
			switch a.app.Id.Kind {
			case model.ApplicationKindElasticacheServerless:
				capacityByName["ECPU"] = c.ECPUUsage()
				capacityByName["storage"] = storage
				report.GetOrCreateChartInGroup("ElastiCache Processing Units <selector>, per second", i.Name, nil).Group("Capacity", 2).
					AddSeries("ECPU", c.ECPU).SetThreshold("limit", c.ECPULimit)
			case model.ApplicationKindAzureRedis:
				capacityByName["memory"] = c.MemoryUsagePercent
				capacityByName["server load"] = c.ServerLoad
			}
			if storage != nil {
				report.GetOrCreateChart("Storage usage, %", nil).Group("Resources", 1).AddSeries(i.Name, storage)
			}
			report.GetOrCreateChart("Memory usage, %", nil).Group("Resources", 1).AddSeries(i.Name, mem)
			report.GetOrCreateChart("Connections", nil).Group("Resources", 1).AddSeries(i.Name, conns)
			report.GetOrCreateChart("IOPS", nil).Group("Resources", 1).AddSeries(i.Name, c.IOPS)
			report.GetOrCreateChart("I/O consumption, % of provisioned IOPS", nil).Group("Resources", 1).AddSeries(i.Name, c.IOConsumption)
		}
		report.GetOrCreateChart("CPU usage, %", nil).Group("Resources", 1).AddSeries(i.Name, cpu)
		report.GetOrCreateChart(lagTitle, nil).Group("Replication", 3).AddSeries(i.Name, lag)

		capChart := report.GetOrCreateChartInGroup(capacityTitle, i.Name, nil).Group("Capacity", 2)
		var maxCapacity float32 = timeseries.NaN
		for _, name := range sortedKeys(capacityByName) {
			ts := capacityByName[name]
			if ts.IsEmpty() {
				continue
			}
			capChart.AddSeries(name, ts)
			if v := ts.Last(); !timeseries.IsNaN(v) {
				if timeseries.IsNaN(maxCapacity) || v > maxCapacity {
					maxCapacity = v
				}
				if v > capacityCheck.Threshold && !i.IsObsolete() {
					capacityCheck.AddItem("%s", i.Name)
					capacityCheck.AddDetail("%s: %s usage is %.0f%% of the limit", i.Name, name, v)
				}
			}
		}
		for _, ts := range capacityByName {
			if !ts.IsEmpty() {
				capChart.SetThreshold("threshold", ts.WithNewValue(capacityCheck.Threshold))
				break
			}
		}

		lagValue := lag.Last()
		if !timeseries.IsNaN(lagValue) && lagValue > lagCheck.Threshold && !i.IsObsolete() {
			lagCheck.AddItem("%s", i.Name)
			lagCheck.AddDetail("%s: the replica is %s behind the primary", i.Name, utils.FormatDuration(timeseries.Duration(lagValue), 1))
		}
		lagCell := model.NewTableCell()
		if !timeseries.IsNaN(lagValue) {
			lagCell.SetValue(utils.FormatLatency(lagValue))
		}
		table.AddRow(
			model.NewTableCell(i.Name),
			model.NewTableCell(service),
			status,
			model.NewTableCell(role),
			percentCell(cpu.Last()),
			percentCell(mem.Last()),
			percentCell(storage.Last()),
			floatCell(conns.Last(), ""),
			lagCell,
			percentCell(maxCapacity),
		)
	}
	lagCheck.AddWidget(report.GetOrCreateChart(lagTitle, nil).Widget())
	capacityCheck.AddWidget(report.GetOrCreateChartGroup(capacityTitle, nil).Widget())

	a.escalateCloudStorage(instances)
}

// escalateCloudStorage makes the disk space check of the Storage report critical when a managed database volume
// is over cloudStorageCritical percent full (the check itself warns at its threshold, 80% by default).
func (a *appAuditor) escalateCloudStorage(instances []*model.Instance) {
	for _, r := range a.reports {
		if r.Name != model.AuditReportStorage {
			continue
		}
		for _, ch := range r.Checks {
			if ch.Id != model.Checks.StorageSpace.Id {
				continue
			}
			for _, i := range instances {
				if i.Cloud == nil {
					continue
				}
				if u := i.Cloud.StorageUsage().Last(); u > cloudStorageCritical && u > ch.Threshold {
					ch.Escalate()
				}
			}
		}
	}
}

package auditor

import (
	"fmt"
	"sort"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: the Elasticsearch/OpenSearch report built from the metrics of the shards cluster agent.

const (
	esRecentWindow    = 5 * timeseries.Minute
	esFloodStageUsage = 95 // cluster.routing.allocation.disk.watermark.flood_stage default: the indices become read-only
)

type esCluster struct {
	name string
	es   *model.Elasticsearch
}

type esNode struct {
	key  model.ElasticsearchNodeKey
	node *model.ElasticsearchNode
}

// esClusters returns the cluster-level data once per cluster: every target of a cluster reports the cluster health.
func esClusters(instances []*model.Instance) ([]esCluster, []esNode, map[string]*model.ElasticsearchIndex) {
	clusters := map[string]esCluster{}
	nodes := map[model.ElasticsearchNodeKey]*model.ElasticsearchNode{}
	indices := map[string]*model.ElasticsearchIndex{}
	sorted := append([]*model.Instance{}, instances...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, i := range sorted {
		es := i.Elasticsearch
		if es == nil || i.IsObsolete() {
			continue
		}
		name := es.Cluster.Value()
		if name == "" {
			name = i.Name
		}
		if _, ok := clusters[name]; !ok && es.Health.Status.Value() != "" {
			clusters[name] = esCluster{name: name, es: es}
		}
		for k, n := range es.Nodes {
			if nodes[k] == nil {
				nodes[k] = n
			}
		}
		for name, idx := range es.Indices {
			if len(clusters) > 1 && es.Cluster.Value() != "" {
				name = es.Cluster.Value() + "/" + name
			}
			if indices[name] == nil {
				indices[name] = idx
			}
		}
	}
	var cs []esCluster
	for _, c := range clusters {
		cs = append(cs, c)
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	var ns []esNode
	for k, n := range nodes {
		ns = append(ns, esNode{key: k, node: n})
	}
	sort.Slice(ns, func(i, j int) bool {
		if ns[i].key.Cluster != ns[j].key.Cluster {
			return ns[i].key.Cluster < ns[j].key.Cluster
		}
		return ns[i].key.Name < ns[j].key.Name
	})
	return cs, ns, indices
}

func esHealthCell(color string) *model.TableCell {
	c := model.NewTableCell()
	switch color {
	case "green":
		c.SetStatus(model.OK, color)
	case "yellow":
		c.SetStatus(model.WARNING, color)
	case "red":
		c.SetStatus(model.CRITICAL, color)
	default:
		c.SetValue(color)
	}
	return c
}

func (a *appAuditor) elasticsearch() {
	types := a.app.ApplicationTypes()
	isES := types[model.ApplicationTypeElasticsearch] || types[model.ApplicationTypeOpensearch]
	if !isES && !a.app.IsElasticsearch() {
		return
	}
	report := a.addReport(model.AuditReportElasticsearch)
	report.Instrumentation = model.ApplicationTypeElasticsearch
	if types[model.ApplicationTypeOpensearch] {
		report.Instrumentation = model.ApplicationTypeOpensearch
	}
	if !a.app.IsElasticsearch() {
		report.Status = model.UNKNOWN
		return
	}

	availabilityCheck := report.CreateCheck(model.Checks.ElasticsearchAvailability)
	healthCheck := report.CreateCheck(model.Checks.ElasticsearchClusterHealth)
	unassignedCheck := report.CreateCheck(model.Checks.ElasticsearchUnassignedShards)
	heapCheck := report.CreateCheck(model.Checks.ElasticsearchJvmHeap)
	diskCheck := report.CreateCheck(model.Checks.ElasticsearchDiskSpace)
	rejectsCheck := report.CreateCheck(model.Checks.ElasticsearchThreadPoolRejects)

	instancesTable := report.GetOrCreateTable("Instance", "Status", "Cluster", "Version")
	clustersTable := report.GetOrCreateTable("Cluster", "Health", "Nodes", "Data nodes", "Active shards", "Relocating", "Initializing", "Unassigned", "Pending tasks").Group("Cluster", 1)
	nodesTable := report.GetOrCreateTable("Node", "Heap", "Disk", "CPU", "Indexing", "Search", "Search latency", "Rejections").Group("Nodes", 2)
	indicesTable := report.GetOrCreateTable("Index", "Health", "Docs", "Size", "Primary size", "Shards").Group("Indices", 4)

	shardsChart := report.GetOrCreateChartGroup("Shards <selector>", nil).Group("Cluster", 1)
	pendingChart := report.GetOrCreateChart("Pending cluster tasks", nil).Group("Cluster", 1)
	heapChart := report.GetOrCreateChart("JVM heap usage, %", nil).Group("Nodes", 2)
	gcChart := report.GetOrCreateChart("GC time, seconds/second", nil).Group("Nodes", 2)
	diskChart := report.GetOrCreateChart("Data disk usage, %", nil).Group("Nodes", 2)
	cpuChart := report.GetOrCreateChart("CPU usage, %", nil).Group("Nodes", 2)
	indexingChart := report.GetOrCreateChart("Indexing rate, documents/second", nil).Group("Indexing and search", 3)
	indexingLatencyChart := report.GetOrCreateChart("Indexing latency, seconds", nil).Group("Indexing and search", 3)
	searchChart := report.GetOrCreateChart("Search rate, queries/second", nil).Group("Indexing and search", 3)
	searchLatencyChart := report.GetOrCreateChart("Search latency (query phase), seconds", nil).Group("Indexing and search", 3)
	rejectsChart := report.GetOrCreateChartGroup("Thread pool rejections <selector>, per second", nil).Group("Thread pools", 5)
	queueChart := report.GetOrCreateChartGroup("Thread pool queues <selector>", nil).Group("Thread pools", 5)
	breakersChart := report.GetOrCreateChartGroup("Circuit breakers tripped <selector>, per second", nil).Group("Thread pools", 5)
	indexSizeChart := report.GetOrCreateChart("Top indices by size, bytes", nil).Group("Indices", 4).Stacked().Sorted()

	availabilityCheck.AddWidget(instancesTable.Widget())
	healthCheck.AddWidget(clustersTable.Widget())
	healthCheck.AddWidget(indicesTable.Widget())
	unassignedCheck.AddWidget(shardsChart.Widget())
	heapCheck.AddWidget(heapChart.Widget())
	heapCheck.AddWidget(gcChart.Widget())
	diskCheck.AddWidget(diskChart.Widget())
	rejectsCheck.AddWidget(rejectsChart.Widget())

	for _, i := range a.app.Instances {
		es := i.Elasticsearch
		if es == nil || i.IsObsolete() {
			continue
		}
		up := es.IsUp()
		if !up {
			availabilityCheck.AddItem("%s", i.Name)
			if e := es.Error.Value(); e != "" {
				availabilityCheck.AddDetail("%s: %s", i.Name, e)
			}
		}
		version := es.Version.Value()
		if d := es.Distribution.Value(); d != "" && version != "" {
			version = d + " " + version
		}
		instancesTable.AddRow(
			model.NewTableCell(i.Name),
			targetStatusCell(up, es.Error.Value(), es.Warning.Value()),
			model.NewTableCell(es.Cluster.Value()),
			model.NewTableCell(version),
		)
	}

	recent := recentPoints(a.w.Ctx, esRecentWindow)
	clusters, nodes, indices := esClusters(a.app.Instances)
	for _, c := range clusters {
		h := c.es.Health
		color := h.Status.Value()
		switch color {
		case "yellow":
			healthCheck.AddItem("%s", c.name)
			healthCheck.AddDetail("%s: yellow - some replica shards are unassigned (%.0f unassigned shards)", c.name, max(lastValue(h.UnassignedShards), 0))
		case "red":
			healthCheck.AddItem("%s", c.name)
			healthCheck.AddDetail("%s: red - some primary shards are unassigned, their data is unavailable (%.0f unassigned shards)", c.name, max(lastValue(h.UnassignedShards), 0))
			healthCheck.SetCritical()
		}
		notDelayed := timeseries.Aggregate2(h.UnassignedShards, h.DelayedUnassignedShards.Map(timeseries.NanToZero), func(u, d float32) float32 { return u - d })
		if notDelayed.IsEmpty() {
			notDelayed = h.UnassignedShards
		}
		if len(tail(notDelayed, recent)) >= recent && sustained(notDelayed, recent, func(v float32) bool { return v > unassignedCheck.Threshold }) {
			unassignedCheck.AddItem("%s", c.name)
			unassignedCheck.AddDetail("%s: %.0f unassigned shards", c.name, lastValue(notDelayed))
		}
		clustersTable.AddRow(
			model.NewTableCell(c.name),
			esHealthCell(color),
			floatCell(lastValue(h.Nodes), ""),
			floatCell(lastValue(h.DataNodes), ""),
			floatCell(lastValue(h.ActiveShards), "").AddTag("primary: %s", utils.FormatFloat(lastValue(h.ActivePrimaryShards))),
			floatCell(lastValue(h.RelocatingShards), ""),
			floatCell(lastValue(h.InitializingShards), ""),
			floatCell(lastValue(h.UnassignedShards), ""),
			floatCell(lastValue(h.PendingTasks), ""),
		)
		if shardsChart != nil {
			shardsChart.GetOrCreateChart(c.name).Stacked().
				AddSeries("active", h.ActiveShards, "green").
				AddSeries("relocating", h.RelocatingShards, "blue").
				AddSeries("initializing", h.InitializingShards, "orange").
				AddSeries("unassigned", h.UnassignedShards, "red")
		}
		pendingChart.AddSeries(c.name, h.PendingTasks)
	}

	multiCluster := len(clusters) > 1
	var maxHeap, maxDisk float32
	for _, n := range nodes {
		name := n.key.Name
		if multiCluster {
			name = n.key.Cluster + "/" + name
		}
		node := n.node

		heap := node.HeapUsedPercent()
		heapChart.AddSeries(name, heap)
		heapCell := model.NewTableCell()
		if v := lastValue(heap); !timeseries.IsNaN(v) {
			heapCell.SetValue(fmt.Sprintf("%.0f%%", v)).SetProgress(int(v), "blue")
		}
		if len(tail(heap, recent)) >= recent && sustained(heap, recent, func(v float32) bool { return v > heapCheck.Threshold }) {
			heapCheck.AddItem("%s", name)
			v := recentAvg(heap, recent)
			heapCheck.AddDetail("%s: %.0f%% of the heap used on average for %s", name, v, esRecentWindow)
			maxHeap = max(maxHeap, v)
			heapCell.SetStatus(model.WARNING, fmt.Sprintf("%.0f%%", lastValue(heap)))
		}
		gc := timeseries.NewAggregate(timeseries.NanSum)
		for _, ts := range node.GcTime {
			gc.Add(ts)
		}
		gcChart.AddSeries(name, gc.Get())

		diskCell := model.NewTableCell()
		var nodeDisk float32
		for path, fs := range node.Fs {
			used := fs.UsedPercent()
			diskChart.AddSeries(name+":"+path, used)
			v := lastValue(used)
			if timeseries.IsNaN(v) {
				continue
			}
			nodeDisk = max(nodeDisk, v)
			if v > diskCheck.Threshold {
				diskCheck.AddItem("%s:%s", name, path)
				diskCheck.AddDetail("%s:%s: %.0f%% used, %s available", name, path, v, formatBytes(lastValue(fs.AvailableBytes)))
				maxDisk = max(maxDisk, v)
				if v > esFloodStageUsage {
					diskCheck.SetCritical()
				}
			}
		}
		if nodeDisk > 0 {
			diskCell.SetValue(fmt.Sprintf("%.0f%%", nodeDisk)).SetProgress(int(nodeDisk), "blue")
			if nodeDisk > diskCheck.Threshold {
				diskCell.SetStatus(model.WARNING, fmt.Sprintf("%.0f%%", nodeDisk))
			}
		}

		var rejected float32
		rejects := map[string]model.SeriesData{}
		for pool, ts := range node.ThreadPoolRejects {
			rejects[pool] = ts
			if r := recentSum(a.w.Ctx, ts, recent); r >= 1 {
				rejected += r
				rejectsCheck.AddDetail("%s: %.0f tasks rejected by the %s thread pool", name, r, pool)
			}
		}
		rejectsCheck.Inc(int64(rejected))
		rejectsCell := model.NewTableCell()
		if rejected >= 1 {
			rejectsCell.SetStatus(model.WARNING, fmt.Sprintf("%.0f in %s", rejected, esRecentWindow))
		}
		if rejectsChart != nil {
			rejectsChart.GetOrCreateChart(name).Stacked().AddMany(rejects, 10, timeseries.NanSum)
		}
		if queueChart != nil {
			queues := map[string]model.SeriesData{}
			for pool, ts := range node.ThreadPoolQueue {
				queues[pool] = ts
			}
			queueChart.GetOrCreateChart(name).Stacked().AddMany(queues, 10, timeseries.Max)
		}
		if breakersChart != nil {
			breakers := map[string]model.SeriesData{}
			for b, ts := range node.BreakersTripped {
				breakers[b] = ts
			}
			breakersChart.GetOrCreateChart(name).Stacked().AddMany(breakers, 10, timeseries.NanSum)
		}
		cpuChart.AddSeries(name, node.CpuPercent)
		indexingChart.AddSeries(name, node.IndexingRate)
		indexingLatencyChart.AddSeries(name, node.IndexingLatency())
		searchChart.AddSeries(name, node.SearchQueryRate)
		searchLatencyChart.AddSeries(name, node.SearchLatency())

		cpuCell := model.NewTableCell()
		if v := lastValue(node.CpuPercent); !timeseries.IsNaN(v) {
			cpuCell.SetValue(fmt.Sprintf("%.0f%%", v))
		}
		latencyCell := model.NewTableCell()
		if v := recentAvg(node.SearchLatency(), recent); !timeseries.IsNaN(v) {
			latencyCell.SetValue(utils.FormatLatency(v))
		}
		nodeCell := model.NewTableCell(name)
		if node.Host != "" {
			nodeCell.AddTag("host: %s", node.Host)
		}
		nodesTable.AddRow(
			nodeCell,
			heapCell,
			diskCell,
			cpuCell,
			floatCell(recentAvg(node.IndexingRate, recent), "docs/s"),
			floatCell(recentAvg(node.SearchQueryRate, recent), "q/s"),
			latencyCell,
			rejectsCell,
		)
	}
	if maxHeap > 0 {
		heapCheck.SetValue(maxHeap)
	}
	if maxDisk > 0 {
		diskCheck.SetValue(maxDisk)
	}
	if heapChart != nil {
		for _, n := range nodes {
			if h := n.node.HeapUsedPercent(); !h.IsEmpty() {
				heapChart.SetThreshold("threshold", h.WithNewValue(heapCheck.Threshold))
				break
			}
		}
	}

	sizes := map[string]model.SeriesData{}
	for name, idx := range indices {
		sizes[name] = idx.SizeTotal
		shards := model.NewTableCell()
		if p, r := lastValue(idx.PrimaryShards), lastValue(idx.Replicas); !timeseries.IsNaN(p) {
			if timeseries.IsNaN(r) {
				r = 0
			}
			shards.SetValue(fmt.Sprintf("%.0f primary × %.0f", p, r+1)).AddTag("replicas: %.0f", r)
		}
		indicesTable.AddRow(
			model.NewTableCell(name),
			esHealthCell(idx.Health.Value()),
			floatCell(lastValue(idx.DocsPrimary), ""),
			bytesCell(lastValue(idx.SizeTotal)),
			bytesCell(lastValue(idx.SizePrimary)),
			shards,
		)
	}
	indexSizeChart.AddMany(sizes, 10, timeseries.Max)
}

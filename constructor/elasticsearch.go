package constructor

import (
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
)

// Shards fork: Elasticsearch/OpenSearch metrics of the shards cluster agent.
func elasticsearch(instance *model.Instance, queryName string, m *model.MetricValues) {
	if instance == nil {
		return
	}
	if instance.Elasticsearch == nil {
		instance.Elasticsearch = model.NewElasticsearch()
	}
	es := instance.Elasticsearch
	ls := m.Labels
	v := m.Values
	set := func(dst **timeseries.TimeSeries) {
		*dst = merge(*dst, v, timeseries.Any)
	}
	setIn := func(dst map[string]*timeseries.TimeSeries, k string) {
		dst[k] = merge(dst[k], v, timeseries.Any)
	}
	if c := ls["cluster"]; c != "" {
		es.Cluster.Update(v, c)
	}
	node := func() *model.ElasticsearchNode {
		n := es.GetOrCreateNode(model.ElasticsearchNodeKey{Cluster: ls["cluster"], Name: ls["name"]})
		if h := ls["host"]; h != "" {
			n.Host = h
		}
		return n
	}
	index := func() *model.ElasticsearchIndex {
		return es.GetOrCreateIndex(ls["index"])
	}
	h := &es.Health
	switch queryName {
	case qESUp:
		set(&es.Up)
	case qESScrapeError:
		es.Error.Update(v, model.HumanizeScrapeError(ls["error"]))
		es.Warning.Update(v, model.HumanizeScrapeError(ls["warning"]))
	case qESVersionInfo:
		es.Version.Update(v, ls["version"])
		es.Distribution.Update(v, ls["distribution"])

	case qESHealthStatus:
		h.Status.Update(v, ls["color"])
	case qESHealthNodes:
		set(&h.Nodes)
	case qESHealthDataNodes:
		set(&h.DataNodes)
	case qESHealthActivePrimary:
		set(&h.ActivePrimaryShards)
	case qESHealthActiveShards:
		set(&h.ActiveShards)
	case qESHealthRelocating:
		set(&h.RelocatingShards)
	case qESHealthInitializing:
		set(&h.InitializingShards)
	case qESHealthUnassigned:
		set(&h.UnassignedShards)
	case qESHealthDelayedUnassigned:
		set(&h.DelayedUnassignedShards)
	case qESHealthPendingTasks:
		set(&h.PendingTasks)
	case qESHealthTaskMaxWaiting:
		h.TaskMaxWaitingSeconds = merge(h.TaskMaxWaitingSeconds, v.Map(func(t timeseries.Time, x float32) float32 { return x / 1000 }), timeseries.Any)

	case qESHeapUsed:
		set(&node().HeapUsed)
	case qESHeapMax:
		set(&node().HeapMax)
	case qESGcTime:
		setIn(node().GcTime, ls["gc"])
	case qESFsAvailable, qESFsSize:
		n := node()
		fs := n.Fs[ls["path"]]
		if fs == nil {
			fs = &model.ElasticsearchFs{}
			n.Fs[ls["path"]] = fs
		}
		if queryName == qESFsAvailable {
			set(&fs.AvailableBytes)
		} else {
			set(&fs.SizeBytes)
		}
	case qESNodeDocs:
		set(&node().Docs)
	case qESNodeStoreSize:
		set(&node().StoreSize)
	case qESIndexingRate:
		set(&node().IndexingRate)
	case qESIndexingTime:
		set(&node().IndexingTime)
	case qESIndexingFailed:
		set(&node().IndexingFailed)
	case qESSearchQueryRate:
		set(&node().SearchQueryRate)
	case qESSearchQueryTime:
		set(&node().SearchQueryTime)
	case qESThreadPoolRejects:
		setIn(node().ThreadPoolRejects, ls["type"])
	case qESThreadPoolQueue:
		setIn(node().ThreadPoolQueue, ls["type"])
	case qESBreakersTripped:
		setIn(node().BreakersTripped, ls["breaker"])
	case qESProcessCpu:
		set(&node().CpuPercent)

	case qESIndexHealth:
		index().Health.Update(v, ls["color"])
	case qESIndexDocsPrimary:
		set(&index().DocsPrimary)
	case qESIndexSizePrimary:
		set(&index().SizePrimary)
	case qESIndexSizeTotal:
		set(&index().SizeTotal)
	case qESIndexShardsPrimary:
		set(&index().PrimaryShards)
	case qESIndexReplicas:
		set(&index().Replicas)
	}
}

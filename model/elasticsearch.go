package model

import (
	"github.com/coroot/coroot/timeseries"
)

// Shards fork: Elasticsearch/OpenSearch metrics collected by the shards cluster agent (elasticsearch_*).

type ElasticsearchNodeKey struct {
	Cluster string
	Name    string
}

type ElasticsearchFs struct {
	AvailableBytes *timeseries.TimeSeries
	SizeBytes      *timeseries.TimeSeries
}

// UsedPercent is the share of the space that isn't available to Elasticsearch, in percent
// (the disk-based shard allocation watermarks are compared against the same value).
func (fs *ElasticsearchFs) UsedPercent() *timeseries.TimeSeries {
	return timeseries.Aggregate2(fs.SizeBytes, fs.AvailableBytes, func(size, avail float32) float32 {
		if size <= 0 || timeseries.IsNaN(size) || timeseries.IsNaN(avail) {
			return timeseries.NaN
		}
		return (size - avail) / size * 100
	})
}

type ElasticsearchNode struct {
	Host string

	HeapUsed *timeseries.TimeSeries
	HeapMax  *timeseries.TimeSeries
	GcTime   map[string]*timeseries.TimeSeries // seconds/second by collector

	Fs map[string]*ElasticsearchFs // by path

	Docs      *timeseries.TimeSeries
	StoreSize *timeseries.TimeSeries

	// rates, per second
	IndexingRate      *timeseries.TimeSeries
	IndexingTime      *timeseries.TimeSeries // seconds/second
	IndexingFailed    *timeseries.TimeSeries
	SearchQueryRate   *timeseries.TimeSeries
	SearchQueryTime   *timeseries.TimeSeries            // seconds/second
	ThreadPoolRejects map[string]*timeseries.TimeSeries // by pool
	BreakersTripped   map[string]*timeseries.TimeSeries // by breaker

	ThreadPoolQueue map[string]*timeseries.TimeSeries // by pool
	CpuPercent      *timeseries.TimeSeries
}

func NewElasticsearchNode() *ElasticsearchNode {
	return &ElasticsearchNode{
		GcTime:            map[string]*timeseries.TimeSeries{},
		Fs:                map[string]*ElasticsearchFs{},
		ThreadPoolRejects: map[string]*timeseries.TimeSeries{},
		BreakersTripped:   map[string]*timeseries.TimeSeries{},
		ThreadPoolQueue:   map[string]*timeseries.TimeSeries{},
	}
}

func (n *ElasticsearchNode) HeapUsedPercent() *timeseries.TimeSeries {
	return timeseries.Aggregate2(n.HeapUsed, n.HeapMax, func(used, max float32) float32 {
		if max <= 0 || timeseries.IsNaN(max) || timeseries.IsNaN(used) {
			return timeseries.NaN
		}
		return used / max * 100
	})
}

func latency(time, count *timeseries.TimeSeries) *timeseries.TimeSeries {
	return timeseries.Aggregate2(time, count, func(t, c float32) float32 {
		if c <= 0 || timeseries.IsNaN(c) || timeseries.IsNaN(t) {
			return timeseries.NaN
		}
		return t / c
	})
}

// IndexingLatency is the average time of an index operation, in seconds.
func (n *ElasticsearchNode) IndexingLatency() *timeseries.TimeSeries {
	return latency(n.IndexingTime, n.IndexingRate)
}

// SearchLatency is the average time of the query phase of a search, in seconds.
func (n *ElasticsearchNode) SearchLatency() *timeseries.TimeSeries {
	return latency(n.SearchQueryTime, n.SearchQueryRate)
}

type ElasticsearchIndex struct {
	Health        LabelLastValue
	DocsPrimary   *timeseries.TimeSeries
	SizePrimary   *timeseries.TimeSeries
	SizeTotal     *timeseries.TimeSeries
	PrimaryShards *timeseries.TimeSeries
	Replicas      *timeseries.TimeSeries
}

type ElasticsearchHealth struct {
	Status LabelLastValue // green, yellow, red

	Nodes                   *timeseries.TimeSeries
	DataNodes               *timeseries.TimeSeries
	ActivePrimaryShards     *timeseries.TimeSeries
	ActiveShards            *timeseries.TimeSeries
	RelocatingShards        *timeseries.TimeSeries
	InitializingShards      *timeseries.TimeSeries
	UnassignedShards        *timeseries.TimeSeries
	DelayedUnassignedShards *timeseries.TimeSeries
	PendingTasks            *timeseries.TimeSeries
	TaskMaxWaitingSeconds   *timeseries.TimeSeries
}

type Elasticsearch struct {
	Up           *timeseries.TimeSeries
	Error        LabelLastValue
	Warning      LabelLastValue
	Cluster      LabelLastValue
	Version      LabelLastValue
	Distribution LabelLastValue // elasticsearch or opensearch

	Health  ElasticsearchHealth
	Nodes   map[ElasticsearchNodeKey]*ElasticsearchNode
	Indices map[string]*ElasticsearchIndex
}

func NewElasticsearch() *Elasticsearch {
	return &Elasticsearch{
		Nodes:   map[ElasticsearchNodeKey]*ElasticsearchNode{},
		Indices: map[string]*ElasticsearchIndex{},
	}
}

func (es *Elasticsearch) IsUp() bool {
	return es.Up.Last() > 0
}

func (es *Elasticsearch) ApplicationType() ApplicationType {
	if es.Distribution.Value() == "opensearch" {
		return ApplicationTypeOpensearch
	}
	return ApplicationTypeElasticsearch
}

func (es *Elasticsearch) GetOrCreateNode(k ElasticsearchNodeKey) *ElasticsearchNode {
	n := es.Nodes[k]
	if n == nil {
		n = NewElasticsearchNode()
		es.Nodes[k] = n
	}
	return n
}

func (es *Elasticsearch) GetOrCreateIndex(name string) *ElasticsearchIndex {
	i := es.Indices[name]
	if i == nil {
		i = &ElasticsearchIndex{}
		es.Indices[name] = i
	}
	return i
}

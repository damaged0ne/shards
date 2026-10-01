package model

import (
	"fmt"

	"github.com/coroot/coroot/timeseries"
)

// Shards fork: ClickHouse server metrics collected by the shards cluster agent (clickhouse_*).

type ClickHouseQueryKey struct {
	Db    string
	Query string
}

func (k ClickHouseQueryKey) String() string {
	if k.Db == "" {
		return k.Query
	}
	return fmt.Sprintf("%s: %s", k.Db, k.Query)
}

type ClickHouseQueryStat struct {
	Calls     *timeseries.TimeSeries // per second
	TotalTime *timeseries.TimeSeries // seconds/second
	ReadRows  *timeseries.TimeSeries // per second
	ReadBytes *timeseries.TimeSeries // per second
	Errors    *timeseries.TimeSeries // per second
}

type ClickHouseTable struct {
	Parts                *timeseries.TimeSeries
	SizeBytes            *timeseries.TimeSeries
	MaxPartsPerPartition *timeseries.TimeSeries

	ReplicaReadonly       *timeseries.TimeSeries
	ReplicaSessionExpired *timeseries.TimeSeries
	ReplicaAbsoluteDelay  *timeseries.TimeSeries
	ReplicaQueueSize      *timeseries.TimeSeries

	MutationsInProgress *timeseries.TimeSeries
	MutationsFailing    *timeseries.TimeSeries
	MutationsStuck      *timeseries.TimeSeries
}

type ClickHouse struct {
	Up      *timeseries.TimeSeries
	Error   LabelLastValue
	Warning LabelLastValue
	Version LabelLastValue

	Uptime *timeseries.TimeSeries

	QueriesRunning       *timeseries.TimeSeries
	MergesRunning        *timeseries.TimeSeries
	MutationsRunning     *timeseries.TimeSeries
	ReplFetchesRunning   *timeseries.TimeSeries
	Connections          map[string]*timeseries.TimeSeries // by protocol
	MemoryTracking       *timeseries.TimeSeries
	MemoryResident       *timeseries.TimeSeries
	OSMemoryTotal        *timeseries.TimeSeries
	OSMemoryAvailable    *timeseries.TimeSeries
	InsertsDelayedNow    *timeseries.TimeSeries
	ReadonlyReplicas     *timeseries.TimeSeries
	ZooKeeperSessions    *timeseries.TimeSeries
	ZooKeeperRequests    *timeseries.TimeSeries
	DistributedFiles     *timeseries.TimeSeries
	MergeTreeParts       *timeseries.TimeSeries
	MergeTreeBytes       *timeseries.TimeSeries
	MaxPartsPerPartition *timeseries.TimeSeries

	ReplicasMaxDelay     *timeseries.TimeSeries
	ReplicasMaxQueueSize *timeseries.TimeSeries
	ReplicasSumQueueSize *timeseries.TimeSeries

	// rates, per second
	Queries               map[string]*timeseries.TimeSeries // by kind: all, select, insert
	FailedQueries         map[string]*timeseries.TimeSeries // by kind
	QueryTime             *timeseries.TimeSeries            // seconds/second
	InsertedRows          *timeseries.TimeSeries
	InsertedBytes         *timeseries.TimeSeries
	SelectedRows          *timeseries.TimeSeries
	SelectedBytes         *timeseries.TimeSeries
	Merges                *timeseries.TimeSeries
	DelayedInserts        *timeseries.TimeSeries
	RejectedInserts       *timeseries.TimeSeries
	ZooKeeperExceptions   map[string]*timeseries.TimeSeries // by type
	ReplFailedFetches     *timeseries.TimeSeries
	ReplDataLoss          *timeseries.TimeSeries
	DistributedConnFails  *timeseries.TimeSeries
	QueryMemLimitExceeded *timeseries.TimeSeries
	Errors                map[string]*timeseries.TimeSeries // by error name

	Tables   map[DbTableKey]*ClickHouseTable
	PerQuery map[ClickHouseQueryKey]*ClickHouseQueryStat
}

func NewClickHouse() *ClickHouse {
	return &ClickHouse{
		Connections:         map[string]*timeseries.TimeSeries{},
		Queries:             map[string]*timeseries.TimeSeries{},
		FailedQueries:       map[string]*timeseries.TimeSeries{},
		ZooKeeperExceptions: map[string]*timeseries.TimeSeries{},
		Errors:              map[string]*timeseries.TimeSeries{},
		Tables:              map[DbTableKey]*ClickHouseTable{},
		PerQuery:            map[ClickHouseQueryKey]*ClickHouseQueryStat{},
	}
}

func (c *ClickHouse) IsUp() bool {
	return c.Up.Last() > 0
}

func (c *ClickHouse) GetOrCreateTable(k DbTableKey) *ClickHouseTable {
	t := c.Tables[k]
	if t == nil {
		t = &ClickHouseTable{}
		c.Tables[k] = t
	}
	return t
}

func (c *ClickHouse) GetOrCreateQuery(k ClickHouseQueryKey) *ClickHouseQueryStat {
	q := c.PerQuery[k]
	if q == nil {
		q = &ClickHouseQueryStat{}
		c.PerQuery[k] = q
	}
	return q
}

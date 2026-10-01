package constructor

import (
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
)

// Shards fork: ClickHouse metrics of the shards cluster agent.
func clickhouse(instance *model.Instance, queryName string, m *model.MetricValues) {
	if instance == nil {
		return
	}
	if instance.ClickHouse == nil {
		instance.ClickHouse = model.NewClickHouse()
	}
	ch := instance.ClickHouse
	ls := m.Labels
	v := m.Values
	set := func(dst **timeseries.TimeSeries) {
		*dst = merge(*dst, v, timeseries.Any)
	}
	setIn := func(dst map[string]*timeseries.TimeSeries, k string) {
		dst[k] = merge(dst[k], v, timeseries.Any)
	}
	table := func() *model.ClickHouseTable {
		return ch.GetOrCreateTable(model.DbTableKey{Db: ls["db"], Table: ls["table"]})
	}
	query := func() *model.ClickHouseQueryStat {
		return ch.GetOrCreateQuery(model.ClickHouseQueryKey{Db: ls["db"], Query: ls["query"]})
	}
	switch queryName {
	case qClickHouseUp:
		set(&ch.Up)
	case qClickHouseScrapeError:
		ch.Error.Update(v, model.HumanizeScrapeError(ls["error"]))
		ch.Warning.Update(v, model.HumanizeScrapeError(ls["warning"]))
	case qClickHouseInfo:
		ch.Version.Update(v, ls["server_version"])

	case qClickHouseQueriesRunning:
		set(&ch.QueriesRunning)
	case qClickHouseMergesRunning:
		set(&ch.MergesRunning)
	case qClickHouseMutationsRunning:
		set(&ch.MutationsRunning)
	case qClickHouseFetchesRunning:
		set(&ch.ReplFetchesRunning)
	case qClickHouseConnections:
		setIn(ch.Connections, ls["protocol"])
	case qClickHouseMemoryTracking:
		set(&ch.MemoryTracking)
	case qClickHouseInsertsDelayed:
		set(&ch.InsertsDelayedNow)
	case qClickHouseReadonlyReplicas:
		set(&ch.ReadonlyReplicas)
	case qClickHouseZooKeeperSessions:
		set(&ch.ZooKeeperSessions)
	case qClickHouseZooKeeperRequests:
		set(&ch.ZooKeeperRequests)
	case qClickHouseDistributedFiles:
		set(&ch.DistributedFiles)

	case qClickHouseUptime:
		set(&ch.Uptime)
	case qClickHouseMaxPartsPerPtn:
		set(&ch.MaxPartsPerPartition)
	case qClickHouseReplicasMaxDelay:
		set(&ch.ReplicasMaxDelay)
	case qClickHouseReplicasMaxQueue:
		set(&ch.ReplicasMaxQueueSize)
	case qClickHouseReplicasSumQueue:
		set(&ch.ReplicasSumQueueSize)
	case qClickHouseMergeTreeParts:
		set(&ch.MergeTreeParts)
	case qClickHouseMergeTreeBytes:
		set(&ch.MergeTreeBytes)
	case qClickHouseMemoryResident:
		set(&ch.MemoryResident)
	case qClickHouseOSMemoryTotal:
		set(&ch.OSMemoryTotal)
	case qClickHouseOSMemoryAvailable:
		set(&ch.OSMemoryAvailable)

	case qClickHouseQueries:
		setIn(ch.Queries, ls["kind"])
	case qClickHouseFailedQueries:
		setIn(ch.FailedQueries, ls["kind"])
	case qClickHouseQueryTime:
		set(&ch.QueryTime)
	case qClickHouseInsertedRows:
		set(&ch.InsertedRows)
	case qClickHouseInsertedBytes:
		set(&ch.InsertedBytes)
	case qClickHouseSelectedRows:
		set(&ch.SelectedRows)
	case qClickHouseSelectedBytes:
		set(&ch.SelectedBytes)
	case qClickHouseMerges:
		set(&ch.Merges)
	case qClickHouseDelayedInserts:
		set(&ch.DelayedInserts)
	case qClickHouseRejectedInserts:
		set(&ch.RejectedInserts)
	case qClickHouseZooKeeperExc:
		setIn(ch.ZooKeeperExceptions, ls["type"])
	case qClickHouseFailedFetches:
		set(&ch.ReplFailedFetches)
	case qClickHouseDataLoss:
		set(&ch.ReplDataLoss)
	case qClickHouseDistributedConnErr:
		set(&ch.DistributedConnFails)
	case qClickHouseMemLimitExceeded:
		set(&ch.QueryMemLimitExceeded)
	case qClickHouseErrors:
		setIn(ch.Errors, ls["name"])

	case qClickHouseTableParts:
		set(&table().Parts)
	case qClickHouseTableSize:
		set(&table().SizeBytes)
	case qClickHouseTableMaxPartsPerPtn:
		set(&table().MaxPartsPerPartition)
	case qClickHouseReplicaReadonly:
		set(&table().ReplicaReadonly)
	case qClickHouseReplicaExpired:
		set(&table().ReplicaSessionExpired)
	case qClickHouseReplicaDelay:
		set(&table().ReplicaAbsoluteDelay)
	case qClickHouseReplicaQueue:
		set(&table().ReplicaQueueSize)
	case qClickHouseMutationsInProgress:
		set(&table().MutationsInProgress)
	case qClickHouseMutationsFailing:
		set(&table().MutationsFailing)
	case qClickHouseMutationsStuck:
		set(&table().MutationsStuck)

	case qClickHouseTopQueryCalls:
		set(&query().Calls)
	case qClickHouseTopQueryTime:
		set(&query().TotalTime)
	case qClickHouseTopQueryReadRows:
		set(&query().ReadRows)
	case qClickHouseTopQueryReadBytes:
		set(&query().ReadBytes)
	case qClickHouseTopQueryErrors:
		set(&query().Errors)
	}
}

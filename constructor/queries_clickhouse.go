package constructor

// Shards fork: ClickHouse metrics of the shards cluster agent (labeled with the target address).

const (
	qClickHouseUp          = "clickhouse_up"
	qClickHouseScrapeError = "clickhouse_scrape_error"
	qClickHouseInfo        = "clickhouse_info"

	qClickHouseQueriesRunning     = "clickhouse_queries_running"
	qClickHouseMergesRunning      = "clickhouse_merges_running"
	qClickHouseMutationsRunning   = "clickhouse_mutations_running"
	qClickHouseFetchesRunning     = "clickhouse_replicated_fetches_running"
	qClickHouseConnections        = "clickhouse_connections"
	qClickHouseMemoryTracking     = "clickhouse_memory_tracking_bytes"
	qClickHouseInsertsDelayed     = "clickhouse_inserts_delayed"
	qClickHouseReadonlyReplicas   = "clickhouse_readonly_replicas"
	qClickHouseZooKeeperSessions  = "clickhouse_zookeeper_sessions"
	qClickHouseZooKeeperRequests  = "clickhouse_zookeeper_requests_in_flight"
	qClickHouseDistributedFiles   = "clickhouse_distributed_files_to_insert"
	qClickHouseUptime             = "clickhouse_uptime_seconds"
	qClickHouseMaxPartsPerPtn     = "clickhouse_max_part_count_for_partition"
	qClickHouseReplicasMaxDelay   = "clickhouse_replicas_max_absolute_delay_seconds"
	qClickHouseReplicasMaxQueue   = "clickhouse_replicas_max_queue_size"
	qClickHouseReplicasSumQueue   = "clickhouse_replicas_sum_queue_size"
	qClickHouseMergeTreeParts     = "clickhouse_mergetree_parts"
	qClickHouseMergeTreeBytes     = "clickhouse_mergetree_bytes"
	qClickHouseMemoryResident     = "clickhouse_memory_resident_bytes"
	qClickHouseOSMemoryTotal      = "clickhouse_os_memory_total_bytes"
	qClickHouseOSMemoryAvailable  = "clickhouse_os_memory_available_bytes"
	qClickHouseQueries            = "clickhouse_queries_total"
	qClickHouseFailedQueries      = "clickhouse_failed_queries_total"
	qClickHouseQueryTime          = "clickhouse_query_time_seconds_total"
	qClickHouseInsertedRows       = "clickhouse_inserted_rows_total"
	qClickHouseInsertedBytes      = "clickhouse_inserted_bytes_total"
	qClickHouseSelectedRows       = "clickhouse_selected_rows_total"
	qClickHouseSelectedBytes      = "clickhouse_selected_bytes_total"
	qClickHouseMerges             = "clickhouse_merges_total"
	qClickHouseDelayedInserts     = "clickhouse_delayed_inserts_total"
	qClickHouseRejectedInserts    = "clickhouse_rejected_inserts_total"
	qClickHouseZooKeeperExc       = "clickhouse_zookeeper_exceptions_total"
	qClickHouseFailedFetches      = "clickhouse_replicated_part_failed_fetches_total"
	qClickHouseDataLoss           = "clickhouse_replicated_data_loss_total"
	qClickHouseDistributedConnErr = "clickhouse_distributed_connection_fail_try_total"
	qClickHouseMemLimitExceeded   = "clickhouse_query_memory_limit_exceeded_total"
	qClickHouseErrors             = "clickhouse_errors_total"

	qClickHouseTableParts          = "clickhouse_table_parts"
	qClickHouseTableSize           = "clickhouse_table_size_bytes"
	qClickHouseTableMaxPartsPerPtn = "clickhouse_table_max_parts_per_partition"
	qClickHouseReplicaReadonly     = "clickhouse_replica_readonly"
	qClickHouseReplicaExpired      = "clickhouse_replica_session_expired"
	qClickHouseReplicaDelay        = "clickhouse_replica_absolute_delay_seconds"
	qClickHouseReplicaQueue        = "clickhouse_replica_queue_size"
	qClickHouseMutationsInProgress = "clickhouse_table_mutations_in_progress"
	qClickHouseMutationsFailing    = "clickhouse_table_mutations_failing"
	qClickHouseMutationsStuck      = "clickhouse_table_mutations_stuck"

	qClickHouseTopQueryCalls     = "clickhouse_top_query_calls_per_second"
	qClickHouseTopQueryTime      = "clickhouse_top_query_time_per_second"
	qClickHouseTopQueryReadRows  = "clickhouse_top_query_read_rows_per_second"
	qClickHouseTopQueryReadBytes = "clickhouse_top_query_read_bytes_per_second"
	qClickHouseTopQueryErrors    = "clickhouse_top_query_errors_per_second"
)

func chRate(metric string) string {
	return `rate(` + metric + `[$RANGE])`
}

var clickhouseQueries = []Query{
	qDB(qClickHouseUp, `clickhouse_up`),
	qDB(qClickHouseScrapeError, `clickhouse_scrape_error`, "error", "warning"),
	qDB(qClickHouseInfo, `clickhouse_info`, "server_version"),

	// system.metrics
	qDB(qClickHouseQueriesRunning, `clickhouse_queries_running`),
	qDB(qClickHouseMergesRunning, `clickhouse_merges_running`),
	qDB(qClickHouseMutationsRunning, `clickhouse_mutations_running`),
	qDB(qClickHouseFetchesRunning, `clickhouse_replicated_fetches_running`),
	qDB(qClickHouseConnections, `clickhouse_connections`, "protocol"),
	qDB(qClickHouseMemoryTracking, `clickhouse_memory_tracking_bytes`),
	qDB(qClickHouseInsertsDelayed, `clickhouse_inserts_delayed`),
	qDB(qClickHouseReadonlyReplicas, `clickhouse_readonly_replicas`),
	qDB(qClickHouseZooKeeperSessions, `clickhouse_zookeeper_sessions`),
	qDB(qClickHouseZooKeeperRequests, `clickhouse_zookeeper_requests_in_flight`),
	qDB(qClickHouseDistributedFiles, `clickhouse_distributed_files_to_insert`),

	// system.asynchronous_metrics
	qDB(qClickHouseUptime, `clickhouse_uptime_seconds`),
	qDB(qClickHouseMaxPartsPerPtn, `clickhouse_max_part_count_for_partition`),
	qDB(qClickHouseReplicasMaxDelay, `clickhouse_replicas_max_absolute_delay_seconds`),
	qDB(qClickHouseReplicasMaxQueue, `clickhouse_replicas_max_queue_size`),
	qDB(qClickHouseReplicasSumQueue, `clickhouse_replicas_sum_queue_size`),
	qDB(qClickHouseMergeTreeParts, `clickhouse_mergetree_parts`),
	qDB(qClickHouseMergeTreeBytes, `clickhouse_mergetree_bytes`),
	qDB(qClickHouseMemoryResident, `clickhouse_memory_resident_bytes`),
	qDB(qClickHouseOSMemoryTotal, `clickhouse_os_memory_total_bytes`),
	qDB(qClickHouseOSMemoryAvailable, `clickhouse_os_memory_available_bytes`),

	// system.events (counters): loaded as per-second rates
	qDB(qClickHouseQueries, chRate(`clickhouse_queries_total`), "kind"),
	qDB(qClickHouseFailedQueries, chRate(`clickhouse_failed_queries_total`), "kind"),
	qDB(qClickHouseQueryTime, chRate(`clickhouse_query_time_seconds_total`)),
	qDB(qClickHouseInsertedRows, chRate(`clickhouse_inserted_rows_total`)),
	qDB(qClickHouseInsertedBytes, chRate(`clickhouse_inserted_bytes_total`)),
	qDB(qClickHouseSelectedRows, chRate(`clickhouse_selected_rows_total`)),
	qDB(qClickHouseSelectedBytes, chRate(`clickhouse_selected_bytes_total`)),
	qDB(qClickHouseMerges, chRate(`clickhouse_merges_total`)),
	qDB(qClickHouseDelayedInserts, chRate(`clickhouse_delayed_inserts_total`)),
	qDB(qClickHouseRejectedInserts, chRate(`clickhouse_rejected_inserts_total`)),
	qDB(qClickHouseZooKeeperExc, chRate(`clickhouse_zookeeper_exceptions_total`), "type"),
	qDB(qClickHouseFailedFetches, chRate(`clickhouse_replicated_part_failed_fetches_total`)),
	qDB(qClickHouseDataLoss, chRate(`clickhouse_replicated_data_loss_total`)),
	qDB(qClickHouseDistributedConnErr, chRate(`clickhouse_distributed_connection_fail_try_total`)),
	qDB(qClickHouseMemLimitExceeded, chRate(`clickhouse_query_memory_limit_exceeded_total`)),
	qDB(qClickHouseErrors, chRate(`clickhouse_errors_total`), "name"),

	// per table (bounded by the agent's topTables)
	qDB(qClickHouseTableParts, `clickhouse_table_parts`, "db", "table"),
	qDB(qClickHouseTableSize, `clickhouse_table_size_bytes`, "db", "table"),
	qDB(qClickHouseTableMaxPartsPerPtn, `clickhouse_table_max_parts_per_partition`, "db", "table"),
	qDB(qClickHouseReplicaReadonly, `clickhouse_replica_readonly`, "db", "table"),
	qDB(qClickHouseReplicaExpired, `clickhouse_replica_session_expired`, "db", "table"),
	qDB(qClickHouseReplicaDelay, `clickhouse_replica_absolute_delay_seconds`, "db", "table"),
	qDB(qClickHouseReplicaQueue, `clickhouse_replica_queue_size`, "db", "table"),
	qDB(qClickHouseMutationsInProgress, `clickhouse_table_mutations_in_progress`, "db", "table"),
	qDB(qClickHouseMutationsFailing, `clickhouse_table_mutations_failing`, "db", "table"),
	qDB(qClickHouseMutationsStuck, `clickhouse_table_mutations_stuck`, "db", "table"),

	// top queries from system.query_log (already per second)
	qDB(qClickHouseTopQueryCalls, `clickhouse_top_query_calls_per_second`, "db", "query"),
	qDB(qClickHouseTopQueryTime, `clickhouse_top_query_time_per_second`, "db", "query"),
	qDB(qClickHouseTopQueryReadRows, `clickhouse_top_query_read_rows_per_second`, "db", "query"),
	qDB(qClickHouseTopQueryReadBytes, `clickhouse_top_query_read_bytes_per_second`, "db", "query"),
	qDB(qClickHouseTopQueryErrors, `clickhouse_top_query_errors_per_second`, "db", "query"),
}

func init() {
	QUERIES = append(QUERIES, clickhouseQueries...)
}

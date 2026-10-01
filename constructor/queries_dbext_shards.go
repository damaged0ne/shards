package constructor

import "slices"

// Shards fork: queries for the metrics added by the shards cluster agent
// (see docs/postgres-mysql-metrics.md and README.md of shards-cluster).
// The query names keep the metric prefix (pg_, mysql_, pgbouncer_, rabbitmq_, etcd_, aws_, azure_)
// so that enrichInstances and the cloud loaders route them.

func qECServerless(name, query string, labels ...string) Query {
	return Q(name, query, slices.Concat([]string{"ec_serverless_id"}, labels)...)
}

func qMemoryDB(name, query string, labels ...string) Query {
	return Q(name, query, slices.Concat([]string{"memorydb_cluster_id"}, labels)...)
}

func qAzureDB(name, query string, labels ...string) Query {
	return Q(name, query, slices.Concat([]string{"azure_db_id"}, labels)...)
}

func qAzureRedis(name, query string, labels ...string) Query {
	return Q(name, query, slices.Concat([]string{"azure_redis_id"}, labels)...)
}

func qRate(metric string) string {
	return `rate(` + metric + `[$RANGE])`
}

const (
	qClusterAgentCollectSuccess  = "cluster_agent_target_collect_success"
	qClusterAgentCollectDuration = "cluster_agent_target_collect_duration"
	qClusterAgentCollectTimeouts = "cluster_agent_target_collect_timeouts"
)

var dbExtQueries = []Query{
	// Postgres: pg_stat_database
	qDB("pg_db_xact_commit_rate", qRate("pg_db_xact_commit_total"), "db"),
	qDB("pg_db_xact_rollback_rate", qRate("pg_db_xact_rollback_total"), "db"),
	qDB("pg_db_blks_hit_rate", qRate("pg_db_blks_hit_total"), "db"),
	qDB("pg_db_blks_read_rate", qRate("pg_db_blks_read_total"), "db"),
	qDB("pg_db_deadlocks_rate", qRate("pg_db_deadlocks_total"), "db"),
	qDB("pg_db_conflicts_rate", qRate("pg_db_conflicts_total"), "db"),
	qDB("pg_db_temp_bytes_rate", qRate("pg_db_temp_bytes_total"), "db"),
	qDB("pg_db_checksum_failures_rate", qRate("pg_db_checksum_failures_total"), "db"),
	qDB("pg_db_sessions_abandoned_rate", qRate("pg_db_sessions_abandoned_total"), "db"),
	qDB("pg_db_sessions_fatal_rate", qRate("pg_db_sessions_fatal_total"), "db"),
	qDB("pg_db_sessions_killed_rate", qRate("pg_db_sessions_killed_total"), "db"),
	qDB("pg_db_idle_in_transaction_time_rate", qRate("pg_db_idle_in_transaction_time_seconds_total"), "db"),

	// Postgres: pg_stat_io (PG16+), aggregated over objects and contexts
	qDB("pg_io_reads_rate", `sum without(object, context) (`+qRate("pg_io_reads_total")+`)`, "backend_type"),
	qDB("pg_io_writes_rate", `sum without(object, context) (`+qRate("pg_io_writes_total")+`)`, "backend_type"),
	qDB("pg_io_extends_rate", `sum without(object, context) (`+qRate("pg_io_extends_total")+`)`, "backend_type"),
	qDB("pg_io_fsyncs_rate", `sum without(object, context) (`+qRate("pg_io_fsyncs_total")+`)`, "backend_type"),
	qDB("pg_io_hits_rate", `sum without(object, context) (`+qRate("pg_io_hits_total")+`)`, "backend_type"),
	qDB("pg_io_evictions_rate", `sum without(object, context) (`+qRate("pg_io_evictions_total")+`)`, "backend_type"),
	qDB("pg_io_read_time_rate", `sum without(object, context) (`+qRate("pg_io_read_time_seconds_total")+`)`, "backend_type"),
	qDB("pg_io_write_time_rate", `sum without(object, context) (`+qRate("pg_io_write_time_seconds_total")+`)`, "backend_type"),

	// Postgres: pg_stat_wal (PG14+)
	qDB("pg_wal_records_rate", qRate("pg_wal_records_total")),
	qDB("pg_wal_fpi_rate", qRate("pg_wal_fpi_total")),
	qDB("pg_wal_bytes_rate", qRate("pg_wal_bytes_total")),
	qDB("pg_wal_buffers_full_rate", qRate("pg_wal_buffers_full_total")),

	// Postgres: logical replication (subscriber side) and standbys as seen by the primary
	qDB("pg_subscription_worker_up", `pg_subscription_worker_up`, "subscription"),
	qDB("pg_subscription_last_msg_receipt_age_seconds", `pg_subscription_last_msg_receipt_age_seconds`, "subscription"),
	qDB("pg_subscription_last_msg_delay_seconds", `pg_subscription_last_msg_delay_seconds`, "subscription"),
	qDB("pg_subscription_latest_end_age_seconds", `pg_subscription_latest_end_age_seconds`, "subscription"),
	qDB("pg_subscription_errors_rate", qRate("pg_subscription_errors_total"), "subscription", "type"),
	qDB("pg_replication_standby_replay_lag_seconds", `pg_replication_standby_lag_seconds{stage="replay"}`, "application_name", "client_addr"),
	qDB("pg_replication_standby_replay_lag_bytes", `pg_replication_standby_lag_bytes{stage="replay"}`, "application_name", "client_addr"),
	qDB("pg_replication_standby_info", `pg_replication_standby_info`, "application_name", "client_addr", "state", "sync_state"),

	// Postgres: indexes (tracker interval)
	qDB("pg_index_unused_bytes", `pg_index_unused_bytes`, "db", "schema", "table", "index"),
	qDB("pg_db_unused_indexes", `pg_db_unused_indexes`, "db"),
	qDB("pg_db_unused_indexes_bytes", `pg_db_unused_indexes_bytes`, "db"),
	qDB("pg_db_duplicate_indexes", `pg_db_duplicate_indexes`, "db"),

	// Postgres: wait events
	qDB("pg_wait_event_sessions", `pg_wait_event_sessions`, "wait_event_type", "wait_event"),

	// Postgres: extended pg_stat_statements columns
	qDB("pg_top_query_plan_time_per_second", `pg_top_query_plan_time_per_second`, "db", "user", "query"),
	qDB("pg_top_query_rows_per_second", `pg_top_query_rows_per_second`, "db", "user", "query"),
	qDB("pg_top_query_shared_blks_hit_per_second", `pg_top_query_shared_blks_hit_per_second`, "db", "user", "query"),
	qDB("pg_top_query_shared_blks_read_per_second", `pg_top_query_shared_blks_read_per_second`, "db", "user", "query"),
	qDB("pg_top_query_temp_blks_read_per_second", `pg_top_query_temp_blks_read_per_second`, "db", "user", "query"),
	qDB("pg_top_query_temp_blks_written_per_second", `pg_top_query_temp_blks_written_per_second`, "db", "user", "query"),
	qDB("pg_top_query_wal_bytes_per_second", `pg_top_query_wal_bytes_per_second`, "db", "user", "query"),
	qDB("pg_top_query_exec_time_min_seconds", `pg_top_query_exec_time_min_seconds`, "db", "user", "query"),
	qDB("pg_top_query_exec_time_mean_seconds", `pg_top_query_exec_time_mean_seconds`, "db", "user", "query"),
	qDB("pg_top_query_exec_time_max_seconds", `pg_top_query_exec_time_max_seconds`, "db", "user", "query"),

	// MySQL
	qDB("mysql_wait_event_time_rate", qRate("mysql_wait_event_seconds_total"), "event"),
	qDB("mysql_wait_event_count_rate", qRate("mysql_wait_event_count_total"), "event"),
	qDB("mysql_replication_applier_last_transaction_lag_seconds", `mysql_replication_applier_last_transaction_lag_seconds`, "channel"),
	qDB("mysql_replication_applier_current_lag_seconds", `mysql_replication_applier_current_lag_seconds`, "channel"),
	qDB("mysql_index_unused", `mysql_index_unused`, "schema", "table", "index"),
	qDB("mysql_index_unused_bytes", `mysql_index_unused_bytes`, "schema", "table", "index"),
	qDB("mysql_schema_unused_indexes", `mysql_schema_unused_indexes`, "schema"),

	// PgBouncer
	qDB("pgbouncer_up", `pgbouncer_up`),
	qDB("pgbouncer_pools_client_active_connections", `pgbouncer_pools_client_active_connections`, "database", "user"),
	qDB("pgbouncer_pools_client_waiting_connections", `pgbouncer_pools_client_waiting_connections`, "database", "user"),
	qDB("pgbouncer_pools_server_active_connections", `pgbouncer_pools_server_active_connections`, "database", "user"),
	qDB("pgbouncer_pools_server_idle_connections", `pgbouncer_pools_server_idle_connections`, "database", "user"),
	qDB("pgbouncer_pools_server_used_connections", `pgbouncer_pools_server_used_connections`, "database", "user"),
	qDB("pgbouncer_pools_client_maxwait_seconds", `pgbouncer_pools_client_maxwait_seconds`, "database", "user"),
	qDB("pgbouncer_stats_queries_rate", qRate("pgbouncer_stats_queries_pooled_total"), "database"),
	qDB("pgbouncer_stats_transactions_rate", qRate("pgbouncer_stats_sql_transactions_pooled_total"), "database"),
	qDB("pgbouncer_stats_query_time_rate", qRate("pgbouncer_stats_queries_duration_seconds_total"), "database"),
	qDB("pgbouncer_stats_client_wait_rate", qRate("pgbouncer_stats_client_wait_seconds_total"), "database"),
	qDB("pgbouncer_stats_received_bytes_rate", qRate("pgbouncer_stats_received_bytes_total"), "database"),
	qDB("pgbouncer_stats_sent_bytes_rate", qRate("pgbouncer_stats_sent_bytes_total"), "database"),

	// RabbitMQ (native scrape, job="rabbitmq"); per-object metrics, if enabled, are aggregated
	qDB("rabbitmq_up", `up{job="rabbitmq"}`),
	qDB("rabbitmq_build_info", `rabbitmq_build_info`, "rabbitmq_version"),
	qDB("rabbitmq_identity_info", `rabbitmq_identity_info`, "rabbitmq_node"),
	qDB("rabbitmq_queue_messages_ready", `sum without(vhost, queue) (rabbitmq_queue_messages_ready)`),
	qDB("rabbitmq_queue_messages_unacked", `sum without(vhost, queue) (rabbitmq_queue_messages_unacked)`),
	qDB("rabbitmq_published_rate", `sum without(protocol) (`+qRate("rabbitmq_global_messages_received_total")+`)`),
	qDB("rabbitmq_delivered_rate", `sum without(protocol) (`+qRate("rabbitmq_global_messages_delivered_total")+`)`),
	qDB("rabbitmq_unroutable_rate", `sum without(protocol) (`+qRate("rabbitmq_global_messages_unroutable_dropped_total")+`)`),
	qDB("rabbitmq_consumers", `rabbitmq_consumers`),
	qDB("rabbitmq_connections", `rabbitmq_connections`),
	qDB("rabbitmq_queues", `rabbitmq_queues`),
	qDB("rabbitmq_process_resident_memory_bytes", `rabbitmq_process_resident_memory_bytes`),
	qDB("rabbitmq_resident_memory_limit_bytes", `rabbitmq_resident_memory_limit_bytes`),
	qDB("rabbitmq_disk_space_available_bytes", `rabbitmq_disk_space_available_bytes`),
	qDB("rabbitmq_disk_space_available_limit_bytes", `rabbitmq_disk_space_available_limit_bytes`),
	qDB("rabbitmq_process_open_fds", `rabbitmq_process_open_fds`),
	qDB("rabbitmq_process_max_fds", `rabbitmq_process_max_fds`),
	qDB("rabbitmq_unreachable_cluster_peers_count", `rabbitmq_unreachable_cluster_peers_count`),
	qDB("rabbitmq_alarms_memory_used_watermark", `rabbitmq_alarms_memory_used_watermark`),
	qDB("rabbitmq_alarms_free_disk_space_watermark", `rabbitmq_alarms_free_disk_space_watermark`),
	qDB("rabbitmq_alarms_file_descriptor_limit", `rabbitmq_alarms_file_descriptor_limit`),

	// etcd (native scrape, job="etcd")
	qDB("etcd_up", `up{job="etcd"}`),
	qDB("etcd_server_version", `etcd_server_version`, "server_version"),
	qDB("etcd_server_has_leader", `etcd_server_has_leader`),
	qDB("etcd_server_is_leader", `etcd_server_is_leader`),
	qDB("etcd_leader_changes_rate", qRate("etcd_server_leader_changes_seen_total")),
	qDB("etcd_proposals_failed_rate", qRate("etcd_server_proposals_failed_total")),
	qDB("etcd_proposals_applied_rate", qRate("etcd_server_proposals_applied_total")),
	qDB("etcd_server_proposals_pending", `etcd_server_proposals_pending`),
	qDB("etcd_wal_fsync_p99", `histogram_quantile(0.99, `+qRate("etcd_disk_wal_fsync_duration_seconds_bucket")+`)`),
	qDB("etcd_backend_commit_p99", `histogram_quantile(0.99, `+qRate("etcd_disk_backend_commit_duration_seconds_bucket")+`)`),
	qDB("etcd_peer_rtt_p99", `histogram_quantile(0.99, max without(To) (`+qRate("etcd_network_peer_round_trip_time_seconds_bucket")+`))`),
	qDB("etcd_mvcc_db_total_size_in_bytes", `etcd_mvcc_db_total_size_in_bytes`),
	qDB("etcd_mvcc_db_total_size_in_use_in_bytes", `etcd_mvcc_db_total_size_in_use_in_bytes`),
	qDB("etcd_server_quota_backend_bytes", `etcd_server_quota_backend_bytes`),

	// AWS Aurora (RDS instances that are cluster members)
	qRDS("aws_rds_cluster_role", `aws_rds_cluster_role`, "role"),
	qRDS("aws_rds_aurora_replica_lag_seconds", `aws_rds_aurora_replica_lag_seconds`),
	qRDS("aws_rds_serverless_capacity_acu", `aws_rds_serverless_capacity_acu`),
	qRDS("aws_rds_serverless_acu_utilization_percent", `aws_rds_serverless_acu_utilization_percent`),
	qRDS("aws_rds_serverless_min_capacity_acu", `aws_rds_serverless_min_capacity_acu`),
	qRDS("aws_rds_serverless_max_capacity_acu", `aws_rds_serverless_max_capacity_acu`),

	// AWS ElastiCache Serverless
	qECServerless("aws_elasticache_serverless_info", `aws_elasticache_serverless_info`, "region", "endpoint", "port", "engine", "engine_version", "cache_name"),
	qECServerless("aws_elasticache_serverless_status", `aws_elasticache_serverless_status`, "status"),
	qECServerless("aws_elasticache_serverless_data_storage_limit_bytes", `aws_elasticache_serverless_data_storage_limit_bytes`),
	qECServerless("aws_elasticache_serverless_ecpu_limit_per_second", `aws_elasticache_serverless_ecpu_limit_per_second`),
	qECServerless("aws_elasticache_serverless_ecpu_per_second", `aws_elasticache_serverless_ecpu_per_second`),
	qECServerless("aws_elasticache_serverless_used_bytes", `aws_elasticache_serverless_used_bytes`),
	qECServerless("aws_elasticache_serverless_connections", `aws_elasticache_serverless_connections`),

	// AWS MemoryDB
	qMemoryDB("aws_memorydb_info", `aws_memorydb_info`, "region", "endpoint", "port", "engine", "engine_version", "node_type", "cluster_name"),
	qMemoryDB("aws_memorydb_status", `aws_memorydb_status`, "status"),
	qMemoryDB("aws_memorydb_node_info", `aws_memorydb_node_info`, "shard", "node", "availability_zone", "endpoint", "port", "status"),

	// Azure
	Q("azure_discovery_error", `azure_discovery_error`, "error"),
	qAzureDB("azure_db_info", `azure_db_info`, "name", "location", "zone", "fqdn", "ipv4", "port", "engine", "engine_version", "sku", "tier", "replication_role", "primary"),
	qAzureDB("azure_db_status", `azure_db_status`, "status"),
	qAzureDB("azure_db_storage_total_bytes", `azure_db_storage_total_bytes`),
	qAzureDB("azure_db_cpu_usage_percent", `azure_db_cpu_usage_percent`),
	qAzureDB("azure_db_memory_usage_percent", `azure_db_memory_usage_percent`),
	qAzureDB("azure_db_storage_usage_percent", `azure_db_storage_usage_percent`),
	qAzureDB("azure_db_storage_used_bytes", `azure_db_storage_used_bytes`),
	qAzureDB("azure_db_iops", `azure_db_iops`),
	qAzureDB("azure_db_io_ops_per_second", `azure_db_io_ops_per_second`, "operation"),
	qAzureDB("azure_db_io_consumption_percent", `azure_db_io_consumption_percent`),
	qAzureDB("azure_db_network_bytes_per_second", `azure_db_network_bytes_per_second`, "direction"),
	qAzureDB("azure_db_connections_active", `azure_db_connections_active`),
	qAzureDB("azure_db_replication_lag_seconds", `azure_db_replication_lag_seconds`),
	qAzureRedis("azure_redis_info", `azure_redis_info`, "name", "location", "host", "ipv4", "port", "ssl_port", "engine_version", "sku", "family", "capacity", "shards"),
	qAzureRedis("azure_redis_status", `azure_redis_status`, "status"),
	qAzureRedis("azure_redis_cpu_usage_percent", `azure_redis_cpu_usage_percent`),
	qAzureRedis("azure_redis_memory_usage_percent", `azure_redis_memory_usage_percent`),
	qAzureRedis("azure_redis_memory_used_bytes", `azure_redis_memory_used_bytes`),
	qAzureRedis("azure_redis_server_load_percent", `azure_redis_server_load_percent`),
	qAzureRedis("azure_redis_connected_clients", `azure_redis_connected_clients`),
	qAzureRedis("azure_redis_network_bytes_per_second", `azure_redis_network_bytes_per_second`, "direction"),

	// cluster agent self-observability
	Q(qClusterAgentCollectSuccess, `coroot_cluster_agent_target_collect_success`, "address", "target_type"),
	Q(qClusterAgentCollectDuration, `coroot_cluster_agent_target_collect_duration_seconds`, "address", "target_type"),
	Q(qClusterAgentCollectTimeouts, qRate("coroot_cluster_agent_target_collect_timeouts_total"), "address", "target_type"),
}

func init() {
	QUERIES = append(QUERIES, dbExtQueries...)
}

package constructor

// Shards fork: Elasticsearch/OpenSearch metrics of the shards cluster agent (labeled with the target address).

const (
	qESUp          = "elasticsearch_up"
	qESScrapeError = "elasticsearch_scrape_error"
	qESVersionInfo = "elasticsearch_clusterinfo_version_info"

	qESHealthStatus            = "elasticsearch_cluster_health_status"
	qESHealthNodes             = "elasticsearch_cluster_health_number_of_nodes"
	qESHealthDataNodes         = "elasticsearch_cluster_health_number_of_data_nodes"
	qESHealthActivePrimary     = "elasticsearch_cluster_health_active_primary_shards"
	qESHealthActiveShards      = "elasticsearch_cluster_health_active_shards"
	qESHealthRelocating        = "elasticsearch_cluster_health_relocating_shards"
	qESHealthInitializing      = "elasticsearch_cluster_health_initializing_shards"
	qESHealthUnassigned        = "elasticsearch_cluster_health_unassigned_shards"
	qESHealthDelayedUnassigned = "elasticsearch_cluster_health_delayed_unassigned_shards"
	qESHealthPendingTasks      = "elasticsearch_cluster_health_number_of_pending_tasks"
	qESHealthTaskMaxWaiting    = "elasticsearch_cluster_health_task_max_waiting_in_queue_millis"

	qESHeapUsed          = "elasticsearch_jvm_heap_used_bytes"
	qESHeapMax           = "elasticsearch_jvm_heap_max_bytes"
	qESGcTime            = "elasticsearch_jvm_gc_time"
	qESFsAvailable       = "elasticsearch_filesystem_data_available_bytes"
	qESFsSize            = "elasticsearch_filesystem_data_size_bytes"
	qESNodeDocs          = "elasticsearch_indices_docs"
	qESNodeStoreSize     = "elasticsearch_indices_store_size_bytes"
	qESIndexingRate      = "elasticsearch_indexing_rate"
	qESIndexingTime      = "elasticsearch_indexing_time"
	qESIndexingFailed    = "elasticsearch_indexing_failed"
	qESSearchQueryRate   = "elasticsearch_search_query_rate"
	qESSearchQueryTime   = "elasticsearch_search_query_time"
	qESThreadPoolRejects = "elasticsearch_thread_pool_rejects"
	qESThreadPoolQueue   = "elasticsearch_thread_pool_queue_count"
	qESBreakersTripped   = "elasticsearch_breakers_tripped_rate"
	qESProcessCpu        = "elasticsearch_process_cpu_percent"

	qESIndexHealth        = "elasticsearch_indices_health_status"
	qESIndexDocsPrimary   = "elasticsearch_indices_docs_primary"
	qESIndexSizePrimary   = "elasticsearch_indices_store_size_bytes_primary"
	qESIndexSizeTotal     = "elasticsearch_indices_store_size_bytes_total"
	qESIndexShardsPrimary = "elasticsearch_indices_shards_primary"
	qESIndexReplicas      = "elasticsearch_indices_replicas"
)

var esNodeLabels = []string{"cluster", "host", "name"}

func qESNode(name, query string, labels ...string) Query {
	return qDB(name, query, append(append([]string{}, esNodeLabels...), labels...)...)
}

var elasticsearchQueries = []Query{
	qDB(qESUp, `elasticsearch_up`),
	qDB(qESScrapeError, `elasticsearch_scrape_error`, "error", "warning"),
	qDB(qESVersionInfo, `elasticsearch_clusterinfo_version_info`, "cluster", "version", "distribution"),

	// _cluster/health
	qDB(qESHealthStatus, `elasticsearch_cluster_health_status == 1`, "cluster", "color"),
	qDB(qESHealthNodes, `elasticsearch_cluster_health_number_of_nodes`, "cluster"),
	qDB(qESHealthDataNodes, `elasticsearch_cluster_health_number_of_data_nodes`, "cluster"),
	qDB(qESHealthActivePrimary, `elasticsearch_cluster_health_active_primary_shards`, "cluster"),
	qDB(qESHealthActiveShards, `elasticsearch_cluster_health_active_shards`, "cluster"),
	qDB(qESHealthRelocating, `elasticsearch_cluster_health_relocating_shards`, "cluster"),
	qDB(qESHealthInitializing, `elasticsearch_cluster_health_initializing_shards`, "cluster"),
	qDB(qESHealthUnassigned, `elasticsearch_cluster_health_unassigned_shards`, "cluster"),
	qDB(qESHealthDelayedUnassigned, `elasticsearch_cluster_health_delayed_unassigned_shards`, "cluster"),
	qDB(qESHealthPendingTasks, `elasticsearch_cluster_health_number_of_pending_tasks`, "cluster"),
	qDB(qESHealthTaskMaxWaiting, `elasticsearch_cluster_health_task_max_waiting_in_queue_millis`, "cluster"),

	// _nodes/stats
	qESNode(qESHeapUsed, `elasticsearch_jvm_memory_used_bytes{area="heap"}`),
	qESNode(qESHeapMax, `elasticsearch_jvm_memory_max_bytes{area="heap"}`),
	qESNode(qESGcTime, `rate(elasticsearch_jvm_gc_collection_seconds_sum[$RANGE])`, "gc"),
	qESNode(qESFsAvailable, `elasticsearch_filesystem_data_available_bytes`, "path"),
	qESNode(qESFsSize, `elasticsearch_filesystem_data_size_bytes`, "path"),
	qESNode(qESNodeDocs, `elasticsearch_indices_docs`),
	qESNode(qESNodeStoreSize, `elasticsearch_indices_store_size_bytes`),
	qESNode(qESIndexingRate, `rate(elasticsearch_indices_indexing_index_total[$RANGE])`),
	qESNode(qESIndexingTime, `rate(elasticsearch_indices_indexing_index_time_seconds_total[$RANGE])`),
	qESNode(qESIndexingFailed, `rate(elasticsearch_indices_indexing_index_failed_total[$RANGE])`),
	qESNode(qESSearchQueryRate, `rate(elasticsearch_indices_search_query_total[$RANGE])`),
	qESNode(qESSearchQueryTime, `rate(elasticsearch_indices_search_query_time_seconds[$RANGE])`),
	// the rejected/tripped counts are cumulative since the node start
	qESNode(qESThreadPoolRejects, `rate(elasticsearch_thread_pool_rejected_count[$RANGE])`, "type"),
	qESNode(qESThreadPoolQueue, `elasticsearch_thread_pool_queue_count`, "type"),
	qESNode(qESBreakersTripped, `rate(elasticsearch_breakers_tripped[$RANGE])`, "breaker"),
	qESNode(qESProcessCpu, `elasticsearch_process_cpu_percent`),

	// _cat/indices (bounded by the agent's topIndices)
	qDB(qESIndexHealth, `elasticsearch_indices_health_status == 1`, "cluster", "index", "color"),
	qDB(qESIndexDocsPrimary, `elasticsearch_indices_docs_primary`, "cluster", "index"),
	qDB(qESIndexSizePrimary, `elasticsearch_indices_store_size_bytes_primary`, "cluster", "index"),
	qDB(qESIndexSizeTotal, `elasticsearch_indices_store_size_bytes_total`, "cluster", "index"),
	qDB(qESIndexShardsPrimary, `elasticsearch_indices_shards_primary`, "cluster", "index"),
	qDB(qESIndexReplicas, `elasticsearch_indices_replicas`, "cluster", "index"),
}

func init() {
	QUERIES = append(QUERIES, elasticsearchQueries...)
}

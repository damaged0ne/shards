package model

import "github.com/coroot/coroot/timeseries"

// Shards fork: checks of the Kafka, ClickHouse and Elasticsearch/OpenSearch targets of the shards cluster agent.

// SetCritical marks the check as critical: if it fires (see Calc), its status is CRITICAL instead of WARNING.
// The alerts of a critical check are raised with the critical severity regardless of the rule's severity.
func (ch *Check) SetCritical() {
	ch.critical = true
}

func (ch *Check) IsCritical() bool {
	return ch.critical
}

func initClusterTargetChecks() {
	Checks.KafkaAvailability = CheckConfig{
		Category:                AuditReportKafka,
		Type:                    CheckTypeItemBased,
		Title:                   "Kafka availability",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithToBe "Kafka target"}} unavailable`,
		ConditionFormatTemplate: "the cluster agent can't reach the Kafka cluster through the target (kafka_up = 0)",
	}
	Checks.KafkaOfflinePartitions = CheckConfig{
		Category:                AuditReportKafka,
		Type:                    CheckTypeItemBased,
		Title:                   "Offline partitions",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithHave "topic"}} offline partitions`,
		ConditionFormatTemplate: "a partition has no leader: it can be neither produced to nor consumed from (critical)",
	}
	// Under-replicated partitions are expected briefly during broker restarts and reassignments,
	// so only a sustained state is reported (Kafka docs recommend alerting on UnderReplicatedPartitions > 0:
	// https://kafka.apache.org/documentation/#monitoring).
	Checks.KafkaUnderReplicatedPartitions = CheckConfig{
		Category:                AuditReportKafka,
		Type:                    CheckTypeItemBased,
		Title:                   "Under-replicated partitions",
		DefaultThreshold:        300,
		Unit:                    CheckUnitSecond,
		MessageTemplate:         `{{.ItemsWithHave "topic"}} under-replicated partitions`,
		ConditionFormatTemplate: "a topic has partitions with fewer in-sync replicas than replicas for longer than <threshold>",
	}
	// Burrow-style evaluation of the consumer lag (https://github.com/linkedin/Burrow/wiki/Consumer-Lag-Evaluation-Rules):
	// the lag itself is not a problem as long as the group keeps up; it is when the lag grows continuously,
	// the consumer is far behind in time, or it stopped committing offsets while there is lag (STALLED, critical).
	Checks.KafkaConsumerLag = CheckConfig{
		Category:                AuditReportKafka,
		Type:                    CheckTypeItemBased,
		Title:                   "Consumer lag",
		DefaultThreshold:        300,
		Unit:                    CheckUnitSecond,
		MessageTemplate:         `{{.ItemsWithToBe "consumer group"}} falling behind`,
		ConditionFormatTemplate: "the lag of a consumer group keeps growing, its estimated time lag > <threshold>, or it stopped committing offsets with lag > 0 (critical)",
	}

	Checks.ClickHouseAvailability = CheckConfig{
		Category:                AuditReportClickHouse,
		Type:                    CheckTypeItemBased,
		Title:                   "ClickHouse availability",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithToBe "ClickHouse server"}} unavailable`,
		ConditionFormatTemplate: "the cluster agent can't query the server (clickhouse_up = 0)",
	}
	// The default threshold matches the default of max_replica_delay_for_distributed_queries (300s): a replica lagging
	// more is skipped by distributed queries
	// (https://clickhouse.com/docs/operations/settings/settings#max_replica_delay_for_distributed_queries).
	Checks.ClickHouseReplication = CheckConfig{
		Category:                AuditReportClickHouse,
		Type:                    CheckTypeItemBased,
		Title:                   "ClickHouse replication",
		DefaultThreshold:        300,
		Unit:                    CheckUnitSecond,
		MessageTemplate:         `replication issues on {{.Items "ClickHouse server"}}`,
		ConditionFormatTemplate: "a replicated table is read-only or lost data parts (critical), or its replication delay > <threshold>",
	}
	// Since ClickHouse 23.6, INSERTs are delayed when a partition has more than parts_to_delay_insert=1000 active
	// parts and rejected above parts_to_throw_insert=3000 (150/300 in older versions)
	// (https://clickhouse.com/docs/operations/settings/merge-tree-settings#parts_to_delay_insert).
	// The warning threshold gives time to react before the throttling starts; above 1000 the check is critical.
	Checks.ClickHouseTooManyParts = CheckConfig{
		Category:                AuditReportClickHouse,
		Type:                    CheckTypeItemBased,
		Title:                   "Too many parts",
		DefaultThreshold:        300,
		MessageTemplate:         `{{.Items "table"}} with too many active parts in a partition (max: {{.Value}})`,
		ConditionFormatTemplate: "the number of active parts in a partition > <threshold> (critical above parts_to_delay_insert = 1000, when INSERTs get throttled)",
	}
	Checks.ClickHouseStuckMutations = CheckConfig{
		Category:                AuditReportClickHouse,
		Type:                    CheckTypeItemBased,
		Title:                   "Stuck mutations",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithHave "table"}} stuck or failing mutations`,
		ConditionFormatTemplate: "a mutation (ALTER UPDATE/DELETE) fails or hasn't finished for more than an hour",
	}
	Checks.ClickHouseRejectedInserts = CheckConfig{
		Category:                AuditReportClickHouse,
		Type:                    CheckTypeEventBased,
		Title:                   "Rejected inserts",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.Count "INSERT"}} rejected with "Too many parts" in the last 5 minutes`,
		ConditionFormatTemplate: "the number of INSERTs rejected with \"Too many parts\" in the last 5 minutes > <threshold>",
	}

	Checks.ElasticsearchAvailability = CheckConfig{
		Category:                AuditReportElasticsearch,
		Type:                    CheckTypeItemBased,
		Title:                   "Elasticsearch availability",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithToBe "Elasticsearch node"}} unavailable`,
		ConditionFormatTemplate: "the cluster agent can't query the node (elasticsearch_up = 0)",
	}
	// https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-health.html:
	// yellow - all primaries are assigned but some replicas aren't; red - some primaries are unassigned.
	Checks.ElasticsearchClusterHealth = CheckConfig{
		Category:                AuditReportElasticsearch,
		Type:                    CheckTypeItemBased,
		Title:                   "Cluster health",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithToBe "cluster"}} not green`,
		ConditionFormatTemplate: "the cluster health status is yellow (warning) or red (critical)",
	}
	Checks.ElasticsearchUnassignedShards = CheckConfig{
		Category:                AuditReportElasticsearch,
		Type:                    CheckTypeItemBased,
		Title:                   "Unassigned shards",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithHave "cluster"}} unassigned shards`,
		ConditionFormatTemplate: "the number of unassigned shards (except the delayed ones) > <threshold> for 5 minutes",
	}
	// Elastic recommends keeping the JVM memory pressure below 85%: above it the GC overhead grows and
	// circuit breakers start rejecting requests
	// (https://www.elastic.co/guide/en/elasticsearch/reference/current/high-jvm-memory-pressure.html).
	Checks.ElasticsearchJvmHeap = CheckConfig{
		Category:                AuditReportElasticsearch,
		Type:                    CheckTypeItemBased,
		Title:                   "JVM heap usage",
		DefaultThreshold:        85,
		Unit:                    CheckUnitPercent,
		MessageTemplate:         `heap usage of {{.Items "node"}} over {{.ThresholdPercent}}`,
		ConditionFormatTemplate: "the JVM heap usage of a node > <threshold> for 5 minutes",
	}
	// The default disk-based shard allocation watermarks are low=85% (no new shards), high=90% (shards relocated away)
	// and flood_stage=95% (indices become read-only)
	// (https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-cluster.html#disk-based-shard-allocation).
	// The check warns at the low watermark (less than 15% available) and is critical at the flood stage (less than 5%).
	Checks.ElasticsearchDiskSpace = CheckConfig{
		Category:                AuditReportElasticsearch,
		Type:                    CheckTypeItemBased,
		Title:                   "Data disk space",
		DefaultThreshold:        85,
		Unit:                    CheckUnitPercent,
		MessageTemplate:         `{{.ItemsWithToBe "data path"}} over {{.ThresholdPercent}} full`,
		ConditionFormatTemplate: "the disk usage of a data path > <threshold> (the low watermark); critical above 95% (the flood stage, indices become read-only)",
	}
	// Rejections mean the node is overloaded and the clients get 429 errors
	// (https://www.elastic.co/guide/en/elasticsearch/reference/current/rejected-requests.html).
	Checks.ElasticsearchThreadPoolRejects = CheckConfig{
		Category:                AuditReportElasticsearch,
		Type:                    CheckTypeEventBased,
		Title:                   "Thread pool rejections",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.Count "task"}} rejected by thread pools in the last 5 minutes`,
		ConditionFormatTemplate: "the number of tasks rejected by thread pools (write, search, ...) in the last 5 minutes > <threshold>",
	}
}

func clusterTargetRule(id, name string, check CheckConfig, severity Status, forDuration timeseries.Duration, description string) AlertingRule {
	return AlertingRule{
		Id:   AlertingRuleId(id),
		Name: name,
		Source: AlertSource{
			Type:  AlertSourceTypeCheck,
			Check: &CheckSource{CheckId: check.Id},
		},
		Selector:      AppSelector{Type: AppSelectorTypeAll},
		Severity:      severity,
		For:           forDuration,
		KeepFiringFor: 5 * timeseries.Minute,
		Templates:     AlertTemplates{Description: description},
		Enabled:       true,
		Builtin:       true,
	}
}

// clusterTargetAlertingRules are the built-in rules of the cluster agent target checks. The checks that can be
// critical (see SetCritical) raise critical alerts even though the rules are created with the warning severity.
func clusterTargetAlertingRules() []AlertingRule {
	m := timeseries.Minute
	return []AlertingRule{
		clusterTargetRule("kafka-availability", "Kafka availability", Checks.KafkaAvailability, WARNING, 2*m,
			"The Kafka cluster is unreachable. Producers and consumers may be failing."),
		clusterTargetRule("kafka-offline-partitions", "Kafka offline partitions", Checks.KafkaOfflinePartitions, CRITICAL, m,
			"Some partitions have no leader: they can be neither produced to nor consumed from."),
		clusterTargetRule("kafka-under-replicated-partitions", "Kafka under-replicated partitions", Checks.KafkaUnderReplicatedPartitions, WARNING, 0,
			"Some partitions have fewer in-sync replicas than replicas. Losing another broker may make them unavailable or lose data."),
		clusterTargetRule("kafka-consumer-lag", "Kafka consumer lag", Checks.KafkaConsumerLag, WARNING, 2*m,
			"A consumer group is falling behind or has stopped consuming. Messages are processed late or not at all."),
		clusterTargetRule("clickhouse-availability", "ClickHouse availability", Checks.ClickHouseAvailability, WARNING, 2*m,
			"Some ClickHouse servers are unavailable. This may cause failures for dependent applications."),
		clusterTargetRule("clickhouse-replication", "ClickHouse replication", Checks.ClickHouseReplication, WARNING, 2*m,
			"Replicated tables are read-only, lost data parts or lag behind. INSERTs into read-only tables fail."),
		clusterTargetRule("clickhouse-too-many-parts", "ClickHouse too many parts", Checks.ClickHouseTooManyParts, WARNING, 5*m,
			"Background merges don't keep up with INSERTs. ClickHouse throttles and then rejects INSERTs into such partitions."),
		clusterTargetRule("clickhouse-stuck-mutations", "ClickHouse stuck mutations", Checks.ClickHouseStuckMutations, WARNING, 0,
			"A mutation (ALTER UPDATE/DELETE) is failing or hasn't finished for more than an hour; it blocks the merges of the affected parts."),
		clusterTargetRule("clickhouse-rejected-inserts", "ClickHouse rejected inserts", Checks.ClickHouseRejectedInserts, CRITICAL, 0,
			"INSERTs are rejected with \"Too many parts\": the data isn't written."),
		clusterTargetRule("elasticsearch-availability", "Elasticsearch availability", Checks.ElasticsearchAvailability, WARNING, 2*m,
			"Some Elasticsearch/OpenSearch nodes are unavailable. This may cause failures for dependent applications."),
		clusterTargetRule("elasticsearch-cluster-health", "Elasticsearch cluster health", Checks.ElasticsearchClusterHealth, WARNING, 2*m,
			"The cluster is yellow (some replicas are unassigned) or red (some primaries are unassigned: data is unavailable)."),
		clusterTargetRule("elasticsearch-unassigned-shards", "Elasticsearch unassigned shards", Checks.ElasticsearchUnassignedShards, WARNING, 0,
			"Some shards have been unassigned for several minutes."),
		clusterTargetRule("elasticsearch-jvm-heap", "Elasticsearch JVM heap", Checks.ElasticsearchJvmHeap, WARNING, 0,
			"The JVM heap usage of a node is high: GC overhead grows and circuit breakers may reject requests."),
		clusterTargetRule("elasticsearch-disk-space", "Elasticsearch disk space", Checks.ElasticsearchDiskSpace, WARNING, 0,
			"A data path is almost full: shards are no longer allocated to the node and at 95% the indices become read-only."),
		clusterTargetRule("elasticsearch-thread-pool-rejections", "Elasticsearch thread pool rejections", Checks.ElasticsearchThreadPoolRejects, WARNING, 0,
			"Thread pools reject tasks: the node is overloaded and clients receive 429 errors."),
	}
}

// Escalate is an alias of SetCritical: the check reports CRITICAL instead of WARNING if it fires,
// and its alerts are raised as CRITICAL even if their rule says WARNING.
func (ch *Check) Escalate() {
	ch.SetCritical()
}

package constructor

// Shards fork: Kafka metrics of the shards cluster agent (labeled with the target address).
// The per-partition metrics (perPartitionMetrics) and kafka_broker_info are not loaded: the former are
// high-cardinality and the cluster is evaluated per topic/consumer group, the latter has an "address" label
// that clashes with the target address label.

const (
	qKafkaUp                       = "kafka_up"
	qKafkaScrapeError              = "kafka_scrape_error"
	qKafkaClusterInfo              = "kafka_cluster_info"
	qKafkaControllerId             = "kafka_controller_id"
	qKafkaBrokers                  = "kafka_brokers"
	qKafkaTopicPartitions          = "kafka_topic_partitions"
	qKafkaTopicUnderReplicated     = "kafka_topic_under_replicated_partitions"
	qKafkaTopicOffline             = "kafka_topic_offline_partitions"
	qKafkaTopicProduceRate         = "kafka_topic_produce_rate"
	qKafkaConsumerGroupMembers     = "kafka_consumergroup_members"
	qKafkaConsumerGroupState       = "kafka_consumergroup_state"
	qKafkaConsumerGroupLag         = "kafka_consumergroup_lag_sum"
	qKafkaConsumerGroupConsumeRate = "kafka_consumergroup_consume_rate"
)

var kafkaQueries = []Query{
	qDB(qKafkaUp, `kafka_up`),
	qDB(qKafkaScrapeError, `kafka_scrape_error`, "error", "warning"),
	qDB(qKafkaClusterInfo, `kafka_cluster_info`, "cluster_id"),
	qDB(qKafkaControllerId, `kafka_controller_id`),
	qDB(qKafkaBrokers, `kafka_brokers`),
	qDB(qKafkaTopicPartitions, `kafka_topic_partitions`, "topic"),
	qDB(qKafkaTopicUnderReplicated, `kafka_topic_under_replicated_partitions`, "topic"),
	qDB(qKafkaTopicOffline, `kafka_topic_offline_partitions`, "topic"),
	// the sums of the end offsets only grow (rate() handles a decrease after a topic is recreated as a reset)
	qDB(qKafkaTopicProduceRate, `rate(kafka_topic_current_offset_sum[$RANGE])`, "topic"),
	qDB(qKafkaConsumerGroupMembers, `kafka_consumergroup_members`, "consumergroup"),
	qDB(qKafkaConsumerGroupState, `kafka_consumergroup_state`, "consumergroup", "state"),
	qDB(qKafkaConsumerGroupLag, `kafka_consumergroup_lag_sum`, "consumergroup", "topic"),
	qDB(qKafkaConsumerGroupConsumeRate, `rate(kafka_consumergroup_current_offset_sum[$RANGE])`, "consumergroup", "topic"),
}

func init() {
	QUERIES = append(QUERIES, kafkaQueries...)
}

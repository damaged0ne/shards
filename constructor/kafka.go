package constructor

import (
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
)

// Shards fork: Kafka metrics of the shards cluster agent.
func kafka(instance *model.Instance, queryName string, m *model.MetricValues) {
	if instance == nil {
		return
	}
	if instance.Kafka == nil {
		instance.Kafka = model.NewKafka()
	}
	k := instance.Kafka
	ls := m.Labels
	switch queryName {
	case qKafkaUp:
		k.Up = merge(k.Up, m.Values, timeseries.Any)
	case qKafkaScrapeError:
		k.Error.Update(m.Values, model.HumanizeScrapeError(ls["error"]))
		k.Warning.Update(m.Values, model.HumanizeScrapeError(ls["warning"]))
	case qKafkaClusterInfo:
		k.ClusterId.Update(m.Values, ls["cluster_id"])
	case qKafkaControllerId:
		k.ControllerId = merge(k.ControllerId, m.Values, timeseries.Any)
	case qKafkaBrokers:
		k.Brokers = merge(k.Brokers, m.Values, timeseries.Any)
	case qKafkaTopicPartitions, qKafkaTopicUnderReplicated, qKafkaTopicOffline, qKafkaTopicProduceRate:
		t := k.GetOrCreateTopic(ls["topic"])
		switch queryName {
		case qKafkaTopicPartitions:
			t.Partitions = merge(t.Partitions, m.Values, timeseries.Any)
		case qKafkaTopicUnderReplicated:
			t.UnderReplicated = merge(t.UnderReplicated, m.Values, timeseries.Any)
		case qKafkaTopicOffline:
			t.Offline = merge(t.Offline, m.Values, timeseries.Any)
		case qKafkaTopicProduceRate:
			t.ProduceRate = merge(t.ProduceRate, m.Values, timeseries.Any)
		}
	case qKafkaConsumerGroupMembers:
		g := k.GetOrCreateConsumerGroup(ls["consumergroup"])
		g.Members = merge(g.Members, m.Values, timeseries.Any)
	case qKafkaConsumerGroupState:
		g := k.GetOrCreateConsumerGroup(ls["consumergroup"])
		g.State.Update(m.Values, ls["state"])
	case qKafkaConsumerGroupLag:
		t := k.GetOrCreateConsumerGroup(ls["consumergroup"]).GetOrCreateTopic(ls["topic"])
		t.Lag = merge(t.Lag, m.Values, timeseries.Any)
	case qKafkaConsumerGroupConsumeRate:
		t := k.GetOrCreateConsumerGroup(ls["consumergroup"]).GetOrCreateTopic(ls["topic"])
		t.ConsumeRate = merge(t.ConsumeRate, m.Values, timeseries.Any)
	}
}

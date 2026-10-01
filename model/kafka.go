package model

import (
	"sort"

	"github.com/coroot/coroot/timeseries"
)

// Shards fork: Kafka cluster metrics collected by the shards cluster agent (kafka_*).
// A Kafka target describes a whole cluster, so the topics and consumer groups belong to the instance
// representing the target address.

type KafkaTopic struct {
	Partitions      *timeseries.TimeSeries
	UnderReplicated *timeseries.TimeSeries
	Offline         *timeseries.TimeSeries
	ProduceRate     *timeseries.TimeSeries // messages/second: rate(kafka_topic_current_offset_sum)
}

type KafkaConsumerGroupTopic struct {
	Lag         *timeseries.TimeSeries // messages
	ConsumeRate *timeseries.TimeSeries // messages/second: rate(kafka_consumergroup_current_offset_sum)
}

type KafkaConsumerGroup struct {
	Members *timeseries.TimeSeries
	State   LabelLastValue
	Topics  map[string]*KafkaConsumerGroupTopic
}

func (g *KafkaConsumerGroup) sum(f func(t *KafkaConsumerGroupTopic) *timeseries.TimeSeries) *timeseries.TimeSeries {
	agg := timeseries.NewAggregate(timeseries.NanSum)
	for _, t := range g.Topics {
		agg.Add(f(t))
	}
	return agg.Get()
}

// Lag is the sum of the lag of the group over all its topics, in messages.
func (g *KafkaConsumerGroup) Lag() *timeseries.TimeSeries {
	return g.sum(func(t *KafkaConsumerGroupTopic) *timeseries.TimeSeries { return t.Lag })
}

// ConsumeRate is the rate at which the group commits offsets over all its topics, in messages/second.
func (g *KafkaConsumerGroup) ConsumeRate() *timeseries.TimeSeries {
	return g.sum(func(t *KafkaConsumerGroupTopic) *timeseries.TimeSeries { return t.ConsumeRate })
}

type Kafka struct {
	Up      *timeseries.TimeSeries
	Error   LabelLastValue
	Warning LabelLastValue

	ClusterId    LabelLastValue
	ControllerId *timeseries.TimeSeries
	Brokers      *timeseries.TimeSeries

	Topics         map[string]*KafkaTopic
	ConsumerGroups map[string]*KafkaConsumerGroup
}

func NewKafka() *Kafka {
	return &Kafka{
		Topics:         map[string]*KafkaTopic{},
		ConsumerGroups: map[string]*KafkaConsumerGroup{},
	}
}

func (k *Kafka) IsUp() bool {
	return k.Up.Last() > 0
}

// ReportsClusterMetrics is false for the targets that only report kafka_up because another
// broker of the same cluster reports the cluster-wide metrics (clusterMetrics: lowest-broker).
func (k *Kafka) ReportsClusterMetrics() bool {
	return !k.Brokers.IsEmpty() || len(k.Topics) > 0 || len(k.ConsumerGroups) > 0
}

func (k *Kafka) GetOrCreateTopic(name string) *KafkaTopic {
	t := k.Topics[name]
	if t == nil {
		t = &KafkaTopic{}
		k.Topics[name] = t
	}
	return t
}

func (k *Kafka) GetOrCreateConsumerGroup(name string) *KafkaConsumerGroup {
	g := k.ConsumerGroups[name]
	if g == nil {
		g = &KafkaConsumerGroup{Topics: map[string]*KafkaConsumerGroupTopic{}}
		k.ConsumerGroups[name] = g
	}
	return g
}

func (g *KafkaConsumerGroup) GetOrCreateTopic(name string) *KafkaConsumerGroupTopic {
	t := g.Topics[name]
	if t == nil {
		t = &KafkaConsumerGroupTopic{}
		g.Topics[name] = t
	}
	return t
}

// ProduceRate is the produce rate of the topics consumed by the group, in messages/second.
func (k *Kafka) ProduceRate(g *KafkaConsumerGroup) *timeseries.TimeSeries {
	agg := timeseries.NewAggregate(timeseries.NanSum)
	for name := range g.Topics {
		if t := k.Topics[name]; t != nil {
			agg.Add(t.ProduceRate)
		}
	}
	return agg.Get()
}

func (k *Kafka) TopicNames() []string {
	res := make([]string, 0, len(k.Topics))
	for name := range k.Topics {
		res = append(res, name)
	}
	sort.Strings(res)
	return res
}

func (k *Kafka) ConsumerGroupNames() []string {
	res := make([]string, 0, len(k.ConsumerGroups))
	for name := range k.ConsumerGroups {
		res = append(res, name)
	}
	sort.Strings(res)
	return res
}

---
sidebar_position: 6
---

# Kafka

shards monitors Apache Kafka clusters (and Kafka API compatible systems such as Redpanda and Amazon MSK) through the
Kafka admin API: the [shards cluster agent](../configuration/shards-cluster.md) connects to the cluster over the network,
so nothing has to be installed next to the brokers.

A Kafka target describes a whole cluster: the brokers, topics and consumer groups are discovered from the cluster metadata.

## Configuration

### Docker Compose and VMs

Add one entry per cluster to the static configuration of the cluster agent (`--config-file`):

```yaml
databases:
  - type: kafka
    host: kafka-1              # a bootstrap broker
    port: "9092"
    credentials:               # optional: enables SASL
      username: shards
      password: ${KAFKA_PASSWORD}
    params:
      brokers: kafka-2:9092,kafka-3:9092   # additional bootstrap brokers (optional)
      sasl: scram-sha-512                  # plain (default with credentials), scram-sha-256, scram-sha-512, aws-msk-iam
      tls: "true"
      excludeConsumerGroups: console-consumer-.*
```

### Kubernetes

Annotate the broker pods:

```yaml
coroot.com/kafka-scrape: "true"
coroot.com/kafka-scrape-port: "9092"
```

Every annotated broker becomes a target, but only the broker with the lowest node ID reports the cluster-wide metrics;
the others report only their availability.

The monitoring user needs the `Describe` permission on the cluster, the topics and the consumer groups.
See the cluster agent README for all the parameters (topic and consumer group filters, TLS, limits).

## Where the data shows up

The metrics are attached to the application listening on the target address: the Kafka container or pod when the
node agent sees it, or the external service the applications connect to. A target that matches no known application
(e.g. a Kafka cluster on VMs without the node agent) gets an application of its own in the `external` namespace,
named after the target address.

The **Kafka** inspection of the application shows:

- **Cluster**: the availability of each target, the cluster ID, the number of brokers and the controller.
- **Topics**: partitions, under-replicated and offline partitions, and the produce rate (messages/second, from the
  growth of the end offsets) per topic.
- **Consumer groups**: state, members, lag (messages) with its trend, consume rate (from the growth of the committed
  offsets) and the estimated time lag (`lag / consume rate`), plus charts of the top consumer groups by lag.

## Checks

| Check | Default | Condition |
|---|---|---|
| Kafka availability | | The cluster is unreachable through the target (`kafka_up = 0`); the reason (authentication, TLS, timeout, ...) is shown in the details |
| Offline partitions | critical | A partition has no leader |
| Under-replicated partitions | 5 minutes | A topic has partitions with fewer in-sync replicas than replicas for longer than the threshold |
| Consumer lag | 5 minutes | See below |

The consumer lag is evaluated the way [Burrow](https://github.com/linkedin/Burrow/wiki/Consumer-Lag-Evaluation-Rules)
does it: a large lag is fine as long as the group keeps up. A consumer group is reported when:

- **warning**: its lag increased in at least 80% of the steps of the last 15 minutes while it consumes slower than the
  producers write, or its estimated time lag is above the threshold (5 minutes by default);
- **critical**: it has lag but hasn't committed offsets for 10 minutes (stalled), or it has lag while its state is
  `Empty` or `Dead` (no consumers).

A consumer group that is no longer used but still has lag is reported as critical: exclude such groups with the
`consumerGroups`/`excludeConsumerGroups` parameters of the target.

Each check has a built-in alerting rule. Checks that detect a critical condition raise critical alerts.

---
sidebar_position: 7
---

# RabbitMQ

RabbitMQ exposes its metrics in the Prometheus format through the built-in
[`rabbitmq_prometheus`](https://www.rabbitmq.com/docs/prometheus) plugin (enabled by default since RabbitMQ 3.8, port `15692`).
shards-cluster scrapes this endpoint natively (no exporter runs in the agent) and keeps only an allowlist of the node-level
and aggregated metrics. The scraped series get the `job="rabbitmq"` and `instance="<host>:<port>"` labels, plus `namespace`
and `pod` for annotated pods.

shards attaches the metrics to the RabbitMQ application detected by the node agent: by the pod (Kubernetes), by the
IP address of the instance, or, for a statically configured host name, by an application or instance with the same name
(e.g. the `rabbitmq` service of a Docker Compose project).

## Configuration

Kubernetes pod annotations:

```yaml
metadata:
  annotations:
    coroot.com/rabbitmq-scrape: "true"
    coroot.com/rabbitmq-scrape-port: "15692"             # optional
    coroot.com/rabbitmq-scrape-scheme: "http"            # optional, http or https
    coroot.com/rabbitmq-scrape-metrics-path: "/metrics"  # optional
```

Static configuration in the cluster-agent [configuration file](/configuration/shards-cluster#configuration-file):

```yaml
databases:
  - type: rabbitmq
    host: rabbitmq.example.internal
    port: "15692"
    credentials:          # optional, HTTP basic auth
      username: monitoring
      password: ${RABBITMQ_MONITORING_PASSWORD}
```

Use the aggregated `/metrics` endpoint: the per-object endpoints (`/metrics/per-object`, `/metrics/detailed`) grow with
the number of queues and connections. If per-object metrics are enabled anyway, shards sums the queue metrics over `vhost`/`queue`.

## What is shown

| Metrics | Shown in the RabbitMQ report as |
|---|---|
| `up{job="rabbitmq"}`, `rabbitmq_build_info`, `rabbitmq_identity_info` | Node status and version |
| `rabbitmq_queue_messages_ready`, `rabbitmq_queue_messages_unacked` | Queued messages |
| `rabbitmq_global_messages_received_total`, `rabbitmq_global_messages_delivered_total`, `rabbitmq_global_messages_unroutable_dropped_total` | Message rates (published, delivered, unroutable) |
| `rabbitmq_consumers`, `rabbitmq_connections`, `rabbitmq_queues` | Consumers, connections |
| `rabbitmq_process_resident_memory_bytes`, `rabbitmq_resident_memory_limit_bytes` | Memory usage vs the high watermark |
| `rabbitmq_disk_space_available_bytes`, `rabbitmq_disk_space_available_limit_bytes` | Free disk space vs the low watermark |
| `rabbitmq_process_open_fds`, `rabbitmq_process_max_fds` | File descriptors |
| `rabbitmq_alarms_memory_used_watermark`, `rabbitmq_alarms_free_disk_space_watermark`, `rabbitmq_alarms_file_descriptor_limit` | Resource alarms |
| `rabbitmq_unreachable_cluster_peers_count` | Node status (unreachable peers) |

The channel, connection churn and other `rabbitmq_global_messages_*` counters of the allowlist are collected but not shown.

## Checks and built-in alerts

| Check | Default condition | Built-in alert |
|---|---|---|
| RabbitMQ availability | the metrics endpoint of a node can't be scraped (`up = 0`) | warning, after 2 minutes |
| RabbitMQ resource alarms | a memory, disk or file descriptor alarm is in effect | **critical** |
| RabbitMQ file descriptors | more than 90% of the file descriptor limit is used | warning, after 5 minutes |

While a [memory or disk alarm](https://www.rabbitmq.com/docs/alarms) is in effect, RabbitMQ blocks all the connections
that publish messages until the alarm clears.

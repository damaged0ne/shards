---
sidebar_position: 8
---

# Elasticsearch and OpenSearch

shards monitors Elasticsearch and OpenSearch clusters with the [shards cluster agent](../configuration/shards-cluster.md),
which reads the cluster health, the node stats and the index stats over the REST API.

## Configuration

The monitoring user needs the `monitor` cluster privilege and `monitor` on the indices to report.

### Docker Compose and VMs

Add the nodes to the static configuration of the cluster agent (`--config-file`):

```yaml
databases:
  - type: elasticsearch       # or "opensearch"
    host: es
    port: "9200"
    credentials:
      username: shards
      password: ${ES_PASSWORD}
    params:
      tls: "true"             # https; "skip-verify" disables certificate verification
      tlsCaFile: /etc/shards/es-ca.pem
      nodes: _local           # the stats of the node at the address (default), or "_all"
      topIndices: "100"       # max indices with per-index metrics (the largest ones)
```

With `nodes: _local`, add every node as a target; with `nodes: _all`, one target reports the stats of all the nodes.

### Kubernetes

```yaml
coroot.com/elasticsearch-scrape: "true"
coroot.com/elasticsearch-scrape-port: "9200"
coroot.com/elasticsearch-scrape-param-tls: skip-verify
```

## Where the data shows up

The metrics are attached to the application listening on the target address (the Elasticsearch container or pod, or
the external service the applications connect to). A target that matches no known application gets an application of
its own in the `external` namespace, named after the target address.

The **Elasticsearch** inspection shows:

- **Cluster**: the health status, nodes and data nodes, active/relocating/initializing/unassigned shards and pending tasks.
- **Nodes**: JVM heap usage, GC time, data disk usage, CPU, indexing and search rates and latencies, thread pool
  rejections and queues, tripped circuit breakers.
- **Indices**: health, documents, size (total and primaries) and shards per index.

## Checks

| Check | Default | Condition |
|---|---|---|
| Elasticsearch availability | | The cluster agent can't query the node |
| Cluster health | | The cluster is yellow (some replicas are unassigned) — warning, or red (some primaries are unassigned) — critical |
| Unassigned shards | 0 | Shards stay unassigned for 5 minutes (shards whose allocation is delayed after a node left are not counted) |
| JVM heap usage | 85% | The heap usage of a node stays above the threshold for 5 minutes ([Elastic recommends](https://www.elastic.co/guide/en/elasticsearch/reference/current/high-jvm-memory-pressure.html) keeping it below 85%) |
| Data disk space | 85% | The usage of a data path is above the threshold (less than 15% available) — warning; above 95% (less than 5% available) — critical. Elasticsearch stops allocating shards to a node at the low watermark (85%), moves shards away at the high watermark (90%) and makes the indices read-only at the flood stage (95%) |
| Thread pool rejections | 0 | Thread pools (write, search, ...) rejected tasks in the last 5 minutes: the node is overloaded and the clients get 429 errors |

Each check has a built-in alerting rule. Checks that detect a critical condition raise critical alerts.

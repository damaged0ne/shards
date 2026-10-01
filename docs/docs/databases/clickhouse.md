---
sidebar_position: 7
---

# ClickHouse

shards monitors ClickHouse servers with the [shards cluster agent](../configuration/shards-cluster.md), which queries
the `system` tables over the network. This page is about monitoring your own ClickHouse servers; to use ClickHouse as
the storage of shards, see [ClickHouse](../configuration/clickhouse.md).

## Configuration

Create a monitoring user that can read the system tables:

```sql
CREATE USER shards IDENTIFIED BY '<PASSWORD>';
GRANT SELECT ON system.* TO shards;
```

### Docker Compose and VMs

Add one entry per server to the static configuration of the cluster agent (`--config-file`):

```yaml
databases:
  - type: clickhouse
    host: clickhouse
    port: "9000"              # native protocol; use 8123 with protocol: http
    credentials:
      username: shards
      password: ${CLICKHOUSE_PASSWORD}
    params:
      topTables: "100"        # max tables with per-table metrics (the largest ones)
      queryLog: "true"        # top queries from system.query_log
      topQueries: "20"
```

### Kubernetes

```yaml
coroot.com/clickhouse-scrape: "true"
coroot.com/clickhouse-scrape-port: "9000"
coroot.com/clickhouse-scrape-credentials-secret-name: clickhouse-monitoring
coroot.com/clickhouse-scrape-credentials-secret-username-key: username
coroot.com/clickhouse-scrape-credentials-secret-password-key: password
```

## Where the data shows up

The metrics are attached to the application listening on the target address (the ClickHouse container or pod, or the
external service the applications connect to). A target that matches no known application gets an application of its
own in the `external` namespace, named after the target address.

The **ClickHouse** inspection shows:

- **Instances**: availability, version, uptime, queries and failed queries per second, running queries, memory, the max
  number of active parts in a partition and the replication delay.
- **Top queries**: the queries from `system.query_log` by total time, with calls, average latency, rows read and errors
  (the query text is normalized and obfuscated).
- **Inserts**: inserted rows/bytes, delayed and rejected INSERTs.
- **Merges and parts**: running merges and mutations, max parts per partition; a per-table view with size, parts,
  replication state and mutations.
- **Replication**: read-only replicated tables, replication delay and queue size.
- **ZooKeeper/Keeper**: sessions and exceptions; and the top errors from `system.errors`.

## Checks

| Check | Default | Condition |
|---|---|---|
| ClickHouse availability | | The cluster agent can't query the server |
| ClickHouse replication | 300 seconds | A replicated table is read-only (e.g. after losing the ZooKeeper/Keeper session) or data parts were lost — critical; or the replication delay is above the threshold — warning. The default matches `max_replica_delay_for_distributed_queries`, beyond which distributed queries skip the replica |
| Too many parts | 300 | The number of active parts in a partition is above the threshold; critical above 1000, where ClickHouse starts throttling INSERTs (`parts_to_delay_insert`, 1000 by default since 23.6; INSERTs are rejected at `parts_to_throw_insert` = 3000) |
| Stuck mutations | | A mutation (`ALTER ... UPDATE/DELETE`) is failing or hasn't finished for more than an hour |
| Rejected inserts | 0 | INSERTs were rejected with "Too many parts" in the last 5 minutes |

Each check has a built-in alerting rule. Checks that detect a critical condition raise critical alerts.

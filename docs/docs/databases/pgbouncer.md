---
sidebar_position: 6
---

# PgBouncer

shards-cluster collects the metrics of PgBouncer from its admin console (the virtual `pgbouncer` database) with
`SHOW STATS`, `SHOW POOLS` and `SHOW LISTS`, using the metric names of
[pgbouncer_exporter](https://github.com/prometheus-community/pgbouncer_exporter). The metrics are attached to the
application running PgBouncer (detected by the node agent) and shown on its **PgBouncer** tab.

## Prerequisites

The monitoring user must be allowed to use the admin console: list it in `stats_users` (or `admin_users`) of `pgbouncer.ini`.
The admin console only supports the simple query protocol; the agent never prepares statements, so
`ignore_startup_parameters` doesn't need to be changed.

## Configuration

Kubernetes pod annotations:

```yaml
metadata:
  annotations:
    coroot.com/pgbouncer-scrape: "true"
    coroot.com/pgbouncer-scrape-port: "6432"   # optional, default 6432
    coroot.com/pgbouncer-scrape-credentials-secret-name: pgbouncer-stats
    coroot.com/pgbouncer-scrape-credentials-secret-username-key: username
    coroot.com/pgbouncer-scrape-credentials-secret-password-key: password
```

Static configuration (Docker Compose, VMs), in the cluster-agent [configuration file](/configuration/shards-cluster#configuration-file):

```yaml
databases:
  - type: pgbouncer
    host: pgbouncer.example.internal
    port: "6432"
    credentials:
      username: stats
      password: ${PGBOUNCER_STATS_PASSWORD}
    params:
      sslmode: disable
```

## What is shown

* **Instances**: status of the admin console, number of pools, active/waiting clients, active/idle servers, the longest
  client wait, queries per second and the average query duration.
* **Pools** (`pgbouncer_pools_*{database,user}`): client active/waiting connections, server active/idle/used connections
  and `maxwait` (the age of the oldest waiting client) per pool.
* **Queries** (`pgbouncer_stats_*{database}`): queries and transactions per second, average query duration
  (`queries_duration_seconds_total / queries_pooled_total`) and the client wait time per database.

`pgbouncer_stats_{received,sent}_bytes_total`, `server_in_transaction_seconds_total`, `server_assignments_total`,
the cancel-related pool gauges and the `SHOW LISTS` counters are collected but not shown.

## Checks and built-in alerts

| Check | Default condition | Built-in alert |
|---|---|---|
| PgBouncer availability | the admin console is unreachable (`pgbouncer_up = 0`) | warning, after 2 minutes |
| PgBouncer client wait time | the oldest waiting client of a pool (`maxwait`, averaged over 2 minutes) has waited more than 1s; **critical** above 5x the threshold (5s) | warning (raised to critical above 5s), after 2 minutes |
| PgBouncer pool saturation | clients have been waiting for 5 minutes while no server connection of the pool was idle | warning |

`maxwait` is the age of the oldest client in the queue; according to the [PgBouncer documentation](https://www.pgbouncer.org/usage.html)
an increasing value means that the pool can't serve the requests quickly enough, either because the server is
overloaded or because `pool_size` is too small. A persistent queue with no idle server connection means the pool can't
grow any further: increase `default_pool_size`/`pool_size` (if the server can take more connections) or make the queries faster.

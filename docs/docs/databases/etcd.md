---
sidebar_position: 8
---

# etcd

etcd exposes its metrics in the Prometheus format. shards-cluster scrapes them natively (no exporter runs in the agent)
and keeps only an allowlist: leadership, proposals, database size, the WAL fsync and backend commit latency histograms,
peer round-trip time and traffic, gRPC traffic and process resources. The scraped series get the `job="etcd"` and
`instance="<host>:<port>"` labels, plus `namespace` and `pod` for annotated pods.

shards attaches the metrics to the etcd application detected by the node agent: by the pod (Kubernetes), by the IP
address of the member, or, for a statically configured host name, by an application or instance with the same name.

## Configuration

etcd serves `/metrics` on its client URLs (usually TLS with client certificates) and, if `--listen-metrics-urls` is set
(e.g. `http://0.0.0.0:2381`, the kubeadm default), without authentication on a separate port.

Kubernetes pod annotations (plain HTTP metrics port):

```yaml
metadata:
  annotations:
    coroot.com/etcd-scrape: "true"
    coroot.com/etcd-scrape-port: "2381"   # optional, default 2381
```

Static configuration in the cluster-agent [configuration file](/configuration/shards-cluster#configuration-file),
e.g. through the client port with client certificates:

```yaml
databases:
  - type: etcd
    host: etcd-0.example.internal
    port: "2379"
    params:
      tls_ca_file: /etc/etcd/pki/ca.crt
      tls_cert_file: /etc/etcd/pki/client.crt
      tls_key_file: /etc/etcd/pki/client.key
```

## What is shown

| Metrics | Shown in the etcd report as |
|---|---|
| `up{job="etcd"}`, `etcd_server_version` | Member status and version |
| `etcd_server_has_leader`, `etcd_server_is_leader` | Leader status and the member role (leader/follower) |
| `etcd_server_leader_changes_seen_total` | Leader changes |
| `etcd_server_proposals_{applied,failed}_total`, `etcd_server_proposals_pending` | Proposals |
| `etcd_disk_wal_fsync_duration_seconds_bucket`, `etcd_disk_backend_commit_duration_seconds_bucket` | WAL fsync and backend commit duration p99 |
| `etcd_network_peer_round_trip_time_seconds_bucket` | Peer round-trip time p99 |
| `etcd_mvcc_db_total_size_in_bytes`, `etcd_mvcc_db_total_size_in_use_in_bytes`, `etcd_server_quota_backend_bytes` | Database size vs the space quota (2 GiB if not reported) |

The MVCC operation counters, peer and gRPC traffic, `grpc_server_handled_total` and the process metrics of the allowlist
are collected but not shown.

## Checks and built-in alerts

| Check | Default condition | Built-in alert |
|---|---|---|
| etcd availability | the metrics endpoint of a member can't be scraped | warning, after 2 minutes |
| etcd leader | a member reports no leader (`etcd_server_has_leader = 0`) | **critical**, after 1 minute |
| etcd leader changes | more than 3 leader changes over the selected period (the last hour for alerts), or failed proposals over the last 5 minutes | warning |
| etcd disk latency | p99 WAL fsync duration above 10ms, or p99 backend commit duration above 25ms (2.5x the threshold), averaged over 10 minutes | warning, after 10 minutes |
| etcd database size | the backend database is above 80% of the space quota | warning, after 5 minutes |

The disk latency thresholds follow the etcd recommendations: the 99th percentile of `etcd_disk_wal_fsync_duration_seconds`
should be below 10ms and that of `etcd_disk_backend_commit_duration_seconds` below 25ms
(see the [etcd hardware recommendations](https://etcd.io/docs/v3.5/op-guide/hardware/) and
[Recommended etcd practices](https://docs.redhat.com/en/documentation/openshift_container_platform/4.5/html/scalability_and_performance/recommended-etcd-practices_)).
Slow disks make members miss heartbeats, which leads to leader elections and request timeouts. Once the database
exceeds the quota, etcd raises a `NOSPACE` alarm and only accepts reads and deletes until it is compacted and defragmented.

---
sidebar_position: 1.5
toc_max_heading_level: 2
---

# Node-agent: shards additions

[shards-node-agent](https://github.com/damaged0ne/shards-node-agent) exports every metric described on the [Node-agent](/metrics/node-agent) page,
plus the metrics and options below, which are specific to the shards fork. Fork-specific code lives in `*shards*.go` files of the agent repository.

## Options

| Flag (environment variable) | Description |
|---|---|
| `--hostname-override` (`HOSTNAME_OVERRIDE`) | Hostname reported in `node_info`, logs, traces and profiles instead of the host's UTS hostname. |
| `--compose-grouping` (`COMPOSE_GROUPING`, on by default) | Docker Compose containers are reported as `/swarm/<project>/<service>/<number>`, so shards shows one application per service (namespace = project) instead of one per replica. One-off `docker compose run` containers become `/swarm/<project>/<service>-run/<suffix>`. `--no-compose-grouping` restores `/docker/<name>`. |
| `shards.ignore=true` Docker label | The container isn't monitored. |
| `--container-labels` (`CONTAINER_LABELS`) | Docker labels exported by `shards_container_labels{container_id,label_<name>...}` (names are sanitized, e.g. `team` becomes `label_team`). |
| `--release-window` (`RELEASE_WINDOW`, default `30m`) | Length of the release window after a container is (re)created. The `shards.release-window` label overrides it per service (`"2h"`, `"0"` disables). |
| `--disable-nftables-monitoring`, `--disable-fail2ban-monitoring` | Turn off the nftables and fail2ban collectors. |
| `--fail2ban-db` | Path to the fail2ban database on the host (default `/var/lib/fail2ban/fail2ban.sqlite3`). |

## Filesystem and load

| Metric | Description |
|---|---|
| `shards_fs_size_bytes`, `shards_fs_avail_bytes` | Filesystem size and space available to non-root users, per host mount (labels `mount`, `device`, `fs`). |
| `shards_fs_files`, `shards_fs_files_free` | Total and free inodes per host mount. |
| `shards_fs_readonly` | 1 if the mount is read-only. |
| `shards_load1`, `shards_load5`, `shards_load15` | Load averages. |

## Docker and Docker Compose containers

| Metric | Description |
|---|---|
| `shards_container_state{container_id,state}` | Docker state: `running`, `exited`, `restarting`, `paused`, `created`, `dead`, `removing` (includes stopped containers). |
| `shards_container_health{container_id,status}` | Docker healthcheck status (`healthy`, `unhealthy`, `starting`) of running containers with a healthcheck. |
| `shards_container_exit_code`, `shards_container_oom_killed` | Result of the last run of a container that isn't running. |
| `shards_container_started_seconds`, `shards_container_finished_seconds` | Unix time of the last start and finish. |
| `shards_container_docker_restarts`, `shards_container_restart_policy{policy}` | dockerd restart count and restart policy. |
| `shards_container_image_info{container_id,image,image_id,version,revision}` | Image, with version and revision from the `org.opencontainers.image.*` labels. |
| `shards_container_labels{container_id,label_<name>...}` | Docker labels selected with `--container-labels`. |
| `shards_compose_info{container_id,project,service}` | Docker Compose project and service of the container. |
| `shards_container_created_seconds` | Unix time the container was created. Compose recreates containers only on image or config changes; restarts and reboots keep it. |
| `shards_release_window{container_id,version,image_id}` | Present during the release window after a container is (re)created; the value is the number of seconds left. One-off `compose run` containers are excluded. `version` comes from the OCI version label, then the image tag, then the short image id. |

## Firewall and intrusion prevention

| Metric | Description |
|---|---|
| `shards_nft_counter_bytes_total`, `shards_nft_counter_packets_total` (`family`, `table`, `counter`) | Named nftables counters, read over netlink in the host network namespace. |
| `shards_nft_rule_bytes_total`, `shards_nft_rule_packets_total` (`family`, `table`, `chain`, `comment`) | nftables rules that have both a `counter` and a `comment` (rules sharing a comment in a chain are summed). |
| `shards_f2b_up` | 1 if the fail2ban database could be read (absent when fail2ban isn't installed). |
| `shards_f2b_banned{jail}`, `shards_f2b_bans_1h{jail}` | Currently banned IPs and bans issued during the last hour, per enabled jail (fail2ban >= 0.11). |

## Agent self-metrics

| Metric | Description |
|---|---|
| `node_agent_info` | Agent version. |
| `node_agent_ebpf_lost_samples_total{buffer}` | eBPF perf buffer samples lost because the buffer was full. If it grows, increase `--ebpf-perf-buffer-scale`. |
| `node_agent_ebpf_decode_errors_total{buffer}` | eBPF events that could not be decoded. |
| `node_agent_events_queue_length` | eBPF events waiting to be processed. |
| `node_agent_recovered_panics_total{component}` | Panics recovered by the agent. |
| `node_agent_remote_write_failures_total` | Failed attempts to send metrics to the collector. |

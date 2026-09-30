<p align="center">
  <img src="front/public/brand/icon.svg" width="96" height="96" alt="shards">
</p>

<h1 align="center">shards</h1>

<p align="center">
  Self-hosted observability, alerting and incident center, operable by people and by AI operator agents.<br>
  Maintained by <a href="https://github.com/damaged0ne">damaged0ne</a>.
</p>

<p align="center">
  <a href="https://github.com/damaged0ne/shards/actions/workflows/ci.yml"><img src="https://github.com/damaged0ne/shards/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License: Apache-2.0"></a>
</p>

## What it does

- Collects metrics, logs, traces and profiles from Linux hosts and containers with eBPF agents, no application changes required.
- Builds a live service map from observed network traffic and runs built-in health checks for every application, node and database.
- Tracks SLOs, opens incidents when they burn, and fires alerts from check, log-pattern, Kubernetes-event and PromQL rules.
- Sends notifications to Slack, Microsoft Teams, PagerDuty, Opsgenie or any webhook.
- Keeps an incident and alert timeline with comments, so people and agents can see what was tried and why.
- Exposes the same data and workflow over MCP and a REST API: agents can triage, comment, edit alerting rules and resolve alerts with a comment.
- Stores telemetry in ClickHouse and metrics in Prometheus (or ClickHouse); runs on a single Docker host, Docker Swarm, systemd or Kubernetes.

## Components

| Repository | Role |
|---|---|
| [damaged0ne/shards](https://github.com/damaged0ne/shards) | Server: UI, API, MCP endpoint, alerting and incident workflow. |
| [damaged0ne/shards-node-agent](https://github.com/damaged0ne/shards-node-agent) | Runs on every host. eBPF-based metrics, logs, traces and profiles for the host and its containers. |
| [damaged0ne/shards-cluster](https://github.com/damaged0ne/shards-cluster) | One per cluster. Database metrics, Kubernetes state and events, cloud integrations. |

Additions in shards-node-agent:

- Docker and Docker Compose container state, health, restarts, exit codes and OOM kills, including stopped containers.
- Release windows after a container is (re)created, with image version and revision.
- Filesystem size, free space and inodes per mount; load averages.
- nftables counters and fail2ban jails.
- `--hostname-override` for hosts whose UTS hostname is not meaningful.
- Agent self-metrics (lost eBPF samples, event queue length, recovered panics, remote write failures).

The full list is in [docs/docs/metrics/shards-node-agent.md](docs/docs/metrics/shards-node-agent.md).

## Quick start

With Docker Compose (server, both agents, Prometheus and ClickHouse on one host):

```bash
git clone https://github.com/damaged0ne/shards.git
cd shards
docker compose -f deploy/docker-compose.yaml up -d
```

Open http://localhost:8080 and set the admin password.

The compose file uses `ghcr.io/damaged0ne/shards`, `ghcr.io/damaged0ne/shards-node-agent` and `ghcr.io/damaged0ne/shards-cluster`,
published by the release workflows of the three repositories. To use a local build of the server instead:

```bash
docker build -t ghcr.io/damaged0ne/shards .
```

Other options (Docker Swarm, systemd via `deploy/install.sh`, Kubernetes) are described in [the documentation](docs/docs/installation).

## Operator agents

AI agents connect to `https://<your-shards>/mcp` with OAuth or a service-account API key and work through alerts and incidents
the way an on-call engineer would: read the timeline, investigate, leave comments, tune alerting rules, and resolve or suppress alerts with a comment.
Every action goes through the normal RBAC and is attributed to the agent's account.
See [docs/docs/agents/operator-agents.md](docs/docs/agents/operator-agents.md) and the tool list in [docs/docs/mcp/overview.md](docs/docs/mcp/overview.md).

## Development

Requirements: Go 1.25 and Node.js 24.

```bash
# UI: builds into ./static, which the server embeds (use `npm run build-dev` for a watcher)
(cd front && npm ci && npm run build-prod)

# server (needs ./static from the UI build)
go build -o shards .
./shards --data-dir=./data --bootstrap-prometheus-url=http://127.0.0.1:9090 --bootstrap-clickhouse-address=127.0.0.1:9000

# tests and linters
make test
make lint
```

A development container: `docker build -f dev.dockerfile -t shards-dev .` More details in [CONTRIBUTING.md](CONTRIBUTING.md).

The documentation site lives in [docs/](docs) (`npm ci && npm run build`).

## Roadmap

- shards-cluster: new collection targets for Kafka, ClickHouse and Elasticsearch (in progress).

## Origins & license

shards is derived from [Coroot](https://github.com/coroot/coroot) (Copyright 2020-present Coroot, Inc.) and, like it,
is licensed under the [Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.
shards is an independent project and is not affiliated with or endorsed by Coroot, Inc.; "Coroot" is a trademark of Coroot, Inc.

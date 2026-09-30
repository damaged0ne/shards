---
sidebar_position: 6
---

# Docker

:::note Container images
shards images are built from the project repositories:
[`ghcr.io/damaged0ne/shards`](https://github.com/damaged0ne/shards) (see the `Dockerfile` in the repository root),
[`ghcr.io/damaged0ne/shards-node-agent`](https://github.com/damaged0ne/shards-node-agent) and
[`ghcr.io/damaged0ne/shards-cluster`](https://github.com/damaged0ne/shards-cluster).
Images are published by the release workflows of these repositories. If they are not available in your environment yet,
build them locally (for example, `docker build -t ghcr.io/damaged0ne/shards .`) or push them to your own registry and adjust the image references.
:::

## Install Docker Compose

Use the following commands to install Docker Compose on Ubuntu:

```bash
apt update
apt install docker-compose-v2
```

## Deploy shards

To deploy shards using Docker Compose, run the following command. Before applying it, you can review the configuration file in the shards GitHub repository: [docker-compose.yaml](https://github.com/damaged0ne/shards/blob/main/deploy/docker-compose.yaml)

```bash
curl -fsS https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/docker-compose.yaml | \
  docker compose -f - up -d
```

The compose file already disables most ClickHouse system log tables (they are not needed by shards and would consume a lot of disk space).

## Validate the deployment

Ensure that the shards containers are running by executing the following command:

```bash
docker ps
```

You should see an output similar to this if the deployment is successful:

```bash
CONTAINER ID   IMAGE                                  COMMAND                  CREATED         STATUS         PORTS                                                 NAMES
b018f1cf6e09   ghcr.io/damaged0ne/shards-cluster      "coroot-cluster-agen…"   5 seconds ago   Up 3 seconds                                                         shards-cluster-agent-1
10b4bc2eef63   ghcr.io/damaged0ne/shards              "/usr/bin/shards --d…"   5 seconds ago   Up 3 seconds   0.0.0.0:8080->8080/tcp, :::8080->8080/tcp             shards-shards-1
d0143aea889b   clickhouse/clickhouse-server:24.3      "/entrypoint.sh"         5 seconds ago   Up 4 seconds   8123/tcp, 9009/tcp, 127.0.0.1:9000->9000/tcp          shards-clickhouse-1
4cbae2f36c1c   ghcr.io/damaged0ne/shards-node-agent   "coroot-node-agent -…"   5 seconds ago   Up 4 seconds                                                         shards-node-agent-1
a6618978d560   prom/prometheus:v2.53.5                "/bin/prometheus --c…"   5 seconds ago   Up 4 seconds   127.0.0.1:9090->9090/tcp                              shards-prometheus-1
```

## Accessing shards

If you installed shards on your desktop machine, you can access it at http://localhost:8080/.
If shards is deployed on a remote node, replace `NODE_IP_ADDRESS` with the IP address of the node in the following URL:
http://NODE_IP_ADDRESS:8080/.

## Upgrade shards

To upgrade shards, pull the latest images and re-apply the Docker Compose configuration:

```bash
curl -fsS https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/docker-compose.yaml | \
  docker compose -f - pull
curl -fsS https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/docker-compose.yaml | \
  docker compose -f - up -d
```

## Uninstall shards

To uninstall shards run the following command:

```bash
curl -fsS https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/docker-compose.yaml | \
  docker compose -f - rm -f -s -v
```

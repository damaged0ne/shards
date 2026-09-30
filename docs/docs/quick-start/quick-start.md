---
sidebar_position: 1
slug: /
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# Quick start

shards is a self-hosted, open-source observability, alerting and incident center built on eBPF agents.
It can be operated by people through the UI and by AI operator agents through its [MCP endpoint](/mcp/overview).

This guide provides a quick overview of launching shards with default options. For more details and customization options check out the [Installation](/installation/) section.

:::note Container images
The images used below (`ghcr.io/damaged0ne/shards`, `ghcr.io/damaged0ne/shards-node-agent`, `ghcr.io/damaged0ne/shards-cluster`)
are built from the [shards](https://github.com/damaged0ne/shards), [shards-node-agent](https://github.com/damaged0ne/shards-node-agent)
and [shards-cluster](https://github.com/damaged0ne/shards-cluster) repositories (see the `Dockerfile` in each one).
If they are not published for your platform yet, build them locally with `docker build` and adjust the image references.
:::

<Tabs queryString="env">
  <TabItem value="docker" label="Docker" default>

To deploy shards using Docker Compose, run the following command. Before applying it, you can review the configuration file in the shards GitHub repository: [docker-compose.yaml](https://github.com/damaged0ne/shards/blob/main/deploy/docker-compose.yaml)

```bash
curl -fsS https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/docker-compose.yaml | \
  docker compose -f - up -d
```

If you installed shards on your desktop machine, you can access it at http://localhost:8080/. If shards is deployed on a remote node, replace `NODE_IP_ADDRESS` with the IP address of the node in the following URL: http://NODE_IP_ADDRESS:8080/.

  </TabItem>

  <TabItem value="docker-swarm" label="Docker Swarm">

Deploy the shards stack to your cluster by running the following command on the manager node. Before applying, you can review the configuration file in the shards GitHub repository: [docker-swarm-stack.yaml](https://github.com/damaged0ne/shards/blob/main/deploy/docker-swarm-stack.yaml)

```bash
curl -fsS https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/docker-swarm-stack.yaml | \
  docker stack deploy -c - shards
```

Since Docker Swarm doesn't support privileged containers, you'll have to manually deploy shards-node-agent on each cluster node. Just replace `NODE_IP` with any node's IP address in the Docker Swarm cluster.

```bash
docker run --detach --name shards-node-agent \
  --pull=always \
  --privileged --pid host \
  -v /sys/kernel/tracing:/sys/kernel/tracing:rw \
  -v /sys/kernel/debug:/sys/kernel/debug:rw \
  -v /sys/fs/cgroup:/host/sys/fs/cgroup:ro \
  ghcr.io/damaged0ne/shards-node-agent \
  --cgroupfs-root=/host/sys/fs/cgroup \
  --collector-endpoint=http://NODE_IP:8080
```
Access shards through any node in your Docker Swarm cluster using its published port: http://NODE_IP:8080.
  </TabItem>

  <TabItem value="ubuntu" label="Ubuntu & Debian">

shards requires a Prometheus server with the Remote Write Receiver enabled, along with a ClickHouse server.
For detailed steps on installing all the necessary components on an Ubuntu/Debian node, refer to the [full instructions](/installation/ubuntu).

To install shards (server and cluster agent as systemd services), run the following command:

```bash
curl -sfL https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/install.sh | \
  BOOTSTRAP_PROMETHEUS_URL="http://PROMETHEUS_IP:9090" \
  BOOTSTRAP_REFRESH_INTERVAL=15s \
  BOOTSTRAP_CLICKHOUSE_ADDRESS=CLICKHOUSE_IP:9000 \
  sh -
```

Install the node agent on every node within your infrastructure:

```bash
curl -sfL https://raw.githubusercontent.com/damaged0ne/shards-node-agent/main/install.sh | \
  COLLECTOR_ENDPOINT=http://SHARDS_NODE_IP:8080 \
  SCRAPE_INTERVAL=15s \
  sh -
```

Access shards at: http://SHARDS_NODE_IP:8080.
</TabItem>

<TabItem value="rhel" label="RHEL & CentOS">

shards requires a Prometheus server with the Remote Write Receiver enabled, along with a ClickHouse server.
For detailed steps on installing all the necessary components on a RHEL/CentOS node, refer to the [full instructions](/installation/rhel).

To install shards (server and cluster agent as systemd services), run the following command:

```bash
curl -sfL https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/install.sh | \
  BOOTSTRAP_PROMETHEUS_URL="http://PROMETHEUS_IP:9090" \
  BOOTSTRAP_REFRESH_INTERVAL=15s \
  BOOTSTRAP_CLICKHOUSE_ADDRESS=CLICKHOUSE_IP:9000 \
  sh -
```

Install the node agent on every node within your infrastructure:

```bash
curl -sfL https://raw.githubusercontent.com/damaged0ne/shards-node-agent/main/install.sh | \
  COLLECTOR_ENDPOINT=http://SHARDS_NODE_IP:8080 \
  SCRAPE_INTERVAL=15s \
  sh -
```
Access shards at: http://SHARDS_NODE_IP:8080.
</TabItem>

  <TabItem value="kubernetes" label="Kubernetes">

shards does not publish its own Helm charts yet. You can deploy it with the Coroot operator (maintained by Coroot, Inc.)
by overriding the component images, see [Kubernetes](/installation/kubernetes).

  </TabItem>

</Tabs>

---
sidebar_position: 7
---

# Docker Swarm


**Step #1: Initialize Docker Swarm**

If you haven't already initialized Docker Swarm on your manager node, run the following command on the manager node:

```bash
docker swarm init
```

This initializes a new Docker Swarm and joins the current node as a manager.

**Step #2: Deploy the shards Stack**

Deploy the shards stack to your cluster by running the following command on the manager node. 
Before applying, you can review the configuration file in shards' GitHub repository: docker-swarm-stack.yaml

```bash
curl -fsS https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/docker-swarm-stack.yaml | \
  docker stack deploy -c - shards
```

**Step #3: Validate the deployment**

After deploying the stack, you can use docker stack ls to list the deployed stacks in your Docker Swarm cluster. 
Here's an example of how the output might look:

```bash
NAME      SERVICES
shards    3
```

**Step #4: Installing shards-node-agent**

Since Docker Swarm doesn't support privileged containers, you'll have to manually deploy shards-node-agent on each cluster node. 
Just replace `NODE_IP` with any node's IP address in the Docker Swarm cluster.

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

**Step #5: Accessing shards**

Access shards through any node in your Docker Swarm cluster using its published port: http://NODE_IP:8080.

**Upgrade shards**

To upgrade shards, re-run the deploy command from Step #2 — Docker Swarm resolves the latest images and updates the services:

```bash
curl -fsS https://raw.githubusercontent.com/damaged0ne/shards/main/deploy/docker-swarm-stack.yaml | \
  docker stack deploy -c - shards
```

To upgrade shards-node-agent, remove the container on each node and re-run the command from Step #4 (it always pulls the latest image):

```bash
docker rm -f shards-node-agent
```

**Uninstall shards**

To uninstall shards run the following command:

```bash
docker stack rm shards
```

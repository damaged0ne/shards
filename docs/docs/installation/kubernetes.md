---
sidebar_position: 3
---

# Kubernetes

:::note Operator and Helm charts
shards does not publish its own Kubernetes operator or Helm charts yet.
The [Coroot operator](https://github.com/coroot/coroot-operator) and the Helm charts at `https://coroot.github.io/helm-charts` are maintained by Coroot, Inc.
Because shards stays compatible with the upstream custom resource, you can deploy shards with that operator by overriding the component images
with the shards images (`ghcr.io/damaged0ne/shards`, `ghcr.io/damaged0ne/shards-node-agent`, `ghcr.io/damaged0ne/shards-cluster`).
These images are built from the shards repositories (see the `Dockerfile` in each repository). Pin explicit tags: the operator's automatic
version discovery only knows about the upstream images.
:::

Add the Coroot helm chart repo:

```bash
helm repo add coroot https://coroot.github.io/helm-charts
helm repo update coroot
```

Next, install the Coroot operator:

```bash
helm install -n shards --create-namespace coroot-operator coroot/coroot-operator
```

Create a custom resource that points the operator at the shards images (see [Kubernetes Operator](/installation/k8s-operator) for all options):

```yaml
apiVersion: coroot.com/v1
kind: Coroot
metadata:
  name: shards
  namespace: shards
spec:
  communityEdition:
    image:
      name: ghcr.io/damaged0ne/shards:<version>
  nodeAgent:
    image:
      name: ghcr.io/damaged0ne/shards-node-agent:<version>
  clusterAgent:
    image:
      name: ghcr.io/damaged0ne/shards-cluster:<version>
  clickhouse:
    shards: 2
    replicas: 2
```

```bash
kubectl apply -f shards.yaml
```

Forward the shards port to your machine (the operator names the service `<resource name>-coroot`):

```bash
kubectl port-forward -n shards service/shards-coroot 8080:8080
```

Then, you can access shards at http://localhost:8080

**Upgrade**

Change the image tags in the custom resource and re-apply it. The operator rolls out the new versions.

To upgrade the operator itself:

```bash
helm repo update coroot
helm upgrade -n shards coroot-operator coroot/coroot-operator
```

**Uninstall**

```bash
kubectl delete -f shards.yaml
helm uninstall coroot-operator -n shards
```

## Without the operator

For a minimal setup, `manifests/shards.yaml` in the shards repository deploys only the shards server (a Deployment with a PersistentVolumeClaim).
In that case, you need to provide Prometheus (with the remote write receiver enabled) and ClickHouse yourself, and deploy
[shards-node-agent](https://github.com/damaged0ne/shards-node-agent) as a privileged DaemonSet and
[shards-cluster](https://github.com/damaged0ne/shards-cluster) as a Deployment.

## Troubleshooting

### Pod Security Standards

The node agent requires privileged access for eBPF monitoring, host filesystem access, and container inspection. If the node agent fails to start due to Pod Security violations (common in Talos clusters), allow privileged workloads in the namespace:

```bash
kubectl label ns shards pod-security.kubernetes.io/enforce=privileged
```

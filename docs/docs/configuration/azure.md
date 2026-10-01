---
sidebar_position: 5.8
---

# Azure

The Azure integration lets shards discover Azure Database for PostgreSQL flexible servers, Azure Database for MySQL
flexible servers (with their read replicas) and Azure Cache for Redis instances, and collect their
[Azure Monitor](https://learn.microsoft.com/azure/azure-monitor/) metrics.

All Azure API calls are made by the [cluster-agent](/configuration/shards-cluster), not by the shards server. The agent
discovers the resources once a minute and fetches their metrics (`PT1M` grain) in the background.

## Configuration

The integration is configured in the cluster-agent [configuration file](/configuration/shards-cluster#configuration-file):

```yaml
azure:
  subscriptionIds: [00000000-0000-0000-0000-000000000000] # AZURE_SUBSCRIPTION_ID by default
  resourceGroups: [prod-db]        # optional: all the resource groups by default
  locations: [westeurope]          # optional
  postgresTagFilters: {env: prod*} # glob patterns; read replicas follow their primary
  mysqlTagFilters: {env: prod*}
  redisTagFilters: {team: web}

databases:
  - {type: postgres, azuredb: orders-pg, credentials: {username: shards, password: "${PG_PASSWORD}"}}
  - {type: mysql, azuredb: billing-mysql, credentials: {username: shards, password: "${MYSQL_PASSWORD}"}}
  - {type: redis, azureredis: sessions-cache, credentials: {password: "${REDIS_ACCESS_KEY}"}}
```

Authentication uses `DefaultAzureCredential`: an environment service principal, AKS workload identity or a managed
identity. The identity needs the **Reader** and **Monitoring Reader** roles on the subscriptions or resource groups.

The **Cloud integrations** page of the project settings shows the discovery status, the errors reported by the agent
(`azure_discovery_error`) and the discovered instances.

## What is shown

Flexible servers become `AzureDB` applications (read replicas are grouped with their primary), caches become
`AzureRedis` applications. Each instance gets a node (`azuredb:<name>`, `azureredis:<name>`) with the cloud provider,
location, zone and SKU.

| Metrics | Used for |
|---|---|
| `azure_db_info`, `azure_db_status` | Inventory, instance status (`Ready`), replication role (primary/replica) |
| `azure_db_cpu_usage_percent` | Node CPU usage, the CPU checks |
| `azure_db_storage_total_bytes`, `azure_db_storage_used_bytes`, `azure_db_storage_usage_percent` | Disk space of the data volume (the Storage report and its disk space check) |
| `azure_db_io_ops_per_second{operation}`, `azure_db_iops`, `azure_db_io_consumption_percent` | IOPS, I/O utilization |
| `azure_db_network_bytes_per_second{direction}` | Node network traffic |
| `azure_db_memory_usage_percent`, `azure_db_connections_active` | Cloud report: memory, connections |
| `azure_db_replication_lag_seconds` | Cloud report: replica lag |
| `azure_redis_info`, `azure_redis_status` | Inventory, status (`Succeeded`) |
| `azure_redis_cpu_usage_percent`, `azure_redis_network_bytes_per_second{direction}` | Node CPU and network |
| `azure_redis_memory_usage_percent`, `azure_redis_server_load_percent`, `azure_redis_connected_clients`, `azure_redis_memory_used_bytes` | Cloud report: memory, server load, connections |

## Checks and built-in alerts

| Check | Default condition | Built-in alert |
|---|---|---|
| Disk space | the storage usage of a flexible server is above 80% (**critical** above 90%) | warning (raised to critical above 90%) |
| CPU | the usual node CPU check | warning |
| Managed database replica lag | the replication lag of a read replica is more than 30s | warning, after 5 minutes |
| Managed service capacity | Azure Cache for Redis memory usage or server load is above 90% | warning, after 10 minutes |

## Limitations

* The Azure resources expose a host name (`fqdn`/`host`), not an IP address. The database metrics collected through the
  `azuredb`/`azureredis` targets (queries, locks, replication internals) are attached to the Azure applications only if
  the agent reports an `ipv4` label on `azure_db_info`/`azure_redis_info`, or if a node agent sees the connections.
* The database logs are not collected: Azure exports them through diagnostic settings, which needs a separate pipeline.

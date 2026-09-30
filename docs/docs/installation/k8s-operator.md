---
sidebar_position: 4
---

# Kubernetes Operator

:::note
The [coroot-operator](https://github.com/coroot/coroot-operator) and its Helm chart are maintained by Coroot, Inc., not by the shards project.
shards keeps compatibility with its custom resource, so the operator can deploy shards when you override the component images
(`communityEdition.image`, `nodeAgent.image`, `clusterAgent.image`) with the shards images. Everything else on this page describes
the upstream operator's behavior.
:::

The operator simplifies the deployment of all required components (shards server, agents, Prometheus, ClickHouse) and enables scaling as needed.

## Operator installation

Add the Coroot helm chart repo:

```bash
helm repo add coroot https://coroot.github.io/helm-charts
helm repo update coroot
```

Next, install the Coroot operator:

```bash
helm install -n shards --create-namespace coroot-operator coroot/coroot-operator
```

## Using shards images

By default, the operator pulls all component images from Coroot's registry (`ghcr.io/coroot`) and keeps them updated automatically.
To run shards, set the images explicitly in the custom resource:

```yaml
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
```

When an image is specified, the operator keeps that version, and you upgrade by changing the tag.

## Coroot CR (Custom Resource)

To deploy shards, you need to create a Coroot resource. Below is an example specification of the Coroot custom resource.
The operator continuously monitors these resources and adjusts the configuration if necessary.
Additionally, the operator checks for new versions of shards components and automatically updates them unless you specify particular versions.

```yaml
apiVersion: coroot.com/v1
kind: Coroot
metadata:
  name: shards
  namespace: shards
spec:
#  metricsRefreshInterval: 15s # Specifies the metric resolution interval.
#  cacheTTL: 30d # Duration for which shards retains the metric cache.
#  authAnonymousRole: # Allows access to shards without authentication if set (one of Admin, Editor, or Viewer).
#  authBootstrapAdminPassword:        # Initial admin password for bootstrapping (plain-text).
#  authBootstrapAdminPasswordSecret:  # Secret containing the initial admin password.
#    name: # Name of the secret to select from.
#    key:  # Key of the secret to select from.
#  # Service accounts for programmatic access with API keys, e.g. autonomous agents using the MCP endpoint.
#  # shards creates or updates them on startup; they are locked in the UI and their keys are managed here.
#  serviceAccounts:
#    - name: claude-agent # Service account name, used as its login (required).
#      role: Viewer       # Admin, Editor, or Viewer (required).
#      apiKeys:           # At least one key is required. Descriptions are required and must be unique within the account.
#        - description: production investigation agent
#          key:           # Plain-text API key. Prefer using `keySecret` for better security.
#          keySecret:     # Secret containing the API key. Generated automatically if missing.
#            name: # Name of the secret to select from.
#            key:  # Key of the secret to select from.
#  env: # Environment variables for shards.
#    - name:
#      value:
#      valueFrom:
#  nodeSelector: # Restricts scheduling to nodes matching the specified labels.
#    <node label name>: <node label value>
#  affinity: # Affinity rules for shards pods.
#  tolerations: # Tolerations for shards pods.
#  resources: # Resource requests and limits for shards pods.
#  podAnnotations: # Annotations for shards pods.
#  storage:
#    size: 10Gi # Volume size for shards storage.
#    className: "" # If not set, the default storage class will be used.
#    reclaimPolicy: Delete # Options: Retain (keeps PVC) or Delete (removes PVC on Coroot CR deletion).
#    annotations: # Annotations for PersistentVolumeClaim (PVC).
#  service:
#    type: # Service type (e.g., ClusterIP, NodePort, LoadBalancer).
#    port:      # Service port (default 8080).
#    nodePort:  # Service nodePort (if type is NodePort).
#    grpcPort:      # gRPC port (default 4317).
#    grpcNodePort:  # gRPC nodePort (if type is NodePort).
#    annotations: # Annotations for Service.
#  ingress: # Ingress configuration for shards.
#    className: # Ingress class name (e.g., nginx, traefik; if not set the default IngressClass will be used).
#    host: # Domain name for shards (e.g., shards.company.com).
#    path: # Path prefix for shards (e.g., /shards).
#    annotations: # Annotations for Ingress.
#    tls: # TLS configuration.
#      hosts: # The array with host names
#      secretName: # The name of secret where TLS certificate and private key would be stored
#  storeMetricsInClickhouse: false # Store metrics in ClickHouse. If enabled, Prometheus will not be installed.
#  grpc: # gRPC settings.
#    disabled: false # Disables gRPC server.
#  tls: # TLS settings (enables TLS for gRPC if defined).
#    certSecret:  # Secret containing TLS certificate (required).
#      name: # Name of the secret to select from.
#      key:  # Key of the secret to select from (e.g., 'tls.crt').
#    keySecret: # Secret containing TLS private key (required).
#      name: # Name of the secret to select from.
#      key:  # Key of the secret to select from (e.g., 'tls.key').

#  disableBuiltinAlerts: false # Disable all built-in alerting rules on startup.

# shards stores Traces, Logs, Profiles (and optionally Metrics) in ClickHouse.
# Their retention is managed by setting a Time-To-Live (TTL) for the corresponding Clickhouse tables.  
# The TTLs below are applied during table creation and do not currently affect existing tables.
#  metricsTTL: 7d
#  tracesTTL: 7d
#  logsTTL: 7d
#  profilesTTL: 7d

# Configuration of the shards server (the operator calls it the Community Edition).
#  communityEdition:
#    image: # If unspecified, the operator will install the upstream Coroot CE image from Coroot's public registry. Set it to run shards.
#      name:           # Specifies the full image reference (e.g., ghcr.io/damaged0ne/shards:<version>)
#      pullPolicy:     # The image pull policy (e.g., Always, IfNotPresent, Never).
#      pullSecrets: [] # The pull secrets for pulling the image from a private registry.

# Configures the operator to install only the node-agent and cluster-agent.
#  agentsOnly:
#    corootURL: http(s)://SHARDS_IP:PORT/ # URL of the shards instance to which agents send metrics, logs, traces, and profiles.
#    tlsSkipVerify: false # Whether to skip verification of the shards server's TLS certificate.

# The API key used by agents when sending telemetry to shards.
#  apiKey: # Plain-text API key. Prefer using `apiKeySecret` for better security.
#  apiKeySecret: # Secret containing the API key.
#    name: # Name of the secret to select from.
#    key:  # Key of the secret to select from.

# Configuration for shards Node Agent.
#  nodeAgent:
#    priorityClassName: # Priority class for the node-agent pods.
#    update_strategy: # Update strategy for node-agent pods.
#    nodeSelector: # Restricts scheduling to nodes matching the specified labels.
#    affinity: # Affinity rules for node-agent pods.
#    tolerations: # Tolerations for node-agent pods.
#      - operator: Exists
#    podAnnotations: # Annotations for node-agent pods.
#    resources: # Resource requests and limits for the node-agent pods.
#      requests: 
#        cpu: 100m
#        memory: 200Mi
#      limits: 
#        cpu: 500m
#        memory: 1Gi
#    env: # Environment variables for the node-agent.
#    image: # If unspecified, the operator will install the upstream node agent from Coroot's public registry. Set it to run shards-node-agent.
#      name:           # Specifies the full image reference (e.g., ghcr.io/damaged0ne/shards-node-agent:<version>)
#      pullPolicy:     # The image pull policy (e.g., Always, IfNotPresent, Never).
#      pullSecrets: [] # The pull secrets for pulling the image from a private registry.
#    trackPublicNetworks: ["0.0.0.0/0"] # Allow track connections to the specified IP networks (e.g., Y.Y.Y.Y/mask). By default, shards tracks all connections.
#    logCollector:
#      collectLogBasedMetrics: true # Collect log-based metrics. Disables `collectLogEntries` if set to false.
#      collectLogEntries: true      # Collect log entries and store them in ClickHouse.
#    ebpfTracer:
#      enabled: true # Collect traces and store them in ClickHouse.
#      sampling: "1.0" # Trace sampling rate (0.0 to 1.0).
#    ebpfProfiler:
#      enabled: true # Collect profiles and store them in ClickHouse.

# Configuration for shards Cluster Agent.
#  clusterAgent:
#    nodeSelector: # Restricts scheduling to nodes matching the specified labels.
#    affinity: # Affinity rules for cluster-agent.
#    tolerations: # Tolerations for cluster-agent.
#    podAnnotations: # Annotations for cluster-agent.
#    resources: # Resource requests and limits for cluster-agent.
#    env: # Environment variables for the cluster-agent.
#    image: # If unspecified, the operator will install the upstream cluster agent from Coroot's public registry. Set it to run shards-cluster.
#      name:           # Specifies the full image reference (e.g., ghcr.io/damaged0ne/shards-cluster:<version>)
#      pullPolicy:     # The image pull policy (e.g., Always, IfNotPresent, Never).
#      pullSecrets: [] # The pull secrets for pulling the image from a private registry.
#    # AWS integration (discovery of RDS and ElastiCache instances). Overrides the settings made in the shards UI.
#    aws:
#      region:          # AWS region to discover instances in (default: the region the cluster runs in).
#      accessKeySecret: # Secret with a static access key (keys: access_key_id, secret_access_key).
#        name:          # Leave unset to use the IAM role of the cluster-agent pod (EKS Pod Identity, IRSA) or the EC2 instance profile.
#      rdsTagFilters:   # Discover only RDS instances whose tags match (glob patterns are supported in values).
#        team: payments
#        env: "prod*"
#      elasticacheTagFilters: {} # Same for ElastiCache clusters.
#    # GCP integration (discovery of Cloud SQL and Memorystore instances).
#    gcp:
#      projectId:         # Project to discover instances in (default: the project of the GKE cluster).
#      region:            # Region to discover instances in (default: the region the cluster runs in; "all" for every region of the project).
#      credentialsSecret: # Secret with a service account key; leave unset to use GKE Workload Identity.
#        name:
#        key: credentials.json
#      cloudsqlLabelFilters:    # Discover only Cloud SQL instances whose labels match (glob patterns are supported in values).
#        team: payments
#      memorystoreLabelFilters: {} # Same for Memorystore instances.
#    # Databases to collect metrics from, in addition to those configured in the shards UI or discovered through pod annotations.
#    oci:
#      compartmentIds: [] # OCIDs of the compartments to discover instances in (default: the cluster's compartment, with OKE Workload Identity).
#      region:            # Region to discover instances in (default: the region the cluster runs in).
#      apiKeySecret:      # Secret with an API key (keys: tenancy_id, user_id, fingerprint, private_key); leave unset to use OKE Workload Identity or the instance principal.
#        name:
#      dbTagFilters:      # Discover only DB systems whose freeform tags match (glob patterns are supported in values).
#        team: payments
#      cacheTagFilters: {} # Same for OCI Cache clusters.
#    # Exactly one of host, rds, elasticache, cloudsql, memorystore, ocidb or ocicache is required per entry.
#    databases:
#      - type: postgres         # postgres, mysql, redis (also for Valkey), memcached or mongodb.
#        rds: my-db             # An RDS instance discovered by the AWS integration: its endpoint is used.
#        credentials:
#          usernameSecret: {name: my-db-shards, key: username}
#          passwordSecret: {name: my-db-shards, key: password}
#        params: {sslmode: require}
#      - type: redis
#        elasticache: my-cache  # An ElastiCache cluster discovered by the AWS integration: every node is monitored.
#      - type: postgres
#        cloudsql: my-db        # A Cloud SQL instance discovered by the GCP integration: its private IP is used.
#        credentials:
#          usernameSecret: {name: my-db-shards, key: username}
#          passwordSecret: {name: my-db-shards, key: password}
#        params: {sslmode: require}
#      - type: redis
#        memorystore: my-cache  # A Memorystore instance (Redis, Valkey with type redis, or Memcached with type memcached) discovered by the GCP integration.
#        ocidb: my-db           # A MySQL HeatWave or PostgreSQL DB system discovered by the OCI integration (display name).
#        ocicache: my-cache     # An OCI Cache cluster discovered by the OCI integration (display name, type redis).
#      - type: mysql
#        host: mysql.example.internal # A hostname is re-resolved on every configuration update; every resolved IP address is monitored.
#        port: "3306"
#        credentials:
#          usernameSecret: {name: mysql-shards, key: username}
#          passwordSecret: {name: mysql-shards, key: password}
#    kubeStateMetrics:
#      image: # If unspecified, the operator will install Kube State Metrics from Coroot's public registry.
#        name:           # Specifies the full image reference (e.g., <private-registry>/kube-state-metrics:<version>)
#        pullPolicy:     # The image pull policy (e.g., Always, IfNotPresent, Never).
#        pullSecrets: [] # The pull secrets for pulling the image from a private registry.

# Configuration for Prometheus managed by the operator.
#  prometheus:
#    nodeSelector: # Restricts scheduling to nodes matching the specified labels.
#    affinity: # Affinity rules for Prometheus.
#    tolerations: # Tolerations for Prometheus.
#    storage:
#      size: 10Gi # Volume size for Prometheus storage.
#      className: "" # If not set, the default storage class will be used.
#      reclaimPolicy: Delete # Options: Retain (keeps PVC) or Delete (removes PVC on Coroot CR deletion).
#      annotations: # Annotations for PersistentVolumeClaim (PVC).
#    resources: # Resource requests and limits for Prometheus.
#    podAnnotations: # Annotations for Prometheus.
#    retention: 2d # Metrics retention time (e.g. 4h, 3d, 2w, 1y).
#    outOfOrderTimeWindow: 1h # The `storage.tsdb.out_of_order_time_window` Prometheus setting.
#    image: # If unspecified, the operator will install Prometheus from Coroot's public registry.
#      name:           # Specifies the full image reference (e.g., <private-registry>/prometheus:<version>).
#      pullPolicy:     # The image pull policy (e.g., Always, IfNotPresent, Never).
#      pullSecrets: [] # The pull secrets for pulling the image from a private registry.

# Use an external Prometheus instance instead of deploying one.
# NOTE: Remote write receiver must be enabled in your Prometheus via the `--web.enable-remote-write-receiver` flag.
#  externalPrometheus:
#    url: # http(s)://<IP>:<port> or http(s)://<domain>:<port> or http(s)://<service name>:<port>.
#    tlsSkipVerify: false # Whether to skip verification of the Prometheus server's TLS certificate.
#    basicAuth: # Basic auth credentials.
#      username: # Basic auth username.
#      password: # Basic auth password.
#      passwordSecret: # Secret containing password.
#        name: # Name of the secret to select from.
#        key:  # Key of the secret to select from.
#    customHeaders:  # Custom headers to include in requests to the Prometheus server.
#      <header name>: <header value>
#    # The URL for metric ingestion though the Prometheus Remote Write protocol (optional).
#    # By default, shards appends /api/v1/write to the base URL configured above.
#    remoteWriteURL: # (e.g., http://vminsert:8480/insert/0/prometheus/api/v1/write).

# Configuration for Clickhouse managed by the operator.
#  clickhouse:
#    shards: 1 # Number of ClickHouse shards.
#    replicas: 1 # Number of replicas per shard.
#    resources: # Resource requests and limits for Clickhouse pods.
#    storage:
#      size: 10Gi # Volume size for EACH ClickHouse instance.
#      className: "" # If not set, the default storage class will be used.
#      reclaimPolicy: Delete # Options: Retain (keeps PVC) or Delete (removes PVC on Coroot CR deletion).
#      annotations: # Annotations for PersistentVolumeClaim (PVC).
#    nodeSelector: # Restricts scheduling to nodes matching the specified labels.
#    affinity: # Affinity rules for ClickHouse pods.
#    tolerations: # Tolerations for ClickHouse pods.
#    podAnnotations: # Annotations for Clickhouse pods.
#    image: # If unspecified, the operator will install Clickhouse from Coroot's public registry.
#      name:           # Specifies the full image reference (e.g., <private-registry>/clickhouse-server:<version>)
#      pullPolicy:     # The image pull policy (e.g., Always, IfNotPresent, Never).
#      pullSecrets: [] # The pull secrets for pulling the image from a private registry.
#    logLevel: warning # Log level (fatal, critical, error, warning, notice, information, debug, trace, test, or none).
#    s3: # S3 storage configuration (optional). Enables ClickHouse to use S3 for data storage.
#      endpoint: # S3 endpoint URL (e.g., https://s3.us-east-1.amazonaws.com/my-bucket/clickhouse/).
#      region:   # S3 region (optional).
#      credentials: # S3 credentials (optional).
#        accessKeyId: # Secret reference for the access key ID.
#          name: # Name of the secret to select from.
#          key:  # Key of the secret to select from.
#        secretAccessKey: # Secret reference for the secret access key.
#          name: # Name of the secret to select from.
#          key:  # Key of the secret to select from.
#      cacheSize: 10Gi # Local cache size for S3 reads. Must be less than clickhouse storage size.
#      mode: tiered # Storage mode: "tiered" (local disk for recent data, S3 for cold) or "s3only" (all data on S3, local disk for cache only).
#      moveFactor: "0.1" # Fraction of local disk free space that triggers moving data to S3 (tiered mode only).
#    keeper: # Configuration for ClickHouse Keeper.
#      replicas: 3 # Use only during initial setup, as changing the replica count for a running Keeper may cause it to fail.
#      nodeSelector: # Restricts scheduling to nodes matching the specified labels.
#      affinity: # Affinity rules for keeper pods.
#      tolerations: # Tolerations for keeper pods.
#      storage:
#        size: 10Gi # Volume size for keeper storage.
#        className: "" # If not set, the default storage class will be used.
#        reclaimPolicy: Delete # Options: Retain (keeps PVC) or Delete (removes PVC on Coroot CR deletion).
#        annotations: # Annotations for PersistentVolumeClaim (PVC).
#      resources: # Resource requests and limits for keeper pods.
#      podAnnotations: # Annotations for keeper pods.
#      image: # If unspecified, the operator will install Clickhouse Keeper from Coroot's public registry.
#        name:           # Specifies the full image reference (e.g., <private-registry>/clickhouse-keeper:<version>)
#        pullPolicy:     # The image pull policy (e.g., Always, IfNotPresent, Never).
#        pullSecrets: [] # The pull secrets for pulling the image from a private registry.
#      logLevel: warning # Log level (fatal, critical, error, warning, notice, information, debug, trace, test, or none).

# Use an external ClickHouse instance instead of deploying one.
#  externalClickhouse:
#    address: # Address of the external ClickHouse instance.
#    database: # Name of the database to be used.
#    user: # Username for accessing the external ClickHouse.
#    password: # Password for accessing the external ClickHouse (plain-text, not recommended).
#    passwordSecret: # Secret containing a password for accessing the external ClickHouse.
#      name: # Name of the secret to select from.
#      key:  # Key of the secret to select from.
#    tlsEnabled: false # Whether to enable TLS for the connection to ClickHouse.
#    tlsSkipVerify: false # Whether to skip verification of the ClickHouse server's TLS certificate.

#  replicas: 1 # Number of shards StatefulSet pods.

# Store configuration in a Postgres DB instead of SQLite (required if `replicas` > 1).
#  postgres:
#    host: # Postgres host or service name.
#    port: # Postgres port (optional, default 5432).
#    database: # Name of the database.
#    user: # Username for accessing Postgres.
#    password: # Password for accessing postgres (plain-text, not recommended).
#    passwordSecret: # Secret containing password.
#      name: # Name of the secret to select from.
#      key:  # Key of the secret to select from.
#    params: # Extra parameters, e.g., sslmode and connect_timeout.
#      sslmode: disable

# The project defined here will be created if it does not exist and will be configured with the provided API keys.
# If a project with the same name already exists (e.g., configured via the UI), its API keys and other settings will be replaced.
#  projects: # Create or update projects.
#    - name:    # Project name (e.g., production, staging; required).
#      # Multi-cluster aggregation: list existing project names to combine (optional).
#      memberProjects:
#        - prod-eu
#        - prod-us
#      # Use another shards instance as the data source for this project (optional).
#      remoteCoroot:
#        url: # Base URL of the remote shards instance (e.g., https://shards.example.com).
#        tlsSkipVerify: false # Whether to skip verification of the shards server's TLS certificate.
#        apiKey: # API key of the remote project. Prefer using `apiKeySecret` for better security.
#        apiKeySecret: # Secret containing the API key.
#          name: # Name of the secret to select from.
#          key:  # Key of the secret to select from.
#        metricResolution: 15s # Prometheus query resolution/refresh interval.
#      # Project API keys, used by agents to send telemetry data (required unless memberProjects or remoteCoroot is set).
#      apiKeys:
#        - description: # The API key description (optional).
#          key:         # Plain-text API key (a random string or UUID). Must be unique. Prefer using `keySecret` for better security.
#          keySecret:   # Secret containing the API key. Generated automatically if missing.
#            name: # Name of the secret to select from.
#            key:  # Key of the secret to select from.
#      # Project notification integrations.
#      notificationIntegrations:
#        baseURL: # The URL of shards instance (required). Used for generating links in notifications.
#        slack:
#          token:        # Slack Bot User OAuth Token (required).
#          tokenSecret:  # Secret containing the Token.
#            name: # Name of the secret to select from.
#            key:  # Key of the secret to select from.
#          defaultChannel:     # Default channel (required).
#          incidents: false    # Notify of incidents (SLO violations).
#          deployments: false  # Notify of deployments.
#          alerts: false       # Notify of alerts.
#        teams:
#          channels:                # MS Teams channels (each channel is a separate webhook).
#            - name: default        # Channel name (required).
#              webhookURL:          # Microsoft Teams Webhook URL for this channel.
#              webhookURLSecret:    # Secret containing the Webhook URL.
#                name: # Name of the secret to select from.
#                key:  # Key of the secret to select from.
#          defaultChannel: default  # The channel used unless an application category specifies another one (default: "default").
#          # webhookURL:            # Deprecated: use `channels`; treated as the "default" channel; cannot be used together with `channels`.
#          # webhookURLSecret:      # Secret containing the Webhook URL.
#          incidents: false         # Notify of incidents (SLO violations).
#          deployments: false       # Notify of deployments.
#          alerts: false            # Notify of alerts.
#        pagerduty:
#          integrationKey:        # PagerDuty Integration Key (required).
#          integrationKeySecret:  # Secret containing the Integration Key.
#            name: # Name of the secret to select from.
#            key:  # Key of the secret to select from.
#          incidents: false    # Notify of incidents (SLO violations).
#          alerts: false       # Notify of alerts.
#        opsgenie:
#          apiKey:        # Opsgenie API Key (required).
#          apiKeySecret:  # Secret containing the API Key.
#            name: # Name of the secret to select from.
#            key:  # Key of the secret to select from.
#          euInstance: false   # EU instance of Opsgenie.
#          incidents: false    # Notify of incidents (SLO violations).
#          alerts: false       # Notify of alerts.
#        webhook:
#          url:                    # Webhook URL (required).
#          tlsSkipVerify: false    # Whether to skip verification of the Webhook server's TLS certificate.
#          basicAuth:              # Basic auth credentials.
#            username:        # Basic auth username.
#            password:        # Basic auth password.
#            passwordSecret:  # Secret containing password.
#              name: # Name of the secret to select from.
#              key:  # Key of the secret to select from.
#          customHeaders:          # Custom headers to include in requests.
#            - key:
#              value:
#          customFields:           # Static key-value pairs included as top-level fields in template data.
#            environment: production
#            team: platform
#          incidents: false        # Notify of incidents (SLO violations).
#          deployments: false      # Notify of deployments.
#          alerts: false           # Notify of alerts.
#          incidentTemplate: ""    # Incident template (required if `incidents: true`).
#          deploymentTemplate: ""  # Deployment template (required if `deployments: true`).
#          alertTemplate: ""       # Alert template (required if `alerts: true`).
#      # Project application category settings.
#      applicationCategories:
#        - name:               # Application category name (required).
#          customPatterns:     # List of glob patterns in the <namespace>/<application_name> format.
#            - staging/*
#            - test-*/*
#          notificationSettings: # Category notification settings.
#            incidents:          # Notify of incidents (SLO violations).
#              enabled: true
#              slack:
#                enabled: true
#                channel: ops    # Slack channel name (the integration's default channel is used if empty).
#              teams:
#                enabled: false
#                channel: ops    # MS Teams channel name (the integration's default channel is used if empty).
#              pagerduty:
#                enabled: false
#              opsgenie:
#                enabled: false
#              webhook:
#                enabled: false
#            deployments:        # Notify of deployments.
#              enabled: true
#              slack:
#                enabled: true
#                channel: general
#              teams:
#                enabled: false
#                channel: general
#              webhook:
#                enabled: false
#            alerts:             # Notify of alerts.
#              enabled: true
#              slack:
#                enabled: true
#                channel: alerts
#              teams:
#                enabled: false
#                channel: alerts
#              pagerduty:
#                enabled: false
#              opsgenie:
#                enabled: false
#              webhook:
#                enabled: false
#      # Project custom applications settings.
#      customApplications:
#        - name: custom-app
#          instancePatterns:
#            - app@node1
#            - app@node2
#      # Alerting rules: adjust built-in rules or define custom ones.
#      # Rules defined here become read-only in the UI (shown with a lock icon).
#      # For built-in rules, only the fields you specify are overridden; unset fields keep their current values.
#      # For custom rules, all required fields (name, source) must be provided.
#      alertingRules:
#        # Adjust a built-in rule (only override severity and description)
#        - id: storage-space          # Required. Built-in rule ID or a custom ID you choose.
#          severity: critical         # One of: warning, critical.
#          templates:
#            description: "Disk space critically low"
#        # Disable a built-in rule
#        - id: memory-pressure
#          enabled: false
#        # Adjust the built-in Kubernetes events rule
#        - id: kubernetes-events
#          source:
#            type: kubernetes_events
#            kubernetesEvents:
#              minCount: 3              # Require at least 3 occurrences before alerting.
#        # Custom check-based rule scoped to a category
#        - id: custom-postgres-latency
#          name: "Postgres latency (production)"
#          source:
#            type: check              # One of: check, log_patterns, kubernetes_events, promql.
#            check:
#              checkId: postgres_latency
#          selector:
#            type: category           # One of: all, category, applications.
#            categories:
#              - production
#          severity: critical
#          for: 5m                    # How long the condition must be true before firing.
#          keepFiringFor: 5m          # How long to keep firing after condition clears.
#          templates:
#            description: "Postgres latency is critically high in production."
#          enabled: true
#        # Custom check-based rule scoped to specific applications
#        - id: custom-cart-latency
#          name: "Cart service latency"
#          source:
#            type: check
#            check:
#              checkId: http_latency
#          selector:
#            type: applications       # One of: all, category, applications.
#            applicationIdPatterns:
#              - otel-demo:Deployment:cart      # exact match: <namespace>:<kind>:<name>
#              - payments:*:*                   # all applications in namespace
#              - "*:StatefulSet:*"              # all StatefulSets across all namespaces
#          severity: critical
#          for: 5m
#          templates:
#            description: "Latency is critically high."
#          enabled: true
#        # Custom PromQL-based rule
#        - id: custom-uptime
#          name: "Instance uptime"
#          source:
#            type: promql
#            promql:
#              expression: "up == 0"
#          severity: warning
#          templates:
#            summary: "Instance {{.instance}} is down"
#          notificationCategory: custom-category # Override the notification category for this rule.
#        # Custom log-based rule
#        - id: custom-log-errors
#          name: "Critical log errors"
#          source:
#            type: log_patterns
#            logPattern:
#              severities:
#                - error
#                - fatal
#              minCount: 5            # Minimum occurrences before alerting.
#              maxAlertsPerApp: 10    # Maximum alerts per application for this rule.
#          severity: critical
#        # Custom Kubernetes events-based rule
#        - id: custom-k8s-events
#          name: "K8s scheduling failures"
#          source:
#            type: kubernetes_events
#            kubernetesEvents:
#              minCount: 3              # Minimum occurrences before alerting.
#              maxAlertsPerApp: 10      # Maximum alerts per application for this rule.
#          severity: warning
#      # Project inspection overrides.
#      inspectionOverrides:
#        # `applicationId` format: <namespace>:<kind>:<name>
#        sloAvailability:
#          - applicationId: otel-demo:Deployment:cart
#            objectivePercent: 99.9 # The percentage of requests that should be served without errors (e.g., 95, 99, 99.9).
#        sloLatency:
#          - applicationId: otel-demo:Deployment:cart
#            objectivePercent: 99.9     # The percentage of requests that should be served faster than `objectiveThreshold` (e.g., 95, 99, 99.9).
#            objectiveThreshold: 100ms  # The latency threshold (e.g., 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s).
#          - applicationId: external:ExternalService:api.github.com:443
#            objectivePercent: 99 
#            objectiveThreshold: 2s
```

## Operator upgrade

```bash
helm repo update coroot
helm upgrade -n shards coroot-operator coroot/coroot-operator
```

package constructor

import (
	"net"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
)

// Shards fork: loaders for the metrics added by the shards cluster agent (see queries_dbext_shards.go).

func mergeInto(dst **timeseries.TimeSeries, ts *timeseries.TimeSeries) {
	*dst = merge(*dst, ts, timeseries.Any)
}

func mergeKey[K comparable](m map[K]*timeseries.TimeSeries, k K, ts *timeseries.TimeSeries) {
	m[k] = merge(m[k], ts, timeseries.Any)
}

// pgExt loads the Postgres metrics that the upstream loader doesn't know about.
func pgExt(pg *model.Postgres, queryName string, m *model.MetricValues) {
	ls, v := m.Labels, m.Values
	var byDb map[string]*timeseries.TimeSeries
	e := pg.GetOrCreateExt()
	switch queryName {
	case "pg_db_xact_commit_rate":
		byDb = e.XactCommit
	case "pg_db_xact_rollback_rate":
		byDb = e.XactRollback
	case "pg_db_blks_hit_rate":
		byDb = e.BlksHit
	case "pg_db_blks_read_rate":
		byDb = e.BlksRead
	case "pg_db_deadlocks_rate":
		byDb = e.Deadlocks
	case "pg_db_conflicts_rate":
		byDb = e.Conflicts
	case "pg_db_temp_bytes_rate":
		byDb = e.TempBytes
	case "pg_db_checksum_failures_rate":
		byDb = e.ChecksumFailures
	case "pg_db_sessions_abandoned_rate":
		byDb = e.SessionsAbandoned
	case "pg_db_sessions_fatal_rate":
		byDb = e.SessionsFatal
	case "pg_db_sessions_killed_rate":
		byDb = e.SessionsKilled
	case "pg_db_idle_in_transaction_time_rate":
		byDb = e.IdleInTxTime
	case "pg_db_unused_indexes":
		byDb = e.DbUnusedIndexes
	case "pg_db_unused_indexes_bytes":
		byDb = e.DbUnusedIndexesBytes
	case "pg_db_duplicate_indexes":
		byDb = e.DbDuplicateIndexes
	}
	if byDb != nil {
		mergeKey(byDb, ls["db"], v)
		return
	}

	var byBackend map[string]*timeseries.TimeSeries
	switch queryName {
	case "pg_io_reads_rate":
		byBackend = e.IOReads
	case "pg_io_writes_rate":
		byBackend = e.IOWrites
	case "pg_io_extends_rate":
		byBackend = e.IOExtends
	case "pg_io_fsyncs_rate":
		byBackend = e.IOFsyncs
	case "pg_io_hits_rate":
		byBackend = e.IOHits
	case "pg_io_evictions_rate":
		byBackend = e.IOEvictions
	case "pg_io_read_time_rate", "pg_io_write_time_rate":
		e.IOTime[ls["backend_type"]] = merge(e.IOTime[ls["backend_type"]], v, timeseries.NanSum)
		return
	}
	if byBackend != nil {
		mergeKey(byBackend, ls["backend_type"], v)
		return
	}

	switch queryName {
	case "pg_wal_records_rate":
		mergeInto(&e.WalRecords, v)
	case "pg_wal_fpi_rate":
		mergeInto(&e.WalFpi, v)
	case "pg_wal_bytes_rate":
		mergeInto(&e.WalBytes, v)
	case "pg_wal_buffers_full_rate":
		mergeInto(&e.WalBuffersFull, v)

	case "pg_subscription_worker_up", "pg_subscription_last_msg_receipt_age_seconds", "pg_subscription_last_msg_delay_seconds",
		"pg_subscription_latest_end_age_seconds", "pg_subscription_errors_rate":
		name := ls["subscription"]
		s := e.Subscriptions[name]
		if s == nil {
			s = &model.PgSubscription{}
			e.Subscriptions[name] = s
		}
		switch queryName {
		case "pg_subscription_worker_up":
			mergeInto(&s.WorkerUp, v)
		case "pg_subscription_last_msg_receipt_age_seconds":
			mergeInto(&s.LastMsgAge, v)
		case "pg_subscription_last_msg_delay_seconds":
			mergeInto(&s.LastMsgDelay, v)
		case "pg_subscription_latest_end_age_seconds":
			mergeInto(&s.LatestEndAge, v)
		case "pg_subscription_errors_rate":
			if ls["type"] == "sync" {
				mergeInto(&s.SyncErrors, v)
			} else {
				mergeInto(&s.ApplyErrors, v)
			}
		}

	case "pg_replication_standby_replay_lag_seconds", "pg_replication_standby_replay_lag_bytes", "pg_replication_standby_info":
		k := model.PgStandbyKey{ApplicationName: ls["application_name"], ClientAddr: ls["client_addr"]}
		s := e.Standbys[k]
		if s == nil {
			s = &model.PgStandby{}
			e.Standbys[k] = s
		}
		switch queryName {
		case "pg_replication_standby_replay_lag_seconds":
			mergeInto(&s.ReplayLagSeconds, v)
		case "pg_replication_standby_replay_lag_bytes":
			mergeInto(&s.ReplayLagBytes, v)
		case "pg_replication_standby_info":
			s.State.Update(v, ls["state"])
			s.SyncState.Update(v, ls["sync_state"])
		}

	case "pg_index_unused_bytes":
		mergeKey(e.UnusedIndexBytes, model.PgIndexKey{Db: ls["db"], Schema: ls["schema"], Table: ls["table"], Index: ls["index"]}, v)

	case "pg_wait_event_sessions":
		mergeKey(e.WaitEvents, ls["wait_event_type"]+": "+ls["wait_event"], v)

	case "pg_top_query_plan_time_per_second", "pg_top_query_rows_per_second", "pg_top_query_shared_blks_hit_per_second",
		"pg_top_query_shared_blks_read_per_second", "pg_top_query_temp_blks_read_per_second", "pg_top_query_temp_blks_written_per_second",
		"pg_top_query_wal_bytes_per_second", "pg_top_query_exec_time_min_seconds", "pg_top_query_exec_time_mean_seconds",
		"pg_top_query_exec_time_max_seconds":
		k := model.QueryKey{Db: ls["db"], User: ls["user"], Query: ls["query"]}
		qs := e.PerQuery[k]
		if qs == nil {
			qs = &model.PgQueryStatExt{}
			e.PerQuery[k] = qs
		}
		switch queryName {
		case "pg_top_query_plan_time_per_second":
			mergeInto(&qs.PlanTime, v)
		case "pg_top_query_rows_per_second":
			mergeInto(&qs.Rows, v)
		case "pg_top_query_shared_blks_hit_per_second":
			mergeInto(&qs.SharedBlksHit, v)
		case "pg_top_query_shared_blks_read_per_second":
			mergeInto(&qs.SharedBlksRead, v)
		case "pg_top_query_temp_blks_read_per_second":
			mergeInto(&qs.TempBlksRead, v)
		case "pg_top_query_temp_blks_written_per_second":
			mergeInto(&qs.TempBlksWritten, v)
		case "pg_top_query_wal_bytes_per_second":
			mergeInto(&qs.WalBytes, v)
		case "pg_top_query_exec_time_min_seconds":
			mergeInto(&qs.ExecTimeMin, v)
		case "pg_top_query_exec_time_mean_seconds":
			mergeInto(&qs.ExecTimeMean, v)
		case "pg_top_query_exec_time_max_seconds":
			mergeInto(&qs.ExecTimeMax, v)
		}
	}
}

// mysqlExt loads the MySQL metrics that the upstream loader doesn't know about.
func mysqlExt(my *model.Mysql, queryName string, m *model.MetricValues) {
	ls, v := m.Labels, m.Values
	switch queryName {
	case "mysql_wait_event_time_rate":
		mergeKey(my.GetOrCreateExt().WaitEventTime, ls["event"], v)
	case "mysql_wait_event_count_rate":
		mergeKey(my.GetOrCreateExt().WaitEventCount, ls["event"], v)
	case "mysql_replication_applier_last_transaction_lag_seconds":
		mergeKey(my.GetOrCreateExt().ApplierLastTransactionLag, ls["channel"], v)
	case "mysql_replication_applier_current_lag_seconds":
		mergeKey(my.GetOrCreateExt().ApplierCurrentLag, ls["channel"], v)
	case "mysql_index_unused":
		mergeKey(my.GetOrCreateExt().UnusedIndexes, model.MysqlIndexKey{Schema: ls["schema"], Table: ls["table"], Index: ls["index"]}, v)
	case "mysql_index_unused_bytes":
		mergeKey(my.GetOrCreateExt().UnusedIndexBytes, model.MysqlIndexKey{Schema: ls["schema"], Table: ls["table"], Index: ls["index"]}, v)
	case "mysql_schema_unused_indexes":
		mergeKey(my.GetOrCreateExt().SchemaUnusedIndexes, ls["schema"], v)
	}
}

// findInstanceByHostname matches the targets configured by a host name (the native RabbitMQ/etcd scrapes report
// the configured "host:port" as the instance label) to an application or an instance with the same name,
// e.g. a Docker Compose service.
func findInstanceByHostname(w *model.World, ls model.Labels, t model.ApplicationType) *model.Instance {
	host, _, err := net.SplitHostPort(ls["instance"])
	if err != nil || host == "" || net.ParseIP(host) != nil {
		return nil
	}
	host = strings.SplitN(host, ".", 2)[0]
	var candidate *model.Instance
	for _, app := range w.Applications {
		for _, i := range app.Instances {
			if i.IsObsolete() {
				continue
			}
			if i.Name != host && app.Id.Name != host {
				continue
			}
			if i.ApplicationTypes()[t] {
				return i
			}
			if candidate == nil {
				candidate = i
			}
		}
	}
	return candidate
}

type dbExtLoader struct {
	w    *model.World
	find func(ls model.Labels, types ...model.ApplicationType) *model.Instance
	byIP map[string][]*model.Instance
}

func newDBExtLoader(w *model.World, find func(ls model.Labels, types ...model.ApplicationType) *model.Instance) *dbExtLoader {
	return &dbExtLoader{w: w, find: find}
}

// findByIP matches a target to an instance listening on the same IP (on any port) and running a process of the type:
// the metrics endpoints of RabbitMQ and etcd listen on other ports than the service itself.
func (l *dbExtLoader) findByIP(ls model.Labels, t model.ApplicationType) *model.Instance {
	addr := ls["address"]
	if addr == "" {
		addr = ls["instance"]
	}
	ip, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(ip) == nil {
		return nil
	}
	if l.byIP == nil {
		l.byIP = map[string][]*model.Instance{}
		for _, app := range l.w.Applications {
			for _, i := range app.Instances {
				for listen := range i.TcpListens {
					l.byIP[listen.IP] = append(l.byIP[listen.IP], i)
				}
			}
		}
	}
	for _, i := range l.byIP[ip] {
		if !i.IsObsolete() && i.ApplicationTypes()[t] {
			return i
		}
	}
	return nil
}

// load handles the PgBouncer, RabbitMQ and etcd metrics. It returns false for other queries.
func (l *dbExtLoader) load(queryName string, m *model.MetricValues) bool {
	var t model.ApplicationType
	switch {
	case strings.HasPrefix(queryName, "pgbouncer_"):
		t = model.ApplicationTypePgbouncer
	case strings.HasPrefix(queryName, "rabbitmq_"):
		t = model.ApplicationTypeRabbitmq
	case strings.HasPrefix(queryName, "etcd_"):
		t = model.ApplicationTypeEtcd
	default:
		return false
	}
	instance := l.find(m.Labels, t)
	if instance == nil || !instance.ApplicationTypes()[t] {
		if i := l.findByIP(m.Labels, t); i != nil {
			instance = i
		}
	}
	if instance == nil && t != model.ApplicationTypePgbouncer {
		instance = findInstanceByHostname(l.w, m.Labels, t)
	}
	if instance == nil {
		return true
	}
	switch t {
	case model.ApplicationTypePgbouncer:
		pgbouncer(instance, queryName, m)
	case model.ApplicationTypeRabbitmq:
		rabbitmq(instance, queryName, m)
	case model.ApplicationTypeEtcd:
		etcd(instance, queryName, m)
	}
	return true
}

func pgbouncer(instance *model.Instance, queryName string, m *model.MetricValues) {
	if instance.Pgbouncer == nil {
		instance.Pgbouncer = model.NewPgbouncer()
	}
	p := instance.Pgbouncer
	ls, v := m.Labels, m.Values
	if queryName == "pgbouncer_up" {
		mergeInto(&p.Up, v)
		return
	}
	if strings.HasPrefix(queryName, "pgbouncer_pools_") {
		k := model.PgbouncerPoolKey{Database: ls["database"], User: ls["user"]}
		pool := p.Pools[k]
		if pool == nil {
			pool = &model.PgbouncerPool{}
			p.Pools[k] = pool
		}
		switch queryName {
		case "pgbouncer_pools_client_active_connections":
			mergeInto(&pool.ClientActive, v)
		case "pgbouncer_pools_client_waiting_connections":
			mergeInto(&pool.ClientWaiting, v)
		case "pgbouncer_pools_server_active_connections":
			mergeInto(&pool.ServerActive, v)
		case "pgbouncer_pools_server_idle_connections":
			mergeInto(&pool.ServerIdle, v)
		case "pgbouncer_pools_server_used_connections":
			mergeInto(&pool.ServerUsed, v)
		case "pgbouncer_pools_client_maxwait_seconds":
			mergeInto(&pool.MaxWait, v)
		}
		return
	}
	if strings.HasPrefix(queryName, "pgbouncer_stats_") {
		s := p.Stats[ls["database"]]
		if s == nil {
			s = &model.PgbouncerStats{}
			p.Stats[ls["database"]] = s
		}
		switch queryName {
		case "pgbouncer_stats_queries_rate":
			mergeInto(&s.Queries, v)
		case "pgbouncer_stats_transactions_rate":
			mergeInto(&s.Transactions, v)
		case "pgbouncer_stats_query_time_rate":
			mergeInto(&s.QueryTime, v)
		case "pgbouncer_stats_client_wait_rate":
			mergeInto(&s.ClientWait, v)
		case "pgbouncer_stats_received_bytes_rate":
			mergeInto(&s.Received, v)
		case "pgbouncer_stats_sent_bytes_rate":
			mergeInto(&s.Sent, v)
		}
	}
}

func rabbitmq(instance *model.Instance, queryName string, m *model.MetricValues) {
	if instance.Rabbitmq == nil {
		instance.Rabbitmq = model.NewRabbitmq()
	}
	r := instance.Rabbitmq
	v := m.Values
	switch queryName {
	case "rabbitmq_up":
		mergeInto(&r.Up, v)
	case "rabbitmq_build_info":
		r.Version.Update(v, m.Labels["rabbitmq_version"])
	case "rabbitmq_identity_info":
		r.Node.Update(v, m.Labels["rabbitmq_node"])
	case "rabbitmq_queue_messages_ready":
		mergeInto(&r.MessagesReady, v)
	case "rabbitmq_queue_messages_unacked":
		mergeInto(&r.MessagesUnacked, v)
	case "rabbitmq_published_rate":
		mergeInto(&r.Published, v)
	case "rabbitmq_delivered_rate":
		mergeInto(&r.Delivered, v)
	case "rabbitmq_unroutable_rate":
		mergeInto(&r.Unroutable, v)
	case "rabbitmq_consumers":
		mergeInto(&r.Consumers, v)
	case "rabbitmq_connections":
		mergeInto(&r.Connections, v)
	case "rabbitmq_queues":
		mergeInto(&r.Queues, v)
	case "rabbitmq_process_resident_memory_bytes":
		mergeInto(&r.MemoryUsed, v)
	case "rabbitmq_resident_memory_limit_bytes":
		mergeInto(&r.MemoryLimit, v)
	case "rabbitmq_disk_space_available_bytes":
		mergeInto(&r.DiskAvailable, v)
	case "rabbitmq_disk_space_available_limit_bytes":
		mergeInto(&r.DiskLimit, v)
	case "rabbitmq_process_open_fds":
		mergeInto(&r.OpenFds, v)
	case "rabbitmq_process_max_fds":
		mergeInto(&r.MaxFds, v)
	case "rabbitmq_unreachable_cluster_peers_count":
		mergeInto(&r.UnreachablePeers, v)
	case "rabbitmq_alarms_memory_used_watermark", "rabbitmq_alarms_free_disk_space_watermark", "rabbitmq_alarms_file_descriptor_limit":
		mergeKey(r.Alarms, strings.TrimPrefix(queryName, "rabbitmq_alarms_"), v)
	}
}

func etcd(instance *model.Instance, queryName string, m *model.MetricValues) {
	if instance.Etcd == nil {
		instance.Etcd = &model.Etcd{}
	}
	e := instance.Etcd
	v := m.Values
	switch queryName {
	case "etcd_up":
		mergeInto(&e.Up, v)
	case "etcd_server_version":
		e.Version.Update(v, m.Labels["server_version"])
	case "etcd_server_has_leader":
		mergeInto(&e.HasLeader, v)
	case "etcd_server_is_leader":
		mergeInto(&e.IsLeader, v)
		instance.UpdateClusterRole("primary", v)
		instance.UpdateClusterRole("replica", v.Map(func(t timeseries.Time, v float32) float32 {
			if v == 0 {
				return 1
			}
			return 0
		}))
	case "etcd_leader_changes_rate":
		mergeInto(&e.LeaderChanges, v)
	case "etcd_proposals_failed_rate":
		mergeInto(&e.ProposalsFailed, v)
	case "etcd_proposals_applied_rate":
		mergeInto(&e.ProposalsApplied, v)
	case "etcd_server_proposals_pending":
		mergeInto(&e.ProposalsPending, v)
	case "etcd_wal_fsync_p99":
		mergeInto(&e.WalFsyncP99, v)
	case "etcd_backend_commit_p99":
		mergeInto(&e.BackendCommitP99, v)
	case "etcd_peer_rtt_p99":
		mergeInto(&e.PeerRttP99, v)
	case "etcd_mvcc_db_total_size_in_bytes":
		mergeInto(&e.DbSize, v)
	case "etcd_mvcc_db_total_size_in_use_in_bytes":
		mergeInto(&e.DbSizeInUse, v)
	case "etcd_server_quota_backend_bytes":
		mergeInto(&e.Quota, v)
	}
}

// rdsExt loads the Aurora metrics of an RDS instance.
func rdsExt(instance *model.Instance, queryName string, m *model.MetricValues) {
	v := m.Values
	switch queryName {
	case "aws_rds_cluster_role":
		a := instance.Rds.GetOrCreateAurora()
		role := m.Labels["role"]
		a.Role.Update(v, role)
		switch role {
		case "writer":
			instance.UpdateClusterRole("primary", v)
		case "reader":
			instance.UpdateClusterRole("replica", v)
		}
	case "aws_rds_aurora_replica_lag_seconds":
		mergeInto(&instance.Rds.GetOrCreateAurora().ReplicaLag, v)
	case "aws_rds_serverless_capacity_acu":
		mergeInto(&instance.Rds.GetOrCreateAurora().ServerlessCapacity, v)
	case "aws_rds_serverless_acu_utilization_percent":
		mergeInto(&instance.Rds.GetOrCreateAurora().ServerlessUsage, v)
	case "aws_rds_serverless_min_capacity_acu":
		mergeInto(&instance.Rds.GetOrCreateAurora().ServerlessMinCapacity, v)
	case "aws_rds_serverless_max_capacity_acu":
		mergeInto(&instance.Rds.GetOrCreateAurora().ServerlessMaxCapacity, v)
	}
}

func ecServerlessKey(id string) string { return "ecserverless/" + id }
func memoryDBKey(id string) string     { return "memorydb/" + id }
func azureDBKey(id string) string      { return "azuredb/" + id }
func azureRedisKey(id string) string   { return "azureredis/" + id }

// cloudServiceInstance returns the instance of a managed service, creating the application, the instance and its node.
func (c *Constructor) cloudServiceInstance(w *model.World, project *db.Project, cloudInstancesById map[string]*model.Instance, key string,
	kind model.ApplicationKind, appName, instanceName, provider, service, id string, v *timeseries.TimeSeries) *model.Instance {
	instance := cloudInstancesById[key]
	if instance == nil {
		appId := c.newApplicationId(project.ClusterId(), "", kind, appName)
		instance = w.GetOrCreateApplication(appId, false).GetOrCreateInstance(instanceName, nil)
		instance.Cloud = &model.CloudService{Provider: provider, Service: service, Id: id}
		cloudInstancesById[key] = instance
	}
	if instance.Node == nil {
		nodeName := strings.ToLower(string(kind)) + ":" + instanceName
		instance.Node = model.NewNode(string(project.Id), model.NewNodeId(nodeName, nodeName))
		instance.Node.Name.Update(v, nodeName)
		instance.Node.Instances = append(instance.Node.Instances, instance)
		instance.Node.CloudProvider.Update(v, provider)
		w.Nodes = append(w.Nodes, instance.Node)
	}
	return instance
}

func addCloudListen(instance *model.Instance, ip, port string, v *timeseries.TimeSeries) {
	if ip == "" || net.ParseIP(ip) == nil || port == "" {
		return
	}
	instance.TcpListens[model.Listen{IP: ip, Port: port}] = true
	if len(instance.Node.NetInterfaces) == 0 {
		instance.Node.NetInterfaces = append(instance.Node.NetInterfaces, &model.InterfaceStats{Name: "eth0", Addresses: []string{ip}, Up: v.WithNewValue(1)})
	}
}

func cloudNetInterface(node *model.Node, v *timeseries.TimeSeries) *model.InterfaceStats {
	if len(node.NetInterfaces) == 0 {
		node.NetInterfaces = append(node.NetInterfaces, &model.InterfaceStats{Name: "eth0", Up: v.WithNewValue(1)})
	}
	return node.NetInterfaces[0]
}

// loadDBExtCloudMetadata creates the applications of the ElastiCache Serverless caches, MemoryDB clusters and
// Azure flexible servers / caches. It runs before the containers are loaded, as the RDS and OCI loaders do.
func (c *Constructor) loadDBExtCloudMetadata(w *model.World, metrics map[string][]*model.MetricValues, cloudInstancesById map[string]*model.Instance, project *db.Project) {
	for _, m := range metrics["azure_discovery_error"] {
		if timeseries.IsNaN(m.Values.Last()) {
			continue
		}
		w.Azure.Configured = true
		if e := m.Labels["error"]; e != "" && m.Values.Last() > 0 {
			w.Azure.DiscoveryErrors[e] = true
		}
	}

	for _, m := range metrics["aws_elasticache_serverless_info"] {
		id, name := m.Labels["ec_serverless_id"], m.Labels["cache_name"]
		if id == "" || name == "" {
			continue
		}
		i := c.cloudServiceInstance(w, project, cloudInstancesById, ecServerlessKey(id), model.ApplicationKindElasticacheServerless, name, name,
			model.CloudProviderAWS, "ElastiCache Serverless", id, m.Values)
		i.Cloud.Engine.Update(m.Values, m.Labels["engine"])
		i.Cloud.EngineVersion.Update(m.Values, m.Labels["engine_version"])
		i.Node.Region.Update(m.Values, m.Labels["region"])
		i.Node.InstanceType.Update(m.Values, "serverless")
	}

	memoryDBClusters := map[string]model.Labels{}
	for _, m := range metrics["aws_memorydb_info"] {
		if id := m.Labels["memorydb_cluster_id"]; id != "" {
			memoryDBClusters[id] = m.Labels
		}
	}
	for _, m := range metrics["aws_memorydb_node_info"] {
		id := m.Labels["memorydb_cluster_id"]
		cl := memoryDBClusters[id]
		if cl == nil || m.Labels["node"] == "" {
			continue
		}
		name := cl["cluster_name"]
		nodeName := m.Labels["node"]
		if !strings.HasPrefix(nodeName, name) {
			nodeName = name + "-" + nodeName
		}
		i := c.cloudServiceInstance(w, project, cloudInstancesById, memoryDBKey(id+"/"+m.Labels["node"]), model.ApplicationKindMemoryDB, name, nodeName,
			model.CloudProviderAWS, "MemoryDB", id, m.Values)
		i.Cloud.Engine.Update(m.Values, cl["engine"])
		i.Cloud.EngineVersion.Update(m.Values, cl["engine_version"])
		i.Cloud.Status.Update(m.Values, m.Labels["status"])
		i.Cloud.LifeSpan = merge(i.Cloud.LifeSpan, m.Values, timeseries.Any)
		i.Node.Region.Update(m.Values, cl["region"])
		i.Node.AvailabilityZone.Update(m.Values, m.Labels["availability_zone"])
		i.Node.InstanceType.Update(m.Values, cl["node_type"])
	}

	for _, m := range metrics["azure_db_info"] {
		id, name := m.Labels["azure_db_id"], m.Labels["name"]
		if id == "" || name == "" {
			continue
		}
		appName := name
		if primary := m.Labels["primary"]; primary != "" { // read replicas are grouped with their primary
			appName = primary
		}
		i := c.cloudServiceInstance(w, project, cloudInstancesById, azureDBKey(id), model.ApplicationKindAzureDB, appName, name,
			model.CloudProviderAzure, "Azure", id, m.Values)
		if len(i.Volumes) == 0 {
			i.Volumes = append(i.Volumes, &model.Volume{MountPoint: "/", EBS: &model.EBS{}})
		}
		i.Volumes[0].Device.Update(m.Values, "data")
		addCloudListen(i, m.Labels["ipv4"], m.Labels["port"], m.Values)
		i.Cloud.Engine.Update(m.Values, m.Labels["engine"])
		i.Cloud.EngineVersion.Update(m.Values, m.Labels["engine_version"])
		role := m.Labels["replication_role"]
		i.Cloud.Role.Update(m.Values, role)
		switch strings.ToLower(role) {
		case "primary", "source":
			i.UpdateClusterRole("primary", m.Values)
		case "asyncreplica", "geoasyncreplica", "replica":
			i.UpdateClusterRole("replica", m.Values)
		}
		i.Node.Region.Update(m.Values, m.Labels["location"])
		i.Node.AvailabilityZone.Update(m.Values, m.Labels["zone"])
		i.Node.InstanceType.Update(m.Values, m.Labels["sku"])
	}

	for _, m := range metrics["azure_redis_info"] {
		id, name := m.Labels["azure_redis_id"], m.Labels["name"]
		if id == "" || name == "" {
			continue
		}
		i := c.cloudServiceInstance(w, project, cloudInstancesById, azureRedisKey(id), model.ApplicationKindAzureRedis, name, name,
			model.CloudProviderAzure, "Azure Cache", id, m.Values)
		addCloudListen(i, m.Labels["ipv4"], m.Labels["port"], m.Values)
		addCloudListen(i, m.Labels["ipv4"], m.Labels["ssl_port"], m.Values)
		i.Cloud.Engine.Update(m.Values, "redis")
		i.Cloud.EngineVersion.Update(m.Values, m.Labels["engine_version"])
		i.Node.Region.Update(m.Values, m.Labels["location"])
		i.Node.InstanceType.Update(m.Values, strings.TrimSpace(m.Labels["sku"]+" "+m.Labels["family"]+m.Labels["capacity"]))
	}
}

// loadDBExtCloud loads the metrics of the managed services created by loadDBExtCloudMetadata.
func (c *Constructor) loadDBExtCloud(w *model.World, metrics map[string][]*model.MetricValues, cloudInstancesById map[string]*model.Instance) {
	for _, q := range dbExtQueries {
		for _, m := range metrics[q.Name] {
			ls, v := m.Labels, m.Values
			switch {
			case strings.HasPrefix(q.Name, "aws_elasticache_serverless_") && q.Name != "aws_elasticache_serverless_info":
				i := cloudInstancesById[ecServerlessKey(ls["ec_serverless_id"])]
				if i == nil {
					continue
				}
				cs := i.Cloud
				switch q.Name {
				case "aws_elasticache_serverless_status":
					cs.LifeSpan = merge(cs.LifeSpan, v, timeseries.Any)
					cs.Status.Update(v, ls["status"])
				case "aws_elasticache_serverless_data_storage_limit_bytes":
					mergeInto(&cs.StorageLimitBytes, v)
				case "aws_elasticache_serverless_ecpu_limit_per_second":
					mergeInto(&cs.ECPULimit, v)
				case "aws_elasticache_serverless_ecpu_per_second":
					mergeInto(&cs.ECPU, v)
				case "aws_elasticache_serverless_used_bytes":
					mergeInto(&cs.StorageUsedBytes, v)
				case "aws_elasticache_serverless_connections":
					mergeInto(&cs.Connections, v)
				}
			case q.Name == "aws_memorydb_status":
				id := ls["memorydb_cluster_id"]
				for key, i := range cloudInstancesById {
					if strings.HasPrefix(key, memoryDBKey(id+"/")) && i.Cloud.Status.Value() == "" {
						i.Cloud.Status.Update(v, ls["status"])
					}
				}
			case strings.HasPrefix(q.Name, "azure_db_") && q.Name != "azure_db_info":
				i := cloudInstancesById[azureDBKey(ls["azure_db_id"])]
				if i == nil {
					continue
				}
				cs, node, volume := i.Cloud, i.Node, i.Volumes[0]
				disk := node.Disks["data"]
				if disk == nil {
					disk = &model.DiskStats{}
					node.Disks["data"] = disk
				}
				switch q.Name {
				case "azure_db_status":
					cs.LifeSpan = merge(cs.LifeSpan, v, timeseries.Any)
					cs.Status.Update(v, ls["status"])
				case "azure_db_storage_total_bytes":
					mergeInto(&volume.CapacityBytes, v)
					mergeInto(&cs.StorageLimitBytes, v)
				case "azure_db_storage_used_bytes":
					mergeInto(&volume.UsedBytes, v)
					mergeInto(&cs.StorageUsedBytes, v)
				case "azure_db_storage_usage_percent":
					mergeInto(&cs.StorageUsedPercent, v)
				case "azure_db_cpu_usage_percent":
					mergeInto(&node.CpuUsagePercent, v)
					mergeInto(&cs.CpuUsagePercent, v)
				case "azure_db_memory_usage_percent":
					mergeInto(&cs.MemoryUsagePercent, v)
				case "azure_db_iops":
					mergeInto(&cs.IOPS, v)
				case "azure_db_io_ops_per_second":
					switch ls["operation"] {
					case "read":
						mergeInto(&disk.ReadOps, v)
					case "write":
						mergeInto(&disk.WriteOps, v)
					}
				case "azure_db_io_consumption_percent":
					mergeInto(&cs.IOConsumption, v)
					mergeInto(&disk.IOUtilizationPercent, v)
				case "azure_db_network_bytes_per_second":
					stat := cloudNetInterface(node, v)
					switch ls["direction"] {
					case "rx":
						mergeInto(&stat.RxBytes, v)
					case "tx":
						mergeInto(&stat.TxBytes, v)
					}
				case "azure_db_connections_active":
					mergeInto(&cs.Connections, v)
				case "azure_db_replication_lag_seconds":
					mergeInto(&cs.ReplicaLag, v)
				}
			case strings.HasPrefix(q.Name, "azure_redis_") && q.Name != "azure_redis_info":
				i := cloudInstancesById[azureRedisKey(ls["azure_redis_id"])]
				if i == nil {
					continue
				}
				cs, node := i.Cloud, i.Node
				switch q.Name {
				case "azure_redis_status":
					cs.LifeSpan = merge(cs.LifeSpan, v, timeseries.Any)
					cs.Status.Update(v, ls["status"])
				case "azure_redis_cpu_usage_percent":
					mergeInto(&node.CpuUsagePercent, v)
					mergeInto(&cs.CpuUsagePercent, v)
				case "azure_redis_memory_usage_percent":
					mergeInto(&cs.MemoryUsagePercent, v)
				case "azure_redis_memory_used_bytes":
					mergeInto(&cs.StorageUsedBytes, v)
				case "azure_redis_server_load_percent":
					mergeInto(&cs.ServerLoad, v)
				case "azure_redis_connected_clients":
					mergeInto(&cs.Connections, v)
				case "azure_redis_network_bytes_per_second":
					stat := cloudNetInterface(node, v)
					switch ls["direction"] {
					case "rx":
						mergeInto(&stat.RxBytes, v)
					case "tx":
						mergeInto(&stat.TxBytes, v)
					}
				}
			}
		}
	}
}

// loadClusterAgentStatus loads the per-target collection health of the cluster agent.
func loadClusterAgentStatus(w *model.World, metrics map[string][]*model.MetricValues) {
	for _, q := range []string{qClusterAgentCollectSuccess, qClusterAgentCollectDuration, qClusterAgentCollectTimeouts} {
		for _, m := range metrics[q] {
			typ, addr := m.Labels["target_type"], m.Labels["address"]
			if typ == "" {
				continue
			}
			t := w.ClusterAgent.GetOrCreateTarget(typ, addr)
			switch q {
			case qClusterAgentCollectSuccess:
				mergeInto(&t.Success, m.Values)
			case qClusterAgentCollectDuration:
				mergeInto(&t.Duration, m.Values)
			case qClusterAgentCollectTimeouts:
				mergeInto(&t.Timeouts, m.Values)
			}
		}
	}
}

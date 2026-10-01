package constructor

import (
	"testing"

	"github.com/coroot/coroot/auditor"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const dbExtPoints = 30 // 15 minutes with testStep = 30s

// flat returns dbExtPoints points with the value v.
func flat(v float32) []float32 {
	res := make([]float32, dbExtPoints)
	for i := range res {
		res[i] = v
	}
	return res
}

// tail returns dbExtPoints points with base and the last k points set to last.
func tail(base, last float32, k int) []float32 {
	res := flat(base)
	for i := dbExtPoints - k; i < dbExtPoints; i++ {
		res[i] = last
	}
	return res
}

type dbExtEnv struct {
	t       *testing.T
	m       testMetrics
	w       *model.World
	project *db.Project
	c       *Constructor
}

func newDBExtEnv(t *testing.T) *dbExtEnv {
	project := &db.Project{Id: "p1"}
	return &dbExtEnv{
		t:       t,
		m:       testMetrics{},
		w:       model.NewWorld(testFrom, testFrom.Add(dbExtPoints*testStep), testStep, testStep),
		project: project,
		c:       New(nil, project, nil, nil),
	}
}

func (e *dbExtEnv) add(query string, labels map[string]string, values []float32) {
	e.m.add(query, "", labels, values...)
}

// app creates an application with one instance listening on addr and running a process of the type t.
func (e *dbExtEnv) app(name string, t model.ApplicationType, ip, port string) *model.Application {
	app := e.w.GetOrCreateApplication(model.NewApplicationId("p1", "default", model.ApplicationKindDeployment, name), false)
	i := app.GetOrCreateInstance(name+"-0", nil)
	c := i.GetOrCreateContainer("/k8s/default/"+name+"-0/"+name, name)
	c.ApplicationTypes[t] = true
	i.TcpListens[model.Listen{IP: ip, Port: port}] = true
	return app
}

func (e *dbExtEnv) load() {
	cloud := map[string]*model.Instance{}
	pjs := promJobStatuses{}
	e.c.loadRdsMetadata(e.w, e.m, pjs, cloud, e.project)
	e.c.loadRds(e.w, e.m, pjs, cloud)
	e.c.loadDBExtCloudMetadata(e.w, e.m, cloud, e.project)
	e.c.loadDBExtCloud(e.w, e.m, cloud)
	loadClusterAgentStatus(e.w, e.m)
	enrichInstances(e.w, e.m, cloud, pjs)
}

func (e *dbExtEnv) audit(app *model.Application) map[model.CheckId]*model.Check {
	auditor.Audit(e.w, e.project, app, nil)
	res := map[model.CheckId]*model.Check{}
	for _, r := range app.Reports {
		for _, ch := range r.Checks {
			res[ch.Id] = ch
		}
	}
	return res
}

func (e *dbExtEnv) report(app *model.Application, name model.AuditReportName) *model.AuditReport {
	for _, r := range app.Reports {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func status(t *testing.T, checks map[model.CheckId]*model.Check, id model.CheckId) model.Status {
	ch := checks[id]
	require.NotNil(t, ch, id)
	return ch.Status
}

func TestDBExtPostgres(t *testing.T) {
	e := newDBExtEnv(t)
	bad := e.app("pg-bad", model.ApplicationTypePostgres, "10.0.0.1", "5432")
	good := e.app("pg-good", model.ApplicationTypePostgres, "10.0.0.2", "5432")
	for _, addr := range []string{"10.0.0.1:5432", "10.0.0.2:5432"} {
		e.add("pg_up", map[string]string{"address": addr}, flat(1))
		e.add("pg_info", map[string]string{"address": addr, "server_version": "16.4"}, flat(1))
	}
	badDb := map[string]string{"address": "10.0.0.1:5432", "db": "shop"}
	goodDb := map[string]string{"address": "10.0.0.2:5432", "db": "shop"}

	// 0.01 deadlocks/s over the last 5 minutes = 3 deadlocks
	e.add("pg_db_deadlocks_rate", badDb, tail(0, 0.01, 10))
	e.add("pg_db_deadlocks_rate", goodDb, flat(0))
	// a single checksum failure in the window
	e.add("pg_db_checksum_failures_rate", badDb, tail(0, 1.0/30, 1))
	e.add("pg_db_checksum_failures_rate", goodDb, flat(0))
	// cache hit ratio: 900 / (900 + 200) = 82%
	e.add("pg_db_blks_hit_rate", badDb, flat(900))
	e.add("pg_db_blks_read_rate", badDb, flat(200))
	e.add("pg_db_blks_hit_rate", goodDb, flat(9900))
	e.add("pg_db_blks_read_rate", goodDb, flat(100))
	// 6 sessions idle in transaction on average
	e.add("pg_db_idle_in_transaction_time_rate", badDb, flat(6))
	e.add("pg_db_idle_in_transaction_time_rate", goodDb, flat(0.1))
	e.add("pg_db_xact_commit_rate", badDb, flat(100))
	// logical replication: the apply worker is down; the other one is healthy
	e.add("pg_subscription_worker_up", map[string]string{"address": "10.0.0.1:5432", "subscription": "orders_sub"}, tail(1, 0, 3))
	e.add("pg_subscription_latest_end_age_seconds", map[string]string{"address": "10.0.0.1:5432", "subscription": "orders_sub"}, flat(10))
	e.add("pg_subscription_worker_up", map[string]string{"address": "10.0.0.2:5432", "subscription": "orders_sub"}, flat(1))
	e.add("pg_subscription_latest_end_age_seconds", map[string]string{"address": "10.0.0.2:5432", "subscription": "orders_sub"}, flat(10))
	// indexes, wait events, top queries, I/O, WAL
	e.add("pg_index_unused_bytes", map[string]string{"address": "10.0.0.1:5432", "db": "shop", "schema": "public", "table": "orders", "index": "orders_tmp_idx"}, flat(1e8))
	e.add("pg_db_unused_indexes", badDb, flat(1))
	e.add("pg_db_unused_indexes_bytes", badDb, flat(1e8))
	e.add("pg_db_duplicate_indexes", badDb, flat(2))
	e.add("pg_wait_event_sessions", map[string]string{"address": "10.0.0.1:5432", "wait_event_type": "Lock", "wait_event": "transactionid"}, flat(3))
	q := map[string]string{"address": "10.0.0.1:5432", "db": "shop", "user": "app", "query": "SELECT * FROM orders WHERE id = ?"}
	e.add("pg_top_query_calls_per_second", q, flat(10))
	e.add("pg_top_query_time_per_second", q, flat(0.5))
	e.add("pg_top_query_exec_time_mean_seconds", q, flat(0.05))
	e.add("pg_top_query_shared_blks_hit_per_second", q, flat(90))
	e.add("pg_top_query_shared_blks_read_per_second", q, flat(10))
	e.add("pg_io_reads_rate", map[string]string{"address": "10.0.0.1:5432", "backend_type": "client backend"}, flat(100))
	e.add("pg_io_read_time_rate", map[string]string{"address": "10.0.0.1:5432", "backend_type": "client backend"}, flat(0.1))
	e.add("pg_io_write_time_rate", map[string]string{"address": "10.0.0.1:5432", "backend_type": "client backend"}, flat(0.2))
	e.add("pg_wal_records_rate", map[string]string{"address": "10.0.0.1:5432"}, flat(1000))
	e.load()

	pg := bad.Instances[0].Postgres
	require.NotNil(t, pg)
	require.NotNil(t, pg.Ext)
	assert.InDelta(t, 81.8, pg.Ext.CacheHitRatio().Last(), 0.1)
	assert.InDelta(t, 0.3, pg.Ext.IOTime["client backend"].Last(), 0.001)
	assert.Len(t, pg.Ext.UnusedIndexBytes, 1)
	assert.Equal(t, float32(0.05), pg.Ext.PerQuery[model.QueryKey{Db: "shop", User: "app", Query: "SELECT * FROM orders WHERE id = ?"}].ExecTimeMean.Last())

	c := model.DBExtChecks
	checks := e.audit(bad)
	assert.Equal(t, model.WARNING, status(t, checks, c.PostgresDeadlocks.Id))
	assert.Equal(t, model.CRITICAL, status(t, checks, c.PostgresChecksumFailures.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.PostgresCacheHitRatio.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.PostgresIdleInTransaction.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.PostgresLogicalReplication.Id))
	assert.Contains(t, checks[c.PostgresLogicalReplication.Id].Details.Items(), "pg-bad-0: orders_sub: the apply worker of the subscription is not running")
	assert.Equal(t, model.CRITICAL, bad.Status)

	r := e.report(bad, model.AuditReportPostgres)
	var tables []string
	for _, w := range r.Widgets {
		if w.Table != nil {
			tables = append(tables, w.Table.Header[0]+"/"+w.Table.Header[1])
		}
	}
	assert.Contains(t, tables, "Unused index (never scanned since the stats reset)/Size")
	assert.Contains(t, tables, "Instance: database/Unused indexes")
	assert.Contains(t, tables, "Instance/Top query (by total time)")

	checks = e.audit(good)
	for _, id := range []model.CheckId{c.PostgresDeadlocks.Id, c.PostgresChecksumFailures.Id, c.PostgresCacheHitRatio.Id, c.PostgresIdleInTransaction.Id, c.PostgresLogicalReplication.Id} {
		assert.Equal(t, model.OK, status(t, checks, id), id)
	}
}

func TestDBExtMysql(t *testing.T) {
	e := newDBExtEnv(t)
	lagging := e.app("mysql-replica", model.ApplicationTypeMysql, "10.0.1.1", "3306")
	ok := e.app("mysql-ok", model.ApplicationTypeMysql, "10.0.1.2", "3306")
	e.add("mysql_up", map[string]string{"address": "10.0.1.1:3306"}, flat(1))
	e.add("mysql_up", map[string]string{"address": "10.0.1.2:3306"}, flat(1))
	e.add("mysql_replication_applier_last_transaction_lag_seconds", map[string]string{"address": "10.0.1.1:3306", "channel": ""}, flat(600))
	e.add("mysql_replication_applier_current_lag_seconds", map[string]string{"address": "10.0.1.2:3306", "channel": ""}, flat(10))
	e.add("mysql_wait_event_time_rate", map[string]string{"address": "10.0.1.1:3306", "event": "wait/io/table/sql/handler"}, flat(0.5))
	e.add("mysql_index_unused", map[string]string{"address": "10.0.1.1:3306", "schema": "shop", "table": "orders", "index": "idx_tmp"}, flat(1))
	e.add("mysql_index_unused_bytes", map[string]string{"address": "10.0.1.1:3306", "schema": "shop", "table": "orders", "index": "idx_tmp"}, flat(1e6))
	e.load()

	require.NotNil(t, lagging.Instances[0].Mysql.Ext)
	assert.Equal(t, float32(600), lagging.Instances[0].Mysql.Ext.ApplierLag().Last())
	assert.Equal(t, model.WARNING, status(t, e.audit(lagging), model.DBExtChecks.MysqlApplierLag.Id))
	assert.Equal(t, model.OK, status(t, e.audit(ok), model.DBExtChecks.MysqlApplierLag.Id))
}

func TestDBExtPgbouncer(t *testing.T) {
	e := newDBExtEnv(t)
	app := e.app("pgbouncer", model.ApplicationTypePgbouncer, "10.0.2.1", "6432")
	quiet := e.app("pgbouncer-quiet", model.ApplicationTypePgbouncer, "10.0.2.2", "6432")
	down := e.app("pgbouncer-down", model.ApplicationTypePgbouncer, "10.0.2.3", "6432")
	noMetrics := e.app("pgbouncer-unmonitored", model.ApplicationTypePgbouncer, "10.0.2.4", "6432")
	addr := "10.0.2.1:6432"
	e.add("pgbouncer_up", map[string]string{"address": addr}, flat(1))
	e.add("pgbouncer_up", map[string]string{"address": "10.0.2.2:6432"}, flat(1))
	e.add("pgbouncer_up", map[string]string{"address": "10.0.2.3:6432"}, tail(1, 0, 3))
	shop := map[string]string{"address": addr, "database": "shop", "user": "app"}
	e.add("pgbouncer_pools_client_maxwait_seconds", shop, flat(6)) // > 5x the threshold: critical
	e.add("pgbouncer_pools_client_waiting_connections", shop, flat(3))
	e.add("pgbouncer_pools_client_active_connections", shop, flat(20))
	e.add("pgbouncer_pools_server_active_connections", shop, flat(20))
	e.add("pgbouncer_pools_server_idle_connections", shop, flat(0))
	e.add("pgbouncer_stats_queries_rate", map[string]string{"address": addr, "database": "shop"}, flat(100))
	e.add("pgbouncer_stats_query_time_rate", map[string]string{"address": addr, "database": "shop"}, flat(0.5))
	q := map[string]string{"address": "10.0.2.2:6432", "database": "shop", "user": "app"}
	e.add("pgbouncer_pools_client_maxwait_seconds", q, flat(0))
	e.add("pgbouncer_pools_client_waiting_connections", q, flat(0))
	e.add("pgbouncer_pools_server_idle_connections", q, flat(5))
	e.load()

	p := app.Instances[0].Pgbouncer
	require.NotNil(t, p)
	assert.InDelta(t, 0.005, p.Stats["shop"].AvgQueryDuration().Last(), 1e-6)

	c := model.DBExtChecks
	checks := e.audit(app)
	assert.Equal(t, model.OK, status(t, checks, c.PgbouncerAvailability.Id))
	assert.Equal(t, model.CRITICAL, status(t, checks, c.PgbouncerClientWaiting.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.PgbouncerPoolSaturation.Id))

	checks = e.audit(quiet)
	assert.Equal(t, model.OK, status(t, checks, c.PgbouncerClientWaiting.Id))
	assert.Equal(t, model.OK, status(t, checks, c.PgbouncerPoolSaturation.Id))

	assert.Equal(t, model.WARNING, status(t, e.audit(down), c.PgbouncerAvailability.Id))

	e.audit(noMetrics)
	r := e.report(noMetrics, model.AuditReportPgbouncer)
	require.NotNil(t, r)
	assert.Equal(t, model.UNKNOWN, r.Status)
	assert.NotNil(t, r.ConfigurationHint)
}

func TestDBExtPgbouncerWarning(t *testing.T) {
	e := newDBExtEnv(t)
	app := e.app("pgbouncer", model.ApplicationTypePgbouncer, "10.0.2.1", "6432")
	e.add("pgbouncer_up", map[string]string{"address": "10.0.2.1:6432"}, flat(1))
	e.add("pgbouncer_pools_client_maxwait_seconds", map[string]string{"address": "10.0.2.1:6432", "database": "shop", "user": "app"}, flat(2))
	e.load()
	assert.Equal(t, model.WARNING, status(t, e.audit(app), model.DBExtChecks.PgbouncerClientWaiting.Id))
}

func TestDBExtRabbitmq(t *testing.T) {
	e := newDBExtEnv(t)
	app := e.app("rabbitmq", model.ApplicationTypeRabbitmq, "10.0.3.1", "5672")
	// matched by the pod IP (k8s annotations) or by the host name of a statically configured target
	byName := e.app("broker", model.ApplicationTypeRabbitmq, "10.0.3.2", "5672")
	e.add("rabbitmq_up", map[string]string{"instance": "10.0.3.1:15692"}, flat(1))
	e.add("rabbitmq_alarms_memory_used_watermark", map[string]string{"instance": "10.0.3.1:15692"}, tail(0, 1, 2))
	e.add("rabbitmq_alarms_free_disk_space_watermark", map[string]string{"instance": "10.0.3.1:15692"}, flat(0))
	e.add("rabbitmq_process_open_fds", map[string]string{"instance": "10.0.3.1:15692"}, flat(950))
	e.add("rabbitmq_process_max_fds", map[string]string{"instance": "10.0.3.1:15692"}, flat(1000))
	e.add("rabbitmq_queue_messages_ready", map[string]string{"instance": "10.0.3.1:15692"}, flat(100))
	e.add("rabbitmq_build_info", map[string]string{"instance": "10.0.3.1:15692", "rabbitmq_version": "3.13.7"}, flat(1))

	e.add("rabbitmq_up", map[string]string{"instance": "broker:15692"}, flat(1))
	e.add("rabbitmq_process_open_fds", map[string]string{"instance": "broker:15692"}, flat(10))
	e.add("rabbitmq_process_max_fds", map[string]string{"instance": "broker:15692"}, flat(1000))
	e.load()

	require.NotNil(t, app.Instances[0].Rabbitmq)
	assert.Equal(t, []string{"memory used watermark"}, app.Instances[0].Rabbitmq.ActiveAlarms())
	require.NotNil(t, byName.Instances[0].Rabbitmq)

	c := model.DBExtChecks
	checks := e.audit(app)
	assert.Equal(t, model.CRITICAL, status(t, checks, c.RabbitmqAlarms.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.RabbitmqFileDescriptors.Id))
	assert.Equal(t, model.OK, status(t, checks, c.RabbitmqAvailability.Id))

	checks = e.audit(byName)
	assert.Equal(t, model.OK, status(t, checks, c.RabbitmqAlarms.Id))
	assert.Equal(t, model.OK, status(t, checks, c.RabbitmqFileDescriptors.Id))
}

func TestDBExtEtcd(t *testing.T) {
	e := newDBExtEnv(t)
	app := e.app("etcd", model.ApplicationTypeEtcd, "10.0.4.1", "2379")
	healthy := e.app("etcd-healthy", model.ApplicationTypeEtcd, "10.0.4.2", "2379")
	a := map[string]string{"instance": "10.0.4.1:2381"}
	e.add("etcd_up", a, flat(1))
	e.add("etcd_server_has_leader", a, tail(1, 0, 2))
	e.add("etcd_server_is_leader", a, flat(0))
	e.add("etcd_wal_fsync_p99", a, flat(0.02))
	e.add("etcd_backend_commit_p99", a, flat(0.01))
	e.add("etcd_mvcc_db_total_size_in_bytes", a, flat(1.8*1024*1024*1024)) // 84% of the default 2GiB quota
	e.add("etcd_leader_changes_rate", a, tail(0, 1.0/30, 5))               // 5 leader changes
	h := map[string]string{"instance": "10.0.4.2:2381"}
	e.add("etcd_up", h, flat(1))
	e.add("etcd_server_has_leader", h, flat(1))
	e.add("etcd_server_is_leader", h, flat(1))
	e.add("etcd_wal_fsync_p99", h, flat(0.002))
	e.add("etcd_backend_commit_p99", h, flat(0.01))
	e.add("etcd_mvcc_db_total_size_in_bytes", h, flat(100e6))
	e.add("etcd_server_quota_backend_bytes", h, flat(8*1024*1024*1024))
	e.add("etcd_leader_changes_rate", h, flat(0))
	e.load()

	assert.Equal(t, model.ClusterRolePrimary, healthy.Instances[0].ClusterRoleLast())
	c := model.DBExtChecks
	checks := e.audit(app)
	assert.Equal(t, model.CRITICAL, status(t, checks, c.EtcdNoLeader.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.EtcdDiskLatency.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.EtcdDbSize.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.EtcdLeaderChanges.Id))

	checks = e.audit(healthy)
	for _, id := range []model.CheckId{c.EtcdNoLeader.Id, c.EtcdDiskLatency.Id, c.EtcdDbSize.Id, c.EtcdLeaderChanges.Id, c.EtcdAvailability.Id} {
		assert.Equal(t, model.OK, status(t, checks, id), id)
	}
}

func TestDBExtCloud(t *testing.T) {
	e := newDBExtEnv(t)
	// Aurora Serverless v2: a writer and a lagging reader near its max capacity
	for _, inst := range []struct{ id, ip, role string }{{"us-east-1/orders-1", "10.1.0.1", "writer"}, {"us-east-1/orders-2", "10.1.0.2", "reader"}} {
		ls := map[string]string{"rds_instance_id": inst.id, "cluster_id": "us-east-1/orders", "ipv4": inst.ip, "port": "5432", "engine": "aurora-postgresql", "engine_version": "16.4", "instance_type": "db.serverless"}
		e.add("aws_rds_info", ls, flat(1))
		e.add("aws_rds_status", map[string]string{"rds_instance_id": inst.id, "status": "available"}, flat(1))
		e.add("aws_rds_cluster_role", map[string]string{"rds_instance_id": inst.id, "role": inst.role}, flat(1))
		e.add("aws_rds_serverless_max_capacity_acu", map[string]string{"rds_instance_id": inst.id}, flat(16))
	}
	e.add("aws_rds_aurora_replica_lag_seconds", map[string]string{"rds_instance_id": "us-east-1/orders-2"}, flat(60))
	e.add("aws_rds_serverless_acu_utilization_percent", map[string]string{"rds_instance_id": "us-east-1/orders-2"}, flat(95))
	e.add("aws_rds_serverless_capacity_acu", map[string]string{"rds_instance_id": "us-east-1/orders-2"}, flat(15.2))

	// ElastiCache Serverless close to its ECPU limit
	ec := map[string]string{"ec_serverless_id": "us-east-1/sessions"}
	e.add("aws_elasticache_serverless_info", map[string]string{"ec_serverless_id": "us-east-1/sessions", "cache_name": "sessions", "engine": "valkey", "engine_version": "8.0", "region": "us-east-1"}, flat(1))
	e.add("aws_elasticache_serverless_status", map[string]string{"ec_serverless_id": "us-east-1/sessions", "status": "available"}, flat(1))
	e.add("aws_elasticache_serverless_ecpu_per_second", ec, flat(9500))
	e.add("aws_elasticache_serverless_ecpu_limit_per_second", ec, flat(10000))

	// MemoryDB
	e.add("aws_memorydb_info", map[string]string{"memorydb_cluster_id": "us-east-1/cart", "cluster_name": "cart", "engine": "redis", "engine_version": "7.1", "node_type": "db.r7g.large", "region": "us-east-1"}, flat(1))
	e.add("aws_memorydb_node_info", map[string]string{"memorydb_cluster_id": "us-east-1/cart", "shard": "0001", "node": "0001-001", "availability_zone": "us-east-1a", "status": "available"}, flat(1))

	// Azure: a PostgreSQL flexible server with 95% storage used and its replica, an Azure Cache for Redis
	az := "sub/rg/postgres/orders-pg"
	e.add("azure_discovery_error", map[string]string{"error": ""}, flat(0))
	e.add("azure_db_info", map[string]string{"azure_db_id": az, "name": "orders-pg", "engine": "postgres", "engine_version": "16", "location": "westeurope", "sku": "Standard_D2ds_v5", "replication_role": "Primary"}, flat(1))
	e.add("azure_db_status", map[string]string{"azure_db_id": az, "status": "Ready"}, flat(1))
	e.add("azure_db_storage_total_bytes", map[string]string{"azure_db_id": az}, flat(100e9))
	e.add("azure_db_storage_used_bytes", map[string]string{"azure_db_id": az}, flat(95e9))
	e.add("azure_db_cpu_usage_percent", map[string]string{"azure_db_id": az}, flat(20))
	e.add("azure_db_io_ops_per_second", map[string]string{"azure_db_id": az, "operation": "read"}, flat(100))
	azr := "sub/rg/postgres/orders-pg-replica"
	e.add("azure_db_info", map[string]string{"azure_db_id": azr, "name": "orders-pg-replica", "engine": "postgres", "engine_version": "16", "replication_role": "AsyncReplica", "primary": "orders-pg"}, flat(1))
	e.add("azure_db_status", map[string]string{"azure_db_id": azr, "status": "Ready"}, flat(1))
	e.add("azure_db_replication_lag_seconds", map[string]string{"azure_db_id": azr}, flat(5))
	e.add("azure_redis_info", map[string]string{"azure_redis_id": "sub/rg/redis/cache", "name": "cache", "engine_version": "6.0", "sku": "Standard", "family": "C", "capacity": "1"}, flat(1))
	e.add("azure_redis_status", map[string]string{"azure_redis_id": "sub/rg/redis/cache", "status": "Succeeded"}, flat(1))
	e.add("azure_redis_memory_usage_percent", map[string]string{"azure_redis_id": "sub/rg/redis/cache"}, flat(50))
	e.load()

	c := model.DBExtChecks
	aurora := e.w.GetApplication(model.NewApplicationId("p1", "", model.ApplicationKindRds, "orders"))
	require.NotNil(t, aurora)
	require.Len(t, aurora.Instances, 2)
	checks := e.audit(aurora)
	assert.Equal(t, model.WARNING, status(t, checks, c.CloudReplicaLag.Id))
	assert.Equal(t, model.WARNING, status(t, checks, c.CloudCapacity.Id))
	for _, i := range aurora.Instances {
		if i.Name == "orders-1" {
			assert.Equal(t, model.ClusterRolePrimary, i.ClusterRoleLast())
		}
	}

	sessions := e.w.GetApplication(model.NewApplicationId("p1", "", model.ApplicationKindElasticacheServerless, "sessions"))
	require.NotNil(t, sessions)
	assert.True(t, sessions.ApplicationTypes()[model.ApplicationTypeValkey])
	assert.Equal(t, model.WARNING, status(t, e.audit(sessions), c.CloudCapacity.Id))

	cart := e.w.GetApplication(model.NewApplicationId("p1", "", model.ApplicationKindMemoryDB, "cart"))
	require.NotNil(t, cart)
	assert.Equal(t, "cart-0001-001", cart.Instances[0].Name)
	assert.True(t, cart.Instances[0].Node.IsUp())

	pg := e.w.GetApplication(model.NewApplicationId("p1", "", model.ApplicationKindAzureDB, "orders-pg"))
	require.NotNil(t, pg)
	require.Len(t, pg.Instances, 2)
	assert.True(t, e.w.Azure.Configured)
	checks = e.audit(pg)
	assert.Equal(t, model.CRITICAL, status(t, checks, model.Checks.StorageSpace.Id)) // 95% > 90%
	assert.Equal(t, model.OK, status(t, checks, c.CloudReplicaLag.Id))
	assert.Equal(t, model.OK, status(t, checks, model.Checks.InstanceAvailability.Id))
	assert.Equal(t, map[string]string{"db": "postgres (Azure)"}, map[string]string(pg.Labels()))

	redis := e.w.GetApplication(model.NewApplicationId("p1", "", model.ApplicationKindAzureRedis, "cache"))
	require.NotNil(t, redis)
	assert.Equal(t, model.OK, status(t, e.audit(redis), c.CloudCapacity.Id))
}

func TestDBExtClusterAgent(t *testing.T) {
	e := newDBExtEnv(t)
	e.add(qClusterAgentCollectSuccess, map[string]string{"address": "10.0.0.1:5432", "target_type": "postgres"}, flat(1))
	e.add(qClusterAgentCollectSuccess, map[string]string{"address": "10.0.0.9:9092", "target_type": "kafka"}, tail(1, 0, 5))
	e.add(qClusterAgentCollectDuration, map[string]string{"address": "10.0.0.9:9092", "target_type": "kafka"}, flat(9))
	e.load()
	ts := e.w.ClusterAgent.TargetsSorted()
	require.Len(t, ts, 2)
	assert.Equal(t, "kafka://10.0.0.9:9092", ts[0].String())
	assert.True(t, ts[0].Failing(3))
	assert.False(t, ts[1].Failing(3))
	assert.Equal(t, float32(9), ts[0].Duration.Last())
}

func TestDBExtQueriesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, q := range QUERIES {
		assert.False(t, seen[q.Name], "duplicate query %s", q.Name)
		seen[q.Name] = true
	}
	_ = timeseries.NaN
}

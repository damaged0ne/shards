package model

import (
	"reflect"

	"github.com/coroot/coroot/timeseries"
)

// Shards fork: checks for the metrics added by the shards cluster agent.
// They live in their own struct (registered in the same index as model.Checks) to keep check.go merge-friendly.

var DBExtChecks = struct {
	PostgresDeadlocks          CheckConfig
	PostgresChecksumFailures   CheckConfig
	PostgresLogicalReplication CheckConfig
	PostgresCacheHitRatio      CheckConfig
	PostgresIdleInTransaction  CheckConfig

	MysqlApplierLag CheckConfig

	PgbouncerAvailability   CheckConfig
	PgbouncerClientWaiting  CheckConfig
	PgbouncerPoolSaturation CheckConfig

	RabbitmqAvailability    CheckConfig
	RabbitmqAlarms          CheckConfig
	RabbitmqFileDescriptors CheckConfig

	EtcdAvailability  CheckConfig
	EtcdNoLeader      CheckConfig
	EtcdLeaderChanges CheckConfig
	EtcdDiskLatency   CheckConfig
	EtcdDbSize        CheckConfig

	CloudReplicaLag CheckConfig
	CloudCapacity   CheckConfig
}{
	PostgresDeadlocks: CheckConfig{
		Category:                AuditReportPostgres,
		Type:                    CheckTypeItemBased,
		Title:                   "Postgres deadlocks",
		DefaultThreshold:        0,
		MessageTemplate:         `deadlocks detected in {{.Items "database"}}`,
		ConditionFormatTemplate: "the number of deadlocks in a database over the last 5 minutes > <threshold>",
	},
	PostgresChecksumFailures: CheckConfig{
		Category:                AuditReportPostgres,
		Type:                    CheckTypeItemBased,
		Title:                   "Postgres data checksum failures",
		DefaultThreshold:        0,
		MessageTemplate:         `data page checksum failures in {{.Items "database"}}`,
		ConditionFormatTemplate: "the number of data page checksum failures > <threshold> (data corruption)",
	},
	PostgresLogicalReplication: CheckConfig{
		Category:                AuditReportPostgres,
		Type:                    CheckTypeItemBased,
		Title:                   "Postgres logical replication",
		DefaultThreshold:        300,
		Unit:                    CheckUnitSecond,
		MessageTemplate:         `{{.ItemsWithToBe "logical replication subscription"}} not replicating`,
		ConditionFormatTemplate: "the apply worker of a subscription is not running, or it hasn't confirmed a WAL position to the publisher for > <threshold>",
	},
	PostgresCacheHitRatio: CheckConfig{
		Category:                AuditReportPostgres,
		Type:                    CheckTypeItemBased,
		Title:                   "Postgres cache hit ratio",
		DefaultThreshold:        90,
		Unit:                    CheckUnitPercent,
		MessageTemplate:         `low buffer cache hit ratio on {{.Items "postgres instance"}}`,
		ConditionFormatTemplate: "the share of block reads served from shared buffers < <threshold> for 10 minutes (OLTP heuristic)",
	},
	PostgresIdleInTransaction: CheckConfig{
		Category:                AuditReportPostgres,
		Type:                    CheckTypeItemBased,
		Title:                   "Postgres idle in transaction",
		DefaultThreshold:        5,
		MessageTemplate:         `{{.ItemsWithHave "postgres instance"}} many sessions idle in transaction`,
		ConditionFormatTemplate: "the average number of sessions idle in a transaction over the last 5 minutes > <threshold>",
	},

	MysqlApplierLag: CheckConfig{
		Category:                AuditReportMysql,
		Type:                    CheckTypeItemBased,
		Title:                   "Mysql replication applier lag",
		DefaultThreshold:        300,
		Unit:                    CheckUnitSecond,
		MessageTemplate:         `the replication applier is behind on {{.Items "mysql replica"}}`,
		ConditionFormatTemplate: "the applier lag of a replication channel (performance_schema) > <threshold>",
	},

	PgbouncerAvailability: CheckConfig{
		Category:                AuditReportPgbouncer,
		Type:                    CheckTypeItemBased,
		Title:                   "PgBouncer availability",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithToBe "pgbouncer instance"}} unavailable`,
		ConditionFormatTemplate: "the number of pgbouncer instances whose admin console is unreachable > <threshold>",
	},
	PgbouncerClientWaiting: CheckConfig{
		Category:                AuditReportPgbouncer,
		Type:                    CheckTypeItemBased,
		Title:                   "PgBouncer client wait time",
		DefaultThreshold:        1,
		Unit:                    CheckUnitSecond,
		MessageTemplate:         `clients are waiting for a server connection in {{.Items "pool"}}`,
		ConditionFormatTemplate: "the age of the oldest waiting client of a pool (maxwait) > <threshold> (critical above 5x the threshold)",
	},
	PgbouncerPoolSaturation: CheckConfig{
		Category:                AuditReportPgbouncer,
		Type:                    CheckTypeItemBased,
		Title:                   "PgBouncer pool saturation",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithToBe "pool"}} saturated`,
		ConditionFormatTemplate: "clients have been waiting for 5 minutes while no server connection of the pool is idle",
	},

	RabbitmqAvailability: CheckConfig{
		Category:                AuditReportRabbitmq,
		Type:                    CheckTypeItemBased,
		Title:                   "RabbitMQ availability",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithToBe "rabbitmq node"}} unavailable`,
		ConditionFormatTemplate: "the number of rabbitmq nodes whose metrics endpoint can't be scraped > <threshold>",
	},
	RabbitmqAlarms: CheckConfig{
		Category:                AuditReportRabbitmq,
		Type:                    CheckTypeItemBased,
		Title:                   "RabbitMQ resource alarms",
		DefaultThreshold:        0,
		MessageTemplate:         `resource alarms on {{.Items "rabbitmq node"}}: publishers are blocked`,
		ConditionFormatTemplate: "a memory, disk or file descriptor alarm is in effect",
	},
	RabbitmqFileDescriptors: CheckConfig{
		Category:                AuditReportRabbitmq,
		Type:                    CheckTypeItemBased,
		Title:                   "RabbitMQ file descriptors",
		DefaultThreshold:        90,
		Unit:                    CheckUnitPercent,
		MessageTemplate:         `{{.ItemsWithToBe "rabbitmq node"}} running out of file descriptors`,
		ConditionFormatTemplate: "open file descriptors > <threshold> of the limit",
	},

	EtcdAvailability: CheckConfig{
		Category:                AuditReportEtcd,
		Type:                    CheckTypeItemBased,
		Title:                   "etcd availability",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithToBe "etcd member"}} unavailable`,
		ConditionFormatTemplate: "the number of etcd members whose metrics endpoint can't be scraped > <threshold>",
	},
	EtcdNoLeader: CheckConfig{
		Category:                AuditReportEtcd,
		Type:                    CheckTypeItemBased,
		Title:                   "etcd leader",
		DefaultThreshold:        0,
		MessageTemplate:         `{{.ItemsWithHave "etcd member"}} no leader`,
		ConditionFormatTemplate: "a member reports no leader (etcd_server_has_leader = 0): the cluster can't accept writes",
	},
	EtcdLeaderChanges: CheckConfig{
		Category:                AuditReportEtcd,
		Type:                    CheckTypeItemBased,
		Title:                   "etcd leader changes",
		DefaultThreshold:        3,
		MessageTemplate:         `frequent leader elections or failed proposals on {{.Items "etcd member"}}`,
		ConditionFormatTemplate: "the number of leader changes over the last hour > <threshold>, or proposals are failing",
	},
	EtcdDiskLatency: CheckConfig{
		Category:                AuditReportEtcd,
		Type:                    CheckTypeItemBased,
		Title:                   "etcd disk latency",
		DefaultThreshold:        0.01,
		Unit:                    CheckUnitSecond,
		MessageTemplate:         `slow disk on {{.Items "etcd member"}}`,
		ConditionFormatTemplate: "the p99 WAL fsync duration > <threshold> or the p99 backend commit duration > 2.5x the threshold",
	},
	EtcdDbSize: CheckConfig{
		Category:                AuditReportEtcd,
		Type:                    CheckTypeItemBased,
		Title:                   "etcd database size",
		DefaultThreshold:        80,
		Unit:                    CheckUnitPercent,
		MessageTemplate:         `the database of {{.Items "etcd member"}} is close to the space quota`,
		ConditionFormatTemplate: "the backend database size > <threshold> of the space quota (--quota-backend-bytes)",
	},

	CloudReplicaLag: CheckConfig{
		Category:                AuditReportCloud,
		Type:                    CheckTypeItemBased,
		Title:                   "Managed database replica lag",
		DefaultThreshold:        30,
		Unit:                    CheckUnitSecond,
		MessageTemplate:         `{{.ItemsWithToBe "replica"}} far behind the primary`,
		ConditionFormatTemplate: "the replica lag reported by the cloud provider (Aurora, Azure) > <threshold>",
	},
	CloudCapacity: CheckConfig{
		Category:                AuditReportCloud,
		Type:                    CheckTypeItemBased,
		Title:                   "Managed service capacity",
		DefaultThreshold:        90,
		Unit:                    CheckUnitPercent,
		MessageTemplate:         `{{.ItemsWithToBe "instance"}} close to the capacity limit`,
		ConditionFormatTemplate: "Aurora Serverless ACU, ElastiCache Serverless ECPU/storage or Azure Redis memory/server load > <threshold> of the limit",
	},
}

func init() {
	cs := reflect.ValueOf(&DBExtChecks).Elem()
	for i := 0; i < cs.NumField(); i++ {
		ch := cs.Field(i).Addr().Interface().(*CheckConfig)
		ch.Id = CheckId(cs.Type().Field(i).Name)
		Checks.index[ch.Id] = ch
	}
}

func dbExtRule(id string, name string, check CheckConfig, severity Status, forDuration, keepFiring timeseries.Duration, description string) AlertingRule {
	return AlertingRule{
		Id:            AlertingRuleId(id),
		Name:          name,
		Source:        AlertSource{Type: AlertSourceTypeCheck, Check: &CheckSource{CheckId: check.Id}},
		Selector:      AppSelector{Type: AppSelectorTypeAll},
		Severity:      severity,
		For:           forDuration,
		KeepFiringFor: keepFiring,
		Templates:     AlertTemplates{Description: description},
		Enabled:       true,
		Builtin:       true,
	}
}

// dbExtBuiltinAlertingRules are the built-in rules for DBExtChecks.
func dbExtBuiltinAlertingRules() []AlertingRule {
	c := &DBExtChecks
	m := timeseries.Minute
	return []AlertingRule{
		dbExtRule("postgres-deadlocks", "Postgres deadlocks", c.PostgresDeadlocks, WARNING, 0, 10*m,
			"Transactions are being aborted by the deadlock detector. The application acquires locks in an inconsistent order; the aborted transactions must be retried."),
		dbExtRule("postgres-checksum-failures", "Postgres data checksum failures", c.PostgresChecksumFailures, CRITICAL, 0, timeseries.Hour,
			"Postgres found data pages whose checksum doesn't match: data on disk is corrupted. Check the storage and restore the affected relations from a backup."),
		dbExtRule("postgres-logical-replication", "Postgres logical replication is stuck", c.PostgresLogicalReplication, WARNING, 5*m, 5*m,
			"A logical replication subscription isn't applying changes. The subscriber falls behind and the publisher retains WAL for its slot."),
		dbExtRule("postgres-cache-hit-ratio", "Postgres low cache hit ratio", c.PostgresCacheHitRatio, WARNING, 15*m, 15*m,
			"Many block reads miss shared buffers. For an OLTP workload this usually means the working set no longer fits in memory or a query is scanning a large table."),
		dbExtRule("postgres-idle-in-transaction", "Postgres sessions idle in transaction", c.PostgresIdleInTransaction, WARNING, 10*m, 5*m,
			"Sessions keep transactions open without doing anything. They hold locks and snapshots, blocking vacuum and other queries."),
		dbExtRule("mysql-applier-lag", "Mysql replication applier lag", c.MysqlApplierLag, WARNING, 5*m, 5*m,
			"The replica applies the transactions of the source with a delay. Reads from the replica return stale data."),
		dbExtRule("pgbouncer-availability", "PgBouncer is unavailable", c.PgbouncerAvailability, WARNING, 2*m, 5*m,
			"The PgBouncer admin console can't be reached. Clients connecting through the pooler may be failing."),
		dbExtRule("pgbouncer-client-waiting", "PgBouncer clients waiting", c.PgbouncerClientWaiting, WARNING, 2*m, 5*m,
			"Clients wait for a server connection. The pool is too small for the load or the queries on the server became slower."),
		dbExtRule("pgbouncer-pool-saturation", "PgBouncer pool saturated", c.PgbouncerPoolSaturation, WARNING, 0, 5*m,
			"All server connections of a pool are busy and clients are queuing. Consider increasing pool_size or optimizing the queries."),
		dbExtRule("rabbitmq-availability", "RabbitMQ is unavailable", c.RabbitmqAvailability, WARNING, 2*m, 5*m,
			"The metrics endpoint of a RabbitMQ node can't be scraped. The node may be down."),
		dbExtRule("rabbitmq-alarms", "RabbitMQ resource alarm", c.RabbitmqAlarms, CRITICAL, 0, 5*m,
			"A memory, disk or file descriptor alarm is in effect: RabbitMQ blocks all publishing connections until it clears."),
		dbExtRule("rabbitmq-file-descriptors", "RabbitMQ file descriptors", c.RabbitmqFileDescriptors, WARNING, 5*m, 5*m,
			"A RabbitMQ node is close to its file descriptor limit. New connections will be refused once it is reached."),
		dbExtRule("etcd-availability", "etcd is unavailable", c.EtcdAvailability, WARNING, 2*m, 5*m,
			"The metrics endpoint of an etcd member can't be scraped. The member may be down."),
		dbExtRule("etcd-no-leader", "etcd has no leader", c.EtcdNoLeader, CRITICAL, m, 5*m,
			"An etcd member has no leader. The cluster can't process writes until a leader is elected."),
		dbExtRule("etcd-leader-changes", "etcd leader elections", c.EtcdLeaderChanges, WARNING, 0, 15*m,
			"The etcd cluster keeps re-electing its leader or failing proposals, usually because of slow disks, network issues or CPU starvation."),
		dbExtRule("etcd-disk-latency", "etcd slow disk", c.EtcdDiskLatency, WARNING, 10*m, 10*m,
			"WAL fsync or backend commit latency is above the etcd recommendations. Slow disks cause missed heartbeats and leader elections."),
		dbExtRule("etcd-db-size", "etcd database close to quota", c.EtcdDbSize, WARNING, 5*m, 15*m,
			"The etcd database is close to its space quota. Once exceeded, etcd raises a NOSPACE alarm and becomes read-only. Compact and defragment it or increase the quota."),
		dbExtRule("cloud-replica-lag", "Managed database replica lag", c.CloudReplicaLag, WARNING, 5*m, 5*m,
			"A read replica of a managed database is far behind its primary. Reads from the replica return stale data."),
		dbExtRule("cloud-capacity", "Managed service near capacity", c.CloudCapacity, WARNING, 10*m, 10*m,
			"A managed service is close to its capacity limit (Aurora Serverless max ACUs, ElastiCache Serverless limits, Azure Cache for Redis memory or server load)."),
	}
}

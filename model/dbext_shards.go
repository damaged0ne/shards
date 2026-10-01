package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/coroot/coroot/timeseries"
)

// Shards fork: models for the database, broker and cloud metrics added by the shards cluster agent
// (Postgres/MySQL extras, PgBouncer, RabbitMQ, etcd, Aurora, ElastiCache Serverless, MemoryDB, Azure)
// and for its self-observability metrics. Kept separate from the upstream files to simplify merges.

const (
	ApplicationTypeEtcd ApplicationType = "etcd"

	AuditReportPgbouncer AuditReportName = "PgBouncer"
	AuditReportRabbitmq  AuditReportName = "RabbitMQ"
	AuditReportEtcd      AuditReportName = "etcd"
	AuditReportCloud     AuditReportName = "Cloud"

	ApplicationKindAzureDB               ApplicationKind = "AzureDB"
	ApplicationKindAzureRedis            ApplicationKind = "AzureRedis"
	ApplicationKindElasticacheServerless ApplicationKind = "ElasticacheServerless"
	ApplicationKindMemoryDB              ApplicationKind = "MemoryDB"
)

// IsShardsCloudKind reports whether the application kind is one of the managed services discovered
// by the shards cluster agent (in addition to the upstream RDS/ElastiCache/Cloud SQL/Memorystore/OCI kinds).
func IsShardsCloudKind(k ApplicationKind) bool {
	switch k {
	case ApplicationKindAzureDB, ApplicationKindAzureRedis, ApplicationKindElasticacheServerless, ApplicationKindMemoryDB:
		return true
	}
	return false
}

// Postgres extras

type PgIndexKey struct {
	Db     string
	Schema string
	Table  string
	Index  string
}

func (k PgIndexKey) String() string {
	if k.Schema != "" && k.Schema != "public" {
		return fmt.Sprintf("%s: %s.%s.%s", k.Db, k.Schema, k.Table, k.Index)
	}
	return fmt.Sprintf("%s: %s.%s", k.Db, k.Table, k.Index)
}

type PgQueryStatExt struct {
	PlanTime        *timeseries.TimeSeries // seconds/second
	Rows            *timeseries.TimeSeries // per second
	SharedBlksHit   *timeseries.TimeSeries // per second
	SharedBlksRead  *timeseries.TimeSeries // per second
	TempBlksRead    *timeseries.TimeSeries // per second
	TempBlksWritten *timeseries.TimeSeries // per second
	WalBytes        *timeseries.TimeSeries // bytes/second
	ExecTimeMin     *timeseries.TimeSeries // seconds
	ExecTimeMean    *timeseries.TimeSeries // seconds
	ExecTimeMax     *timeseries.TimeSeries // seconds
}

type PgSubscription struct {
	WorkerUp     *timeseries.TimeSeries
	LastMsgAge   *timeseries.TimeSeries
	LastMsgDelay *timeseries.TimeSeries
	LatestEndAge *timeseries.TimeSeries
	ApplyErrors  *timeseries.TimeSeries // per second
	SyncErrors   *timeseries.TimeSeries // per second
}

// ApplyLag is the time since the subscriber last confirmed a WAL position to the publisher
// (keepalives update it on an idle publisher too, so it only grows when the apply worker is stuck or disconnected).
func (s *PgSubscription) ApplyLag() *timeseries.TimeSeries {
	return timeseries.NewAggregate(timeseries.Max).Add(s.LatestEndAge, s.LastMsgAge).Get()
}

type PgStandbyKey struct {
	ApplicationName string
	ClientAddr      string
}

func (k PgStandbyKey) String() string {
	if k.ApplicationName == "" {
		return k.ClientAddr
	}
	if k.ClientAddr == "" {
		return k.ApplicationName
	}
	return k.ApplicationName + " (" + k.ClientAddr + ")"
}

type PgStandby struct {
	ReplayLagSeconds *timeseries.TimeSeries
	ReplayLagBytes   *timeseries.TimeSeries
	State            LabelLastValue
	SyncState        LabelLastValue
}

// PgExt holds the metrics added by the shards cluster agent (pg_stat_database, pg_stat_io, pg_stat_wal,
// logical replication, indexes, wait events, extended pg_stat_statements columns). Counters are per-second rates.
type PgExt struct {
	XactCommit        map[string]*timeseries.TimeSeries // by db
	XactRollback      map[string]*timeseries.TimeSeries
	BlksHit           map[string]*timeseries.TimeSeries
	BlksRead          map[string]*timeseries.TimeSeries
	Deadlocks         map[string]*timeseries.TimeSeries
	Conflicts         map[string]*timeseries.TimeSeries
	TempBytes         map[string]*timeseries.TimeSeries
	ChecksumFailures  map[string]*timeseries.TimeSeries
	SessionsAbandoned map[string]*timeseries.TimeSeries
	SessionsFatal     map[string]*timeseries.TimeSeries
	SessionsKilled    map[string]*timeseries.TimeSeries
	IdleInTxTime      map[string]*timeseries.TimeSeries // seconds/second

	IOReads     map[string]*timeseries.TimeSeries // by backend_type
	IOWrites    map[string]*timeseries.TimeSeries
	IOExtends   map[string]*timeseries.TimeSeries
	IOFsyncs    map[string]*timeseries.TimeSeries
	IOHits      map[string]*timeseries.TimeSeries
	IOEvictions map[string]*timeseries.TimeSeries
	IOTime      map[string]*timeseries.TimeSeries // read+write+extend+fsync seconds/second

	WalRecords     *timeseries.TimeSeries
	WalFpi         *timeseries.TimeSeries
	WalBytes       *timeseries.TimeSeries
	WalBuffersFull *timeseries.TimeSeries

	Subscriptions map[string]*PgSubscription
	Standbys      map[PgStandbyKey]*PgStandby

	UnusedIndexBytes     map[PgIndexKey]*timeseries.TimeSeries
	DbUnusedIndexes      map[string]*timeseries.TimeSeries
	DbUnusedIndexesBytes map[string]*timeseries.TimeSeries
	DbDuplicateIndexes   map[string]*timeseries.TimeSeries

	WaitEvents map[string]*timeseries.TimeSeries // by "<type>: <event>"

	PerQuery map[QueryKey]*PgQueryStatExt
}

func newPgExt() *PgExt {
	return &PgExt{
		XactCommit:           map[string]*timeseries.TimeSeries{},
		XactRollback:         map[string]*timeseries.TimeSeries{},
		BlksHit:              map[string]*timeseries.TimeSeries{},
		BlksRead:             map[string]*timeseries.TimeSeries{},
		Deadlocks:            map[string]*timeseries.TimeSeries{},
		Conflicts:            map[string]*timeseries.TimeSeries{},
		TempBytes:            map[string]*timeseries.TimeSeries{},
		ChecksumFailures:     map[string]*timeseries.TimeSeries{},
		SessionsAbandoned:    map[string]*timeseries.TimeSeries{},
		SessionsFatal:        map[string]*timeseries.TimeSeries{},
		SessionsKilled:       map[string]*timeseries.TimeSeries{},
		IdleInTxTime:         map[string]*timeseries.TimeSeries{},
		IOReads:              map[string]*timeseries.TimeSeries{},
		IOWrites:             map[string]*timeseries.TimeSeries{},
		IOExtends:            map[string]*timeseries.TimeSeries{},
		IOFsyncs:             map[string]*timeseries.TimeSeries{},
		IOHits:               map[string]*timeseries.TimeSeries{},
		IOEvictions:          map[string]*timeseries.TimeSeries{},
		IOTime:               map[string]*timeseries.TimeSeries{},
		Subscriptions:        map[string]*PgSubscription{},
		Standbys:             map[PgStandbyKey]*PgStandby{},
		UnusedIndexBytes:     map[PgIndexKey]*timeseries.TimeSeries{},
		DbUnusedIndexes:      map[string]*timeseries.TimeSeries{},
		DbUnusedIndexesBytes: map[string]*timeseries.TimeSeries{},
		DbDuplicateIndexes:   map[string]*timeseries.TimeSeries{},
		WaitEvents:           map[string]*timeseries.TimeSeries{},
		PerQuery:             map[QueryKey]*PgQueryStatExt{},
	}
}

func (p *Postgres) GetOrCreateExt() *PgExt {
	if p.Ext == nil {
		p.Ext = newPgExt()
	}
	return p.Ext
}

func SumSeries(m map[string]*timeseries.TimeSeries) *timeseries.TimeSeries {
	agg := timeseries.NewAggregate(timeseries.NanSum)
	for _, ts := range m {
		agg.Add(ts)
	}
	return agg.Get()
}

// CacheHitRatio is the share of the block reads served from shared buffers (blks_hit / (blks_hit + blks_read)), in percent.
func (e *PgExt) CacheHitRatio() *timeseries.TimeSeries {
	if e == nil {
		return nil
	}
	return timeseries.Aggregate2(SumSeries(e.BlksHit), SumSeries(e.BlksRead), func(hit, read float32) float32 {
		if timeseries.IsNaN(hit) || timeseries.IsNaN(read) || hit+read <= 0 {
			return timeseries.NaN
		}
		return hit / (hit + read) * 100
	})
}

// BlockReads is the total rate of block requests (hits + reads), used to skip idle databases in the cache hit check.
func (e *PgExt) BlockReads() *timeseries.TimeSeries {
	if e == nil {
		return nil
	}
	return timeseries.NewAggregate(timeseries.NanSum).Add(SumSeries(e.BlksHit), SumSeries(e.BlksRead)).Get()
}

// MySQL extras

type MysqlIndexKey struct {
	Schema string
	Table  string
	Index  string
}

func (k MysqlIndexKey) String() string {
	return fmt.Sprintf("%s.%s.%s", k.Schema, k.Table, k.Index)
}

type MysqlExt struct {
	WaitEventTime  map[string]*timeseries.TimeSeries // seconds/second, by event
	WaitEventCount map[string]*timeseries.TimeSeries // per second, by event

	ApplierLastTransactionLag map[string]*timeseries.TimeSeries // by channel
	ApplierCurrentLag         map[string]*timeseries.TimeSeries // by channel

	UnusedIndexes       map[MysqlIndexKey]*timeseries.TimeSeries // 1 if unused
	UnusedIndexBytes    map[MysqlIndexKey]*timeseries.TimeSeries
	SchemaUnusedIndexes map[string]*timeseries.TimeSeries
}

func (m *Mysql) GetOrCreateExt() *MysqlExt {
	if m.Ext == nil {
		m.Ext = &MysqlExt{
			WaitEventTime:             map[string]*timeseries.TimeSeries{},
			WaitEventCount:            map[string]*timeseries.TimeSeries{},
			ApplierLastTransactionLag: map[string]*timeseries.TimeSeries{},
			ApplierCurrentLag:         map[string]*timeseries.TimeSeries{},
			UnusedIndexes:             map[MysqlIndexKey]*timeseries.TimeSeries{},
			UnusedIndexBytes:          map[MysqlIndexKey]*timeseries.TimeSeries{},
			SchemaUnusedIndexes:       map[string]*timeseries.TimeSeries{},
		}
	}
	return m.Ext
}

// ApplierLag is the worst of the applier lags of all the replication channels.
func (e *MysqlExt) ApplierLag() *timeseries.TimeSeries {
	if e == nil {
		return nil
	}
	agg := timeseries.NewAggregate(timeseries.Max)
	for _, ts := range e.ApplierLastTransactionLag {
		agg.Add(ts)
	}
	for _, ts := range e.ApplierCurrentLag {
		agg.Add(ts)
	}
	return agg.Get()
}

// PgBouncer

type PgbouncerPoolKey struct {
	Database string
	User     string
}

func (k PgbouncerPoolKey) String() string {
	return k.User + "@" + k.Database
}

type PgbouncerPool struct {
	ClientActive  *timeseries.TimeSeries
	ClientWaiting *timeseries.TimeSeries
	ServerActive  *timeseries.TimeSeries
	ServerIdle    *timeseries.TimeSeries
	ServerUsed    *timeseries.TimeSeries
	MaxWait       *timeseries.TimeSeries // seconds
}

// Saturated reports whether clients have been waiting while no server connection was idle over the last n points:
// the pool can't grow (pool_size is reached or the server refuses new connections).
func (p *PgbouncerPool) Saturated(n int) bool {
	return LastNMin(p.ClientWaiting, n, 0) > 0 && p.ServerIdle.LastNMax(n, 0) <= 0
}

// LastNMin returns the minimum of the last n points (NaNs are skipped), or defaultValue if there are none.
func LastNMin(ts *timeseries.TimeSeries, n int, defaultValue float32) float32 {
	if ts.IsEmpty() {
		return defaultValue
	}
	return -ts.Map(func(t timeseries.Time, v float32) float32 { return -v }).LastNMax(n, -defaultValue)
}

type PgbouncerStats struct {
	Queries      *timeseries.TimeSeries // per second
	Transactions *timeseries.TimeSeries // per second
	QueryTime    *timeseries.TimeSeries // seconds/second
	ClientWait   *timeseries.TimeSeries // seconds/second
	Received     *timeseries.TimeSeries // bytes/second
	Sent         *timeseries.TimeSeries // bytes/second
}

// AvgQueryDuration is the average duration of the queries executed through the pooler, in seconds.
func (s *PgbouncerStats) AvgQueryDuration() *timeseries.TimeSeries {
	return timeseries.Aggregate2(s.QueryTime, s.Queries, func(t, q float32) float32 {
		if q <= 0 || timeseries.IsNaN(q) || timeseries.IsNaN(t) {
			return timeseries.NaN
		}
		return t / q
	})
}

type Pgbouncer struct {
	Up    *timeseries.TimeSeries
	Pools map[PgbouncerPoolKey]*PgbouncerPool
	Stats map[string]*PgbouncerStats // by database
}

func NewPgbouncer() *Pgbouncer {
	return &Pgbouncer{Pools: map[PgbouncerPoolKey]*PgbouncerPool{}, Stats: map[string]*PgbouncerStats{}}
}

func (p *Pgbouncer) IsUp() bool {
	return p != nil && p.Up.Last() == 1
}

func (p *Pgbouncer) PoolsSorted() []PgbouncerPoolKey {
	res := make([]PgbouncerPoolKey, 0, len(p.Pools))
	for k := range p.Pools {
		res = append(res, k)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].String() < res[j].String() })
	return res
}

// RabbitMQ (rabbitmq_prometheus plugin, aggregated /metrics endpoint)

type Rabbitmq struct {
	Up      *timeseries.TimeSeries
	Version LabelLastValue
	Node    LabelLastValue

	MessagesReady   *timeseries.TimeSeries
	MessagesUnacked *timeseries.TimeSeries
	Published       *timeseries.TimeSeries // per second (messages received from publishers)
	Delivered       *timeseries.TimeSeries // per second
	Unroutable      *timeseries.TimeSeries // per second (dropped + returned)
	Consumers       *timeseries.TimeSeries
	Connections     *timeseries.TimeSeries
	Queues          *timeseries.TimeSeries

	MemoryUsed    *timeseries.TimeSeries
	MemoryLimit   *timeseries.TimeSeries
	DiskAvailable *timeseries.TimeSeries
	DiskLimit     *timeseries.TimeSeries
	OpenFds       *timeseries.TimeSeries
	MaxFds        *timeseries.TimeSeries

	UnreachablePeers *timeseries.TimeSeries

	Alarms map[string]*timeseries.TimeSeries // memory_used_watermark, free_disk_space_watermark, file_descriptor_limit
}

func NewRabbitmq() *Rabbitmq {
	return &Rabbitmq{Alarms: map[string]*timeseries.TimeSeries{}}
}

func (r *Rabbitmq) IsUp() bool {
	return r != nil && r.Up.Last() == 1
}

// ActiveAlarms returns the names of the alarms in effect now.
func (r *Rabbitmq) ActiveAlarms() []string {
	var res []string
	for name, ts := range r.Alarms {
		if ts.Last() > 0 {
			res = append(res, strings.ReplaceAll(name, "_", " "))
		}
	}
	sort.Strings(res)
	return res
}

// etcd (native /metrics endpoint)

type Etcd struct {
	Up      *timeseries.TimeSeries
	Version LabelLastValue

	HasLeader        *timeseries.TimeSeries
	IsLeader         *timeseries.TimeSeries
	LeaderChanges    *timeseries.TimeSeries // per second
	ProposalsFailed  *timeseries.TimeSeries // per second
	ProposalsPending *timeseries.TimeSeries
	ProposalsApplied *timeseries.TimeSeries // per second

	WalFsyncP99      *timeseries.TimeSeries // seconds
	BackendCommitP99 *timeseries.TimeSeries // seconds
	PeerRttP99       *timeseries.TimeSeries // seconds

	DbSize      *timeseries.TimeSeries
	DbSizeInUse *timeseries.TimeSeries
	Quota       *timeseries.TimeSeries
}

// EtcdDefaultQuotaBytes is the default --quota-backend-bytes (2 GiB).
const EtcdDefaultQuotaBytes = 2 * 1024 * 1024 * 1024

func (e *Etcd) IsUp() bool {
	return e != nil && e.Up.Last() == 1
}

// DbSizePercent is the size of the backend database relative to the space quota, in percent.
func (e *Etcd) DbSizePercent() *timeseries.TimeSeries {
	quota := e.Quota
	if quota.IsEmpty() {
		quota = e.DbSize.WithNewValue(EtcdDefaultQuotaBytes)
	}
	return timeseries.Aggregate2(e.DbSize, quota, func(size, q float32) float32 {
		if q <= 0 || timeseries.IsNaN(q) || timeseries.IsNaN(size) {
			return timeseries.NaN
		}
		return size / q * 100
	})
}

// Aurora (RDS instances that are members of an Aurora cluster)

type Aurora struct {
	Role                  LabelLastValue         // writer or reader
	ReplicaLag            *timeseries.TimeSeries // seconds
	ServerlessCapacity    *timeseries.TimeSeries // ACU
	ServerlessUsage       *timeseries.TimeSeries // percent of the max capacity
	ServerlessMinCapacity *timeseries.TimeSeries // ACU
	ServerlessMaxCapacity *timeseries.TimeSeries // ACU
}

func (r *Rds) GetOrCreateAurora() *Aurora {
	if r.Aurora == nil {
		r.Aurora = &Aurora{}
	}
	return r.Aurora
}

// ServerlessUtilization is the ACU utilization in percent (ACUUtilization, or the capacity relative to the max capacity).
func (a *Aurora) ServerlessUtilization() *timeseries.TimeSeries {
	if a == nil {
		return nil
	}
	if !a.ServerlessUsage.IsEmpty() {
		return a.ServerlessUsage
	}
	return timeseries.Aggregate2(a.ServerlessCapacity, a.ServerlessMaxCapacity, func(c, m float32) float32 {
		if m <= 0 || timeseries.IsNaN(m) || timeseries.IsNaN(c) {
			return timeseries.NaN
		}
		return c / m * 100
	})
}

// Managed services discovered by the shards cluster agent: Azure Database for PostgreSQL/MySQL flexible servers,
// Azure Cache for Redis, ElastiCache Serverless caches and MemoryDB clusters.

type CloudService struct {
	Provider string // CloudProviderAWS or CloudProviderAzure
	Service  string // "Azure Database", "Azure Cache for Redis", "ElastiCache Serverless", "MemoryDB"
	Id       string

	Status        LabelLastValue
	Engine        LabelLastValue
	EngineVersion LabelLastValue
	Role          LabelLastValue // replication role (Azure)

	LifeSpan *timeseries.TimeSeries

	CpuUsagePercent    *timeseries.TimeSeries
	MemoryUsagePercent *timeseries.TimeSeries
	StorageUsedBytes   *timeseries.TimeSeries
	StorageLimitBytes  *timeseries.TimeSeries
	StorageUsedPercent *timeseries.TimeSeries
	IOPS               *timeseries.TimeSeries
	IOConsumption      *timeseries.TimeSeries // percent of the provisioned IOPS (Azure MySQL)
	Connections        *timeseries.TimeSeries
	ReplicaLag         *timeseries.TimeSeries // seconds
	ServerLoad         *timeseries.TimeSeries // percent (Azure Redis)
	ECPU               *timeseries.TimeSeries // ElastiCache Processing Units per second
	ECPULimit          *timeseries.TimeSeries
	Nodes              map[string]LabelLastValue // MemoryDB nodes: status by "<shard>/<node>"
}

func (c *CloudService) ApplicationType() ApplicationType {
	if c == nil {
		return ApplicationTypeUnknown
	}
	switch strings.ToLower(c.Engine.Value()) {
	case "postgres", "postgresql":
		return ApplicationTypePostgres
	case "mysql":
		return ApplicationTypeMysql
	case "redis":
		return ApplicationTypeRedis
	case "valkey":
		return ApplicationTypeValkey
	case "memcached":
		return ApplicationTypeMemcached
	}
	return ApplicationTypeUnknown
}

// IsUp reports whether the service is in a healthy state according to the cloud API.
func (c *CloudService) IsUp() bool {
	if c == nil {
		return false
	}
	switch strings.ToLower(c.Status.Value()) {
	case "ready", "available", "succeeded", "running", "active":
		return true
	}
	return false
}

// StorageUsage is the used storage relative to the limit, in percent.
func (c *CloudService) StorageUsage() *timeseries.TimeSeries {
	if c == nil {
		return nil
	}
	if !c.StorageUsedPercent.IsEmpty() {
		return c.StorageUsedPercent
	}
	return timeseries.Aggregate2(c.StorageUsedBytes, c.StorageLimitBytes, func(u, l float32) float32 {
		if l <= 0 || timeseries.IsNaN(l) || timeseries.IsNaN(u) {
			return timeseries.NaN
		}
		return u / l * 100
	})
}

func (c *CloudService) ECPUUsage() *timeseries.TimeSeries {
	if c == nil {
		return nil
	}
	return timeseries.Aggregate2(c.ECPU, c.ECPULimit, func(u, l float32) float32 {
		if l <= 0 || timeseries.IsNaN(l) || timeseries.IsNaN(u) {
			return timeseries.NaN
		}
		return u / l * 100
	})
}

type Azure struct {
	Configured      bool
	DiscoveryErrors map[string]bool
}

// Cluster agent self-observability (coroot_cluster_agent_target_collect_*).

type ClusterAgentTarget struct {
	Type     string
	Address  string
	Success  *timeseries.TimeSeries
	Duration *timeseries.TimeSeries
	Timeouts *timeseries.TimeSeries // per second
}

func (t *ClusterAgentTarget) String() string {
	return t.Type + "://" + t.Address
}

// Failing reports whether the recent collections of the target didn't complete within the deadline.
func (t *ClusterAgentTarget) Failing(n int) bool {
	if t.Success.IsEmpty() || t.Success.TailIsEmpty() {
		return false
	}
	return t.Success.LastNMax(n, 1) < 1
}

type ClusterAgentStatus struct {
	Targets map[string]*ClusterAgentTarget // by "<type>://<address>"
}

func (s *ClusterAgentStatus) GetOrCreateTarget(typ, address string) *ClusterAgentTarget {
	if s.Targets == nil {
		s.Targets = map[string]*ClusterAgentTarget{}
	}
	key := typ + "://" + address
	t := s.Targets[key]
	if t == nil {
		t = &ClusterAgentTarget{Type: typ, Address: address}
		s.Targets[key] = t
	}
	return t
}

func (s *ClusterAgentStatus) TargetsSorted() []*ClusterAgentTarget {
	res := make([]*ClusterAgentTarget, 0, len(s.Targets))
	for _, t := range s.Targets {
		res = append(res, t)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].String() < res[j].String() })
	return res
}

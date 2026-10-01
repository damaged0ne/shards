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

// 40 points of 30s: 20 minutes, longer than the longest evaluation window (the 15-minute lag trend)
const ctPoints = 40

func constant(v float32) []float32 {
	res := make([]float32, ctPoints)
	for i := range res {
		res[i] = v
	}
	return res
}

func ramp(from, step float32) []float32 {
	res := make([]float32, ctPoints)
	for i := range res {
		res[i] = from + float32(i)*step
	}
	return res
}

// lastN is v for the last n points and `before` before them
func lastN(n int, before, v float32) []float32 {
	res := constant(before)
	for i := ctPoints - n; i < ctPoints; i++ {
		res[i] = v
	}
	return res
}

func (tm testMetrics) target(query, address string, labels map[string]string, values []float32) {
	ls := map[string]string{"address": address}
	for k, v := range labels {
		ls[k] = v
	}
	tm.add(query, "", ls, values...)
}

// replace replaces the series of the query reported by the target
func (tm testMetrics) replace(query, address string, labels map[string]string, values []float32) {
	var kept []*model.MetricValues
	for _, mv := range tm[query] {
		if mv.Labels["address"] != address {
			kept = append(kept, mv)
		}
	}
	tm[query] = kept
	tm.target(query, address, labels, values)
}

type ctWorld struct {
	t       *testing.T
	w       *model.World
	project *db.Project
}

func newCTWorld(t *testing.T, m testMetrics, prepare func(w *model.World)) *ctWorld {
	project := &db.Project{Id: "p1"}
	w := model.NewWorld(testFrom, testFrom.Add(ctPoints*testStep), testStep, testStep)
	if prepare != nil {
		prepare(w)
	}
	enrichInstances(w, m, map[string]*model.Instance{}, promJobStatuses{})
	auditor.Audit(w, project, nil, nil)
	return &ctWorld{t: t, w: w, project: project}
}

func targetAppId(address string) model.ApplicationId {
	return model.NewApplicationId(model.ClusterIdExternal, "external", model.ApplicationKindExternalService, address)
}

func (cw *ctWorld) app(address string) *model.Application {
	app := cw.w.GetApplication(targetAppId(address))
	require.NotNil(cw.t, app, address)
	return app
}

func (cw *ctWorld) check(address string, id model.CheckId) *model.Check {
	app := cw.app(address)
	for _, r := range app.Reports {
		for _, ch := range r.Checks {
			if ch.Id == id {
				return ch
			}
		}
	}
	require.Failf(cw.t, "check not found", "%s %s", address, id)
	return nil
}

// detailed audits the app with the widgets and returns the report.
func (cw *ctWorld) detailed(address string, name model.AuditReportName) *model.AuditReport {
	app := cw.app(address)
	app.Reports = nil
	app.Status = model.UNKNOWN
	auditor.Audit(cw.w, cw.project, app, nil)
	for _, r := range app.Reports {
		if r.Name == name {
			return r
		}
	}
	require.Failf(cw.t, "report not found", "%s %s", address, name)
	return nil
}

func tableByHeader(r *model.AuditReport, first string) *model.Table {
	for _, w := range r.Widgets {
		if w.Table != nil && w.Table.Header[0] == first {
			return w.Table
		}
	}
	return nil
}

func chartTitles(r *model.AuditReport) []string {
	var res []string
	for _, w := range r.Widgets {
		if w.Chart != nil {
			res = append(res, w.Chart.Title)
		}
		if w.ChartGroup != nil {
			res = append(res, w.ChartGroup.Title)
		}
	}
	return res
}

func kafkaCluster(m testMetrics, addr, clusterId string) {
	m.target(qKafkaUp, addr, nil, constant(1))
	m.target(qKafkaScrapeError, addr, map[string]string{"error": "", "warning": ""}, constant(0))
	m.target(qKafkaClusterInfo, addr, map[string]string{"cluster_id": clusterId}, constant(1))
	m.target(qKafkaControllerId, addr, nil, constant(1))
	m.target(qKafkaBrokers, addr, nil, constant(3))
	topic := map[string]string{"topic": "orders"}
	m.target(qKafkaTopicPartitions, addr, topic, constant(6))
	m.target(qKafkaTopicProduceRate, addr, topic, constant(100))
}

func kafkaGroup(m testMetrics, addr, group, state string, lag, consume []float32) {
	g := map[string]string{"consumergroup": group}
	gt := map[string]string{"consumergroup": group, "topic": "orders"}
	m.target(qKafkaConsumerGroupMembers, addr, g, constant(2))
	m.target(qKafkaConsumerGroupState, addr, map[string]string{"consumergroup": group, "state": state}, constant(1))
	m.target(qKafkaConsumerGroupLag, addr, gt, lag)
	m.target(qKafkaConsumerGroupConsumeRate, addr, gt, consume)
}

func TestKafkaTargets(t *testing.T) {
	const (
		healthy  = "10.0.0.1:9092"
		broken   = "10.0.0.2:9092"
		stalled  = "10.0.0.3:9092"
		empty    = "10.0.0.4:9092"
		down     = "10.0.0.5:9092"
		timeLag  = "10.0.0.6:9092"
		follower = "10.0.0.7:9092"
	)
	m := testMetrics{}
	topic := map[string]string{"topic": "orders"}

	kafkaCluster(m, healthy, "c1")
	m.target(qKafkaTopicUnderReplicated, healthy, topic, constant(0))
	m.target(qKafkaTopicOffline, healthy, topic, constant(0))
	kafkaGroup(m, healthy, "billing", "Stable", constant(10), constant(100))
	// a group catching up after a spike: the lag shrinks
	kafkaGroup(m, healthy, "catching-up", "Stable", ramp(5000, -100), constant(150))

	kafkaCluster(m, broken, "c2")
	m.target(qKafkaTopicUnderReplicated, broken, topic, constant(2))
	m.target(qKafkaTopicOffline, broken, topic, lastN(2, 0, 1))
	kafkaGroup(m, broken, "slow", "Stable", ramp(100, 50), constant(50)) // consumes 50/s while 100/s are produced

	kafkaCluster(m, stalled, "c3")
	m.target(qKafkaTopicUnderReplicated, stalled, topic, constant(0))
	kafkaGroup(m, stalled, "stuck", "Stable", constant(500), constant(0))

	kafkaCluster(m, empty, "c4")
	m.target(qKafkaTopicUnderReplicated, empty, topic, lastN(2, 0, 1)) // a broker restart: not sustained
	kafkaGroup(m, empty, "gone", "Empty", constant(5), constant(0))

	m.target(qKafkaUp, down, nil, constant(0))
	m.target(qKafkaScrapeError, down, map[string]string{"error": "auth", "warning": ""}, constant(1))

	kafkaCluster(m, timeLag, "c6")
	kafkaGroup(m, timeLag, "batch", "Stable", constant(100000), constant(100)) // 1000s behind, not growing

	// a broker that leaves the cluster-wide metrics to another target (clusterMetrics: lowest-broker)
	m.target(qKafkaUp, follower, nil, constant(1))

	cw := newCTWorld(t, m, nil)

	app := cw.app(healthy)
	require.Len(t, app.Instances, 1)
	k := app.Instances[0].Kafka
	require.NotNil(t, k)
	assert.Equal(t, "c1", k.ClusterId.Value())
	assert.Equal(t, float32(3), k.Brokers.Last())
	assert.Equal(t, float32(6), k.Topics["orders"].Partitions.Last())
	assert.Equal(t, float32(10), k.ConsumerGroups["billing"].Lag().Last())
	assert.Equal(t, "Stable", k.ConsumerGroups["billing"].State.Value())
	assert.True(t, app.IsQueue())
	assert.Equal(t, model.ApplicationTypeKafka, app.Instances[0].InstrumentedType())

	ok := func(addr string, id model.CheckId) {
		assert.Equal(t, model.OK, cw.check(addr, id).Status, "%s %s", addr, id)
	}
	ok(healthy, model.Checks.KafkaAvailability.Id)
	ok(healthy, model.Checks.KafkaOfflinePartitions.Id)
	ok(healthy, model.Checks.KafkaUnderReplicatedPartitions.Id)
	ok(healthy, model.Checks.KafkaConsumerLag.Id)
	assert.Equal(t, model.OK, cw.app(healthy).Status)

	ch := cw.check(broken, model.Checks.KafkaOfflinePartitions.Id)
	assert.Equal(t, model.CRITICAL, ch.Status)
	assert.Equal(t, "1 topic has offline partitions", ch.Message)
	ch = cw.check(broken, model.Checks.KafkaUnderReplicatedPartitions.Id)
	assert.Equal(t, model.WARNING, ch.Status)
	assert.Equal(t, []string{"orders: 2 under-replicated partitions"}, ch.Details.Items())
	ch = cw.check(broken, model.Checks.KafkaConsumerLag.Id)
	assert.Equal(t, model.WARNING, ch.Status)
	assert.Equal(t, "1 consumer group is falling behind", ch.Message)
	assert.Contains(t, ch.Details.Items()[0], "slow: the lag keeps growing")
	assert.Equal(t, model.CRITICAL, cw.app(broken).Status)

	ch = cw.check(stalled, model.Checks.KafkaConsumerLag.Id)
	assert.Equal(t, model.CRITICAL, ch.Status)
	assert.Contains(t, ch.Details.Items()[0], "stuck: stalled")

	ch = cw.check(empty, model.Checks.KafkaConsumerLag.Id)
	assert.Equal(t, model.CRITICAL, ch.Status)
	assert.Contains(t, ch.Details.Items()[0], "no active consumers (Empty)")
	ok(empty, model.Checks.KafkaUnderReplicatedPartitions.Id)

	ch = cw.check(down, model.Checks.KafkaAvailability.Id)
	assert.Equal(t, model.WARNING, ch.Status)
	assert.Equal(t, []string{down + ": authentication failed"}, ch.Details.Items())

	ch = cw.check(timeLag, model.Checks.KafkaConsumerLag.Id)
	assert.Equal(t, model.WARNING, ch.Status)
	assert.Contains(t, ch.Details.Items()[0], "estimated time lag 16 minutes")

	ok(follower, model.Checks.KafkaAvailability.Id)
	ok(follower, model.Checks.KafkaConsumerLag.Id)

	// the widgets
	r := cw.detailed(broken, model.AuditReportKafka)
	topics := tableByHeader(r, "Topic")
	require.NotNil(t, topics)
	require.Len(t, topics.Rows, 1)
	assert.Equal(t, "orders", topics.Rows[0].Cells[0].Value)
	assert.Equal(t, "100", topics.Rows[0].Cells[4].Value) // produce rate
	groups := tableByHeader(r, "Consumer group")
	require.NotNil(t, groups)
	require.Len(t, groups.Rows, 1)
	assert.Equal(t, []string{"growing"}, groups.Rows[0].Cells[4].Tags)
	assert.NotNil(t, groups.Rows[0].Cells[5].Chart)
	assert.NotNil(t, tableByHeader(r, "Target"))
	assert.Subset(t, chartTitles(r), []string{"Produce rate by topic, messages/second", "Consume rate by consumer group, messages/second",
		"Consumer lag by group, messages", "Estimated time lag by group, seconds", "Unhealthy partitions"})
	assert.Equal(t, model.CRITICAL, r.Status)
}

func TestKafkaTargetAttachedToContainerInstance(t *testing.T) {
	m := testMetrics{}
	kafkaCluster(m, "10.0.1.1:9092", "c1")
	var instance *model.Instance
	cw := newCTWorld(t, m, func(w *model.World) {
		app := w.GetOrCreateApplication(model.NewApplicationId("p1", "shop", model.ApplicationKindDockerSwarmService, "kafka"), false)
		instance = app.GetOrCreateInstance("kafka.1", nil)
		instance.TcpListens[model.Listen{IP: "10.0.1.1", Port: "9092"}] = true
	})
	require.NotNil(t, instance.Kafka)
	assert.Equal(t, "c1", instance.Kafka.ClusterId.Value())
	assert.Nil(t, cw.w.GetApplication(targetAppId("10.0.1.1:9092")), "no separate application for a matched target")
	found := false
	for _, r := range instance.Owner.Reports {
		found = found || r.Name == model.AuditReportKafka
	}
	assert.True(t, found)
}

func clickhouseServer(m testMetrics, addr string) {
	m.target(qClickHouseUp, addr, nil, constant(1))
	m.target(qClickHouseInfo, addr, map[string]string{"server_version": "24.8.1"}, constant(1))
	m.target(qClickHouseUptime, addr, nil, ramp(3600, 30))
	m.target(qClickHouseQueries, addr, map[string]string{"kind": "all"}, constant(10))
	m.target(qClickHouseFailedQueries, addr, map[string]string{"kind": "all"}, constant(0))
	m.target(qClickHouseQueriesRunning, addr, nil, constant(2))
	m.target(qClickHouseMemoryTracking, addr, nil, constant(1e9))
	m.target(qClickHouseOSMemoryTotal, addr, nil, constant(8e9))
	m.target(qClickHouseReadonlyReplicas, addr, nil, constant(0))
	m.target(qClickHouseReplicasMaxDelay, addr, nil, constant(0))
	m.target(qClickHouseMaxPartsPerPtn, addr, nil, constant(20))
	m.target(qClickHouseRejectedInserts, addr, nil, constant(0))
	m.target(qClickHouseDataLoss, addr, nil, constant(0))
	tbl := map[string]string{"db": "default", "table": "events"}
	m.target(qClickHouseTableParts, addr, tbl, constant(40))
	m.target(qClickHouseTableSize, addr, tbl, constant(1e10))
	m.target(qClickHouseTableMaxPartsPerPtn, addr, tbl, constant(20))
	m.target(qClickHouseMutationsStuck, addr, tbl, constant(0))
	m.target(qClickHouseMutationsFailing, addr, tbl, constant(0))
	q := map[string]string{"db": "default", "query": "SELECT count() FROM events WHERE ts > ?"}
	m.target(qClickHouseTopQueryCalls, addr, q, constant(2))
	m.target(qClickHouseTopQueryTime, addr, q, constant(0.5))
	m.target(qClickHouseTopQueryReadRows, addr, q, constant(1e6))
	m.target(qClickHouseTopQueryErrors, addr, q, constant(0))
	q2 := map[string]string{"db": "default", "query": "INSERT INTO events VALUES"}
	m.target(qClickHouseTopQueryCalls, addr, q2, constant(5))
	m.target(qClickHouseTopQueryTime, addr, q2, constant(0.05))
}

func TestClickHouseTargets(t *testing.T) {
	const (
		healthy = "10.0.2.1:9000"
		broken  = "10.0.2.2:9000"
		lagging = "10.0.2.3:9000"
	)
	m := testMetrics{}
	clickhouseServer(m, healthy)

	clickhouseServer(m, broken)
	m.replace(qClickHouseReadonlyReplicas, broken, nil, constant(1))
	m.target(qClickHouseReplicaReadonly, broken, map[string]string{"db": "default", "table": "events"}, constant(1))
	m.replace(qClickHouseMaxPartsPerPtn, broken, nil, constant(500))
	m.target(qClickHouseTableMaxPartsPerPtn, broken, map[string]string{"db": "default", "table": "logs"}, constant(500))
	m.target(qClickHouseMutationsStuck, broken, map[string]string{"db": "default", "table": "logs"}, constant(1))
	m.replace(qClickHouseRejectedInserts, broken, nil, lastN(4, 0, 0.1)) // 4 points * 30s * 0.1/s = 12 inserts

	clickhouseServer(m, lagging)
	m.replace(qClickHouseReplicasMaxDelay, lagging, nil, constant(400))
	m.target(qClickHouseReplicaDelay, lagging, map[string]string{"db": "default", "table": "events"}, constant(400))
	m.replace(qClickHouseMaxPartsPerPtn, lagging, nil, constant(1500)) // over parts_to_delay_insert: critical

	cw := newCTWorld(t, m, nil)

	ch := cw.app(healthy).Instances[0].ClickHouse
	require.NotNil(t, ch)
	assert.Equal(t, "24.8.1", ch.Version.Value())
	assert.Len(t, ch.PerQuery, 2)
	assert.True(t, cw.app(healthy).IsDatabase())

	for _, id := range []model.CheckId{model.Checks.ClickHouseAvailability.Id, model.Checks.ClickHouseReplication.Id,
		model.Checks.ClickHouseTooManyParts.Id, model.Checks.ClickHouseStuckMutations.Id, model.Checks.ClickHouseRejectedInserts.Id} {
		assert.Equal(t, model.OK, cw.check(healthy, id).Status, id)
	}

	c := cw.check(broken, model.Checks.ClickHouseReplication.Id)
	assert.Equal(t, model.CRITICAL, c.Status)
	assert.Contains(t, c.Details.Items()[0], "read-only")
	c = cw.check(broken, model.Checks.ClickHouseTooManyParts.Id)
	assert.Equal(t, model.WARNING, c.Status)
	assert.Equal(t, "1 table with too many active parts in a partition (max: 500)", c.Message)
	c = cw.check(broken, model.Checks.ClickHouseStuckMutations.Id)
	assert.Equal(t, model.WARNING, c.Status)
	assert.Equal(t, "1 table has stuck or failing mutations", c.Message)
	c = cw.check(broken, model.Checks.ClickHouseRejectedInserts.Id)
	assert.Equal(t, model.WARNING, c.Status)
	assert.Equal(t, `12 INSERTs rejected with "Too many parts" in the last 5 minutes`, c.Message)
	assert.Equal(t, model.CRITICAL, cw.app(broken).Status)

	c = cw.check(lagging, model.Checks.ClickHouseReplication.Id)
	assert.Equal(t, model.WARNING, c.Status)
	assert.Contains(t, c.Details.Items()[0], "default.events")
	c = cw.check(lagging, model.Checks.ClickHouseTooManyParts.Id)
	assert.Equal(t, model.CRITICAL, c.Status)

	r := cw.detailed(broken, model.AuditReportClickHouse)
	queries := tableByHeader(r, "Query")
	require.NotNil(t, queries)
	require.Len(t, queries.Rows, 2)
	assert.Equal(t, "SELECT count() FROM events WHERE ts > ?", queries.Rows[0].Cells[0].Value) // sorted by the total time
	assert.Equal(t, "250ms", queries.Rows[0].Cells[3].Value)
	tables := tableByHeader(r, "Table")
	require.NotNil(t, tables)
	assert.Len(t, tables.Rows, 2)
	assert.NotNil(t, tableByHeader(r, "Instance"))
	assert.Subset(t, chartTitles(r), []string{"Queries, per second", "Max active parts in a partition", "Read-only replicated tables",
		"Queries by total time <selector>, query seconds/second", "Delayed and rejected INSERTs <selector>, per second"})
}

func esCluster(m testMetrics, addr, cluster, color string, distribution string) {
	cl := map[string]string{"cluster": cluster}
	node := map[string]string{"cluster": cluster, "host": "10.0.3.1", "name": "es-0"}
	with := func(base map[string]string, k, v string) map[string]string {
		res := map[string]string{k: v}
		for kk, vv := range base {
			res[kk] = vv
		}
		return res
	}
	m.target(qESUp, addr, nil, constant(1))
	m.target(qESVersionInfo, addr, map[string]string{"cluster": cluster, "version": "8.15.0", "distribution": distribution}, constant(1))
	m.target(qESHealthStatus, addr, with(cl, "color", color), constant(1))
	m.target(qESHealthNodes, addr, cl, constant(3))
	m.target(qESHealthDataNodes, addr, cl, constant(3))
	m.target(qESHealthActiveShards, addr, cl, constant(20))
	m.target(qESHealthActivePrimary, addr, cl, constant(10))
	m.target(qESHealthUnassigned, addr, cl, constant(0))
	m.target(qESHealthDelayedUnassigned, addr, cl, constant(0))
	m.target(qESHeapUsed, addr, node, constant(4e9))
	m.target(qESHeapMax, addr, node, constant(8e9))
	m.target(qESFsSize, addr, with(node, "path", "/usr/share/elasticsearch/data"), constant(100e9))
	m.target(qESFsAvailable, addr, with(node, "path", "/usr/share/elasticsearch/data"), constant(50e9))
	m.target(qESThreadPoolRejects, addr, with(node, "type", "write"), constant(0))
	m.target(qESSearchQueryRate, addr, node, constant(10))
	m.target(qESSearchQueryTime, addr, node, constant(0.2))
	m.target(qESIndexHealth, addr, map[string]string{"cluster": cluster, "index": "logs-1", "color": color}, constant(1))
	m.target(qESIndexDocsPrimary, addr, map[string]string{"cluster": cluster, "index": "logs-1"}, constant(1e6))
	m.target(qESIndexSizeTotal, addr, map[string]string{"cluster": cluster, "index": "logs-1"}, constant(2e9))
	m.target(qESIndexShardsPrimary, addr, map[string]string{"cluster": cluster, "index": "logs-1"}, constant(1))
	m.target(qESIndexReplicas, addr, map[string]string{"cluster": cluster, "index": "logs-1"}, constant(1))
}

func TestElasticsearchTargets(t *testing.T) {
	const (
		healthy = "10.0.3.1:9200"
		yellow  = "10.0.3.2:9200"
		red     = "10.0.3.3:9200"
	)
	m := testMetrics{}
	esCluster(m, healthy, "green-cluster", "green", "elasticsearch")

	esCluster(m, yellow, "yellow-cluster", "yellow", "elasticsearch")
	node := map[string]string{"cluster": "yellow-cluster", "host": "10.0.3.1", "name": "es-0"}
	replace := m.replace
	replace(qESHeapUsed, yellow, node, constant(7.2e9)) // 90%
	fs := map[string]string{"cluster": "yellow-cluster", "host": "10.0.3.1", "name": "es-0", "path": "/data"}
	replace(qESFsSize, yellow, fs, constant(100e9))
	replace(qESFsAvailable, yellow, fs, constant(10e9)) // 90% used
	replace(qESThreadPoolRejects, yellow, map[string]string{"cluster": "yellow-cluster", "host": "10.0.3.1", "name": "es-0", "type": "write"}, lastN(2, 0, 1))
	replace(qESHealthUnassigned, yellow, map[string]string{"cluster": "yellow-cluster"}, constant(3))

	esCluster(m, red, "red-cluster", "red", "opensearch")
	fs = map[string]string{"cluster": "red-cluster", "host": "10.0.3.1", "name": "es-0", "path": "/data"}
	replace(qESFsSize, red, fs, constant(100e9))
	replace(qESFsAvailable, red, fs, constant(3e9)) // 97% used: past the flood stage
	replace(qESHealthUnassigned, red, map[string]string{"cluster": "red-cluster"}, constant(5))
	replace(qESHealthDelayedUnassigned, red, map[string]string{"cluster": "red-cluster"}, constant(5)) // all delayed

	cw := newCTWorld(t, m, nil)

	es := cw.app(healthy).Instances[0].Elasticsearch
	require.NotNil(t, es)
	assert.Equal(t, "green", es.Health.Status.Value())
	assert.Equal(t, "green-cluster", es.Cluster.Value())
	require.Len(t, es.Nodes, 1)
	n := es.Nodes[model.ElasticsearchNodeKey{Cluster: "green-cluster", Name: "es-0"}]
	require.NotNil(t, n)
	assert.Equal(t, float32(50), n.HeapUsedPercent().Last())
	assert.InDelta(t, 0.02, n.SearchLatency().Last(), 1e-6)
	assert.True(t, cw.app(healthy).ApplicationTypes()[model.ApplicationTypeElasticsearch])
	assert.True(t, cw.app(red).ApplicationTypes()[model.ApplicationTypeOpensearch])

	for _, id := range []model.CheckId{model.Checks.ElasticsearchAvailability.Id, model.Checks.ElasticsearchClusterHealth.Id,
		model.Checks.ElasticsearchUnassignedShards.Id, model.Checks.ElasticsearchJvmHeap.Id, model.Checks.ElasticsearchDiskSpace.Id,
		model.Checks.ElasticsearchThreadPoolRejects.Id} {
		assert.Equal(t, model.OK, cw.check(healthy, id).Status, id)
	}
	assert.Equal(t, model.OK, cw.app(healthy).Status)

	c := cw.check(yellow, model.Checks.ElasticsearchClusterHealth.Id)
	assert.Equal(t, model.WARNING, c.Status)
	assert.Equal(t, "1 cluster is not green", c.Message)
	assert.Equal(t, model.WARNING, cw.check(yellow, model.Checks.ElasticsearchUnassignedShards.Id).Status)
	c = cw.check(yellow, model.Checks.ElasticsearchJvmHeap.Id)
	assert.Equal(t, model.WARNING, c.Status)
	assert.Equal(t, "heap usage of 1 node over 85%", c.Message)
	c = cw.check(yellow, model.Checks.ElasticsearchDiskSpace.Id)
	assert.Equal(t, model.WARNING, c.Status)
	assert.Equal(t, "1 data path is over 85% full", c.Message)
	c = cw.check(yellow, model.Checks.ElasticsearchThreadPoolRejects.Id)
	assert.Equal(t, model.WARNING, c.Status)
	assert.Equal(t, "60 tasks rejected by thread pools in the last 5 minutes", c.Message)
	assert.Equal(t, model.WARNING, cw.app(yellow).Status)

	assert.Equal(t, model.CRITICAL, cw.check(red, model.Checks.ElasticsearchClusterHealth.Id).Status)
	assert.Equal(t, model.CRITICAL, cw.check(red, model.Checks.ElasticsearchDiskSpace.Id).Status)
	assert.Equal(t, model.OK, cw.check(red, model.Checks.ElasticsearchUnassignedShards.Id).Status) // delayed allocation
	assert.Equal(t, model.CRITICAL, cw.app(red).Status)

	r := cw.detailed(yellow, model.AuditReportElasticsearch)
	clusters := tableByHeader(r, "Cluster")
	require.NotNil(t, clusters)
	require.Len(t, clusters.Rows, 1)
	assert.Equal(t, "yellow", clusters.Rows[0].Cells[1].Value)
	nodes := tableByHeader(r, "Node")
	require.NotNil(t, nodes)
	require.Len(t, nodes.Rows, 1)
	indices := tableByHeader(r, "Index")
	require.NotNil(t, indices)
	require.Len(t, indices.Rows, 1)
	assert.Equal(t, "1 primary × 2", indices.Rows[0].Cells[5].Value)
	assert.Subset(t, chartTitles(r), []string{"JVM heap usage, %", "Data disk usage, %", "Shards <selector>",
		"Thread pool rejections <selector>, per second", "Search latency (query phase), seconds"})
}

func TestClusterTargetAlertingRules(t *testing.T) {
	byCheck := map[model.CheckId]model.AlertingRule{}
	for _, r := range model.BuiltinAlertingRules() {
		if r.Source.Check != nil {
			byCheck[r.Source.Check.CheckId] = r
		}
	}
	for _, id := range []model.CheckId{
		model.Checks.KafkaAvailability.Id, model.Checks.KafkaOfflinePartitions.Id, model.Checks.KafkaUnderReplicatedPartitions.Id,
		model.Checks.KafkaConsumerLag.Id, model.Checks.ClickHouseAvailability.Id, model.Checks.ClickHouseReplication.Id,
		model.Checks.ClickHouseTooManyParts.Id, model.Checks.ClickHouseStuckMutations.Id, model.Checks.ClickHouseRejectedInserts.Id,
		model.Checks.ElasticsearchAvailability.Id, model.Checks.ElasticsearchClusterHealth.Id, model.Checks.ElasticsearchUnassignedShards.Id,
		model.Checks.ElasticsearchJvmHeap.Id, model.Checks.ElasticsearchDiskSpace.Id, model.Checks.ElasticsearchThreadPoolRejects.Id,
	} {
		require.NotEmpty(t, id)
		_, ok := byCheck[id]
		assert.True(t, ok, "no builtin rule for %s", id)
		assert.NotNil(t, model.GetCheckConfigs()[id])
	}
	assert.Equal(t, timeseries.Duration(300), timeseries.Duration(model.Checks.KafkaConsumerLag.DefaultThreshold))
}

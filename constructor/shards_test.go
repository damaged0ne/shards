package constructor

import (
	"testing"

	"github.com/coroot/coroot/auditor"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testFrom = timeseries.Time(1_700_000_000)
	testStep = timeseries.Duration(30)
)

type testMetrics map[string][]*model.MetricValues

func (tm testMetrics) add(query, containerId string, labels map[string]string, values ...float32) {
	ls := model.Labels{}
	for k, v := range labels {
		ls[k] = v
	}
	mv := &model.MetricValues{Labels: ls, Values: timeseries.NewWithData(testFrom, testStep, values)}
	mv.MachineID = "m1"
	mv.ContainerId = containerId
	mv.LabelsHash = ls.Hash()
	tm[query] = append(tm[query], mv)
}

func TestShardsNodeAgentMetrics(t *testing.T) {
	m := testMetrics{}
	m.add("node_info", "", map[string]string{"hostname": "h1"}, 1, 1, 1, 1)
	m.add("node_agent_info", "", map[string]string{"version": "1.2.3-shards"}, 1, 1, 1, 1)
	m.add("node_cpu_usage_percent", "", nil, 10, 10, 10, 10)
	m.add("node_cpu_cores", "", nil, 4, 4, 4, 4)

	root := map[string]string{"mount": "/", "device": "/dev/sda1", "fs": "ext4"}
	data := map[string]string{"mount": "/data", "device": "/dev/sdb1", "fs": "xfs"}
	m.add(qShardsFsSize, "", root, 100, 100, 100, 100)
	m.add(qShardsFsAvail, "", root, 50, 20, 8, 5)
	m.add(qShardsFsFiles, "", root, 1000, 1000, 1000, 1000)
	m.add(qShardsFsFilesFree, "", root, 900, 900, 900, 900)
	m.add(qShardsFsReadonly, "", root, 0, 0, 0, 0)
	m.add(qShardsFsSize, "", data, 100, 100, 100, 100)
	m.add(qShardsFsAvail, "", data, 90, 90, 90, 90)
	m.add(qShardsFsFiles, "", data, 1000, 1000, 1000, 1000)
	m.add(qShardsFsFilesFree, "", data, 50, 50, 50, 50) // 95% inodes used
	m.add(qShardsFsReadonly, "", data, 0, 0, 1, 1)      // remounted read-only

	m.add(qShardsLoad1, "", nil, 1, 2, 3, 4)
	m.add(qShardsLoad5, "", nil, 1, 1, 2, 2)
	m.add(qShardsLoad15, "", nil, 1, 1, 1, 1)
	m.add(qShardsNodeCpuPressure, "", map[string]string{"kind": "some"}, 0.1, 0.1, 0.2, 0.2)
	m.add(qShardsNodeIOPressure, "", map[string]string{"kind": "full"}, 0.01, 0.01, 0.01, 0.01)

	m.add(qShardsF2bUp, "", nil, 1, 1, 1, 1)
	m.add(qShardsF2bBanned, "", map[string]string{"jail": "sshd"}, 3, 4, 5, 6)
	m.add(qShardsF2bBans1h, "", map[string]string{"jail": "sshd"}, 10, 10, 11, 12)
	m.add(qShardsNftCounterBytes, "", map[string]string{"family": "inet", "table": "filter", "counter": "http"}, 100, 100, 100, 100)
	m.add(qShardsNftRulePackets, "", map[string]string{"family": "inet", "table": "filter", "chain": "input", "comment": "ssh"}, 1, 1, 1, 1)

	m.add(qShardsAgentEbpfLostSamples, "", nil, 0, 1, 0, 1)
	m.add(qShardsAgentRemoteWriteFailures, "", nil, 0, 0, 0, 0)
	m.add(qShardsAgentEventsQueueLength, "", nil, 5, 10, 20000, 3)

	// Compose service with two replicas, one of them unhealthy
	for _, id := range []string{"/swarm/shop/api/1", "/swarm/shop/api/2"} {
		m.add("container_memory_rss", id, nil, 1e6, 1e6, 1e6, 1e6)
		m.add(qShardsContainerState, id, map[string]string{"state": "running"}, 1, 1, 1, 1)
		m.add(qShardsComposeInfo, id, map[string]string{"project": "shop", "service": "api"}, 1, 1, 1, 1)
		m.add(qShardsContainerImageInfo, id, map[string]string{"image": "registry/shop/api:1.4.2", "image_id": "sha256:aaaaaaaaaaaaaaaa", "version": "1.4.2", "revision": "0123456789abcdef"}, 1, 1, 1, 1)
		m.add(qShardsContainerRestartPolicy, id, map[string]string{"policy": "unless-stopped"}, 1, 1, 1, 1)
		m.add(qShardsContainerStartedAge, id, nil, 100, 130, 160, 190)
	}
	m.add(qShardsContainerHealth, "/swarm/shop/api/1", map[string]string{"status": "healthy"}, 1, 1, 1, 1)
	m.add(qShardsContainerHealth, "/swarm/shop/api/2", map[string]string{"status": "healthy"}, 1, 1, timeseries.NaN, timeseries.NaN)
	m.add(qShardsContainerHealth, "/swarm/shop/api/2", map[string]string{"status": "unhealthy"}, timeseries.NaN, timeseries.NaN, 1, 1)
	m.add(qShardsContainerDockerRestart, "/swarm/shop/api/1", nil, 0, 0, 0, 0)
	m.add(qShardsContainerDockerRestart, "/swarm/shop/api/2", nil, 1, 2, 4, 5)

	// a worker killed by the OOM killer
	w1 := "/swarm/shop/worker/1"
	m.add(qShardsContainerState, w1, map[string]string{"state": "exited"}, 1, 1, 1, 1)
	m.add(qShardsContainerExitCode, w1, nil, 137, 137, 137, 137)
	m.add(qShardsContainerOOMKilled, w1, nil, 1, 1, 1, 1)
	m.add(qShardsContainerRestartPolicy, w1, map[string]string{"policy": "no"}, 1, 1, 1, 1)
	m.add(qShardsComposeInfo, w1, map[string]string{"project": "shop", "service": "worker"}, 1, 1, 1, 1)

	// a completed one-off `docker compose run migrate`
	r1 := "/swarm/shop/migrate-run/5f3a"
	m.add(qShardsContainerState, r1, map[string]string{"state": "exited"}, 1, 1, 1, 1)
	m.add(qShardsContainerExitCode, r1, nil, 3, 3, 3, 3)
	m.add(qShardsContainerOOMKilled, r1, nil, 0, 0, 0, 0)
	m.add(qShardsComposeInfo, r1, map[string]string{"project": "shop", "service": "migrate"}, 1, 1, 1, 1)

	project := &db.Project{Id: "p1"}
	c := New(nil, project, nil, nil)
	w := model.NewWorld(testFrom, testFrom.Add(4*testStep), testStep, testStep)
	nodes := nodeCache{}
	c.loadNodes(w, m, nodes, project)
	c.loadContainers(w, m, promJobStatuses{}, nodes, containerCache{}, map[string]*model.Service{}, map[string]*utils.StringSet{}, project)

	require.Len(t, w.Nodes, 1)
	node := w.Nodes[0]
	s := node.Shards
	require.NotNil(t, s)
	require.Len(t, s.Filesystems, 2)
	assert.Equal(t, "ext4", s.Filesystems["/"].FsType)
	assert.InDelta(t, 95, s.Filesystems["/"].UsedPercent().Last(), 0.01)
	assert.InDelta(t, 95, s.Filesystems["/data"].InodesUsedPercent().Last(), 0.01)
	assert.True(t, s.Filesystems["/data"].BecameReadonly())
	assert.False(t, s.Filesystems["/"].BecameReadonly())
	assert.Equal(t, float32(4), s.Load1.Last())
	assert.Equal(t, float32(0.2), s.Pressure["cpu/some"].Last())
	assert.NotNil(t, s.Pressure["io/full"])
	assert.Equal(t, float32(6), s.F2bJails["sshd"].Banned.Last())
	assert.Len(t, s.NftCounters, 1)
	assert.Len(t, s.NftRules, 1)
	assert.Equal(t, "1.2.3-shards", node.AgentVersion.Value())

	// Compose containers: one application per service, the project is the namespace
	api := w.GetApplication(model.NewApplicationId("p1", "shop", model.ApplicationKindDockerSwarmService, "api"))
	require.NotNil(t, api)
	require.Len(t, api.Instances, 2)
	assert.Equal(t, "api.1", api.Instances[0].Name)
	_, d := api.Instances[1].DockerContainerOf()
	require.NotNil(t, d)
	assert.Equal(t, model.DockerHealthUnhealthy, d.Health.Value())
	assert.Equal(t, "1.4.2", d.Version.Value())
	assert.Equal(t, float32(4), d.RestartsIncrease())
	assert.Equal(t, "registry/shop/api:1.4.2", api.Instances[1].Containers["api"].Image)

	worker := w.GetApplication(model.NewApplicationId("p1", "shop", model.ApplicationKindDockerSwarmService, "worker"))
	require.NotNil(t, worker)
	_, wd := worker.Instances[0].DockerContainerOf()
	failed, reason := wd.Failed()
	assert.True(t, failed)
	assert.Equal(t, "OOM killed", reason)

	migrate := w.GetApplication(model.NewApplicationId("p1", "shop", model.ApplicationKindDockerSwarmService, "migrate-run"))
	require.NotNil(t, migrate)
	assert.True(t, migrate.PeriodicJob())

	// the checks
	auditor.Audit(w, project, api, nil)
	checks := map[model.CheckId]*model.Check{}
	for _, app := range []*model.Application{api, worker, migrate} {
		for _, r := range app.Reports {
			for _, ch := range r.Checks {
				checks[model.CheckId(string(app.Id.Name)+"/"+string(ch.Id))] = ch
			}
		}
	}
	get := func(app string, id model.CheckId) *model.Check {
		ch := checks[model.CheckId(app+"/"+string(id))]
		require.NotNil(t, ch, "%s/%s", app, id)
		return ch
	}
	assert.Equal(t, model.WARNING, get("api", model.Checks.DockerContainerHealth.Id).Status)
	assert.Equal(t, "1 container is unhealthy", get("api", model.Checks.DockerContainerHealth.Id).Message)
	assert.Equal(t, []string{"api.2: unhealthy"}, get("api", model.Checks.DockerContainerHealth.Id).Details.Items())
	assert.Equal(t, model.WARNING, get("api", model.Checks.DockerContainerRestarts.Id).Status)
	assert.Equal(t, model.OK, get("api", model.Checks.DockerContainerState.Id).Status)
	assert.Equal(t, model.WARNING, get("api", model.Checks.InstanceAvailability.Id).Status) // 1 of 2 available < 75%
	assert.Equal(t, model.WARNING, get("api", model.Checks.NodeDiskSpace.Id).Status)
	assert.Equal(t, "2 node filesystems are over 90% full, max usage: 95%", get("api", model.Checks.NodeDiskSpace.Id).Message)
	assert.Equal(t, model.WARNING, get("api", model.Checks.NodeFilesystemReadonly.Id).Status)

	assert.Equal(t, model.WARNING, get("worker", model.Checks.DockerContainerState.Id).Status)
	assert.Equal(t, model.WARNING, get("worker", model.Checks.InstanceAvailability.Id).Status)

	assert.Equal(t, model.OK, get("migrate-run", model.Checks.DockerContainerState.Id).Status)
	assert.Equal(t, model.OK, get("migrate-run", model.Checks.InstanceAvailability.Id).Status)

	// the node report
	nr := auditor.AuditNode(w, node)
	assert.Equal(t, model.WARNING, nr.Status)
	var tables, charts []string
	for _, wg := range nr.Widgets {
		if wg.Table != nil {
			tables = append(tables, wg.Table.Header[0])
		}
		if wg.Chart != nil {
			charts = append(charts, wg.Chart.Title)
		}
		if wg.ChartGroup != nil {
			charts = append(charts, wg.ChartGroup.Title)
		}
	}
	assert.Subset(t, tables, []string{"Mount point", "fail2ban jail", "nftables counter / rule", "Agent"})
	assert.Subset(t, charts, []string{"Load average", "Resource pressure (PSI) <selector>, % of time stalled", "Filesystem usage <selector>, %",
		"fail2ban: banned IPs", "nftables traffic, bits/second", "Agent data loss, events/second", "Agent events queue length"})
}

func TestHumanizeScrapeErrors(t *testing.T) {
	m := testMetrics{}
	m.add("node_info", "", map[string]string{"hostname": "h1"}, 1, 1)
	m.add("pg_scrape_error", "", map[string]string{"error": "auth", "warning": "", "instance": "10.0.0.1:5432"}, 1, 1)
	pg := &model.Instance{}
	postgres(pg, "pg_scrape_error", m["pg_scrape_error"][0], promJobStatuses{})
	assert.Equal(t, "authentication failed", pg.Postgres.Error.Value())
	assert.Equal(t, "replSetGetStatus: server unreachable", model.HumanizeScrapeError("replSetGetStatus: unreachable"))
	assert.Equal(t, "dial tcp 10.0.0.1:5432: connect: connection refused", model.HumanizeScrapeError("dial tcp 10.0.0.1:5432: connect: connection refused"))
}

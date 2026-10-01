package constructor

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A synthetic fleet reproducing the "parsers" host of the service map report (upstream node-agent data only: no
// shards_compose_info, no hostname override) plus a gateway host and an admin host with published ports of their own.

type fleet struct {
	m testMetrics
}

func (f *fleet) series(query, machine, cid string, labels map[string]string, dest, actual string, v float32) {
	ls := model.Labels{}
	for k, val := range labels {
		ls[k] = val
	}
	if dest != "" {
		ls["destination"] = dest
	}
	if actual != "" {
		ls["actual_destination"] = actual
	}
	mv := &model.MetricValues{Labels: ls, Values: timeseries.NewWithData(testFrom, testStep, []float32{v, v, v, v})}
	mv.MachineID = machine
	mv.ContainerId = cid
	mv.Destination = dest
	mv.ActualDestination = actual
	mv.LabelsHash = ls.Hash()
	f.m[query] = append(f.m[query], mv)
}

func (f *fleet) node(machine, hostname string, ifaces map[string]string) {
	f.series("node_info", machine, "", map[string]string{"hostname": hostname}, "", "", 1)
	f.series("node_cpu_cores", machine, "", nil, "", "", 4)
	for iface, ip := range ifaces {
		f.series("node_net_ip", machine, "", map[string]string{"interface": iface, "ip": ip}, "", "", 1)
	}
}

func (f *fleet) container(machine, cid, image string) {
	ls := map[string]string{}
	if image != "" {
		ls["image"] = image
	}
	f.series("container_info", machine, cid, ls, "", "", 1)
	f.series("container_cpu_usage", machine, cid, nil, "", "", 0.1)
}

func (f *fleet) listen(machine, cid, addr, proxy string) {
	f.series("container_net_tcp_listen_info", machine, cid, map[string]string{"listen_addr": addr, "proxy": proxy}, "", "", 1)
}

func (f *fleet) connect(machine, cid, dest, actual string, rate float32) {
	f.series("container_net_tcp_successful_connects", machine, cid, nil, dest, actual, rate)
	f.series("container_net_tcp_active_connections", machine, cid, nil, dest, actual, 1)
}

func (f *fleet) failed(machine, cid, dest string, rate float32) {
	f.series("container_net_tcp_failed_connects", machine, cid, nil, dest, "", rate)
}

func (f *fleet) l7(machine, cid, proto, dest, actual string, rps, latency float32) {
	switch proto {
	case "http":
		f.series("container_http_requests_count", machine, cid, map[string]string{"status": "200"}, dest, actual, rps)
		f.series("container_http_requests_latency_total", machine, cid, nil, dest, actual, latency)
	case "postgres":
		f.series("container_postgres_queries_count", machine, cid, map[string]string{"status": "ok"}, dest, actual, rps)
		f.series("container_postgres_queries_latency_total", machine, cid, nil, dest, actual, latency)
	}
}

const (
	mParsers = "m-parsers"
	mGw      = "m-gw"
	mAdmin   = "m-admin"
)

// publish emulates a published port: docker-proxy's raw listens (including loopback) and the container's proxied
// listens on the host IPs the agent expanded 0.0.0.0 to (loopback NOT included, as the agent's host-IP list
// doesn't always contain it -- the case where upstream falls back to docker-proxy).
func (f *fleet) publish(machine, cid string, hostIPs []string, port string, loopback bool) {
	for _, ip := range hostIPs {
		addr := ip + ":" + port
		f.listen(machine, "/system.slice/docker.service", addr, "")
		f.listen(machine, cid, addr, "dockerd")
	}
	if loopback {
		f.listen(machine, "/system.slice/docker.service", "127.0.0.1:"+port, "")
		f.listen(machine, "/system.slice/docker.service", "[::1]:"+port, "")
	}
}

func parsersFleet() *fleet {
	f := &fleet{m: testMetrics{}}
	parsersIPs := []string{"192.168.0.4", "172.17.0.1", "172.18.0.1", "172.19.0.1", "172.20.0.1"}
	f.node(mParsers, "minust-parser", map[string]string{"eth0": "192.168.0.4", "docker0": "172.17.0.1", "br-minust": "172.18.0.1", "br-pravoved": "172.19.0.1", "br-shards": "172.20.0.1", "lo": "127.0.0.1"})
	f.node(mGw, "gateway", map[string]string{"eth0": "192.168.0.10", "docker0": "172.17.0.1", "br-gw": "172.18.0.1"})
	f.node(mAdmin, "admin", map[string]string{"eth0": "192.168.0.20", "docker0": "172.17.0.1"})

	d := func(name string) string { return "/docker/" + name }

	// --- parsers: 29 applications
	for _, name := range []string{"minust_parser", "minust_scheduler", "minust_outbox_publisher", "minust_postgres",
		"pravoved_parser", "pravoved_scraper", "pravoved_scheduler", "pravoved_outbox", "pravoved_postgres",
		"shards-coroot", "shards-clickhouse", "shards-prometheus", "shards-grafana", "shards-node-agent", "shards-cluster-agent",
		"heimdall-alloy", "heimdall-db-collector", "heimdall-node-exporter", "heimdall-docker-exporter", "heimdall-docker-socket-proxy"} {
		f.container(mParsers, d(name), "registry/"+name+":1")
	}
	for _, unit := range []string{"docker", "containerd", "ssh", "chrony", "fail2ban", "cron", "atd", "rsyslog", "watchdog"} {
		f.container(mParsers, "/system.slice/"+unit+".service", "")
	}
	f.listen(mParsers, d("minust_parser"), "172.18.0.2:8000", "")
	f.listen(mParsers, d("minust_postgres"), "172.18.0.5:5432", "")
	f.listen(mParsers, d("pravoved_parser"), "172.19.0.2:8000", "")
	f.listen(mParsers, d("pravoved_postgres"), "172.19.0.5:5432", "")
	f.listen(mParsers, d("shards-coroot"), "172.20.0.2:8080", "")
	f.listen(mParsers, d("shards-clickhouse"), "172.20.0.3:9000", "")
	f.listen(mParsers, d("shards-prometheus"), "172.20.0.4:9090", "")
	f.listen(mParsers, d("shards-grafana"), "172.20.0.5:3000", "")
	f.listen(mParsers, d("heimdall-db-collector"), "192.168.0.4:18090", "") // host network
	f.listen(mParsers, d("heimdall-node-exporter"), "192.168.0.4:9100", "")
	f.listen(mParsers, d("heimdall-docker-exporter"), "172.17.0.4:9417", "")
	f.listen(mParsers, "/system.slice/ssh.service", "192.168.0.4:22", "")

	f.publish(mParsers, d("minust_parser"), parsersIPs, "80", true)   // 0.0.0.0:80->8000
	f.publish(mParsers, d("pravoved_parser"), parsersIPs, "81", true) // 0.0.0.0:81->8000
	f.publish(mParsers, d("shards-coroot"), []string{"192.168.0.4"}, "8080", false)
	f.publish(mParsers, d("minust_postgres"), []string{"127.0.0.1"}, "5437", false)
	f.publish(mParsers, d("pravoved_postgres"), []string{"127.0.0.1"}, "5438", false)

	f.connect(mParsers, d("minust_scheduler"), "172.18.0.5:5432", "172.18.0.5:5432", 1)
	f.connect(mParsers, d("minust_outbox_publisher"), "172.18.0.5:5432", "172.18.0.5:5432", 1)
	f.connect(mParsers, d("minust_parser"), "172.18.0.5:5432", "172.18.0.5:5432", 2)
	f.connect(mParsers, d("minust_parser"), "104.18.26.90:443", "104.18.26.90:443", 0.5)
	f.series("ip_to_fqdn", mParsers, "", map[string]string{"ip": "104.18.26.90", "fqdn": "api.deepseek.com"}, "", "", 1)
	f.connect(mParsers, d("pravoved_scraper"), "185.65.148.10:443", "185.65.148.10:443", 3)
	f.series("ip_to_fqdn", mParsers, "", map[string]string{"ip": "185.65.148.10", "fqdn": "pravoved.ru"}, "", "", 1)
	for _, c := range []string{"pravoved_scraper", "pravoved_scheduler", "pravoved_outbox", "pravoved_parser"} {
		f.connect(mParsers, d(c), "172.19.0.5:5432", "172.19.0.5:5432", 1)
	}
	f.connect(mParsers, d("pravoved_outbox"), "10.10.0.7:9092", "10.10.0.7:9092", 1) // redpanda on a host without an agent
	f.failed(mParsers, d("pravoved_outbox"), "10.10.0.8:9092", 0.2)                  // a broker that is down

	// host-network collector -> published loopback ports (docker-proxy carries the traffic)
	f.connect(mParsers, d("heimdall-db-collector"), "127.0.0.1:5437", "127.0.0.1:5437", 0.1)
	f.connect(mParsers, d("heimdall-db-collector"), "127.0.0.1:5438", "127.0.0.1:5438", 0.1)
	// bridge container -> bridge gateway (172.17.0.1 exists on every host of the fleet)
	f.connect(mParsers, d("heimdall-alloy"), "172.17.0.1:80", "172.17.0.1:80", 0.5)
	f.connect(mParsers, d("heimdall-alloy"), "192.168.0.4:9100", "192.168.0.4:9100", 0.1)
	f.connect(mParsers, d("heimdall-alloy"), "172.17.0.4:9417", "172.17.0.4:9417", 0.1)
	// the monitoring plane
	f.connect(mParsers, d("shards-coroot"), "172.20.0.3:9000", "172.20.0.3:9000", 1)
	f.connect(mParsers, d("shards-coroot"), "172.20.0.4:9090", "172.20.0.4:9090", 1)
	f.connect(mParsers, d("shards-grafana"), "172.20.0.3:9000", "172.20.0.3:9000", 1)
	f.connect(mParsers, d("shards-prometheus"), "192.168.0.20:9009", "192.168.0.20:9009", 0.2) // remote write to admin's mimir
	f.connect(mParsers, d("shards-node-agent"), "192.168.0.4:8080", "172.20.0.2:8080", 0.1)    // local: DNATed by the kernel
	f.connect(mParsers, d("shards-cluster-agent"), "192.168.0.4:8080", "192.168.0.4:8080", 0.1)

	// docker-proxy's backend legs, with the L7 stats the clients don't have
	f.connect(mParsers, "/system.slice/docker.service", "172.18.0.5:5432", "172.18.0.5:5432", 0.1)
	f.l7(mParsers, "/system.slice/docker.service", "postgres", "172.18.0.5:5432", "172.18.0.5:5432", 40, 0.4)
	f.connect(mParsers, "/system.slice/docker.service", "172.20.0.2:8080", "172.20.0.2:8080", 0.3)
	f.l7(mParsers, "/system.slice/docker.service", "http", "172.20.0.2:8080", "172.20.0.2:8080", 9, 0.9)
	f.connect(mParsers, "/system.slice/docker.service", "172.18.0.2:8000", "172.18.0.2:8000", 5)
	f.l7(mParsers, "/system.slice/docker.service", "http", "172.18.0.2:8000", "172.18.0.2:8000", 100, 5)

	// --- gateway host
	for _, name := range []string{"gateway", "gw-cache-warmer", "shards-node-agent"} {
		f.container(mGw, d(name), "registry/"+name+":1")
	}
	f.container(mGw, "/system.slice/docker.service", "")
	f.listen(mGw, d("gateway"), "172.18.0.2:8000", "") // same bridge IP as minust_parser on parsers
	f.publish(mGw, d("gateway"), []string{"192.168.0.10", "172.17.0.1", "172.18.0.1"}, "443", true)
	f.connect(mGw, d("gateway"), "192.168.0.4:80", "192.168.0.4:80", 5)
	f.l7(mGw, d("gateway"), "http", "192.168.0.4:80", "192.168.0.4:80", 50, 2)
	f.connect(mGw, d("gw-cache-warmer"), "172.18.0.2:8000", "172.18.0.2:8000", 1)
	f.connect(mGw, d("shards-node-agent"), "192.168.0.4:8080", "192.168.0.4:8080", 0.1)

	// --- admin host
	for _, name := range []string{"mimir-worker", "mimir-api", "mimir-reporter", "admin-ui", "shards-node-agent"} {
		f.container(mAdmin, d(name), "registry/"+name+":1")
	}
	f.container(mAdmin, "/system.slice/docker.service", "")
	f.listen(mAdmin, d("mimir-api"), "172.17.0.2:9009", "")
	f.publish(mAdmin, d("mimir-api"), []string{"192.168.0.20", "172.17.0.1"}, "9009", true)
	f.listen(mAdmin, d("admin-ui"), "172.17.0.3:80", "")
	f.publish(mAdmin, d("admin-ui"), []string{"192.168.0.20", "172.17.0.1"}, "80", true) // 172.17.0.1:80 also on parsers
	f.connect(mAdmin, d("mimir-worker"), "172.17.0.1:80", "172.17.0.1:80", 1)            // -> admin-ui, not minust_parser
	f.connect(mAdmin, d("mimir-worker"), "192.168.0.4:80", "192.168.0.4:80", 1)
	f.connect(mAdmin, d("mimir-worker"), "[::ffff:192.168.0.4]:81", "[::ffff:192.168.0.4]:81", 1) // dual-stack socket
	f.connect(mAdmin, d("mimir-reporter"), "127.0.0.1:9009", "127.0.0.1:9009", 0.1)               // via docker-proxy
	f.connect(mAdmin, d("shards-node-agent"), "192.168.0.4:8080", "192.168.0.4:8080", 0.1)
	return f
}

func loadFleet(t *testing.T, f *fleet, project *db.Project) *model.World {
	c := New(nil, project, nil, nil)
	w := model.NewWorld(testFrom, testFrom.Add(4*testStep), testStep, testStep)
	nodes := nodeCache{}
	ip2fqdn, fqdn2ip := map[string]*utils.StringSet{}, map[string]*utils.StringSet{}
	c.loadNodes(w, f.m, nodes, project)
	loadFQDNs(f.m, ip2fqdn, fqdn2ip)
	c.loadContainers(w, f.m, promJobStatuses{}, nodes, containerCache{}, map[string]*model.Service{}, ip2fqdn, project)
	c.calcApplicationCategories(w, project)
	require.NotEmpty(t, w.Applications)
	return w
}

// appEdges returns "client -> server" for every instance-level connection (what the recording rules aggregate).
func appEdges(w *model.World) map[string]bool {
	res := map[string]bool{}
	for _, app := range w.Applications {
		for _, i := range app.Instances {
			for _, u := range i.Upstreams {
				if r := u.RemoteApplication(); r != nil && r != app {
					res[app.Id.Name+" -> "+r.Id.Name] = true
				}
			}
		}
	}
	return res
}

func edgeConn(w *model.World, client, server string) *model.Connection {
	for _, app := range w.Applications {
		if app.Id.Name != client {
			continue
		}
		for _, i := range app.Instances {
			for _, u := range i.Upstreams {
				if r := u.RemoteApplication(); r != nil && r.Id.Name == server {
					return u
				}
			}
		}
	}
	return nil
}

func TestDockerProxyResolution(t *testing.T) {
	project := &db.Project{Id: "p1"}
	w := loadFleet(t, parsersFleet(), project)
	edges := appEdges(w)

	// acceptance 1: no edge terminates at docker.service/containerd, on any host
	for e := range edges {
		assert.False(t, strings.HasSuffix(e, "-> docker") || strings.HasSuffix(e, "-> containerd"), e)
		assert.False(t, strings.HasPrefix(e, "docker ->"), e)
	}

	// acceptance 2 (+5: works on the gateway and admin hosts too)
	for _, e := range []string{
		"shards-node-agent -> shards-coroot", // from all three hosts (one application)
		"shards-cluster-agent -> shards-coroot",
		"gateway -> minust_parser",
		"mimir-worker -> minust_parser",
		"mimir-worker -> pravoved_parser", // IPv4-mapped destination
		"heimdall-db-collector -> minust_postgres",
		"heimdall-db-collector -> pravoved_postgres",
		"heimdall-alloy -> minust_parser",          // bridge gateway shared by every host
		"shards-prometheus -> mimir-api",           // published port on another host
		"mimir-reporter -> mimir-api",              // loopback form the agent didn't expand
		"gw-cache-warmer -> gateway",               // bridge IP colliding with minust_parser's
		"minust_scheduler -> minust_postgres",      // plain container-to-container
		"pravoved_outbox -> pravoved_postgres",     //
		"heimdall-alloy -> heimdall-node-exporter", // host network
		"mimir-worker -> admin-ui",                 // the same bridge gateway on the admin host
	} {
		assert.True(t, edges[e], "missing edge %s", e)
	}
	assert.False(t, edges["gw-cache-warmer -> minust_parser"])

	// every fleet agent lands on shards-coroot
	agents := 0
	for _, app := range w.Applications {
		if app.Id.Name != "shards-node-agent" {
			continue
		}
		for _, i := range app.Instances {
			for _, u := range i.Upstreams {
				require.NotNil(t, u.RemoteApplication())
				assert.Equal(t, "shards-coroot", u.RemoteApplication().Id.Name, i.Name)
				agents++
			}
		}
	}
	assert.Equal(t, 3, agents)

	// externals keep their FQDN
	assert.True(t, edges["minust_parser -> api.deepseek.com:443"], fmt.Sprint(sortedKeys(edges)))
	assert.True(t, edges["pravoved_scraper -> pravoved.ru:443"])

	// L7 stats of docker-proxy's backend leg are carried over where the client has none ...
	c := edgeConn(w, "heimdall-db-collector", "minust_postgres")
	require.NotNil(t, c)
	require.NotNil(t, c.RequestsCount[model.ProtocolPostgres])
	assert.InDelta(t, 40, c.RequestsCount[model.ProtocolPostgres]["ok"].Last(), 0.01)
	assert.InDelta(t, 0.4, c.RequestsLatency[model.ProtocolPostgres].Last(), 0.01)
	// ... split between the clients of a published port in proportion to their connections ...
	var agentRps float32
	for _, client := range []string{"shards-node-agent", "shards-cluster-agent"} {
		for _, app := range w.Applications {
			if app.Id.Name != client {
				continue
			}
			for _, i := range app.Instances {
				for _, u := range i.Upstreams {
					if byStatus := u.RequestsCount[model.ProtocolHttp]; byStatus != nil {
						agentRps += byStatus["200"].Last()
					}
				}
			}
		}
	}
	assert.InDelta(t, 9, agentRps, 0.01)
	// ... and never added on top of the client's own stats
	c = edgeConn(w, "gateway", "minust_parser")
	require.NotNil(t, c)
	assert.InDelta(t, 50, c.RequestsCount[model.ProtocolHttp]["200"].Last(), 0.01)

	// the docker application has no connections at all (backend legs are kept aside)
	for _, app := range w.Applications {
		if app.Id.Name == "docker" {
			for _, i := range app.Instances {
				assert.Empty(t, i.Upstreams)
			}
		}
	}
}

func TestDockerProxyResolutionVsUpstream(t *testing.T) {
	project := &db.Project{Id: "p1"}
	disableDockerProxyResolution = true
	before := appEdges(loadFleet(t, parsersFleet(), project))
	disableDockerProxyResolution = false
	after := appEdges(loadFleet(t, parsersFleet(), project))
	var lost, gained []string
	for e := range before {
		if !after[e] {
			lost = append(lost, e)
		}
	}
	for e := range after {
		if !before[e] {
			gained = append(gained, e)
		}
	}
	sort.Strings(lost)
	sort.Strings(gained)
	t.Logf("upstream-only edges (wrong): %v", lost)
	t.Logf("resolved edges (added): %v", gained)
	assert.NotEmpty(t, gained)
}

func TestFleetCategoriesGroupsAndDisplayNames(t *testing.T) {
	project := &db.Project{Id: "p1"}
	project.Settings.ServiceMap = &db.ServiceMapSettings{NodeDisplayNames: map[string]string{mParsers: "parsers"}}
	w := loadFleet(t, parsersFleet(), project)

	// F4: the node is renamed everywhere, and can still be found by its hostname
	n := w.GetNode("parsers")
	require.NotNil(t, n)
	assert.Equal(t, "minust-parser", n.Shards.Hostname)
	assert.Same(t, n, w.GetNode("minust-parser"))
	for _, app := range w.Applications {
		for _, i := range app.Instances {
			if i.Node == n {
				assert.True(t, strings.HasSuffix(i.Name, "@parsers"), i.Name)
			}
		}
	}

	byName := map[string]*model.Application{}
	var parsersApps []*model.Application
	for _, app := range w.Applications {
		for _, i := range app.Instances {
			if i.Node == n {
				byName[app.Id.Name] = app
				parsersApps = append(parsersApps, app)
				break
			}
		}
	}
	assert.Len(t, parsersApps, 29)

	// F2: systemd infrastructure units are in the hidden "system" category
	for _, unit := range []string{"docker", "containerd", "ssh", "chrony", "fail2ban", "cron", "atd", "rsyslog", "watchdog"} {
		require.NotNil(t, byName[unit], unit)
		assert.Equal(t, model.ApplicationCategorySystem, byName[unit].Category, unit)
		assert.Equal(t, db.ServiceMapCategoryHidden, project.ServiceMapCategoryMode(byName[unit].Category))
	}
	// F3: the monitoring stack and the legacy collectors are "monitoring" (collapsed on the map)
	for name, app := range byName {
		if strings.HasPrefix(name, "shards-") || strings.HasPrefix(name, "heimdall-") {
			assert.Equal(t, model.ApplicationCategoryMonitoring, app.Category, name)
		}
	}
	assert.Equal(t, db.ServiceMapCategoryCollapsed, project.ServiceMapCategoryMode(model.ApplicationCategoryMonitoring))

	// acceptance 3: two product groups (+ the collapsed monitoring plane), business apps are "application"
	groups := project.ServiceMapGroups(parsersApps)
	products := map[string][]string{}
	for _, app := range parsersApps {
		if app.Category != model.ApplicationCategoryApplication {
			continue
		}
		g, ok := groups[app.Id]
		require.True(t, ok, app.Id.Name)
		assert.Equal(t, db.ServiceMapGroupPrefix, g.Source)
		products[g.Name] = append(products[g.Name], app.Id.Name)
	}
	assert.Len(t, products, 2)
	assert.Len(t, products["minust"], 4)
	assert.Len(t, products["pravoved"], 5)
	for _, unit := range []string{"docker", "ssh"} {
		_, ok := groups[byName[unit].Id]
		assert.False(t, ok, unit)
	}

	// explicit group rules win over the prefix heuristic
	project.Settings.ServiceMap.Groups = []db.ServiceMapGroupRule{{Name: "pravoved-product", Patterns: []string{"pravoved_*"}}}
	groups = project.ServiceMapGroups(parsersApps)
	assert.Equal(t, db.ServiceMapGroup{Name: "pravoved-product", Source: db.ServiceMapGroupByRule}, groups[byName["pravoved_parser"].Id])
}

func sortedKeys(m map[string]bool) []string {
	res := make([]string, 0, len(m))
	for k := range m {
		res = append(res, k)
	}
	sort.Strings(res)
	return res
}

// The service map is built from the connection recording rules: run them over the instance-level world and load
// the application-level world the way the UI's constructor does.
func TestFleetAppToAppConnections(t *testing.T) {
	project := &db.Project{Id: "p1"}
	w := loadFleet(t, parsersFleet(), project)
	metrics := map[string][]*model.MetricValues{}
	for _, q := range append([]string{qRecordingRuleApplicationL7Requests, qRecordingRuleApplicationL7Latency, qRecordingRuleApplicationExternalEndpoint}, qConnectionAggregations...) {
		metrics[q] = RecordingRules[q](nil, project, w)
	}
	w2 := model.NewWorld(testFrom, testFrom.Add(4*testStep), testStep, testStep)
	c := New(nil, project, nil, nil)
	c.loadAppToAppConnections(w2, metrics, map[string]*utils.StringSet{}, project)

	edges := map[string]*model.AppToAppConnection{}
	for _, app := range w2.Applications {
		for _, u := range app.Upstreams {
			edges[app.Id.Name+" -> "+u.RemoteApplication.Id.Name] = u
			assert.NotEqual(t, "docker", u.RemoteApplication.Id.Name)
			assert.NotEqual(t, "docker", app.Id.Name)
		}
	}
	for _, e := range []string{"shards-node-agent -> shards-coroot", "gateway -> minust_parser", "mimir-worker -> pravoved_parser", "heimdall-db-collector -> pravoved_postgres"} {
		assert.NotNil(t, edges[e], e)
	}
	pg := edges["heimdall-db-collector -> minust_postgres"]
	require.NotNil(t, pg)
	assert.InDelta(t, 40, pg.GetConnectionsRequestsSum(nil).Last(), 0.01)
	failed := edges["pravoved_outbox -> external-kafka"]
	require.NotNil(t, failed)
	st, _ := failed.Status()
	assert.Equal(t, model.CRITICAL, st)
}

package constructor

import (
	"net"
	"strings"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"inet.af/netaddr"
)

// shards fork: docker-proxy resolution.
//
// With the userland proxy enabled (Docker's default) every published port is listened on twice:
//   - by docker-proxy, a process in the /system.slice/docker.service cgroup (container_net_tcp_listen_info without a
//     proxy label, "raw" below);
//   - by nobody, really, but the node agent reports the published address under the container that owns it with
//     proxy="dockerd" ("proxied" below). 0.0.0.0/:: bindings are expanded by the agent to the host's IPs.
//
// Upstream resolves a connection destination through one global ip:port index, which is ambiguous on a fleet of
// Docker hosts: loopback, bridge gateways (172.17.0.1 exists on every host) and container bridge IPs (172.18.0.2)
// repeat on every node, and a published address that the agent didn't expand (a [::1] or IPv4-mapped destination,
// an interface added after the container started) falls through to docker-proxy's raw socket, so the edge terminates
// at docker.service. The resolver below re-resolves every connection on the node that actually owns the destination
// address and prefers the container that published it:
//
//  1. normalize the destination (actual_destination, or destination for failed-only connections): IPv4-mapped IPv6 is
//     unmapped, [::1]/127.0.0.0/8 are loopback;
//  2. pick the target node: the client's own node for loopback; otherwise the node that owns the IP (node_net_ip,
//     published and docker-proxy listen addresses), preferring the client's node when several nodes share it (bridge
//     gateways). An IP that is not a host IP (a container bridge IP) is resolved on the client's node first;
//  3. on the target node: a proxied listen for ip:port -> that container; a raw listen owned by something other than
//     docker-proxy -> that instance (a host process really listens there); otherwise, if exactly one container on
//     that node publishes the port (on any address) -> that container;
//  4. otherwise keep upstream's choice.
//
// docker-proxy's own connections (the backend leg docker-proxy -> container_ip:port) are never drawn (upstream drops
// them), but they carry L7 stats when the client side has none (e.g. TLS terminated in the container is still
// opaque, but plain HTTP from a client the agent sees only as a TCP flow). The resolver keeps those legs aside and
// copies their requests/latency/histograms onto the re-attributed client edges that lack them, split in proportion to
// the clients' connection counts.

// disableDockerProxyResolution restores upstream's behaviour (used by tests to compare).
var disableDockerProxyResolution = false

type dockerProxyResolver struct {
	proxied map[*model.Node]map[string]*model.Instance // published host ip:port -> container
	raw     map[*model.Node]map[string]*model.Instance // ip:port -> listening instance (docker-proxy last)
	hostIPs map[string]map[*model.Node]bool            // host IP -> nodes owning it
	legs    map[*model.Instance]map[model.ConnectionKey]*model.Connection

	reattributed map[*model.Instance][]*model.Connection // container -> client edges resolved through a published port
	Resolved     int                                     // number of re-attributed connections (for tests/debugging)
}

func newDockerProxyResolver() *dockerProxyResolver {
	return &dockerProxyResolver{
		proxied:      map[*model.Node]map[string]*model.Instance{},
		raw:          map[*model.Node]map[string]*model.Instance{},
		hostIPs:      map[string]map[*model.Node]bool{},
		legs:         map[*model.Instance]map[model.ConnectionKey]*model.Connection{},
		reattributed: map[*model.Instance][]*model.Connection{},
	}
}

func isDockerProxy(i *model.Instance) bool {
	return i != nil && i.Owner != nil && i.Owner.Id.Name == "docker"
}

// normalizeIP returns the canonical text form of an IP (IPv4-mapped addresses unmapped, zones dropped).
func normalizeIP(s string) (string, netaddr.IP, bool) {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	if i := strings.IndexByte(s, '%'); i >= 0 {
		s = s[:i]
	}
	ip, err := netaddr.ParseIP(s)
	if err != nil {
		return "", netaddr.IP{}, false
	}
	ip = ip.Unmap()
	return ip.String(), ip, true
}

func (r *dockerProxyResolver) addHostIP(ip string, node *model.Node) {
	if ip == "" || node == nil {
		return
	}
	if _, p, ok := normalizeIP(ip); !ok || p.IsLoopback() || p.IsUnspecified() || p.IsLinkLocalUnicast() {
		return
	} else {
		ip = p.String()
	}
	nodes := r.hostIPs[ip]
	if nodes == nil {
		nodes = map[*model.Node]bool{}
		r.hostIPs[ip] = nodes
	}
	nodes[node] = true
}

// addListen indexes one container_net_tcp_listen_info series.
func (r *dockerProxyResolver) addListen(instance *model.Instance, ipStr, port, proxy string) {
	node := instance.Node
	if node == nil {
		return
	}
	ip, parsed, ok := normalizeIP(ipStr)
	if !ok {
		return
	}
	key := net.JoinHostPort(ip, port)
	if proxy != "" {
		m := r.proxied[node]
		if m == nil {
			m = map[string]*model.Instance{}
			r.proxied[node] = m
		}
		m[key] = instance
		r.addHostIP(ip, node) // a published address is a host address by definition
		return
	}
	m := r.raw[node]
	if m == nil {
		m = map[string]*model.Instance{}
		r.raw[node] = m
	}
	if cur := m[key]; cur == nil || (isDockerProxy(cur) && !isDockerProxy(instance)) {
		m[key] = instance
	}
	if isDockerProxy(instance) && !parsed.IsUnspecified() {
		r.addHostIP(ip, node) // docker-proxy lives in the host network namespace
	}
}

// backendLeg returns a detached connection collecting docker-proxy's own (backend leg) metrics, or nil if the
// instance is not docker-proxy.
func (r *dockerProxyResolver) backendLeg(instance *model.Instance, m *model.MetricValues) *model.Connection {
	if !isDockerProxy(instance) || m.ActualDestination == "" && m.Destination == "" {
		return nil
	}
	byKey := r.legs[instance]
	if byKey == nil {
		byKey = map[model.ConnectionKey]*model.Connection{}
		r.legs[instance] = byKey
	}
	c := byKey[m.ConnectionKey]
	if c == nil {
		dest := m.ActualDestination
		if dest == "" {
			dest = m.Destination
		}
		ip, port, err := net.SplitHostPort(dest)
		if err != nil {
			return nil
		}
		c = &model.Connection{
			Instance:          instance,
			ActualRemoteIP:    ip,
			ActualRemotePort:  port,
			RequestsCount:     map[model.Protocol]map[string]*timeseries.TimeSeries{},
			RequestsLatency:   map[model.Protocol]*timeseries.TimeSeries{},
			RequestsHistogram: map[model.Protocol]map[float32]*timeseries.TimeSeries{},
		}
		byKey[m.ConnectionKey] = c
	}
	return c
}

// target resolves ip:port as seen by the client to the instance accepting the connection. published is true when the
// result was found through a docker published port (so the backend-leg stats of that container may be merged).
func (r *dockerProxyResolver) target(client *model.Instance, ipStr, port string) (*model.Instance, bool) {
	ip, parsed, ok := normalizeIP(ipStr)
	if !ok || port == "" || port == "0" {
		return nil, false
	}
	key := net.JoinHostPort(ip, port)
	var node *model.Node
	switch owners := r.hostIPs[ip]; {
	case parsed.IsLoopback():
		node = client.Node
	case len(owners) == 1:
		for n := range owners {
			node = n
		}
	case len(owners) > 1:
		if client.Node != nil && owners[client.Node] {
			node = client.Node
		} else {
			return nil, false // shared address (e.g. a bridge gateway) not present on the client's node: ambiguous
		}
	default:
		// not a host address: a container/bridge IP. Bridge networks are host-local, so a listener on the client's
		// own node wins over the global (fleet-wide, colliding) index.
		if client.Node != nil {
			if i := r.raw[client.Node][key]; i != nil && !isDockerProxy(i) {
				return i, false
			}
		}
		return nil, false
	}
	if node == nil {
		return nil, false
	}
	if i := r.proxied[node][key]; i != nil {
		return i, true
	}
	if i := r.raw[node][key]; i != nil && !isDockerProxy(i) {
		return i, false
	}
	// the published address wasn't expanded to this IP (IPv6, an interface added later, a loopback form):
	// fall back to the only container on that node publishing the port
	var found *model.Instance
	for k, i := range r.proxied[node] {
		if _, p, _ := net.SplitHostPort(k); p != port {
			continue
		}
		if found != nil && found != i {
			return nil, false
		}
		found = i
	}
	return found, found != nil
}

// resolve re-attributes the connections of all instances; it must run after upstream's listen lookup and before
// unresolved connections are turned into external services.
func (r *dockerProxyResolver) resolve(w *model.World, instancesByListen map[model.Listen]*model.Instance) {
	if disableDockerProxyResolution {
		return
	}
	for _, n := range w.Nodes {
		for _, iface := range n.NetInterfaces {
			for _, a := range iface.Addresses {
				r.addHostIP(a, n)
			}
		}
	}
	if len(r.proxied) == 0 && len(r.legs) == 0 && len(r.raw) == 0 {
		return
	}
	for _, app := range w.Applications {
		for _, instance := range app.Instances {
			if isDockerProxy(instance) {
				continue
			}
			for _, u := range instance.Upstreams {
				ip, port := u.ActualRemoteIP, u.ActualRemotePort
				if ip == "" {
					ip, port = u.ServiceRemoteIP, u.ServiceRemotePort
				}
				t, published := r.target(instance, ip, port)
				if t == nil {
					continue
				}
				if t != u.RemoteInstance {
					u.RemoteInstance = t
					r.Resolved++
				}
				if published {
					r.reattributed[t] = append(r.reattributed[t], u)
				}
			}
		}
	}
	r.mergeBackendLegs(instancesByListen)
}

func (r *dockerProxyResolver) mergeBackendLegs(instancesByListen map[model.Listen]*model.Instance) {
	if len(r.legs) == 0 || len(r.reattributed) == 0 {
		return
	}
	byTarget := map[*model.Instance][]*model.Connection{}
	for proxy, legs := range r.legs {
		for _, leg := range legs {
			ip, _, ok := normalizeIP(leg.ActualRemoteIP)
			if !ok {
				continue
			}
			key := net.JoinHostPort(ip, leg.ActualRemotePort)
			t := r.raw[proxy.Node][key]
			if t == nil {
				t = instancesByListen[model.Listen{IP: ip, Port: leg.ActualRemotePort}]
			}
			if t == nil || isDockerProxy(t) {
				continue
			}
			byTarget[t] = append(byTarget[t], leg)
		}
	}
	for t, legs := range byTarget {
		clients := r.reattributed[t]
		if len(clients) == 0 {
			continue
		}
		protocols := map[model.Protocol]bool{}
		for _, leg := range legs {
			for p := range leg.RequestsCount {
				protocols[p] = true
			}
			for p := range leg.RequestsLatency {
				protocols[p] = true
			}
		}
		for p := range protocols {
			var lacking []*model.Connection
			for _, c := range clients {
				if len(c.RequestsCount[p]) == 0 && c.RequestsLatency[p] == nil {
					lacking = append(lacking, c)
				}
			}
			if len(lacking) == 0 {
				continue
			}
			shares := connectionShares(lacking)
			for idx, c := range lacking {
				share := shares[idx]
				for _, leg := range legs {
					for status, ts := range leg.RequestsCount[p] {
						if c.RequestsCount[p] == nil {
							c.RequestsCount[p] = map[string]*timeseries.TimeSeries{}
						}
						c.RequestsCount[p][status] = merge(c.RequestsCount[p][status], scale(ts, share), timeseries.NanSum)
					}
					if ts := leg.RequestsLatency[p]; ts != nil {
						c.RequestsLatency[p] = merge(c.RequestsLatency[p], scale(ts, share), timeseries.NanSum)
					}
					for le, ts := range leg.RequestsHistogram[p] {
						if c.RequestsHistogram[p] == nil {
							c.RequestsHistogram[p] = map[float32]*timeseries.TimeSeries{}
						}
						c.RequestsHistogram[p][le] = merge(c.RequestsHistogram[p][le], scale(ts, share), timeseries.NanSum)
					}
				}
			}
		}
	}
}

// connectionShares splits 1 between connections in proportion to their connection counts (equally if unknown).
func connectionShares(cs []*model.Connection) []float32 {
	weights := make([]float32, len(cs))
	var total float32
	for i, c := range cs {
		w := c.SuccessfulConnections.Reduce(timeseries.NanSum)
		if timeseries.IsNaN(w) || w <= 0 {
			w = c.Active.Reduce(timeseries.NanSum)
		}
		if timeseries.IsNaN(w) || w < 0 {
			w = 0
		}
		weights[i] = w
		total += w
	}
	for i := range weights {
		if total > 0 {
			weights[i] /= total
		} else {
			weights[i] = 1 / float32(len(cs))
		}
	}
	return weights
}

func scale(ts *timeseries.TimeSeries, k float32) *timeseries.TimeSeries {
	if k == 1 {
		return ts.Map(func(t timeseries.Time, v float32) float32 { return v })
	}
	return ts.Map(func(t timeseries.Time, v float32) float32 { return v * k })
}

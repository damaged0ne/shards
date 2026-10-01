---
sidebar_position: 10
---

# Service Map

The service map (**Overview → Service map**) shows applications and the connections between them, built from the
node agent's TCP connection metrics (`container_net_tcp_*`), L7 request metrics and `ip_to_fqdn`. This page describes
how shards attributes traffic on Docker hosts, how applications are grouped, and the settings in
**Project settings → Applications → Service map**.

Everything below works with the upstream node agent. The shards node agent adds better signals when present
(`shards_compose_info`, `/swarm/<project>/<service>/<n>` container ids, `--hostname-override`), but none of them is required.

## Published Docker ports (docker-proxy)

With Docker's userland proxy (the default), every published port is listened on by `docker-proxy`, a process in the
`/system.slice/docker.service` cgroup. The node agent reports the address twice in `container_net_tcp_listen_info`:
once as docker-proxy's raw socket, and once under the container that published it with `proxy="dockerd"`
(`0.0.0.0` bindings expanded to the host's addresses). Without special handling, traffic that reaches a container
through a published port looks like it terminates at `docker`.

shards resolves every connection destination (`actual_destination`, or `destination` for connections that only
failed) on the node that owns the address:

1. **Normalize** the address: IPv4-mapped IPv6 (`[::ffff:192.168.0.4]:81`) is unmapped; `127.0.0.0/8` and `[::1]` are loopback.
2. **Pick the target node.**
   - Loopback → the client's own node.
   - A host address (from `node_net_interface_ip`, published addresses and docker-proxy's listens) → the node that owns
     it. Addresses present on several nodes — Docker bridge gateways such as `172.17.0.1` exist on every Docker host —
     resolve to the client's node when it has the address, and are otherwise left alone as ambiguous.
   - Any other address (a container bridge IP like `172.18.0.2`, which repeats across hosts) → a listener on the
     client's own node, if there is one.
3. **Pick the instance on that node:** the container with a `proxy="dockerd"` listen for `ip:port`; otherwise the raw
   owner of the socket if it isn't docker-proxy (a host process really listens there); otherwise the only container on
   that node publishing that port on any address (covers `[::1]`, IPv4-mapped forms and `0.0.0.0` bindings the agent
   didn't expand).
4. Otherwise the upstream resolution is kept (and unknown destinations become external services).

docker-proxy's own connections — the backend leg `docker-proxy → container_ip:port` — are never drawn, so there are no
double edges. When they carry L7 stats (requests, latency, errors) that the re-attributed client edges don't have,
those stats are copied onto the client edges, split between the clients of that published port in proportion to
their connection counts. A client's own L7 stats are never added to.

This works fleet-wide: a client on host A connecting to `hostB:8080` lands on the container on host B that published
port 8080.

## Categories

Categories are described in [Application Categories](./application-categories). Two built-ins matter for the map:

| Category | Contents | Map default |
|---|---|---|
| `application` | everything not matched by another category | expanded |
| `monitoring` | upstream's patterns plus `_/shards-*`, node/cluster agents, exporters, alloy, promtail, cadvisor, otel collectors, `_/heimdall-*` | collapsed |
| `control-plane` | Kubernetes control plane, `*/docker*`, `*/systemd*`, … | hidden |
| `system` | host infrastructure units in the non-Kubernetes namespace: `docker`, `containerd`, `ssh`, `chrony`, `fail2ban`, `cron`, `atd`, `rsyslog`, `watchdog`, `snapd`, `dbus`, `systemd-*`, … | hidden |

`system` is checked before the other built-ins. Only well-known infrastructure units are matched: on hosts without
containers the business applications are systemd services too, so shards does not hide every `/system.slice/*` unit.
To move a unit back, add a custom pattern for it to another category — custom patterns always win.
Applications in a hidden category keep all their metrics and still appear on the node page.

### Map modes

Each category has a map mode, editable under **Project settings → Applications → Service map**:

- **expanded** — every application is a node.
- **collapsed** — the whole category is one node with the union of its members' edges; click it (or its chip above the
  map) to expand it into a framed group.
- **muted** — collapsed and visually de-emphasized (dashed, translucent). Use it for a custom `legacy` category, e.g.
  with the pattern `_/heimdall-*`; a category named `legacy` is muted by default.
- **hidden** — not drawn until its checkbox above the map is ticked.

## Groups

Applications are framed by group in the topology view:

1. a **group rule** from the settings (a group name plus glob patterns on the application name, e.g. `minust_* minust-*`);
2. the **compose project** reported by the shards node agent (`shards_compose_info`) or the swarm id;
3. the Kubernetes/Nomad **namespace**;
4. the **container-name prefix** (split on the first `_` or `-`: `minust_parser` → `minust`), when at least two containers
   share it. systemd units are never grouped by prefix. Turn this off with *group containers by name prefix*.

Click a group's header on the map, or its chip above the map, to collapse it into one node. The state is remembered per
browser. The *group frames* toolbar button hides the frames.

Edges: healthy connections are neutral grey, failed connection attempts are red, edges without recent traffic are
dashed. External endpoints (no agent on the other side) are drawn with a dashed outline and named by their FQDN from
`ip_to_fqdn` when known (e.g. `pravoved.ru:443`).

## Node display names

**Project settings → Applications → Service map → Node display names** maps a `machine_id` (or a hostname) to the name
shown everywhere: the node list, node pages, the map's tooltips, alerts and MCP tools. The node page is still reachable
by the original hostname. This is the server-side alternative to the shards node agent's `--hostname-override`; use one
or the other.

## API

`GET/PUT /api/project/<project>/service_map_settings`

```json
{
  "category_modes": {"monitoring": "collapsed", "legacy": "muted"},
  "groups": [{"name": "minust", "patterns": ["minust_*"]}],
  "disable_prefix_grouping": false,
  "node_display_names": {"<machine_id>": "parsers"}
}
```

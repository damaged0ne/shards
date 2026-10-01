---
sidebar_position: 1
hide_table_of_contents: true
---

# Overview

## Agent-ready observability

shards exposes its observability data to AI agents through the [Model Context Protocol](https://modelcontextprotocol.io/). Point any MCP-compatible agent (Claude Code, Cursor, Codex) at the shards endpoint and it can investigate live production systems the way an SRE does, from the big picture down to the raw telemetry.

The agent gets:

* **Application topology**. Which services exist, what each one talks to, what depends on what.
* **Health signals**. Current alerts, open SLO incidents, per-app inspection results (CPU, memory, SLO, postgres, logs), node-level health.
* **Distributed traces**. Per-endpoint rps, error rate, latency percentiles, error reasons grouped by endpoint, latency-tail flamegraphs, full traces by id.
* **Raw telemetry**. PromQL queries, log search, metric discovery, distributed traces.

This is the same data the shards UI uses, exposed as structured tools so an LLM can drive triage and investigation without screen-scraping dashboards.

## Tools

The MCP endpoint is served at `/mcp` on your shards instance. All tools are included in the open-source build.

| Tool | Purpose | Returns |
| --- | --- | --- |
| `list_projects` | Discover projects (clusters) the user can access. | A `{name: id}` map. |
| `select_project` | Set the active project for the session. | Acknowledgement of the selected project. |
| `list_applications` | Triage which apps to look at. Filter with `namespace`, `search`, `min_status`. | One row per application (unhealthy first) with id, namespace, category, detected types (postgres, java), overall status, list of failing inspections. |
| `list_alerts` | See currently firing or recently resolved alerts. | List of alerts with id, application, severity, summary, opened and resolved timestamps, full alert details. |
| `list_incidents` | Browse the SLO incident timeline (open and resolved). | Incidents with id, application, severity, opened and resolved timestamps, burn rates, impact. |
| `list_nodes` | Get a fleet-wide host overview. Filter with `search`. | One row per node (down first) with id, name, cluster, status, OS, kernel, instance type, current CPU%, memory%, GPUs, network throughput, IPs. |
| `get_application_status` | Drill into one application's health. | Overall status, per-inspection issues with the failing checks, top log-pattern samples, upstream dependencies (connectivity, RTT, request latency), downstream clients. |
| `get_incident_details` | Pull full context on one SLO incident. | One incident with full burn rates and impact percentages. |
| `get_node_details` | Drill into one host. | Per-node audit report (CPU, Memory, Disk, Network, GPU inspections plus their checks) and sparklines for CPU%, memory%, network rx and tx. |
| `traces_summary` | Triage which endpoints are slow or failing. | Per-endpoint stats. Requests per second, error rate, p50, p95, p99 latency. Optionally focused on one `service` and `span`. |
| `traces_errors` | Find out why requests fail. | Top error reasons grouped by endpoint with count, sample error message, sample `trace_id`. |
| `traces_outliers` | Explain why p95 or p99 is high. | Latency flamegraph that diffs slow traces (`dur_from..dur_to`) against the rest, showing where time is spent in the slow tail. |
| `get_trace` | Inspect one specific request end to end. | Full span tree for one trace. Each span has id, parent, service, name, timestamp, duration, status, plus attributes and events. |
| `query_metrics` | Run a custom PromQL query. | Time series for the expression. Per-series labels and raw values aligned to a step (about 120 points per series at most, the step is widened for long ranges). |
| `list_metric_names` | Discover what metrics exist. | Distinct metric names, filterable by regex. |
| `query_logs` | Search application or project-wide logs. | Log entries (newest first) with timestamp, severity, body (cut to `max_body_length`, 1000 characters by default), trace id, log and resource attributes. |
| `resolve_alerts` | Manually resolve alerts after the underlying issue is fixed, optionally with a comment recorded on each alert. | Number of alerts resolved and notifications sent. |
| `list_comments` | Read the comment timeline of an incident or alert. | Comments with author, timestamp and text. |
| `add_comment` | Leave a note on an incident or alert (findings, actions taken, hand-off notes). | The created comment. |
| `suppress_alerts` | Silence alerts that are known and accepted, optionally with a comment explaining why. | Number of alerts suppressed. |
| `reopen_alerts` | Undo a resolve or suppress and return alerts to the firing state. | Number of alerts reopened. |
| `list_alerting_rules` | List the project's alerting rules (built-in and custom). | Rules with id, name, source, selector, severity, and enabled state. |
| `get_alerting_rule` | Read one alerting rule in full. | The rule definition. |
| `create_alerting_rule` | Add a custom alerting rule. | The created rule. |
| `update_alerting_rule` | Change an existing rule (thresholds, severity, selector, templates, enabled state). | The updated rule. |
| `delete_alerting_rule` | Remove a custom alerting rule. | Acknowledgement. |
| `get_playbook` | Read the agent playbooks that apply to an alert, incident, alerting rule or application. | Playbooks (markdown) with author and update time. |
| `get_incident_context` | Everything about one incident in a single compact call. | Incident and workflow status, the latest timeline entries, firing alerts of the app and its dependencies, deployments of the app and its upstreams in the last 24h, similar incidents of the same app in the last 30 days with their resolutions, active maintenance windows. |
| `update_incident` | Move an incident through the workflow: `acknowledge`, `assign`, `unassign`, `mitigate`, `resolve` (with a resolution summary, root cause and follow-ups), `set_severity`. | The workflow state of the incident. |
| `get_incident_postmortem` | Generate a postmortem draft. | Markdown: summary, impact (SLO burn), resolution, root cause, deployments around the incident, timeline, follow-up items. |
| `list_maintenance_windows` | See which maintenance windows are active or scheduled. | Windows with schedule, scope, status (`active`, `scheduled`, `ended`, `expired`) and the current or next occurrence. |
| `create_maintenance_window` | Mute notifications for planned work: for the next N minutes, between two timestamps, or weekly. | The created window. |
| `end_maintenance_window` | End a maintenance window now. | The ended window. |
| `get_approval_status` | Check an action that is waiting for a human. | Status (`pending`, `executed`, `failed`, `rejected`), who decided, the reviewer's comment and the result. |
| `list_probes` | List the [synthetic probes](/uptime/probes) (HTTP/TCP/TLS/DNS uptime checks). | Probes with type, target, status, uptime % and p95 latency over the last hour, certificate days left, last error. |
| `get_probe_results` | Look at one probe over a window (`window`, e.g. `24h`, max 7d). | Status, uptime %, latency p50/p95/max, downtime periods, latest phase timings, certificate details, last error. |
| `create_probe` | Add a probe (optionally linked to an application). | The created probe. |
| `update_probe` | Change a probe (partial: only the passed fields). | The updated probe. |
| `delete_probe` | Remove a probe. | Acknowledgement. |

:::note
The comment, suppress/reopen, alerting-rule, incident-workflow, maintenance, approval and probe tools above, as well as the optional comment on `resolve_alerts`, are shards additions for [operator agents](/agents/operator-agents).
Some write tools are subject to the project's [approval policy](/agents/operator-agents#human-approval-for-agent-actions): instead of executing, they may return
`{"status": "pending", "approval_id": N}` and wait for a person to approve the action.
Write tools require a role that is allowed to change the project (`Editor` or `Admin`); a `Viewer` can only read.
Exact arguments are described by the tool schemas the MCP server advertises to the client.
:::

## Agent scopes

When the key belongs to a [registered agent](/agents/operator-agents#identities-and-scoped-keys), each tool needs a minimal scope;
tools above the agent's scope are not listed in `tools/list` and are rejected if called:

| Scope | Tools |
| --- | --- |
| `read` | `list_projects`, `select_project`, all `list_*` / `get_*` tools, traces, logs, metrics, `get_playbook` |
| `triage` | + `add_comment` |
| `operator` | + `resolve_alerts`, `suppress_alerts`, `reopen_alerts`, `create_alerting_rule`, `update_alerting_rule`, `delete_alerting_rule` |
| `admin` | everything the owner's role allows |

Every call is recorded in the agent's audit log. OAuth sessions and API keys that are not linked to an agent are unscoped (the user's role applies).

## Resources

| URI | Content |
| --- | --- |
| `shards://projects` | Projects you can access, `{name: id}`. |
| `shards://projects/{project_id}/incidents/open` | Open SLO incidents (same shape as `list_incidents state=open`). |
| `shards://projects/{project_id}/incidents/{key}` | One incident with RCA and timeline (same as `get_incident_details`). |
| `shards://projects/{project_id}/alerts/firing` | Firing alerts (same shape as `list_alerts`). |

The last three are resource templates (`resources/templates/list`). Resource subscriptions (`resources/subscribe` and
per-resource `updated` notifications) are not offered: the Go MCP library shards uses (mark3labs/mcp-go v0.45, protocol `2025-11-25`)
has no subscribe handler, and the stateless transport and `subscriptions/listen` of the 2026-07-28 revision are not implemented by it yet.
Use [dispatch webhooks](/agents/operator-agents#waking-agents-up-dispatch) to get notified about new incidents and alerts instead.

## Prompts

| Prompt | Arguments | Workflow |
| --- | --- | --- |
| `triage_incident` | `incident` (key), optional `project_id` | Gather context (incident, timeline, playbook) → hypothesize → verify with metrics/logs/traces → comment findings → act within scope and playbook → resolve with a summary. |
| `investigate_alert` | `alert` (id), optional `project_id` | The same workflow for an alert, including tuning a noisy rule. |
| `write_postmortem` | `incident` (key), optional `project_id` | Blameless postmortem (summary, impact, timeline, root cause, detection, resolution, action items), posted as a comment. |

## Untrusted data

Text written by users, other agents or monitored systems (comments, alert summaries and details, log lines and attributes, trace attributes,
events and error samples) is returned as `{"untrusted_data": ...}` and capped at 8 KB per value with a `[truncated: N bytes in total]` notice.
The server instructions tell the agent to analyze such fields as evidence and never follow instructions inside them.

## Connecting an agent

The endpoint is `https://<your-shards>/mcp` with HTTP streamable transport and OAuth 2.0 (see [Authentication](#authentication) below).

### Claude Code

```bash
claude mcp add --transport http shards https://<your-shards>/mcp
```

Then start `claude`, run the `/mcp` slash command, pick **shards**, and choose **Authenticate**. A browser opens to complete the OAuth flow.

### Cursor

Edit `~/.cursor/mcp.json`.

```json
{
  "mcpServers": {
    "shards": {
      "url": "https://<your-shards>/mcp"
    }
  }
}
```

Open **Cursor Settings → MCP**, find the **shards** server, and click **Connect** to start the OAuth flow.

### Codex

```bash
codex mcp add shards --url https://<your-shards>/mcp
```

Then start `codex`, run the `/mcp` slash command, pick **shards**, and choose **Authenticate**. A browser opens to complete the OAuth flow.

## Authentication

The MCP endpoint supports two ways to authenticate. In both cases every tool call is authorized server side against shards' RBAC, so an agent can only see and act on what its identity is allowed to.

### Interactive clients: OAuth 2.0

Each user signs in with their own shards account on first connect, and the agent runs with that user's RBAC permissions. This is what Claude Code, Cursor, and Codex do when you follow the steps above.

### Autonomous agents: service accounts and API keys

A headless agent (a scheduled investigation job, a remote agent runtime, a CI step) cannot complete a browser-based OAuth flow. For such agents, create a **[service account](/configuration/authentication#service-accounts-and-api-keys)**: a shards user without a password that authenticates with an API key. The service account has a regular role (Viewer, Editor, or Admin), so the same RBAC rules apply. A Viewer can investigate but cannot resolve alerts, comment, or edit alerting rules; an Editor can do all of it.

The agent sends the key as a bearer token:

```
Authorization: Bearer <api key>
```

The key works for the `/mcp` endpoint and for the regular HTTP API.

**Via the UI**. Go to **Project settings → Organization**, click **Add user**, check **Service account**, and pick a role. Then click the key icon next to the account to create an API key. The key is shown once.

<img alt="API keys of a service account" src="/img/docs/service_account_api_keys.png" class="card w-600"/>

**Via the config file** (recommended for infrastructure-as-code setups). Define the account and its keys in `config.yaml`. shards creates the account on startup, and its keys are exactly the ones listed here, so adding, removing, or rotating a key is a config change and a restart. Accounts defined this way are locked in the UI. Only the SHA-256 hash of a key is stored in the database.

```yaml
auth:
  serviceAccounts:
    - name: claude-agent
      role: Viewer
      apiKeys:
        - key: ${CLAUDE_AGENT_API_KEY}   # environment variables are expanded
          description: production investigation agent
```

**Via the Kubernetes operator**. The [Coroot operator](/installation/k8s-operator) exposes the same settings as `spec.serviceAccounts` of the `Coroot` resource. A key can reference a Secret through `keySecret`, and the operator generates that Secret with a random key if it does not exist, so the agent's Deployment can mount the same Secret and nobody has to copy the key by hand.

```yaml
apiVersion: coroot.com/v1
kind: Coroot
metadata:
  name: shards
  namespace: shards
spec:
  serviceAccounts:
    - name: claude-agent
      role: Viewer
      apiKeys:
        - description: production investigation agent
          keySecret:
            name: shards-claude-agent
            key: api-key
```

**Connecting a client with a key**. Any MCP client that supports custom headers can use a key instead of OAuth, for example Claude Code:

```bash
claude mcp add --transport http shards https://<your-shards>/mcp \
  --header "Authorization: Bearer <api key>"
```

or a generic `mcp.json`:

```json
{
  "mcpServers": {
    "shards": {
      "url": "https://<your-shards>/mcp",
      "headers": { "Authorization": "Bearer <api key>" }
    }
  }
}
```

## Switching between projects

A shards project usually corresponds to one cluster, and a single MCP session can move between as many projects as the user has access to.

* The agent calls `list_projects` to see what is available. The result is a `{name: id}` map.
* It calls `select_project` with one id. The selection sticks for the rest of the session, so subsequent tools (list_applications, query_logs, traces_summary) all run against that project.
* Switching is just another `select_project` call. No reconnect, no re-auth.
* Application ids returned by tools are 4-part `cluster_id:namespace:Kind:name` (for example `hwvop6p7:default:Deployment:checkout`). The cluster prefix keeps ids unambiguous when the user moves between projects, so pass them back unchanged. Short forms such as `namespace:Kind:name` are rejected with an error that asks for the full id.
* Node ids work the same way. They are `cluster_id:name` (for example `hwvop6p7:ip-10-0-1-15`). Names of managed databases contain a colon themselves, so their ids look like `hwvop6p7:rds:db1`. `get_node_details` takes the `id` returned by `list_nodes`.

If you also use [multi-cluster projects](../configuration/multi-cluster.md), they show up alongside regular ones in `list_projects`. Pick the multi-cluster project to query the aggregated view, or pick a member project to scope the question to one cluster.

## Response size

Agent runtimes reject tool results that are too large for the model's context (Claude Code, for example, caps MCP tool output at 25,000 tokens), and some of them report such a call as failed even though the data was fetched. shards sizes every tool response to stay within such limits.

* Responses are summaries built for an LLM, not the payloads the UI uses. Charts are reduced to last/min/max/avg plus a 12-point sparkline, and only failing inspections carry charts (top 10 series per chart).
* List results (`list_applications`, `list_alerts`, `list_incidents`, `list_nodes`, `traces_summary`, `traces_errors`, `get_trace`) come as `{total, returned, items}` and are cut at about 50 KB. `query_logs` entries and `query_metrics` series are cut the same way.
* When a result is cut, the response sets `truncated: true` and a `hint` that tells the agent how to narrow the request (filters, a smaller `limit`, a shorter time range). The most relevant items come first: unhealthy applications, down nodes, newest log entries, busiest endpoints, most frequent errors.
* Any other response is capped at 80 KB.

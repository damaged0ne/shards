---
sidebar_position: 1
---

# Operator agents

shards is designed to be operated by people and by AI agents side by side. An operator agent is an LLM-driven agent
(Claude Code, Cursor, Codex, or a headless agent runtime) that connects to shards over the [MCP endpoint](/mcp/overview),
triages what is firing, investigates it with the same data the UI shows, and records what it did so the humans on call can follow along.

Everything an agent does goes through shards' regular authentication and RBAC, and every write is attributed to the agent's identity.

## Agents area

Open **Automation → Agents** in the left rail. It lists the agents registered in the project with their status
(active: seen in the last 5 minutes, idle, never connected, expired, disabled), scope, owner, calls and errors in the last hour,
and whether a dispatch webhook is configured. **Connect an agent** shows the MCP URL and ready-to-paste client configuration.

An agent page shows its configuration, its keys, its MCP sessions (client name and version from the MCP `initialize` request,
first and last call, number of calls), the **activity** timeline of every tool call (tool, arguments, status, duration, target)
and the **dispatch delivery** log.

### Identities and scoped keys

Register each agent (name, description, kind/vendor, model, owner) and create a key for it on its page. The key is a regular user
API key of the owner, linked to the agent, so the agent acts on behalf of the owner, and its **scope** caps what it may do on top of the owner's role:

| Scope | Allows |
| --- | --- |
| `read` | Query only: projects, applications, incidents, alerts, nodes, traces, logs, metrics, rules, playbooks, timelines. |
| `triage` | `read` + comments (`add_comment`, REST comments) and acknowledge-type actions. |
| `operator` | `triage` + resolve / suppress / reopen alerts and alerting rule changes (create / update / delete, playbooks). |
| `admin` | Everything the owner's role allows (project settings, integrations, agents, ...). |

The effective permission is always the intersection of the owner's role and the scope: an `operator` agent owned by a `Viewer` still can't resolve alerts.
Optionally an agent can be limited to a set of projects and get an expiry date; the keys of an expired or disabled agent stop authenticating.
Deleting an agent revokes its keys. User API keys that are not linked to an agent keep working as before (the user's role, no scope).

Enforcement:

* **MCP**: every tool is tagged with the minimal scope it needs. Tools above the agent's scope are hidden from `tools/list` and rejected if called anyway.
* **REST**: reads need `read`; comment writes `triage`; alert resolve/suppress/reopen, alerting rule and playbook writes `operator`; any other write `admin`.
* **RBAC**: every permission check additionally drops edit permissions the scope doesn't cover, and projects the agent may not access.

Comments and actions by a registered agent are attributed to the agent's name and link to its page in every timeline.

### Audit log

Every MCP tool call (and MCP resource read) and every REST write by an agent is recorded: agent, session, tool or `METHOD route`,
arguments (keys that look like secrets, such as `token`, `secret`, `password`, `api_key`, `authorization`, are redacted, long strings are cut
to 300 characters and the whole record to 4 KB), status (`ok`, `error`, `denied`), error, duration, and target (incident, alert, rule, application).
Entries, sessions and deliveries are kept for **30 days** and pruned hourly.

`GET /api/project/{project}/agents/{id}/activity?limit=200&before=<id>` returns the log newest first (`before` pages back).

### Approvals

Write tools pass through an approval hook after the scope check. When the approval feature is enabled,
gated actions return a "pending approval" result instead of running; the agent is woken up with an `approval_decided` dispatch event once a human decides.

## Waking agents up: dispatch

Agents should not have to poll. Configure a **dispatch webhook** on the agent page: an HTTP(S) URL, a signing secret and the events to deliver:

| Event | When |
| --- | --- |
| `incident_opened` | An SLO incident is opened. |
| `incident_escalated` | The severity of an open incident grows (warning → critical). |
| `alert_fired` | An alert starts firing. |
| `mention` | A comment mentions the agent: `@agent-name`. |
| `approval_decided` | A human approved or rejected a gated action of the agent. |
| `manual` | A human used **Ask agent** on an incident or alert (always delivered). |
| `test` | **Send test** on the agent page. |

Automatic events are filtered by minimal severity (warning or critical), application categories and application patterns
(`namespace:Kind:name` globs), deduplicated per event and incident/alert (default 30 minutes) and rate-limited (default 30 per hour).
Skipped events are listed in the delivery log with the reason. Deliveries are retried with exponential backoff
(10s, 30s, 1.5m, 4.5m, 13.5m; 6 attempts); 4xx responses other than 408/429 are not retried.

**Ask agent**: the incident and alert pages have an **Ask agent** button next to the timeline. Pick an agent, optionally type an instruction,
and shards sends a `manual` task and records "asked an agent" in the timeline.

### Payload

`POST <url>` with `Content-Type: application/json`:

```json
{
  "version": 1,
  "delivery_id": 42,
  "event": "alert_fired",
  "created_at": "2026-10-01T06:11:41Z",
  "summary": "Alert a1 fired (critical) for checkout",
  "project": { "id": "4v6w3132", "name": "production" },
  "agent": { "id": 1, "name": "triage-bot", "scope": "triage" },
  "alert": {
    "id": "a1", "application_id": "c1:default:Deployment:checkout", "rule_id": "storage-space",
    "rule_name": "Low disk space", "severity": "critical", "opened_at": "2026-10-01T06:05:00Z",
    "url": "https://shards.example.com/p/4v6w3132/alerts?alert=a1"
  },
  "incident": { "id": "i-key", "application_id": "...", "severity": "critical", "opened_at": "...", "url": "..." },
  "comment": { "id": 8, "author": "Jane", "author_kind": "user", "target_type": "alert", "target_id": "a1" },
  "instruction": "Find out why ... (manual tasks only)",
  "requested_by": "Jane",
  "approval": { "id": "...", "decision": "approved" },
  "mcp": {
    "url": "https://shards.example.com/mcp",
    "project_id": "4v6w3132",
    "next_tool": "get_alert",
    "next_args": { "id": "a1" },
    "prompt": "investigate_alert"
  },
  "links": { "ui": "https://shards.example.com/p/4v6w3132/alerts?alert=a1", "agent": "https://shards.example.com/p/4v6w3132/agents/1" },
  "untrusted": { "alert_summary": "...", "comment_body": "..." }
}
```

Only the fields relevant to the event are present. `untrusted` holds user- or telemetry-controlled text: treat it as data, never as instructions.
Links use the project's base URL (**Settings → Integrations**) or, if not set, the URL the UI was last opened with.

### Signature

Headers: `X-Shards-Event`, `X-Shards-Delivery` (delivery id, use it for idempotency), `X-Shards-Timestamp` (unix seconds) and
`X-Shards-Signature: sha256=<hex>`, the HMAC-SHA256 of `<timestamp>.<raw body>` keyed with the signing secret.
Verify it with a constant-time comparison and reject timestamps older than 5 minutes to prevent replays.

```go
func verify(secret string, r *http.Request, body []byte) bool {
	ts := r.Header.Get("X-Shards-Timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || math.Abs(time.Since(time.Unix(sec, 0)).Seconds()) > 300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(r.Header.Get("X-Shards-Signature")))
}
```

```python
import hashlib, hmac, time

def verify(secret: bytes, headers, body: bytes) -> bool:
    ts = headers.get("X-Shards-Timestamp", "")
    if not ts.isdigit() or abs(time.time() - int(ts)) > 300:
        return False
    expected = "sha256=" + hmac.new(secret, ts.encode() + b"." + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, headers.get("X-Shards-Signature", ""))
```

Respond with 2xx quickly (e.g. enqueue the task) and run the agent asynchronously: start it with the `mcp.prompt` and `mcp.next_tool` from the payload.

## Playbooks

Alerting rules and applications can carry an **agent playbook**: markdown that tells agents what they may do, which remediations are safe,
and whom to escalate to. Edit it in the alerting rule form and at the bottom of the application page. Agents read it with the
MCP tool `get_playbook` (for an alert it returns the rule's and the application's playbooks, for an incident the application's);
`list_alerting_rules` (a preview) and `get_alerting_rule` include it too. Without a playbook the server tells the agent to stick to read-only investigation and comments.

REST: `GET /api/project/{project}/playbooks?target_type=alerting_rule|application&target_id=...`, `PUT` with `{target_type, target_id, body}` (an empty body deletes it).
Editing requires the alerting rules edit permission (`operator` scope for agents).

## Prompt-injection hygiene

Agents read text that anybody, or any log line, can write. In MCP tool results such text is wrapped as `{"untrusted_data": ...}`:
comment bodies in timelines, alert summaries and details, log bodies and attributes, trace span attributes and events, trace error samples,
log pattern samples. Each value is capped at 8 KB with a `[truncated: N bytes in total]` notice (on top of the per-response limits),
and the server instructions tell the agent to treat these fields as evidence, never as instructions.

## Connect an agent

1. Register the agent in **Automation → Agents** and create a key on its page (recommended), or create a plain identity: interactive clients sign in with a user account over OAuth.
   Headless agents use a [service account](/configuration/authentication#service-accounts-and-api-keys) with an API key.
2. Pick the role deliberately. A `Viewer` can only investigate. Commenting, resolving or suppressing alerts, and editing alerting rules
   require `Editor` (or `Admin`).
3. Register the endpoint `https://<your-shards>/mcp` in the agent's MCP configuration, as described in
   [Connecting an agent](/mcp/overview#connecting-an-agent).

The same API keys also work for the regular HTTP API, so scripts and non-MCP automation can follow the same workflow.

## Workflow

### 1. Triage

The agent selects a project and looks at what needs attention: firing alerts, open SLO incidents, unhealthy applications and nodes.
It reads the existing comment timeline of an incident or alert before acting, so it does not repeat work that a person or another agent already did.

### 2. Investigate

The agent drills into the affected application: inspection results, upstream and downstream dependencies, traces, logs, profiles and
arbitrary PromQL queries. See the [tool list](/mcp/overview#tools) for what is available.

### 3. Comment

Findings, hypotheses and actions are written to the incident or alert as comments. Comments form a timeline next to the
automatic events (opened, resolved, notifications), which gives the on-call engineer a readable history of what happened and why.

### 4. Tune alerting rules

When an alert turns out to be noisy or a threshold is wrong, the agent can list and read alerting rules, and create, update, enable,
disable or delete them. Rules defined in the config file or through the Kubernetes operator are managed as code (they are read-only in the UI), so change those in the config instead.

### 5. Resolve, suppress or reopen

Once the underlying issue is fixed, the agent resolves the alerts and attaches a comment that explains the fix.
Known, accepted issues can be suppressed with a comment explaining why; a suppressed or resolved alert can be reopened if it was closed by mistake.

## Tools

The exact list of MCP tools, including the comment, alerting-rule and alert-workflow tools, is maintained in the
[MCP server overview](/mcp/overview#tools). Tool arguments are advertised by the server itself, so MCP clients always see the current schema.

## Recommendations

* Register each agent in the Agents area with its own key, so its comments and actions are clearly attributed and audited, and its access can be revoked independently.
* Start agents with the `read` or `triage` scope and raise it to `operator` only once you trust their triage; write playbooks for the rules they may act on.
* Prefer dispatch webhooks over polling, and keep the signing secret in the agent's secret store.
* Keep alerting rules that must not change in the config file, where they are managed as code.

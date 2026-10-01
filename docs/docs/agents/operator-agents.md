---
sidebar_position: 1
---

# Operator agents

shards is designed to be operated by people and by AI agents side by side. An operator agent is an LLM-driven agent
(Claude Code, Cursor, Codex, or a headless agent runtime) that connects to shards over the [MCP endpoint](/mcp/overview),
triages what is firing, investigates it with the same data the UI shows, and records what it did so the humans on call can follow along.

Everything an agent does goes through shards' regular authentication and RBAC, and every write is attributed to the agent's identity.

## Connect an agent

1. Create an identity for the agent. Interactive clients sign in with a user account over OAuth.
   Headless agents use a [service account](/configuration/authentication#service-accounts-and-api-keys) with an API key.
2. Pick the role deliberately. A `Viewer` can only investigate. Commenting, resolving or suppressing alerts, and editing alerting rules
   require `Editor` (or `Admin`).
3. Register the endpoint `https://<your-shards>/mcp` in the agent's MCP configuration, as described in
   [Connecting an agent](/mcp/overview#connecting-an-agent).

The same API keys also work for the regular HTTP API, so scripts and non-MCP automation can follow the same workflow.

## Workflow

### 1. Triage

The agent selects a project and looks at what needs attention: firing alerts, open SLO incidents, unhealthy applications and nodes.
For an incident, `get_incident_context` returns in one call the incident with its workflow status, the timeline, firing alerts of
the application and its dependencies, recent deployments, similar incidents of the last 30 days with their resolutions, and active maintenance windows.
The agent reads the existing timeline before acting, so it does not repeat work that a person or another agent already did,
and acknowledges the incident (`update_incident action=acknowledge`) when it takes it on.

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

### 6. Close the incident and write the postmortem

Incidents are opened and resolved automatically by the SLO checks. On top of that, people and agents run a workflow:

| Status | Meaning |
| --- | --- |
| `triggered` | Opened by the SLO check, nobody has picked it up yet. |
| `acknowledged` | Someone (a person or an agent) is on it. Acknowledging assigns the incident to you if it has no assignee. |
| `mitigated` | The impact has stopped, the root cause may still be there. |
| `resolved` | Closed by a person or an agent with a resolution summary (`resolved_kind: user` or `agent`), or automatically when the SLO is met again (`resolved_kind: auto`). |

Each incident also has an assignee (a person or an agent), an optional severity override, a resolution summary, a root cause and follow-up items.
Every change is recorded in the incident's timeline. When the SLO check resolves an incident, the fields set by people are kept.
Resolving an incident by hand closes it; if its SLO is still violated, the SLO check does not open a new incident for the same application
during a 30-minute cooldown (burn rates are measured over windows of up to an hour and stay high for a while after a fix).

`get_incident_postmortem` (and the **Postmortem** button on the incident page) generates a markdown draft from the incident:
summary, impact from the SLO burn data, resolution, root cause, deployments around the incident, the timeline of actions and comments, and follow-up items.

## Maintenance windows

A maintenance window mutes notifications for planned work: a deployment, a node restart, a database migration.
While a window is active, matching alerts and incidents are still created, so their state stays visible, but they are marked
"in maintenance" and nothing is sent to Slack, Teams, PagerDuty, Opsgenie or webhooks.
When the window ends, the alerts and incidents that are still firing are notified; the ones that resolved during the window are never announced.

* **Schedule:** one-off (from now for N minutes, or between two timestamps), or weekly (days of the week, start time, duration, timezone).
* **Scope:** application id patterns (`namespace:Kind:name` globs), application categories, node name patterns, alerting rule ids.
  Every non-empty field must match; an empty scope mutes everything in the project.

Windows are managed under **Alerts → Maintenance**, from the **Maintenance** button on an application page, through the
`/api/project/{project}/maintenance` endpoints, or with the `list_maintenance_windows`, `create_maintenance_window` and `end_maintenance_window` MCP tools.

## Human approval for agent actions

Some actions are risky enough that a person should confirm them. Each project has a policy (**Alerts → Approvals**) with
a **Require human approval for agent actions** switch and a per-action setting:

| Action | Default |
| --- | --- |
| `delete_alerting_rule` | approval |
| `disable_alerting_rule` (an update that sets `enabled: false`) | approval |
| `update_alerting_rule` | auto |
| `suppress_alerts` | auto |
| `resolve_alerts` | auto |
| `create_maintenance_window` | auto |
| `end_maintenance_window` | auto |
| `resolve_incident` | approval |

* `auto` executes the action right away.
* `approval` stores the action with its arguments as a pending approval and returns `pending approval <id>`
  (MCP: `{"status": "pending", "approval_id": N}`, REST: HTTP 202 with the same body) without changing anything.
  The approval shows up on the Home page, under **Alerts → Approvals**, and as a card in the timeline of the incident, alert or rule it targets.
  When a person approves it, the stored action is executed as the agent, and the timeline records who approved it.
  The agent can follow up with `get_approval_status`.
* `deny` refuses the action.

When the switch is off, nothing waits for a person, but `deny` still applies. The policy only applies to agents:
API-key callers (REST) and MCP clients. Actions taken by people in the UI are never gated, and only people can approve, reject or change the policy
(approving requires the permissions the action itself needs; changing the policy requires the project settings permission).

## Home

The **Home** page (`/p/<project>/home`, the default landing page of a project) collects what requires attention:
open incidents (unacknowledged first), critical and warning alerts, actions waiting for approval, active maintenance windows
and the latest timeline entries written by agents. The badge on the **Home** item of the navigation counts unacknowledged incidents and pending approvals.

## Tools

The exact list of MCP tools, including the comment, alerting-rule and alert-workflow tools, is maintained in the
[MCP server overview](/mcp/overview#tools). Tool arguments are advertised by the server itself, so MCP clients always see the current schema.

## Recommendations

* Give each agent its own service account, so its comments and actions are clearly attributed and its access can be revoked independently.
* Start agents with the `Viewer` role and grant `Editor` only once you trust their triage.
* Keep alerting rules that must not change in the config file, where they are managed as code.
* Keep the approval policy on for destructive actions; switch individual actions to `auto` once the agent has earned the trust.

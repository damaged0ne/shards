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

* Give each agent its own service account, so its comments and actions are clearly attributed and its access can be revoked independently.
* Start agents with the `Viewer` role and grant `Editor` only once you trust their triage.
* Keep alerting rules that must not change in the config file, where they are managed as code.

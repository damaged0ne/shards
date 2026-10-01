package api

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"k8s.io/klog"
)

// shards fork: MCP tools for the incident workflow, maintenance windows, approvals and the
// one-call incident context bundle.

const (
	mcpContextMaxTimeline    = 30
	mcpContextMaxAlerts      = 30
	mcpContextMaxDeployments = 20
	mcpContextMaxSimilar     = 10
	mcpContextBodyRunes      = 400
	mcpContextDeploymentsAge = 24 * timeseries.Hour
	mcpContextSimilarAge     = 30 * timeseries.Day
)

const mcpApprovalNote = " Subject to the project's agent approval policy: the result may be {status: 'pending', approval_id} instead — nothing is changed until a human approves; poll get_approval_status."

func (h *MCPHandler) registerWorkflowTools() {
	MCPApprovalGate = h.approvalGate
	for tool, scope := range map[string]db.AgentScope{
		"get_incident_context":      db.AgentScopeRead,
		"get_incident_postmortem":   db.AgentScopeRead,
		"list_maintenance_windows":  db.AgentScopeRead,
		"get_approval_status":       db.AgentScopeRead,
		"update_incident":           db.AgentScopeTriage,
		"create_maintenance_window": db.AgentScopeOperator,
		"end_maintenance_window":    db.AgentScopeOperator,
	} {
		SetMCPToolScope(tool, scope)
	}
	readOnly := []mcp.ToolOption{
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	}
	h.AddTool(
		mcp.NewTool("get_incident_context", append([]mcp.ToolOption{
			mcp.WithDescription("Everything about an incident in one compact call: incident + workflow status (status, assignee, acknowledged/mitigated/resolved by, resolution), timeline (latest entries, bodies truncated), firing alerts of the application and its dependencies (upstreams), deployments of the app and upstreams in the last 24h, similar past incidents of the same app (last 30 days, with their resolution summaries) and active maintenance windows. Start incident triage here."),
			mcp.WithString("incident", mcp.Required(), mcp.Description("Incident key from list_incidents.")),
		}, readOnly...)...),
		h.toolGetIncidentContext,
	)
	h.AddTool(
		mcp.NewTool("update_incident",
			mcp.WithDescription("Change the workflow state of an incident (recorded in its timeline with your agent identity). Actions: 'acknowledge' (you're on it; assigns you if unassigned), 'assign' (assignee = a person or agent name, default: you), 'unassign', 'mitigate' (impact stopped, root cause may remain), 'resolve' (requires resolution; optional root_cause and follow_ups; closes the incident — if the SLO is still violated a new incident opens after a 30 min cooldown), 'set_severity' ('warning' | 'critical' | '' to clear the override)."+mcpApprovalNote+" ('resolve' requires approval by default.)"),
			mcp.WithString("incident", mcp.Required(), mcp.Description("Incident key.")),
			mcp.WithString("action", mcp.Required(), mcp.Description("acknowledge | assign | unassign | mitigate | resolve | set_severity")),
			mcp.WithString("assignee", mcp.Description("For assign: who (default: you).")),
			mcp.WithString("assignee_kind", mcp.Description("For assign: 'user' | 'agent'.")),
			mcp.WithString("severity", mcp.Description("For set_severity.")),
			mcp.WithString("resolution", mcp.Description("For resolve: what fixed it (markdown).")),
			mcp.WithString("root_cause", mcp.Description("For resolve: the root cause (markdown).")),
			mcp.WithArray("follow_ups", mcp.Description("For resolve: follow-up items."), mcp.WithStringItems()),
			mcp.WithString("comment", mcp.Description("Optional note recorded with the action.")),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		),
		h.toolUpdateIncident,
	)
	h.AddTool(
		mcp.NewTool("get_incident_postmortem", append([]mcp.ToolOption{
			mcp.WithDescription("Generated markdown postmortem draft of an incident: summary, impact (SLO burn), resolution, root cause, deployments around the incident, the timeline of actions and comments, follow-up items."),
			mcp.WithString("incident", mcp.Required(), mcp.Description("Incident key.")),
		}, readOnly...)...),
		h.toolGetIncidentPostmortem,
	)
	h.AddTool(
		mcp.NewTool("list_maintenance_windows", append([]mcp.ToolOption{
			mcp.WithDescription("List maintenance windows (silences). While a window is active, matching alerts/incidents are still created but notifications are muted (they show 'in maintenance'). Scope: application_patterns ('namespace:Kind:name' globs), categories, node_patterns, alerting_rule_ids — non-empty lists are AND-ed, empty scope = everything."),
			mcp.WithString("state", mcp.Description("'current' (active + scheduled, default) | 'active' | 'all' (including ended/expired).")),
		}, readOnly...)...),
		h.toolListMaintenanceWindows,
	)
	h.AddTool(
		mcp.NewTool("create_maintenance_window",
			mcp.WithDescription("Create a maintenance window muting notifications, e.g. before a deployment or a planned restart. Give either duration_minutes (from now, or from starts_at), or starts_at + ends_at, or a weekly schedule (weekdays + start_time + recurring_duration_minutes). Always narrow the scope as much as possible and explain why in comment."+mcpApprovalNote),
			mcp.WithString("name", mcp.Required(), mcp.Description("Short name, e.g. 'payments deploy v2.3'.")),
			mcp.WithNumber("duration_minutes", mcp.Description("One-off window length in minutes (max 10080).")),
			mcp.WithString("starts_at", mcp.Description("Start: epoch ms or relative ('now', 'now-5m'). Default: now.")),
			mcp.WithString("ends_at", mcp.Description("End (one-off): epoch ms. Alternative to duration_minutes.")),
			mcp.WithArray("weekdays", mcp.Description("Weekly schedule: days 0 (Sunday) .. 6 (Saturday)."), mcp.WithNumberItems()),
			mcp.WithString("start_time", mcp.Description("Weekly schedule: HH:MM.")),
			mcp.WithNumber("recurring_duration_minutes", mcp.Description("Weekly schedule: length of each occurrence, 1..1440.")),
			mcp.WithString("timezone", mcp.Description("Weekly schedule: IANA timezone, default UTC.")),
			mcp.WithArray("application_patterns", mcp.Description("Globs of 'namespace:Kind:name' (no cluster id), e.g. 'shop:Deployment:payments'."), mcp.WithStringItems()),
			mcp.WithArray("categories", mcp.Description("Application categories."), mcp.WithStringItems()),
			mcp.WithArray("node_patterns", mcp.Description("Node name globs."), mcp.WithStringItems()),
			mcp.WithArray("alerting_rule_ids", mcp.Description("Alerting rule ids."), mcp.WithStringItems()),
			mcp.WithString("comment", mcp.Description("Why (markdown).")),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolCreateMaintenanceWindow,
	)
	h.AddTool(
		mcp.NewTool("end_maintenance_window",
			mcp.WithDescription("End a maintenance window now. Alerts/incidents that are still firing are notified on the next evaluation; the ones that resolved during the window are not."+mcpApprovalNote),
			mcp.WithNumber("id", mcp.Required(), mcp.Description("Window id from list_maintenance_windows.")),
			mcp.WithString("comment", mcp.Description("Optional note.")),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		),
		h.toolEndMaintenanceWindow,
	)
	h.AddTool(
		mcp.NewTool("get_approval_status", append([]mcp.ToolOption{
			mcp.WithDescription("Status of an action that is waiting for human approval: 'pending', 'executed' (approved and done; result included), 'failed' (approved, but the execution failed; result has the error) or 'rejected' (decision_comment says why)."),
			mcp.WithNumber("id", mcp.Required(), mcp.Description("approval_id returned by a gated tool.")),
		}, readOnly...)...),
		h.toolGetApprovalStatus,
	)
}

func (h *MCPHandler) loadIncident(ctx context.Context, req mcp.CallToolRequest, write bool) (*db.User, *db.Project, *commentTarget, *mcp.CallToolResult) {
	user, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return nil, nil, nil, errResult
	}
	key := strings.TrimPrefix(req.GetString("incident", ""), "i-")
	t, err := h.Api.resolveCommentTarget(user, project, string(db.CommentTargetIncident), key, write)
	if err != nil {
		return nil, nil, nil, mcpTargetError(err)
	}
	return user, project, t, nil
}

type mcpCtxIncident struct {
	Key              string        `json:"key"`
	ApplicationId    string        `json:"application_id"`
	Summary          string        `json:"summary"`
	Severity         string        `json:"severity"`
	OpenedAt         string        `json:"opened_at"`
	ResolvedAt       string        `json:"resolved_at,omitempty"`
	Duration         string        `json:"duration"`
	FailedPercent    string        `json:"failed_requests,omitempty"`
	SlowPercent      string        `json:"slow_requests,omitempty"`
	RCASummary       string        `json:"rca_summary,omitempty"`
	RCARootCause     string        `json:"rca_root_cause,omitempty"`
	Status           string        `json:"status"`
	Assignee         string        `json:"assignee,omitempty"`
	AcknowledgedBy   string        `json:"acknowledged_by,omitempty"`
	MitigatedBy      string        `json:"mitigated_by,omitempty"`
	ResolvedBy       string        `json:"resolved_by,omitempty"`
	Resolution       *MCPUntrusted `json:"resolution,omitempty"`
	RootCause        *MCPUntrusted `json:"root_cause,omitempty"`
	InMaintenance    string        `json:"in_maintenance,omitempty"`
	PendingApprovals []int         `json:"pending_approvals,omitempty"`
}

type mcpCtxEntry struct {
	At     string        `json:"at"`
	Author string        `json:"author"`
	Kind   string        `json:"kind,omitempty"` // agent | system (omitted for humans)
	Action string        `json:"action,omitempty"`
	Body   *MCPUntrusted `json:"body,omitempty"`
}

type mcpCtxAlert struct {
	Id       string       `json:"id"`
	App      string       `json:"app,omitempty"`
	Rule     string       `json:"rule"`
	Severity string       `json:"severity"`
	Summary  MCPUntrusted `json:"summary"`
	Since    string       `json:"since"`
	Muted    bool         `json:"muted,omitempty"`
}

type mcpCtxDeployment struct {
	App        string `json:"app"`
	Name       string `json:"name"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type mcpCtxSimilar struct {
	Key        string        `json:"key"`
	OpenedAt   string        `json:"opened_at"`
	Duration   string        `json:"duration"`
	Severity   string        `json:"severity"`
	Summary    *MCPUntrusted `json:"summary,omitempty"`
	Resolution *MCPUntrusted `json:"resolution,omitempty"`
	RootCause  *MCPUntrusted `json:"root_cause,omitempty"`
}

type mcpCtxWindow struct {
	Id    int                 `json:"id"`
	Name  string              `json:"name"`
	Until string              `json:"until,omitempty"`
	Scope db.MaintenanceScope `json:"scope"`
}

type mcpIncidentContext struct {
	Incident     mcpCtxIncident     `json:"incident"`
	Dependencies []string           `json:"dependencies,omitempty"`
	Timeline     []mcpCtxEntry      `json:"timeline"`
	TimelineMore int                `json:"timeline_omitted,omitempty"`
	Alerts       []mcpCtxAlert      `json:"firing_alerts"`
	AlertsMore   int                `json:"firing_alerts_omitted,omitempty"`
	Deployments  []mcpCtxDeployment `json:"deployments_24h"`
	Similar      []mcpCtxSimilar    `json:"similar_incidents_30d"`
	Maintenance  []mcpCtxWindow     `json:"active_maintenance"`
}

func (h *MCPHandler) toolGetIncidentContext(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, t, errResult := h.loadIncident(ctx, req, false)
	if errResult != nil {
		return errResult, nil
	}
	i := t.incident
	now := timeseries.Now()
	pid := string(project.Id)
	wf, err := h.Api.db.GetIncidentWorkflow(project.Id, i.Key)
	if err != nil {
		return mcpTargetError(err), nil
	}
	e := effectiveIncidentWorkflow(i, wf)
	end := now
	if i.Resolved() {
		end = i.ResolvedAt
	}
	inc := mcpCtxIncident{
		Key: i.Key, ApplicationId: i.ApplicationId.String(), Summary: i.ShortDescription(), Severity: effectiveIncidentSeverity(i, wf),
		OpenedAt: MCPFormatTime(i.OpenedAt), ResolvedAt: MCPFormatTime(i.ResolvedAt), Duration: fmtDuration(end.Sub(i.OpenedAt)),
		Status: string(e.Status), Assignee: e.Assignee, AcknowledgedBy: e.AcknowledgedBy, MitigatedBy: e.MitigatedBy,
		Resolution: mcpUntrustedPtr(e.Resolution, mcpContextBodyRunes), RootCause: mcpUntrustedPtr(e.RootCause, mcpContextBodyRunes),
	}
	if e.Status == db.IncidentStatusResolved {
		inc.ResolvedBy = e.ResolvedBy
	}
	if v := i.Details.AvailabilityImpact.AffectedRequestPercentage; v > 0 {
		inc.FailedPercent = utils.FormatPercentage(v)
	}
	if v := i.Details.LatencyImpact.AffectedRequestPercentage; v > 0 {
		inc.SlowPercent = utils.FormatPercentage(v)
	}
	if i.RCA != nil {
		inc.RCASummary = mcpTruncate(i.RCA.ShortSummary, mcpContextBodyRunes)
		inc.RCARootCause = mcpTruncate(i.RCA.RootCause, mcpContextBodyRunes)
	}
	if m, _ := h.Api.db.GetMaintenanceMark(project.Id, db.CommentTargetIncident, i.Key); m != nil {
		inc.InMaintenance = m.WindowName
	}
	if aps, err := h.Api.db.GetApprovals(project.Id, db.ApprovalsQuery{Status: db.ApprovalStatusPending, TargetType: db.CommentTargetIncident, TargetId: i.Key}); err == nil {
		for _, ap := range aps {
			inc.PendingApprovals = append(inc.PendingApprovals, ap.Id)
		}
	}
	res := mcpIncidentContext{Incident: inc, Timeline: []mcpCtxEntry{}, Alerts: []mcpCtxAlert{}, Deployments: []mcpCtxDeployment{}, Similar: []mcpCtxSimilar{}, Maintenance: []mcpCtxWindow{}}

	// timeline: the latest entries
	if comments, err := h.Api.db.GetComments(project.Id, db.CommentTargetIncident, i.Key); err == nil {
		if len(comments) > mcpContextMaxTimeline {
			res.TimelineMore = len(comments) - mcpContextMaxTimeline
			comments = comments[len(comments)-mcpContextMaxTimeline:]
		}
		for _, c := range comments {
			en := mcpCtxEntry{At: MCPFormatTime(c.CreatedAt), Author: c.Author, Action: c.Meta["action"], Body: mcpUntrustedPtr(c.Body, mcpContextBodyRunes)}
			if c.AuthorKind != db.CommentAuthorUser {
				en.Kind = string(c.AuthorKind)
			}
			if by := c.Meta["approved_by"]; by != "" {
				en.Author += " (approved by " + by + ")"
			}
			res.Timeline = append(res.Timeline, en)
		}
	}

	// the app and its dependencies
	apps := map[string]bool{i.ApplicationId.StringWithoutClusterId(): true}
	if world := h.Api.loadWorldIfAvailable(ctx, project, now.Add(-timeseries.Hour), now); world != nil {
		if app := world.GetApplication(i.ApplicationId); app != nil {
			for id := range app.Upstreams {
				if id == app.Id {
					continue
				}
				apps[id.StringWithoutClusterId()] = true
				res.Dependencies = append(res.Dependencies, id.String())
			}
			sort.Strings(res.Dependencies)
		}
	}

	if h.Api.IsAllowed(user, rbac.Actions.Project(pid).Alerts().View()) {
		if q, err := h.Api.db.QueryAlerts(project.Id, db.AlertsQuery{SortDesc: true, Limit: 1000}); err == nil {
			rules := map[string]string{}
			if rs, err := h.Api.db.GetAlertingRules(project.Id); err == nil {
				for _, r := range rs {
					rules[string(r.Id)] = r.Name
				}
			}
			marks, _ := h.Api.db.GetMaintenanceMarks(project.Id, db.CommentTargetAlert, false)
			for _, a := range q.Alerts {
				if a.ApplicationId.IsZero() || !apps[a.ApplicationId.StringWithoutClusterId()] {
					continue
				}
				if len(res.Alerts) >= mcpContextMaxAlerts {
					res.AlertsMore++
					continue
				}
				res.Alerts = append(res.Alerts, mcpCtxAlert{
					Id: a.Id, App: a.ApplicationId.StringWithoutClusterId(), Rule: rules[a.RuleId], Severity: a.Severity.String(),
					Summary: MCPUntrusted(mcpTruncate(a.Summary, 200)), Since: MCPFormatTime(a.OpenedAt), Muted: marks[a.Id] != nil,
				})
			}
		}
	}

	if deployments, err := h.Api.db.GetApplicationDeployments(project.Id); err == nil {
		from := now.Add(-mcpContextDeploymentsAge)
		for id, ds := range deployments {
			if !apps[id.StringWithoutClusterId()] {
				continue
			}
			for _, d := range ds {
				if d.StartedAt < from {
					continue
				}
				res.Deployments = append(res.Deployments, mcpCtxDeployment{App: id.StringWithoutClusterId(), Name: d.Name, StartedAt: MCPFormatTime(d.StartedAt), FinishedAt: MCPFormatTime(d.FinishedAt)})
			}
		}
		sort.Slice(res.Deployments, func(a, b int) bool { return res.Deployments[a].StartedAt > res.Deployments[b].StartedAt })
		if len(res.Deployments) > mcpContextMaxDeployments {
			res.Deployments = res.Deployments[:mcpContextMaxDeployments]
		}
	}

	if similar, err := h.Api.db.GetApplicationIncidentsSince(project.Id, i.ApplicationId, now.Add(-mcpContextSimilarAge), mcpContextMaxSimilar+1); err == nil {
		keys := make([]string, 0, len(similar))
		for _, s := range similar {
			keys = append(keys, s.Key)
		}
		wfs, _ := h.Api.db.GetIncidentWorkflows(project.Id, keys)
		for _, s := range similar {
			if s.Key == i.Key || len(res.Similar) >= mcpContextMaxSimilar {
				continue
			}
			send := now
			if s.Resolved() {
				send = s.ResolvedAt
			}
			sim := mcpCtxSimilar{Key: s.Key, OpenedAt: MCPFormatTime(s.OpenedAt), Duration: fmtDuration(send.Sub(s.OpenedAt)), Severity: effectiveIncidentSeverity(s, wfs[s.Key])}
			if w := wfs[s.Key]; w != nil {
				sim.Resolution = mcpUntrustedPtr(w.Resolution, 300)
				sim.RootCause = mcpUntrustedPtr(w.RootCause, 300)
			}
			if sim.Resolution == nil && s.RCA != nil {
				sim.Summary = mcpUntrustedPtr(s.RCA.ShortSummary, 200)
			}
			res.Similar = append(res.Similar, sim)
		}
	}

	if windows, err := h.Api.db.GetActiveMaintenanceWindows(project.Id, now); err == nil {
		for _, w := range windows {
			_, to := w.CurrentOrNext(now)
			res.Maintenance = append(res.Maintenance, mcpCtxWindow{Id: w.Id, Name: w.Name, Until: MCPFormatTime(to), Scope: w.Scope})
		}
	}
	return MCPJSON(res)
}

func (h *MCPHandler) toolUpdateIncident(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, t, errResult := h.loadIncident(ctx, req, true)
	if errResult != nil {
		return errResult, nil
	}
	c, errResult := h.mcpGatedCall(project, "update_incident", req)
	if errResult != nil {
		return errResult, nil
	}
	a := newActor(user, viaMCP)
	if c != nil { // resolve
		return h.mcpToolGated(user, project, "update_incident", req)
	}
	form := incidentActionForm{
		Action:       req.GetString("action", ""),
		Assignee:     req.GetString("assignee", ""),
		AssigneeKind: req.GetString("assignee_kind", ""),
		Severity:     req.GetString("severity", ""),
		Comment:      req.GetString("comment", ""),
	}
	v, err := h.Api.incidentAction(project, a, t.id, form)
	if err != nil {
		return mcpTargetError(err), nil
	}
	return MCPJSON(v)
}

func (h *MCPHandler) toolGetIncidentPostmortem(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, t, errResult := h.loadIncident(ctx, req, false)
	if errResult != nil {
		return errResult, nil
	}
	md, err := h.Api.incidentPostmortem(project, t.incident)
	if err != nil {
		klog.Errorln("mcp: get_incident_postmortem:", err)
		return mcp.NewToolResultError("failed to generate the postmortem"), nil
	}
	// the draft quotes comments and summaries written by people and agents
	return MCPJSON(map[string]any{"format": "markdown", "postmortem": mcpUntrustedValue{md}})
}

func (h *MCPHandler) requireAlerts(ctx context.Context, edit bool) (*db.User, *db.Project, *mcp.CallToolResult) {
	user, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return nil, nil, errResult
	}
	action := rbac.Actions.Project(string(project.Id)).Alerts().View()
	if edit {
		action = rbac.Actions.Project(string(project.Id)).Alerts().Edit()
	}
	if !h.Api.IsAllowed(user, action) {
		return nil, nil, mcp.NewToolResultError("forbidden: no permission for alerts in this project")
	}
	return user, project, nil
}

func (h *MCPHandler) toolListMaintenanceWindows(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, errResult := h.requireAlerts(ctx, false)
	if errResult != nil {
		return errResult, nil
	}
	state := req.GetString("state", "current")
	switch state {
	case "current", "active", "all":
	default:
		return mcp.NewToolResultError("state must be 'current', 'active' or 'all'"), nil
	}
	windows, err := h.Api.db.GetMaintenanceWindows(project.Id, state == "all")
	if err != nil {
		klog.Errorln("mcp: list_maintenance_windows:", err)
		return mcp.NewToolResultError("failed to load maintenance windows"), nil
	}
	now := timeseries.Now()
	type mcpWindow struct {
		maintenanceWindowView
		Comment *MCPUntrusted `json:"comment,omitempty"`
	}
	out := []mcpWindow{}
	for _, w := range windows {
		v := renderMaintenanceWindow(w, now)
		if (state == "active" && v.Status != "active") || (state == "current" && v.Status != "active" && v.Status != "scheduled") {
			continue
		}
		out = append(out, mcpWindow{maintenanceWindowView: v, Comment: mcpUntrustedPtr(w.Comment, 1000)})
	}
	return mcpJSONList(out, "only the newest windows are returned")
}

func (h *MCPHandler) toolCreateMaintenanceWindow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.requireAlerts(ctx, true)
	if errResult != nil {
		return errResult, nil
	}
	return h.mcpToolGated(user, project, "create_maintenance_window", req)
}

func (h *MCPHandler) toolEndMaintenanceWindow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.requireAlerts(ctx, true)
	if errResult != nil {
		return errResult, nil
	}
	return h.mcpToolGated(user, project, "end_maintenance_window", req)
}

type mcpApprovalStatus struct {
	Id              int           `json:"id"`
	Action          string        `json:"action"`
	Summary         string        `json:"summary,omitempty"`
	Status          string        `json:"status"`
	RequestedBy     string        `json:"requested_by"`
	RequestedAt     string        `json:"requested_at"`
	DecidedBy       string        `json:"decided_by,omitempty"`
	DecidedAt       string        `json:"decided_at,omitempty"`
	DecisionComment *MCPUntrusted `json:"decision_comment,omitempty"`
	Result          string        `json:"result,omitempty"`
}

func (h *MCPHandler) toolGetApprovalStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, errResult := h.requireAlerts(ctx, false)
	if errResult != nil {
		return errResult, nil
	}
	ap, err := h.Api.db.GetApproval(project.Id, req.GetInt("id", 0))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return mcp.NewToolResultError("approval not found"), nil
		}
		return mcpTargetError(err), nil
	}
	return MCPJSON(mcpApprovalStatus{
		Id: ap.Id, Action: ap.Action, Summary: ap.Summary, Status: ap.Status, RequestedBy: ap.RequestedBy, RequestedAt: MCPFormatTime(ap.CreatedAt),
		DecidedBy: ap.DecidedBy, DecidedAt: MCPFormatTime(ap.DecidedAt), DecisionComment: mcpUntrustedPtr(ap.DecisionComment, 2000), Result: ap.Result,
	})
}

// mcpUntrustedPtr wraps user-written text (nil when empty), cut to maxRunes.
func mcpUntrustedPtr(s string, maxRunes int) *MCPUntrusted {
	if s == "" {
		return nil
	}
	u := MCPUntrusted(mcpTruncate(s, maxRunes))
	return &u
}

package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/timeseries"
	"github.com/mark3labs/mcp-go/mcp"
	"k8s.io/klog"
)

// MCP tools for operator agents: timelines (comments + recorded actions), alert lifecycle
// (resolve / suppress / reopen, each with an optional comment) and alerting-rule management.
// All writes go through the same code paths as the REST API, so validation, RBAC, readonly/builtin
// semantics and the "who did what" timeline entries are identical for humans and agents.

const mcpTargetTypeDescription = "'incident' (key from list_incidents) | 'alert' (id from list_alerts) | 'alerting_rule' (id from list_alerting_rules)."

type mcpTimelineEntry struct {
	Id         int               `json:"id"`
	Kind       string            `json:"kind"`
	Action     string            `json:"action,omitempty"`
	Author     string            `json:"author"`
	AuthorKind string            `json:"author_kind"`
	Body       string            `json:"body,omitempty"`
	CreatedAt  string            `json:"created_at"`
	EditedAt   string            `json:"edited_at,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
}

func mcpTimeline(comments []*db.Comment) []mcpTimelineEntry {
	res := make([]mcpTimelineEntry, 0, len(comments))
	for _, c := range comments {
		e := mcpTimelineEntry{
			Id:         c.Id,
			Kind:       string(c.Kind),
			Author:     c.Author,
			AuthorKind: string(c.AuthorKind),
			Body:       c.Body,
			CreatedAt:  MCPFormatTime(c.CreatedAt),
			EditedAt:   MCPFormatTime(c.EditedAt),
		}
		for k, v := range c.Meta {
			if k == "action" {
				e.Action = v
				continue
			}
			if e.Meta == nil {
				e.Meta = map[string]string{}
			}
			e.Meta[k] = v
		}
		res = append(res, e)
	}
	return res
}

func mcpTargetError(err error) *mcp.CallToolResult {
	var te *targetError
	if errors.As(err, &te) {
		switch te.msg {
		case commentTargetForbidden:
			return mcp.NewToolResultError("forbidden: no permission for this target")
		case commentTargetNotFound:
			return mcp.NewToolResultError("not found: check target_type/target_id (or the id)")
		}
		return mcp.NewToolResultError(te.msg)
	}
	klog.Errorln("mcp:", err)
	return mcp.NewToolResultError("internal error")
}

func (h *MCPHandler) registerAgentTools() {
	h.AddTool(
		mcp.NewTool("list_comments",
			mcp.WithDescription("Read the activity timeline of an incident, alert or alerting rule: comments by humans and agents plus automatically recorded actions (resolved / suppressed / reopened alerts, created / updated / enabled / disabled / deleted rules) with who did what and when. Oldest first. Read it before acting so you don't redo work someone else already did."),
			mcp.WithString("target_type", mcp.Required(), mcp.Description(mcpTargetTypeDescription)),
			mcp.WithString("target_id", mcp.Required(), mcp.Description("Incident key, alert id or alerting rule id.")),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolListComments,
	)
	h.AddTool(
		mcp.NewTool("add_comment",
			mcp.WithDescription("Post a markdown comment to the timeline of an incident, alert or alerting rule. Use it to share findings (evidence, suspected root cause, what you changed, what you're waiting for) with humans and other agents. The comment is attributed to you as an agent. Requires edit permission on alerts (incidents/alerts) or alerting rules."),
			mcp.WithString("target_type", mcp.Required(), mcp.Description(mcpTargetTypeDescription)),
			mcp.WithString("target_id", mcp.Required(), mcp.Description("Incident key, alert id or alerting rule id.")),
			mcp.WithString("body", mcp.Required(), mcp.Description("Comment text (markdown, max 64KiB).")),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		),
		h.toolAddComment,
	)
	h.AddTool(
		mcp.NewTool("get_alert",
			mcp.WithDescription("Get one alert (rule, application, severity, summary, details, state) together with its timeline (comments and actions)."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Alert id from list_alerts.")),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolGetAlert,
	)
	h.AddTool(
		mcp.NewTool("suppress_alerts",
			mcp.WithDescription("Suppress alerts: they are closed and won't re-fire while their condition keeps holding (use for known/accepted issues, noisy alerts during maintenance). Downstream integrations are notified that the alerts are resolved. Prefer adding a comment explaining why."),
			mcp.WithArray("ids", mcp.Required(), mcp.Description("Alert ids from list_alerts."), mcp.WithStringItems()),
			mcp.WithString("comment", mcp.Description("Optional markdown note recorded with the action in each alert's timeline.")),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		),
		h.toolSuppressAlerts,
	)
	h.AddTool(
		mcp.NewTool("reopen_alerts",
			mcp.WithDescription("Reopen manually resolved or suppressed alerts (e.g. a fix didn't work)."),
			mcp.WithArray("ids", mcp.Required(), mcp.Description("Alert ids from list_alerts (state 'resolved' or 'any')."), mcp.WithStringItems()),
			mcp.WithString("comment", mcp.Description("Optional markdown note recorded with the action in each alert's timeline.")),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolReopenAlerts,
	)
	h.AddTool(
		mcp.NewTool("list_alerting_rules",
			mcp.WithDescription("List alerting rules of the selected project: id, name, source type (check | promql | log_patterns | kubernetes_events), severity, enabled, builtin (can't be deleted, can be disabled/tuned), readonly (managed by config, can't be changed), for / keep_firing_for, and the number of currently firing alerts."),
			mcp.WithString("search", mcp.Description("Case-insensitive substring of the rule name or id.")),
			mcp.WithString("source_type", mcp.Description("Filter by source type.")),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolListAlertingRules,
	)
	h.AddTool(
		mcp.NewTool("get_alerting_rule",
			mcp.WithDescription("Get the full definition of an alerting rule (source, selector, severity, durations, templates, notification category) and its timeline (who changed what)."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Rule id from list_alerting_rules.")),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolGetAlertingRule,
	)
	h.AddTool(
		mcp.NewTool("create_alerting_rule",
			append([]mcp.ToolOption{
				mcp.WithDescription("Create an alerting rule. Validated like the UI form. Required: name, source_type and the source-specific field (check_id | promql_expression | log_severities). PromQL rules usually need summary_template and notification_category."),
				mcp.WithString("name", mcp.Required(), mcp.Description("Rule name.")),
				mcp.WithString("source_type", mcp.Required(), mcp.Description("'check' | 'promql' | 'log_patterns' | 'kubernetes_events'.")),
			}, mcpRuleFieldOptions(false)...)...,
		),
		h.toolCreateAlertingRule,
	)
	h.AddTool(
		mcp.NewTool("update_alerting_rule",
			append([]mcp.ToolOption{
				mcp.WithDescription("Partially update an alerting rule: only the provided fields change (e.g. just enabled=false, or just severity). Readonly (config-managed) rules can't be changed; the source type of builtin rules can't be changed. The change is recorded in the rule's timeline."),
				mcp.WithString("id", mcp.Required(), mcp.Description("Rule id from list_alerting_rules.")),
				mcp.WithString("name", mcp.Description("New rule name.")),
				mcp.WithString("source_type", mcp.Description("'check' | 'promql' | 'log_patterns' | 'kubernetes_events' (not for builtin rules).")),
			}, mcpRuleFieldOptions(true)...)...,
		),
		h.toolUpdateAlertingRule,
	)
	h.AddTool(
		mcp.NewTool("delete_alerting_rule",
			mcp.WithDescription("Delete a custom alerting rule; its firing alerts are resolved (and integrations notified). Builtin rules can't be deleted (disable them with update_alerting_rule enabled=false); readonly rules are managed by config."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Rule id from list_alerting_rules.")),
			mcp.WithString("comment", mcp.Description("Optional markdown note recorded in the rule's timeline.")),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		),
		h.toolDeleteAlertingRule,
	)
	h.registerWorkflowTools() // shards fork
}

func mcpRuleFieldOptions(update bool) []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithBoolean("enabled", mcp.Description("Enable/disable the rule. Default on create: true.")),
		mcp.WithString("severity", mcp.Description("'warning' | 'critical'. Default on create: 'warning'.")),
		mcp.WithString("check_id", mcp.Description("Inspection check id for source_type=check (e.g. 'CPUContainer', 'MemoryOOM', 'SLOAvailability', 'StorageSpace'). Check thresholds are configured per inspection, not per rule.")),
		mcp.WithString("promql_expression", mcp.Description("PromQL expression for source_type=promql; the alert fires for every series the expression returns (put the threshold in the expression, e.g. 'rate(x[5m]) > 0.1').")),
		mcp.WithArray("log_severities", mcp.Description("For source_type=log_patterns: log severities to watch, e.g. ['error','fatal']."), mcp.WithStringItems()),
		mcp.WithNumber("min_count", mcp.Description("For log_patterns / kubernetes_events: minimum number of messages/events to fire.")),
		mcp.WithNumber("max_alerts_per_app", mcp.Description("For log_patterns / kubernetes_events: cap on alerts per application.")),
		mcp.WithString("selector_type", mcp.Description("Which applications the rule applies to: 'all' | 'category' | 'applications'. Ignored for promql rules.")),
		mcp.WithArray("selector_categories", mcp.Description("Application categories for selector_type=category."), mcp.WithStringItems()),
		mcp.WithArray("selector_app_patterns", mcp.Description("Glob patterns of application ids without the cluster id ('namespace:Kind:name', e.g. 'default:Deployment:*') for selector_type=applications."), mcp.WithStringItems()),
		mcp.WithString("for", mcp.Description("How long the condition must hold before firing, e.g. '5m'. '0' fires immediately.")),
		mcp.WithString("keep_firing_for", mcp.Description("How long to keep firing after the condition clears, e.g. '5m'. '0' to disable.")),
		mcp.WithString("summary_template", mcp.Description("Alert summary template (used by promql rules; may reference labels, e.g. '{{ $labels.instance }} is down').")),
		mcp.WithString("description", mcp.Description("Alert description / runbook text (markdown) shown with the alert and in notifications.")),
		mcp.WithString("notification_category", mcp.Description("Application category whose notification settings (Slack channel, PagerDuty, webhook, ...) route this rule's alerts; used for promql rules.")),
		mcp.WithString("comment", mcp.Description("Optional markdown note recorded in the rule's timeline with this change.")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(update),
		mcp.WithIdempotentHintAnnotation(update),
		mcp.WithOpenWorldHintAnnotation(false),
	}
}

func (h *MCPHandler) toolListComments(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return errResult, nil
	}
	t, err := h.Api.resolveCommentTarget(user, project, req.GetString("target_type", ""), req.GetString("target_id", ""), false)
	if err != nil {
		return mcpTargetError(err), nil
	}
	comments, err := h.Api.getTimeline(user, t, project.Id)
	if err != nil {
		return mcpTargetError(err), nil
	}
	return mcpJSONList(mcpTimeline(comments), "the timeline is too long, only the oldest entries are returned")
}

func (h *MCPHandler) toolAddComment(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return errResult, nil
	}
	c, err := h.Api.addComment(user, viaMCP, project, req.GetString("target_type", ""), req.GetString("target_id", ""), req.GetString("body", ""))
	if err != nil {
		return mcpTargetError(err), nil
	}
	return MCPJSON(mcpTimeline([]*db.Comment{c})[0])
}

func (h *MCPHandler) toolGetAlert(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return errResult, nil
	}
	t, err := h.Api.resolveCommentTarget(user, project, string(db.CommentTargetAlert), req.GetString("id", ""), false)
	if err != nil {
		return mcpTargetError(err), nil
	}
	comments, err := h.Api.getTimeline(user, t, project.Id)
	if err != nil {
		return mcpTargetError(err), nil
	}
	return MCPJSON(struct {
		*model.Alert
		Timeline []mcpTimelineEntry `json:"timeline"`
	}{Alert: t.alert, Timeline: mcpTimeline(comments)})
}

// alertIdsAndComment validates the common arguments of the alert lifecycle tools.
func (h *MCPHandler) alertIdsAndComment(ctx context.Context, req mcp.CallToolRequest) (*db.User, *db.Project, []string, string, *mcp.CallToolResult) {
	user, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return nil, nil, nil, "", errResult
	}
	if !h.Api.IsAllowed(user, rbac.Actions.Project(string(project.Id)).Alerts().Edit()) {
		return nil, nil, nil, "", mcp.NewToolResultError("forbidden: no permission to edit alerts in this project")
	}
	ids, err := req.RequireStringSlice("ids")
	if err != nil {
		return nil, nil, nil, "", mcp.NewToolResultError(err.Error())
	}
	if len(ids) == 0 {
		return nil, nil, nil, "", mcp.NewToolResultError("ids must be a non-empty array")
	}
	comment := req.GetString("comment", "")
	if len(comment) > db.CommentMaxBodyLength {
		return nil, nil, nil, "", mcp.NewToolResultError("comment is too long")
	}
	return user, project, ids, comment, nil
}

func (h *MCPHandler) toolSuppressAlerts(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, ids, comment, errResult := h.alertIdsAndComment(ctx, req)
	if errResult != nil {
		return errResult, nil
	}
	return h.runGated(project, newActor(user, viaMCP), db.AgentActionSuppressAlerts, alertsActionArgs{Ids: ids, Comment: comment},
		alertsSummary("Suppress", ids), alertsGatedTarget(ids), comment)
}

func (h *MCPHandler) toolReopenAlerts(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, ids, comment, errResult := h.alertIdsAndComment(ctx, req)
	if errResult != nil {
		return errResult, nil
	}
	n, err := h.Api.db.ReopenAlerts(project.Id, ids)
	if err != nil {
		klog.Errorln("mcp: reopen_alerts:", err)
		return mcp.NewToolResultError("failed to reopen alerts"), nil
	}
	h.Api.recordAlertActions(newActor(user, viaMCP), project.Id, ids, actionAlertReopened, comment)
	return MCPJSON(map[string]any{"reopened": n})
}

type mcpAlertingRuleInfo struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	SourceType    string `json:"source_type"`
	Severity      string `json:"severity"`
	Enabled       bool   `json:"enabled"`
	Builtin       bool   `json:"builtin,omitempty"`
	Readonly      bool   `json:"readonly,omitempty"`
	For           string `json:"for,omitempty"`
	KeepFiringFor string `json:"keep_firing_for,omitempty"`
	FiringAlerts  int    `json:"firing_alerts"`
}

func (h *MCPHandler) requireRules(ctx context.Context, edit bool) (*db.User, *db.Project, *mcp.CallToolResult) {
	user, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return nil, nil, errResult
	}
	action := rbac.Actions.Project(string(project.Id)).AlertingRules().View()
	if edit {
		action = rbac.Actions.Project(string(project.Id)).AlertingRules().Edit()
	}
	if !h.Api.IsAllowed(user, action) {
		return nil, nil, mcp.NewToolResultError("forbidden: no permission for alerting rules in this project")
	}
	return user, project, nil
}

func mcpShortDuration(d timeseries.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.ShortString()
}

func (h *MCPHandler) toolListAlertingRules(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, errResult := h.requireRules(ctx, false)
	if errResult != nil {
		return errResult, nil
	}
	rules, err := h.Api.db.GetAlertingRules(project.Id)
	if err != nil {
		klog.Errorln("mcp: list_alerting_rules:", err)
		return mcp.NewToolResultError("failed to load alerting rules"), nil
	}
	counts, err := h.Api.db.GetFiringAlertCountsByRule(project.Id)
	if err != nil {
		klog.Errorln("mcp: list_alerting_rules:", err)
	}
	search := strings.ToLower(req.GetString("search", ""))
	sourceType := req.GetString("source_type", "")
	out := make([]mcpAlertingRuleInfo, 0, len(rules))
	for _, r := range rules {
		if search != "" && !strings.Contains(strings.ToLower(r.Name), search) && !strings.Contains(strings.ToLower(string(r.Id)), search) {
			continue
		}
		if sourceType != "" && string(r.Source.Type) != sourceType {
			continue
		}
		out = append(out, mcpAlertingRuleInfo{
			Id:            string(r.Id),
			Name:          r.Name,
			SourceType:    string(r.Source.Type),
			Severity:      r.Severity.String(),
			Enabled:       r.Enabled,
			Builtin:       r.Builtin,
			Readonly:      r.Readonly,
			For:           mcpShortDuration(r.For),
			KeepFiringFor: mcpShortDuration(r.KeepFiringFor),
			FiringAlerts:  counts[string(r.Id)],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FiringAlerts != out[j].FiringAlerts {
			return out[i].FiringAlerts > out[j].FiringAlerts
		}
		return out[i].Name < out[j].Name
	})
	return mcpJSONList(out, "only the first rules are returned, narrow with search or source_type")
}

func (h *MCPHandler) getRule(project *db.Project, req mcp.CallToolRequest) (*model.AlertingRule, *mcp.CallToolResult) {
	id, err := req.RequireString("id")
	if err != nil || id == "" {
		return nil, mcp.NewToolResultError("id is required")
	}
	rule, err := h.Api.db.GetAlertingRule(project.Id, model.AlertingRuleId(id))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, mcp.NewToolResultError("alerting rule not found")
		}
		klog.Errorln("mcp:", err)
		return nil, mcp.NewToolResultError("failed to load the alerting rule")
	}
	return rule, nil
}

func (h *MCPHandler) ruleWithTimeline(user *db.User, project *db.Project, rule *model.AlertingRule) (*mcp.CallToolResult, error) {
	comments, err := h.Api.getTimeline(user, &commentTarget{typ: db.CommentTargetAlertingRule, id: string(rule.Id)}, project.Id)
	if err != nil {
		return mcpTargetError(err), nil
	}
	return MCPJSON(struct {
		*model.AlertingRule
		Timeline []mcpTimelineEntry `json:"timeline"`
	}{AlertingRule: rule, Timeline: mcpTimeline(comments)})
}

func (h *MCPHandler) toolGetAlertingRule(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.requireRules(ctx, false)
	if errResult != nil {
		return errResult, nil
	}
	rule, errResult := h.getRule(project, req)
	if errResult != nil {
		return errResult, nil
	}
	return h.ruleWithTimeline(user, project, rule)
}

func mcpParseDuration(s string) (timeseries.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "0" || s == "" {
		return 0, nil
	}
	var d timeseries.Duration
	if err := d.Set(s); err != nil {
		if td, err2 := time.ParseDuration(s); err2 == nil && td >= 0 {
			return timeseries.DurationFromStandard(td), nil
		}
		return 0, err
	}
	return d, nil
}

// applyRuleArgs applies the optional rule fields present in the request to rule.
func applyRuleArgs(rule *model.AlertingRule, req mcp.CallToolRequest) error {
	args := req.GetArguments()
	has := func(k string) bool { _, ok := args[k]; return ok }

	if has("name") {
		rule.Name = req.GetString("name", "")
	}
	if has("enabled") {
		rule.Enabled = req.GetBool("enabled", rule.Enabled)
	}
	if has("severity") {
		switch s := req.GetString("severity", ""); s {
		case "warning":
			rule.Severity = model.WARNING
		case "critical":
			rule.Severity = model.CRITICAL
		default:
			return fmt.Errorf("severity must be 'warning' or 'critical'")
		}
	}
	if has("source_type") {
		t := model.AlertSourceType(req.GetString("source_type", ""))
		if t != rule.Source.Type {
			rule.Source = model.AlertSource{Type: t}
		}
	}
	switch rule.Source.Type {
	case model.AlertSourceTypeCheck:
		if has("check_id") {
			rule.Source.Check = &model.CheckSource{CheckId: model.CheckId(req.GetString("check_id", ""))}
		}
	case model.AlertSourceTypePromQL:
		if has("promql_expression") {
			rule.Source.PromQL = &model.PromQLSource{Expression: req.GetString("promql_expression", "")}
		}
		// promql rules evaluate the expression project-wide
		rule.Selector = model.AppSelector{Type: model.AppSelectorTypeAll}
	case model.AlertSourceTypeLogPatterns:
		if rule.Source.LogPattern == nil {
			rule.Source.LogPattern = &model.LogPatternSource{Severities: []string{"error", "fatal"}, MinCount: 10, MaxAlertsPerApp: 20}
		}
		if has("log_severities") {
			rule.Source.LogPattern.Severities = req.GetStringSlice("log_severities", nil)
		}
		if has("min_count") {
			rule.Source.LogPattern.MinCount = req.GetInt("min_count", 0)
		}
		if has("max_alerts_per_app") {
			rule.Source.LogPattern.MaxAlertsPerApp = req.GetInt("max_alerts_per_app", 0)
		}
	case model.AlertSourceTypeKubernetesEvents:
		if rule.Source.KubernetesEvents == nil {
			rule.Source.KubernetesEvents = &model.KubernetesEventsSource{MinCount: 1, MaxAlertsPerApp: 20}
		}
		if has("min_count") {
			rule.Source.KubernetesEvents.MinCount = req.GetInt("min_count", 0)
		}
		if has("max_alerts_per_app") {
			rule.Source.KubernetesEvents.MaxAlertsPerApp = req.GetInt("max_alerts_per_app", 0)
		}
	}
	if rule.Source.Type != model.AlertSourceTypePromQL {
		if has("selector_type") {
			rule.Selector = model.AppSelector{Type: model.AppSelectorType(req.GetString("selector_type", ""))}
		}
		if has("selector_categories") {
			rule.Selector.Categories = req.GetStringSlice("selector_categories", nil)
		}
		if has("selector_app_patterns") {
			rule.Selector.ApplicationIdPatterns = req.GetStringSlice("selector_app_patterns", nil)
		}
		if rule.Selector.Type == "" {
			rule.Selector.Type = model.AppSelectorTypeAll
		}
	}
	for _, f := range []struct {
		key string
		dst *timeseries.Duration
	}{{"for", &rule.For}, {"keep_firing_for", &rule.KeepFiringFor}} {
		if !has(f.key) {
			continue
		}
		d, err := mcpParseDuration(req.GetString(f.key, ""))
		if err != nil {
			return fmt.Errorf("%s: %w", f.key, err)
		}
		*f.dst = d
	}
	if has("summary_template") {
		rule.Templates.Summary = req.GetString("summary_template", "")
	}
	if has("description") {
		rule.Templates.Description = req.GetString("description", "")
	}
	if has("notification_category") {
		rule.NotificationCategory = model.ApplicationCategory(req.GetString("notification_category", ""))
	}
	return nil
}

func (h *MCPHandler) toolCreateAlertingRule(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.requireRules(ctx, true)
	if errResult != nil {
		return errResult, nil
	}
	rule := &model.AlertingRule{
		Severity: model.WARNING,
		Enabled:  true,
		Selector: model.AppSelector{Type: model.AppSelectorTypeAll},
	}
	if err := applyRuleArgs(rule, req); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if rule.Source.Type == model.AlertSourceTypePromQL && rule.NotificationCategory == "" {
		rule.NotificationCategory = model.ApplicationCategoryApplication
	}
	a := newActor(user, viaMCP)
	if err := h.Api.createAlertingRule(project.Id, rule, a); err != nil {
		return mcpTargetError(err), nil
	}
	h.recordRuleNote(a, project.Id, rule, req)
	return h.ruleWithTimeline(user, project, rule)
}

func (h *MCPHandler) toolUpdateAlertingRule(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.requireRules(ctx, true)
	if errResult != nil {
		return errResult, nil
	}
	existing, errResult := h.getRule(project, req)
	if errResult != nil {
		return errResult, nil
	}
	if existing.Readonly {
		return mcp.NewToolResultError("this rule is managed via config and cannot be edited"), nil
	}
	if st := req.GetString("source_type", ""); existing.Builtin && st != "" && model.AlertSourceType(st) != existing.Source.Type {
		return mcp.NewToolResultError("the source type of a builtin rule cannot be changed"), nil
	}
	updated := *existing
	updated.Source = cloneAlertSource(existing.Source)
	updated.Selector.Categories = slices.Clone(existing.Selector.Categories)
	updated.Selector.ApplicationIdPatterns = slices.Clone(existing.Selector.ApplicationIdPatterns)
	if err := applyRuleArgs(&updated, req); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(alertingRuleChanges(existing, &updated)) == 0 && req.GetString("comment", "") == "" {
		return mcp.NewToolResultError("nothing to update: pass at least one field to change"), nil
	}
	comment := req.GetString("comment", "")
	_, ap, err := h.Api.runAgentAction(project, newActor(user, viaMCP), ruleUpdateAction(existing, &updated),
		ruleUpdateArgs{RuleId: string(existing.Id), Rule: &updated, Comment: comment}, ruleUpdateSummary(existing, &updated), ruleGatedTarget(existing), comment)
	if err != nil {
		return mcpTargetError(err), nil
	}
	if ap != nil {
		return MCPJSON(newPendingApproval(ap))
	}
	return h.ruleWithTimeline(user, project, &updated)
}

func (h *MCPHandler) toolDeleteAlertingRule(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.requireRules(ctx, true)
	if errResult != nil {
		return errResult, nil
	}
	rule, errResult := h.getRule(project, req)
	if errResult != nil {
		return errResult, nil
	}
	if err := ruleDeletable(rule); err != nil {
		return mcpTargetError(err), nil
	}
	comment := req.GetString("comment", "")
	return h.runGated(project, newActor(user, viaMCP), db.AgentActionDeleteAlertingRule, ruleDeleteArgs{RuleId: string(rule.Id), Comment: comment},
		"Delete the alerting rule \""+rule.Name+"\"", ruleGatedTarget(rule), comment)
}

// cloneAlertSource deep-copies a rule source so a partial update doesn't mutate the original.
func cloneAlertSource(s model.AlertSource) model.AlertSource {
	res := model.AlertSource{Type: s.Type}
	if s.Check != nil {
		c := *s.Check
		res.Check = &c
	}
	if s.PromQL != nil {
		p := *s.PromQL
		res.PromQL = &p
	}
	if s.LogPattern != nil {
		lp := *s.LogPattern
		lp.Severities = slices.Clone(s.LogPattern.Severities)
		res.LogPattern = &lp
	}
	if s.KubernetesEvents != nil {
		ke := *s.KubernetesEvents
		res.KubernetesEvents = &ke
	}
	return res
}

// recordRuleNote stores the optional "comment" argument of a rule change as a comment in the rule's timeline.
func (h *MCPHandler) recordRuleNote(a actor, projectId db.ProjectId, rule *model.AlertingRule, req mcp.CallToolRequest) {
	note := strings.TrimSpace(req.GetString("comment", ""))
	if note == "" || len(note) > db.CommentMaxBodyLength {
		return
	}
	if err := h.Api.db.AddComment(a.comment(projectId, db.CommentTargetAlertingRule, string(rule.Id), db.CommentKindComment, note, nil)); err != nil {
		klog.Errorln("mcp: failed to record a rule comment:", err)
	}
}

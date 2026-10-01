package api

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/mark3labs/mcp-go/mcp"
)

// shards fork: the MCP side of agent approvals.
//
// Every gated MCP tool turns its arguments into a gatedCall (the db.AgentAction* action plus
// typed, validated arguments) with mcpGatedCall. The same builder is used
//   - by MCPApprovalGate (the agents area's hook, called for registered agents after the scope
//     check, before the tool runs): 'approval' stores the call and short-circuits with
//     "pending approval <id>", 'deny' refuses it;
//   - by the tools themselves (runGated), which also covers agents that are not in the registry
//     (OAuth MCP clients, legacy user API keys). For a registered agent the tool only runs when
//     the hook let it through, so the second check is a no-op.
// Approved calls are executed by doAgentAction from the stored typed arguments.

type gatedCall struct {
	action  string
	args    any
	summary string
	target  gatedTarget
	reason  string
	// for rendering the tool result
	rule *model.AlertingRule
}

func mcpError(msg string) *mcp.CallToolResult {
	return mcp.NewToolResultError(msg)
}

// mcpGatedCall builds the gated action of a tool call. It returns (nil, nil) when the call is not
// subject to the approval policy (e.g. update_incident with an action other than resolve).
func (h *MCPHandler) mcpGatedCall(project *db.Project, tool string, req mcp.CallToolRequest) (*gatedCall, *mcp.CallToolResult) {
	comment := strings.TrimSpace(req.GetString("comment", ""))
	if len(comment) > db.CommentMaxBodyLength {
		return nil, mcpError("comment is too long")
	}
	switch tool {
	case "resolve_alerts", "suppress_alerts":
		ids, err := req.RequireStringSlice("ids")
		if err != nil {
			return nil, mcpError(err.Error())
		}
		if len(ids) == 0 {
			return nil, mcpError("ids must be a non-empty array")
		}
		action, verb := db.AgentActionResolveAlerts, "Resolve"
		if tool == "suppress_alerts" {
			action, verb = db.AgentActionSuppressAlerts, "Suppress"
		}
		return &gatedCall{action: action, args: alertsActionArgs{Ids: ids, Comment: comment}, summary: alertsSummary(verb, ids), target: alertsGatedTarget(ids), reason: comment}, nil

	case "delete_alerting_rule":
		rule, errResult := h.getRule(project, req)
		if errResult != nil {
			return nil, errResult
		}
		if err := ruleDeletable(rule); err != nil {
			return nil, mcpTargetError(err)
		}
		return &gatedCall{action: db.AgentActionDeleteAlertingRule, args: ruleDeleteArgs{RuleId: string(rule.Id), Comment: comment},
			summary: "Delete the alerting rule \"" + rule.Name + "\"", target: ruleGatedTarget(rule), reason: comment}, nil

	case "update_alerting_rule":
		existing, errResult := h.getRule(project, req)
		if errResult != nil {
			return nil, errResult
		}
		if existing.Readonly {
			return nil, mcpError("this rule is managed via config and cannot be edited")
		}
		if st := req.GetString("source_type", ""); existing.Builtin && st != "" && model.AlertSourceType(st) != existing.Source.Type {
			return nil, mcpError("the source type of a builtin rule cannot be changed")
		}
		updated := *existing
		updated.Source = cloneAlertSource(existing.Source)
		updated.Selector.Categories = slices.Clone(existing.Selector.Categories)
		updated.Selector.ApplicationIdPatterns = slices.Clone(existing.Selector.ApplicationIdPatterns)
		if err := applyRuleArgs(&updated, req); err != nil {
			return nil, mcpError(err.Error())
		}
		if len(alertingRuleChanges(existing, &updated)) == 0 && comment == "" {
			return nil, mcpError("nothing to update: pass at least one field to change")
		}
		return &gatedCall{action: ruleUpdateAction(existing, &updated), args: ruleUpdateArgs{RuleId: string(existing.Id), Rule: &updated, Comment: comment},
			summary: ruleUpdateSummary(existing, &updated), target: ruleGatedTarget(existing), reason: comment, rule: &updated}, nil

	case "update_incident":
		form := incidentActionForm{
			Action:       req.GetString("action", ""),
			Assignee:     req.GetString("assignee", ""),
			AssigneeKind: req.GetString("assignee_kind", ""),
			Severity:     req.GetString("severity", ""),
			Resolution:   req.GetString("resolution", ""),
			RootCause:    req.GetString("root_cause", ""),
			FollowUps:    req.GetStringSlice("follow_ups", nil),
			Comment:      comment,
		}
		if err := form.validate(); err != nil {
			return nil, mcpTargetError(err)
		}
		if form.Action != incidentActionResolve {
			return nil, nil
		}
		key := strings.TrimPrefix(req.GetString("incident", ""), "i-")
		return &gatedCall{action: db.AgentActionResolveIncident, args: incidentResolveArgs{Key: key, Form: form},
			summary: "Resolve incident " + key + ": " + utils.Truncate(form.Resolution, 200),
			target:  gatedTarget{typ: db.CommentTargetIncident, id: key, title: "Incident " + key}, reason: comment}, nil

	case "create_maintenance_window":
		now := timeseries.Now()
		form := maintenanceForm{DurationMinutes: req.GetInt("duration_minutes", 0)}
		form.Name = req.GetString("name", "")
		form.Comment = comment
		if s := req.GetString("starts_at", ""); s != "" {
			form.StartsAt = utils.ParseTime(now, s, now)
		}
		if s := req.GetString("ends_at", ""); s != "" {
			form.EndsAt = utils.ParseTime(now, s, 0)
		}
		if days, ok := req.GetArguments()["weekdays"].([]any); ok && len(days) > 0 {
			r := &db.MaintenanceRecurrence{StartTime: req.GetString("start_time", ""), DurationMinutes: req.GetInt("recurring_duration_minutes", 0), Timezone: req.GetString("timezone", "")}
			for _, d := range days {
				if f, ok := d.(float64); ok {
					r.Weekdays = append(r.Weekdays, int(f))
				}
			}
			form.Recurrence = r
		}
		form.Scope = db.MaintenanceScope{
			ApplicationPatterns: req.GetStringSlice("application_patterns", nil),
			Categories:          req.GetStringSlice("categories", nil),
			NodePatterns:        req.GetStringSlice("node_patterns", nil),
			AlertingRuleIds:     req.GetStringSlice("alerting_rule_ids", nil),
		}
		if form.DurationMinutes == 0 && form.EndsAt == 0 && form.Recurrence == nil {
			return nil, mcpError("give duration_minutes, ends_at or a weekly schedule")
		}
		w, err := form.window(now)
		if err != nil {
			return nil, mcpTargetError(err)
		}
		return &gatedCall{action: db.AgentActionCreateMaintenanceWindow, args: maintenanceCreateArgs{Window: w}, summary: maintenanceSummary(w), reason: w.Comment}, nil

	case "end_maintenance_window":
		id := req.GetInt("id", 0)
		w, err := h.Api.db.GetMaintenanceWindow(project.Id, id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, mcpError("maintenance window not found")
			}
			return nil, mcpTargetError(err)
		}
		return &gatedCall{action: db.AgentActionEndMaintenanceWindow, args: maintenanceEndArgs{Id: id, Comment: comment},
			summary: "End the maintenance window \"" + w.Name + "\"",
			target:  gatedTarget{typ: db.CommentTargetMaintenanceWindow, id: strconv.Itoa(id), title: w.Name}, reason: comment}, nil
	}
	return nil, nil
}

// runGatedCall gates (for agents) and executes a gated call.
func (h *MCPHandler) runGatedCall(project *db.Project, a actor, c *gatedCall) (any, *mcp.CallToolResult, error) {
	res, ap, err := h.Api.runAgentAction(project, a, c.action, c.args, c.summary, c.target, c.reason)
	if err != nil {
		return nil, mcpTargetError(err), nil
	}
	if ap != nil {
		r, err := MCPJSON(newPendingApproval(ap))
		return nil, r, err
	}
	return res, nil, nil
}

// mcpToolGated is the common body of the gated tools whose result is the action's result.
func (h *MCPHandler) mcpToolGated(user *db.User, project *db.Project, tool string, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c, errResult := h.mcpGatedCall(project, tool, req)
	if errResult != nil {
		return errResult, nil
	}
	res, r, err := h.runGatedCall(project, newActor(user, viaMCP), c)
	if r != nil || err != nil {
		return r, err
	}
	return MCPJSON(res)
}

// approvalGate implements MCPApprovalGate for registered agents.
func (h *MCPHandler) approvalGate(ctx context.Context, _ *db.Agent, user *db.User, tool string, args map[string]any) *mcp.CallToolResult {
	project, err := h.currentProject(ctx)
	if err != nil || project == nil {
		return nil
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = tool
	req.Params.Arguments = args
	c, errResult := h.mcpGatedCall(project, tool, req)
	if errResult != nil || c == nil {
		return nil // not gated, or invalid arguments: the tool reports the error itself
	}
	ap, err := h.Api.gateAgentAction(project, newActor(user, viaMCP), c.action, c.args, c.summary, c.target, c.reason)
	if err != nil {
		return mcpTargetError(err)
	}
	if ap != nil {
		r, _ := MCPJSON(newPendingApproval(ap))
		return r
	}
	return nil
}

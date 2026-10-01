package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/timeseries"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rest calls a REST handler as the given user.
func (e *mcpTestEnv) rest(h func(http.ResponseWriter, *http.Request, *db.User), u *db.User, method, path string, vars map[string]string, body any) *httptest.ResponseRecorder {
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		require.NoError(e.t, err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	v := map[string]string{"project": string(e.project.Id)}
	for k, val := range vars {
		v[k] = val
	}
	r = mux.SetURLVars(r, v)
	w := httptest.NewRecorder()
	h(w, r, u)
	return w
}

func (e *mcpTestEnv) incident(key string, appName string, opened timeseries.Time) *model.ApplicationIncident {
	appId := model.NewApplicationId(string(e.project.Id), "shop", model.ApplicationKindDeployment, appName)
	i := &model.ApplicationIncident{ApplicationId: appId, Key: key, OpenedAt: opened, Severity: model.CRITICAL}
	i.Details.AvailabilityImpact.AffectedRequestPercentage = 12.5
	require.NoError(e.t, e.db.CreateIncident(e.project.Id, appId, i))
	return i
}

func human() *db.User {
	return db.AnonymousUser(rbac.RoleAdmin)
}

func TestIncidentWorkflowREST(t *testing.T) {
	e := newMCPTestEnv(t)
	now := timeseries.Now()
	e.incident("inc1", "payments", now.Add(-timeseries.Hour))
	u := human()
	vars := map[string]string{"incident": "inc1"}
	post := func(form incidentActionForm) *httptest.ResponseRecorder {
		return e.rest(e.h.Api.IncidentWorkflow, u, http.MethodPost, "/", vars, form)
	}

	w := e.rest(e.h.Api.IncidentWorkflow, u, http.MethodGet, "/", vars, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var v incidentWorkflowView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	assert.Equal(t, db.IncidentStatusTriggered, v.Status)
	assert.True(t, v.Open)

	require.Equal(t, http.StatusOK, post(incidentActionForm{Action: "acknowledge"}).Code)
	assert.Equal(t, http.StatusConflict, post(incidentActionForm{Action: "acknowledge"}).Code)
	require.Equal(t, http.StatusOK, post(incidentActionForm{Action: "assign", Assignee: "bob"}).Code)
	require.Equal(t, http.StatusOK, post(incidentActionForm{Action: "set_severity", Severity: "warning"}).Code)
	require.Equal(t, http.StatusOK, post(incidentActionForm{Action: "mitigate", Comment: "rolled back"}).Code)
	assert.Equal(t, http.StatusBadRequest, post(incidentActionForm{Action: "resolve"}).Code, "a resolution summary is required")
	assert.Equal(t, http.StatusBadRequest, post(incidentActionForm{Action: "nope"}).Code)
	w = post(incidentActionForm{Action: "resolve", Resolution: "Rolled back v2.3", RootCause: "bad config", FollowUps: []string{"add a canary", " "}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	assert.Equal(t, db.IncidentStatusResolved, v.Status)
	assert.Equal(t, db.IncidentResolvedByUser, v.ResolvedKind)
	assert.Equal(t, "bob", v.Assignee)
	assert.Equal(t, "warning", v.Severity)
	assert.Equal(t, []string{"add a canary"}, v.FollowUps)
	assert.False(t, v.Open)

	i, err := e.db.GetIncidentByKey(e.project.Id, "inc1")
	require.NoError(t, err)
	assert.True(t, i.Resolved(), "a manual resolve closes the incident")

	timeline, err := e.db.GetComments(e.project.Id, db.CommentTargetIncident, "inc1")
	require.NoError(t, err)
	var actions []string
	for _, c := range timeline {
		actions = append(actions, c.Meta["action"])
	}
	assert.Equal(t, []string{"acknowledged", "assigned", "severity_changed", "mitigated", "resolved"}, actions)

	// postmortem
	w = e.rest(e.h.Api.IncidentPostmortem, u, http.MethodGet, "/", vars, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var pm map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &pm))
	md := pm["markdown"]
	for _, s := range []string{"# Postmortem: payments", "## Impact", "12.5% of requests failed", "## Resolution", "Rolled back v2.3", "bad config", "## Timeline", "acknowledged the incident", "- [ ] add a canary", "## Deployments"} {
		assert.Contains(t, md, s)
	}

	// viewers can read but not change
	viewer := db.AnonymousUser(rbac.RoleViewer)
	assert.Equal(t, http.StatusOK, e.rest(e.h.Api.IncidentWorkflow, viewer, http.MethodGet, "/", vars, nil).Code)
	assert.Equal(t, http.StatusForbidden, e.rest(e.h.Api.IncidentWorkflow, viewer, http.MethodPost, "/", vars, incidentActionForm{Action: "acknowledge"}).Code)

	// list + attention counts
	e.incident("inc2", "catalog", now.Add(-2*timeseries.Hour))
	w = e.rest(e.h.Api.IncidentsWorkflow, u, http.MethodGet, "/", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list map[string]incidentListWorkflow
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Equal(t, db.IncidentStatusResolved, list["inc1"].Status)
	assert.Equal(t, db.IncidentStatusTriggered, list["inc2"].Status)
	assert.Equal(t, 1, e.h.Api.attentionCounts(e.project.Id).UnacknowledgedIncidents)
}

func TestAgentApprovalFlow(t *testing.T) {
	e := newMCPTestEnv(t)
	ctx := e.ctx(rbac.RoleEditor, "editor")
	rule := &model.AlertingRule{Name: "Noisy", Severity: model.WARNING, Enabled: true, Source: model.AlertSource{Type: model.AlertSourceTypePromQL, PromQL: &model.PromQLSource{Expression: "up == 0"}},
		Selector: model.AppSelector{Type: model.AppSelectorTypeAll}, NotificationCategory: model.ApplicationCategoryApplication}
	require.NoError(t, e.h.Api.createAlertingRule(e.project.Id, rule, actor{name: "alice", kind: db.CommentAuthorUser}))

	// default policy: deleting a rule waits for a human
	res := e.call(ctx, e.h.toolDeleteAlertingRule, map[string]any{"id": string(rule.Id), "comment": "duplicate of the SLO alert"})
	require.False(t, res.IsError, resultText(res))
	var pending pendingApproval
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &pending))
	assert.Equal(t, "pending", pending.Status)
	assert.Contains(t, pending.Message, "pending approval "+strconv.Itoa(pending.ApprovalId))
	_, err := e.db.GetAlertingRule(e.project.Id, rule.Id)
	require.NoError(t, err, "nothing is executed before the approval")

	res = e.call(ctx, e.h.toolGetApprovalStatus, map[string]any{"id": float64(pending.ApprovalId)})
	require.False(t, res.IsError, resultText(res))
	assert.Contains(t, resultText(res), `"status":"pending"`)
	assert.Equal(t, 1, e.h.Api.attentionCounts(e.project.Id).PendingApprovals)

	// agents can't approve
	agent, err := e.db.GetUserByApiKey("key-editor")
	require.NoError(t, err)
	_, err = e.h.Api.decideApproval(agent, e.project, pending.ApprovalId, true, "")
	require.Error(t, err)

	// a human approves via REST: executed as the agent, "approved by" in the timeline
	w := e.rest(e.h.Api.Approval, human(), http.MethodPost, "/", map[string]string{"id": strconv.Itoa(pending.ApprovalId)}, approvalDecisionForm{Decision: "approve", Comment: "ok"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var ap db.Approval
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ap))
	assert.Equal(t, db.ApprovalStatusExecuted, ap.Status)
	assert.Equal(t, db.AnonymousUserName, ap.DecidedBy)
	_, err = e.db.GetAlertingRule(e.project.Id, rule.Id)
	assert.ErrorIs(t, err, db.ErrNotFound)
	timeline, err := e.db.GetComments(e.project.Id, db.CommentTargetAlertingRule, string(rule.Id))
	require.NoError(t, err)
	var deleted *db.Comment
	actions := map[string]bool{}
	for _, c := range timeline {
		actions[c.Meta["action"]] = true
		if c.Meta["action"] == actionRuleDeleted {
			deleted = c
		}
	}
	assert.True(t, actions[actionApprovalRequested])
	assert.True(t, actions[actionApprovalApproved])
	require.NotNil(t, deleted)
	assert.Equal(t, "agent-editor", deleted.Author)
	assert.Equal(t, db.CommentAuthorAgent, deleted.AuthorKind)
	assert.Equal(t, db.AnonymousUserName, deleted.Meta["approved_by"])

	// already decided
	w = e.rest(e.h.Api.Approval, human(), http.MethodPost, "/", map[string]string{"id": strconv.Itoa(pending.ApprovalId)}, approvalDecisionForm{Decision: "reject"})
	assert.Equal(t, http.StatusConflict, w.Code)

	// incident resolve by an agent: pending, rejected -> nothing changes
	e.incident("inc1", "payments", timeseries.Now().Add(-timeseries.Hour))
	res = e.call(ctx, e.h.toolUpdateIncident, map[string]any{"incident": "inc1", "action": "resolve", "resolution": "restarted the pod"})
	require.False(t, res.IsError, resultText(res))
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &pending))
	assert.Equal(t, "pending", pending.Status)
	ap2, err := e.h.Api.decideApproval(human(), e.project, pending.ApprovalId, false, "not fixed yet")
	require.NoError(t, err)
	assert.Equal(t, db.ApprovalStatusRejected, ap2.Status)
	i, err := e.db.GetIncidentByKey(e.project.Id, "inc1")
	require.NoError(t, err)
	assert.False(t, i.Resolved())

	// acknowledge is never gated
	res = e.call(ctx, e.h.toolUpdateIncident, map[string]any{"incident": "inc1", "action": "acknowledge"})
	require.False(t, res.IsError, resultText(res))
	assert.Contains(t, resultText(res), `"assignee":"agent-editor"`)

	// deny policy
	e.project.Settings.AgentApprovals = &db.AgentApprovalPolicy{RequireApproval: true, Actions: map[string]string{db.AgentActionSuppressAlerts: db.ApprovalPolicyDeny}}
	require.NoError(t, e.db.SaveProjectSettings(e.project))
	res = e.call(ctx, e.h.toolSuppressAlerts, map[string]any{"ids": []any{"a1"}})
	assert.True(t, res.IsError)
	assert.Contains(t, resultText(res), "denied")

	// REST: an API key gets 202 + approval id; humans are never gated
	key := agent
	w = e.rest(e.h.Api.IncidentWorkflow, key, http.MethodPost, "/", map[string]string{"incident": "inc1"}, incidentActionForm{Action: "resolve", Resolution: "done"})
	assert.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "pending approval")
	w = e.rest(e.h.Api.IncidentWorkflow, human(), http.MethodPost, "/", map[string]string{"incident": "inc1"}, incidentActionForm{Action: "resolve", Resolution: "done"})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// policy endpoint: humans with settings permission can edit, agents can't
	w = e.rest(e.h.Api.ApprovalPolicy, human(), http.MethodPut, "/", nil, db.AgentApprovalPolicy{RequireApproval: false})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"require_approval":false`)
	w = e.rest(e.h.Api.ApprovalPolicy, key, http.MethodPut, "/", nil, db.AgentApprovalPolicy{RequireApproval: true})
	assert.Equal(t, http.StatusForbidden, w.Code)
	w = e.rest(e.h.Api.ApprovalPolicy, human(), http.MethodPut, "/", nil, db.AgentApprovalPolicy{Actions: map[string]string{"x": "auto"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMaintenanceWindowsAPIAndContext(t *testing.T) {
	e := newMCPTestEnv(t)
	ctx := e.ctx(rbac.RoleEditor, "editor")
	u := human()
	now := timeseries.Now()

	// quick action from an app page
	w := e.rest(e.h.Api.MaintenanceWindows, u, http.MethodPost, "/", nil, map[string]any{
		"name": "payments deploy", "duration_minutes": 30, "scope": map[string]any{"application_patterns": []string{"shop:Deployment:payments"}},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var mw maintenanceWindowView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &mw))
	assert.Equal(t, "active", mw.Status)
	assert.Equal(t, int64(30*60), int64(mw.EndsAt-mw.StartsAt))

	w = e.rest(e.h.Api.MaintenanceWindows, u, http.MethodPost, "/", nil, map[string]any{"name": "", "duration_minutes": 30})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w = e.rest(e.h.Api.MaintenanceWindows, db.AnonymousUser(rbac.RoleViewer), http.MethodPost, "/", nil, map[string]any{"name": "x", "duration_minutes": 30})
	assert.Equal(t, http.StatusForbidden, w.Code)

	// MCP: agent creates a weekly window (auto by default) and lists it
	res := e.call(ctx, e.h.toolCreateMaintenanceWindow, map[string]any{
		"name": "backups", "weekdays": []any{float64(0)}, "start_time": "02:00", "recurring_duration_minutes": float64(60), "categories": []any{"db"},
	})
	require.False(t, res.IsError, resultText(res))
	res = e.call(ctx, e.h.toolListMaintenanceWindows, map[string]any{"state": "current"})
	require.False(t, res.IsError, resultText(res))
	assert.Contains(t, resultText(res), `"returned":2`)

	// end the quick window via MCP
	res = e.call(ctx, e.h.toolEndMaintenanceWindow, map[string]any{"id": float64(mw.Id), "comment": "deploy finished"})
	require.False(t, res.IsError, resultText(res))
	assert.Contains(t, resultText(res), `"status":"ended"`)
	timeline, err := e.db.GetComments(e.project.Id, db.CommentTargetMaintenanceWindow, strconv.Itoa(mw.Id))
	require.NoError(t, err)
	require.Len(t, timeline, 2)
	assert.Equal(t, "ended", timeline[1].Meta["action"])

	// incident context bundle
	cur := e.incident("cur", "payments", now.Add(-timeseries.Hour))
	e.incident("old", "payments", now.Add(-5*timeseries.Day))
	require.NoError(t, e.db.SaveIncidentWorkflow(&db.IncidentWorkflow{ProjectId: e.project.Id, IncidentKey: "old", ApplicationId: cur.ApplicationId.String(),
		Status: db.IncidentStatusResolved, ResolvedKind: db.IncidentResolvedByUser, Resolution: "scaled up the pool"}))
	require.NoError(t, e.db.CreateAlert(e.project.Id, &model.Alert{Id: "al1", Fingerprint: "f", RuleId: "cpu", ApplicationId: cur.ApplicationId, Severity: model.CRITICAL, Summary: "CPU throttling"}))
	require.NoError(t, e.db.CreateAlert(e.project.Id, &model.Alert{Id: "al2", Fingerprint: "g", RuleId: "cpu",
		ApplicationId: model.NewApplicationId(string(e.project.Id), "x", model.ApplicationKindDeployment, "other"), Severity: model.CRITICAL, Summary: "other"}))
	res = e.call(ctx, e.h.toolGetIncidentContext, map[string]any{"incident": "i-cur"})
	require.False(t, res.IsError, resultText(res))
	var ic mcpIncidentContext
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &ic))
	assert.Equal(t, "cur", ic.Incident.Key)
	assert.Equal(t, "triggered", ic.Incident.Status)
	require.Len(t, ic.Alerts, 1)
	assert.Equal(t, "al1", ic.Alerts[0].Id)
	require.Len(t, ic.Similar, 1)
	require.NotNil(t, ic.Similar[0].Resolution)
	assert.Equal(t, "scaled up the pool", string(*ic.Similar[0].Resolution))
	assert.Len(t, ic.Maintenance, 0) // the quick window was ended, the weekly one isn't active (most likely)
	assert.Less(t, len(resultText(res)), 3000, "the context bundle must stay compact")

	// postmortem via MCP
	res = e.call(ctx, e.h.toolGetIncidentPostmortem, map[string]any{"incident": "cur"})
	require.False(t, res.IsError, resultText(res))
	assert.True(t, strings.HasPrefix(resultText(res), `{"format":"markdown","postmortem":{"untrusted_data":"# Postmortem`), resultText(res))

	// home
	w = e.rest(e.h.Api.Home, u, http.MethodGet, "/", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var home struct {
		Data homeView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &home))
	require.Len(t, home.Data.Incidents, 2)
	assert.Equal(t, 2, home.Data.AlertsTotal)
	assert.NotEmpty(t, home.Data.Activity, "agent actions show up as recent agent activity")
}

func TestRegisteredAgentApprovalGate(t *testing.T) {
	e := newMCPTestEnv(t)
	ctx, agent, _ := e.agentCtx("ops-bot", db.AgentScopeOperator, rbac.RoleEditor)
	agent.Dispatch = &db.AgentDispatchConfig{Enabled: true, URL: "http://127.0.0.1:1/hook", Secret: "s", Events: []string{db.AgentEventApprovalDecided}}
	require.NoError(t, e.db.UpdateAgent(agent))
	rule := &model.AlertingRule{Name: "Noisy", Severity: model.WARNING, Enabled: true, Source: model.AlertSource{Type: model.AlertSourceTypePromQL, PromQL: &model.PromQLSource{Expression: "up == 0"}},
		Selector: model.AppSelector{Type: model.AppSelectorTypeAll}, NotificationCategory: model.ApplicationCategoryApplication}
	require.NoError(t, e.h.Api.createAlertingRule(e.project.Id, rule, actor{name: "alice", kind: db.CommentAuthorUser}))

	// through the MCP server: scope check, then the approval gate short-circuits the call
	out := e.rpc(ctx, "tools/call", map[string]any{"name": "update_alerting_rule", "arguments": map[string]any{"id": string(rule.Id), "enabled": false}})
	assert.Contains(t, string(out), "pending approval")
	stored, err := e.db.GetAlertingRule(e.project.Id, rule.Id)
	require.NoError(t, err)
	assert.True(t, stored.Enabled)
	aps, err := e.db.GetApprovals(e.project.Id, db.ApprovalsQuery{Status: db.ApprovalStatusPending})
	require.NoError(t, err)
	require.Len(t, aps, 1)
	assert.Equal(t, db.AgentActionDisableAlertingRule, aps[0].Action)
	assert.Equal(t, strconv.Itoa(agent.Id), aps[0].RequestedMeta["agent_id"])

	// auto actions pass the gate and run
	out = e.rpc(ctx, "tools/call", map[string]any{"name": "create_maintenance_window", "arguments": map[string]any{"name": "deploy", "duration_minutes": float64(15)}})
	assert.Contains(t, string(out), `\"status\":\"active\"`)

	// the decision wakes the agent up
	ap, err := e.h.Api.decideApproval(human(), e.project, aps[0].Id, true, "")
	require.NoError(t, err)
	assert.Equal(t, db.ApprovalStatusExecuted, ap.Status)
	stored, err = e.db.GetAlertingRule(e.project.Id, rule.Id)
	require.NoError(t, err)
	assert.False(t, stored.Enabled)
	dls, err := e.db.GetAgentDeliveries(agent.Id, 10)
	require.NoError(t, err)
	require.Len(t, dls, 1)
	assert.Equal(t, db.AgentEventApprovalDecided, dls[0].Event)

	// a triage agent can acknowledge incidents but not create maintenance windows
	tctx, _, _ := e.agentCtx("triage-bot", db.AgentScopeTriage, rbac.RoleEditor)
	e.incident("inc1", "payments", timeseries.Now().Add(-timeseries.Hour))
	out = e.rpc(tctx, "tools/call", map[string]any{"name": "update_incident", "arguments": map[string]any{"incident": "inc1", "action": "acknowledge"}})
	assert.NotContains(t, string(out), `"isError":true`)
	out = e.rpc(tctx, "tools/call", map[string]any{"name": "create_maintenance_window", "arguments": map[string]any{"name": "x", "duration_minutes": float64(15)}})
	assert.Contains(t, string(out), "forbidden")
}

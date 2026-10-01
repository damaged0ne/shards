package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/coroot/coroot/config"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testMCPSession struct{ id string }

func (s *testMCPSession) Initialize()       {}
func (s *testMCPSession) Initialized() bool { return true }
func (s *testMCPSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return make(chan mcp.JSONRPCNotification, 10)
}
func (s *testMCPSession) SessionID() string { return s.id }

type mcpTestEnv struct {
	t       *testing.T
	db      *db.DB
	h       *MCPHandler
	project *db.Project
}

func newMCPTestEnv(t *testing.T) *mcpTestEnv {
	database, err := db.NewSqlite(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { _ = database.DB().Close() })
	p := &db.Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))
	a := NewApi(&config.Config{}, nil, database, nil, nil, nil, rbac.NewStaticRoleManager(), nil, nil, "", "", nil)
	return &mcpTestEnv{t: t, db: database, h: a.SetupMCP(MCPInstructions), project: p}
}

// ctx returns a context authenticated as an agent (user API key) with the test project selected.
func (e *mcpTestEnv) ctx(role rbac.RoleName, sessionId string) context.Context {
	id, err := e.db.AddServiceAccount("sa-"+sessionId, "SA "+sessionId, role)
	require.NoError(e.t, err)
	require.NoError(e.t, e.db.AddUserApiKey(id, "key-"+sessionId, "agent-"+sessionId))
	user, err := e.db.GetUserByApiKey("key-" + sessionId)
	require.NoError(e.t, err)
	ctx := context.WithValue(context.Background(), mcpUserCtxKey{}, user)
	ctx = e.h.Server.WithContext(ctx, &testMCPSession{id: sessionId})
	res := e.call(ctx, e.h.toolSelectProject, map[string]any{"project_id": string(e.project.Id)})
	require.False(e.t, res.IsError, resultText(res))
	return ctx
}

func (e *mcpTestEnv) call(ctx context.Context, tool func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := tool(ctx, req)
	require.NoError(e.t, err)
	return res
}

func resultText(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestMCPAgentAlertWorkflow(t *testing.T) {
	e := newMCPTestEnv(t)
	ctx := e.ctx(rbac.RoleEditor, "editor")

	alert := &model.Alert{
		Id:            "alert-1",
		Fingerprint:   "fp",
		RuleId:        "memory-oom",
		ApplicationId: model.NewApplicationId(string(e.project.Id), "default", model.ApplicationKindDeployment, "api"),
		Severity:      model.WARNING,
		Summary:       "OOM",
	}
	require.NoError(t, e.db.CreateAlert(e.project.Id, alert))

	res := e.call(ctx, e.h.toolAddComment, map[string]any{"target_type": "alert", "target_id": "alert-1", "body": "Looking into it"})
	require.False(t, res.IsError, resultText(res))

	res = e.call(ctx, e.h.toolAddComment, map[string]any{"target_type": "alert", "target_id": "nope", "body": "x"})
	assert.True(t, res.IsError)
	res = e.call(ctx, e.h.toolAddComment, map[string]any{"target_type": "alert", "target_id": "alert-1", "body": "  "})
	assert.True(t, res.IsError)

	res = e.call(ctx, e.h.toolResolveAlerts, map[string]any{"ids": []any{"alert-1"}, "comment": "Raised the memory limit"})
	require.False(t, res.IsError, resultText(res))

	res = e.call(ctx, e.h.toolListComments, map[string]any{"target_type": "alert", "target_id": "alert-1"})
	require.False(t, res.IsError, resultText(res))
	var list struct {
		Items []mcpTimelineEntry `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &list))
	require.Len(t, list.Items, 2)
	assert.Equal(t, "comment", list.Items[0].Kind)
	assert.Equal(t, "agent-editor", list.Items[0].Author)
	assert.Equal(t, "agent", list.Items[0].AuthorKind)
	assert.Equal(t, "mcp", list.Items[0].Meta["via"])
	assert.Equal(t, "action", list.Items[1].Kind)
	assert.Equal(t, "resolved", list.Items[1].Action)
	assert.Equal(t, "Raised the memory limit", list.Items[1].Body)

	a, err := e.db.GetAlert(e.project.Id, "alert-1")
	require.NoError(t, err)
	assert.NotZero(t, a.ManuallyResolvedAt)
	assert.Equal(t, "agent-editor (via MCP)", a.ResolvedBy)

	res = e.call(ctx, e.h.toolReopenAlerts, map[string]any{"ids": []any{"alert-1"}})
	require.False(t, res.IsError, resultText(res))
	res = e.call(ctx, e.h.toolGetAlert, map[string]any{"id": "alert-1"})
	require.False(t, res.IsError, resultText(res))
	var got struct {
		Id       string             `json:"id"`
		Timeline []mcpTimelineEntry `json:"timeline"`
	}
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &got))
	assert.Equal(t, "alert-1", got.Id)
	require.Len(t, got.Timeline, 3)
	assert.Equal(t, "reopened", got.Timeline[2].Action)

	// viewers can read but not write
	vctx := e.ctx(rbac.RoleViewer, "viewer")
	res = e.call(vctx, e.h.toolListComments, map[string]any{"target_type": "alert", "target_id": "alert-1"})
	assert.False(t, res.IsError, resultText(res))
	res = e.call(vctx, e.h.toolAddComment, map[string]any{"target_type": "alert", "target_id": "alert-1", "body": "hi"})
	assert.True(t, res.IsError)
	res = e.call(vctx, e.h.toolSuppressAlerts, map[string]any{"ids": []any{"alert-1"}})
	assert.True(t, res.IsError)
}

func TestMCPAgentAlertingRules(t *testing.T) {
	e := newMCPTestEnv(t)
	ctx := e.ctx(rbac.RoleEditor, "editor")
	// this test covers the rule tools themselves: no human approvals (see approvals_shards_test.go)
	e.project.Settings.AgentApprovals = &db.AgentApprovalPolicy{RequireApproval: false}
	require.NoError(t, e.db.SaveProjectSettings(e.project))

	// validation is shared with the REST form
	res := e.call(ctx, e.h.toolCreateAlertingRule, map[string]any{"name": "bad", "source_type": "promql", "promql_expression": "rate(x[5m] >"})
	assert.True(t, res.IsError)
	res = e.call(ctx, e.h.toolCreateAlertingRule, map[string]any{"name": "bad", "source_type": "check", "check_id": "NoSuchCheck"})
	assert.True(t, res.IsError)

	res = e.call(ctx, e.h.toolCreateAlertingRule, map[string]any{
		"name":              "High error rate",
		"source_type":       "promql",
		"promql_expression": `sum(rate(http_errors_total[5m])) > 1`,
		"severity":          "critical",
		"for":               "5m",
		"description":       "Runbook: check the last deploy.",
		"comment":           "Created during incident triage",
	})
	require.False(t, res.IsError, resultText(res))
	var rule struct {
		model.AlertingRule
		Timeline []mcpTimelineEntry `json:"timeline"`
	}
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &rule))
	assert.Equal(t, model.CRITICAL, rule.Severity)
	assert.Equal(t, int64(300), int64(rule.For))
	assert.Equal(t, model.ApplicationCategoryApplication, rule.NotificationCategory)
	require.Len(t, rule.Timeline, 2)
	assert.Equal(t, "created", rule.Timeline[0].Action)
	assert.Equal(t, "Created during incident triage", rule.Timeline[1].Body)

	// partial update: only enabled
	res = e.call(ctx, e.h.toolUpdateAlertingRule, map[string]any{"id": string(rule.Id), "enabled": false})
	require.False(t, res.IsError, resultText(res))
	stored, err := e.db.GetAlertingRule(e.project.Id, rule.Id)
	require.NoError(t, err)
	assert.False(t, stored.Enabled)
	assert.Equal(t, model.CRITICAL, stored.Severity)
	assert.Equal(t, `sum(rate(http_errors_total[5m])) > 1`, stored.Source.PromQL.Expression)

	res = e.call(ctx, e.h.toolUpdateAlertingRule, map[string]any{"id": string(rule.Id), "promql_expression": "sum(rate(http_errors_total[5m])) > 5", "keep_firing_for": "10m"})
	require.False(t, res.IsError, resultText(res))
	res = e.call(ctx, e.h.toolUpdateAlertingRule, map[string]any{"id": string(rule.Id)})
	assert.True(t, res.IsError, "empty update must be rejected")

	res = e.call(ctx, e.h.toolGetAlertingRule, map[string]any{"id": string(rule.Id)})
	require.False(t, res.IsError, resultText(res))
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &rule))
	require.Len(t, rule.Timeline, 4)
	assert.Equal(t, "disabled", rule.Timeline[2].Action)
	assert.Equal(t, "updated", rule.Timeline[3].Action)
	assert.Equal(t, "keep_firing_for,source", rule.Timeline[3].Meta["changed"])

	// builtin rules: can be tuned/disabled but not deleted, source type is fixed
	res = e.call(ctx, e.h.toolUpdateAlertingRule, map[string]any{"id": "memory-oom", "severity": "critical"})
	require.False(t, res.IsError, resultText(res))
	res = e.call(ctx, e.h.toolUpdateAlertingRule, map[string]any{"id": "memory-oom", "source_type": "promql"})
	assert.True(t, res.IsError)
	res = e.call(ctx, e.h.toolDeleteAlertingRule, map[string]any{"id": "memory-oom"})
	assert.True(t, res.IsError)

	// readonly rules can't be changed
	ro, err := e.db.GetAlertingRule(e.project.Id, "memory-leak")
	require.NoError(t, err)
	ro.Readonly = true
	require.NoError(t, e.db.UpdateAlertingRule(e.project.Id, ro))
	res = e.call(ctx, e.h.toolUpdateAlertingRule, map[string]any{"id": "memory-leak", "enabled": false})
	assert.True(t, res.IsError)

	res = e.call(ctx, e.h.toolListAlertingRules, map[string]any{"search": "error rate"})
	require.False(t, res.IsError, resultText(res))
	assert.Contains(t, resultText(res), `"returned":1`)

	res = e.call(ctx, e.h.toolDeleteAlertingRule, map[string]any{"id": string(rule.Id)})
	require.False(t, res.IsError, resultText(res))
	_, err = e.db.GetAlertingRule(e.project.Id, rule.Id)
	assert.ErrorIs(t, err, db.ErrNotFound)
	comments, err := e.db.GetComments(e.project.Id, db.CommentTargetAlertingRule, string(rule.Id))
	require.NoError(t, err)
	assert.Equal(t, "deleted", comments[len(comments)-1].Meta["action"])

	// viewers can't change rules
	vctx := e.ctx(rbac.RoleViewer, "viewer")
	res = e.call(vctx, e.h.toolUpdateAlertingRule, map[string]any{"id": "memory-oom", "enabled": false})
	assert.True(t, res.IsError)
}

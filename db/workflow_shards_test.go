package db

import (
	"testing"
	"time"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncidentWorkflow(t *testing.T) {
	database := newTestDB(t)
	p := &Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))
	appId := model.NewApplicationId(string(p.Id), "default", model.ApplicationKindDeployment, "api")
	now := timeseries.Now()

	for i, key := range []string{"inc1", "inc2"} {
		opened := now.Add(-timeseries.Hour * timeseries.Duration(i+1))
		require.NoError(t, database.CreateIncident(p.Id, appId, &model.ApplicationIncident{Key: key, OpenedAt: opened, Severity: model.CRITICAL}))
	}
	n, err := database.CountUnacknowledgedIncidents(p.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	wf, err := database.GetIncidentWorkflow(p.Id, "inc1")
	require.NoError(t, err)
	assert.Nil(t, wf)

	wf = &IncidentWorkflow{ProjectId: p.Id, IncidentKey: "inc1", ApplicationId: appId.String(), Status: IncidentStatusAcknowledged,
		Assignee: "alice", AssigneeKind: CommentAuthorUser, AcknowledgedAt: now, AcknowledgedBy: "alice", FollowUps: []string{"add a test"}}
	require.NoError(t, database.SaveIncidentWorkflow(wf))
	n, err = database.CountUnacknowledgedIncidents(p.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// upsert
	wf.Status = IncidentStatusResolved
	wf.ResolvedAt = now
	wf.ResolvedKind = IncidentResolvedByUser
	wf.Resolution = "rolled back"
	require.NoError(t, database.SaveIncidentWorkflow(wf))
	got, err := database.GetIncidentWorkflow(p.Id, "inc1")
	require.NoError(t, err)
	assert.Equal(t, IncidentStatusResolved, got.Status)
	assert.Equal(t, "rolled back", got.Resolution)
	assert.Equal(t, []string{"add a test"}, got.FollowUps)
	assert.Equal(t, "alice", got.Assignee)

	all, err := database.GetIncidentWorkflows(p.Id, []string{"inc1", "inc2"})
	require.NoError(t, err)
	assert.Len(t, all, 1)

	ok, err := database.IncidentResolvedByHumanSince(p.Id, appId, now.Add(-timeseries.Minute))
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = database.IncidentResolvedByHumanSince(p.Id, appId, now.Add(timeseries.Minute))
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, database.SetIncidentResolvedAt(p.Id, "inc1", now))
	i, err := database.GetIncidentByKey(p.Id, "inc1")
	require.NoError(t, err)
	assert.Equal(t, now, i.ResolvedAt)

	similar, err := database.GetApplicationIncidentsSince(p.Id, appId, now.Add(-timeseries.Day), 10)
	require.NoError(t, err)
	assert.Len(t, similar, 2)

	require.NoError(t, database.DeleteProject(p.Id))
}

func TestMaintenanceWindowSchedule(t *testing.T) {
	ts := func(s string) timeseries.Time {
		tt, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return timeseries.TimeFromStandard(tt)
	}
	oneOff := &MaintenanceWindow{Name: "deploy", StartsAt: ts("2026-10-01T10:00:00Z"), EndsAt: ts("2026-10-01T11:00:00Z")}
	require.NoError(t, oneOff.Validate())
	assert.False(t, oneOff.IsActive(ts("2026-10-01T09:59:00Z")))
	assert.True(t, oneOff.IsActive(ts("2026-10-01T10:30:00Z")))
	assert.False(t, oneOff.IsActive(ts("2026-10-01T11:00:00Z")))
	assert.Equal(t, "scheduled", oneOff.Status(ts("2026-10-01T09:00:00Z")))
	assert.Equal(t, "expired", oneOff.Status(ts("2026-10-01T12:00:00Z")))
	oneOff.EndedAt = ts("2026-10-01T10:15:00Z")
	assert.False(t, oneOff.IsActive(ts("2026-10-01T10:30:00Z")))
	assert.Equal(t, "ended", oneOff.Status(ts("2026-10-01T10:30:00Z")))

	// every Saturday and Sunday 23:00-01:00 (Europe/Berlin = UTC+2 in October)
	weekly := &MaintenanceWindow{Name: "backups", StartsAt: ts("2026-09-01T00:00:00Z"),
		Recurrence: &MaintenanceRecurrence{Weekdays: []int{0, 6}, StartTime: "23:00", DurationMinutes: 120, Timezone: "Europe/Berlin"}}
	require.NoError(t, weekly.Validate())
	// 2026-10-03 is a Saturday: 23:00 Berlin = 21:00 UTC
	assert.False(t, weekly.IsActive(ts("2026-10-03T20:59:00Z")))
	assert.True(t, weekly.IsActive(ts("2026-10-03T21:00:00Z")))
	assert.True(t, weekly.IsActive(ts("2026-10-03T22:59:00Z"))) // crosses midnight
	assert.False(t, weekly.IsActive(ts("2026-10-03T23:00:00Z")))
	assert.False(t, weekly.IsActive(ts("2026-10-05T21:30:00Z"))) // Monday
	from, to := weekly.CurrentOrNext(ts("2026-10-01T12:00:00Z"))
	assert.Equal(t, ts("2026-10-03T21:00:00Z"), from)
	assert.Equal(t, ts("2026-10-03T23:00:00Z"), to)
	weekly.EndsAt = ts("2026-10-02T00:00:00Z")
	assert.Equal(t, "expired", weekly.Status(ts("2026-10-03T21:30:00Z")))

	for _, bad := range []*MaintenanceWindow{
		{Name: "", StartsAt: 1, EndsAt: 2},
		{Name: "x", StartsAt: 2, EndsAt: 1},
		{Name: "x", Recurrence: &MaintenanceRecurrence{Weekdays: []int{7}, StartTime: "10:00", DurationMinutes: 10}},
		{Name: "x", Recurrence: &MaintenanceRecurrence{Weekdays: []int{1}, StartTime: "25:00", DurationMinutes: 10}},
		{Name: "x", Recurrence: &MaintenanceRecurrence{Weekdays: []int{1}, StartTime: "10:00", DurationMinutes: 0}},
		{Name: "x", Recurrence: &MaintenanceRecurrence{Weekdays: []int{1}, StartTime: "10:00", DurationMinutes: 10, Timezone: "Mars/Base"}},
	} {
		assert.Error(t, bad.Validate(), bad.Name)
	}
}

func TestMaintenanceScope(t *testing.T) {
	app := model.NewApplicationId("c1", "shop", model.ApplicationKindDeployment, "payments")
	target := MaintenanceTarget{ApplicationId: app, Category: "application", Nodes: []string{"node-1"}, RuleId: "cpu"}
	assert.True(t, MaintenanceScope{}.Matches(target))
	assert.True(t, MaintenanceScope{ApplicationPatterns: []string{"shop:*"}}.Matches(target))
	assert.False(t, MaintenanceScope{ApplicationPatterns: []string{"db:*"}}.Matches(target))
	assert.True(t, MaintenanceScope{Categories: []string{"application", "db"}}.Matches(target))
	assert.True(t, MaintenanceScope{NodePatterns: []string{"node-*"}}.Matches(target))
	assert.False(t, MaintenanceScope{NodePatterns: []string{"node-*"}}.Matches(MaintenanceTarget{ApplicationId: app}))
	assert.True(t, MaintenanceScope{AlertingRuleIds: []string{"cpu"}}.Matches(target))
	// dimensions are AND-ed
	assert.False(t, MaintenanceScope{ApplicationPatterns: []string{"shop:*"}, AlertingRuleIds: []string{"memory"}}.Matches(target))
	assert.False(t, MaintenanceScope{ApplicationPatterns: []string{"*"}}.Matches(MaintenanceTarget{RuleId: "promql"}))
}

func TestMaintenanceWindowCRUDAndMarks(t *testing.T) {
	database := newTestDB(t)
	p := &Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))
	now := timeseries.Now()

	w := &MaintenanceWindow{ProjectId: p.Id, Name: "deploy", StartsAt: now.Add(-timeseries.Minute), EndsAt: now.Add(timeseries.Hour),
		Scope: MaintenanceScope{ApplicationPatterns: []string{"shop:*"}}, CreatedBy: "alice", CreatedByKind: CommentAuthorUser}
	require.NoError(t, database.CreateMaintenanceWindow(w))
	assert.NotZero(t, w.Id)
	require.ErrorIs(t, database.CreateMaintenanceWindow(&MaintenanceWindow{ProjectId: p.Id}), ErrInvalid)

	active, err := database.GetActiveMaintenanceWindows(p.Id, now)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, []string{"shop:*"}, active[0].Scope.ApplicationPatterns)

	w.Name = "deploy v2"
	require.NoError(t, database.UpdateMaintenanceWindow(w))
	got, err := database.GetMaintenanceWindow(p.Id, w.Id)
	require.NoError(t, err)
	assert.Equal(t, "deploy v2", got.Name)

	added, err := database.AddMaintenanceMark(p.Id, &MaintenanceMark{TargetType: CommentTargetAlert, TargetId: "a1", WindowId: w.Id, WindowName: w.Name})
	require.NoError(t, err)
	assert.True(t, added)
	added, err = database.AddMaintenanceMark(p.Id, &MaintenanceMark{TargetType: CommentTargetAlert, TargetId: "a1", WindowId: w.Id})
	require.NoError(t, err)
	assert.False(t, added)
	pending, err := database.GetMaintenanceMarks(p.Id, CommentTargetAlert, true)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	require.NoError(t, database.SetMaintenanceMarkNotified(p.Id, CommentTargetAlert, "a1", MaintenanceMarkSkipped))
	pending, err = database.GetMaintenanceMarks(p.Id, CommentTargetAlert, true)
	require.NoError(t, err)
	assert.Len(t, pending, 0)

	require.NoError(t, database.EndMaintenanceWindow(p.Id, w.Id, "bob", now))
	assert.ErrorIs(t, database.EndMaintenanceWindow(p.Id, w.Id, "bob", now), ErrNotFound)
	active, err = database.GetActiveMaintenanceWindows(p.Id, now)
	require.NoError(t, err)
	assert.Len(t, active, 0)

	require.NoError(t, database.DeleteMaintenanceWindow(p.Id, w.Id))
	require.NoError(t, database.DeleteProject(p.Id))
}

func TestApprovals(t *testing.T) {
	database := newTestDB(t)
	p := &Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))

	a := &Approval{ProjectId: p.Id, Action: AgentActionDeleteAlertingRule, Args: []byte(`{"rule_id":"r1"}`), RequestedBy: "agent-1",
		RequestedByKind: CommentAuthorAgent, RequestedMeta: map[string]string{"via": "mcp"}, TargetType: CommentTargetAlertingRule, TargetId: "r1"}
	require.NoError(t, database.CreateApproval(a))
	n, err := database.CountPendingApprovals(p.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	list, err := database.GetApprovals(p.Id, ApprovalsQuery{Status: ApprovalStatusPending, TargetType: CommentTargetAlertingRule, TargetId: "r1"})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "mcp", list[0].RequestedMeta["via"])
	assert.JSONEq(t, `{"rule_id":"r1"}`, string(list[0].Args))

	require.NoError(t, database.DecideApproval(p.Id, a.Id, ApprovalStatusApproved, "alice", "ok"))
	assert.ErrorIs(t, database.DecideApproval(p.Id, a.Id, ApprovalStatusRejected, "bob", ""), ErrConflict)
	require.NoError(t, database.SetApprovalResult(p.Id, a.Id, ApprovalStatusExecuted, `{"deleted":"r1"}`))
	got, err := database.GetApproval(p.Id, a.Id)
	require.NoError(t, err)
	assert.Equal(t, ApprovalStatusExecuted, got.Status)
	assert.Equal(t, "alice", got.DecidedBy)

	// policy defaults
	var nilPolicy *AgentApprovalPolicy
	assert.Equal(t, ApprovalPolicyApproval, nilPolicy.Effective(AgentActionDeleteAlertingRule))
	assert.Equal(t, ApprovalPolicyApproval, nilPolicy.Effective(AgentActionResolveIncident))
	assert.Equal(t, ApprovalPolicyAuto, nilPolicy.Effective(AgentActionResolveAlerts))
	off := &AgentApprovalPolicy{RequireApproval: false, Actions: map[string]string{AgentActionSuppressAlerts: ApprovalPolicyDeny}}
	assert.Equal(t, ApprovalPolicyAuto, off.Effective(AgentActionDeleteAlertingRule))
	assert.Equal(t, ApprovalPolicyDeny, off.Effective(AgentActionSuppressAlerts))
}

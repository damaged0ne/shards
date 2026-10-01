package watchers

import (
	"testing"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncidentAutoResolveKeepsHumanFields(t *testing.T) {
	database, err := db.NewSqlite(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { _ = database.DB().Close() })
	p := &db.Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))
	w := &Incidents{db: database}
	now := timeseries.Now()
	appId := model.NewApplicationId(string(p.Id), "shop", model.ApplicationKindDeployment, "payments")

	i := &model.ApplicationIncident{ApplicationId: appId, Key: "inc1", OpenedAt: now.Add(-timeseries.Hour), Severity: model.CRITICAL}
	require.NoError(t, database.CreateIncident(p.Id, appId, i))
	require.NoError(t, database.SaveIncidentWorkflow(&db.IncidentWorkflow{ProjectId: p.Id, IncidentKey: "inc1", ApplicationId: appId.String(),
		Status: db.IncidentStatusMitigated, Assignee: "alice", AcknowledgedAt: now, AcknowledgedBy: "alice", Resolution: "draft"}))

	i.ResolvedAt = now
	require.NoError(t, database.ResolveIncident(p.Id, i))
	w.onIncidentAutoResolved(p, i)
	wf, err := database.GetIncidentWorkflow(p.Id, "inc1")
	require.NoError(t, err)
	assert.Equal(t, db.IncidentStatusResolved, wf.Status)
	assert.Equal(t, db.IncidentResolvedByAuto, wf.ResolvedKind)
	assert.Equal(t, "alice", wf.Assignee)
	assert.Equal(t, "alice", wf.AcknowledgedBy)
	assert.Equal(t, "draft", wf.Resolution)
	timeline, err := database.GetComments(p.Id, db.CommentTargetIncident, "inc1")
	require.NoError(t, err)
	require.Len(t, timeline, 1)
	assert.Equal(t, "auto_resolved", timeline[0].Meta["action"])

	// auto-resolving an incident nobody touched creates the workflow row
	i2 := &model.ApplicationIncident{ApplicationId: appId, Key: "inc2", OpenedAt: now.Add(-2 * timeseries.Hour), ResolvedAt: now, Severity: model.WARNING}
	require.NoError(t, database.CreateIncident(p.Id, appId, i2))
	w.onIncidentAutoResolved(p, i2)
	wf, err = database.GetIncidentWorkflow(p.Id, "inc2")
	require.NoError(t, err)
	assert.Equal(t, db.IncidentResolvedByAuto, wf.ResolvedKind)

	// cooldown after a manual resolve
	assert.False(t, w.inHumanResolveCooldown(p, appId, now))
	require.NoError(t, database.SaveIncidentWorkflow(&db.IncidentWorkflow{ProjectId: p.Id, IncidentKey: "inc3", ApplicationId: appId.String(),
		Status: db.IncidentStatusResolved, ResolvedAt: now.Add(-10 * timeseries.Minute), ResolvedKind: db.IncidentResolvedByUser}))
	assert.True(t, w.inHumanResolveCooldown(p, appId, now))
	assert.False(t, w.inHumanResolveCooldown(p, appId, now.Add(HumanResolveCooldown)))
}

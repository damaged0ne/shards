package notifications

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMaintenanceTestEnv(t *testing.T) (*db.DB, *db.Project) {
	database, err := db.NewSqlite(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { _ = database.DB().Close() })
	p := &db.Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))
	dest := db.ApplicationCategoryNotificationDestinations{Webhook: &db.ApplicationCategoryNotificationSettingsWebhook{Enabled: true}}
	p.Settings.ApplicationCategorySettings = map[model.ApplicationCategory]*db.ApplicationCategorySettings{
		model.ApplicationCategoryApplication: {NotificationSettings: db.ApplicationCategoryNotificationSettings{
			Alerts:    db.ApplicationCategoryAlertNotificationSettings{Enabled: true, ApplicationCategoryNotificationDestinations: dest},
			Incidents: db.ApplicationCategoryIncidentNotificationSettings{Enabled: true, ApplicationCategoryNotificationDestinations: dest},
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	alerts := true
	p.Settings.Integrations.Webhook = &db.IntegrationWebhook{Url: srv.URL, Incidents: true, Alerts: &alerts, IncidentTemplate: "{{.Key}}", AlertTemplate: "{{.Summary}}"}
	require.NoError(t, database.SaveProjectSettings(p))
	p, err = database.GetProject(p.Id)
	require.NoError(t, err)
	return database, p
}

func alertNotifications(t *testing.T, database *db.DB, p *db.Project, alertId string) []db.AlertNotification {
	ns, err := database.GetAlertNotificationsByAlertIds(p.Id, []string{alertId})
	require.NoError(t, err)
	return ns[alertId]
}

func TestMaintenanceMutesAlertNotifications(t *testing.T) {
	database, p := newMaintenanceTestEnv(t)
	n := &AlertNotifier{db: database}
	now := timeseries.Now()
	rule := &model.AlertingRule{Id: "cpu", Name: "CPU"}
	payments := model.NewApplicationId(string(p.Id), "shop", model.ApplicationKindDeployment, "payments")
	catalog := model.NewApplicationId(string(p.Id), "shop", model.ApplicationKindDeployment, "catalog")
	newAlert := func(id string, app model.ApplicationId) *model.Alert {
		a := &model.Alert{Id: id, Fingerprint: id, RuleId: "cpu", ApplicationId: app, ApplicationCategory: model.ApplicationCategoryApplication, Severity: model.WARNING, Summary: id}
		require.NoError(t, database.CreateAlert(p.Id, a))
		return a
	}

	w := &db.MaintenanceWindow{ProjectId: p.Id, Name: "payments deploy", StartsAt: now.Add(-timeseries.Minute), EndsAt: now.Add(timeseries.Hour),
		Scope: db.MaintenanceScope{ApplicationPatterns: []string{"shop:Deployment:payments"}}}
	require.NoError(t, database.CreateMaintenanceWindow(w))

	// in scope: created, but not notified; marked and recorded in the timeline
	a1 := newAlert("a1", payments)
	n.Enqueue(p, nil, a1, rule, now)
	assert.Len(t, alertNotifications(t, database, p, "a1"), 0)
	mark, err := database.GetMaintenanceMark(p.Id, db.CommentTargetAlert, "a1")
	require.NoError(t, err)
	require.NotNil(t, mark)
	assert.Equal(t, "payments deploy", mark.WindowName)
	timeline, err := database.GetComments(p.Id, db.CommentTargetAlert, "a1")
	require.NoError(t, err)
	require.Len(t, timeline, 1)
	assert.Equal(t, "muted", timeline[0].Meta["action"])

	// out of scope: notified as usual
	a2 := newAlert("a2", catalog)
	n.Enqueue(p, nil, a2, rule, now)
	assert.Len(t, alertNotifications(t, database, p, "a2"), 1)

	// a muted alert that resolves is never announced, even after the window ends
	a3 := newAlert("a3", payments)
	n.Enqueue(p, nil, a3, rule, now)
	a3.ResolvedAt = now
	require.NoError(t, database.ResolveAlert(p.Id, "a3", now))
	n.Enqueue(p, nil, a3, rule, now)
	assert.Len(t, alertNotifications(t, database, p, "a3"), 0)

	// manual resolve of a muted alert: no "resolved" notification either
	a4 := newAlert("a4", payments)
	n.Enqueue(p, nil, a4, rule, now)
	EnqueueResolvedAlerts(database, p, []*model.Alert{a4}, rule)
	assert.Len(t, alertNotifications(t, database, p, "a4"), 0)

	// release while the window is active: nothing happens
	require.NoError(t, database.CreateAlertingRule(p.Id, rule))
	n.ReleaseMaintenance(p, nil, now)
	assert.Len(t, alertNotifications(t, database, p, "a1"), 0)

	// the window ends: the still firing alert is notified once, resolved ones are not
	require.NoError(t, database.EndMaintenanceWindow(p.Id, w.Id, "alice", now))
	n.ReleaseMaintenance(p, nil, now)
	assert.Len(t, alertNotifications(t, database, p, "a1"), 1)
	assert.Len(t, alertNotifications(t, database, p, "a3"), 0)
	n.ReleaseMaintenance(p, nil, now)
	assert.Len(t, alertNotifications(t, database, p, "a1"), 1)
	mark, err = database.GetMaintenanceMark(p.Id, db.CommentTargetAlert, "a1")
	require.NoError(t, err)
	assert.Equal(t, db.MaintenanceMarkNotified, mark.Notified)

	// ... and its resolve notification goes out normally
	a1.ResolvedAt = now
	n.Enqueue(p, nil, a1, rule, now)
	ns := alertNotifications(t, database, p, "a1")
	require.Len(t, ns, 2)
}

func TestMaintenanceMutesIncidentNotifications(t *testing.T) {
	database, p := newMaintenanceTestEnv(t)
	n := &IncidentNotifier{db: database}
	now := timeseries.Now()
	appId := model.NewApplicationId(string(p.Id), "shop", model.ApplicationKindDeployment, "payments")
	app := &model.Application{Id: appId, Category: model.ApplicationCategoryApplication}
	count := func(key string) int {
		var c int
		require.NoError(t, database.DB().QueryRow("SELECT count(*) FROM incident_notification WHERE incident_key = $1", key).Scan(&c))
		return c
	}

	w := &db.MaintenanceWindow{ProjectId: p.Id, Name: "everything", StartsAt: now.Add(-timeseries.Minute), EndsAt: now.Add(timeseries.Hour)}
	require.NoError(t, database.CreateMaintenanceWindow(w))
	i := &model.ApplicationIncident{ApplicationId: appId, Key: "inc1", OpenedAt: now, Severity: model.CRITICAL}
	require.NoError(t, database.CreateIncident(p.Id, appId, i))
	n.Enqueue(p, app, i, now)
	assert.Equal(t, 0, count("inc1"))

	world := model.NewWorld(now.Add(-timeseries.Hour), now, timeseries.Minute, timeseries.Minute)
	world.Applications = map[model.ApplicationId]*model.Application{appId: app}
	n.ReleaseMaintenance(p, world, now)
	assert.Equal(t, 0, count("inc1"))

	require.NoError(t, database.EndMaintenanceWindow(p.Id, w.Id, "alice", now))
	n.ReleaseMaintenance(p, world, now)
	assert.Equal(t, 1, count("inc1"))

	// manual resolve (EnqueueIncidentResolved) notifies as the incident was announced
	i.ResolvedAt = now
	EnqueueIncidentResolved(database, p, app, i)
	assert.Equal(t, 2, count("inc1"))
}

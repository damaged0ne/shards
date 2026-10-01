package notifications

import (
	"testing"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Telegram/Discord/Mattermost/Email channels are enqueued after the maintenance check, so they
// are muted like the upstream ones, and notified once the window ends.
func TestMaintenanceMutesShardsChannels(t *testing.T) {
	database, err := db.NewSqlite(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { _ = database.DB().Close() })
	p := &db.Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))

	c := &capture{}
	srv := c.server(t)
	defer srv.Close()
	p.Settings.Integrations.BaseUrl = "http://shards"
	p.Settings.Integrations.Discord = &db.IntegrationDiscord{WebhookUrl: srv.URL, ShardsNotificationSettings: db.ShardsNotificationSettings{Alerts: true}}
	require.NoError(t, database.SaveProjectSettings(p))
	p, err = database.GetProject(p.Id)
	require.NoError(t, err)

	n := &AlertNotifier{db: database}
	now := timeseries.Now()
	rule := &model.AlertingRule{Id: "probe-test", Name: "Probe is failing"}
	require.NoError(t, database.CreateAlertingRule(p.Id, rule))
	app := model.NewApplicationId(string(p.Id), "shop", model.ApplicationKindDeployment, "api")
	alert := &model.Alert{Id: "a1", Fingerprint: "a1", RuleId: "probe-test", ApplicationId: app, ApplicationCategory: model.ApplicationCategoryApplication, Severity: model.CRITICAL, Summary: "1 probe is failing"}
	require.NoError(t, database.CreateAlert(p.Id, alert))

	w := &db.MaintenanceWindow{ProjectId: p.Id, Name: "api deploy", StartsAt: now.Add(-timeseries.Minute), EndsAt: now.Add(timeseries.Hour),
		Scope: db.MaintenanceScope{ApplicationPatterns: []string{"shop:Deployment:api"}}}
	require.NoError(t, database.CreateMaintenanceWindow(w))

	n.Enqueue(p, nil, alert, rule, now)
	assert.Len(t, alertNotifications(t, database, p, "a1"), 0)
	assert.Len(t, c.bodies, 0, "nothing is sent to Discord during the window")

	require.NoError(t, database.EndMaintenanceWindow(p.Id, w.Id, "alice", now))
	n.ReleaseMaintenance(p, nil, now)
	ns := alertNotifications(t, database, p, "a1")
	require.Len(t, ns, 1)
	assert.Equal(t, db.IntegrationTypeDiscord, ns[0].Destination.IntegrationType)
	assert.Len(t, c.bodies, 1, "the still firing alert is sent once the window ends")
}

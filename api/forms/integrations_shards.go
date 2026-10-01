package forms

import (
	"context"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/notifications"
	"github.com/coroot/coroot/timeseries"
)

// Shards fork: forms of the Telegram, Discord, Mattermost and Email notification channels.

const hidden = "<hidden>"

func newIntegrationFormShards(t db.IntegrationType) IntegrationForm {
	switch t {
	case db.IntegrationTypeTelegram:
		return &IntegrationFormTelegram{}
	case db.IntegrationTypeDiscord:
		return &IntegrationFormDiscord{}
	case db.IntegrationTypeMattermost:
		return &IntegrationFormMattermost{}
	case db.IntegrationTypeEmail:
		return &IntegrationFormEmail{}
	}
	return nil
}

func defaultShardsSettings() db.ShardsNotificationSettings {
	return db.ShardsNotificationSettings{Incidents: true, Alerts: true}
}

// sendTestShards sends a test incident and/or alert depending on what the channel is enabled for.
func sendTestShards(ctx context.Context, project *db.Project, client notifications.ShardsClient, s db.ShardsNotificationSettings) error {
	baseUrl := project.Settings.Integrations.BaseUrl
	if s.Alerts || !s.Incidents {
		if err := client.SendAlert(ctx, baseUrl, testAlertNotification(project)); err != nil {
			return err
		}
	}
	if s.Incidents {
		return client.SendIncident(ctx, baseUrl, testIncidentNotification(project))
	}
	return nil
}

func testAlertNotification(project *db.Project) *db.AlertNotification {
	return &db.AlertNotification{
		ProjectId:     project.Id,
		AlertId:       "test-alert",
		RuleId:        "probe-down",
		ApplicationId: model.NewApplicationId(string(project.Id), "default", model.ApplicationKindDeployment, "fake-app"),
		Status:        model.CRITICAL,
		Timestamp:     timeseries.Now(),
		Details: &db.AlertNotificationDetails{
			ProjectName: project.Name,
			RuleName:    "Probe is failing",
			Severity:    model.CRITICAL.String(),
			Summary:     "1 probe is failing",
			Details: []model.AlertDetail{
				{Name: "Description", Value: "This is a test notification sent from shards."},
				{Name: "Findings", Value: "fake-app-health: 2 failed runs in a row: unexpected status code: 503 (expected 200-399)"},
			},
		},
	}
}

type IntegrationFormTelegram struct {
	db.IntegrationTelegram
}

func (f *IntegrationFormTelegram) Valid() bool {
	return f.Validate() == nil
}

func (f *IntegrationFormTelegram) Get(project *db.Project, masked bool) {
	cfg := project.Settings.Integrations.Telegram
	if cfg == nil {
		f.ShardsNotificationSettings = defaultShardsSettings()
		return
	}
	f.IntegrationTelegram = *cfg
	if masked {
		f.BotToken = hidden
	}
}

func (f *IntegrationFormTelegram) Update(ctx context.Context, project *db.Project, clear bool) error {
	cfg := &f.IntegrationTelegram
	if clear {
		cfg = nil
	}
	project.Settings.Integrations.Telegram = cfg
	return nil
}

func (f *IntegrationFormTelegram) Test(ctx context.Context, project *db.Project) error {
	return sendTestShards(ctx, project, notifications.NewTelegram(&f.IntegrationTelegram), f.ShardsNotificationSettings)
}

type IntegrationFormDiscord struct {
	db.IntegrationDiscord
}

func (f *IntegrationFormDiscord) Valid() bool {
	return f.Validate() == nil
}

func (f *IntegrationFormDiscord) Get(project *db.Project, masked bool) {
	cfg := project.Settings.Integrations.Discord
	if cfg == nil {
		f.ShardsNotificationSettings = defaultShardsSettings()
		return
	}
	f.IntegrationDiscord = *cfg
	if masked {
		f.WebhookUrl = hidden
	}
}

func (f *IntegrationFormDiscord) Update(ctx context.Context, project *db.Project, clear bool) error {
	cfg := &f.IntegrationDiscord
	if clear {
		cfg = nil
	}
	project.Settings.Integrations.Discord = cfg
	return nil
}

func (f *IntegrationFormDiscord) Test(ctx context.Context, project *db.Project) error {
	return sendTestShards(ctx, project, notifications.NewDiscord(&f.IntegrationDiscord), f.ShardsNotificationSettings)
}

type IntegrationFormMattermost struct {
	db.IntegrationMattermost
}

func (f *IntegrationFormMattermost) Valid() bool {
	return f.Validate() == nil
}

func (f *IntegrationFormMattermost) Get(project *db.Project, masked bool) {
	cfg := project.Settings.Integrations.Mattermost
	if cfg == nil {
		f.ShardsNotificationSettings = defaultShardsSettings()
		return
	}
	f.IntegrationMattermost = *cfg
	if masked {
		f.WebhookUrl = hidden
	}
}

func (f *IntegrationFormMattermost) Update(ctx context.Context, project *db.Project, clear bool) error {
	cfg := &f.IntegrationMattermost
	if clear {
		cfg = nil
	}
	project.Settings.Integrations.Mattermost = cfg
	return nil
}

func (f *IntegrationFormMattermost) Test(ctx context.Context, project *db.Project) error {
	return sendTestShards(ctx, project, notifications.NewMattermost(&f.IntegrationMattermost), f.ShardsNotificationSettings)
}

type IntegrationFormEmail struct {
	db.IntegrationEmail
}

func (f *IntegrationFormEmail) Valid() bool {
	return f.Validate() == nil
}

func (f *IntegrationFormEmail) Get(project *db.Project, masked bool) {
	cfg := project.Settings.Integrations.Email
	if cfg == nil {
		f.ShardsNotificationSettings = defaultShardsSettings()
		f.Port = 587
		f.TLSMode = db.EmailTLSModeStartTLS
		return
	}
	f.IntegrationEmail = *cfg
	if masked {
		f.Host = hidden
		f.Username = hidden
		f.Password = hidden
	}
}

func (f *IntegrationFormEmail) Update(ctx context.Context, project *db.Project, clear bool) error {
	cfg := &f.IntegrationEmail
	if clear {
		cfg = nil
	}
	project.Settings.Integrations.Email = cfg
	return nil
}

func (f *IntegrationFormEmail) Test(ctx context.Context, project *db.Project) error {
	return sendTestShards(ctx, project, notifications.NewEmail(&f.IntegrationEmail), f.ShardsNotificationSettings)
}

// sendCategoryTestAlert sends a test alert to the channel selected in the alert notification settings
// of an application category (the UI sends {"test": {"alert": {"<channel>": {...}}}}).
func sendCategoryTestAlert(ctx context.Context, project *db.Project, d *db.ApplicationCategoryNotificationDestinations) error {
	integrations := project.Settings.Integrations
	var client notifications.NotificationClient
	switch {
	case d.Slack != nil && integrations.Slack != nil:
		channel := d.Slack.Channel
		if channel == "" {
			channel = integrations.Slack.DefaultChannel
		}
		client = notifications.NewSlack(integrations.Slack.Token, channel)
	case d.Teams != nil && integrations.Teams != nil:
		client = notifications.NewTeams(integrations.Teams.GetWebhookUrl(d.Teams.Channel))
	case d.Pagerduty != nil && integrations.Pagerduty != nil:
		client = notifications.NewPagerduty(integrations.Pagerduty.IntegrationKey)
	case d.Opsgenie != nil && integrations.Opsgenie != nil:
		client = notifications.NewOpsgenie(integrations.Opsgenie.ApiKey, integrations.Opsgenie.EUInstance)
	case d.Webhook != nil && integrations.Webhook != nil:
		client = notifications.NewWebhook(integrations.Webhook)
	default:
		client = testClientShards(d, integrations)
	}
	if client == nil {
		return nil
	}
	return client.SendAlert(ctx, integrations.BaseUrl, testAlertNotification(project))
}

// testClientShards returns a client for the test notification of an application category.
func testClientShards(d *db.ApplicationCategoryNotificationDestinations, integrations db.Integrations) notifications.NotificationClient {
	if d == nil {
		return nil
	}
	for _, dest := range []struct {
		t   db.IntegrationType
		set bool
	}{
		{db.IntegrationTypeTelegram, d.Telegram != nil},
		{db.IntegrationTypeDiscord, d.Discord != nil},
		{db.IntegrationTypeMattermost, d.Mattermost != nil},
		{db.IntegrationTypeEmail, d.Email != nil},
	} {
		if dest.set {
			if c := notifications.NewShardsClient(dest.t, &integrations.ShardsNotificationIntegrations); c != nil {
				return c
			}
		}
	}
	return nil
}

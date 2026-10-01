package db

import (
	"encoding/json"
	"testing"

	"github.com/coroot/coroot/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestShardsNotificationIntegrations(t *testing.T) {
	p := &Project{}
	ni := &p.Settings.Integrations.NotificationIntegrations
	ni.BaseUrl = "http://shards"
	ni.Telegram = &IntegrationTelegram{BotToken: "t", ChatId: "-100", ShardsNotificationSettings: ShardsNotificationSettings{Incidents: true, Alerts: true}}
	ni.Email = &IntegrationEmail{Host: "smtp", From: "a@example.com", To: []string{"b@example.com"}, ShardsNotificationSettings: ShardsNotificationSettings{Alerts: true}}
	require.NoError(t, ni.Validate())
	assert.Equal(t, 587, ni.Email.Port)
	assert.Equal(t, EmailTLSModeStartTLS, ni.Email.TLSMode)

	ni.Telegram.ChatId = "channel"
	assert.ErrorContains(t, ni.Validate(), "invalid telegram configuration")
	ni.Telegram.ChatId = "@channel"
	require.NoError(t, ni.Validate())

	// JSON (project settings) and YAML (config file) round trips
	data, err := json.Marshal(p.Settings.Integrations)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"telegram":{"bot_token":"t","chat_id":"@channel","incidents":true,"alerts":true,"comments":false}`)
	var cfg NotificationIntegrations
	require.NoError(t, yaml.Unmarshal([]byte("baseURL: http://x\ndiscord:\n  webhookURL: https://discord.com/api/webhooks/1/x\n  alerts: true\n  comments: true\n"), &cfg))
	require.NotNil(t, cfg.Discord)
	assert.True(t, cfg.Discord.Alerts)
	assert.True(t, cfg.Discord.Comments)

	var titles []string
	for _, i := range p.Settings.Integrations.GetInfo() {
		titles = append(titles, i.Title)
		if i.Type == IntegrationTypeEmail {
			assert.True(t, i.Configured)
			assert.False(t, i.Incidents)
			assert.True(t, i.Alerts)
			assert.Equal(t, "to: b@example.com", i.Details)
		}
	}
	assert.Equal(t, []string{"Slack", "MS Teams", "Pagerduty", "Opsgenie", "Webhook", "Telegram", "Discord", "Mattermost", "Email"}, titles)

	// the configured channels are enabled for the categories by default
	c := p.GetApplicationCategories()[model.ApplicationCategoryApplication]
	require.NotNil(t, c)
	ns := c.NotificationSettings
	assert.True(t, ns.Incidents.Enabled)
	require.NotNil(t, ns.Incidents.Telegram)
	assert.True(t, ns.Incidents.Telegram.Enabled)
	assert.Nil(t, ns.Incidents.Email)
	require.NotNil(t, ns.Alerts.Email)
	assert.Equal(t, []IncidentNotificationDestination{{IntegrationType: IntegrationTypeTelegram}, {IntegrationType: IntegrationTypeEmail}}, ns.Alerts.ShardsNotificationDestinations.Enabled())

	// explicitly disabled for a category
	p.Settings.ApplicationCategorySettings = map[model.ApplicationCategory]*ApplicationCategorySettings{
		model.ApplicationCategoryApplication: {NotificationSettings: ApplicationCategoryNotificationSettings{
			Alerts: ApplicationCategoryAlertNotificationSettings{Enabled: true, ApplicationCategoryNotificationDestinations: ApplicationCategoryNotificationDestinations{
				ShardsNotificationDestinations: ShardsNotificationDestinations{Telegram: &ApplicationCategoryNotificationSettingsShards{Enabled: false}},
			}},
		}},
	}
	ns = p.GetApplicationCategories()[model.ApplicationCategoryApplication].NotificationSettings
	assert.Equal(t, []IncidentNotificationDestination{{IntegrationType: IntegrationTypeEmail}}, ns.Alerts.ShardsNotificationDestinations.Enabled())
}

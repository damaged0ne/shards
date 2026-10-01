package api

import "github.com/coroot/coroot/db"

// shardsIntegrationTitle is the display name of a notification channel added by the fork.
func shardsIntegrationTitle(t db.IntegrationType) string {
	switch t {
	case db.IntegrationTypeTelegram:
		return "Telegram"
	case db.IntegrationTypeDiscord:
		return "Discord"
	case db.IntegrationTypeMattermost:
		return "Mattermost"
	case db.IntegrationTypeEmail:
		return "Email"
	}
	return string(t)
}

package db

import (
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

// Shards fork: Telegram, Discord, Mattermost and Email notification channels.
// They support incidents, alerts and (optionally) timeline comments; deployments are not supported.

const (
	IntegrationTypeTelegram   IntegrationType = "telegram"
	IntegrationTypeDiscord    IntegrationType = "discord"
	IntegrationTypeMattermost IntegrationType = "mattermost"
	IntegrationTypeEmail      IntegrationType = "email"
)

// ShardsIntegrationTypes are the notification channels added by the fork, in the display order.
var ShardsIntegrationTypes = []IntegrationType{IntegrationTypeTelegram, IntegrationTypeDiscord, IntegrationTypeMattermost, IntegrationTypeEmail}

type ShardsNotificationIntegrations struct {
	Telegram   *IntegrationTelegram   `json:"telegram,omitempty" yaml:"telegram,omitempty"`
	Discord    *IntegrationDiscord    `json:"discord,omitempty" yaml:"discord,omitempty"`
	Mattermost *IntegrationMattermost `json:"mattermost,omitempty" yaml:"mattermost,omitempty"`
	Email      *IntegrationEmail      `json:"email,omitempty" yaml:"email,omitempty"`
}

// ShardsNotificationSettings are the common settings of the fork channels.
type ShardsNotificationSettings struct {
	Incidents bool `json:"incidents" yaml:"incidents"`
	Alerts    bool `json:"alerts" yaml:"alerts"`
	// Comments forwards human/agent comments on incidents and alerts.
	Comments bool `json:"comments" yaml:"comments"`
}

func (s ShardsNotificationSettings) Enabled(incidents bool) bool {
	if incidents {
		return s.Incidents
	}
	return s.Alerts
}

type IntegrationTelegram struct {
	BotToken        string `json:"bot_token" yaml:"botToken"`
	ChatId          string `json:"chat_id" yaml:"chatId"`
	MessageThreadId int    `json:"message_thread_id,omitempty" yaml:"messageThreadId,omitempty"`

	ShardsNotificationSettings `yaml:",inline"`
}

func (i *IntegrationTelegram) Validate() error {
	if i.BotToken == "" {
		return fmt.Errorf("bot token is required")
	}
	if i.ChatId == "" {
		return fmt.Errorf("chat id is required")
	}
	if !strings.HasPrefix(i.ChatId, "@") {
		if _, err := strconv.ParseInt(i.ChatId, 10, 64); err != nil {
			return fmt.Errorf("chat id must be a number or @channelusername")
		}
	}
	if i.MessageThreadId < 0 {
		return fmt.Errorf("invalid message thread id")
	}
	return nil
}

type IntegrationDiscord struct {
	WebhookUrl string `json:"webhook_url" yaml:"webhookURL"`

	ShardsNotificationSettings `yaml:",inline"`
}

func (i *IntegrationDiscord) Validate() error {
	return validateWebhookUrl(i.WebhookUrl)
}

type IntegrationMattermost struct {
	WebhookUrl string `json:"webhook_url" yaml:"webhookURL"`
	// Channel overrides the default channel of the incoming webhook (if the webhook allows it).
	Channel  string `json:"channel,omitempty" yaml:"channel,omitempty"`
	Username string `json:"username,omitempty" yaml:"username,omitempty"`

	ShardsNotificationSettings `yaml:",inline"`
}

func (i *IntegrationMattermost) Validate() error {
	return validateWebhookUrl(i.WebhookUrl)
}

type EmailTLSMode string

const (
	EmailTLSModeStartTLS EmailTLSMode = "starttls"
	EmailTLSModeTLS      EmailTLSMode = "tls" // implicit TLS (SMTPS, usually port 465)
	EmailTLSModeNone     EmailTLSMode = "none"
)

type IntegrationEmail struct {
	Host          string       `json:"host" yaml:"host"`
	Port          int          `json:"port" yaml:"port"`
	TLSMode       EmailTLSMode `json:"tls_mode" yaml:"tlsMode"`
	TlsSkipVerify bool         `json:"tls_skip_verify,omitempty" yaml:"tlsSkipVerify,omitempty"`
	Username      string       `json:"username,omitempty" yaml:"username,omitempty"`
	Password      string       `json:"password,omitempty" yaml:"password,omitempty"`
	From          string       `json:"from" yaml:"from"`
	To            []string     `json:"to" yaml:"to"`

	ShardsNotificationSettings `yaml:",inline"`
}

func (i *IntegrationEmail) Validate() error {
	if i.Host == "" {
		return fmt.Errorf("host is required")
	}
	if i.Port == 0 {
		switch i.TLSMode {
		case EmailTLSModeTLS:
			i.Port = 465
		default:
			i.Port = 587
		}
	}
	if i.Port < 1 || i.Port > 65535 {
		return fmt.Errorf("invalid port")
	}
	switch i.TLSMode {
	case "":
		i.TLSMode = EmailTLSModeStartTLS
	case EmailTLSModeStartTLS, EmailTLSModeTLS, EmailTLSModeNone:
	default:
		return fmt.Errorf("invalid TLS mode: %s", i.TLSMode)
	}
	if _, err := mail.ParseAddress(i.From); err != nil {
		return fmt.Errorf("invalid from address: %s", i.From)
	}
	var to []string
	for _, a := range i.To {
		for _, part := range strings.Split(a, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, err := mail.ParseAddress(part); err != nil {
				return fmt.Errorf("invalid recipient address: %s", part)
			}
			to = append(to, part)
		}
	}
	if len(to) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}
	i.To = to
	return nil
}

func validateWebhookUrl(s string) error {
	if s == "" {
		return fmt.Errorf("webhook url is required")
	}
	if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid webhook url")
	}
	return nil
}

func (i *ShardsNotificationIntegrations) validate() error {
	if i.Telegram != nil {
		if err := i.Telegram.Validate(); err != nil {
			return fmt.Errorf("invalid telegram configuration: %w", err)
		}
	}
	if i.Discord != nil {
		if err := i.Discord.Validate(); err != nil {
			return fmt.Errorf("invalid discord configuration: %w", err)
		}
	}
	if i.Mattermost != nil {
		if err := i.Mattermost.Validate(); err != nil {
			return fmt.Errorf("invalid mattermost configuration: %w", err)
		}
	}
	if i.Email != nil {
		if err := i.Email.Validate(); err != nil {
			return fmt.Errorf("invalid email configuration: %w", err)
		}
	}
	return nil
}

// Settings returns the common settings of a configured channel, or nil.
func (i *ShardsNotificationIntegrations) Settings(t IntegrationType) *ShardsNotificationSettings {
	switch t {
	case IntegrationTypeTelegram:
		if i.Telegram != nil {
			return &i.Telegram.ShardsNotificationSettings
		}
	case IntegrationTypeDiscord:
		if i.Discord != nil {
			return &i.Discord.ShardsNotificationSettings
		}
	case IntegrationTypeMattermost:
		if i.Mattermost != nil {
			return &i.Mattermost.ShardsNotificationSettings
		}
	case IntegrationTypeEmail:
		if i.Email != nil {
			return &i.Email.ShardsNotificationSettings
		}
	}
	return nil
}

func (i *ShardsNotificationIntegrations) info() []IntegrationInfo {
	var res []IntegrationInfo
	for _, t := range ShardsIntegrationTypes {
		info := IntegrationInfo{Type: t}
		switch t {
		case IntegrationTypeTelegram:
			info.Title = "Telegram"
			if i.Telegram != nil {
				info.Details = "chat: " + i.Telegram.ChatId
				if i.Telegram.MessageThreadId > 0 {
					info.Details += fmt.Sprintf(", topic: %d", i.Telegram.MessageThreadId)
				}
			}
		case IntegrationTypeDiscord:
			info.Title = "Discord"
		case IntegrationTypeMattermost:
			info.Title = "Mattermost"
			if i.Mattermost != nil && i.Mattermost.Channel != "" {
				info.Details = "channel: " + i.Mattermost.Channel
			}
		case IntegrationTypeEmail:
			info.Title = "Email"
			if i.Email != nil {
				info.Details = "to: " + strings.Join(i.Email.To, ", ")
			}
		}
		if s := i.Settings(t); s != nil {
			info.Configured = true
			info.Incidents = s.Incidents
			info.Alerts = s.Alerts
		}
		res = append(res, info)
	}
	return res
}

// ApplicationCategoryNotificationSettingsShards enables a fork channel for a category.
type ApplicationCategoryNotificationSettingsShards struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

type ShardsNotificationDestinations struct {
	Telegram   *ApplicationCategoryNotificationSettingsShards `json:"telegram,omitempty" yaml:"telegram,omitempty"`
	Discord    *ApplicationCategoryNotificationSettingsShards `json:"discord,omitempty" yaml:"discord,omitempty"`
	Mattermost *ApplicationCategoryNotificationSettingsShards `json:"mattermost,omitempty" yaml:"mattermost,omitempty"`
	Email      *ApplicationCategoryNotificationSettingsShards `json:"email,omitempty" yaml:"email,omitempty"`
}

func (d *ShardsNotificationDestinations) get(t IntegrationType) **ApplicationCategoryNotificationSettingsShards {
	switch t {
	case IntegrationTypeTelegram:
		return &d.Telegram
	case IntegrationTypeDiscord:
		return &d.Discord
	case IntegrationTypeMattermost:
		return &d.Mattermost
	case IntegrationTypeEmail:
		return &d.Email
	}
	return nil
}

func (d ShardsNotificationDestinations) hasEnabled() bool {
	for _, t := range ShardsIntegrationTypes {
		if s := *d.get(t); s != nil && s.Enabled {
			return true
		}
	}
	return false
}

// Enabled returns the destinations of the enabled fork channels.
func (d ShardsNotificationDestinations) Enabled() []IncidentNotificationDestination {
	var res []IncidentNotificationDestination
	for _, t := range ShardsIntegrationTypes {
		if s := *d.get(t); s != nil && s.Enabled {
			res = append(res, IncidentNotificationDestination{IntegrationType: t})
		}
	}
	return res
}

// applyDefaults mirrors the logic for the upstream channels in GetApplicationCategories:
// a configured channel is enabled for every category unless it was explicitly disabled.
func (d *ShardsNotificationDestinations) applyDefaults(integrations *ShardsNotificationIntegrations, incidents bool, categoryEnabled *bool) {
	for _, t := range ShardsIntegrationTypes {
		dest := d.get(t)
		s := integrations.Settings(t)
		if s == nil || !s.Enabled(incidents) {
			*dest = nil
			continue
		}
		if *dest == nil {
			*categoryEnabled = true
			*dest = &ApplicationCategoryNotificationSettingsShards{Enabled: true}
		}
	}
}

func (d *ShardsNotificationDestinations) newDefaults(integrations *ShardsNotificationIntegrations, incidents bool) {
	for _, t := range ShardsIntegrationTypes {
		if s := integrations.Settings(t); s != nil && s.Enabled(incidents) {
			*d.get(t) = &ApplicationCategoryNotificationSettingsShards{}
		}
	}
}

func (category *ApplicationCategory) applyShardsDefaults(integrations *ShardsNotificationIntegrations) {
	ns := &category.NotificationSettings
	ns.Incidents.ShardsNotificationDestinations.applyDefaults(integrations, true, &ns.Incidents.Enabled)
	ns.Alerts.ShardsNotificationDestinations.applyDefaults(integrations, false, &ns.Alerts.Enabled)
	ns.Deployments.ShardsNotificationDestinations = ShardsNotificationDestinations{}
}

func (category *ApplicationCategory) newShardsDefaults(integrations *ShardsNotificationIntegrations) {
	ns := &category.NotificationSettings
	ns.Incidents.ShardsNotificationDestinations.newDefaults(integrations, true)
	ns.Alerts.ShardsNotificationDestinations.newDefaults(integrations, false)
}

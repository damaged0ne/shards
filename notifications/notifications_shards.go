package notifications

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"k8s.io/klog"
)

// Shards fork: Telegram, Discord, Mattermost and Email channels. They render the same channel-neutral
// message built from an incident, alert or comment notification.

const commentSendTimeout = 15 * time.Second

func getClientShards(destination db.IncidentNotificationDestination, integrations db.Integrations, notificationType NotificationType) NotificationClient {
	s := integrations.ShardsNotificationIntegrations.Settings(destination.IntegrationType)
	if s == nil || !s.Enabled(notificationType == NotificationTypeIncident) {
		return nil
	}
	return NewShardsClient(destination.IntegrationType, &integrations.ShardsNotificationIntegrations)
}

// ShardsClient is implemented by the fork channels.
type ShardsClient interface {
	NotificationClient
	SendComment(ctx context.Context, values CommentTemplateValues) error
}

func NewShardsClient(t db.IntegrationType, cfg *db.ShardsNotificationIntegrations) ShardsClient {
	switch t {
	case db.IntegrationTypeTelegram:
		if cfg.Telegram != nil {
			return NewTelegram(cfg.Telegram)
		}
	case db.IntegrationTypeDiscord:
		if cfg.Discord != nil {
			return NewDiscord(cfg.Discord)
		}
	case db.IntegrationTypeMattermost:
		if cfg.Mattermost != nil {
			return NewMattermost(cfg.Mattermost)
		}
	case db.IntegrationTypeEmail:
		if cfg.Email != nil {
			return NewEmail(cfg.Email)
		}
	}
	return nil
}

// ForwardCommentShards sends a timeline comment to the fork channels that have comments enabled (asynchronously).
func ForwardCommentShards(integrations db.Integrations, values CommentTemplateValues) {
	values.Event = "comment"
	for _, t := range db.ShardsIntegrationTypes {
		s := integrations.ShardsNotificationIntegrations.Settings(t)
		if s == nil || !s.Comments {
			continue
		}
		client := NewShardsClient(t, &integrations.ShardsNotificationIntegrations)
		go func(t db.IntegrationType) {
			ctx, cancel := context.WithTimeout(context.Background(), commentSendTimeout)
			defer cancel()
			if err := client.SendComment(ctx, values); err != nil {
				klog.Errorf("failed to forward a comment to %s: %s", t, err)
			}
		}(t)
	}
}

func enqueueResolvedAlertShards(database *db.DB, now timeseries.Time, project *db.Project, alert *model.Alert, settings db.ApplicationCategoryAlertNotificationSettings, details *db.AlertNotificationDetails) {
	for _, d := range settings.ShardsNotificationDestinations.Enabled() {
		database.PutAlertNotification(db.AlertNotification{
			ProjectId:     project.Id,
			AlertId:       alert.Id,
			RuleId:        alert.RuleId,
			ApplicationId: alert.ApplicationId,
			Destination:   d,
			Timestamp:     now,
			Status:        model.OK,
			Details:       details,
		})
	}
}

type messageField struct {
	Name  string
	Value string
	Code  bool
}

// message is a channel-neutral notification.
type message struct {
	Status  model.Status
	Title   string
	Url     string
	UrlText string
	Lines   []string
	Fields  []messageField
	Footer  string
}

func (m *message) severityColor() int {
	var c int
	_, _ = fmt.Sscanf(strings.TrimPrefix(m.Status.Color(), "#"), "%x", &c)
	return c
}

func incidentMessage(baseUrl string, n *db.IncidentNotification) *message {
	m := &message{Status: n.Status, Url: incidentUrl(baseUrl, n), UrlText: "View incident", Footer: "shards incident " + n.IncidentKey}
	if n.Status == model.OK {
		m.Title = fmt.Sprintf("%s incident resolved", n.ApplicationId.Name)
	} else {
		m.Title = fmt.Sprintf("[%s] %s is not meeting its SLOs", strings.ToUpper(n.Status.String()), n.ApplicationId.Name)
	}
	if n.Details != nil {
		for _, r := range n.Details.Reports {
			m.Lines = append(m.Lines, fmt.Sprintf("%s / %s: %s", r.Name, r.Check, r.Message))
		}
		if n.Details.RCASummary != "" {
			m.Fields = append(m.Fields, messageField{Name: "Root cause", Value: n.Details.RCASummary})
			if n.Details.RCARemediations != "" {
				m.Fields = append(m.Fields, messageField{Name: "Remediations", Value: utils.Truncate(n.Details.RCARemediations, 1000)})
			}
		}
	}
	return m
}

func alertMessage(baseUrl string, n *db.AlertNotification) *message {
	name := alertDisplayName(n)
	m := &message{Status: n.Status, Url: alertUrl(baseUrl, n), UrlText: "View alert", Footer: "shards alert " + n.AlertId}
	if n.Status == model.OK {
		resolved := "resolved"
		if n.Details != nil && n.Details.ResolvedBy != "" {
			resolved = "manually resolved by " + n.Details.ResolvedBy
		}
		m.Title = fmt.Sprintf("%s alert %s", name, resolved)
		if n.Details != nil && n.Details.Duration != "" {
			m.Title += fmt.Sprintf(" (duration: %s)", n.Details.Duration)
		}
	} else {
		summary := ""
		if n.Details != nil {
			summary = n.Details.Summary
		}
		m.Title = fmt.Sprintf("[%s] %s: %s", strings.ToUpper(n.Status.String()), name, summary)
	}
	if n.Details != nil {
		if n.Details.ProjectName != "" {
			m.Fields = append(m.Fields, messageField{Name: "Project", Value: n.Details.ProjectName})
		}
		if n.Details.RuleName != "" {
			m.Fields = append(m.Fields, messageField{Name: "Alerting rule", Value: n.Details.RuleName})
		}
		for _, d := range n.Details.Details {
			m.Fields = append(m.Fields, messageField{Name: d.Name, Value: utils.Truncate(d.Value, 1000), Code: d.Code})
		}
	}
	return m
}

func commentMessage(v CommentTemplateValues) *message {
	target := v.Title
	if target == "" {
		target = v.TargetType + " " + v.TargetId
	}
	m := &message{
		Status:  model.INFO,
		Title:   fmt.Sprintf("%s commented on %s", v.Author, target),
		Url:     v.URL,
		UrlText: "View timeline",
		Lines:   []string{utils.Truncate(v.Body, 2000)},
	}
	if v.ProjectName != "" {
		m.Fields = append(m.Fields, messageField{Name: "Project", Value: v.ProjectName})
	}
	if v.Application.Name != "" {
		m.Fields = append(m.Fields, messageField{Name: "Application", Value: v.Application.Name})
	}
	if v.AuthorKind == string(db.CommentAuthorAgent) {
		m.Footer = "comment by an agent"
	}
	return m
}

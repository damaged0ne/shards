package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/utils"
)

// Mattermost sends notifications to an incoming webhook using Slack-compatible message attachments.
type Mattermost struct {
	cfg    *db.IntegrationMattermost
	client *http.Client
}

func NewMattermost(cfg *db.IntegrationMattermost) *Mattermost {
	return &Mattermost{cfg: cfg, client: http.DefaultClient}
}

func (mm *Mattermost) SendIncident(ctx context.Context, baseUrl string, n *db.IncidentNotification) error {
	return mm.send(ctx, incidentMessage(baseUrl, n))
}

func (mm *Mattermost) SendAlert(ctx context.Context, baseUrl string, n *db.AlertNotification) error {
	return mm.send(ctx, alertMessage(baseUrl, n))
}

func (mm *Mattermost) SendDeployment(ctx context.Context, project *db.Project, ds model.ApplicationDeploymentStatus) error {
	return nil // not supported
}

func (mm *Mattermost) SendComment(ctx context.Context, values CommentTemplateValues) error {
	return mm.send(ctx, commentMessage(values))
}

type mattermostField struct {
	Short bool   `json:"short"`
	Title string `json:"title"`
	Value string `json:"value"`
}

type mattermostAttachment struct {
	Fallback  string            `json:"fallback"`
	Color     string            `json:"color"`
	Title     string            `json:"title"`
	TitleLink string            `json:"title_link,omitempty"`
	Text      string            `json:"text,omitempty"`
	Fields    []mattermostField `json:"fields,omitempty"`
	Footer    string            `json:"footer,omitempty"`
}

type mattermostPayload struct {
	Channel     string                 `json:"channel,omitempty"`
	Username    string                 `json:"username,omitempty"`
	Attachments []mattermostAttachment `json:"attachments"`
}

func mattermostPayloadFor(cfg *db.IntegrationMattermost, m *message) mattermostPayload {
	a := mattermostAttachment{
		Fallback:  m.Title,
		Color:     m.Status.Color(),
		Title:     m.Title,
		TitleLink: m.Url,
		Footer:    m.Footer,
	}
	var lines []string
	for _, l := range m.Lines {
		lines = append(lines, "- "+l)
	}
	a.Text = utils.Truncate(strings.Join(lines, "\n"), 4000)
	for _, f := range m.Fields {
		v := f.Value
		if f.Code {
			v = "```\n" + v + "\n```"
		}
		a.Fields = append(a.Fields, mattermostField{Title: f.Name, Value: v, Short: !f.Code && len(v) < 40})
	}
	return mattermostPayload{Channel: cfg.Channel, Username: cfg.Username, Attachments: []mattermostAttachment{a}}
}

func (mm *Mattermost) send(ctx context.Context, m *message) error {
	data, err := json.Marshal(mattermostPayloadFor(mm.cfg, m))
	if err != nil {
		return err
	}
	return postJSON(ctx, mm.client, mm.cfg.WebhookUrl, data, "mattermost")
}

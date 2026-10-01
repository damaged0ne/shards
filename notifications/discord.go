package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/utils"
)

// Discord sends notifications to a channel webhook as embeds colored by severity.
type Discord struct {
	cfg    *db.IntegrationDiscord
	client *http.Client
}

func NewDiscord(cfg *db.IntegrationDiscord) *Discord {
	return &Discord{cfg: cfg, client: http.DefaultClient}
}

func (d *Discord) SendIncident(ctx context.Context, baseUrl string, n *db.IncidentNotification) error {
	return d.send(ctx, incidentMessage(baseUrl, n))
}

func (d *Discord) SendAlert(ctx context.Context, baseUrl string, n *db.AlertNotification) error {
	return d.send(ctx, alertMessage(baseUrl, n))
}

func (d *Discord) SendDeployment(ctx context.Context, project *db.Project, ds model.ApplicationDeploymentStatus) error {
	return nil // not supported
}

func (d *Discord) SendComment(ctx context.Context, values CommentTemplateValues) error {
	return d.send(ctx, commentMessage(values))
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Url         string              `json:"url,omitempty"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Footer      *struct {
		Text string `json:"text"`
	} `json:"footer,omitempty"`
	Timestamp string `json:"timestamp"`
}

type discordPayload struct {
	Username        string         `json:"username"`
	Embeds          []discordEmbed `json:"embeds"`
	AllowedMentions struct {
		Parse []string `json:"parse"`
	} `json:"allowed_mentions"`
}

func discordPayloadFor(m *message) discordPayload {
	e := discordEmbed{
		Title:     utils.Truncate(m.Title, 256),
		Url:       m.Url,
		Color:     m.severityColor(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	var lines []string
	for _, l := range m.Lines {
		lines = append(lines, "• "+l)
	}
	e.Description = utils.Truncate(strings.Join(lines, "\n"), 4000)
	for i, f := range m.Fields {
		if i >= 25 {
			break
		}
		v := f.Value
		if f.Code {
			v = "```\n" + utils.Truncate(v, 990) + "\n```"
		}
		if v == "" {
			v = "-"
		}
		e.Fields = append(e.Fields, discordEmbedField{Name: utils.Truncate(f.Name, 256), Value: utils.Truncate(v, 1024)})
	}
	if m.Footer != "" {
		e.Footer = &struct {
			Text string `json:"text"`
		}{Text: m.Footer}
	}
	p := discordPayload{Username: "shards", Embeds: []discordEmbed{e}}
	p.AllowedMentions.Parse = []string{} // never ping @everyone/@here from alert texts
	return p
}

func (d *Discord) send(ctx context.Context, m *message) error {
	data, err := json.Marshal(discordPayloadFor(m))
	if err != nil {
		return err
	}
	return postJSON(ctx, d.client, d.cfg.WebhookUrl, data, "discord")
}

func postJSON(ctx context.Context, client *http.Client, url string, data []byte, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s: %s: %s", name, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

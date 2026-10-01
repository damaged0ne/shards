package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/utils"
)

const (
	telegramApiUrl     = "https://api.telegram.org"
	telegramMaxMessage = 4096
)

// Telegram sends notifications through the Bot API (sendMessage, HTML parse mode).
type Telegram struct {
	cfg    *db.IntegrationTelegram
	apiUrl string
	client *http.Client
}

func NewTelegram(cfg *db.IntegrationTelegram) *Telegram {
	return &Telegram{cfg: cfg, apiUrl: telegramApiUrl, client: http.DefaultClient}
}

func (t *Telegram) SendIncident(ctx context.Context, baseUrl string, n *db.IncidentNotification) error {
	return t.send(ctx, incidentMessage(baseUrl, n))
}

func (t *Telegram) SendAlert(ctx context.Context, baseUrl string, n *db.AlertNotification) error {
	return t.send(ctx, alertMessage(baseUrl, n))
}

func (t *Telegram) SendDeployment(ctx context.Context, project *db.Project, ds model.ApplicationDeploymentStatus) error {
	return nil // not supported
}

func (t *Telegram) SendComment(ctx context.Context, values CommentTemplateValues) error {
	return t.send(ctx, commentMessage(values))
}

func telegramHTML(m *message) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString("<b>" + esc(m.Title) + "</b>\n")
	for _, l := range m.Lines {
		b.WriteString("• " + esc(l) + "\n")
	}
	for _, f := range m.Fields {
		if f.Code {
			b.WriteString("<b>" + esc(f.Name) + "</b>:\n<pre>" + esc(f.Value) + "</pre>\n")
		} else {
			b.WriteString("<b>" + esc(f.Name) + "</b>: " + esc(f.Value) + "\n")
		}
	}
	text := b.String()
	link := ""
	if m.Url != "" {
		link = fmt.Sprintf("\n<a href=\"%s\">%s</a>", esc(m.Url), esc(m.UrlText))
	}
	if len(text)+len(link) > telegramMaxMessage {
		// cutting HTML could break the markup: fall back to the escaped title only
		text = "<b>" + esc(utils.Truncate(m.Title, 1000)) + "</b>\n(details truncated)\n"
	}
	return strings.TrimRight(text, "\n") + link
}

func (t *Telegram) send(ctx context.Context, m *message) error {
	payload := map[string]any{
		"chat_id":                  t.cfg.ChatId,
		"text":                     telegramHTML(m),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if t.cfg.MessageThreadId > 0 {
		payload["message_thread_id"] = t.cfg.MessageThreadId
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/bot%s/sendMessage", t.apiUrl, t.cfg.BotToken), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		// the error contains the URL with the bot token
		return fmt.Errorf("telegram: %s", strings.ReplaceAll(err.Error(), t.cfg.BotToken, "<token>"))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var res struct {
		Ok          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(body, &res)
	if resp.StatusCode >= 300 || !res.Ok {
		if res.Description != "" {
			return fmt.Errorf("telegram: %s", res.Description)
		}
		return fmt.Errorf("telegram: %s", resp.Status)
	}
	return nil
}

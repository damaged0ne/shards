package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"text/template"

	"github.com/coroot/coroot/utils"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
)

type Webhook struct {
	cfg *db.IntegrationWebhook
}

type IncidentTemplateValues struct {
	Status          string                                 `json:"status"`
	Application     model.ApplicationId                    `json:"application"`
	Reports         []db.IncidentNotificationDetailsReport `json:"reports"`
	URL             string                                 `json:"url"`
	RCASummary      string                                 `json:"rca_summary,omitempty"`
	RCARemediations string                                 `json:"rca_remediations,omitempty"`
}

type DeploymentTemplateValues struct {
	Status      string              `json:"status"`
	Application model.ApplicationId `json:"application"`
	Version     string              `json:"version"`
	Summary     []string            `json:"summary"`
	URL         string              `json:"url"`
}

type AlertTemplateValues struct {
	Status      string              `json:"status"`
	ProjectName string              `json:"project_name"`
	Application model.ApplicationId `json:"application"`
	RuleName    string              `json:"rule_name"`
	Severity    string              `json:"severity"`
	Summary     string              `json:"summary"`
	Details     []model.AlertDetail `json:"details,omitempty"`
	Duration    string              `json:"duration,omitempty"`
	ResolvedBy  string              `json:"resolved_by,omitempty"`
	URL         string              `json:"url"`
}

// CommentTemplateValues are available to the webhook comment template ("comment" event).
type CommentTemplateValues struct {
	Event       string              `json:"event"`
	ProjectName string              `json:"project_name"`
	TargetType  string              `json:"target_type"`
	TargetId    string              `json:"target_id"`
	Application model.ApplicationId `json:"application"`
	Title       string              `json:"title,omitempty"`
	Author      string              `json:"author"`
	AuthorKind  string              `json:"author_kind"`
	Body        string              `json:"body"`
	URL         string              `json:"url"`
}

func NewWebhook(cfg *db.IntegrationWebhook) *Webhook {
	return &Webhook{cfg: cfg}
}

func mergeCustomFields(values any, customFields map[string]string) any {
	if len(customFields) == 0 {
		return values
	}
	v := reflect.ValueOf(values)
	t := v.Type()

	existingFields := make(map[string]bool)
	var fields []reflect.StructField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fields = append(fields, reflect.StructField{
			Name: f.Name,
			Type: f.Type,
			Tag:  f.Tag,
		})
		existingFields[f.Name] = true
	}

	var customKeys []string
	for k := range customFields {
		if k == "" {
			continue
		}
		exported := strings.ToUpper(k[:1]) + k[1:]
		if existingFields[exported] {
			continue
		}
		customKeys = append(customKeys, k)
		fields = append(fields, reflect.StructField{
			Name: exported,
			Type: reflect.TypeOf(""),
			Tag:  reflect.StructTag(`json:"` + k + `"`),
		})
	}

	newType := reflect.StructOf(fields)
	newValue := reflect.New(newType).Elem()
	for i := 0; i < t.NumField(); i++ {
		newValue.Field(i).Set(v.Field(i))
	}
	for i, k := range customKeys {
		newValue.Field(t.NumField() + i).SetString(customFields[k])
	}
	return newValue.Interface()
}

func (wh *Webhook) SendIncident(ctx context.Context, baseUrl string, n *db.IncidentNotification) error {
	tmpl, err := template.New("incidentTemplate").Funcs(templateFunctions).Parse(wh.cfg.IncidentTemplate)
	if err != nil {
		return fmt.Errorf("invalid incident template: %s", err)
	}

	var data bytes.Buffer
	values := IncidentTemplateValues{
		Status:      strings.ToUpper(n.Status.String()),
		Application: n.ApplicationId,
		URL:         incidentUrl(baseUrl, n),
	}
	if n.Details != nil {
		values.Reports = n.Details.Reports
		values.RCASummary = n.Details.RCASummary
		values.RCARemediations = n.Details.RCARemediations
	}
	err = tmpl.Execute(&data, mergeCustomFields(values, wh.cfg.CustomFields))
	if err != nil {
		return fmt.Errorf("invalid incident template: %s", err)
	}

	return wh.send(ctx, data.Bytes())
}

func (wh *Webhook) SendAlert(ctx context.Context, baseUrl string, n *db.AlertNotification) error {
	if wh.cfg.AlertTemplate == "" {
		return nil
	}
	tmpl, err := template.New("alertTemplate").Funcs(templateFunctions).Parse(wh.cfg.AlertTemplate)
	if err != nil {
		return fmt.Errorf("invalid alert template: %s", err)
	}

	var data bytes.Buffer
	values := AlertTemplateValues{
		Status:      strings.ToUpper(n.Status.String()),
		Application: n.ApplicationId,
		URL:         alertUrl(baseUrl, n),
	}
	if n.Details != nil {
		values.ProjectName = n.Details.ProjectName
		values.RuleName = n.Details.RuleName
		values.Severity = n.Details.Severity
		values.Summary = n.Details.Summary
		values.Details = n.Details.Details
		values.Duration = n.Details.Duration
		values.ResolvedBy = n.Details.ResolvedBy
	}
	err = tmpl.Execute(&data, mergeCustomFields(values, wh.cfg.CustomFields))
	if err != nil {
		return fmt.Errorf("invalid alert template: %s", err)
	}

	return wh.send(ctx, data.Bytes())
}

// SendComment forwards a timeline comment. It is a no-op when no comment template is configured.
func (wh *Webhook) SendComment(ctx context.Context, values CommentTemplateValues) error {
	if wh.cfg.CommentTemplate == "" {
		return nil
	}
	tmpl, err := template.New("commentTemplate").Funcs(templateFunctions).Parse(wh.cfg.CommentTemplate)
	if err != nil {
		return fmt.Errorf("invalid comment template: %s", err)
	}
	values.Event = "comment"
	var data bytes.Buffer
	if err = tmpl.Execute(&data, mergeCustomFields(values, wh.cfg.CustomFields)); err != nil {
		return fmt.Errorf("invalid comment template: %s", err)
	}
	return wh.send(ctx, data.Bytes())
}

// CommentUrl links to the incident or alert a comment was posted on.
func CommentUrl(baseUrl string, projectId db.ProjectId, targetType db.CommentTargetType, targetId string) string {
	switch targetType {
	case db.CommentTargetIncident:
		return fmt.Sprintf("%s/p/%s/incidents?incident=%s", baseUrl, projectId, targetId)
	case db.CommentTargetAlert:
		return fmt.Sprintf("%s/p/%s/alerts?alert=%s", baseUrl, projectId, targetId)
	}
	return fmt.Sprintf("%s/p/%s/alerts", baseUrl, projectId)
}

func (wh *Webhook) SendDeployment(ctx context.Context, project *db.Project, ds model.ApplicationDeploymentStatus) error {
	tmpl, err := template.New("deploymentTemplate").Funcs(templateFunctions).Parse(wh.cfg.DeploymentTemplate)
	if err != nil {
		return fmt.Errorf("invalid deployment template: %s", err)
	}

	status := "Deployed"
	var summary []string
	switch ds.State {
	case model.ApplicationDeploymentStateInProgress:
		status = "In-progress"
	case model.ApplicationDeploymentStateStuck:
		status = "Stuck"
	case model.ApplicationDeploymentStateCancelled:
		status = "Cancelled"
	case model.ApplicationDeploymentStateSummary:
		for _, s := range ds.Summary {
			summary = append(summary, fmt.Sprintf("%s %s", s.Emoji(), s.Message))
		}
		if len(summary) == 0 {
			summary = append(summary, "No notable changes")
		}
	}

	var data bytes.Buffer
	err = tmpl.Execute(&data, mergeCustomFields(DeploymentTemplateValues{
		Application: ds.Deployment.ApplicationId,
		Status:      status,
		Version:     ds.Deployment.Version(),
		Summary:     summary,
		URL:         deploymentUrl(project.Settings.Integrations.BaseUrl, project.Id, ds.Deployment),
	}, wh.cfg.CustomFields))
	if err != nil {
		return fmt.Errorf("invalid deployment template: %s", err)
	}

	return wh.send(ctx, data.Bytes())
}

func (wh *Webhook) send(ctx context.Context, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.cfg.Url, bytes.NewReader(utils.EscapeJsonMultilineStrings(data)))
	if err != nil {
		return err
	}
	if wh.cfg.BasicAuth != nil && wh.cfg.BasicAuth.User != "" && wh.cfg.BasicAuth.Password != "" {
		req.SetBasicAuth(wh.cfg.BasicAuth.User, wh.cfg.BasicAuth.Password)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, h := range wh.cfg.CustomHeaders {
		req.Header.Add(h.Key, h.Value)
	}
	httpClient := &http.Client{}
	if wh.cfg.TlsSkipVerify {
		httpClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("response status: %s", resp.Status)
		}
		return fmt.Errorf("%s: %s", resp.Status, string(body))
	}

	return nil
}

var (
	templateFunctions = template.FuncMap{
		"json": func(arg any) (string, error) {
			data, err := json.Marshal(arg)
			return string(data), err
		},
	}
)

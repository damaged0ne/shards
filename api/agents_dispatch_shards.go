package api

// shards fork: outbound agent dispatch — wake agents up with signed webhooks instead of polling.
//
// Events: incident_opened, incident_escalated, alert_fired (from the watchers via
// notifications.AgentEvents), mention (an @agent-name in a comment), approval_decided (called by
// the approval system via DispatchAgentEvent), manual ("Ask agent" in the UI) and test.
//
// Delivery: every task is stored in agent_dispatch first, then POSTed by a background worker with
// retries (exponential backoff, AgentDispatchMaxAttempts). Automatic events are filtered by the
// agent's dispatch config (events, min severity, categories, app patterns), deduplicated per
// event+target within DedupMinutes and capped by RateLimitPerHour; skipped tasks are logged too.
//
// Signature: X-Shards-Timestamp: <unix seconds>, X-Shards-Signature: sha256=<hex HMAC-SHA256 of
// "<timestamp>.<body>" keyed with the agent's secret>. Receivers must recompute it with a
// constant-time compare and reject timestamps older than 5 minutes (replay protection).

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/notifications"
	"github.com/coroot/coroot/utils"
	"k8s.io/klog"
)

const (
	AgentDispatchMaxAttempts  = 6
	agentDispatchTimeout      = 10 * time.Second
	agentDispatchPollInterval = 5 * time.Second
	agentDispatchBatch        = 50
	agentDispatchVersion      = 1
	AgentSignatureHeader      = "X-Shards-Signature"
	AgentTimestampHeader      = "X-Shards-Timestamp"
	AgentEventHeader          = "X-Shards-Event"
	AgentDeliveryHeader       = "X-Shards-Delivery"
	agentDeliverySkipped      = "skipped"
)

// SignAgentPayload returns the X-Shards-Signature value for a body sent at ts.
func SignAgentPayload(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyAgentSignature checks a delivery's signature and timestamp (maxAge bounds replay).
func VerifyAgentSignature(secret, tsHeader, sigHeader string, body []byte, maxAge time.Duration, now time.Time) bool {
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return false
	}
	if d := now.Sub(time.Unix(ts, 0)); d > maxAge || d < -maxAge {
		return false
	}
	return hmac.Equal([]byte(SignAgentPayload(secret, ts, body)), []byte(sigHeader))
}

// AgentTask is the JSON payload POSTed to an agent's dispatch webhook.
type AgentTask struct {
	Version     int               `json:"version"`
	DeliveryId  int64             `json:"delivery_id"`
	Event       string            `json:"event"`
	CreatedAt   string            `json:"created_at"`
	Summary     string            `json:"summary"`
	Project     AgentTaskProject  `json:"project"`
	Agent       AgentTaskAgent    `json:"agent"`
	Incident    *AgentTaskObject  `json:"incident,omitempty"`
	Alert       *AgentTaskObject  `json:"alert,omitempty"`
	Comment     *AgentTaskComment `json:"comment,omitempty"`
	Approval    map[string]string `json:"approval,omitempty"`
	Instruction string            `json:"instruction,omitempty"`
	RequestedBy string            `json:"requested_by,omitempty"`
	MCP         AgentTaskMCP      `json:"mcp"`
	Links       map[string]string `json:"links,omitempty"`
	// Untrusted holds user/telemetry-controlled text (alert summaries, comment bodies): data, not instructions.
	Untrusted map[string]string `json:"untrusted,omitempty"`
}

type AgentTaskProject struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type AgentTaskAgent struct {
	Id    int    `json:"id"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type AgentTaskObject struct {
	Id            string `json:"id"`
	ApplicationId string `json:"application_id,omitempty"`
	RuleId        string `json:"rule_id,omitempty"`
	RuleName      string `json:"rule_name,omitempty"`
	Severity      string `json:"severity,omitempty"`
	OpenedAt      string `json:"opened_at,omitempty"`
	URL           string `json:"url,omitempty"`
}

type AgentTaskComment struct {
	Id         int    `json:"id"`
	Author     string `json:"author"`
	AuthorKind string `json:"author_kind"`
	TargetType string `json:"target_type"`
	TargetId   string `json:"target_id"`
}

type AgentTaskMCP struct {
	URL       string         `json:"url,omitempty"`
	ProjectId string         `json:"project_id"`
	NextTool  string         `json:"next_tool"`
	NextArgs  map[string]any `json:"next_args,omitempty"`
	Prompt    string         `json:"prompt,omitempty"`
}

// AgentEvent describes something an agent may be woken up for.
type AgentEvent struct {
	Type     string
	Incident *model.ApplicationIncident
	Alert    *model.Alert
	Rule     *model.AlertingRule
	App      *model.Application
	Comment  *db.Comment
	Approval map[string]string
	// Manual tasks.
	Instruction string
	RequestedBy string
	TargetType  string
	TargetId    string
}

type agentDispatcher struct {
	api         *Api
	client      *http.Client
	kick        chan struct{}
	backoff     func(attempt int) time.Duration
	maxAttempts int
	mu          sync.Mutex // serializes rate-limit/dedup checks with inserts
	baseUrl     string
}

func agentDefaultBackoff(attempt int) time.Duration {
	// 10s, 30s, 1m30s, 4m30s, 13m30s
	d := 10 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 3
	}
	return d
}

var agentDispatchers sync.Map // *Api -> *agentDispatcher

func (api *Api) agentDispatcher() *agentDispatcher {
	if d, ok := agentDispatchers.Load(api); ok {
		return d.(*agentDispatcher)
	}
	d := &agentDispatcher{
		api:         api,
		client:      &http.Client{Timeout: agentDispatchTimeout},
		kick:        make(chan struct{}, 1),
		backoff:     agentDefaultBackoff,
		maxAttempts: AgentDispatchMaxAttempts,
	}
	actual, _ := agentDispatchers.LoadOrStore(api, d)
	return actual.(*agentDispatcher)
}

// StartAgentBackground starts the dispatch worker and the audit retention loop, and subscribes
// to incident/alert events. Called once from main.
func (api *Api) StartAgentBackground() {
	d := api.agentDispatcher()
	notifications.AgentEvents = api
	go d.loop()
	go api.pruneAgentDataLoop()
}

func (d *agentDispatcher) loop() {
	t := time.NewTicker(agentDispatchPollInterval)
	defer t.Stop()
	for {
		d.deliverDue()
		select {
		case <-t.C:
		case <-d.kick:
		}
	}
}

func (d *agentDispatcher) wake() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// rememberBaseUrl records the externally visible URL of the UI, used for links when the project
// has no base URL configured.
func (api *Api) rememberBaseUrl(r *http.Request) {
	u := strings.TrimSuffix(api.GetAbsoluteUrl(r, "/").String(), "/")
	d := api.agentDispatcher()
	d.mu.Lock()
	d.baseUrl = u
	d.mu.Unlock()
}

func (d *agentDispatcher) baseUrlFor(project *db.Project) string {
	if u := strings.TrimSuffix(project.Settings.Integrations.BaseUrl, "/"); u != "" {
		return u
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.baseUrl
}

// IncidentEvent implements notifications.AgentEventsHook.
func (api *Api) IncidentEvent(project *db.Project, app *model.Application, incident *model.ApplicationIncident, escalated bool) {
	ev := AgentEvent{Type: db.AgentEventIncidentOpened, Incident: incident, App: app}
	if escalated {
		ev.Type = db.AgentEventIncidentEscalated
	}
	go api.dispatchAuto(project, ev)
}

// AlertEvent implements notifications.AgentEventsHook.
func (api *Api) AlertEvent(project *db.Project, app *model.Application, alert *model.Alert, rule *model.AlertingRule) {
	go api.dispatchAuto(project, AgentEvent{Type: db.AgentEventAlertFired, Alert: alert, Rule: rule, App: app})
}

// DispatchAgentEvent wakes a specific agent up (e.g. approval_decided from the approval system:
// pass Approval with id/decision/tool/decided_by). It honors the agent's event filter, dedup and
// rate limit like other automatic events.
func (api *Api) DispatchAgentEvent(project *db.Project, agentId int, ev AgentEvent) error {
	a, err := api.db.GetAgent(project.Id, agentId)
	if err != nil {
		return err
	}
	if !agentWants(a, ev) {
		return nil
	}
	_, err = api.agentDispatcher().enqueue(project, a, ev, false)
	return err
}

func severityAtLeast(s model.Status, min string) bool {
	if min == "critical" {
		return s >= model.CRITICAL
	}
	return s >= model.WARNING
}

// agentWants applies an agent's dispatch config to an automatic event.
func agentWants(a *db.Agent, ev AgentEvent) bool {
	c := a.Dispatch
	if c == nil || !c.Enabled || c.URL == "" || a.Disabled || a.Expired(time.Now()) {
		return false
	}
	if !c.HasEvent(ev.Type) {
		return false
	}
	var appId model.ApplicationId
	var category model.ApplicationCategory
	switch {
	case ev.Incident != nil:
		if !severityAtLeast(ev.Incident.Severity, c.MinSeverity) {
			return false
		}
		appId = ev.Incident.ApplicationId
	case ev.Alert != nil:
		if !severityAtLeast(ev.Alert.Severity, c.MinSeverity) {
			return false
		}
		appId, category = ev.Alert.ApplicationId, ev.Alert.ApplicationCategory
	}
	if ev.App != nil {
		category = ev.App.Category
	}
	if len(c.Categories) > 0 && (ev.Incident != nil || ev.Alert != nil) {
		ok := false
		for _, cat := range c.Categories {
			if model.ApplicationCategory(cat) == category {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(c.AppPatterns) > 0 && !appId.IsZero() {
		if !utils.GlobMatch(fmt.Sprintf("%s:%s:%s", appId.Namespace, appId.Kind, appId.Name), c.AppPatterns...) {
			return false
		}
	}
	return true
}

func (api *Api) dispatchAuto(project *db.Project, ev AgentEvent) {
	agents, err := api.db.GetAgents(project.Id)
	if err != nil {
		klog.Errorln("agent dispatch:", err)
		return
	}
	for _, a := range agents {
		if !agentWants(a, ev) {
			continue
		}
		if _, err := api.agentDispatcher().enqueue(project, a, ev, false); err != nil {
			klog.Errorln("agent dispatch:", err)
		}
	}
}

func agentEventDedupKey(ev AgentEvent) string {
	switch {
	case ev.Incident != nil:
		return ev.Type + ":incident:" + ev.Incident.Key
	case ev.Alert != nil:
		return ev.Type + ":alert:" + ev.Alert.Id
	case ev.Comment != nil:
		return ev.Type + ":comment:" + strconv.Itoa(ev.Comment.Id)
	case ev.Approval != nil:
		return ev.Type + ":approval:" + ev.Approval["id"]
	}
	return ""
}

// enqueue stores a task for delivery. Automatic tasks (manual=false) are deduplicated and rate
// limited; the decision is recorded as a "skipped" delivery.
func (d *agentDispatcher) enqueue(project *db.Project, a *db.Agent, ev AgentEvent, manual bool) (*db.AgentDelivery, error) {
	now := time.Now()
	dl := &db.AgentDelivery{
		AgentId:       a.Id,
		ProjectId:     project.Id,
		Event:         ev.Type,
		DedupKey:      agentEventDedupKey(ev),
		Status:        db.AgentDeliveryPending,
		CreatedAt:     now.UnixMilli(),
		NextAttemptAt: now.UnixMilli(),
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !manual {
		c := a.Dispatch
		dedup := c.DedupMinutes
		if dedup == 0 {
			dedup = db.AgentDispatchDefaultDedup
		}
		limit := c.RateLimitPerHour
		if limit == 0 {
			limit = db.AgentDispatchDefaultRateLimit
		}
		if dl.DedupKey != "" && dedup > 0 {
			n, err := d.api.db.CountAgentDeliveries(a.Id, now.Add(-time.Duration(dedup)*time.Minute).UnixMilli(), dl.DedupKey)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				dl.Status, dl.Error = agentDeliverySkipped, fmt.Sprintf("duplicate within %dm", dedup)
			}
		}
		if dl.Status == db.AgentDeliveryPending && limit > 0 {
			n, err := d.api.db.CountAgentDeliveries(a.Id, now.Add(-time.Hour).UnixMilli(), "")
			if err != nil {
				return nil, err
			}
			if n >= limit {
				dl.Status, dl.Error = agentDeliverySkipped, fmt.Sprintf("rate limit: %d per hour", limit)
			}
		}
		if dl.Status == agentDeliverySkipped {
			dl.NextAttemptAt = 0
		}
	}
	dl.Payload = "{}"
	if err := d.api.db.AddAgentDelivery(dl); err != nil {
		return nil, err
	}
	task := d.buildTask(project, a, ev, dl)
	data, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	dl.Payload = string(data)
	if err = d.api.db.UpdateAgentDeliveryPayload(dl.Id, dl.Payload); err != nil {
		return nil, err
	}
	if dl.Status == db.AgentDeliveryPending {
		d.wake()
	}
	return dl, nil
}

func (d *agentDispatcher) buildTask(project *db.Project, a *db.Agent, ev AgentEvent, dl *db.AgentDelivery) AgentTask {
	base := d.baseUrlFor(project)
	t := AgentTask{
		Version:     agentDispatchVersion,
		DeliveryId:  dl.Id,
		Event:       ev.Type,
		CreatedAt:   time.UnixMilli(dl.CreatedAt).UTC().Format(time.RFC3339),
		Project:     AgentTaskProject{Id: string(project.Id), Name: project.Name},
		Agent:       AgentTaskAgent{Id: a.Id, Name: a.Name, Scope: string(a.Scope)},
		Approval:    ev.Approval,
		Instruction: ev.Instruction,
		RequestedBy: ev.RequestedBy,
		MCP:         AgentTaskMCP{ProjectId: string(project.Id)},
		Links:       map[string]string{},
		Untrusted:   map[string]string{},
	}
	if base != "" {
		t.MCP.URL = base + "/mcp"
		t.Links["agent"] = fmt.Sprintf("%s/p/%s/agents/%d", base, project.Id, a.Id)
	}
	targetType, targetId := ev.TargetType, ev.TargetId
	if ev.Incident != nil {
		targetType, targetId = string(db.CommentTargetIncident), ev.Incident.Key
		t.Incident = &AgentTaskObject{Id: ev.Incident.Key, ApplicationId: ev.Incident.ApplicationId.String(), Severity: ev.Incident.Severity.String(), OpenedAt: MCPFormatTime(ev.Incident.OpenedAt)}
	}
	if ev.Alert != nil {
		targetType, targetId = string(db.CommentTargetAlert), ev.Alert.Id
		t.Alert = &AgentTaskObject{Id: ev.Alert.Id, ApplicationId: ev.Alert.ApplicationId.String(), RuleId: ev.Alert.RuleId, Severity: ev.Alert.Severity.String(), OpenedAt: MCPFormatTime(ev.Alert.OpenedAt)}
		if ev.Rule != nil {
			t.Alert.RuleName = ev.Rule.Name
		}
		t.Untrusted["alert_summary"] = mcpTruncate(ev.Alert.Summary, 500)
	}
	if ev.Comment != nil {
		targetType, targetId = string(ev.Comment.TargetType), ev.Comment.TargetId
		t.Comment = &AgentTaskComment{Id: ev.Comment.Id, Author: ev.Comment.Author, AuthorKind: string(ev.Comment.AuthorKind), TargetType: targetType, TargetId: targetId}
		t.Untrusted["comment_body"] = mcpTruncate(ev.Comment.Body, 2000)
	}
	if base != "" && targetId != "" {
		u := notifications.CommentUrl(base, project.Id, db.CommentTargetType(targetType), targetId)
		t.Links["ui"] = u
		if t.Incident != nil {
			t.Incident.URL = u
		}
		if t.Alert != nil {
			t.Alert.URL = u
		}
	}
	switch targetType {
	case string(db.CommentTargetIncident):
		t.MCP.NextTool, t.MCP.NextArgs, t.MCP.Prompt = "get_incident_details", map[string]any{"key": targetId}, "triage_incident"
	case string(db.CommentTargetAlert):
		t.MCP.NextTool, t.MCP.NextArgs, t.MCP.Prompt = "get_alert", map[string]any{"id": targetId}, "investigate_alert"
	case string(db.CommentTargetAlertingRule):
		t.MCP.NextTool, t.MCP.NextArgs = "get_alerting_rule", map[string]any{"id": targetId}
	default:
		t.MCP.NextTool = "list_alerts"
	}
	switch ev.Type {
	case db.AgentEventIncidentOpened:
		t.Summary = fmt.Sprintf("Incident %s opened for %s (%s)", targetId, ev.Incident.ApplicationId.Name, ev.Incident.Severity)
	case db.AgentEventIncidentEscalated:
		t.Summary = fmt.Sprintf("Incident %s escalated to %s for %s", targetId, ev.Incident.Severity, ev.Incident.ApplicationId.Name)
	case db.AgentEventAlertFired:
		t.Summary = fmt.Sprintf("Alert %s fired (%s) for %s", targetId, ev.Alert.Severity, ev.Alert.ApplicationId.Name)
	case db.AgentEventMention:
		t.Summary = fmt.Sprintf("%s mentioned @%s on %s %s", ev.Comment.Author, a.Name, targetType, targetId)
	case db.AgentEventApprovalDecided:
		t.Summary = fmt.Sprintf("Approval %s: %s", ev.Approval["id"], ev.Approval["decision"])
	case db.AgentEventManual:
		t.Summary = fmt.Sprintf("%s asked @%s to look at %s %s", ev.RequestedBy, a.Name, targetType, targetId)
	case db.AgentEventTest:
		t.Summary = "Test delivery from shards"
		t.MCP.NextTool = "list_projects"
	}
	if len(t.Links) == 0 {
		t.Links = nil
	}
	if len(t.Untrusted) == 0 {
		t.Untrusted = nil
	}
	return t
}

func (d *agentDispatcher) deliverDue() {
	due, err := d.api.db.GetDueAgentDeliveries(time.Now().UnixMilli(), agentDispatchBatch)
	if err != nil {
		klog.Errorln("agent dispatch:", err)
		return
	}
	for _, dl := range due {
		d.deliver(dl)
	}
}

func (d *agentDispatcher) deliver(dl *db.AgentDelivery) {
	a, err := d.api.db.GetAgent(dl.ProjectId, dl.AgentId)
	now := time.Now()
	dl.Attempts++
	fail := func(msg string, final bool) {
		dl.Error = msg
		if final || dl.Attempts >= d.maxAttempts {
			dl.Status, dl.NextAttemptAt = db.AgentDeliveryFailed, 0
		} else {
			dl.NextAttemptAt = now.Add(d.backoff(dl.Attempts)).UnixMilli()
		}
	}
	switch {
	case err != nil:
		fail("agent not found", true)
	case a.Dispatch == nil || a.Dispatch.URL == "":
		fail("the agent has no dispatch webhook configured", true)
	default:
		code, msg := d.post(a, dl)
		dl.ResponseCode = code
		if code >= 200 && code < 300 {
			dl.Status, dl.Error, dl.DeliveredAt, dl.NextAttemptAt = db.AgentDeliveryDelivered, "", now.UnixMilli(), 0
		} else {
			// 4xx other than 408/429 won't succeed on retry
			final := code >= 400 && code < 500 && code != http.StatusRequestTimeout && code != http.StatusTooManyRequests
			fail(msg, final)
		}
	}
	if err := d.api.db.UpdateAgentDelivery(dl); err != nil {
		klog.Errorln("agent dispatch:", err)
	}
}

func (d *agentDispatcher) post(a *db.Agent, dl *db.AgentDelivery) (int, string) {
	body := []byte(dl.Payload)
	ctx, cancel := context.WithTimeout(context.Background(), agentDispatchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Dispatch.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	ts := time.Now().Unix()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shards-agent-dispatch/1")
	req.Header.Set(AgentEventHeader, dl.Event)
	req.Header.Set(AgentDeliveryHeader, strconv.FormatInt(dl.Id, 10))
	req.Header.Set(AgentTimestampHeader, strconv.FormatInt(ts, 10))
	if a.Dispatch.Secret != "" {
		req.Header.Set(AgentSignatureHeader, SignAgentPayload(a.Dispatch.Secret, ts, body))
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, strings.TrimSpace(resp.Status + " " + string(data))
	}
	return resp.StatusCode, ""
}

var agentMentionRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_@./-])@([A-Za-z0-9][A-Za-z0-9_.-]{0,62})`)

// mentionedAgentNames returns the distinct @names in a comment body (code spans included; agents
// are matched case-insensitively by name).
func mentionedAgentNames(body string) []string {
	seen := map[string]bool{}
	var res []string
	for _, m := range agentMentionRe.FindAllStringSubmatch(body, -1) {
		name := strings.TrimRight(m[1], ".-")
		k := strings.ToLower(name)
		if name == "" || seen[k] {
			continue
		}
		seen[k] = true
		res = append(res, name)
	}
	return res
}

// dispatchMentions wakes up the agents @mentioned in a new comment.
func (api *Api) dispatchMentions(project *db.Project, c *db.Comment) {
	for _, name := range mentionedAgentNames(c.Body) {
		a, err := api.db.GetAgentByName(project.Id, name)
		if err != nil {
			continue
		}
		if c.AuthorKind == db.CommentAuthorAgent && strings.EqualFold(c.Author, a.Name) {
			continue // an agent mentioning itself
		}
		ev := AgentEvent{Type: db.AgentEventMention, Comment: c}
		if !agentWants(a, ev) {
			continue
		}
		if _, err := api.agentDispatcher().enqueue(project, a, ev, false); err != nil {
			klog.Errorln("agent dispatch:", err)
		}
	}
}

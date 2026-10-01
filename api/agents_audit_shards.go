package api

// shards fork: audit log of agent calls (every MCP tool call and every REST write by an agent).

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

const (
	// AgentAuditRetention is how long audit entries, sessions and dispatch deliveries are kept.
	AgentAuditRetention     = 30 * 24 * time.Hour
	agentAuditPruneInterval = time.Hour
	agentAuditMaxArgsBytes  = 4096
	agentAuditMaxStrRunes   = 300
	agentAuditMaxErrorBytes = 1000
	agentRESTMaxBodyBytes   = 256 * 1024
)

const (
	agentCallOK     = "ok"
	agentCallError  = "error"
	agentCallDenied = "denied"
)

var agentSecretKeyRe = regexp.MustCompile(`(?i)(secret|token|password|passwd|api_?key|authorization|credential|private_?key|cookie)`)

// redactArgs returns a compact JSON rendering of call arguments with secret-looking keys
// redacted, long strings shortened and the whole thing capped at agentAuditMaxArgsBytes.
func redactArgs(args any) string {
	if args == nil {
		return ""
	}
	v := redactValue(args, 0)
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	if len(data) > agentAuditMaxArgsBytes {
		return strings.TrimSuffix(utils.TruncateUtf8(string(data), agentAuditMaxArgsBytes), "...") + "…[truncated]"
	}
	if string(data) == "{}" || string(data) == "null" {
		return ""
	}
	return string(data)
}

func redactValue(v any, depth int) any {
	if depth > 6 {
		return "…"
	}
	switch x := v.(type) {
	case map[string]any:
		res := make(map[string]any, len(x))
		for k, val := range x {
			if agentSecretKeyRe.MatchString(k) {
				res[k] = "[redacted]"
				continue
			}
			res[k] = redactValue(val, depth+1)
		}
		return res
	case []any:
		n := len(x)
		if n > 20 {
			n = 20
		}
		res := make([]any, 0, n+1)
		for _, val := range x[:n] {
			res = append(res, redactValue(val, depth+1))
		}
		if len(x) > n {
			res = append(res, "…+"+strconv.Itoa(len(x)-n))
		}
		return res
	case string:
		return mcpTruncate(x, agentAuditMaxStrRunes)
	default:
		return x
	}
}

// agentCallTarget extracts the object a call acted on from its arguments.
func agentCallTarget(args map[string]any) (string, string) {
	str := func(k string) string {
		s, _ := args[k].(string)
		return s
	}
	if t := str("target_type"); t != "" {
		return t, str("target_id")
	}
	if k := str("incident_key"); k != "" {
		return "incident", k
	}
	if k := str("key"); k != "" {
		return "incident", k
	}
	if ids, ok := args["ids"].([]any); ok && len(ids) > 0 {
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			if s, ok := id.(string); ok {
				parts = append(parts, s)
			}
		}
		return "alert", mcpTruncate(strings.Join(parts, ","), 200)
	}
	if k := str("app_id"); k != "" {
		return "application", k
	}
	if k := str("node_id"); k != "" {
		return "node", k
	}
	if k := str("trace_id"); k != "" {
		return "trace", k
	}
	return "", ""
}

func (api *Api) recordAgentActivity(a *db.AgentActivity) {
	if len(a.Error) > agentAuditMaxErrorBytes {
		a.Error = utils.TruncateUtf8(a.Error, agentAuditMaxErrorBytes)
	}
	if err := api.db.AddAgentActivity(a); err != nil {
		klog.Errorln("failed to record agent activity:", err)
	}
}

type agentStatusRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *agentStatusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *agentStatusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.status >= 400 && r.body.Len() < agentAuditMaxErrorBytes {
		r.body.Write(b)
	}
	return r.ResponseWriter.Write(b)
}

// serveAgentREST enforces the agent's scope on a REST request and audits writes (and denials).
func (api *Api) serveAgentREST(w http.ResponseWriter, r *http.Request, u *db.User, h func(http.ResponseWriter, *http.Request, *db.User)) {
	vars := mux.Vars(r)
	projectId := vars["project"]
	write := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions

	route := r.URL.Path
	if cr := mux.CurrentRoute(r); cr != nil {
		if tpl, err := cr.GetPathTemplate(); err == nil {
			route = tpl
		}
	}
	act := &db.AgentActivity{
		AgentId:   u.Agent.Id,
		ProjectId: db.ProjectId(projectId),
		Time:      time.Now().UnixMilli(),
		Channel:   "rest",
		Tool:      r.Method + " " + route,
	}
	var args map[string]any
	if write && r.Body != nil {
		data, _ := io.ReadAll(io.LimitReader(r.Body, agentRESTMaxBodyBytes))
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(data))
		_ = json.Unmarshal(data, &args)
	}
	if args == nil {
		args = map[string]any{}
	}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			args[k] = v[0]
		}
	}
	act.TargetType, act.TargetId = agentCallTarget(args)
	switch {
	case vars["alert"] != "":
		act.TargetType, act.TargetId = "alert", vars["alert"]
	case vars["rule"] != "":
		act.TargetType, act.TargetId = "alerting_rule", vars["rule"]
	case vars["incident"] != "":
		act.TargetType, act.TargetId = "incident", vars["incident"]
	}
	act.Args = redactArgs(args)

	if !agentRESTAllowed(w, r, u, projectId) {
		act.Status = agentCallDenied
		act.Error = "scope '" + string(u.Agent.Scope) + "' does not allow " + r.Method + " " + route
		api.recordAgentActivity(act)
		return
	}
	if !write {
		h(w, r, u)
		return
	}
	rec := &agentStatusRecorder{ResponseWriter: w}
	start := time.Now()
	h(rec, r, u)
	act.DurationMs = time.Since(start).Milliseconds()
	act.Status = agentCallOK
	if rec.status >= 400 {
		act.Status = agentCallError
		if rec.status == http.StatusForbidden {
			act.Status = agentCallDenied
		}
		act.Error = strings.TrimSpace(http.StatusText(rec.status) + ": " + strings.TrimSpace(rec.body.String()))
	}
	api.recordAgentActivity(act)
}

func (api *Api) pruneAgentDataLoop() {
	for {
		if err := api.db.PruneAgentData(time.Now().Add(-AgentAuditRetention).UnixMilli()); err != nil {
			klog.Errorln("failed to prune agent audit data:", err)
		}
		time.Sleep(agentAuditPruneInterval)
	}
}

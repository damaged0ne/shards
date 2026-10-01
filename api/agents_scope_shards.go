package api

// shards fork: scope enforcement for operator agents.
//
// An agent is a registry entry (db.Agent) linked to one or more user API keys. A request
// authenticated with a linked key runs as the key's user (the agent's owner) with the agent
// attached (db.User.Agent). What it may do is the intersection of:
//   - the owner's role (unchanged RBAC),
//   - the agent's scope preset (read < triage < operator < admin),
//   - the agent's allowed projects (empty = all projects the owner can access),
//   - expiry / disabled state (an expired or disabled agent's keys stop authenticating).
//
// Enforcement points:
//   - IsAllowed: every RBAC check drops edit actions the scope doesn't cover and actions on
//     projects the agent may not access (coarse, applies to every code path);
//   - MCP: every tool is tagged with a minimal scope (mcpToolScope); tools above the agent's
//     scope are hidden from tools/list and rejected on call;
//   - REST: agentRESTScope classifies (method, path) into a minimal scope; Auth rejects the
//     request before the handler runs.
// Unscoped (legacy) user API keys, sessions and OAuth tokens keep today's behavior (the role).

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/rbac"
	"k8s.io/klog"
)

// attachAgent resolves the agent linked to the API key the user authenticated with. It returns
// false if the key belongs to a disabled or expired agent (the request must be rejected).
func (api *Api) attachAgent(u *db.User) bool {
	if u == nil || u.ApiKeyId == 0 {
		return true
	}
	a, err := api.db.GetAgentByApiKeyId(u.ApiKeyId)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			klog.Errorln(err)
			return false
		}
		return true
	}
	if a.Disabled || a.Expired(time.Now()) {
		return false
	}
	u.Agent = a
	return true
}

// agentScopeForAction is the minimal scope an RBAC action needs.
func agentScopeForAction(a rbac.Action) db.AgentScope {
	if a.Action == rbac.ActionView {
		return db.AgentScopeRead
	}
	switch a.Scope {
	case rbac.ScopeProjectAlerts:
		return db.AgentScopeTriage
	case rbac.ScopeProjectAlertingRules:
		return db.AgentScopeOperator
	case rbac.ScopeProjectProbes: // uptime probes
		return db.AgentScopeOperator
	}
	return db.AgentScopeAdmin
}

// agentFilterActions drops the actions an agent's scope / allowed projects don't cover.
// For non-agent users it returns the actions unchanged.
func agentFilterActions(u *db.User, actions []rbac.Action) []rbac.Action {
	if u == nil || u.Agent == nil {
		return actions
	}
	res := make([]rbac.Action, 0, len(actions))
	for _, a := range actions {
		if pid := a.Object["project_id"]; pid != "" && pid != "*" && !u.Agent.ProjectAllowed(db.ProjectId(pid)) {
			continue
		}
		if !u.Agent.Scope.Allows(agentScopeForAction(a)) {
			continue
		}
		res = append(res, a)
	}
	return res
}

// MCP tool scopes. Read-only tools (readOnlyHint) default to read; any other tool not listed
// here requires admin, so a newly added write tool is safe by default until it is classified.
var (
	mcpToolScopesMu sync.RWMutex
	mcpToolScopes   = map[string]db.AgentScope{
		"select_project":       db.AgentScopeRead,
		"add_comment":          db.AgentScopeTriage,
		"resolve_alerts":       db.AgentScopeOperator,
		"suppress_alerts":      db.AgentScopeOperator,
		"reopen_alerts":        db.AgentScopeOperator,
		"create_alerting_rule": db.AgentScopeOperator,
		"update_alerting_rule": db.AgentScopeOperator,
		"delete_alerting_rule": db.AgentScopeOperator,
	}
)

// SetMCPToolScope sets the minimal agent scope of an MCP tool (e.g. triage for acknowledge-type tools).
func SetMCPToolScope(tool string, scope db.AgentScope) {
	mcpToolScopesMu.Lock()
	defer mcpToolScopesMu.Unlock()
	mcpToolScopes[tool] = scope
}

func mcpToolScope(name string, readOnly bool) db.AgentScope {
	mcpToolScopesMu.RLock()
	s, ok := mcpToolScopes[name]
	mcpToolScopesMu.RUnlock()
	if ok {
		return s
	}
	if readOnly {
		return db.AgentScopeRead
	}
	return db.AgentScopeAdmin
}

type agentRESTRule struct {
	methods string // comma-separated
	path    *regexp.Regexp
	scope   db.AgentScope
}

var (
	agentRESTRulesMu sync.RWMutex
	agentRESTRules   = []agentRESTRule{
		{"POST,PUT,DELETE", regexp.MustCompile(`/api/project/[^/]+/comments(/\d+)?$`), db.AgentScopeTriage},
		{"POST", regexp.MustCompile(`/api/project/[^/]+/alerts/(resolve|suppress|reopen)$`), db.AgentScopeOperator},
		{"POST,PUT,DELETE", regexp.MustCompile(`/api/project/[^/]+/alerting-rules(/[^/]+)?$`), db.AgentScopeOperator},
		{"PUT", regexp.MustCompile(`/api/project/[^/]+/playbooks$`), db.AgentScopeOperator},
	}
)

// RegisterAgentRESTScope classifies a REST write endpoint (path regexp, methods "POST,PUT") with a
// minimal agent scope. Unclassified writes require admin.
func RegisterAgentRESTScope(methods, pathRe string, scope db.AgentScope) {
	agentRESTRulesMu.Lock()
	defer agentRESTRulesMu.Unlock()
	agentRESTRules = append(agentRESTRules, agentRESTRule{methods, regexp.MustCompile(pathRe), scope})
}

// agentRESTScope is the minimal scope of a REST request: reads need read, classified writes
// their rule's scope, any other write admin.
func agentRESTScope(r *http.Request) db.AgentScope {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return db.AgentScopeRead
	}
	agentRESTRulesMu.RLock()
	defer agentRESTRulesMu.RUnlock()
	for _, rule := range agentRESTRules {
		if strings.Contains(rule.methods, r.Method) && rule.path.MatchString(r.URL.Path) {
			return rule.scope
		}
	}
	return db.AgentScopeAdmin
}

// agentRESTAllowed checks scope and allowed projects of an agent REST request; it writes the
// error response and returns false when the request must be rejected.
func agentRESTAllowed(w http.ResponseWriter, r *http.Request, u *db.User, projectId string) bool {
	if projectId != "" && !u.Agent.ProjectAllowed(db.ProjectId(projectId)) {
		http.Error(w, "forbidden: the agent is not allowed to access this project", http.StatusForbidden)
		return false
	}
	if need := agentRESTScope(r); !u.Agent.Scope.Allows(need) {
		http.Error(w, "forbidden: the agent's scope '"+string(u.Agent.Scope)+"' does not allow this action (needs '"+string(need)+"')", http.StatusForbidden)
		return false
	}
	return true
}

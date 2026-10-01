package api

// shards fork: REST API of the Agents area (registry, keys, activity, sessions, dispatch, Ask agent)
// and agent playbooks.

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

const (
	agentActiveWindow = 5 * time.Minute
	agentKeyPrefix    = "shards_agent_"
	askMaxLength      = 8 * 1024
)

// RegisterAgentRoutes adds the Agents area endpoints (called from main before the /api/ catch-all).
func (api *Api) RegisterAgentRoutes(r *mux.Router) {
	r.HandleFunc("/api/project/{project}/agents", api.Auth(api.Agents)).Methods(http.MethodGet, http.MethodPost)
	r.HandleFunc("/api/project/{project}/agents/{agent:[0-9]+}", api.Auth(api.AgentHandler)).Methods(http.MethodGet, http.MethodPut, http.MethodDelete)
	r.HandleFunc("/api/project/{project}/agents/{agent:[0-9]+}/{action}", api.Auth(api.AgentAction)).Methods(http.MethodGet, http.MethodPost)
	r.HandleFunc("/api/project/{project}/playbooks", api.Auth(api.Playbooks)).Methods(http.MethodGet, http.MethodPut)
}

type agentView struct {
	*db.Agent
	SecretSet bool             `json:"secret_set"`
	Owner     string           `json:"owner"`
	Status    string           `json:"status"` // active | idle | never | expired | disabled
	Stats     *db.AgentStats   `json:"stats"`
	Keys      []db.AgentApiKey `json:"keys"`
}

func agentStatus(a *db.Agent, s *db.AgentStats, now time.Time) string {
	switch {
	case a.Disabled:
		return "disabled"
	case a.Expired(now):
		return "expired"
	case s == nil || s.LastSeen == 0:
		return "never"
	case now.Sub(time.UnixMilli(s.LastSeen)) <= agentActiveWindow:
		return "active"
	}
	return "idle"
}

func (api *Api) agentViews(agents []*db.Agent) ([]agentView, error) {
	ids := make([]int, 0, len(agents))
	for _, a := range agents {
		ids = append(ids, a.Id)
	}
	now := time.Now()
	stats, err := api.db.GetAgentStats(ids, now.Add(-time.Hour).UnixMilli())
	if err != nil {
		return nil, err
	}
	users := map[int]string{}
	if us, err := api.db.GetUsers(); err == nil {
		for _, u := range us {
			users[u.Id] = userDisplayName(u)
		}
	}
	res := make([]agentView, 0, len(agents))
	for _, a := range agents {
		v := agentView{Agent: a, Owner: users[a.OwnerId], Stats: stats[a.Id]}
		if a.Dispatch != nil {
			// never return the signing secret, only whether it is set
			d := *a.Dispatch
			v.SecretSet = d.Secret != ""
			d.Secret = ""
			cp := *a
			cp.Dispatch = &d
			v.Agent = &cp
		}
		v.Status = agentStatus(a, v.Stats, now)
		if v.Keys, err = api.db.GetAgentApiKeys(a.Id); err != nil {
			return nil, err
		}
		res = append(res, v)
	}
	return res, nil
}

func (api *Api) canManageAgents(u *db.User, projectId db.ProjectId) bool {
	return api.IsAllowed(u, rbac.Actions.Project(string(projectId)).Settings().Edit())
}

func (api *Api) canViewAgents(u *db.User, projectId db.ProjectId) bool {
	return api.IsAllowed(u, rbac.Actions.Project(string(projectId)).Alerts().View())
}

type agentForm struct {
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	Vendor          string                  `json:"vendor"`
	Model           string                  `json:"model"`
	OwnerId         int                     `json:"owner_id"`
	Scope           db.AgentScope           `json:"scope"`
	ExpiresAt       int64                   `json:"expires_at"`
	AllowedProjects []db.ProjectId          `json:"allowed_projects"`
	Dispatch        *db.AgentDispatchConfig `json:"dispatch"`
	Disabled        bool                    `json:"disabled"`
}

func (f *agentForm) apply(a *db.Agent) {
	a.Name = strings.TrimSpace(f.Name)
	a.Description = strings.TrimSpace(f.Description)
	a.Vendor = strings.TrimSpace(f.Vendor)
	a.Model = strings.TrimSpace(f.Model)
	if f.OwnerId > 0 {
		a.OwnerId = f.OwnerId
	}
	a.Scope = f.Scope
	a.ExpiresAt = f.ExpiresAt
	a.AllowedProjects = f.AllowedProjects
	a.Disabled = f.Disabled
	if f.Dispatch != nil {
		d := *f.Dispatch
		d.URL = strings.TrimSpace(d.URL)
		if d.Secret == "" && a.Dispatch != nil {
			d.Secret = a.Dispatch.Secret // keep the existing secret unless a new one is set
		}
		a.Dispatch = &d
	}
}

func writeAgentDBError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrInvalid):
		http.Error(w, strings.TrimPrefix(err.Error(), db.ErrInvalid.Error()+": "), http.StatusBadRequest)
	case errors.Is(err, db.ErrConflict):
		http.Error(w, "An agent with this name already exists in the project.", http.StatusConflict)
	case errors.Is(err, db.ErrNotFound):
		http.Error(w, "agent not found", http.StatusNotFound)
	default:
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
	}
}

// defaultAgentOwner is the current user, or (anonymous mode) the first admin.
func (api *Api) defaultAgentOwner(u *db.User) int {
	if !u.Anonymous && u.Id > 0 {
		return u.Id
	}
	users, err := api.db.GetUsers()
	if err != nil {
		return 0
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Id < users[j].Id })
	for _, x := range users {
		for _, r := range x.Roles {
			if r == rbac.RoleAdmin {
				return x.Id
			}
		}
	}
	if len(users) > 0 {
		return users[0].Id
	}
	return 0
}

// Agents: GET lists the project's agents, POST creates one.
func (api *Api) Agents(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return
	}
	api.rememberBaseUrl(r)
	if r.Method == http.MethodPost {
		if !api.canManageAgents(u, project.Id) {
			http.Error(w, "You are not allowed to manage agents.", http.StatusForbidden)
			return
		}
		var form agentForm
		if err := utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		a := &db.Agent{ProjectId: project.Id, OwnerId: api.defaultAgentOwner(u)}
		form.apply(a)
		if a.Scope == "" {
			a.Scope = db.AgentScopeRead
		}
		if a.OwnerId == 0 {
			http.Error(w, "owner is required", http.StatusBadRequest)
			return
		}
		if err := api.db.CreateAgent(a); err != nil {
			writeAgentDBError(w, err)
			return
		}
		views, err := api.agentViews([]*db.Agent{a})
		if err != nil {
			writeAgentDBError(w, err)
			return
		}
		utils.WriteJson(w, views[0])
		return
	}
	if !api.canViewAgents(u, project.Id) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	agents, err := api.db.GetAgents(project.Id)
	if err != nil {
		writeAgentDBError(w, err)
		return
	}
	views, err := api.agentViews(agents)
	if err != nil {
		writeAgentDBError(w, err)
		return
	}
	res := map[string]any{"agents": views, "editable": api.canManageAgents(u, project.Id), "default_owner_id": api.defaultAgentOwner(u)}
	if api.canManageAgents(u, project.Id) {
		type userRef struct {
			Id   int    `json:"id"`
			Name string `json:"name"`
		}
		var users []userRef
		if us, err := api.db.GetUsers(); err == nil {
			for _, x := range us {
				users = append(users, userRef{x.Id, userDisplayName(x)})
			}
		}
		res["users"] = users
	}
	utils.WriteJson(w, res)
}

func (api *Api) loadAgent(w http.ResponseWriter, r *http.Request, u *db.User) (*db.Project, *db.Agent) {
	vars := mux.Vars(r)
	project := api.getProjectOrError(w, db.ProjectId(vars["project"]))
	if project == nil {
		return nil, nil
	}
	if !api.canViewAgents(u, project.Id) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, nil
	}
	id, _ := strconv.Atoi(vars["agent"])
	a, err := api.db.GetAgent(project.Id, id)
	if err != nil {
		writeAgentDBError(w, err)
		return nil, nil
	}
	return project, a
}

// AgentHandler: GET returns an agent with its keys, stats, sessions, recent activity and
// deliveries; PUT updates it; DELETE removes it and revokes its keys.
func (api *Api) AgentHandler(w http.ResponseWriter, r *http.Request, u *db.User) {
	project, a := api.loadAgent(w, r, u)
	if a == nil {
		return
	}
	api.rememberBaseUrl(r)
	switch r.Method {
	case http.MethodPut:
		if !api.canManageAgents(u, project.Id) {
			http.Error(w, "You are not allowed to manage agents.", http.StatusForbidden)
			return
		}
		var form agentForm
		if err := utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		form.apply(a)
		if err := api.db.UpdateAgent(a); err != nil {
			writeAgentDBError(w, err)
			return
		}
	case http.MethodDelete:
		if !api.canManageAgents(u, project.Id) {
			http.Error(w, "You are not allowed to manage agents.", http.StatusForbidden)
			return
		}
		keys, err := api.db.GetAgentApiKeys(a.Id)
		if err != nil {
			writeAgentDBError(w, err)
			return
		}
		// the agent's keys are revoked with it, so they don't silently become unscoped user keys
		for _, k := range keys {
			if err = api.db.DeleteUserApiKey(k.UserId, k.Id); err != nil {
				writeAgentDBError(w, err)
				return
			}
		}
		if err = api.db.DeleteAgent(project.Id, a.Id); err != nil {
			writeAgentDBError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	views, err := api.agentViews([]*db.Agent{a})
	if err != nil {
		writeAgentDBError(w, err)
		return
	}
	sessions, err := api.db.GetAgentSessions(a.Id, 20)
	if err != nil {
		writeAgentDBError(w, err)
		return
	}
	activity, err := api.db.GetAgentActivity(a.Id, 0, 100)
	if err != nil {
		writeAgentDBError(w, err)
		return
	}
	deliveries, err := api.db.GetAgentDeliveries(a.Id, 50)
	if err != nil {
		writeAgentDBError(w, err)
		return
	}
	utils.WriteJson(w, map[string]any{
		"agent":      views[0],
		"editable":   api.canManageAgents(u, project.Id),
		"sessions":   sessions,
		"activity":   activity,
		"deliveries": deliveries,
	})
}

type agentKeyForm struct {
	Action      string `json:"action"` // create | link | revoke
	KeyId       int    `json:"key_id"`
	Description string `json:"description"`
}

type agentAskForm struct {
	TargetType  string `json:"target_type"`
	TargetId    string `json:"target_id"`
	Instruction string `json:"instruction"`
}

// AgentAction handles /agents/{id}/activity|sessions|deliveries (GET) and keys|test|ask (POST).
func (api *Api) AgentAction(w http.ResponseWriter, r *http.Request, u *db.User) {
	project, a := api.loadAgent(w, r, u)
	if a == nil {
		return
	}
	action := mux.Vars(r)["action"]
	if r.Method == http.MethodGet {
		var res any
		var err error
		switch action {
		case "activity":
			before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			res, err = api.db.GetAgentActivity(a.Id, before, limit)
		case "sessions":
			res, err = api.db.GetAgentSessions(a.Id, 50)
		case "deliveries":
			res, err = api.db.GetAgentDeliveries(a.Id, 100)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeAgentDBError(w, err)
			return
		}
		utils.WriteJson(w, res)
		return
	}
	api.rememberBaseUrl(r)
	switch action {
	case "keys":
		if !api.canManageAgents(u, project.Id) {
			http.Error(w, "You are not allowed to manage agents.", http.StatusForbidden)
			return
		}
		var form agentKeyForm
		if err := utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		switch form.Action {
		case "create":
			desc := strings.TrimSpace(form.Description)
			if desc == "" {
				desc = a.Name
			}
			key := agentKeyPrefix + utils.RandomString(32)
			id, err := api.db.CreateAgentApiKey(a.Id, a.OwnerId, key, desc)
			if err != nil {
				if errors.Is(err, db.ErrConflict) {
					http.Error(w, "A key with this description already exists.", http.StatusConflict)
					return
				}
				writeAgentDBError(w, err)
				return
			}
			utils.WriteJson(w, map[string]any{"id": id, "key": key})
		case "link":
			keys, err := api.db.GetUserApiKeys(a.OwnerId)
			if err != nil {
				writeAgentDBError(w, err)
				return
			}
			found := false
			for _, k := range keys {
				found = found || k.Id == form.KeyId
			}
			if !found {
				http.Error(w, "only API keys of the agent's owner can be linked", http.StatusBadRequest)
				return
			}
			if err = api.db.LinkAgentApiKey(a.Id, form.KeyId); err != nil {
				writeAgentDBError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "revoke":
			keys, err := api.db.GetAgentApiKeys(a.Id)
			if err != nil {
				writeAgentDBError(w, err)
				return
			}
			for _, k := range keys {
				if k.Id != form.KeyId {
					continue
				}
				if err = api.db.DeleteUserApiKey(k.UserId, k.Id); err == nil {
					err = api.db.UnlinkAgentApiKey(a.Id, k.Id)
				}
				if err != nil {
					writeAgentDBError(w, err)
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "action must be create, link or revoke", http.StatusBadRequest)
		}
	case "test":
		if !api.canManageAgents(u, project.Id) {
			http.Error(w, "You are not allowed to manage agents.", http.StatusForbidden)
			return
		}
		if a.Dispatch == nil || a.Dispatch.URL == "" {
			http.Error(w, "Configure the dispatch webhook URL first.", http.StatusBadRequest)
			return
		}
		d := api.agentDispatcher()
		dl, err := d.enqueue(project, a, AgentEvent{Type: db.AgentEventTest, RequestedBy: userDisplayName(u)}, true)
		if err != nil {
			writeAgentDBError(w, err)
			return
		}
		// deliver right away so the UI shows the outcome; failures are retried in the background
		d.deliver(dl)
		utils.WriteJson(w, dl)
	case "ask":
		var form agentAskForm
		if err := utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		form.Instruction = strings.TrimSpace(form.Instruction)
		if len(form.Instruction) > askMaxLength {
			http.Error(w, "instruction is too long", http.StatusBadRequest)
			return
		}
		t, err := api.resolveCommentTarget(u, project, form.TargetType, form.TargetId, true)
		if err != nil {
			writeTargetError(w, err)
			return
		}
		if a.Disabled || a.Expired(time.Now()) {
			http.Error(w, "The agent is disabled or expired.", http.StatusBadRequest)
			return
		}
		if a.Dispatch == nil || !a.Dispatch.Enabled || a.Dispatch.URL == "" {
			http.Error(w, "The agent has no dispatch webhook: configure it on the agent page.", http.StatusBadRequest)
			return
		}
		ev := AgentEvent{Type: db.AgentEventManual, Instruction: form.Instruction, RequestedBy: userDisplayName(u), TargetType: string(t.typ), TargetId: t.id, Incident: t.incident, Alert: t.alert, Rule: t.rule}
		dl, err := api.agentDispatcher().enqueue(project, a, ev, true)
		if err != nil {
			writeAgentDBError(w, err)
			return
		}
		api.recordAction(newActor(u, viaUI), project.Id, t.typ, t.id, actionAskedAgent, form.Instruction,
			map[string]string{"agent": a.Name, "agent_id": strconv.Itoa(a.Id), "delivery_id": strconv.FormatInt(dl.Id, 10)})
		utils.WriteJson(w, dl)
	default:
		http.NotFound(w, r)
	}
}

const actionAskedAgent = "asked_agent"

type playbookForm struct {
	TargetType string `json:"target_type"`
	TargetId   string `json:"target_id"`
	Body       string `json:"body"`
}

// Playbooks: GET ?target_type=alerting_rule|application&target_id=... returns the playbook (body
// is empty if none); PUT {target_type, target_id, body} stores it (empty body deletes it).
func (api *Api) Playbooks(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return
	}
	var form playbookForm
	if r.Method == http.MethodPut {
		if err := utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		form.TargetType, form.TargetId = r.URL.Query().Get("target_type"), r.URL.Query().Get("target_id")
	}
	pid := string(project.Id)
	switch form.TargetType {
	case db.PlaybookTargetAlertingRule:
		if !api.IsAllowed(u, rbac.Actions.Project(pid).AlertingRules().View()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	case db.PlaybookTargetApplication:
		id, err := model.NewApplicationIdFromString(form.TargetId, "")
		if err != nil {
			http.Error(w, "invalid application id", http.StatusBadRequest)
			return
		}
		form.TargetId = id.String()
		if !api.IsAllowed(u, rbac.Actions.Project(pid).Application(project.CalcApplicationCategory(id), id.Namespace, id.Kind, id.Name).View()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	default:
		http.Error(w, "target_type must be alerting_rule or application", http.StatusBadRequest)
		return
	}
	editable := api.IsAllowed(u, rbac.Actions.Project(pid).AlertingRules().Edit())
	if r.Method == http.MethodPut {
		if !editable {
			http.Error(w, "You are not allowed to edit playbooks (requires alerting rules edit permission).", http.StatusForbidden)
			return
		}
		p := &db.Playbook{ProjectId: project.Id, TargetType: form.TargetType, TargetId: form.TargetId, Body: form.Body, UpdatedBy: newActor(u, viaUI).name}
		if err := api.db.SetPlaybook(p); err != nil {
			writeAgentDBError(w, err)
			return
		}
	}
	p, err := api.db.GetPlaybook(project.Id, form.TargetType, form.TargetId)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		writeAgentDBError(w, err)
		return
	}
	if p == nil {
		p = &db.Playbook{ProjectId: project.Id, TargetType: form.TargetType, TargetId: form.TargetId}
	}
	utils.WriteJson(w, map[string]any{"playbook": p, "editable": editable})
}

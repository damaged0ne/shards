package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

// shards fork: maintenance windows (REST). See db/maintenance_shards.go and notifications/maintenance_shards.go.

const maintenanceMaxQuickMinutes = 7 * 24 * 60

type maintenanceWindowView struct {
	*db.MaintenanceWindow
	Status      string          `json:"status"`
	CurrentFrom timeseries.Time `json:"current_from"`
	CurrentTo   timeseries.Time `json:"current_to"`
}

func renderMaintenanceWindow(w *db.MaintenanceWindow, now timeseries.Time) maintenanceWindowView {
	v := maintenanceWindowView{MaintenanceWindow: w, Status: w.Status(now)}
	if v.Status == "active" || v.Status == "scheduled" {
		v.CurrentFrom, v.CurrentTo = w.CurrentOrNext(now)
	}
	return v
}

// maintenanceForm is the body of create/update: a window plus the "for the next N minutes" shortcut.
type maintenanceForm struct {
	db.MaintenanceWindow
	DurationMinutes int `json:"duration_minutes,omitempty"`
}

func (f *maintenanceForm) window(now timeseries.Time) (*db.MaintenanceWindow, error) {
	w := f.MaintenanceWindow
	if f.DurationMinutes != 0 {
		if f.DurationMinutes < 0 || f.DurationMinutes > maintenanceMaxQuickMinutes {
			return nil, &targetError{http.StatusBadRequest, "duration_minutes must be 1..10080"}
		}
		if w.StartsAt == 0 {
			w.StartsAt = now
		}
		w.EndsAt = w.StartsAt.Add(timeseries.Duration(f.DurationMinutes) * timeseries.Minute)
	}
	if w.Recurrence != nil && w.Recurrence.Timezone == "" {
		w.Recurrence.Timezone = "UTC"
	}
	cleanList := func(l []string) []string {
		var res []string
		for _, s := range l {
			if s = strings.TrimSpace(s); s != "" {
				res = append(res, s)
			}
		}
		return res
	}
	w.Scope.ApplicationPatterns = cleanList(w.Scope.ApplicationPatterns)
	w.Scope.Categories = cleanList(w.Scope.Categories)
	w.Scope.NodePatterns = cleanList(w.Scope.NodePatterns)
	w.Scope.AlertingRuleIds = cleanList(w.Scope.AlertingRuleIds)
	if err := w.Validate(); err != nil {
		return nil, &targetError{http.StatusBadRequest, strings.TrimPrefix(err.Error(), db.ErrInvalid.Error()+": ")}
	}
	return &w, nil
}

func maintenanceSummary(w *db.MaintenanceWindow) string {
	scope := "everything"
	if !w.Scope.IsAll() {
		var parts []string
		if len(w.Scope.ApplicationPatterns) > 0 {
			parts = append(parts, "apps "+strings.Join(w.Scope.ApplicationPatterns, ", "))
		}
		if len(w.Scope.Categories) > 0 {
			parts = append(parts, "categories "+strings.Join(w.Scope.Categories, ", "))
		}
		if len(w.Scope.NodePatterns) > 0 {
			parts = append(parts, "nodes "+strings.Join(w.Scope.NodePatterns, ", "))
		}
		if len(w.Scope.AlertingRuleIds) > 0 {
			parts = append(parts, "rules "+strings.Join(w.Scope.AlertingRuleIds, ", "))
		}
		scope = strings.Join(parts, "; ")
	}
	when := fmt.Sprintf("%s – %s", fmtTime(w.StartsAt), fmtTime(w.EndsAt))
	if r := w.Recurrence; r != nil {
		when = fmt.Sprintf("weekly on %v at %s %s for %dm", r.Weekdays, r.StartTime, r.Timezone, r.DurationMinutes)
	}
	return fmt.Sprintf("Maintenance window \"%s\" (%s) muting %s", w.Name, when, scope)
}

func (api *Api) createMaintenanceWindow(project *db.Project, a actor, w *db.MaintenanceWindow) (*maintenanceWindowView, error) {
	w.Id = 0
	w.ProjectId = project.Id
	w.CreatedBy = a.name
	w.CreatedByKind = a.kind
	w.CreatedAt = 0
	w.EndedAt, w.EndedBy = 0, ""
	if err := api.db.CreateMaintenanceWindow(w); err != nil {
		if errors.Is(err, db.ErrInvalid) {
			return nil, &targetError{http.StatusBadRequest, err.Error()}
		}
		return nil, err
	}
	api.recordAction(a, project.Id, db.CommentTargetMaintenanceWindow, strconv.Itoa(w.Id), "created", w.Comment, map[string]string{"window": w.Name})
	v := renderMaintenanceWindow(w, timeseries.Now())
	return &v, nil
}

func (api *Api) endMaintenanceWindow(project *db.Project, a actor, id int, comment string) (*maintenanceWindowView, error) {
	if err := api.db.EndMaintenanceWindow(project.Id, id, a.name, timeseries.Now()); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, &targetError{http.StatusNotFound, "maintenance window not found or already ended"}
		}
		return nil, err
	}
	w, err := api.db.GetMaintenanceWindow(project.Id, id)
	if err != nil {
		return nil, err
	}
	api.recordAction(a, project.Id, db.CommentTargetMaintenanceWindow, strconv.Itoa(id), "ended", comment, map[string]string{"window": w.Name})
	v := renderMaintenanceWindow(w, timeseries.Now())
	return &v, nil
}

func (api *Api) maintenanceAccess(w http.ResponseWriter, r *http.Request, u *db.User, write bool) *db.Project {
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return nil
	}
	action := rbac.Actions.Project(string(project.Id)).Alerts().View()
	if write {
		action = rbac.Actions.Project(string(project.Id)).Alerts().Edit()
	}
	if !api.IsAllowed(u, action) {
		http.Error(w, "", http.StatusForbidden)
		return nil
	}
	return project
}

// MaintenanceWindows handles GET (?include_ended=true) and POST (create) on /api/project/{project}/maintenance.
func (api *Api) MaintenanceWindows(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.maintenanceAccess(w, r, u, r.Method == http.MethodPost)
	if project == nil {
		return
	}
	now := timeseries.Now()
	switch r.Method {
	case http.MethodGet:
		windows, err := api.db.GetMaintenanceWindows(project.Id, true)
		if err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		includeEnded := r.URL.Query().Get("include_ended") == "true"
		res := make([]maintenanceWindowView, 0, len(windows))
		for _, mw := range windows {
			v := renderMaintenanceWindow(mw, now)
			if !includeEnded && (v.Status == "ended" || v.Status == "expired") {
				continue
			}
			res = append(res, v)
		}
		utils.WriteJson(w, res)
	case http.MethodPost:
		var form maintenanceForm
		if err := utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		mw, err := form.window(now)
		if err != nil {
			writeTargetError(w, err)
			return
		}
		a := newActor(u, viaUI)
		if api.gateREST(w, project, a, db.AgentActionCreateMaintenanceWindow, maintenanceCreateArgs{Window: mw}, maintenanceSummary(mw), gatedTarget{}, mw.Comment) {
			return
		}
		v, err := api.createMaintenanceWindow(project, a, mw)
		if err != nil {
			writeTargetError(w, err)
			return
		}
		utils.WriteJson(w, v)
	}
}

// MaintenanceWindow handles GET, PUT (update) and DELETE on /api/project/{project}/maintenance/{id},
// and POST {"comment": "..."} on /api/project/{project}/maintenance/{id}/end.
func (api *Api) MaintenanceWindow(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.maintenanceAccess(w, r, u, r.Method != http.MethodGet)
	if project == nil {
		return
	}
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	existing, err := api.db.GetMaintenanceWindow(project.Id, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Error(w, "maintenance window not found", http.StatusNotFound)
			return
		}
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	now := timeseries.Now()
	a := newActor(u, viaUI)
	end := strings.HasSuffix(r.URL.Path, "/end")
	switch {
	case r.Method == http.MethodGet:
		utils.WriteJson(w, renderMaintenanceWindow(existing, now))
	case end || r.Method == http.MethodDelete:
		var form struct {
			Comment string `json:"comment"`
		}
		if end {
			_ = utils.ReadJson(r, &form)
		}
		if api.gateREST(w, project, a, db.AgentActionEndMaintenanceWindow, maintenanceEndArgs{Id: id, Comment: form.Comment},
			"End the maintenance window \""+existing.Name+"\"", gatedTarget{typ: db.CommentTargetMaintenanceWindow, id: strconv.Itoa(id), title: existing.Name}, form.Comment) {
			return
		}
		if r.Method == http.MethodDelete {
			if err = api.db.DeleteMaintenanceWindow(project.Id, id); err != nil {
				klog.Errorln(err)
				http.Error(w, "", http.StatusInternalServerError)
				return
			}
			api.recordAction(a, project.Id, db.CommentTargetMaintenanceWindow, strconv.Itoa(id), "deleted", "", map[string]string{"window": existing.Name})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		v, err := api.endMaintenanceWindow(project, a, id, form.Comment)
		if err != nil {
			writeTargetError(w, err)
			return
		}
		utils.WriteJson(w, v)
	case r.Method == http.MethodPut:
		var form maintenanceForm
		if err := utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		mw, err := form.window(now)
		if err != nil {
			writeTargetError(w, err)
			return
		}
		mw.Id, mw.ProjectId = id, project.Id
		// agents changing a window are subject to the same policy as creating one
		if a.kind == db.CommentAuthorAgent && project.Settings.AgentApprovals.Effective(db.AgentActionCreateMaintenanceWindow) != db.ApprovalPolicyAuto {
			http.Error(w, "agents can't edit maintenance windows under the current approval policy; end it and create a new one", http.StatusForbidden)
			return
		}
		if err = api.db.UpdateMaintenanceWindow(mw); err != nil {
			writeTargetError(w, &targetError{http.StatusBadRequest, err.Error()})
			return
		}
		api.recordAction(a, project.Id, db.CommentTargetMaintenanceWindow, strconv.Itoa(id), "updated", "", map[string]string{"window": mw.Name})
		updated, err := api.db.GetMaintenanceWindow(project.Id, id)
		if err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		utils.WriteJson(w, renderMaintenanceWindow(updated, now))
	}
}

func init() {
	// agent scopes of the REST write endpoints (see agents_scope_shards.go); approvals and the
	// approval policy stay admin-only for agents (and humans-only in the handlers)
	RegisterAgentRESTScope("POST", `/api/project/[^/]+/incident/[^/]+/workflow$`, db.AgentScopeTriage)
	RegisterAgentRESTScope("POST,PUT,DELETE", `/api/project/[^/]+/maintenance(/\d+(/end)?)?$`, db.AgentScopeOperator)
}

// RegisterWorkflowRoutes registers the shards fork's incident workflow, maintenance, approval and home endpoints.
func (api *Api) RegisterWorkflowRoutes(r *mux.Router) {
	get, post, put, del := http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete
	r.HandleFunc("/api/project/{project}/home", api.Auth(api.Home)).Methods(get)
	r.HandleFunc("/api/project/{project}/incidents/workflow", api.Auth(api.IncidentsWorkflow)).Methods(get)
	r.HandleFunc("/api/project/{project}/incident/{incident}/workflow", api.Auth(api.IncidentWorkflow)).Methods(get, post)
	r.HandleFunc("/api/project/{project}/incident/{incident}/postmortem", api.Auth(api.IncidentPostmortem)).Methods(get)
	r.HandleFunc("/api/project/{project}/maintenance", api.Auth(api.MaintenanceWindows)).Methods(get, post)
	r.HandleFunc("/api/project/{project}/maintenance/{id}", api.Auth(api.MaintenanceWindow)).Methods(get, put, del)
	r.HandleFunc("/api/project/{project}/maintenance/{id}/end", api.Auth(api.MaintenanceWindow)).Methods(post)
	r.HandleFunc("/api/project/{project}/approvals", api.Auth(api.Approvals)).Methods(get)
	r.HandleFunc("/api/project/{project}/approvals/policy", api.Auth(api.ApprovalPolicy)).Methods(get, put)
	r.HandleFunc("/api/project/{project}/approvals/{id}", api.Auth(api.Approval)).Methods(get, post)
	r.HandleFunc("/api/project/{project}/service_map_settings", api.Auth(api.ServiceMapSettings)).Methods(get, put)
}

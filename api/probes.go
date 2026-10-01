package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/probes"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

// Shards fork: synthetic probes REST API.
//
//	GET    /api/project/{project}/probes            the probes with their results within the time range
//	POST   /api/project/{project}/probes            create a probe ({"name": ..., "spec": {...}})
//	POST   /api/project/{project}/probes/test       run a probe spec once without saving it
//	GET    /api/project/{project}/probes/{probe}    one probe
//	PUT    /api/project/{project}/probes/{probe}    update a probe
//	DELETE /api/project/{project}/probes/{probe}    delete a probe

const probeTestTimeout = 30 * time.Second

type probeForm struct {
	Name string       `json:"name"`
	Spec db.ProbeSpec `json:"spec"`
}

type ProbeView struct {
	Id            string                 `json:"id"`
	Name          string                 `json:"name"`
	Spec          db.ProbeSpec           `json:"spec"`
	ApplicationId string                 `json:"application_id"`
	Linked        bool                   `json:"linked"`
	Status        string                 `json:"status"` // up | down | unknown | paused
	Uptime        *float32               `json:"uptime"` // percent within the time range
	LatencyP95    *float32               `json:"latency_p95"`
	LatencyLast   *float32               `json:"latency_last"`
	StatusCode    int                    `json:"status_code,omitempty"`
	CertDaysLeft  *float32               `json:"cert_days_left"`
	CertSubject   string                 `json:"cert_subject,omitempty"`
	CertIssuer    string                 `json:"cert_issuer,omitempty"`
	CertNotAfter  string                 `json:"cert_not_after,omitempty"`
	CertValid     *bool                  `json:"cert_valid,omitempty"`
	LastError     string                 `json:"last_error,omitempty"`
	LastRunAt     timeseries.Time        `json:"last_run_at,omitempty"`
	ConsecFails   int                    `json:"consecutive_failures"`
	UpChart       *timeseries.TimeSeries `json:"up_chart,omitempty"`
	LatencyChart  *timeseries.TimeSeries `json:"latency_chart,omitempty"`
	CreatedAt     timeseries.Time        `json:"created_at"`
	UpdatedAt     timeseries.Time        `json:"updated_at"`
}

func f32(v float32) *float32 {
	if timeseries.IsNaN(v) {
		return nil
	}
	return &v
}

// probeViews combines the definitions and the latest states from the DB with the results from the world.
func probeViews(project *db.Project, defs []*db.Probe, w *model.World, withCharts bool) []ProbeView {
	byId := map[string]*model.Probe{}
	if w != nil {
		for _, p := range w.Probes {
			byId[p.Id] = p
		}
	}
	res := make([]ProbeView, 0, len(defs))
	for _, d := range defs {
		v := ProbeView{
			Id:          d.Id,
			Name:        d.Name,
			Spec:        d.Spec,
			Status:      "unknown",
			LastError:   d.State.Error,
			LastRunAt:   d.State.LastRunAt,
			StatusCode:  d.State.StatusCode,
			ConsecFails: d.State.ConsecutiveFailures,
			CertValid:   d.State.CertValid,
			CreatedAt:   d.CreatedAt,
			UpdatedAt:   d.UpdatedAt,
		}
		if d.State.LastRunAt > 0 {
			if d.State.Up {
				v.Status = "up"
			} else {
				v.Status = "down"
			}
			if d.State.CertNotAfter > 0 {
				v.CertDaysLeft = f32(float32(d.State.CertNotAfter.Sub(timeseries.Now())) / (24 * 3600))
				v.CertSubject, v.CertIssuer = d.State.CertSubject, d.State.CertIssuer
				v.CertNotAfter = d.State.CertNotAfter.ToStandard().UTC().Format(time.RFC3339)
			}
		}
		if d.Spec.Paused {
			v.Status = "paused"
		}
		if p := byId[d.Id]; p != nil {
			v.ApplicationId = p.ApplicationId.String()
			v.Linked = p.Linked
			v.Uptime = f32(p.UptimePercent())
			v.LatencyP95 = f32(p.LatencyQuantile(0.95))
			_, last := p.Durations["total"].LastNotNull()
			if !p.Durations["total"].IsEmpty() {
				v.LatencyLast = f32(last)
			}
			if v.CertNotAfter == "" && p.CertNotAfter != "" {
				v.CertNotAfter, v.CertSubject, v.CertIssuer = p.CertNotAfter, p.CertSubject, p.CertIssuer
			}
			if withCharts {
				v.UpChart = p.Up
				v.LatencyChart = p.Durations["total"]
			}
		} else {
			v.ApplicationId = d.Spec.ApplicationId
			v.Linked = d.Spec.ApplicationId != ""
			if !v.Linked {
				v.ApplicationId = model.NewApplicationId(project.ClusterId(), model.ProbeNamespace, model.ApplicationKindProbe, d.Name).String()
			}
		}
		if v.LatencyLast == nil && d.State.LastRunAt > 0 {
			v.LatencyLast = f32(d.State.Duration)
		}
		res = append(res, v)
	}
	return res
}

type probeApplication struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Ns   string `json:"ns"`
	Kind string `json:"kind"`
}

func probeApplications(w *model.World) []probeApplication {
	res := []probeApplication{}
	if w == nil {
		return res
	}
	for _, app := range w.Applications {
		if app.Id.Kind == model.ApplicationKindProbe {
			continue
		}
		res = append(res, probeApplication{Id: app.Id.String(), Name: app.Id.Name, Ns: app.Id.Namespace, Kind: string(app.Id.Kind)})
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].Name != res[j].Name {
			return res[i].Name < res[j].Name
		}
		return res[i].Id < res[j].Id
	})
	return res
}

// validateProbe normalizes the form; it returns a user-facing error.
func validateProbe(project *db.Project, name string, spec *db.ProbeSpec) error {
	if project.Multicluster() {
		return fmt.Errorf("probes can't be created in a multi-cluster project: create them in a member project")
	}
	if err := db.ValidateProbeName(name); err != nil {
		return err
	}
	if spec.ApplicationId != "" {
		id, err := model.NewApplicationIdFromString(spec.ApplicationId, project.ClusterId())
		if err != nil {
			return fmt.Errorf("invalid application id: %s", spec.ApplicationId)
		}
		if id.Kind == model.ApplicationKindProbe {
			spec.ApplicationId = ""
		} else {
			spec.ApplicationId = id.String()
		}
	}
	return spec.Normalize()
}

func (api *Api) createProbe(project *db.Project, name string, spec db.ProbeSpec) (*db.Probe, error) {
	if err := validateProbe(project, name, &spec); err != nil {
		return nil, &targetError{status: http.StatusBadRequest, msg: err.Error()}
	}
	p := &db.Probe{ProjectId: project.Id, Name: name, Spec: spec}
	if err := api.db.CreateProbe(p); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return nil, &targetError{status: http.StatusConflict, msg: "a probe with this name already exists"}
		}
		return nil, err
	}
	probes.NotifyChanged()
	return p, nil
}

func (api *Api) updateProbe(project *db.Project, p *db.Probe, name string, spec db.ProbeSpec) error {
	if err := validateProbe(project, name, &spec); err != nil {
		return &targetError{status: http.StatusBadRequest, msg: err.Error()}
	}
	p.Name, p.Spec = name, spec
	if err := api.db.UpdateProbe(p); err != nil {
		switch {
		case errors.Is(err, db.ErrConflict):
			return &targetError{status: http.StatusConflict, msg: "a probe with this name already exists"}
		case errors.Is(err, db.ErrNotFound):
			return &targetError{status: http.StatusNotFound, msg: "probe not found"}
		}
		return err
	}
	probes.NotifyChanged()
	return nil
}

func (api *Api) deleteProbe(project *db.Project, id string) error {
	if err := api.db.DeleteProbe(project.Id, id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return &targetError{status: http.StatusNotFound, msg: "probe not found"}
		}
		return err
	}
	probes.NotifyChanged()
	return nil
}

// testProbe runs the spec once from the server (subject to the same address guard as the scheduler).
func testProbe(ctx context.Context, project *db.Project, spec db.ProbeSpec) (*probes.Result, error) {
	if err := validateProbe(project, "test", &spec); err != nil {
		return nil, &targetError{status: http.StatusBadRequest, msg: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, probeTestTimeout)
	defer cancel()
	return probes.DefaultRunner().Run(ctx, spec), nil
}

func (api *Api) Probes(w http.ResponseWriter, r *http.Request, u *db.User) {
	projectId := mux.Vars(r)["project"]
	if r.Method == http.MethodGet {
		world, project, cacheStatus, err := api.LoadWorldByRequest(r)
		if err != nil {
			klog.Errorln(err)
		}
		if project == nil {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		defs, err := api.db.GetProbes(project.Id)
		if err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		res := struct {
			Probes       []ProbeView        `json:"probes"`
			Applications []probeApplication `json:"applications"`
			Editable     bool               `json:"editable"`
			Multicluster bool               `json:"multicluster"`
		}{
			Probes:       probeViews(project, defs, world, true),
			Applications: probeApplications(world),
			Editable:     api.IsAllowed(u, rbac.Actions.Project(projectId).Probes().Edit()),
			Multicluster: project.Multicluster(),
		}
		utils.WriteJson(w, api.WithContext(project, cacheStatus, world, res))
		return
	}

	if !api.IsAllowed(u, rbac.Actions.Project(projectId).Probes().Edit()) {
		http.Error(w, "You are not allowed to configure probes.", http.StatusForbidden)
		return
	}
	project := api.getProjectOrError(w, db.ProjectId(projectId))
	if project == nil {
		return
	}
	var form probeForm
	if err := utils.ReadJson(r, &form); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	form.Name = strings.TrimSpace(form.Name)
	p, err := api.createProbe(project, form.Name, form.Spec)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	utils.WriteJson(w, probeViews(project, []*db.Probe{p}, nil, false)[0])
}

func (api *Api) ProbeTest(w http.ResponseWriter, r *http.Request, u *db.User) {
	projectId := mux.Vars(r)["project"]
	if !api.IsAllowed(u, rbac.Actions.Project(projectId).Probes().Edit()) {
		http.Error(w, "You are not allowed to configure probes.", http.StatusForbidden)
		return
	}
	project := api.getProjectOrError(w, db.ProjectId(projectId))
	if project == nil {
		return
	}
	var form probeForm
	if err := utils.ReadJson(r, &form); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	res, err := testProbe(r.Context(), project, form.Spec)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	utils.WriteJson(w, res)
}

func (api *Api) Probe(w http.ResponseWriter, r *http.Request, u *db.User) {
	vars := mux.Vars(r)
	projectId := vars["project"]
	if r.Method != http.MethodGet && !api.IsAllowed(u, rbac.Actions.Project(projectId).Probes().Edit()) {
		http.Error(w, "You are not allowed to configure probes.", http.StatusForbidden)
		return
	}
	project := api.getProjectOrError(w, db.ProjectId(projectId))
	if project == nil {
		return
	}
	p, err := api.db.GetProbe(project.Id, vars["probe"])
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Error(w, "probe not found", http.StatusNotFound)
			return
		}
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		utils.WriteJson(w, probeViews(project, []*db.Probe{p}, nil, false)[0])
	case http.MethodPut:
		var form probeForm
		if err = utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err = api.updateProbe(project, p, strings.TrimSpace(form.Name), form.Spec); err != nil {
			writeTargetError(w, err)
			return
		}
		utils.WriteJson(w, probeViews(project, []*db.Probe{p}, nil, false)[0])
	case http.MethodDelete:
		// delete_probe is a gated agent action: API keys of agents may get 202 + a pending approval
		args := probeDeleteArgs{Id: p.Id}
		if api.gateREST(w, project, newActor(u, viaUI), db.AgentActionDeleteProbe, args, "Delete the probe \""+p.Name+"\"", gatedTarget{}, "") {
			return
		}
		if err = api.deleteProbe(project, p.Id); err != nil {
			writeTargetError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "", http.StatusMethodNotAllowed)
	}
}

func init() {
	// agent scopes of the REST write endpoints (see agents_scope_shards.go)
	RegisterAgentRESTScope("POST,PUT,DELETE", `/api/project/[^/]+/probes(/[^/]+)?$`, db.AgentScopeOperator)
}

// RegisterProbeRoutes adds the probe endpoints to the router.
func (api *Api) RegisterProbeRoutes(r *mux.Router) {
	r.HandleFunc("/api/project/{project}/probes", api.Auth(api.Probes)).Methods(http.MethodGet, http.MethodPost)
	r.HandleFunc("/api/project/{project}/probes/test", api.Auth(api.ProbeTest)).Methods(http.MethodPost)
	r.HandleFunc("/api/project/{project}/probes/{probe}", api.Auth(api.Probe)).Methods(http.MethodGet, http.MethodPut, http.MethodDelete)
}

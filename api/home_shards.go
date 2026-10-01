package api

import (
	"context"
	"net/http"
	"sort"

	"github.com/coroot/coroot/cache"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

// shards fork: the "Requires attention" home page of a project.

const (
	homeMaxAlerts   = 30
	homeMaxActivity = 15
)

// AttentionCounts is added to the response context (rail badge of the Home item).
type AttentionCounts struct {
	UnacknowledgedIncidents int `json:"unacknowledged_incidents"`
	PendingApprovals        int `json:"pending_approvals"`
}

func (api *Api) attentionCounts(projectId db.ProjectId) *AttentionCounts {
	res := &AttentionCounts{}
	var err error
	if res.UnacknowledgedIncidents, err = api.db.CountUnacknowledgedIncidents(projectId); err != nil {
		klog.Errorln(err)
	}
	if res.PendingApprovals, err = api.db.CountPendingApprovals(projectId); err != nil {
		klog.Errorln(err)
	}
	return res
}

type homeIncident struct {
	Key              string            `json:"key"`
	ApplicationId    string            `json:"application_id"`
	ShortDescription string            `json:"short_description"`
	Severity         string            `json:"severity"`
	Status           db.IncidentStatus `json:"status"`
	Assignee         string            `json:"assignee,omitempty"`
	OpenedAt         timeseries.Time   `json:"opened_at"`
	InMaintenance    bool              `json:"in_maintenance,omitempty"`
}

type homeAlert struct {
	Id            string          `json:"id"`
	Summary       string          `json:"summary"`
	Severity      string          `json:"severity"`
	ApplicationId string          `json:"application_id,omitempty"`
	RuleName      string          `json:"rule_name,omitempty"`
	OpenedAt      timeseries.Time `json:"opened_at"`
	InMaintenance bool            `json:"in_maintenance,omitempty"`
}

type homeActivity struct {
	*db.Comment
	TargetTitle string `json:"target_title,omitempty"`
}

type homeView struct {
	Incidents   []homeIncident          `json:"incidents"`
	Alerts      []homeAlert             `json:"alerts"`
	AlertsTotal int                     `json:"alerts_total"`
	Approvals   []*db.Approval          `json:"approvals"`
	Maintenance []maintenanceWindowView `json:"maintenance"`
	Activity    []homeActivity          `json:"activity"`
}

func (api *Api) renderHome(u *db.User, project *db.Project) (*homeView, error) {
	pid := string(project.Id)
	now := timeseries.Now()
	res := &homeView{Incidents: []homeIncident{}, Alerts: []homeAlert{}, Approvals: []*db.Approval{}, Maintenance: []maintenanceWindowView{}, Activity: []homeActivity{}}

	incidents, err := api.db.GetOpenIncidents(project.Id)
	if err != nil {
		return nil, err
	}
	wfs, err := api.db.GetIncidentWorkflows(project.Id, nil)
	if err != nil {
		return nil, err
	}
	incidentMarks, err := api.db.GetMaintenanceMarks(project.Id, db.CommentTargetIncident, false)
	if err != nil {
		return nil, err
	}
	for _, i := range incidents {
		cat := project.CalcApplicationCategory(i.ApplicationId)
		if !api.IsAllowed(u, rbac.Actions.Project(pid).Application(cat, i.ApplicationId.Namespace, i.ApplicationId.Kind, i.ApplicationId.Name).View()) {
			continue
		}
		wf := wfs[i.Key]
		e := effectiveIncidentWorkflow(i, wf)
		res.Incidents = append(res.Incidents, homeIncident{
			Key: i.Key, ApplicationId: i.ApplicationId.String(), ShortDescription: i.ShortDescription(),
			Severity: effectiveIncidentSeverity(i, wf), Status: e.Status, Assignee: e.Assignee, OpenedAt: i.OpenedAt,
			InMaintenance: incidentMarks[i.Key] != nil,
		})
	}
	statusRank := map[db.IncidentStatus]int{db.IncidentStatusTriggered: 0, db.IncidentStatusAcknowledged: 1, db.IncidentStatusMitigated: 2}
	sort.SliceStable(res.Incidents, func(a, b int) bool {
		ia, ib := res.Incidents[a], res.Incidents[b]
		if statusRank[ia.Status] != statusRank[ib.Status] {
			return statusRank[ia.Status] < statusRank[ib.Status]
		}
		if ia.Severity != ib.Severity {
			return ia.Severity == model.CRITICAL.String()
		}
		return ia.OpenedAt > ib.OpenedAt
	})

	if api.IsAllowed(u, rbac.Actions.Project(pid).Alerts().View()) {
		alerts, err := api.db.QueryAlerts(project.Id, db.AlertsQuery{SortDesc: true, Limit: 500})
		if err != nil {
			return nil, err
		}
		rules, err := api.db.GetAlertingRules(project.Id)
		if err != nil {
			return nil, err
		}
		ruleNames := map[string]string{}
		for _, r := range rules {
			ruleNames[string(r.Id)] = r.Name
		}
		alertMarks, err := api.db.GetMaintenanceMarks(project.Id, db.CommentTargetAlert, false)
		if err != nil {
			return nil, err
		}
		for _, a := range alerts.Alerts {
			if a.ResolvedAt != 0 || a.ManuallyResolvedAt != 0 || a.Suppressed || a.Severity < model.WARNING {
				continue
			}
			if !a.ApplicationId.IsZero() && !api.IsAllowed(u, rbac.Actions.Project(pid).Application(a.ApplicationCategory, a.ApplicationId.Namespace, a.ApplicationId.Kind, a.ApplicationId.Name).View()) {
				continue
			}
			res.AlertsTotal++
			if len(res.Alerts) >= homeMaxAlerts {
				continue
			}
			h := homeAlert{Id: a.Id, Summary: a.Summary, Severity: a.Severity.String(), RuleName: ruleNames[a.RuleId], OpenedAt: a.OpenedAt, InMaintenance: alertMarks[a.Id] != nil}
			if !a.ApplicationId.IsZero() {
				h.ApplicationId = a.ApplicationId.String()
			}
			res.Alerts = append(res.Alerts, h)
		}
		sort.SliceStable(res.Alerts, func(a, b int) bool {
			if res.Alerts[a].Severity != res.Alerts[b].Severity {
				return res.Alerts[a].Severity == model.CRITICAL.String()
			}
			return res.Alerts[a].OpenedAt > res.Alerts[b].OpenedAt
		})

		if res.Approvals, err = api.db.GetApprovals(project.Id, db.ApprovalsQuery{Status: db.ApprovalStatusPending, Limit: 50}); err != nil {
			return nil, err
		}
		windows, err := api.db.GetActiveMaintenanceWindows(project.Id, now)
		if err != nil {
			return nil, err
		}
		for _, w := range windows {
			res.Maintenance = append(res.Maintenance, renderMaintenanceWindow(w, now))
		}
		activity, err := api.db.GetRecentCommentsByAuthorKind(project.Id, db.CommentAuthorAgent, homeMaxActivity)
		if err != nil {
			return nil, err
		}
		for _, c := range activity {
			res.Activity = append(res.Activity, homeActivity{Comment: c, TargetTitle: c.Meta["rule"]})
		}
	}
	return res, nil
}

// Home handles GET /api/project/{project}/home.
func (api *Api) Home(w http.ResponseWriter, r *http.Request, u *db.User) {
	var world *model.World
	var cacheStatus *cache.Status
	var err error
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return
	}
	if api.cache != nil { // the world is only needed for the response context (rail badges, status)
		if world, project, cacheStatus, err = api.LoadWorldByRequest(r); err != nil || project == nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}
	if !api.IsAllowed(u, rbac.Actions.Project(string(project.Id)).List()...) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	v, err := api.renderHome(u, project)
	if err != nil {
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	utils.WriteJson(w, api.WithContext(project, cacheStatus, world, v))
}

// loadWorldIfAvailable loads the world, or returns nil when there is no metrics cache (tests).
func (api *Api) loadWorldIfAvailable(ctx context.Context, project *db.Project, from, to timeseries.Time) *model.World {
	if api.cache == nil {
		return nil
	}
	world, _, err := api.LoadWorld(ctx, project, from, to)
	if err != nil {
		klog.Warningln(err)
	}
	return world
}

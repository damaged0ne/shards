package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/notifications"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

// shards fork: incident workflow (status, assignee, acknowledgement, severity override, resolution)
// and the generated postmortem draft.

const (
	incidentActionAcknowledge = "acknowledge"
	incidentActionAssign      = "assign"
	incidentActionUnassign    = "unassign"
	incidentActionMitigate    = "mitigate"
	incidentActionResolve     = "resolve"
	incidentActionSetSeverity = "set_severity"

	incidentMaxFollowUps = 50
)

type incidentActionForm struct {
	Action       string   `json:"action"`
	Assignee     string   `json:"assignee,omitempty"`      // assign: empty = the caller
	AssigneeKind string   `json:"assignee_kind,omitempty"` // assign: 'user' | 'agent'
	Severity     string   `json:"severity,omitempty"`      // set_severity: 'warning' | 'critical' | '' (clear the override)
	Resolution   string   `json:"resolution,omitempty"`    // resolve: summary (required)
	RootCause    string   `json:"root_cause,omitempty"`
	FollowUps    []string `json:"follow_ups,omitempty"`
	Comment      string   `json:"comment,omitempty"`
}

func (f *incidentActionForm) validate() error {
	bad := func(msg string) error { return &targetError{http.StatusBadRequest, msg} }
	f.Assignee = strings.TrimSpace(f.Assignee)
	f.Resolution = strings.TrimSpace(f.Resolution)
	f.RootCause = strings.TrimSpace(f.RootCause)
	var followUps []string
	for _, s := range f.FollowUps {
		if s = strings.TrimSpace(s); s != "" {
			followUps = append(followUps, s)
		}
	}
	f.FollowUps = followUps
	switch f.Action {
	case incidentActionAcknowledge, incidentActionAssign, incidentActionUnassign, incidentActionMitigate:
	case incidentActionResolve:
		if f.Resolution == "" {
			return bad("resolution summary is required")
		}
	case incidentActionSetSeverity:
		if f.Severity != "" && f.Severity != "warning" && f.Severity != "critical" {
			return bad("severity must be 'warning', 'critical' or '' (clear the override)")
		}
	default:
		return bad("action must be one of: acknowledge, assign, unassign, mitigate, resolve, set_severity")
	}
	if len(f.Assignee) > 200 || len(f.Resolution) > db.CommentMaxBodyLength || len(f.RootCause) > db.CommentMaxBodyLength ||
		len(f.Comment) > db.CommentMaxBodyLength || len(f.FollowUps) > incidentMaxFollowUps {
		return bad("a field is too long")
	}
	return nil
}

// incidentWorkflowView is the effective workflow state of an incident.
type incidentWorkflowView struct {
	*db.IncidentWorkflow
	Severity         string              `json:"severity"`
	Open             bool                `json:"open"`
	Maintenance      *db.MaintenanceMark `json:"maintenance,omitempty"`
	PendingApprovals []*db.Approval      `json:"pending_approvals,omitempty"`
}

// effectiveIncidentWorkflow merges the stored workflow with the SLO-driven incident state.
func effectiveIncidentWorkflow(i *model.ApplicationIncident, wf *db.IncidentWorkflow) *db.IncidentWorkflow {
	var res db.IncidentWorkflow
	if wf != nil {
		res = *wf
	} else {
		res = db.IncidentWorkflow{ProjectId: "", IncidentKey: i.Key, Status: db.IncidentStatusTriggered}
	}
	if i.Resolved() && res.Status != db.IncidentStatusResolved {
		res.Status = db.IncidentStatusResolved
		res.ResolvedAt = i.ResolvedAt
		res.ResolvedKind = db.IncidentResolvedByAuto
		res.ResolvedBy = "SLO check"
	}
	return &res
}

func effectiveIncidentSeverity(i *model.ApplicationIncident, wf *db.IncidentWorkflow) string {
	if wf != nil && wf.SeverityOverride != "" {
		return wf.SeverityOverride
	}
	if i.Severity == model.OK && len(i.Details.AvailabilityBurnRates)+len(i.Details.LatencyBurnRates) > 0 {
		// resolved incidents get severity OK, report the worst one seen
		s := model.UNKNOWN
		for _, br := range append(i.Details.AvailabilityBurnRates, i.Details.LatencyBurnRates...) {
			if br.Severity > s {
				s = br.Severity
			}
		}
		if s > model.OK {
			return s.String()
		}
	}
	return i.Severity.String()
}

func (api *Api) incidentWorkflowView(projectId db.ProjectId, i *model.ApplicationIncident) (*incidentWorkflowView, error) {
	wf, err := api.db.GetIncidentWorkflow(projectId, i.Key)
	if err != nil {
		return nil, err
	}
	v := &incidentWorkflowView{IncidentWorkflow: effectiveIncidentWorkflow(i, wf), Severity: effectiveIncidentSeverity(i, wf), Open: !i.Resolved()}
	if v.Maintenance, err = api.db.GetMaintenanceMark(projectId, db.CommentTargetIncident, i.Key); err != nil {
		return nil, err
	}
	if v.PendingApprovals, err = api.db.GetApprovals(projectId, db.ApprovalsQuery{Status: db.ApprovalStatusPending, TargetType: db.CommentTargetIncident, TargetId: i.Key}); err != nil {
		return nil, err
	}
	return v, nil
}

// incidentAction applies a workflow action (no approval gating: callers gate 'resolve' for agents).
func (api *Api) incidentAction(project *db.Project, a actor, key string, f incidentActionForm) (*incidentWorkflowView, error) {
	if err := f.validate(); err != nil {
		return nil, err
	}
	i, err := api.db.GetIncidentByKey(project.Id, key)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, &targetError{http.StatusNotFound, commentTargetNotFound}
		}
		return nil, err
	}
	stored, err := api.db.GetIncidentWorkflow(project.Id, key)
	if err != nil {
		return nil, err
	}
	wf := effectiveIncidentWorkflow(i, stored)
	wf.ProjectId = project.Id
	wf.ApplicationId = i.ApplicationId.String()
	now := timeseries.Now()
	resolved := wf.Status == db.IncidentStatusResolved

	ack := func() {
		if wf.AcknowledgedAt == 0 {
			wf.AcknowledgedAt = now
			wf.AcknowledgedBy = a.name
		}
		if wf.Assignee == "" {
			wf.Assignee, wf.AssigneeKind = a.name, a.kind
		}
	}
	action, note := "", strings.TrimSpace(f.Comment)
	meta := map[string]string{}
	switch f.Action {
	case incidentActionAcknowledge:
		if wf.AcknowledgedAt != 0 {
			return nil, &targetError{http.StatusConflict, "the incident has already been acknowledged by " + wf.AcknowledgedBy}
		}
		ack()
		if wf.Status == db.IncidentStatusTriggered {
			wf.Status = db.IncidentStatusAcknowledged
		}
		action = "acknowledged"
	case incidentActionAssign:
		assignee, kind := f.Assignee, db.CommentAuthorKind(f.AssigneeKind)
		if assignee == "" || assignee == a.name {
			assignee, kind = a.name, a.kind
		}
		if kind != db.CommentAuthorAgent {
			kind = db.CommentAuthorUser
		}
		wf.Assignee, wf.AssigneeKind = assignee, kind
		if wf.Status == db.IncidentStatusTriggered {
			wf.Status = db.IncidentStatusAcknowledged
			if wf.AcknowledgedAt == 0 {
				wf.AcknowledgedAt, wf.AcknowledgedBy = now, a.name
			}
		}
		action = "assigned"
		meta["assignee"] = assignee
	case incidentActionUnassign:
		wf.Assignee, wf.AssigneeKind = "", ""
		action = "unassigned"
	case incidentActionMitigate:
		if resolved {
			return nil, &targetError{http.StatusConflict, "the incident is already resolved"}
		}
		ack()
		wf.Status = db.IncidentStatusMitigated
		wf.MitigatedAt, wf.MitigatedBy = now, a.name
		action = "mitigated"
	case incidentActionSetSeverity:
		wf.SeverityOverride = f.Severity
		action = "severity_changed"
		meta["severity"] = f.Severity
		if f.Severity == "" {
			meta["severity"] = "auto"
		}
	case incidentActionResolve:
		ack()
		wf.Resolution = f.Resolution
		if f.RootCause != "" {
			wf.RootCause = f.RootCause
		}
		if f.FollowUps != nil {
			wf.FollowUps = f.FollowUps
		}
		if !resolved {
			wf.Status = db.IncidentStatusResolved
			wf.ResolvedAt, wf.ResolvedBy = now, a.name
			wf.ResolvedKind = db.IncidentResolvedByUser
			if a.kind == db.CommentAuthorAgent {
				wf.ResolvedKind = db.IncidentResolvedByAgent
			}
		}
		action = "resolved"
		if resolved {
			action = "resolution_updated"
		}
		note = f.Resolution
		if f.RootCause != "" {
			note += "\n\n**Root cause:** " + f.RootCause
		}
		if len(f.FollowUps) > 0 {
			note += "\n\n**Follow-ups:**\n- " + strings.Join(f.FollowUps, "\n- ")
		}
		if c := strings.TrimSpace(f.Comment); c != "" {
			note += "\n\n" + c
		}
	}
	if err = api.db.SaveIncidentWorkflow(wf); err != nil {
		return nil, err
	}
	if f.Action == incidentActionResolve && !resolved {
		if err = api.db.SetIncidentResolvedAt(project.Id, key, now); err != nil {
			return nil, err
		}
		i.ResolvedAt = now
		app := &model.Application{Id: i.ApplicationId, Category: project.CalcApplicationCategory(i.ApplicationId)}
		notifications.EnqueueIncidentResolved(api.db, project, app, i)
	}
	api.recordAction(a, project.Id, db.CommentTargetIncident, key, action, note, meta)
	return api.incidentWorkflowView(project.Id, i)
}

// IncidentWorkflow handles GET (effective workflow state) and POST (an incidentActionForm) on
// /api/project/{project}/incident/{incident}/workflow. Permissions are those of the incident's
// timeline: view the application to read, Alerts:Edit to change.
func (api *Api) IncidentWorkflow(w http.ResponseWriter, r *http.Request, u *db.User) {
	vars := mux.Vars(r)
	project := api.getProjectOrError(w, db.ProjectId(vars["project"]))
	if project == nil {
		return
	}
	key := vars["incident"]
	t, err := api.resolveCommentTarget(u, project, string(db.CommentTargetIncident), key, r.Method == http.MethodPost)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		v, err := api.incidentWorkflowView(project.Id, t.incident)
		if err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		utils.WriteJson(w, v)
		return
	}
	var form incidentActionForm
	if err = utils.ReadJson(r, &form); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	a := newActor(u, viaUI)
	if form.Action == incidentActionResolve {
		if err = form.validate(); err != nil {
			writeTargetError(w, err)
			return
		}
		if api.gateREST(w, project, a, db.AgentActionResolveIncident, incidentResolveArgs{Key: key, Form: form},
			"Resolve incident "+key+": "+utils.Truncate(form.Resolution, 200),
			gatedTarget{typ: db.CommentTargetIncident, id: key, title: "Incident " + key}, form.Comment) {
			return
		}
	}
	v, err := api.incidentAction(project, a, key, form)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	utils.WriteJson(w, v)
}

type incidentListWorkflow struct {
	Status        db.IncidentStatus `json:"status"`
	Assignee      string            `json:"assignee,omitempty"`
	Severity      string            `json:"severity,omitempty"`
	InMaintenance bool              `json:"in_maintenance,omitempty"`
}

// IncidentsWorkflow handles GET /api/project/{project}/incidents/workflow: the workflow state of
// the latest incidents by key (for the incidents list).
func (api *Api) IncidentsWorkflow(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return
	}
	incidents, err := api.db.GetLatestIncidentsBrief(project.Id, 500)
	if err != nil {
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	wfs, err := api.db.GetIncidentWorkflows(project.Id, nil)
	if err != nil {
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	marks, err := api.db.GetMaintenanceMarks(project.Id, db.CommentTargetIncident, false)
	if err != nil {
		klog.Errorln(err)
	}
	res := map[string]incidentListWorkflow{}
	for _, i := range incidents {
		wf := wfs[i.Key]
		e := effectiveIncidentWorkflow(i, wf)
		res[i.Key] = incidentListWorkflow{Status: e.Status, Assignee: e.Assignee, Severity: effectiveIncidentSeverity(i, wf), InMaintenance: marks[i.Key] != nil}
	}
	utils.WriteJson(w, res)
}

// IncidentPostmortem handles GET /api/project/{project}/incident/{incident}/postmortem and returns
// a markdown draft: {"markdown": "..."}.
func (api *Api) IncidentPostmortem(w http.ResponseWriter, r *http.Request, u *db.User) {
	vars := mux.Vars(r)
	project := api.getProjectOrError(w, db.ProjectId(vars["project"]))
	if project == nil {
		return
	}
	t, err := api.resolveCommentTarget(u, project, string(db.CommentTargetIncident), vars["incident"], false)
	if err != nil {
		writeTargetError(w, err)
		return
	}
	md, err := api.incidentPostmortem(project, t.incident)
	if err != nil {
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("format") == "markdown" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(md))
		return
	}
	utils.WriteJson(w, map[string]string{"markdown": md})
}

func fmtTime(t timeseries.Time) string {
	if t == 0 {
		return "—"
	}
	return t.ToStandard().Format("2006-01-02 15:04:05 UTC")
}

func fmtDuration(d timeseries.Duration) string {
	if d <= 0 {
		return "0s"
	}
	return utils.FormatDurationShort(d, 2)
}

func mdEscapeCell(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

func timelineActionText(c *db.Comment) string {
	action := c.Meta["action"]
	switch action {
	case "":
		return "commented"
	case "assigned":
		return "assigned the incident to " + c.Meta["assignee"]
	case "severity_changed":
		return "set the severity to " + c.Meta["severity"]
	case "auto_resolved":
		return "resolved automatically (SLO is met again)"
	case "muted":
		return "muted notifications (maintenance)"
	case "unmuted":
		return "sent postponed notifications (maintenance is over)"
	case actionApprovalRequested:
		return "requested approval to " + strings.ReplaceAll(c.Meta["requested_action"], "_", " ")
	case actionApprovalApproved:
		return "approved " + c.Meta["requested_by"] + "'s request to " + strings.ReplaceAll(c.Meta["requested_action"], "_", " ")
	case actionApprovalRejected:
		return "rejected " + c.Meta["requested_by"] + "'s request to " + strings.ReplaceAll(c.Meta["requested_action"], "_", " ")
	case actionApprovalDenied:
		return "was denied (policy) to " + strings.ReplaceAll(c.Meta["requested_action"], "_", " ")
	}
	return strings.ReplaceAll(action, "_", " ") + " the incident"
}

func (api *Api) incidentPostmortem(project *db.Project, i *model.ApplicationIncident) (string, error) {
	wf, err := api.db.GetIncidentWorkflow(project.Id, i.Key)
	if err != nil {
		return "", err
	}
	e := effectiveIncidentWorkflow(i, wf)
	comments, err := api.db.GetComments(project.Id, db.CommentTargetIncident, i.Key)
	if err != nil {
		return "", err
	}
	deployments, err := api.db.GetApplicationDeployments(project.Id)
	if err != nil {
		return "", err
	}
	now := timeseries.Now()
	end := now
	if i.Resolved() {
		end = i.ResolvedAt
	}

	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	app := i.ApplicationId
	p("# Postmortem: %s — incident i-%s\n\n", app.Name, i.Key)
	p("_Draft generated by shards on %s. Review and edit before sharing._\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"))

	p("## Summary\n\n")
	p("- **Application:** `%s`\n", app.StringWithoutClusterId())
	p("- **Severity:** %s", effectiveIncidentSeverity(i, wf))
	if wf != nil && wf.SeverityOverride != "" {
		p(" (set manually)")
	}
	p("\n- **Status:** %s", e.Status)
	if e.Status == db.IncidentStatusResolved {
		switch e.ResolvedKind {
		case db.IncidentResolvedByAuto:
			p(" (automatically, the SLO is met again)")
		default:
			p(" by %s", e.ResolvedBy)
		}
	}
	p("\n- **Opened:** %s\n", fmtTime(i.OpenedAt))
	p("- **Resolved:** %s\n", fmtTime(i.ResolvedAt))
	p("- **Duration:** %s%s\n", fmtDuration(end.Sub(i.OpenedAt)), map[bool]string{true: "", false: " (still open)"}[i.Resolved()])
	if e.Assignee != "" {
		p("- **Assignee:** %s\n", e.Assignee)
	}
	if e.AcknowledgedAt != 0 {
		p("- **Acknowledged:** by %s at %s (time to acknowledge: %s)\n", e.AcknowledgedBy, fmtTime(e.AcknowledgedAt), fmtDuration(e.AcknowledgedAt.Sub(i.OpenedAt)))
	}
	if e.MitigatedAt != 0 {
		p("- **Mitigated:** by %s at %s (time to mitigate: %s)\n", e.MitigatedBy, fmtTime(e.MitigatedAt), fmtDuration(e.MitigatedAt.Sub(i.OpenedAt)))
	}
	if mark, _ := api.db.GetMaintenanceMark(project.Id, db.CommentTargetIncident, i.Key); mark != nil {
		p("- **Maintenance:** notifications were muted by the window \"%s\"\n", mark.WindowName)
	}

	p("\n## Impact\n\n")
	impact := false
	if v := i.Details.AvailabilityImpact.AffectedRequestPercentage; v > 0 {
		p("- **Availability:** %s of requests failed during the incident.\n", utils.FormatPercentage(v))
		impact = true
	}
	if v := i.Details.LatencyImpact.AffectedRequestPercentage; v > 0 {
		p("- **Latency:** %s of requests were slower than the SLO objective.\n", utils.FormatPercentage(v))
		impact = true
	}
	burn := func(name string, brs []model.BurnRate) {
		for _, br := range brs {
			if br.Severity <= model.OK {
				continue
			}
			p("- %s error budget burn rate: %.0fx over %s, %.0fx over %s (threshold %.0fx, %s).\n", name,
				br.LongWindowBurnRate, fmtDuration(br.LongWindow), br.ShortWindowBurnRate, fmtDuration(br.ShortWindow), br.Threshold, br.Severity.String())
			impact = true
		}
	}
	burn("Availability", i.Details.AvailabilityBurnRates)
	burn("Latency", i.Details.LatencyBurnRates)
	if !impact {
		p("_No SLO burn data recorded._\n")
	}

	p("\n## Resolution\n\n")
	if e.Resolution != "" {
		p("%s\n", e.Resolution)
	} else {
		p("_TBD_\n")
	}
	p("\n## Root cause\n\n")
	switch {
	case e.RootCause != "":
		p("%s\n", e.RootCause)
	case i.RCA != nil && i.RCA.RootCause != "":
		p("%s\n\n_(from the automated root cause analysis)_\n", i.RCA.RootCause)
	case i.RCA != nil && i.RCA.ShortSummary != "":
		p("%s\n\n_(from the automated root cause analysis)_\n", i.RCA.ShortSummary)
	default:
		p("_TBD_\n")
	}

	p("\n## Deployments around the incident\n\n")
	from := i.OpenedAt.Add(-timeseries.Hour)
	var ds []*model.ApplicationDeployment
	for _, list := range deployments {
		for _, d := range list {
			if d.StartedAt >= from && d.StartedAt <= end {
				ds = append(ds, d)
			}
		}
	}
	sort.Slice(ds, func(a, b int) bool { return ds[a].StartedAt < ds[b].StartedAt })
	if len(ds) > 20 {
		ds = ds[len(ds)-20:]
	}
	if len(ds) == 0 {
		p("_No deployments from 1h before the incident until it was resolved._\n")
	}
	for _, d := range ds {
		mark := ""
		if d.ApplicationId == i.ApplicationId || d.ApplicationId.StringWithoutClusterId() == i.ApplicationId.StringWithoutClusterId() {
			mark = " **(this application)**"
		}
		p("- %s — `%s` %s%s\n", fmtTime(d.StartedAt), d.ApplicationId.StringWithoutClusterId(), d.Name, mark)
	}

	p("\n## Timeline\n\n| Time (UTC) | Who | What |\n|---|---|---|\n")
	p("| %s | shards | Incident opened (SLO violation) |\n", fmtTime(i.OpenedAt))
	for _, c := range comments {
		who := c.Author
		if c.AuthorKind == db.CommentAuthorAgent {
			who += " (agent)"
		}
		if by := c.Meta["approved_by"]; by != "" {
			who += ", approved by " + by
		}
		what := timelineActionText(c)
		if c.Body != "" {
			what += ": " + utils.Truncate(c.Body, 300)
		}
		p("| %s | %s | %s |\n", fmtTime(c.CreatedAt), mdEscapeCell(who), mdEscapeCell(what))
	}

	p("\n## Follow-up items\n\n")
	if len(e.FollowUps) == 0 {
		p("- [ ] _TBD_\n")
	}
	for _, f := range e.FollowUps {
		p("- [ ] %s\n", f)
	}
	return b.String(), nil
}

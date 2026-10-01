package watchers

import (
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"k8s.io/klog"
)

// shards fork: the incident workflow layer (see db/incident_workflow_shards.go).

// HumanResolveCooldown: after a person or an agent resolved an incident, the SLO check doesn't open
// a new incident for the same application for this long. Burn rates are computed over windows of up
// to an hour, so they stay above the threshold for a while after a fix; without a cooldown a manual
// resolve would immediately re-open the incident.
const HumanResolveCooldown = 30 * timeseries.Minute

func (w *Incidents) inHumanResolveCooldown(project *db.Project, appId model.ApplicationId, now timeseries.Time) bool {
	ok, err := w.db.IncidentResolvedByHumanSince(project.Id, appId, now.Add(-HumanResolveCooldown))
	if err != nil {
		klog.Errorln(err)
		return false
	}
	return ok
}

// onIncidentAutoResolved records that the SLO check resolved the incident. Fields set by people
// (assignee, acknowledgement, resolution summary, ...) are kept as they are.
func (w *Incidents) onIncidentAutoResolved(project *db.Project, incident *model.ApplicationIncident) {
	wf, err := w.db.GetIncidentWorkflow(project.Id, incident.Key)
	if err != nil {
		klog.Errorln(err)
		return
	}
	if wf == nil {
		wf = &db.IncidentWorkflow{ProjectId: project.Id, IncidentKey: incident.Key, ApplicationId: incident.ApplicationId.String()}
	}
	if wf.Status == db.IncidentStatusResolved {
		return
	}
	wf.Status = db.IncidentStatusResolved
	wf.ResolvedAt = incident.ResolvedAt
	wf.ResolvedBy = "SLO check"
	wf.ResolvedKind = db.IncidentResolvedByAuto
	if err = w.db.SaveIncidentWorkflow(wf); err != nil {
		klog.Errorln(err)
		return
	}
	c := &db.Comment{
		ProjectId:  project.Id,
		TargetType: db.CommentTargetIncident,
		TargetId:   incident.Key,
		Author:     "shards",
		AuthorKind: db.CommentAuthorSystem,
		Kind:       db.CommentKindAction,
		Meta:       map[string]string{"action": "auto_resolved"},
	}
	if err = w.db.AddComment(c); err != nil {
		klog.Errorln(err)
	}
}

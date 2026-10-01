package incident

import (
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
)

// RenderWithoutApplication renders an incident whose application is not in the world (anymore):
// the incident itself, its workflow and timeline stay accessible, without SLO charts. shards fork.
func RenderWithoutApplication(w *model.World, incident *model.ApplicationIncident) *View {
	to := timeseries.Now()
	if incident.Resolved() {
		to = incident.ResolvedAt
	}
	return &View{Incident: renderIncident(w, incident), ActualFrom: incident.OpenedAt, ActualTo: to}
}

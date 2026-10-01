package notifications

// shards fork: incident and alert events are also offered to operator agents (outbound dispatch
// webhooks). The api package installs the hook; it filters by each agent's dispatch config and
// enqueues the deliveries asynchronously, so the hook must not block.

import (
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
)

type AgentEventsHook interface {
	IncidentEvent(project *db.Project, app *model.Application, incident *model.ApplicationIncident, escalated bool)
	AlertEvent(project *db.Project, app *model.Application, alert *model.Alert, rule *model.AlertingRule)
}

// AgentEvents is set by the api package at startup (nil in tests and tools).
var AgentEvents AgentEventsHook

func notifyAgentsIncident(project *db.Project, app *model.Application, incident *model.ApplicationIncident) {
	if AgentEvents != nil && app != nil && incident != nil && !incident.Resolved() {
		AgentEvents.IncidentEvent(project, app, incident, false)
	}
}

// NotifyAgentsIncidentEscalated is called by the incidents watcher when an open incident's severity grows.
func NotifyAgentsIncidentEscalated(project *db.Project, app *model.Application, incident *model.ApplicationIncident) {
	if AgentEvents != nil && app != nil && incident != nil && !incident.Resolved() {
		AgentEvents.IncidentEvent(project, app, incident, true)
	}
}

func notifyAgentsAlert(project *db.Project, app *model.Application, alert *model.Alert, rule *model.AlertingRule) {
	if AgentEvents != nil && alert != nil && alert.IsFiring() && alert.ManuallyResolvedAt == 0 && !alert.Suppressed {
		AgentEvents.AlertEvent(project, app, alert, rule)
	}
}

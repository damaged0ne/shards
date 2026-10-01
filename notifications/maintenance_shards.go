package notifications

import (
	"fmt"
	"sort"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"k8s.io/klog"
)

// shards fork: maintenance windows mute notifications.
//
// While a window matching an alert/incident is active, the alert/incident is still created, but
// its "opened" notification is not enqueued; it is marked "in maintenance" instead. When the
// window ends (or no longer matches) and the alert/incident is still firing, the notification is
// sent late (ReleaseMaintenance, called by the watchers after each evaluation). Alerts/incidents
// that resolved while muted are never announced: neither the open nor the resolve notification
// is sent, so ending a window does not re-notify for things that are already over.

const maintenanceAuthor = "maintenance"

// MaintenanceAlertTarget describes an alert for maintenance scope matching.
func MaintenanceAlertTarget(app *model.Application, alert *model.Alert) db.MaintenanceTarget {
	t := db.MaintenanceTarget{ApplicationId: alert.ApplicationId, Category: alert.ApplicationCategory, RuleId: alert.RuleId}
	if app != nil {
		t.Category = app.Category
		t.Nodes = appNodes(app)
	}
	for _, d := range alert.Details {
		if d.Name != "Labels" {
			continue
		}
		for _, line := range strings.Split(d.Value, "\n") {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch k {
			case "node", "node_name", "nodename", "instance", "host", "hostname", "kubernetes_node":
				v = strings.Trim(v, `"`)
				if host, _, ok := strings.Cut(v, ":"); ok && k == "instance" {
					v = host
				}
				t.Nodes = append(t.Nodes, v)
			}
		}
	}
	return t
}

// MaintenanceIncidentTarget describes an incident for maintenance scope matching.
func MaintenanceIncidentTarget(project *db.Project, app *model.Application, incident *model.ApplicationIncident) db.MaintenanceTarget {
	t := db.MaintenanceTarget{ApplicationId: incident.ApplicationId}
	if app != nil {
		t.Category = app.Category
		t.Nodes = appNodes(app)
	} else if project != nil {
		t.Category = project.CalcApplicationCategory(incident.ApplicationId)
	}
	return t
}

func appNodes(app *model.Application) []string {
	seen := map[string]bool{}
	var res []string
	for _, i := range app.Instances {
		if i.Node == nil {
			continue
		}
		if name := i.Node.GetName(); name != "" && !seen[name] {
			seen[name] = true
			res = append(res, name)
		}
	}
	sort.Strings(res)
	return res
}

// MatchingMaintenanceWindow returns the first of the windows matching the target.
func MatchingMaintenanceWindow(windows []*db.MaintenanceWindow, t db.MaintenanceTarget) *db.MaintenanceWindow {
	for _, w := range windows {
		if w.Scope.Matches(t) {
			return w
		}
	}
	return nil
}

func recordMaintenanceAction(database *db.DB, projectId db.ProjectId, targetType db.CommentTargetType, targetId, action, body string, windowId int) {
	c := &db.Comment{
		ProjectId:  projectId,
		TargetType: targetType,
		TargetId:   targetId,
		Author:     maintenanceAuthor,
		AuthorKind: db.CommentAuthorSystem,
		Kind:       db.CommentKindAction,
		Body:       body,
		Meta:       map[string]string{"action": action, "maintenance_window": fmt.Sprint(windowId)},
	}
	if err := database.AddComment(c); err != nil {
		klog.Errorln("maintenance: failed to record a timeline entry:", err)
	}
}

// muteByMaintenance decides whether a notification about the target must be skipped.
// resolved=true means a resolve notification.
func muteByMaintenance(database *db.DB, project *db.Project, targetType db.CommentTargetType, targetId string, resolved bool, target func() db.MaintenanceTarget, now timeseries.Time) bool {
	if resolved {
		m, err := database.GetMaintenanceMark(project.Id, targetType, targetId)
		if err != nil {
			klog.Errorln("maintenance:", err)
			return false
		}
		if m == nil || m.Notified == db.MaintenanceMarkNotified {
			return false
		}
		if m.Notified == db.MaintenanceMarkPending {
			if err = database.SetMaintenanceMarkNotified(project.Id, targetType, targetId, db.MaintenanceMarkSkipped); err != nil {
				klog.Errorln("maintenance:", err)
			}
		}
		return true
	}
	windows, err := database.GetActiveMaintenanceWindows(project.Id, now)
	if err != nil {
		klog.Errorln("maintenance:", err)
		return false
	}
	if len(windows) == 0 {
		return false
	}
	w := MatchingMaintenanceWindow(windows, target())
	if w == nil {
		return false
	}
	added, err := database.AddMaintenanceMark(project.Id, &db.MaintenanceMark{TargetType: targetType, TargetId: targetId, WindowId: w.Id, WindowName: w.Name, MarkedAt: now})
	if err != nil {
		klog.Errorln("maintenance:", err)
	}
	if added {
		recordMaintenanceAction(database, project.Id, targetType, targetId, "muted",
			fmt.Sprintf("Notifications muted by the maintenance window **%s**.", w.Name), w.Id)
	}
	return true
}

func muteAlertNotification(database *db.DB, project *db.Project, app *model.Application, alert *model.Alert, now timeseries.Time) bool {
	resolved := alert.ResolvedAt > 0 || alert.ManuallyResolvedAt > 0
	return muteByMaintenance(database, project, db.CommentTargetAlert, alert.Id, resolved, func() db.MaintenanceTarget {
		return MaintenanceAlertTarget(app, alert)
	}, now)
}

func muteIncidentNotification(database *db.DB, project *db.Project, app *model.Application, incident *model.ApplicationIncident, now timeseries.Time) bool {
	return muteByMaintenance(database, project, db.CommentTargetIncident, incident.Key, incident.Resolved(), func() db.MaintenanceTarget {
		return MaintenanceIncidentTarget(project, app, incident)
	}, now)
}

// ReleaseMaintenance sends the postponed notifications of alerts that are still firing after
// their maintenance window has ended.
func (n *AlertNotifier) ReleaseMaintenance(project *db.Project, world *model.World, now timeseries.Time) {
	marks, err := n.db.GetMaintenanceMarks(project.Id, db.CommentTargetAlert, true)
	if err != nil || len(marks) == 0 {
		if err != nil {
			klog.Errorln("maintenance:", err)
		}
		return
	}
	windows, err := n.db.GetActiveMaintenanceWindows(project.Id, now)
	if err != nil {
		klog.Errorln("maintenance:", err)
		return
	}
	rules := map[string]*model.AlertingRule{}
	for id := range marks {
		alert, err := n.db.GetAlert(project.Id, id)
		if err != nil || alert == nil || alert.ResolvedAt > 0 || alert.ManuallyResolvedAt > 0 || alert.Suppressed {
			_ = n.db.SetMaintenanceMarkNotified(project.Id, db.CommentTargetAlert, id, db.MaintenanceMarkSkipped)
			continue
		}
		var app *model.Application
		if world != nil {
			app = world.GetApplication(alert.ApplicationId)
		}
		if MatchingMaintenanceWindow(windows, MaintenanceAlertTarget(app, alert)) != nil {
			continue
		}
		rule, ok := rules[alert.RuleId]
		if !ok {
			rule, _ = n.db.GetAlertingRule(project.Id, model.AlertingRuleId(alert.RuleId))
			rules[alert.RuleId] = rule
		}
		if rule == nil {
			_ = n.db.SetMaintenanceMarkNotified(project.Id, db.CommentTargetAlert, id, db.MaintenanceMarkSkipped)
			continue
		}
		if err = n.db.SetMaintenanceMarkNotified(project.Id, db.CommentTargetAlert, id, db.MaintenanceMarkNotified); err != nil {
			klog.Errorln("maintenance:", err)
			continue
		}
		recordMaintenanceAction(n.db, project.Id, db.CommentTargetAlert, id, "unmuted", "The maintenance window is over and the alert is still firing: notifications sent.", marks[id].WindowId)
		n.Enqueue(project, app, alert, rule, now)
	}
}

// ReleaseMaintenance sends the postponed notifications of incidents that are still open after
// their maintenance window has ended.
func (n *IncidentNotifier) ReleaseMaintenance(project *db.Project, world *model.World, now timeseries.Time) {
	marks, err := n.db.GetMaintenanceMarks(project.Id, db.CommentTargetIncident, true)
	if err != nil || len(marks) == 0 {
		if err != nil {
			klog.Errorln("maintenance:", err)
		}
		return
	}
	windows, err := n.db.GetActiveMaintenanceWindows(project.Id, now)
	if err != nil {
		klog.Errorln("maintenance:", err)
		return
	}
	for key := range marks {
		incident, err := n.db.GetIncidentByKey(project.Id, key)
		if err != nil || incident.Resolved() {
			_ = n.db.SetMaintenanceMarkNotified(project.Id, db.CommentTargetIncident, key, db.MaintenanceMarkSkipped)
			continue
		}
		if world == nil {
			continue
		}
		app := world.GetApplication(incident.ApplicationId)
		if app == nil {
			continue
		}
		if MatchingMaintenanceWindow(windows, MaintenanceIncidentTarget(project, app, incident)) != nil {
			continue
		}
		if err = n.db.SetMaintenanceMarkNotified(project.Id, db.CommentTargetIncident, key, db.MaintenanceMarkNotified); err != nil {
			klog.Errorln("maintenance:", err)
			continue
		}
		recordMaintenanceAction(n.db, project.Id, db.CommentTargetIncident, key, "unmuted", "The maintenance window is over and the incident is still open: notifications sent.", marks[key].WindowId)
		n.Enqueue(project, app, incident, now)
	}
}

// EnqueueIncidentResolved enqueues the "resolved" notifications of an incident closed by a person
// or an agent (the incidents watcher only notifies about what it resolves itself).
func EnqueueIncidentResolved(database *db.DB, project *db.Project, app *model.Application, incident *model.ApplicationIncident) {
	if app == nil || !incident.Resolved() {
		return
	}
	now := timeseries.Now()
	if muteIncidentNotification(database, project, app, incident, now) {
		return
	}
	categorySettings := project.GetApplicationCategories()[app.Category]
	if categorySettings == nil {
		return
	}
	s := categorySettings.NotificationSettings.Incidents
	if !s.Enabled {
		return
	}
	n := &IncidentNotifier{db: database}
	if s.Slack != nil && s.Slack.Enabled {
		n.enqueue(now, project, app, incident, db.IncidentNotificationDestination{IntegrationType: db.IntegrationTypeSlack, SlackChannel: s.Slack.Channel})
	}
	if s.Teams != nil && s.Teams.Enabled {
		n.enqueue(now, project, app, incident, db.IncidentNotificationDestination{IntegrationType: db.IntegrationTypeTeams, TeamsChannel: s.Teams.Channel})
	}
	if s.Pagerduty != nil && s.Pagerduty.Enabled {
		n.enqueue(now, project, app, incident, db.IncidentNotificationDestination{IntegrationType: db.IntegrationTypePagerduty})
	}
	if s.Opsgenie != nil && s.Opsgenie.Enabled {
		n.enqueue(now, project, app, incident, db.IncidentNotificationDestination{IntegrationType: db.IntegrationTypeOpsgenie})
	}
	if s.Webhook != nil && s.Webhook.Enabled {
		n.enqueue(now, project, app, incident, db.IncidentNotificationDestination{IntegrationType: db.IntegrationTypeWebhook})
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/coroot/coroot/api/forms"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/notifications"
	"github.com/coroot/coroot/utils"
	"k8s.io/klog"
)

// Shared create/update/delete logic for alerting rules, used by both the REST API (UI) and
// the MCP tools (agents). Every change is validated with forms.ValidateAlertingRule and
// recorded as an action entry in the rule's timeline. Errors are *targetError when they
// map to a client-facing HTTP status.

func (api *Api) createAlertingRule(projectId db.ProjectId, rule *model.AlertingRule, a actor) error {
	rule.Id = model.AlertingRuleId(utils.NanoId(8))
	rule.ProjectId = string(projectId)
	rule.Builtin = false
	rule.Readonly = false
	if err := forms.ValidateAlertingRule(rule); err != nil {
		return &targetError{http.StatusBadRequest, err.Error()}
	}
	if err := api.db.CreateAlertingRule(projectId, rule); err != nil {
		return err
	}
	api.recordAction(a, projectId, db.CommentTargetAlertingRule, string(rule.Id), actionRuleCreated, "", map[string]string{"rule": rule.Name})
	return nil
}

func (api *Api) updateAlertingRule(projectId db.ProjectId, existing, rule *model.AlertingRule, a actor) error {
	if existing.Readonly {
		return &targetError{http.StatusForbidden, "This rule is managed via config and cannot be edited"}
	}
	rule.Id = existing.Id
	rule.ProjectId = string(projectId)
	rule.Builtin = existing.Builtin
	rule.Readonly = false
	rule.CreatedAt = existing.CreatedAt
	if err := forms.ValidateAlertingRule(rule); err != nil {
		return &targetError{http.StatusBadRequest, err.Error()}
	}
	if err := api.db.UpdateAlertingRule(projectId, rule); err != nil {
		return err
	}
	changes := alertingRuleChanges(existing, rule)
	action := actionRuleUpdated
	if len(changes) == 1 && changes[0] == "enabled" {
		action = actionRuleDisabled
		if rule.Enabled {
			action = actionRuleEnabled
		}
	}
	meta := map[string]string{"rule": rule.Name}
	if len(changes) > 0 {
		meta["changed"] = strings.Join(changes, ",")
	}
	api.recordAction(a, projectId, db.CommentTargetAlertingRule, string(rule.Id), action, "", meta)
	return nil
}

func (api *Api) deleteAlertingRule(projectId db.ProjectId, rule *model.AlertingRule, a actor) error {
	if rule.Readonly {
		return &targetError{http.StatusForbidden, "This rule is managed via config and cannot be deleted"}
	}
	if rule.Builtin {
		return &targetError{http.StatusForbidden, "Builtin rules cannot be deleted, disable them instead"}
	}
	if resolvedAlerts, err := api.db.ResolveAlertsByRule(projectId, string(rule.Id)); err != nil {
		klog.Errorln(err)
	} else if len(resolvedAlerts) > 0 {
		if project, err := api.db.GetProject(projectId); err != nil {
			klog.Errorln(err)
		} else {
			notifications.EnqueueResolvedAlerts(api.db, project, resolvedAlerts, rule)
		}
	}
	if err := api.db.DeleteAlertingRule(projectId, rule.Id); err != nil {
		return err
	}
	api.recordAction(a, projectId, db.CommentTargetAlertingRule, string(rule.Id), actionRuleDeleted, "", map[string]string{"rule": rule.Name})
	return nil
}

// alertingRuleChanges lists the top-level fields that differ between two versions of a rule.
func alertingRuleChanges(old, upd *model.AlertingRule) []string {
	var res []string
	add := func(changed bool, name string) {
		if changed {
			res = append(res, name)
		}
	}
	add(old.Name != upd.Name, "name")
	add(!jsonEqual(old.Source, upd.Source), "source")
	add(!jsonEqual(old.Selector, upd.Selector), "selector")
	add(old.Severity != upd.Severity, "severity")
	add(old.For != upd.For, "for")
	add(old.KeepFiringFor != upd.KeepFiringFor, "keep_firing_for")
	add(old.Templates != upd.Templates, "templates")
	add(old.NotificationCategory != upd.NotificationCategory, "notification_category")
	add(old.Enabled != upd.Enabled, "enabled")
	sort.Strings(res)
	return res
}

func jsonEqual(a, b any) bool {
	da, errA := json.Marshal(a)
	dbb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(da) == string(dbb)
}

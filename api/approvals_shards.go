package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

// shards fork: agent action approvals.
//
// Some actions of operator agents (author_kind=agent: API keys and MCP clients) are gated by a
// per-project policy (db.AgentApprovalPolicy): 'auto' executes immediately, 'deny' refuses, and
// 'approval' stores the action with its arguments as a pending db.Approval and returns
// "pending approval <id>" instead of executing it. When a human approves, the stored action is
// executed through the same code path, as the agent, with "approved_by" recorded in the timeline.
// Humans are never gated.

// Timeline action names of the approval flow.
const (
	actionApprovalRequested = "approval_requested"
	actionApprovalApproved  = "approval_approved"
	actionApprovalRejected  = "approval_rejected"
	actionApprovalDenied    = "approval_denied"
)

type gatedTarget struct {
	typ   db.CommentTargetType
	id    string
	title string
}

// Arguments of the gated actions (stored as JSON in db.Approval.Args).
type alertsActionArgs struct {
	Ids     []string `json:"ids"`
	Comment string   `json:"comment,omitempty"`
}

type ruleDeleteArgs struct {
	RuleId  string `json:"rule_id"`
	Comment string `json:"comment,omitempty"`
}

type ruleUpdateArgs struct {
	RuleId  string              `json:"rule_id"`
	Rule    *model.AlertingRule `json:"rule"`
	Comment string              `json:"comment,omitempty"`
}

type maintenanceCreateArgs struct {
	Window *db.MaintenanceWindow `json:"window"`
}

type maintenanceEndArgs struct {
	Id      int    `json:"id"`
	Comment string `json:"comment,omitempty"`
}

type incidentResolveArgs struct {
	Key  string             `json:"key"`
	Form incidentActionForm `json:"form"`
}

// pendingApproval is returned (HTTP 202 / MCP result) when an action waits for a human.
type pendingApproval struct {
	Status     string `json:"status"`
	ApprovalId int    `json:"approval_id"`
	Action     string `json:"action"`
	Message    string `json:"message"`
}

func newPendingApproval(a *db.Approval) pendingApproval {
	return pendingApproval{
		Status:     db.ApprovalStatusPending,
		ApprovalId: a.Id,
		Action:     a.Action,
		Message: fmt.Sprintf("pending approval %d: the project requires a human to approve '%s'. Nothing was changed yet; check get_approval_status(%d) later.",
			a.Id, a.Action, a.Id),
	}
}

func errAgentActionDenied(action string) error {
	return &targetError{http.StatusForbidden, fmt.Sprintf("denied: the project policy doesn't allow agents to %s", strings.ReplaceAll(action, "_", " "))}
}

// gateAgentAction applies the project's approval policy to an agent action. It returns a pending
// approval if the action must wait for a human, an error if it's denied, and (nil, nil) if it
// may be executed now.
func (api *Api) gateAgentAction(project *db.Project, a actor, action string, args any, summary string, target gatedTarget, reason string) (*db.Approval, error) {
	if a.kind != db.CommentAuthorAgent || a.meta["approved_by"] != "" {
		return nil, nil
	}
	switch project.Settings.AgentApprovals.Effective(action) {
	case db.ApprovalPolicyAuto:
		return nil, nil
	case db.ApprovalPolicyDeny:
		if target.typ != "" {
			api.recordAction(a, project.Id, target.typ, target.id, actionApprovalDenied, "", map[string]string{"requested_action": action})
		}
		return nil, errAgentActionDenied(action)
	}
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	ap := &db.Approval{
		ProjectId:       project.Id,
		Action:          action,
		Args:            data,
		Summary:         summary,
		TargetType:      target.typ,
		TargetId:        target.id,
		TargetTitle:     target.title,
		RequestedBy:     a.name,
		RequestedById:   a.id,
		RequestedByKind: a.kind,
		RequestedMeta:   a.meta,
		Reason:          strings.TrimSpace(reason),
	}
	if err = api.db.CreateApproval(ap); err != nil {
		return nil, err
	}
	if target.typ != "" {
		api.recordAction(a, project.Id, target.typ, target.id, actionApprovalRequested, ap.Reason,
			map[string]string{"approval_id": strconv.Itoa(ap.Id), "requested_action": action, "summary": summary})
	}
	return ap, nil
}

// runAgentAction gates (for agents) and executes a gated action. Exactly one of the results is set:
// the action's result, or a pending approval.
func (api *Api) runAgentAction(project *db.Project, a actor, action string, args any, summary string, target gatedTarget, reason string) (any, *db.Approval, error) {
	ap, err := api.gateAgentAction(project, a, action, args, summary, target, reason)
	if err != nil || ap != nil {
		return nil, ap, err
	}
	data, err := json.Marshal(args)
	if err != nil {
		return nil, nil, err
	}
	res, err := api.doAgentAction(project, a, action, data)
	return res, nil, err
}

func resolvedByName(a actor) string {
	name := a.name
	if a.meta["via"] == viaMCP {
		name += " (via MCP)"
	}
	if by := a.meta["approved_by"]; by != "" {
		name += ", approved by " + by
	}
	return name
}

// doAgentAction executes a gated action without checking the policy.
func (api *Api) doAgentAction(project *db.Project, a actor, action string, raw json.RawMessage) (any, error) {
	bad := func(err error) error { return &targetError{http.StatusBadRequest, "invalid arguments: " + err.Error()} }
	switch action {
	case db.AgentActionResolveAlerts, db.AgentActionSuppressAlerts:
		var args alertsActionArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, bad(err)
		}
		if action == db.AgentActionResolveAlerts {
			notified, err := api.resolveAlerts(project, args.Ids, resolvedByName(a))
			if err != nil {
				return nil, err
			}
			api.recordAlertActions(a, project.Id, args.Ids, actionAlertResolved, args.Comment)
			return map[string]any{"resolved": len(args.Ids), "notified": notified}, nil
		}
		notified, err := api.suppressAlerts(project, args.Ids, resolvedByName(a))
		if err != nil {
			return nil, err
		}
		api.recordAlertActions(a, project.Id, args.Ids, actionAlertSuppressed, args.Comment)
		return map[string]any{"suppressed": len(args.Ids), "notified": notified}, nil

	case db.AgentActionDeleteAlertingRule:
		var args ruleDeleteArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, bad(err)
		}
		rule, err := api.db.GetAlertingRule(project.Id, model.AlertingRuleId(args.RuleId))
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, &targetError{http.StatusNotFound, "alerting rule not found"}
			}
			return nil, err
		}
		if err = api.deleteAlertingRule(project.Id, rule, a); err != nil {
			return nil, err
		}
		api.recordNote(a, project.Id, db.CommentTargetAlertingRule, string(rule.Id), args.Comment)
		return map[string]any{"deleted": string(rule.Id), "name": rule.Name}, nil

	case db.AgentActionUpdateAlertingRule, db.AgentActionDisableAlertingRule:
		var args ruleUpdateArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, bad(err)
		}
		if args.Rule == nil {
			return nil, bad(errors.New("rule is required"))
		}
		existing, err := api.db.GetAlertingRule(project.Id, model.AlertingRuleId(args.RuleId))
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, &targetError{http.StatusNotFound, "alerting rule not found"}
			}
			return nil, err
		}
		if err = api.updateAlertingRule(project.Id, existing, args.Rule, a); err != nil {
			return nil, err
		}
		api.recordNote(a, project.Id, db.CommentTargetAlertingRule, string(existing.Id), args.Comment)
		return args.Rule, nil

	case db.AgentActionCreateMaintenanceWindow:
		var args maintenanceCreateArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, bad(err)
		}
		if args.Window == nil {
			return nil, bad(errors.New("window is required"))
		}
		return api.createMaintenanceWindow(project, a, args.Window)

	case db.AgentActionEndMaintenanceWindow:
		var args maintenanceEndArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, bad(err)
		}
		return api.endMaintenanceWindow(project, a, args.Id, args.Comment)

	case db.AgentActionResolveIncident:
		var args incidentResolveArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, bad(err)
		}
		return api.incidentAction(project, a, args.Key, args.Form)
	}
	return nil, &targetError{http.StatusBadRequest, "unknown action " + action}
}

// recordNote stores an optional free-form note as a comment in a target's timeline.
func (api *Api) recordNote(a actor, projectId db.ProjectId, targetType db.CommentTargetType, targetId, note string) {
	note = strings.TrimSpace(note)
	if note == "" || len(note) > db.CommentMaxBodyLength {
		return
	}
	if err := api.db.AddComment(a.comment(projectId, targetType, targetId, db.CommentKindComment, note, nil)); err != nil {
		klog.Errorln("failed to record a note:", err)
	}
}

// gateREST applies the approval policy for REST callers. It returns true if the response has been
// written (the action is pending or denied).
func (api *Api) gateREST(w http.ResponseWriter, project *db.Project, a actor, action string, args any, summary string, target gatedTarget, reason string) bool {
	ap, err := api.gateAgentAction(project, a, action, args, summary, target, reason)
	if err != nil {
		writeTargetError(w, err)
		return true
	}
	if ap != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(newPendingApproval(ap))
		return true
	}
	return false
}

// approvalActor reconstructs the agent that requested an approval, marked as approved by a human.
func approvalActor(ap *db.Approval, approvedBy string) actor {
	a := actor{name: ap.RequestedBy, id: ap.RequestedById, kind: ap.RequestedByKind, meta: map[string]string{}}
	for k, v := range ap.RequestedMeta {
		a.meta[k] = v
	}
	a.meta["approved_by"] = approvedBy
	a.meta["approval_id"] = strconv.Itoa(ap.Id)
	return a
}

// canDecideApproval: approving requires the permissions the action itself needs.
func (api *Api) canDecideApproval(u *db.User, projectId db.ProjectId, action string) bool {
	p := rbac.Actions.Project(string(projectId))
	switch action {
	case db.AgentActionDeleteAlertingRule, db.AgentActionDisableAlertingRule, db.AgentActionUpdateAlertingRule:
		return api.IsAllowed(u, p.AlertingRules().Edit())
	}
	return api.IsAllowed(u, p.Alerts().Edit())
}

// decideApproval approves (and executes) or rejects a pending approval on behalf of a human.
func (api *Api) decideApproval(u *db.User, project *db.Project, id int, approve bool, comment string) (*db.Approval, error) {
	human := newActor(u, viaUI)
	if human.kind != db.CommentAuthorUser {
		return nil, &targetError{http.StatusForbidden, "only humans can approve or reject agent actions"}
	}
	ap, err := api.db.GetApproval(project.Id, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, &targetError{http.StatusNotFound, "approval not found"}
		}
		return nil, err
	}
	if !api.canDecideApproval(u, project.Id, ap.Action) {
		return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
	}
	if len(comment) > db.CommentMaxBodyLength {
		return nil, &targetError{http.StatusBadRequest, "comment is too long"}
	}
	status := db.ApprovalStatusRejected
	if approve {
		status = db.ApprovalStatusApproved
	}
	if err = api.db.DecideApproval(project.Id, id, status, human.name, comment); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return nil, &targetError{http.StatusConflict, "this approval has already been decided"}
		}
		return nil, err
	}
	meta := map[string]string{"approval_id": strconv.Itoa(id), "requested_action": ap.Action, "requested_by": ap.RequestedBy}
	if !approve {
		if ap.TargetType != "" {
			api.recordAction(human, project.Id, ap.TargetType, ap.TargetId, actionApprovalRejected, comment, meta)
		}
		decided, err := api.db.GetApproval(project.Id, id)
		if err == nil {
			api.notifyApprovalDecided(project, decided)
		}
		return decided, err
	}
	if ap.TargetType != "" {
		api.recordAction(human, project.Id, ap.TargetType, ap.TargetId, actionApprovalApproved, comment, meta)
	}
	res, execErr := api.doAgentAction(project, approvalActor(ap, human.name), ap.Action, ap.Args)
	resultStatus, result := db.ApprovalStatusExecuted, ""
	if execErr != nil {
		resultStatus = db.ApprovalStatusFailed
		var te *targetError
		if errors.As(execErr, &te) {
			result = te.msg
		} else {
			klog.Errorln("approval", id, "execution failed:", execErr)
			result = "internal error"
		}
	} else if res != nil {
		if d, err := json.Marshal(res); err == nil {
			result = utils.TruncateUtf8(string(d), 4000)
		}
	}
	if err = api.db.SetApprovalResult(project.Id, id, resultStatus, result); err != nil {
		klog.Errorln(err)
	}
	decided, err := api.db.GetApproval(project.Id, id)
	if err == nil {
		api.notifyApprovalDecided(project, decided)
	}
	return decided, err
}

// notifyApprovalDecided wakes the requesting agent up (if it is a registered agent with dispatch on).
func (api *Api) notifyApprovalDecided(project *db.Project, ap *db.Approval) {
	agentId, err := strconv.Atoi(ap.RequestedMeta["agent_id"])
	if err != nil || agentId == 0 {
		return
	}
	ev := AgentEvent{
		Type: db.AgentEventApprovalDecided,
		Approval: map[string]string{
			"id": strconv.Itoa(ap.Id), "decision": ap.Status, "tool": ap.Action, "action": ap.Action,
			"decided_by": ap.DecidedBy, "summary": ap.Summary,
		},
		TargetType: string(ap.TargetType),
		TargetId:   ap.TargetId,
	}
	if err = api.DispatchAgentEvent(project, agentId, ev); err != nil && !errors.Is(err, db.ErrNotFound) {
		klog.Errorln("failed to dispatch approval_decided:", err)
	}
}

// Approvals handles GET /api/project/{project}/approvals?status=&target_type=&target_id=
func (api *Api) Approvals(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return
	}
	if !api.IsAllowed(u, rbac.Actions.Project(string(project.Id)).Alerts().View()) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	res, err := api.db.GetApprovals(project.Id, db.ApprovalsQuery{
		Status: q.Get("status"), TargetType: db.CommentTargetType(q.Get("target_type")), TargetId: q.Get("target_id"), Limit: limit,
	})
	if err != nil {
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	utils.WriteJson(w, res)
}

type approvalDecisionForm struct {
	Decision string `json:"decision"` // approve | reject
	Comment  string `json:"comment"`
}

// Approval handles GET and POST {"decision": "approve"|"reject", "comment": "..."} on /api/project/{project}/approvals/{id}.
func (api *Api) Approval(w http.ResponseWriter, r *http.Request, u *db.User) {
	vars := mux.Vars(r)
	project := api.getProjectOrError(w, db.ProjectId(vars["project"]))
	if project == nil {
		return
	}
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "invalid approval id", http.StatusBadRequest)
		return
	}
	if !api.IsAllowed(u, rbac.Actions.Project(string(project.Id)).Alerts().View()) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		ap, err := api.db.GetApproval(project.Id, id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				http.Error(w, "approval not found", http.StatusNotFound)
				return
			}
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		utils.WriteJson(w, ap)
	case http.MethodPost:
		var form approvalDecisionForm
		if err := utils.ReadJson(r, &form); err != nil || (form.Decision != "approve" && form.Decision != "reject") {
			http.Error(w, "decision must be 'approve' or 'reject'", http.StatusBadRequest)
			return
		}
		ap, err := api.decideApproval(u, project, id, form.Decision == "approve", strings.TrimSpace(form.Comment))
		if err != nil {
			writeTargetError(w, err)
			return
		}
		utils.WriteJson(w, ap)
	}
}

type approvalPolicyView struct {
	Policy   *db.AgentApprovalPolicy `json:"policy"`
	Actions  []db.AgentActionInfo    `json:"actions"`
	Editable bool                    `json:"editable"`
}

// ApprovalPolicy handles GET and PUT on /api/project/{project}/approvals/policy.
func (api *Api) ApprovalPolicy(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return
	}
	pid := string(project.Id)
	if !api.IsAllowed(u, rbac.Actions.Project(pid).Alerts().View()) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	editable := api.IsAllowed(u, rbac.Actions.Project(pid).Settings().Edit()) && newActor(u, viaUI).kind == db.CommentAuthorUser
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		if !editable {
			http.Error(w, "Only project admins (humans) can change the approval policy.", http.StatusForbidden)
			return
		}
		var p db.AgentApprovalPolicy
		if err := utils.ReadJson(r, &p); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		for action, v := range p.Actions {
			if !db.IsAgentAction(action) || !db.ValidApprovalPolicy(v) {
				http.Error(w, fmt.Sprintf("invalid policy %q for %q", v, action), http.StatusBadRequest)
				return
			}
		}
		project.Settings.AgentApprovals = &p
		if err := api.db.SaveProjectSettings(project); err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}
	policy := project.Settings.AgentApprovals
	if policy == nil {
		policy = db.DefaultAgentApprovalPolicy()
	}
	effective := &db.AgentApprovalPolicy{RequireApproval: policy.RequireApproval, Actions: map[string]string{}}
	for _, a := range db.AgentActions {
		v := policy.Actions[a.Action]
		if !db.ValidApprovalPolicy(v) {
			v = a.Default
		}
		effective.Actions[a.Action] = v
	}
	utils.WriteJson(w, approvalPolicyView{Policy: effective, Actions: db.AgentActions, Editable: editable})
}

func alertsSummary(verb string, ids []string) string {
	if len(ids) == 1 {
		return verb + " alert " + ids[0]
	}
	return fmt.Sprintf("%s %d alerts: %s", verb, len(ids), utils.Truncate(strings.Join(ids, ", "), 200))
}

// alertsGatedTarget: a single alert gets the approval card in its timeline.
func alertsGatedTarget(ids []string) gatedTarget {
	if len(ids) == 1 {
		return gatedTarget{typ: db.CommentTargetAlert, id: ids[0], title: "Alert " + ids[0]}
	}
	return gatedTarget{}
}

func ruleGatedTarget(rule *model.AlertingRule) gatedTarget {
	return gatedTarget{typ: db.CommentTargetAlertingRule, id: string(rule.Id), title: rule.Name}
}

// ruleUpdateAction: disabling a rule is gated separately from other updates.
func ruleUpdateAction(existing, updated *model.AlertingRule) string {
	if existing.Enabled && !updated.Enabled {
		return db.AgentActionDisableAlertingRule
	}
	return db.AgentActionUpdateAlertingRule
}

func ruleUpdateSummary(existing, updated *model.AlertingRule) string {
	if ruleUpdateAction(existing, updated) == db.AgentActionDisableAlertingRule {
		return "Disable the alerting rule \"" + existing.Name + "\""
	}
	summary := "Update the alerting rule \"" + existing.Name + "\""
	if changes := alertingRuleChanges(existing, updated); len(changes) > 0 {
		summary += " (" + strings.Join(changes, ", ") + ")"
	}
	return summary
}

// ruleDeletable / ruleEditable reject impossible changes before they are queued for approval.
func ruleDeletable(rule *model.AlertingRule) error {
	if rule.Readonly {
		return &targetError{http.StatusForbidden, "This rule is managed via config and cannot be deleted"}
	}
	if rule.Builtin {
		return &targetError{http.StatusForbidden, "Builtin rules cannot be deleted, disable them instead"}
	}
	return nil
}

func ruleEditable(rule *model.AlertingRule) error {
	if rule.Readonly {
		return &targetError{http.StatusForbidden, "This rule is managed via config and cannot be edited"}
	}
	return nil
}

func (api *Api) gateRuleUpdate(w http.ResponseWriter, project *db.Project, a actor, existing, updated *model.AlertingRule, comment string) bool {
	if err := ruleEditable(existing); err != nil {
		writeTargetError(w, err)
		return true
	}
	return api.gateREST(w, project, a, ruleUpdateAction(existing, updated), ruleUpdateArgs{RuleId: string(existing.Id), Rule: updated, Comment: comment},
		ruleUpdateSummary(existing, updated), ruleGatedTarget(existing), comment)
}

func (api *Api) gateRuleDelete(w http.ResponseWriter, project *db.Project, a actor, rule *model.AlertingRule) bool {
	if err := ruleDeletable(rule); err != nil {
		writeTargetError(w, err)
		return true
	}
	return api.gateREST(w, project, a, db.AgentActionDeleteAlertingRule, ruleDeleteArgs{RuleId: string(rule.Id)},
		"Delete the alerting rule \""+rule.Name+"\"", ruleGatedTarget(rule), "")
}

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coroot/coroot/api/forms"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/notifications"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/utils"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

// Channels through which a timeline entry was created.
const (
	viaUI  = "ui"
	viaAPI = "api"
	viaMCP = "mcp"
)

// Timeline action names (stored in Comment.Meta["action"]).
const (
	actionAlertResolved     = "resolved"
	actionAlertSuppressed   = "suppressed"
	actionAlertReopened     = "reopened"
	actionRuleCreated       = "created"
	actionRuleUpdated       = "updated"
	actionRuleDeleted       = "deleted"
	actionRuleEnabled       = "enabled"
	actionRuleDisabled      = "disabled"
	commentWebhookTimeout   = 30 * time.Second
	commentTargetNotFound   = "target not found"
	commentTargetForbidden  = "forbidden"
	commentTargetBadRequest = "invalid target_type, must be one of: incident, alert, alerting_rule"
)

// actor identifies who performed an action: a human (session), or an operator agent
// (a user API key via REST, or any MCP client).
type actor struct {
	name string
	id   int
	kind db.CommentAuthorKind
	meta map[string]string
}

func userDisplayName(u *db.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

func newActor(u *db.User, via string) actor {
	a := actor{name: userDisplayName(u), kind: db.CommentAuthorUser, meta: map[string]string{}}
	if !u.Anonymous {
		a.id = u.Id
	}
	if via == viaUI && (u.ApiKey != "" || u.IsServiceAccount()) {
		via = viaAPI
	}
	if via != viaUI {
		a.meta["via"] = via
	}
	if u.ApiKey != "" || u.IsServiceAccount() || via == viaMCP {
		a.kind = db.CommentAuthorAgent
		if u.ApiKey != "" {
			// the API key name identifies the agent, the user is who it acts on behalf of
			a.name = u.ApiKey
			a.meta["api_key"] = u.ApiKey
			a.meta["user"] = userDisplayName(u)
		}
	}
	return a
}

func (a actor) comment(projectId db.ProjectId, targetType db.CommentTargetType, targetId string, kind db.CommentKind, body string, meta map[string]string) *db.Comment {
	m := map[string]string{}
	for k, v := range a.meta {
		m[k] = v
	}
	for k, v := range meta {
		m[k] = v
	}
	return &db.Comment{
		ProjectId:  projectId,
		TargetType: targetType,
		TargetId:   targetId,
		Author:     a.name,
		AuthorId:   a.id,
		AuthorKind: a.kind,
		Kind:       kind,
		Body:       body,
		Meta:       m,
	}
}

// commentTarget is a resolved, permission-checked timeline target.
type commentTarget struct {
	typ      db.CommentTargetType
	id       string
	incident *model.ApplicationIncident
	alert    *model.Alert
	rule     *model.AlertingRule
}

type targetError struct {
	status int
	msg    string
}

func (e *targetError) Error() string { return e.msg }

// resolveCommentTarget loads a target and checks that the user may read (write=false) or
// comment on (write=true) it. Reading requires the same permissions as viewing the object;
// writing requires Alerts:Edit for incidents/alerts and AlertingRules:Edit for rules.
func (api *Api) resolveCommentTarget(u *db.User, project *db.Project, targetType, targetId string, write bool) (*commentTarget, error) {
	pid := string(project.Id)
	if !api.IsAllowed(u, rbac.Actions.Project(pid).List()...) {
		return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
	}
	t := &commentTarget{typ: db.CommentTargetType(targetType), id: targetId}
	if !t.typ.Valid() {
		return nil, &targetError{http.StatusBadRequest, commentTargetBadRequest}
	}
	if targetId == "" {
		return nil, &targetError{http.StatusBadRequest, "target_id is required"}
	}
	notFound := func(err error) error {
		if errors.Is(err, db.ErrNotFound) {
			return &targetError{http.StatusNotFound, commentTargetNotFound}
		}
		return err
	}
	switch t.typ {
	case db.CommentTargetIncident:
		i, err := api.db.GetIncidentByKey(project.Id, targetId)
		if err != nil {
			return nil, notFound(err)
		}
		t.incident = i
		category := project.CalcApplicationCategory(i.ApplicationId)
		if !api.IsAllowed(u, rbac.Actions.Project(pid).Application(category, i.ApplicationId.Namespace, i.ApplicationId.Kind, i.ApplicationId.Name).View()) {
			return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
		}
		if write && !api.IsAllowed(u, rbac.Actions.Project(pid).Alerts().Edit()) {
			return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
		}
	case db.CommentTargetAlert:
		if !api.IsAllowed(u, rbac.Actions.Project(pid).Alerts().View()) {
			return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
		}
		a, err := api.db.GetAlert(project.Id, targetId)
		if err != nil {
			return nil, notFound(err)
		}
		if a == nil {
			return nil, &targetError{http.StatusNotFound, commentTargetNotFound}
		}
		t.alert = a
		if !api.IsAllowed(u, rbac.Actions.Project(pid).Application(a.ApplicationCategory, a.ApplicationId.Namespace, a.ApplicationId.Kind, a.ApplicationId.Name).View()) {
			return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
		}
		if write && !api.IsAllowed(u, rbac.Actions.Project(pid).Alerts().Edit()) {
			return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
		}
	case db.CommentTargetAlertingRule:
		if !api.IsAllowed(u, rbac.Actions.Project(pid).AlertingRules().View()) {
			return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
		}
		r, err := api.db.GetAlertingRule(project.Id, model.AlertingRuleId(targetId))
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return nil, err
		}
		// the timeline of a deleted rule stays readable (it ends with the "deleted" action)
		if r == nil && write {
			return nil, &targetError{http.StatusNotFound, commentTargetNotFound}
		}
		t.rule = r
		if write && !api.IsAllowed(u, rbac.Actions.Project(pid).AlertingRules().Edit()) {
			return nil, &targetError{http.StatusForbidden, commentTargetForbidden}
		}
	}
	return t, nil
}

// canModifyComment: authors may edit/delete their own comments, project admins may edit/delete any entry.
func (api *Api) canModifyComment(u *db.User, c *db.Comment) bool {
	if api.IsAllowed(u, rbac.Actions.Project(string(c.ProjectId)).Settings().Edit()) {
		return true
	}
	return c.Kind == db.CommentKindComment && !u.Anonymous && c.AuthorId != 0 && c.AuthorId == u.Id
}

func (api *Api) getTimeline(u *db.User, t *commentTarget, projectId db.ProjectId) ([]*db.Comment, error) {
	comments, err := api.db.GetComments(projectId, t.typ, t.id)
	if err != nil {
		return nil, err
	}
	for _, c := range comments {
		c.Editable = api.canModifyComment(u, c)
	}
	return comments, nil
}

// addComment posts a comment on behalf of the user after checking permissions, and forwards
// it to the project's webhook (if a comment template is configured).
func (api *Api) addComment(u *db.User, via string, project *db.Project, targetType, targetId, body string) (*db.Comment, error) {
	if err := forms.ValidateCommentBody(body); err != nil {
		return nil, &targetError{http.StatusBadRequest, err.Error()}
	}
	t, err := api.resolveCommentTarget(u, project, targetType, targetId, true)
	if err != nil {
		return nil, err
	}
	c := newActor(u, via).comment(project.Id, t.typ, t.id, db.CommentKindComment, body, nil)
	if err = api.db.AddComment(c); err != nil {
		return nil, err
	}
	c.Editable = api.canModifyComment(u, c)
	api.forwardComment(project, t, c)
	return c, nil
}

// recordAction appends an action entry to a target's timeline. Failures are logged only:
// the action itself has already been performed.
func (api *Api) recordAction(a actor, projectId db.ProjectId, targetType db.CommentTargetType, targetId, action, note string, meta map[string]string) {
	m := map[string]string{"action": action}
	for k, v := range meta {
		m[k] = v
	}
	c := a.comment(projectId, targetType, targetId, db.CommentKindAction, note, m)
	if err := api.db.AddComment(c); err != nil {
		klog.Errorln("failed to record timeline action:", err)
	}
}

func (api *Api) recordAlertActions(a actor, projectId db.ProjectId, ids []string, action, note string) {
	for _, id := range ids {
		if alert, err := api.db.GetAlert(projectId, id); err != nil || alert == nil {
			continue
		}
		api.recordAction(a, projectId, db.CommentTargetAlert, id, action, note, nil)
	}
}

func (api *Api) forwardComment(project *db.Project, t *commentTarget, c *db.Comment) {
	values := notifications.CommentTemplateValues{
		ProjectName: project.Name,
		TargetType:  string(t.typ),
		TargetId:    t.id,
		Author:      c.Author,
		AuthorKind:  string(c.AuthorKind),
		Body:        c.Body,
		URL:         notifications.CommentUrl(project.Settings.Integrations.BaseUrl, project.Id, t.typ, t.id),
	}
	switch {
	case t.incident != nil:
		values.Application = t.incident.ApplicationId
		values.Title = "Incident " + t.incident.Key
	case t.alert != nil:
		values.Application = t.alert.ApplicationId
		values.Title = t.alert.Summary
	case t.rule != nil:
		values.Title = t.rule.Name
	}
	notifications.ForwardCommentShards(project.Settings.Integrations, values)
	cfg := project.Settings.Integrations.Webhook
	if cfg == nil || cfg.CommentTemplate == "" {
		return
	}
	wh := notifications.NewWebhook(cfg)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), commentWebhookTimeout)
		defer cancel()
		if err := wh.SendComment(ctx, values); err != nil {
			klog.Errorln("failed to forward comment to webhook:", err)
		}
	}()
}

func writeTargetError(w http.ResponseWriter, err error) {
	var te *targetError
	if errors.As(err, &te) {
		http.Error(w, te.msg, te.status)
		return
	}
	klog.Errorln(err)
	http.Error(w, "", http.StatusInternalServerError)
}

func (api *Api) getProjectOrError(w http.ResponseWriter, projectId db.ProjectId) *db.Project {
	project, err := api.db.GetProject(projectId)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Error(w, "project not found", http.StatusNotFound)
			return nil
		}
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return nil
	}
	return project
}

// Comments handles GET (timeline) and POST (new comment) on
// /api/project/{project}/comments?target_type=incident|alert|alerting_rule&target_id=...
// POST accepts {"body": "...markdown..."} (target_type/target_id may also be passed in the body).
func (api *Api) Comments(w http.ResponseWriter, r *http.Request, u *db.User) {
	project := api.getProjectOrError(w, db.ProjectId(mux.Vars(r)["project"]))
	if project == nil {
		return
	}
	q := r.URL.Query()
	targetType, targetId := q.Get("target_type"), q.Get("target_id")

	switch r.Method {
	case http.MethodGet:
		t, err := api.resolveCommentTarget(u, project, targetType, targetId, false)
		if err != nil {
			writeTargetError(w, err)
			return
		}
		comments, err := api.getTimeline(u, t, project.Id)
		if err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		utils.WriteJson(w, comments)
	case http.MethodPost:
		var form forms.CommentForm
		if err := utils.ReadJson(r, &form); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if form.TargetType != "" {
			targetType = form.TargetType
		}
		if form.TargetId != "" {
			targetId = form.TargetId
		}
		c, err := api.addComment(u, viaUI, project, targetType, targetId, form.Body)
		if err != nil {
			writeTargetError(w, err)
			return
		}
		utils.WriteJson(w, c)
	}
}

// Comment handles PUT {"body": "..."} and DELETE on /api/project/{project}/comments/{id}.
// Only the author of a comment or a project admin may edit or delete it.
func (api *Api) Comment(w http.ResponseWriter, r *http.Request, u *db.User) {
	vars := mux.Vars(r)
	project := api.getProjectOrError(w, db.ProjectId(vars["project"]))
	if project == nil {
		return
	}
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "invalid comment id", http.StatusBadRequest)
		return
	}
	c, err := api.db.GetComment(project.Id, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Error(w, "comment not found", http.StatusNotFound)
			return
		}
		klog.Errorln(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	if _, err = api.resolveCommentTarget(u, project, string(c.TargetType), c.TargetId, false); err != nil {
		writeTargetError(w, err)
		return
	}
	if !api.canModifyComment(u, c) {
		http.Error(w, "Only the author or an admin can modify this entry.", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var form forms.CommentForm
		if err = forms.ReadAndValidate(r, &form); err != nil {
			http.Error(w, "comment body is required", http.StatusBadRequest)
			return
		}
		if err = api.db.UpdateCommentBody(project.Id, id, form.Body); err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		if c, err = api.db.GetComment(project.Id, id); err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		c.Editable = true
		utils.WriteJson(w, c)
	case http.MethodDelete:
		if err = api.db.DeleteComment(project.Id, id); err != nil {
			klog.Errorln(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

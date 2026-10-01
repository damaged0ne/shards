package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/coroot/coroot/timeseries"
)

// shards fork: agent actions waiting for a human decision.

const (
	ApprovalStatusPending  = "pending"
	ApprovalStatusApproved = "approved" // approved, being executed
	ApprovalStatusExecuted = "executed"
	ApprovalStatusFailed   = "failed" // approved, but the execution failed
	ApprovalStatusRejected = "rejected"
)

type Approval struct {
	Id              int               `json:"id"`
	ProjectId       ProjectId         `json:"-"`
	Action          string            `json:"action"`
	Args            json.RawMessage   `json:"args"`
	Summary         string            `json:"summary"`
	TargetType      CommentTargetType `json:"target_type,omitempty"`
	TargetId        string            `json:"target_id,omitempty"`
	TargetTitle     string            `json:"target_title,omitempty"`
	RequestedBy     string            `json:"requested_by"`
	RequestedById   int               `json:"-"`
	RequestedByKind CommentAuthorKind `json:"requested_by_kind"`
	RequestedMeta   map[string]string `json:"requested_meta,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	Status          string            `json:"status"`
	DecidedBy       string            `json:"decided_by,omitempty"`
	DecidedAt       timeseries.Time   `json:"decided_at"`
	DecisionComment string            `json:"decision_comment,omitempty"`
	Result          string            `json:"result,omitempty"`
	CreatedAt       timeseries.Time   `json:"created_at"`
}

func (a *Approval) Migrate(m *Migrator) error {
	err := m.Exec(`
	CREATE TABLE IF NOT EXISTS approval (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL REFERENCES project(id),
		action TEXT NOT NULL,
		args TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		target_type TEXT NOT NULL DEFAULT '',
		target_id TEXT NOT NULL DEFAULT '',
		target_title TEXT NOT NULL DEFAULT '',
		requested_by TEXT NOT NULL,
		requested_by_id INT NOT NULL DEFAULT 0,
		requested_by_kind TEXT NOT NULL,
		requested_meta TEXT NOT NULL DEFAULT '',
		reason TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		decided_by TEXT NOT NULL DEFAULT '',
		decided_at INT NOT NULL DEFAULT 0,
		decision_comment TEXT NOT NULL DEFAULT '',
		result TEXT NOT NULL DEFAULT '',
		created_at INT NOT NULL
	)`)
	if err != nil {
		return err
	}
	return m.Exec(`CREATE INDEX IF NOT EXISTS approval_project_status ON approval (project_id, status)`)
}

const approvalColumns = "id, action, args, summary, target_type, target_id, target_title, requested_by, requested_by_id, requested_by_kind, requested_meta, reason, status, decided_by, decided_at, decision_comment, result, created_at"

func scanApproval(projectId ProjectId, scan func(dest ...any) error) (*Approval, error) {
	a := &Approval{ProjectId: projectId}
	var args, meta string
	err := scan(&a.Id, &a.Action, &args, &a.Summary, &a.TargetType, &a.TargetId, &a.TargetTitle, &a.RequestedBy, &a.RequestedById,
		&a.RequestedByKind, &meta, &a.Reason, &a.Status, &a.DecidedBy, &a.DecidedAt, &a.DecisionComment, &a.Result, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	if args != "" {
		a.Args = json.RawMessage(args)
	}
	if meta != "" {
		_ = json.Unmarshal([]byte(meta), &a.RequestedMeta)
	}
	return a, nil
}

func (db *DB) CreateApproval(a *Approval) error {
	if a.CreatedAt == 0 {
		a.CreatedAt = timeseries.Now()
	}
	if a.Status == "" {
		a.Status = ApprovalStatusPending
	}
	meta, err := marshalCommentMeta(a.RequestedMeta)
	if err != nil {
		return err
	}
	return db.db.QueryRow(
		"INSERT INTO approval (project_id, action, args, summary, target_type, target_id, target_title, requested_by, requested_by_id, requested_by_kind, requested_meta, reason, status, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id",
		a.ProjectId, a.Action, string(a.Args), a.Summary, a.TargetType, a.TargetId, a.TargetTitle, a.RequestedBy, a.RequestedById, a.RequestedByKind, meta, a.Reason, a.Status, a.CreatedAt,
	).Scan(&a.Id)
}

func (db *DB) GetApproval(projectId ProjectId, id int) (*Approval, error) {
	row := db.db.QueryRow("SELECT "+approvalColumns+" FROM approval WHERE project_id = $1 AND id = $2", projectId, id)
	a, err := scanApproval(projectId, row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

type ApprovalsQuery struct {
	Status     string
	TargetType CommentTargetType
	TargetId   string
	Limit      int
}

// GetApprovals returns approvals newest first.
func (db *DB) GetApprovals(projectId ProjectId, q ApprovalsQuery) ([]*Approval, error) {
	sqlq := "SELECT " + approvalColumns + " FROM approval WHERE project_id = $1"
	args := []any{projectId}
	add := func(cond string, v any) {
		args = append(args, v)
		sqlq += " AND " + cond + " = $" + strconv.Itoa(len(args))
	}
	if q.Status != "" {
		add("status", q.Status)
	}
	if q.TargetType != "" {
		add("target_type", q.TargetType)
	}
	if q.TargetId != "" {
		add("target_id", q.TargetId)
	}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	args = append(args, q.Limit)
	sqlq += " ORDER BY created_at DESC, id DESC LIMIT $" + strconv.Itoa(len(args))
	rows, err := db.db.Query(sqlq, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []*Approval{}
	for rows.Next() {
		a, err := scanApproval(projectId, rows.Scan)
		if err != nil {
			return nil, err
		}
		res = append(res, a)
	}
	return res, rows.Err()
}

func (db *DB) CountPendingApprovals(projectId ProjectId) (int, error) {
	var n int
	err := db.db.QueryRow("SELECT count(*) FROM approval WHERE project_id = $1 AND status = $2", projectId, ApprovalStatusPending).Scan(&n)
	return n, err
}

// DecideApproval moves a pending approval to approved/rejected. It returns ErrConflict if the
// approval has already been decided (so an action can never be executed twice).
func (db *DB) DecideApproval(projectId ProjectId, id int, status, by, comment string) error {
	res, err := db.db.Exec(
		"UPDATE approval SET status = $1, decided_by = $2, decided_at = $3, decision_comment = $4 WHERE project_id = $5 AND id = $6 AND status = $7",
		status, by, timeseries.Now(), comment, projectId, id, ApprovalStatusPending)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (db *DB) SetApprovalResult(projectId ProjectId, id int, status, result string) error {
	_, err := db.db.Exec("UPDATE approval SET status = $1, result = $2 WHERE project_id = $3 AND id = $4", status, result, projectId, id)
	return err
}

// Agent action policies.
const (
	ApprovalPolicyAuto     = "auto"
	ApprovalPolicyApproval = "approval"
	ApprovalPolicyDeny     = "deny"
)

// Gated agent actions.
const (
	AgentActionDeleteAlertingRule      = "delete_alerting_rule"
	AgentActionDisableAlertingRule     = "disable_alerting_rule"
	AgentActionUpdateAlertingRule      = "update_alerting_rule"
	AgentActionSuppressAlerts          = "suppress_alerts"
	AgentActionResolveAlerts           = "resolve_alerts"
	AgentActionCreateMaintenanceWindow = "create_maintenance_window"
	AgentActionEndMaintenanceWindow    = "end_maintenance_window"
	AgentActionResolveIncident         = "resolve_incident"
)

type AgentActionInfo struct {
	Action  string `json:"action"`
	Title   string `json:"title"`
	Default string `json:"default"`
}

// AgentActions lists the gated actions with their default policy.
var AgentActions = []AgentActionInfo{
	{AgentActionDeleteAlertingRule, "Delete an alerting rule", ApprovalPolicyApproval},
	{AgentActionDisableAlertingRule, "Disable an alerting rule", ApprovalPolicyApproval},
	{AgentActionUpdateAlertingRule, "Update an alerting rule", ApprovalPolicyAuto},
	{AgentActionSuppressAlerts, "Suppress alerts", ApprovalPolicyAuto},
	{AgentActionResolveAlerts, "Resolve alerts", ApprovalPolicyAuto},
	{AgentActionCreateMaintenanceWindow, "Create a maintenance window", ApprovalPolicyAuto},
	{AgentActionEndMaintenanceWindow, "End a maintenance window", ApprovalPolicyAuto},
	{AgentActionResolveIncident, "Resolve an incident", ApprovalPolicyApproval},
}

// AgentApprovalPolicy is the project setting "require human approval for agent actions".
// A nil policy means the defaults (enabled, AgentActions[].Default).
type AgentApprovalPolicy struct {
	RequireApproval bool              `json:"require_approval"`
	Actions         map[string]string `json:"actions,omitempty"`
}

func DefaultAgentApprovalPolicy() *AgentApprovalPolicy {
	p := &AgentApprovalPolicy{RequireApproval: true, Actions: map[string]string{}}
	for _, a := range AgentActions {
		p.Actions[a.Action] = a.Default
	}
	return p
}

func IsAgentAction(action string) bool {
	for _, a := range AgentActions {
		if a.Action == action {
			return true
		}
	}
	return false
}

func ValidApprovalPolicy(p string) bool {
	return p == ApprovalPolicyAuto || p == ApprovalPolicyApproval || p == ApprovalPolicyDeny
}

// Effective returns the policy for an action. With RequireApproval off nothing waits for a human,
// but 'deny' still applies.
func (p *AgentApprovalPolicy) Effective(action string) string {
	if p == nil {
		p = DefaultAgentApprovalPolicy()
	}
	v, ok := p.Actions[action]
	if !ok || !ValidApprovalPolicy(v) {
		v = ApprovalPolicyAuto
		for _, a := range AgentActions {
			if a.Action == action {
				v = a.Default
			}
		}
	}
	if v == ApprovalPolicyApproval && !p.RequireApproval {
		return ApprovalPolicyAuto
	}
	return v
}

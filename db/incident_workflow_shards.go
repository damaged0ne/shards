package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
)

// shards fork: the human/agent workflow layer on top of the SLO-driven incidents.
// The incident table stays owned by the incidents watcher (open/resolve by burn rate);
// the workflow state (status, assignee, resolution, ...) lives in its own table so that
// auto-resolution never overwrites what people wrote.

type IncidentStatus string

const (
	IncidentStatusTriggered    IncidentStatus = "triggered"
	IncidentStatusAcknowledged IncidentStatus = "acknowledged"
	IncidentStatusMitigated    IncidentStatus = "mitigated"
	IncidentStatusResolved     IncidentStatus = "resolved"
)

// IncidentResolvedKind tells who closed an incident: a human, an agent, or the SLO check ("auto").
const (
	IncidentResolvedByUser  = "user"
	IncidentResolvedByAgent = "agent"
	IncidentResolvedByAuto  = "auto"
)

type IncidentWorkflow struct {
	ProjectId        ProjectId         `json:"-"`
	IncidentKey      string            `json:"incident_key"`
	ApplicationId    string            `json:"-"`
	Status           IncidentStatus    `json:"status"`
	Assignee         string            `json:"assignee,omitempty"`
	AssigneeKind     CommentAuthorKind `json:"assignee_kind,omitempty"`
	AcknowledgedAt   timeseries.Time   `json:"acknowledged_at"`
	AcknowledgedBy   string            `json:"acknowledged_by,omitempty"`
	MitigatedAt      timeseries.Time   `json:"mitigated_at"`
	MitigatedBy      string            `json:"mitigated_by,omitempty"`
	ResolvedAt       timeseries.Time   `json:"resolved_at"`
	ResolvedBy       string            `json:"resolved_by,omitempty"`
	ResolvedKind     string            `json:"resolved_kind,omitempty"`
	SeverityOverride string            `json:"severity_override,omitempty"`
	Resolution       string            `json:"resolution,omitempty"`
	RootCause        string            `json:"root_cause,omitempty"`
	FollowUps        []string          `json:"follow_ups,omitempty"`
	UpdatedAt        timeseries.Time   `json:"updated_at"`
}

func (w *IncidentWorkflow) Migrate(m *Migrator) error {
	err := m.Exec(`
	CREATE TABLE IF NOT EXISTS incident_workflow (
		project_id TEXT NOT NULL REFERENCES project(id),
		incident_key TEXT NOT NULL,
		application_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		assignee TEXT NOT NULL DEFAULT '',
		assignee_kind TEXT NOT NULL DEFAULT '',
		acknowledged_at INT NOT NULL DEFAULT 0,
		acknowledged_by TEXT NOT NULL DEFAULT '',
		mitigated_at INT NOT NULL DEFAULT 0,
		mitigated_by TEXT NOT NULL DEFAULT '',
		resolved_at INT NOT NULL DEFAULT 0,
		resolved_by TEXT NOT NULL DEFAULT '',
		resolved_kind TEXT NOT NULL DEFAULT '',
		severity_override TEXT NOT NULL DEFAULT '',
		resolution TEXT NOT NULL DEFAULT '',
		root_cause TEXT NOT NULL DEFAULT '',
		follow_ups TEXT NOT NULL DEFAULT '',
		updated_at INT NOT NULL DEFAULT 0,
		PRIMARY KEY (project_id, incident_key)
	)`)
	if err != nil {
		return err
	}
	return m.Exec(`CREATE INDEX IF NOT EXISTS incident_workflow_app ON incident_workflow (project_id, application_id, resolved_at)`)
}

const incidentWorkflowColumns = "incident_key, application_id, status, assignee, assignee_kind, acknowledged_at, acknowledged_by, mitigated_at, mitigated_by, resolved_at, resolved_by, resolved_kind, severity_override, resolution, root_cause, follow_ups, updated_at"

func scanIncidentWorkflow(projectId ProjectId, scan func(dest ...any) error) (*IncidentWorkflow, error) {
	w := &IncidentWorkflow{ProjectId: projectId}
	var followUps string
	err := scan(&w.IncidentKey, &w.ApplicationId, &w.Status, &w.Assignee, &w.AssigneeKind, &w.AcknowledgedAt, &w.AcknowledgedBy,
		&w.MitigatedAt, &w.MitigatedBy, &w.ResolvedAt, &w.ResolvedBy, &w.ResolvedKind, &w.SeverityOverride, &w.Resolution, &w.RootCause,
		&followUps, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if followUps != "" {
		_ = json.Unmarshal([]byte(followUps), &w.FollowUps)
	}
	return w, nil
}

// GetIncidentWorkflow returns the workflow state of an incident, or nil if nobody has touched it yet.
func (db *DB) GetIncidentWorkflow(projectId ProjectId, key string) (*IncidentWorkflow, error) {
	row := db.db.QueryRow("SELECT "+incidentWorkflowColumns+" FROM incident_workflow WHERE project_id = $1 AND incident_key = $2", projectId, key)
	w, err := scanIncidentWorkflow(projectId, row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

// GetIncidentWorkflows returns the workflow states of the given incidents (all of the project if keys is empty).
func (db *DB) GetIncidentWorkflows(projectId ProjectId, keys []string) (map[string]*IncidentWorkflow, error) {
	q := "SELECT " + incidentWorkflowColumns + " FROM incident_workflow WHERE project_id = $1"
	args := []any{projectId}
	if len(keys) > 0 {
		ph := make([]string, len(keys))
		for i, k := range keys {
			ph[i] = fmt.Sprintf("$%d", i+2)
			args = append(args, k)
		}
		q += " AND incident_key IN (" + strings.Join(ph, ", ") + ")"
	}
	rows, err := db.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]*IncidentWorkflow{}
	for rows.Next() {
		w, err := scanIncidentWorkflow(projectId, rows.Scan)
		if err != nil {
			return nil, err
		}
		res[w.IncidentKey] = w
	}
	return res, rows.Err()
}

func (db *DB) SaveIncidentWorkflow(w *IncidentWorkflow) error {
	if w.Status == "" {
		w.Status = IncidentStatusTriggered
	}
	w.UpdatedAt = timeseries.Now()
	followUps := ""
	if len(w.FollowUps) > 0 {
		d, err := json.Marshal(w.FollowUps)
		if err != nil {
			return err
		}
		followUps = string(d)
	}
	_, err := db.db.Exec(`
	INSERT INTO incident_workflow (project_id, `+incidentWorkflowColumns+`)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	ON CONFLICT (project_id, incident_key) DO UPDATE SET
		application_id = excluded.application_id, status = excluded.status, assignee = excluded.assignee,
		assignee_kind = excluded.assignee_kind, acknowledged_at = excluded.acknowledged_at, acknowledged_by = excluded.acknowledged_by,
		mitigated_at = excluded.mitigated_at, mitigated_by = excluded.mitigated_by, resolved_at = excluded.resolved_at,
		resolved_by = excluded.resolved_by, resolved_kind = excluded.resolved_kind, severity_override = excluded.severity_override,
		resolution = excluded.resolution, root_cause = excluded.root_cause, follow_ups = excluded.follow_ups, updated_at = excluded.updated_at`,
		w.ProjectId, w.IncidentKey, w.ApplicationId, w.Status, w.Assignee, w.AssigneeKind, w.AcknowledgedAt, w.AcknowledgedBy,
		w.MitigatedAt, w.MitigatedBy, w.ResolvedAt, w.ResolvedBy, w.ResolvedKind, w.SeverityOverride, w.Resolution, w.RootCause,
		followUps, w.UpdatedAt)
	return err
}

// CountUnacknowledgedIncidents counts open incidents nobody has acknowledged yet.
func (db *DB) CountUnacknowledgedIncidents(projectId ProjectId) (int, error) {
	var n int
	err := db.db.QueryRow(`
	SELECT count(*) FROM incident i
	LEFT JOIN incident_workflow w ON w.project_id = i.project_id AND w.incident_key = i.key
	WHERE i.project_id = $1 AND i.resolved_at = 0 AND (w.status IS NULL OR w.status = $2)`,
		projectId, IncidentStatusTriggered).Scan(&n)
	return n, err
}

// IncidentResolvedByHumanSince reports whether an incident of the app was resolved by a person or an agent after since.
func (db *DB) IncidentResolvedByHumanSince(projectId ProjectId, appId model.ApplicationId, since timeseries.Time) (bool, error) {
	var n int
	err := db.db.QueryRow(`
	SELECT count(*) FROM incident_workflow
	WHERE project_id = $1 AND (application_id = $2 OR application_id = $3) AND status = $4 AND resolved_kind IN ($5, $6) AND resolved_at >= $7`,
		projectId, appId.String(), appId.StringWithoutClusterId(), IncidentStatusResolved, IncidentResolvedByUser, IncidentResolvedByAgent, since).Scan(&n)
	return n > 0, err
}

// GetApplicationIncidentsSince returns the incidents of an application opened after since, newest first.
func (db *DB) GetApplicationIncidentsSince(projectId ProjectId, appId model.ApplicationId, since timeseries.Time, limit int) ([]*model.ApplicationIncident, error) {
	rows, err := db.db.Query(
		"SELECT application_id, key, opened_at, resolved_at, severity, details, rca FROM incident WHERE project_id = $1 AND (application_id = $2 OR application_id = $3) AND opened_at >= $4 ORDER BY opened_at DESC LIMIT $5",
		projectId, appId.String(), appId.StringWithoutClusterId(), since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*model.ApplicationIncident
	for rows.Next() {
		i, err := scanIncident(rows, projectId)
		if err != nil {
			return nil, err
		}
		res = append(res, i)
	}
	return res, rows.Err()
}

// SetIncidentResolvedAt closes an incident on behalf of a person or an agent.
func (db *DB) SetIncidentResolvedAt(projectId ProjectId, key string, at timeseries.Time) error {
	_, err := db.db.Exec("UPDATE incident SET resolved_at = $1 WHERE project_id = $2 AND key = $3 AND resolved_at = 0", at, projectId, key)
	return err
}

// GetRecentCommentsByAuthorKind returns the latest timeline entries of the project written by the given kind of author.
func (db *DB) GetRecentCommentsByAuthorKind(projectId ProjectId, kind CommentAuthorKind, limit int) ([]*Comment, error) {
	rows, err := db.db.Query(
		"SELECT "+commentColumns+" FROM comment WHERE project_id = $1 AND author_kind = $2 ORDER BY created_at DESC, id DESC LIMIT $3",
		projectId, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []*Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, c)
	}
	return res, rows.Err()
}

// deleteProjectWorkflowData removes the fork's per-project rows (called from DeleteProject).
func deleteProjectWorkflowData(tx *sql.Tx, id ProjectId) error {
	for _, table := range []string{"incident_workflow", "maintenance_mark", "maintenance_window", "approval"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE project_id = $1", id); err != nil {
			return err
		}
	}
	return nil
}

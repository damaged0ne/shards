package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// shards fork: maintenance windows (silences). While a window is active, matching alerts and
// incidents are still created (so the state stays visible) but notifications are not sent; they
// are marked "in maintenance" instead (see maintenance_mark).

// MaintenanceScope selects what a window silences. Empty lists mean "any"; the non-empty
// dimensions must all match (AND), any entry of a list may match (OR). An empty scope silences everything.
type MaintenanceScope struct {
	ApplicationPatterns []string `json:"application_patterns,omitempty"` // globs of 'namespace:Kind:name'
	Categories          []string `json:"categories,omitempty"`
	NodePatterns        []string `json:"node_patterns,omitempty"` // globs of node names
	AlertingRuleIds     []string `json:"alerting_rule_ids,omitempty"`
}

func (s MaintenanceScope) IsAll() bool {
	return len(s.ApplicationPatterns) == 0 && len(s.Categories) == 0 && len(s.NodePatterns) == 0 && len(s.AlertingRuleIds) == 0
}

// MaintenanceRecurrence is a simple weekly schedule: on the given weekdays, starting at StartTime
// (in Timezone, UTC by default) for DurationMinutes.
type MaintenanceRecurrence struct {
	Weekdays        []int  `json:"weekdays"`   // 0 = Sunday ... 6 = Saturday
	StartTime       string `json:"start_time"` // HH:MM
	DurationMinutes int    `json:"duration_minutes"`
	Timezone        string `json:"timezone,omitempty"`
}

type MaintenanceWindow struct {
	Id            int                    `json:"id"`
	ProjectId     ProjectId              `json:"-"`
	Name          string                 `json:"name"`
	StartsAt      timeseries.Time        `json:"starts_at"`
	EndsAt        timeseries.Time        `json:"ends_at"` // one-off: required; recurring: optional end of the schedule
	Recurrence    *MaintenanceRecurrence `json:"recurrence,omitempty"`
	Scope         MaintenanceScope       `json:"scope"`
	Comment       string                 `json:"comment,omitempty"`
	CreatedBy     string                 `json:"created_by"`
	CreatedByKind CommentAuthorKind      `json:"created_by_kind"`
	CreatedAt     timeseries.Time        `json:"created_at"`
	EndedAt       timeseries.Time        `json:"ended_at"`
	EndedBy       string                 `json:"ended_by,omitempty"`
}

const (
	MaintenanceMaxDuration          = 30 * timeseries.Day
	MaintenanceMaxRecurringDuration = 24 * 60
)

var maintenanceTimeRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (w *MaintenanceWindow) Validate() error {
	w.Name = strings.TrimSpace(w.Name)
	if w.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(w.Name) > 200 || len(w.Comment) > CommentMaxBodyLength {
		return fmt.Errorf("%w: name or comment is too long", ErrInvalid)
	}
	if w.StartsAt == 0 {
		w.StartsAt = timeseries.Now()
	}
	if w.Recurrence == nil {
		if w.EndsAt <= w.StartsAt {
			return fmt.Errorf("%w: ends_at must be after starts_at", ErrInvalid)
		}
		if w.EndsAt.Sub(w.StartsAt) > MaintenanceMaxDuration {
			return fmt.Errorf("%w: a one-off window can't be longer than 30 days, use a weekly schedule", ErrInvalid)
		}
	} else {
		r := w.Recurrence
		if len(r.Weekdays) == 0 {
			return fmt.Errorf("%w: recurrence.weekdays is required", ErrInvalid)
		}
		for _, d := range r.Weekdays {
			if d < 0 || d > 6 {
				return fmt.Errorf("%w: weekdays must be 0 (Sunday) .. 6 (Saturday)", ErrInvalid)
			}
		}
		if !maintenanceTimeRe.MatchString(r.StartTime) {
			return fmt.Errorf("%w: recurrence.start_time must be HH:MM", ErrInvalid)
		}
		if r.DurationMinutes <= 0 || r.DurationMinutes > MaintenanceMaxRecurringDuration {
			return fmt.Errorf("%w: recurrence.duration_minutes must be 1..1440", ErrInvalid)
		}
		if _, err := time.LoadLocation(r.Timezone); err != nil {
			return fmt.Errorf("%w: unknown timezone %q", ErrInvalid, r.Timezone)
		}
		if w.EndsAt != 0 && w.EndsAt <= w.StartsAt {
			return fmt.Errorf("%w: ends_at must be after starts_at", ErrInvalid)
		}
	}
	return nil
}

// occurrence returns the recurring occurrence [from, to) that contains now, or the next one (within 8 days).
func (w *MaintenanceWindow) occurrence(now timeseries.Time) (timeseries.Time, timeseries.Time, bool) {
	r := w.Recurrence
	loc, err := time.LoadLocation(r.Timezone)
	if err != nil {
		loc = time.UTC
	}
	var hh, mm int
	if _, err := fmt.Sscanf(r.StartTime, "%d:%d", &hh, &mm); err != nil {
		return 0, 0, false
	}
	days := map[int]bool{}
	for _, d := range r.Weekdays {
		days[d] = true
	}
	n := now.ToStandard().In(loc)
	dur := time.Duration(r.DurationMinutes) * time.Minute
	for offset := -1; offset <= 8; offset++ {
		d := n.AddDate(0, 0, offset)
		if !days[int(d.Weekday())] {
			continue
		}
		start := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, loc)
		end := start.Add(dur)
		if !end.After(n) {
			continue
		}
		from, to := timeseries.TimeFromStandard(start), timeseries.TimeFromStandard(end)
		if from < w.StartsAt {
			if to <= w.StartsAt {
				continue
			}
			from = w.StartsAt
		}
		if w.EndsAt != 0 && from >= w.EndsAt {
			return 0, 0, false
		}
		if w.EndsAt != 0 && to > w.EndsAt {
			to = w.EndsAt
		}
		return from, to, true
	}
	return 0, 0, false
}

func (w *MaintenanceWindow) IsActive(now timeseries.Time) bool {
	if w.EndedAt != 0 && now >= w.EndedAt {
		return false
	}
	if now < w.StartsAt || (w.EndsAt != 0 && now >= w.EndsAt) {
		return false
	}
	if w.Recurrence == nil {
		return true
	}
	from, to, ok := w.occurrence(now)
	return ok && from <= now && now < to
}

// Status is one of: active, scheduled, ended (manually), expired.
func (w *MaintenanceWindow) Status(now timeseries.Time) string {
	switch {
	case w.EndedAt != 0 && now >= w.EndedAt:
		return "ended"
	case w.IsActive(now):
		return "active"
	case w.EndsAt != 0 && now >= w.EndsAt:
		return "expired"
	case w.Recurrence != nil:
		if _, _, ok := w.occurrence(now); !ok {
			return "expired"
		}
	}
	return "scheduled"
}

// CurrentOrNext returns the active occurrence, or the next one if the window is scheduled.
func (w *MaintenanceWindow) CurrentOrNext(now timeseries.Time) (timeseries.Time, timeseries.Time) {
	if w.Recurrence == nil {
		return w.StartsAt, w.EndsAt
	}
	from, to, _ := w.occurrence(now)
	return from, to
}

// MaintenanceTarget describes an alert or an incident for scope matching.
type MaintenanceTarget struct {
	ApplicationId model.ApplicationId
	Category      model.ApplicationCategory
	Nodes         []string
	RuleId        string
}

func (s MaintenanceScope) Matches(t MaintenanceTarget) bool {
	if len(s.ApplicationPatterns) > 0 {
		if t.ApplicationId.IsZero() || !utils.GlobMatch(t.ApplicationId.StringWithoutClusterId(), s.ApplicationPatterns...) {
			return false
		}
	}
	if len(s.Categories) > 0 {
		found := false
		for _, c := range s.Categories {
			if c == string(t.Category) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(s.NodePatterns) > 0 {
		found := false
		for _, n := range t.Nodes {
			if utils.GlobMatch(n, s.NodePatterns...) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(s.AlertingRuleIds) > 0 {
		found := false
		for _, id := range s.AlertingRuleIds {
			if id == t.RuleId {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (w *MaintenanceWindow) Migrate(m *Migrator) error {
	err := m.Exec(`
	CREATE TABLE IF NOT EXISTS maintenance_window (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL REFERENCES project(id),
		name TEXT NOT NULL,
		starts_at INT NOT NULL,
		ends_at INT NOT NULL DEFAULT 0,
		recurrence TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT '',
		comment TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL DEFAULT '',
		created_by_kind TEXT NOT NULL DEFAULT '',
		created_at INT NOT NULL,
		ended_at INT NOT NULL DEFAULT 0,
		ended_by TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	if err = m.Exec(`CREATE INDEX IF NOT EXISTS maintenance_window_project ON maintenance_window (project_id, ended_at)`); err != nil {
		return err
	}
	return m.Exec(`
	CREATE TABLE IF NOT EXISTS maintenance_mark (
		project_id TEXT NOT NULL REFERENCES project(id),
		target_type TEXT NOT NULL,
		target_id TEXT NOT NULL,
		window_id INT NOT NULL,
		window_name TEXT NOT NULL DEFAULT '',
		marked_at INT NOT NULL,
		notified INT NOT NULL DEFAULT 0,
		PRIMARY KEY (project_id, target_type, target_id)
	)`)
}

const maintenanceWindowColumns = "id, name, starts_at, ends_at, recurrence, scope, comment, created_by, created_by_kind, created_at, ended_at, ended_by"

func scanMaintenanceWindow(projectId ProjectId, scan func(dest ...any) error) (*MaintenanceWindow, error) {
	w := &MaintenanceWindow{ProjectId: projectId}
	var recurrence, scope string
	if err := scan(&w.Id, &w.Name, &w.StartsAt, &w.EndsAt, &recurrence, &scope, &w.Comment, &w.CreatedBy, &w.CreatedByKind, &w.CreatedAt, &w.EndedAt, &w.EndedBy); err != nil {
		return nil, err
	}
	if recurrence != "" {
		if err := unmarshal(recurrence, &w.Recurrence); err != nil {
			return nil, err
		}
	}
	if scope != "" {
		if err := json.Unmarshal([]byte(scope), &w.Scope); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func maintenanceWindowJSON(w *MaintenanceWindow) (string, string, error) {
	var recurrence string
	if w.Recurrence != nil {
		d, err := json.Marshal(w.Recurrence)
		if err != nil {
			return "", "", err
		}
		recurrence = string(d)
	}
	scope, err := json.Marshal(w.Scope)
	return recurrence, string(scope), err
}

func (db *DB) CreateMaintenanceWindow(w *MaintenanceWindow) error {
	if err := w.Validate(); err != nil {
		return err
	}
	if w.CreatedAt == 0 {
		w.CreatedAt = timeseries.Now()
	}
	recurrence, scope, err := maintenanceWindowJSON(w)
	if err != nil {
		return err
	}
	return db.db.QueryRow(
		"INSERT INTO maintenance_window (project_id, name, starts_at, ends_at, recurrence, scope, comment, created_by, created_by_kind, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id",
		w.ProjectId, w.Name, w.StartsAt, w.EndsAt, recurrence, scope, w.Comment, w.CreatedBy, w.CreatedByKind, w.CreatedAt,
	).Scan(&w.Id)
}

func (db *DB) UpdateMaintenanceWindow(w *MaintenanceWindow) error {
	if err := w.Validate(); err != nil {
		return err
	}
	recurrence, scope, err := maintenanceWindowJSON(w)
	if err != nil {
		return err
	}
	res, err := db.db.Exec(
		"UPDATE maintenance_window SET name = $1, starts_at = $2, ends_at = $3, recurrence = $4, scope = $5, comment = $6 WHERE project_id = $7 AND id = $8",
		w.Name, w.StartsAt, w.EndsAt, recurrence, scope, w.Comment, w.ProjectId, w.Id)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

func (db *DB) GetMaintenanceWindow(projectId ProjectId, id int) (*MaintenanceWindow, error) {
	row := db.db.QueryRow("SELECT "+maintenanceWindowColumns+" FROM maintenance_window WHERE project_id = $1 AND id = $2", projectId, id)
	w, err := scanMaintenanceWindow(projectId, row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}

// GetMaintenanceWindows returns the windows of a project, newest first. With includeEnded=false
// manually ended windows are skipped (expired ones are still returned, the caller filters by status).
func (db *DB) GetMaintenanceWindows(projectId ProjectId, includeEnded bool) ([]*MaintenanceWindow, error) {
	q := "SELECT " + maintenanceWindowColumns + " FROM maintenance_window WHERE project_id = $1"
	if !includeEnded {
		q += " AND ended_at = 0"
	}
	rows, err := db.db.Query(q+" ORDER BY created_at DESC, id DESC", projectId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*MaintenanceWindow
	for rows.Next() {
		w, err := scanMaintenanceWindow(projectId, rows.Scan)
		if err != nil {
			return nil, err
		}
		res = append(res, w)
	}
	return res, rows.Err()
}

// GetActiveMaintenanceWindows returns the windows active at now.
func (db *DB) GetActiveMaintenanceWindows(projectId ProjectId, now timeseries.Time) ([]*MaintenanceWindow, error) {
	all, err := db.GetMaintenanceWindows(projectId, false)
	if err != nil {
		return nil, err
	}
	var res []*MaintenanceWindow
	for _, w := range all {
		if w.IsActive(now) {
			res = append(res, w)
		}
	}
	return res, nil
}

func (db *DB) EndMaintenanceWindow(projectId ProjectId, id int, by string, at timeseries.Time) error {
	res, err := db.db.Exec("UPDATE maintenance_window SET ended_at = $1, ended_by = $2 WHERE project_id = $3 AND id = $4 AND ended_at = 0", at, by, projectId, id)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

func (db *DB) DeleteMaintenanceWindow(projectId ProjectId, id int) error {
	res, err := db.db.Exec("DELETE FROM maintenance_window WHERE project_id = $1 AND id = $2", projectId, id)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

// MaintenanceMark records that the notifications of an alert or incident were muted by a window.
type MaintenanceMark struct {
	TargetType CommentTargetType `json:"target_type"`
	TargetId   string            `json:"target_id"`
	WindowId   int               `json:"window_id"`
	WindowName string            `json:"window_name"`
	MarkedAt   timeseries.Time   `json:"marked_at"`
	// Notified: 0 = muted, still pending (notify if it is still firing when the window ends),
	// 1 = notified after the window ended, -1 = resolved while muted (never notified).
	Notified int `json:"notified"`
}

const (
	MaintenanceMarkPending  = 0
	MaintenanceMarkNotified = 1
	MaintenanceMarkSkipped  = -1
)

// AddMaintenanceMark marks the target; returns false if it was already marked.
func (db *DB) AddMaintenanceMark(projectId ProjectId, m *MaintenanceMark) (bool, error) {
	if m.MarkedAt == 0 {
		m.MarkedAt = timeseries.Now()
	}
	res, err := db.db.Exec(
		"INSERT INTO maintenance_mark (project_id, target_type, target_id, window_id, window_name, marked_at, notified) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (project_id, target_type, target_id) DO NOTHING",
		projectId, m.TargetType, m.TargetId, m.WindowId, m.WindowName, m.MarkedAt, m.Notified)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (db *DB) GetMaintenanceMark(projectId ProjectId, targetType CommentTargetType, targetId string) (*MaintenanceMark, error) {
	m := &MaintenanceMark{}
	err := db.db.QueryRow(
		"SELECT target_type, target_id, window_id, window_name, marked_at, notified FROM maintenance_mark WHERE project_id = $1 AND target_type = $2 AND target_id = $3",
		projectId, targetType, targetId).Scan(&m.TargetType, &m.TargetId, &m.WindowId, &m.WindowName, &m.MarkedAt, &m.Notified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// GetMaintenanceMarks returns the marks of the project's targets of the given type (pendingOnly: Notified == 0).
func (db *DB) GetMaintenanceMarks(projectId ProjectId, targetType CommentTargetType, pendingOnly bool) (map[string]*MaintenanceMark, error) {
	q := "SELECT target_type, target_id, window_id, window_name, marked_at, notified FROM maintenance_mark WHERE project_id = $1 AND target_type = $2"
	if pendingOnly {
		q += " AND notified = 0"
	}
	rows, err := db.db.Query(q, projectId, targetType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]*MaintenanceMark{}
	for rows.Next() {
		m := &MaintenanceMark{}
		if err := rows.Scan(&m.TargetType, &m.TargetId, &m.WindowId, &m.WindowName, &m.MarkedAt, &m.Notified); err != nil {
			return nil, err
		}
		res[m.TargetId] = m
	}
	return res, rows.Err()
}

func (db *DB) SetMaintenanceMarkNotified(projectId ProjectId, targetType CommentTargetType, targetId string, notified int) error {
	_, err := db.db.Exec("UPDATE maintenance_mark SET notified = $1 WHERE project_id = $2 AND target_type = $3 AND target_id = $4",
		notified, projectId, targetType, targetId)
	return err
}

package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/coroot/coroot/timeseries"
)

// CommentTargetType is the kind of object a timeline entry is attached to.
type CommentTargetType string

const (
	CommentTargetIncident     CommentTargetType = "incident"
	CommentTargetAlert        CommentTargetType = "alert"
	CommentTargetAlertingRule CommentTargetType = "alerting_rule"
	// shards fork
	CommentTargetMaintenanceWindow CommentTargetType = "maintenance_window"
)

func (t CommentTargetType) Valid() bool {
	switch t {
	case CommentTargetIncident, CommentTargetAlert, CommentTargetAlertingRule, CommentTargetMaintenanceWindow:
		return true
	}
	return false
}

// CommentAuthorKind tells humans, operator agents (API keys / MCP) and the server itself apart.
type CommentAuthorKind string

const (
	CommentAuthorUser   CommentAuthorKind = "user"
	CommentAuthorAgent  CommentAuthorKind = "agent"
	CommentAuthorSystem CommentAuthorKind = "system"
)

// CommentKind distinguishes free-form comments from automatically recorded actions.
type CommentKind string

const (
	CommentKindComment CommentKind = "comment"
	CommentKindAction  CommentKind = "action"
)

const CommentMaxBodyLength = 64 * 1024

// Comment is an entry in the activity timeline of an incident, alert or alerting rule.
type Comment struct {
	Id         int               `json:"id"`
	ProjectId  ProjectId         `json:"project_id"`
	TargetType CommentTargetType `json:"target_type"`
	TargetId   string            `json:"target_id"`
	Author     string            `json:"author"`
	AuthorId   int               `json:"author_id,omitempty"`
	AuthorKind CommentAuthorKind `json:"author_kind"`
	Kind       CommentKind       `json:"kind"`
	Body       string            `json:"body"`
	CreatedAt  timeseries.Time   `json:"created_at"`
	EditedAt   timeseries.Time   `json:"edited_at"`
	Meta       map[string]string `json:"meta,omitempty"`

	// Editable is computed per request (author or admin); it is not stored.
	Editable bool `json:"editable"`
}

func (c *Comment) Migrate(m *Migrator) error {
	err := m.Exec(`
	CREATE TABLE IF NOT EXISTS comment (
		id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL REFERENCES project(id),
		target_type TEXT NOT NULL,
		target_id TEXT NOT NULL,
		author TEXT NOT NULL,
		author_id INT NOT NULL DEFAULT 0,
		author_kind TEXT NOT NULL,
		kind TEXT NOT NULL,
		body TEXT NOT NULL,
		created_at INT NOT NULL,
		edited_at INT NOT NULL DEFAULT 0,
		meta TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	return m.Exec(`CREATE INDEX IF NOT EXISTS comment_project_target ON comment (project_id, target_type, target_id)`)
}

func (db *DB) AddComment(c *Comment) error {
	if !c.TargetType.Valid() {
		return fmt.Errorf("%w: unknown target type %q", ErrInvalid, c.TargetType)
	}
	if c.TargetId == "" {
		return fmt.Errorf("%w: target id is required", ErrInvalid)
	}
	if c.Kind == "" {
		c.Kind = CommentKindComment
	}
	if c.AuthorKind == "" {
		c.AuthorKind = CommentAuthorUser
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = timeseries.Now()
	}
	meta, err := marshalCommentMeta(c.Meta)
	if err != nil {
		return err
	}
	return db.db.QueryRow(
		"INSERT INTO comment (project_id, target_type, target_id, author, author_id, author_kind, kind, body, created_at, edited_at, meta) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id",
		c.ProjectId, c.TargetType, c.TargetId, c.Author, c.AuthorId, c.AuthorKind, c.Kind, c.Body, c.CreatedAt, c.EditedAt, meta,
	).Scan(&c.Id)
}

const commentColumns = "id, project_id, target_type, target_id, author, author_id, author_kind, kind, body, created_at, edited_at, meta"

// GetComments returns the timeline of a target, oldest first.
func (db *DB) GetComments(projectId ProjectId, targetType CommentTargetType, targetId string) ([]*Comment, error) {
	rows, err := db.db.Query(
		"SELECT "+commentColumns+" FROM comment WHERE project_id = $1 AND target_type = $2 AND target_id = $3 ORDER BY created_at, id",
		projectId, targetType, targetId)
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

func (db *DB) GetComment(projectId ProjectId, id int) (*Comment, error) {
	rows, err := db.db.Query("SELECT "+commentColumns+" FROM comment WHERE project_id = $1 AND id = $2", projectId, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	return scanComment(rows)
}

func (db *DB) UpdateCommentBody(projectId ProjectId, id int, body string) error {
	res, err := db.db.Exec("UPDATE comment SET body = $1, edited_at = $2 WHERE project_id = $3 AND id = $4", body, timeseries.Now(), projectId, id)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

func (db *DB) DeleteComment(projectId ProjectId, id int) error {
	res, err := db.db.Exec("DELETE FROM comment WHERE project_id = $1 AND id = $2", projectId, id)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

func affectedOrNotFound(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanComment(rows *sql.Rows) (*Comment, error) {
	var c Comment
	var meta string
	err := rows.Scan(&c.Id, &c.ProjectId, &c.TargetType, &c.TargetId, &c.Author, &c.AuthorId, &c.AuthorKind, &c.Kind, &c.Body, &c.CreatedAt, &c.EditedAt, &meta)
	if err != nil {
		return nil, err
	}
	if meta != "" {
		if err = json.Unmarshal([]byte(meta), &c.Meta); err != nil {
			return nil, errors.Join(fmt.Errorf("invalid comment meta (id=%d)", c.Id), err)
		}
	}
	return &c, nil
}

func marshalCommentMeta(meta map[string]string) (string, error) {
	if len(meta) == 0 {
		return "", nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

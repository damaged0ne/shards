package db

// shards fork: operator-agent registry (identities with scoped API keys), the audit log of agent
// tool calls, agent sessions, outbound dispatch deliveries and agent playbooks.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// AgentScope is a preset that caps what an agent may do, on top of the owner's role.
type AgentScope string

const (
	// AgentScopeRead: query only (list/get/traces/logs/metrics).
	AgentScopeRead AgentScope = "read"
	// AgentScopeTriage: read + comments and acknowledge-type actions.
	AgentScopeTriage AgentScope = "triage"
	// AgentScopeOperator: triage + alert resolve/suppress/reopen and alerting rule changes.
	AgentScopeOperator AgentScope = "operator"
	// AgentScopeAdmin: everything the owner's role allows.
	AgentScopeAdmin AgentScope = "admin"
)

var agentScopeLevels = map[AgentScope]int{AgentScopeRead: 1, AgentScopeTriage: 2, AgentScopeOperator: 3, AgentScopeAdmin: 4}

func (s AgentScope) Valid() bool {
	_, ok := agentScopeLevels[s]
	return ok
}

// Allows reports whether scope s includes the required scope.
func (s AgentScope) Allows(required AgentScope) bool {
	if required == "" {
		required = AgentScopeRead
	}
	return agentScopeLevels[s] >= agentScopeLevels[required] && agentScopeLevels[s] > 0
}

// Agent dispatch event types.
const (
	AgentEventIncidentOpened    = "incident_opened"
	AgentEventIncidentEscalated = "incident_escalated"
	AgentEventAlertFired        = "alert_fired"
	AgentEventMention           = "mention"
	AgentEventApprovalDecided   = "approval_decided"
	AgentEventManual            = "manual"
	AgentEventTest              = "test"
)

var AgentDispatchEvents = []string{AgentEventIncidentOpened, AgentEventIncidentEscalated, AgentEventAlertFired, AgentEventMention, AgentEventApprovalDecided}

// AgentDispatchConfig configures outbound webhooks that wake an agent up.
type AgentDispatchConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Secret  string `json:"secret,omitempty"`
	// Events to dispatch automatically (manual "Ask agent" tasks are always delivered).
	Events []string `json:"events"`
	// MinSeverity for alert_fired / incident events: "warning" (default) or "critical".
	MinSeverity string `json:"min_severity,omitempty"`
	// Categories and AppPatterns ('namespace:Kind:name' globs) narrow automatic events; empty = all.
	Categories  []string `json:"categories,omitempty"`
	AppPatterns []string `json:"app_patterns,omitempty"`
	// RateLimitPerHour caps automatic deliveries; DedupMinutes suppresses repeated events per incident/alert.
	RateLimitPerHour int `json:"rate_limit_per_hour,omitempty"`
	DedupMinutes     int `json:"dedup_minutes,omitempty"`
}

const (
	AgentDispatchDefaultRateLimit = 30
	AgentDispatchDefaultDedup     = 30
)

func (c *AgentDispatchConfig) HasEvent(e string) bool {
	for _, x := range c.Events {
		if x == e {
			return true
		}
	}
	return false
}

type Agent struct {
	Id          int        `json:"id"`
	ProjectId   ProjectId  `json:"project_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Vendor      string     `json:"vendor"`
	Model       string     `json:"model"`
	OwnerId     int        `json:"owner_id"`
	Scope       AgentScope `json:"scope"`
	// ExpiresAt (unix ms, 0 = never): after it the agent's keys stop working.
	ExpiresAt int64 `json:"expires_at"`
	// AllowedProjects restricts the projects the agent may access; empty = every project the owner can access.
	AllowedProjects []ProjectId          `json:"allowed_projects"`
	Dispatch        *AgentDispatchConfig `json:"dispatch,omitempty"`
	Disabled        bool                 `json:"disabled"`
	CreatedAt       int64                `json:"created_at"`
	UpdatedAt       int64                `json:"updated_at"`
}

var agentNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

func (a *Agent) Validate() error {
	if !agentNameRe.MatchString(a.Name) {
		return fmt.Errorf("%w: name must be 1-63 characters: letters, digits, '-', '_', '.' (it is used for @mentions)", ErrInvalid)
	}
	if !a.Scope.Valid() {
		return fmt.Errorf("%w: scope must be one of read, triage, operator, admin", ErrInvalid)
	}
	if d := a.Dispatch; d != nil && d.Enabled {
		if !strings.HasPrefix(d.URL, "http://") && !strings.HasPrefix(d.URL, "https://") {
			return fmt.Errorf("%w: dispatch url must be an http(s) URL", ErrInvalid)
		}
		if d.MinSeverity != "" && d.MinSeverity != "warning" && d.MinSeverity != "critical" {
			return fmt.Errorf("%w: min_severity must be warning or critical", ErrInvalid)
		}
	}
	return nil
}

func (a *Agent) Expired(now time.Time) bool {
	return a.ExpiresAt > 0 && now.UnixMilli() >= a.ExpiresAt
}

// ProjectAllowed reports whether the agent may access the project (the owner's role is checked separately).
func (a *Agent) ProjectAllowed(id ProjectId) bool {
	if len(a.AllowedProjects) == 0 {
		return true
	}
	for _, p := range a.AllowedProjects {
		if p == id {
			return true
		}
	}
	return false
}

// AgentActivity is one audited agent call (an MCP tool call or a REST write).
type AgentActivity struct {
	Id         int64     `json:"id"`
	AgentId    int       `json:"agent_id"`
	ProjectId  ProjectId `json:"project_id"`
	Time       int64     `json:"time"`
	Channel    string    `json:"channel"` // mcp | rest
	SessionId  string    `json:"session_id,omitempty"`
	Tool       string    `json:"tool"`
	Args       string    `json:"args,omitempty"`
	Status     string    `json:"status"` // ok | error | denied
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	TargetType string    `json:"target_type,omitempty"`
	TargetId   string    `json:"target_id,omitempty"`
}

type AgentSession struct {
	AgentId       int    `json:"agent_id"`
	SessionId     string `json:"session_id"`
	ClientName    string `json:"client_name,omitempty"`
	ClientVersion string `json:"client_version,omitempty"`
	StartedAt     int64  `json:"started_at"`
	LastSeen      int64  `json:"last_seen"`
	Calls         int    `json:"calls"`
}

type AgentStats struct {
	LastSeen       int64 `json:"last_seen"`
	CallsLastHour  int   `json:"calls_last_hour"`
	ErrorsLastHour int   `json:"errors_last_hour"`
	DeniedLastHour int   `json:"denied_last_hour"`
}

// Dispatch delivery statuses.
const (
	AgentDeliveryPending   = "pending"
	AgentDeliveryDelivered = "delivered"
	AgentDeliveryFailed    = "failed"
	// AgentDeliveryNew is a row being created (its payload isn't written yet); the worker ignores it.
	AgentDeliveryNew = "new"
)

type AgentDelivery struct {
	Id            int64     `json:"id"`
	AgentId       int       `json:"agent_id"`
	ProjectId     ProjectId `json:"project_id"`
	Event         string    `json:"event"`
	DedupKey      string    `json:"dedup_key,omitempty"`
	Payload       string    `json:"payload"`
	Status        string    `json:"status"`
	Attempts      int       `json:"attempts"`
	ResponseCode  int       `json:"response_code,omitempty"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     int64     `json:"created_at"`
	NextAttemptAt int64     `json:"next_attempt_at,omitempty"`
	DeliveredAt   int64     `json:"delivered_at,omitempty"`
}

// Playbook target types.
const (
	PlaybookTargetAlertingRule = "alerting_rule"
	PlaybookTargetApplication  = "application"
)

const PlaybookMaxLength = 64 * 1024

type Playbook struct {
	ProjectId  ProjectId `json:"project_id"`
	TargetType string    `json:"target_type"`
	TargetId   string    `json:"target_id"`
	Body       string    `json:"body"`
	UpdatedAt  int64     `json:"updated_at"`
	UpdatedBy  string    `json:"updated_by"`
}

// AgentTables migrates the fork's agent tables.
type AgentTables struct{}

func (AgentTables) Migrate(m *Migrator) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS agent (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			vendor TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			owner_id INT NOT NULL DEFAULT 0,
			scope TEXT NOT NULL,
			expires_at BIGINT NOT NULL DEFAULT 0,
			allowed_projects TEXT NOT NULL DEFAULT '',
			dispatch TEXT NOT NULL DEFAULT '',
			disabled INT NOT NULL DEFAULT 0,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS agent_project_name ON agent (project_id, name)`,
		`CREATE TABLE IF NOT EXISTS agent_api_key (
			key_id INT NOT NULL PRIMARY KEY,
			agent_id INT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS agent_activity (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			agent_id INT NOT NULL,
			project_id TEXT NOT NULL DEFAULT '',
			ts BIGINT NOT NULL,
			channel TEXT NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			tool TEXT NOT NULL,
			args TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			duration_ms BIGINT NOT NULL DEFAULT 0,
			target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS agent_activity_agent_ts ON agent_activity (agent_id, ts)`,
		`CREATE TABLE IF NOT EXISTS agent_session (
			agent_id INT NOT NULL,
			session_id TEXT NOT NULL,
			client_name TEXT NOT NULL DEFAULT '',
			client_version TEXT NOT NULL DEFAULT '',
			started_at BIGINT NOT NULL,
			last_seen BIGINT NOT NULL,
			calls INT NOT NULL DEFAULT 0,
			PRIMARY KEY (agent_id, session_id)
		)`,
		`CREATE TABLE IF NOT EXISTS agent_dispatch (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			agent_id INT NOT NULL,
			project_id TEXT NOT NULL,
			event TEXT NOT NULL,
			dedup_key TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL,
			status TEXT NOT NULL,
			attempts INT NOT NULL DEFAULT 0,
			response_code INT NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL,
			next_attempt_at BIGINT NOT NULL DEFAULT 0,
			delivered_at BIGINT NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS agent_dispatch_agent ON agent_dispatch (agent_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS agent_dispatch_status ON agent_dispatch (status, next_attempt_at)`,
		`CREATE TABLE IF NOT EXISTS playbook (
			project_id TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			body TEXT NOT NULL,
			updated_at BIGINT NOT NULL,
			updated_by TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (project_id, target_type, target_id)
		)`,
	} {
		if err := m.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

const agentColumns = "id, project_id, name, description, vendor, model, owner_id, scope, expires_at, allowed_projects, dispatch, disabled, created_at, updated_at"

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAgent(r rowScanner) (*Agent, error) {
	var a Agent
	var allowed, dispatch string
	var disabled int
	if err := r.Scan(&a.Id, &a.ProjectId, &a.Name, &a.Description, &a.Vendor, &a.Model, &a.OwnerId, &a.Scope, &a.ExpiresAt, &allowed, &dispatch, &disabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.Disabled = disabled != 0
	if allowed != "" {
		if err := json.Unmarshal([]byte(allowed), &a.AllowedProjects); err != nil {
			return nil, err
		}
	}
	if dispatch != "" {
		a.Dispatch = &AgentDispatchConfig{}
		if err := json.Unmarshal([]byte(dispatch), a.Dispatch); err != nil {
			return nil, err
		}
	}
	return &a, nil
}

func marshalAgentFields(a *Agent) (string, string, int, error) {
	allowed, dispatch := "", ""
	if len(a.AllowedProjects) > 0 {
		data, err := json.Marshal(a.AllowedProjects)
		if err != nil {
			return "", "", 0, err
		}
		allowed = string(data)
	}
	if a.Dispatch != nil {
		data, err := json.Marshal(a.Dispatch)
		if err != nil {
			return "", "", 0, err
		}
		dispatch = string(data)
	}
	disabled := 0
	if a.Disabled {
		disabled = 1
	}
	return allowed, dispatch, disabled, nil
}

func (db *DB) CreateAgent(a *Agent) error {
	if err := a.Validate(); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	a.CreatedAt, a.UpdatedAt = now, now
	allowed, dispatch, disabled, err := marshalAgentFields(a)
	if err != nil {
		return err
	}
	err = db.db.QueryRow(
		"INSERT INTO agent (project_id, name, description, vendor, model, owner_id, scope, expires_at, allowed_projects, dispatch, disabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id",
		a.ProjectId, a.Name, a.Description, a.Vendor, a.Model, a.OwnerId, a.Scope, a.ExpiresAt, allowed, dispatch, disabled, a.CreatedAt, a.UpdatedAt,
	).Scan(&a.Id)
	if db.IsUniqueViolationError(err) {
		return ErrConflict
	}
	return err
}

func (db *DB) UpdateAgent(a *Agent) error {
	if err := a.Validate(); err != nil {
		return err
	}
	a.UpdatedAt = time.Now().UnixMilli()
	allowed, dispatch, disabled, err := marshalAgentFields(a)
	if err != nil {
		return err
	}
	res, err := db.db.Exec(
		"UPDATE agent SET name = $1, description = $2, vendor = $3, model = $4, owner_id = $5, scope = $6, expires_at = $7, allowed_projects = $8, dispatch = $9, disabled = $10, updated_at = $11 WHERE project_id = $12 AND id = $13",
		a.Name, a.Description, a.Vendor, a.Model, a.OwnerId, a.Scope, a.ExpiresAt, allowed, dispatch, disabled, a.UpdatedAt, a.ProjectId, a.Id,
	)
	if db.IsUniqueViolationError(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

// DeleteAgent removes the agent with its key links, sessions and pending deliveries.
// The audit log is kept until it expires (it may still be needed to investigate what the agent did).
func (db *DB) DeleteAgent(projectId ProjectId, id int) error {
	res, err := db.db.Exec("DELETE FROM agent WHERE project_id = $1 AND id = $2", projectId, id)
	if err != nil {
		return err
	}
	if err = affectedOrNotFound(res); err != nil {
		return err
	}
	for _, q := range []string{
		"DELETE FROM agent_api_key WHERE agent_id = $1",
		"DELETE FROM agent_session WHERE agent_id = $1",
		"DELETE FROM agent_dispatch WHERE agent_id = $1",
	} {
		if _, err = db.db.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) GetAgents(projectId ProjectId) ([]*Agent, error) {
	rows, err := db.db.Query("SELECT "+agentColumns+" FROM agent WHERE project_id = $1 ORDER BY name", projectId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []*Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, a)
	}
	return res, rows.Err()
}

func (db *DB) GetAgent(projectId ProjectId, id int) (*Agent, error) {
	a, err := scanAgent(db.db.QueryRow("SELECT "+agentColumns+" FROM agent WHERE project_id = $1 AND id = $2", projectId, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (db *DB) GetAgentByName(projectId ProjectId, name string) (*Agent, error) {
	a, err := scanAgent(db.db.QueryRow("SELECT "+agentColumns+" FROM agent WHERE project_id = $1 AND LOWER(name) = LOWER($2)", projectId, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// GetAgentByApiKeyId returns the agent a user API key is linked to, or ErrNotFound for an unscoped key.
func (db *DB) GetAgentByApiKeyId(keyId int) (*Agent, error) {
	a, err := scanAgent(db.db.QueryRow("SELECT "+prefixColumns("a.", agentColumns)+" FROM agent a JOIN agent_api_key k ON k.agent_id = a.id WHERE k.key_id = $1", keyId))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func prefixColumns(prefix, columns string) string {
	parts := strings.Split(columns, ", ")
	for i := range parts {
		parts[i] = prefix + parts[i]
	}
	return strings.Join(parts, ", ")
}

// AgentApiKey is a user API key linked to an agent.
type AgentApiKey struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id"`
	Description string `json:"description"`
}

func (db *DB) GetAgentApiKeys(agentId int) ([]AgentApiKey, error) {
	rows, err := db.db.Query("SELECT u.id, u.user_id, u.description FROM user_api_keys u JOIN agent_api_key k ON k.key_id = u.id WHERE k.agent_id = $1 ORDER BY u.id", agentId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []AgentApiKey{}
	for rows.Next() {
		var k AgentApiKey
		if err = rows.Scan(&k.Id, &k.UserId, &k.Description); err != nil {
			return nil, err
		}
		res = append(res, k)
	}
	return res, rows.Err()
}

// LinkAgentApiKey links a user API key to an agent (a key belongs to at most one agent).
func (db *DB) LinkAgentApiKey(agentId, keyId int) error {
	if _, err := db.db.Exec("DELETE FROM agent_api_key WHERE key_id = $1", keyId); err != nil {
		return err
	}
	_, err := db.db.Exec("INSERT INTO agent_api_key (key_id, agent_id) VALUES ($1, $2)", keyId, agentId)
	return err
}

func (db *DB) UnlinkAgentApiKey(agentId, keyId int) error {
	_, err := db.db.Exec("DELETE FROM agent_api_key WHERE agent_id = $1 AND key_id = $2", agentId, keyId)
	return err
}

// CreateAgentApiKey stores a new user API key for the owner and links it to the agent.
func (db *DB) CreateAgentApiKey(agentId, userId int, key, description string) (int, error) {
	if err := db.AddUserApiKey(userId, key, description); err != nil {
		return 0, err
	}
	var id int
	if err := db.db.QueryRow("SELECT id FROM user_api_keys WHERE hash = $1", hashApiKey(key)).Scan(&id); err != nil {
		return 0, err
	}
	return id, db.LinkAgentApiKey(agentId, id)
}

func (db *DB) AddAgentActivity(a *AgentActivity) error {
	return db.db.QueryRow(
		"INSERT INTO agent_activity (agent_id, project_id, ts, channel, session_id, tool, args, status, error, duration_ms, target_type, target_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id",
		a.AgentId, a.ProjectId, a.Time, a.Channel, a.SessionId, a.Tool, a.Args, a.Status, a.Error, a.DurationMs, a.TargetType, a.TargetId,
	).Scan(&a.Id)
}

// GetAgentActivity returns the newest audit entries of an agent (before = id cursor, 0 = newest).
func (db *DB) GetAgentActivity(agentId int, before int64, limit int) ([]*AgentActivity, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := "SELECT id, agent_id, project_id, ts, channel, session_id, tool, args, status, error, duration_ms, target_type, target_id FROM agent_activity WHERE agent_id = $1"
	args := []any{agentId}
	if before > 0 {
		q += " AND id < $2"
		args = append(args, before)
	}
	q += fmt.Sprintf(" ORDER BY id DESC LIMIT %d", limit)
	rows, err := db.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []*AgentActivity{}
	for rows.Next() {
		var a AgentActivity
		if err = rows.Scan(&a.Id, &a.AgentId, &a.ProjectId, &a.Time, &a.Channel, &a.SessionId, &a.Tool, &a.Args, &a.Status, &a.Error, &a.DurationMs, &a.TargetType, &a.TargetId); err != nil {
			return nil, err
		}
		res = append(res, &a)
	}
	return res, rows.Err()
}

// GetAgentStats aggregates the audit log since `since` (unix ms) per agent.
func (db *DB) GetAgentStats(agentIds []int, since int64) (map[int]*AgentStats, error) {
	res := map[int]*AgentStats{}
	if len(agentIds) == 0 {
		return res, nil
	}
	ids := make([]string, 0, len(agentIds))
	for _, id := range agentIds {
		ids = append(ids, fmt.Sprint(id))
		res[id] = &AgentStats{}
	}
	in := strings.Join(ids, ",")
	rows, err := db.db.Query("SELECT agent_id, status, COUNT(*) FROM agent_activity WHERE agent_id IN ("+in+") AND ts >= $1 GROUP BY agent_id, status", since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n int
		var status string
		if err = rows.Scan(&id, &status, &n); err != nil {
			return nil, err
		}
		s := res[id]
		s.CallsLastHour += n
		switch status {
		case "error":
			s.ErrorsLastHour += n
		case "denied":
			s.DeniedLastHour += n
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows2, err := db.db.Query("SELECT agent_id, MAX(last_seen) FROM agent_session WHERE agent_id IN (" + in + ") GROUP BY agent_id")
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var id int
		var ls sql.NullInt64
		if err = rows2.Scan(&id, &ls); err != nil {
			return nil, err
		}
		res[id].LastSeen = ls.Int64
	}
	return res, rows2.Err()
}

// TouchAgentSession records a call in an agent session (creating the session on first use).
func (db *DB) TouchAgentSession(agentId int, sessionId, clientName, clientVersion string, now int64) error {
	_, err := db.db.Exec(`
		INSERT INTO agent_session (agent_id, session_id, client_name, client_version, started_at, last_seen, calls) VALUES ($1, $2, $3, $4, $5, $5, 1)
		ON CONFLICT (agent_id, session_id) DO UPDATE SET last_seen = excluded.last_seen, calls = agent_session.calls + 1,
			client_name = CASE WHEN excluded.client_name <> '' THEN excluded.client_name ELSE agent_session.client_name END,
			client_version = CASE WHEN excluded.client_version <> '' THEN excluded.client_version ELSE agent_session.client_version END`,
		agentId, sessionId, clientName, clientVersion, now)
	return err
}

func (db *DB) GetAgentSessions(agentId int, limit int) ([]*AgentSession, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.db.Query(fmt.Sprintf("SELECT agent_id, session_id, client_name, client_version, started_at, last_seen, calls FROM agent_session WHERE agent_id = $1 ORDER BY last_seen DESC LIMIT %d", limit), agentId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []*AgentSession{}
	for rows.Next() {
		var s AgentSession
		if err = rows.Scan(&s.AgentId, &s.SessionId, &s.ClientName, &s.ClientVersion, &s.StartedAt, &s.LastSeen, &s.Calls); err != nil {
			return nil, err
		}
		res = append(res, &s)
	}
	return res, rows.Err()
}

// PruneAgentData removes audit entries, sessions and deliveries older than `before` (unix ms).
func (db *DB) PruneAgentData(before int64) error {
	for _, q := range []string{
		"DELETE FROM agent_activity WHERE ts < $1",
		"DELETE FROM agent_session WHERE last_seen < $1",
		"DELETE FROM agent_dispatch WHERE created_at < $1 AND status <> 'pending'",
	} {
		if _, err := db.db.Exec(q, before); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) AddAgentDelivery(d *AgentDelivery) error {
	return db.db.QueryRow(
		"INSERT INTO agent_dispatch (agent_id, project_id, event, dedup_key, payload, status, attempts, response_code, error, created_at, next_attempt_at, delivered_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id",
		d.AgentId, d.ProjectId, d.Event, d.DedupKey, d.Payload, d.Status, d.Attempts, d.ResponseCode, d.Error, d.CreatedAt, d.NextAttemptAt, d.DeliveredAt,
	).Scan(&d.Id)
}

// UpdateAgentDeliveryPayload sets the payload of a delivery inserted as AgentDeliveryNew and makes it
// visible to the delivery worker (status/next attempt).
func (db *DB) UpdateAgentDeliveryPayload(d *AgentDelivery) error {
	_, err := db.db.Exec("UPDATE agent_dispatch SET payload = $1, status = $2, next_attempt_at = $3 WHERE id = $4", d.Payload, d.Status, d.NextAttemptAt, d.Id)
	return err
}

func (db *DB) UpdateAgentDelivery(d *AgentDelivery) error {
	_, err := db.db.Exec(
		"UPDATE agent_dispatch SET status = $1, attempts = $2, response_code = $3, error = $4, next_attempt_at = $5, delivered_at = $6 WHERE id = $7",
		d.Status, d.Attempts, d.ResponseCode, d.Error, d.NextAttemptAt, d.DeliveredAt, d.Id)
	return err
}

const agentDeliveryColumns = "id, agent_id, project_id, event, dedup_key, payload, status, attempts, response_code, error, created_at, next_attempt_at, delivered_at"

func (db *DB) queryAgentDeliveries(q string, args ...any) ([]*AgentDelivery, error) {
	rows, err := db.db.Query("SELECT "+agentDeliveryColumns+" FROM agent_dispatch "+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []*AgentDelivery{}
	for rows.Next() {
		var d AgentDelivery
		if err = rows.Scan(&d.Id, &d.AgentId, &d.ProjectId, &d.Event, &d.DedupKey, &d.Payload, &d.Status, &d.Attempts, &d.ResponseCode, &d.Error, &d.CreatedAt, &d.NextAttemptAt, &d.DeliveredAt); err != nil {
			return nil, err
		}
		res = append(res, &d)
	}
	return res, rows.Err()
}

func (db *DB) GetAgentDeliveries(agentId int, limit int) ([]*AgentDelivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return db.queryAgentDeliveries(fmt.Sprintf("WHERE agent_id = $1 ORDER BY id DESC LIMIT %d", limit), agentId)
}

// GetDueAgentDeliveries returns pending deliveries whose next attempt is due.
func (db *DB) GetDueAgentDeliveries(now int64, limit int) ([]*AgentDelivery, error) {
	return db.queryAgentDeliveries(fmt.Sprintf("WHERE status = 'pending' AND next_attempt_at <= $1 ORDER BY next_attempt_at LIMIT %d", limit), now)
}

// CountAgentDeliveries counts automatic deliveries (manual/test excluded) since `since`, optionally
// with a given dedup key.
func (db *DB) CountAgentDeliveries(agentId int, since int64, dedupKey string) (int, error) {
	q := "SELECT COUNT(*) FROM agent_dispatch WHERE agent_id = $1 AND created_at >= $2 AND event NOT IN ('manual', 'test') AND status <> 'skipped'"
	args := []any{agentId, since}
	if dedupKey != "" {
		q += " AND dedup_key = $3"
		args = append(args, dedupKey)
	}
	var n int
	err := db.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

func (db *DB) GetPlaybook(projectId ProjectId, targetType, targetId string) (*Playbook, error) {
	var p Playbook
	err := db.db.QueryRow("SELECT project_id, target_type, target_id, body, updated_at, updated_by FROM playbook WHERE project_id = $1 AND target_type = $2 AND target_id = $3",
		projectId, targetType, targetId).Scan(&p.ProjectId, &p.TargetType, &p.TargetId, &p.Body, &p.UpdatedAt, &p.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// GetPlaybooks returns all playbooks of a target type in a project, keyed by target id.
func (db *DB) GetPlaybooks(projectId ProjectId, targetType string) (map[string]*Playbook, error) {
	rows, err := db.db.Query("SELECT project_id, target_type, target_id, body, updated_at, updated_by FROM playbook WHERE project_id = $1 AND target_type = $2", projectId, targetType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]*Playbook{}
	for rows.Next() {
		var p Playbook
		if err = rows.Scan(&p.ProjectId, &p.TargetType, &p.TargetId, &p.Body, &p.UpdatedAt, &p.UpdatedBy); err != nil {
			return nil, err
		}
		res[p.TargetId] = &p
	}
	return res, rows.Err()
}

// SetPlaybook stores a playbook; an empty body deletes it.
func (db *DB) SetPlaybook(p *Playbook) error {
	if p.TargetType != PlaybookTargetAlertingRule && p.TargetType != PlaybookTargetApplication {
		return fmt.Errorf("%w: target_type must be alerting_rule or application", ErrInvalid)
	}
	if p.TargetId == "" {
		return fmt.Errorf("%w: target_id is required", ErrInvalid)
	}
	if len(p.Body) > PlaybookMaxLength {
		return fmt.Errorf("%w: playbook is too long", ErrInvalid)
	}
	if strings.TrimSpace(p.Body) == "" {
		_, err := db.db.Exec("DELETE FROM playbook WHERE project_id = $1 AND target_type = $2 AND target_id = $3", p.ProjectId, p.TargetType, p.TargetId)
		return err
	}
	p.UpdatedAt = time.Now().UnixMilli()
	_, err := db.db.Exec(`
		INSERT INTO playbook (project_id, target_type, target_id, body, updated_at, updated_by) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (project_id, target_type, target_id) DO UPDATE SET body = excluded.body, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		p.ProjectId, p.TargetType, p.TargetId, p.Body, p.UpdatedAt, p.UpdatedBy)
	return err
}

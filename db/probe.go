package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: synthetic probes (HTTP/TCP/TLS/DNS checks run by the server, see the probes package).

type ProbeType string

const (
	ProbeTypeHTTP ProbeType = "http"
	ProbeTypeTCP  ProbeType = "tcp"
	ProbeTypeTLS  ProbeType = "tls"
	ProbeTypeDNS  ProbeType = "dns"

	ProbeDefaultInterval = timeseries.Minute
	ProbeMinInterval     = 10 * timeseries.Second
	ProbeMaxInterval     = timeseries.Hour
	ProbeDefaultTimeout  = 10 * timeseries.Second
	ProbeMaxBodyContains = 1024
)

var (
	probeNameRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	probeStatusRe    = regexp.MustCompile(`^\d{3}(-\d{3})?$`)
	probeDNSRecTypes = map[string]bool{"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true, "NS": true}
)

type ProbeSpec struct {
	Type     ProbeType           `json:"type"`
	Target   string              `json:"target"`
	Interval timeseries.Duration `json:"interval"`
	Timeout  timeseries.Duration `json:"timeout"`
	Paused   bool                `json:"paused,omitempty"`

	// HTTP
	Method          string         `json:"method,omitempty"`
	ExpectedStatus  string         `json:"expected_status,omitempty"` // e.g. "200-399" or "200,204,301-302"
	BodyContains    string         `json:"body_contains,omitempty"`
	Headers         []utils.Header `json:"headers,omitempty"`
	FollowRedirects bool           `json:"follow_redirects"`

	// HTTP and TLS
	TlsSkipVerify bool `json:"tls_skip_verify,omitempty"`

	// DNS
	DNSRecordType string `json:"dns_record_type,omitempty"`
	DNSServer     string `json:"dns_server,omitempty"` // host:port, the system resolver is used if empty

	// ApplicationId links the probe to an application: the results are shown in its Uptime report
	// and the probe checks fire alerts for this application.
	ApplicationId string `json:"application_id,omitempty"`
}

// StatusRanges parses ExpectedStatus ("200-399", "200,204,301-302").
func (s *ProbeSpec) StatusRanges() ([][2]int, error) {
	expr := strings.TrimSpace(s.ExpectedStatus)
	if expr == "" {
		expr = "200-399"
	}
	var res [][2]int
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if !probeStatusRe.MatchString(part) {
			return nil, fmt.Errorf("invalid expected status: %q", part)
		}
		from, to, found := strings.Cut(part, "-")
		lo, _ := strconv.Atoi(from)
		hi := lo
		if found {
			hi, _ = strconv.Atoi(to)
		}
		if lo < 100 || hi > 599 || lo > hi {
			return nil, fmt.Errorf("invalid expected status: %q", part)
		}
		res = append(res, [2]int{lo, hi})
	}
	return res, nil
}

// Normalize fills in the defaults and validates the spec.
func (s *ProbeSpec) Normalize() error {
	s.Target = strings.TrimSpace(s.Target)
	if s.Target == "" {
		return fmt.Errorf("target is required")
	}
	if s.Interval == 0 {
		s.Interval = ProbeDefaultInterval
	}
	if s.Interval < ProbeMinInterval || s.Interval > ProbeMaxInterval {
		return fmt.Errorf("interval must be between %s and %s", ProbeMinInterval, ProbeMaxInterval)
	}
	if s.Timeout == 0 {
		s.Timeout = min(ProbeDefaultTimeout, s.Interval)
	}
	if s.Timeout < timeseries.Second || s.Timeout > s.Interval {
		return fmt.Errorf("timeout must be between 1s and the interval")
	}
	switch s.Type {
	case ProbeTypeHTTP:
		u, err := url.Parse(s.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("target must be an http:// or https:// URL")
		}
		s.Method = strings.ToUpper(strings.TrimSpace(s.Method))
		if s.Method == "" {
			s.Method = "GET"
		}
		switch s.Method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		default:
			return fmt.Errorf("unsupported method: %s", s.Method)
		}
		if _, err = s.StatusRanges(); err != nil {
			return err
		}
		if s.ExpectedStatus == "" {
			s.ExpectedStatus = "200-399"
		}
		if len(s.BodyContains) > ProbeMaxBodyContains {
			return fmt.Errorf("body substring is too long")
		}
		var headers []utils.Header
		for _, h := range s.Headers {
			if h.Key == "" && h.Value == "" {
				continue
			}
			if !h.Valid() {
				return fmt.Errorf("invalid header: %q", h.Key)
			}
			headers = append(headers, h)
		}
		s.Headers = headers
		s.DNSRecordType, s.DNSServer = "", ""
	case ProbeTypeTCP, ProbeTypeTLS:
		host, port, err := net.SplitHostPort(s.Target)
		if err != nil || host == "" || port == "" {
			return fmt.Errorf("target must be in the host:port format")
		}
		s.clearHTTP()
		s.DNSRecordType, s.DNSServer = "", ""
		if s.Type == ProbeTypeTCP {
			s.TlsSkipVerify = false
		}
	case ProbeTypeDNS:
		if strings.ContainsAny(s.Target, "/: ") {
			return fmt.Errorf("target must be a domain name")
		}
		s.DNSRecordType = strings.ToUpper(strings.TrimSpace(s.DNSRecordType))
		if s.DNSRecordType == "" {
			s.DNSRecordType = "A"
		}
		if !probeDNSRecTypes[s.DNSRecordType] {
			return fmt.Errorf("unsupported DNS record type: %s", s.DNSRecordType)
		}
		if s.DNSServer != "" {
			if _, _, err := net.SplitHostPort(s.DNSServer); err != nil {
				s.DNSServer = net.JoinHostPort(s.DNSServer, "53")
			}
		}
		s.clearHTTP()
		s.TlsSkipVerify = false
	default:
		return fmt.Errorf("unknown probe type: %q", s.Type)
	}
	return nil
}

func (s *ProbeSpec) clearHTTP() {
	s.Method, s.ExpectedStatus, s.BodyContains, s.Headers, s.FollowRedirects = "", "", "", nil, false
}

// ProbeState is the result of the latest run, kept in the DB because things like error messages
// can't be stored as metrics. The history is available through the shards_probe_* metrics.
type ProbeState struct {
	LastRunAt           timeseries.Time `json:"last_run_at"`
	Up                  bool            `json:"up"`
	Duration            float32         `json:"duration"` // seconds
	StatusCode          int             `json:"status_code,omitempty"`
	Error               string          `json:"error,omitempty"`
	ConsecutiveFailures int             `json:"consecutive_failures"`
	LastUpAt            timeseries.Time `json:"last_up_at,omitempty"`
	CertNotAfter        timeseries.Time `json:"cert_not_after,omitempty"`
	CertIssuer          string          `json:"cert_issuer,omitempty"`
	CertSubject         string          `json:"cert_subject,omitempty"`
	CertValid           *bool           `json:"cert_valid,omitempty"`
}

type Probe struct {
	Id        string          `json:"id"`
	ProjectId ProjectId       `json:"project_id"`
	Name      string          `json:"name"`
	Spec      ProbeSpec       `json:"spec"`
	State     ProbeState      `json:"state"`
	CreatedAt timeseries.Time `json:"created_at"`
	UpdatedAt timeseries.Time `json:"updated_at"`
}

func ValidateProbeName(name string) error {
	if !probeNameRe.MatchString(name) {
		return fmt.Errorf("the name must start with a letter or digit and contain only letters, digits, '.', '_' and '-' (max 63 characters)")
	}
	return nil
}

func (p *Probe) Migrate(m *Migrator) error {
	err := m.Exec(`
	CREATE TABLE IF NOT EXISTS probe (
		id TEXT NOT NULL PRIMARY KEY,
		project_id TEXT NOT NULL REFERENCES project(id),
		name TEXT NOT NULL,
		spec TEXT NOT NULL,
		state TEXT NOT NULL DEFAULT '',
		created_at INT NOT NULL,
		updated_at INT NOT NULL
	)`)
	if err != nil {
		return err
	}
	return m.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS probe_project_name ON probe (project_id, name)`)
}

const probeColumns = "id, project_id, name, spec, state, created_at, updated_at"

func (db *DB) GetProbes(projectId ProjectId) ([]*Probe, error) {
	return db.queryProbes("SELECT "+probeColumns+" FROM probe WHERE project_id = $1 ORDER BY name", projectId)
}

// GetAllProbes returns the probes of all projects (used by the scheduler).
func (db *DB) GetAllProbes() ([]*Probe, error) {
	return db.queryProbes("SELECT " + probeColumns + " FROM probe ORDER BY project_id, name")
}

func (db *DB) GetProbe(projectId ProjectId, id string) (*Probe, error) {
	ps, err := db.queryProbes("SELECT "+probeColumns+" FROM probe WHERE project_id = $1 AND id = $2", projectId, id)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, ErrNotFound
	}
	return ps[0], nil
}

// GetProbeByIdOrName looks a probe up by its id or, if there is no such id, by its name.
func (db *DB) GetProbeByIdOrName(projectId ProjectId, idOrName string) (*Probe, error) {
	ps, err := db.queryProbes("SELECT "+probeColumns+" FROM probe WHERE project_id = $1 AND (id = $2 OR name = $2) ORDER BY CASE WHEN id = $2 THEN 0 ELSE 1 END", projectId, idOrName)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, ErrNotFound
	}
	return ps[0], nil
}

func (db *DB) CreateProbe(p *Probe) error {
	if p.Id == "" {
		p.Id = utils.NanoId(8)
	}
	now := timeseries.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	spec, err := json.Marshal(p.Spec)
	if err != nil {
		return err
	}
	_, err = db.db.Exec("INSERT INTO probe ("+probeColumns+") VALUES ($1, $2, $3, $4, '', $5, $6)", p.Id, p.ProjectId, p.Name, string(spec), p.CreatedAt, p.UpdatedAt)
	if err != nil && db.IsUniqueViolationError(err) {
		return ErrConflict
	}
	return err
}

// UpdateProbe updates the name and the spec. The state is reset since it may not be relevant anymore.
func (db *DB) UpdateProbe(p *Probe) error {
	p.UpdatedAt = timeseries.Now()
	spec, err := json.Marshal(p.Spec)
	if err != nil {
		return err
	}
	res, err := db.db.Exec("UPDATE probe SET name = $1, spec = $2, state = '', updated_at = $3 WHERE project_id = $4 AND id = $5", p.Name, string(spec), p.UpdatedAt, p.ProjectId, p.Id)
	if err != nil {
		if db.IsUniqueViolationError(err) {
			return ErrConflict
		}
		return err
	}
	p.State = ProbeState{}
	return affectedOrNotFound(res)
}

func (db *DB) UpdateProbeState(projectId ProjectId, id string, state ProbeState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = db.db.Exec("UPDATE probe SET state = $1 WHERE project_id = $2 AND id = $3", string(data), projectId, id)
	return err
}

func (db *DB) DeleteProbe(projectId ProjectId, id string) error {
	res, err := db.db.Exec("DELETE FROM probe WHERE project_id = $1 AND id = $2", projectId, id)
	if err != nil {
		return err
	}
	return affectedOrNotFound(res)
}

func (db *DB) queryProbes(query string, args ...any) ([]*Probe, error) {
	rows, err := db.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []*Probe{}
	for rows.Next() {
		var p Probe
		var spec, state string
		if err = rows.Scan(&p.Id, &p.ProjectId, &p.Name, &spec, &state, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(spec), &p.Spec); err != nil {
			return nil, errors.Join(fmt.Errorf("invalid probe spec (id=%s)", p.Id), err)
		}
		if state != "" {
			if err = json.Unmarshal([]byte(state), &p.State); err != nil {
				return nil, errors.Join(fmt.Errorf("invalid probe state (id=%s)", p.Id), err)
			}
		}
		res = append(res, &p)
	}
	return res, rows.Err()
}

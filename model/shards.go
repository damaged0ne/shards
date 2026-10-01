package model

import (
	"sort"
	"strconv"
	"strings"

	"github.com/coroot/coroot/timeseries"
)

// Shards fork: models for the metrics exported by the shards node agent (shards_*, node_agent_*,
// node PSI). Kept separate from the upstream files to simplify merges.

type NodeFilesystem struct {
	MountPoint string
	Device     string
	FsType     string

	SizeBytes  *timeseries.TimeSeries
	AvailBytes *timeseries.TimeSeries
	Files      *timeseries.TimeSeries
	FilesFree  *timeseries.TimeSeries
	Readonly   *timeseries.TimeSeries
}

// UsedPercent is the share of the space unavailable to non-root users (like df), in percent.
func (fs *NodeFilesystem) UsedPercent() *timeseries.TimeSeries {
	return timeseries.Aggregate2(fs.SizeBytes, fs.AvailBytes, func(size, avail float32) float32 {
		if size <= 0 || timeseries.IsNaN(size) || timeseries.IsNaN(avail) {
			return timeseries.NaN
		}
		return (size - avail) / size * 100
	})
}

func (fs *NodeFilesystem) InodesUsedPercent() *timeseries.TimeSeries {
	return timeseries.Aggregate2(fs.Files, fs.FilesFree, func(files, free float32) float32 {
		if files <= 0 || timeseries.IsNaN(files) || timeseries.IsNaN(free) {
			return timeseries.NaN
		}
		return (files - free) / files * 100
	})
}

func (fs *NodeFilesystem) IsReadonly() bool {
	return fs.Readonly.Last() == 1
}

// BecameReadonly reports whether the filesystem is read-only now but was writable earlier in the window,
// e.g. ext4 remounted with errors=remount-ro. Mounts that are read-only by design are not reported.
func (fs *NodeFilesystem) BecameReadonly() bool {
	if !fs.IsReadonly() {
		return false
	}
	return fs.Readonly.Reduce(timeseries.Min) == 0
}

type NftKey struct {
	Family string
	Table  string
	Chain  string // empty for named counters
	Name   string // counter name or rule comment
}

func (k NftKey) String() string {
	parts := []string{k.Family, k.Table}
	if k.Chain != "" {
		parts = append(parts, k.Chain)
	}
	return strings.Join(parts, "/") + ": " + k.Name
}

type NftStat struct {
	Bytes   *timeseries.TimeSeries // per second
	Packets *timeseries.TimeSeries // per second
}

type F2bJail struct {
	Banned *timeseries.TimeSeries
	Bans1h *timeseries.TimeSeries
}

// NodeAgentStats are the self-observability metrics of the node agent (node_agent_*).
// Counters are per-second rates.
type NodeAgentStats struct {
	EbpfLostSamples     *timeseries.TimeSeries
	EbpfDecodeErrors    *timeseries.TimeSeries
	L7ParseErrors       *timeseries.TimeSeries
	RecoveredPanics     *timeseries.TimeSeries
	RemoteWriteFailures *timeseries.TimeSeries
	EventsQueueLength   *timeseries.TimeSeries
}

func (s NodeAgentStats) IsEmpty() bool {
	return s.EbpfLostSamples.IsEmpty() && s.EbpfDecodeErrors.IsEmpty() && s.L7ParseErrors.IsEmpty() &&
		s.RecoveredPanics.IsEmpty() && s.RemoteWriteFailures.IsEmpty() && s.EventsQueueLength.IsEmpty()
}

type NodeShards struct {
	// Hostname is the hostname reported by node_info when a display name from the project settings replaced it
	// (see db.ServiceMapSettings.NodeDisplayNames); empty otherwise.
	Hostname string

	Filesystems map[string]*NodeFilesystem // by mount point

	Load1  *timeseries.TimeSeries
	Load5  *timeseries.TimeSeries
	Load15 *timeseries.TimeSeries

	// Pressure is the share of time (0..1 seconds/second) tasks were stalled, by "<resource>/<kind>",
	// e.g. "cpu/some", "memory/full", "io/some".
	Pressure map[string]*timeseries.TimeSeries

	F2bUp    *timeseries.TimeSeries
	F2bJails map[string]*F2bJail

	NftCounters map[NftKey]*NftStat
	NftRules    map[NftKey]*NftStat

	Agent NodeAgentStats
}

func NewNodeShards() *NodeShards {
	return &NodeShards{
		Filesystems: map[string]*NodeFilesystem{},
		Pressure:    map[string]*timeseries.TimeSeries{},
		F2bJails:    map[string]*F2bJail{},
		NftCounters: map[NftKey]*NftStat{},
		NftRules:    map[NftKey]*NftStat{},
	}
}

func (s *NodeShards) FilesystemsSorted() []*NodeFilesystem {
	if s == nil {
		return nil
	}
	res := make([]*NodeFilesystem, 0, len(s.Filesystems))
	for _, fs := range s.Filesystems {
		res = append(res, fs)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].MountPoint < res[j].MountPoint })
	return res
}

// Docker container states reported by shards_container_state.
const (
	DockerStateRunning    = "running"
	DockerStateExited     = "exited"
	DockerStateRestarting = "restarting"
	DockerStatePaused     = "paused"
	DockerStateCreated    = "created"
	DockerStateDead       = "dead"
	DockerStateRemoving   = "removing"

	DockerHealthHealthy   = "healthy"
	DockerHealthUnhealthy = "unhealthy"
	DockerHealthStarting  = "starting"
)

type DockerRelease struct {
	Version string
	ImageId string
}

// DockerContainer holds the Docker-level state of a container (shards_container_*, shards_compose_info).
// Unlike the cgroup-based metrics, it is also reported for containers that aren't running.
type DockerContainer struct {
	State         LabelLastValue
	Health        LabelLastValue
	RestartPolicy LabelLastValue

	Image    LabelLastValue
	ImageId  LabelLastValue
	Version  LabelLastValue
	Revision LabelLastValue

	ComposeProject LabelLastValue
	ComposeService LabelLastValue

	Unhealthy   *timeseries.TimeSeries // 1 while the healthcheck status is unhealthy
	ExitCode    *timeseries.TimeSeries
	OOMKilled   *timeseries.TimeSeries
	StartedAge  *timeseries.TimeSeries // seconds since the last start
	FinishedAge *timeseries.TimeSeries // seconds since the last finish (stopped containers only)
	Restarts    *timeseries.TimeSeries // restart count maintained by dockerd (a gauge)

	// The creation time (unix seconds) doesn't fit into float32 exactly, so it is loaded
	// as two parts: created = CreatedHi*65536 + CreatedLo.
	CreatedHi *timeseries.TimeSeries
	CreatedLo *timeseries.TimeSeries

	// ReleaseWindows are present while the container is within the release window after it was (re)created.
	ReleaseWindows map[DockerRelease]*timeseries.TimeSeries
}

func NewDockerContainer() *DockerContainer {
	return &DockerContainer{ReleaseWindows: map[DockerRelease]*timeseries.TimeSeries{}}
}

const DockerCreatedSplit = 65536

// CreatedAt returns the creation time at each point of the window (NaN points are skipped).
func (d *DockerContainer) CreatedAt() map[timeseries.Time]timeseries.Time {
	if d == nil || d.CreatedHi.IsEmpty() || d.CreatedLo.IsEmpty() {
		return nil
	}
	res := map[timeseries.Time]timeseries.Time{}
	hi, lo := d.CreatedHi.Iter(), d.CreatedLo.Iter()
	for hi.Next() && lo.Next() {
		t, h := hi.Value()
		_, l := lo.Value()
		if timeseries.IsNaN(h) || timeseries.IsNaN(l) {
			continue
		}
		res[t] = timeseries.Time(int64(h)*DockerCreatedSplit + int64(l))
	}
	return res
}

// LastCreatedAt returns the most recent creation time of the container.
func (d *DockerContainer) LastCreatedAt() timeseries.Time {
	var lastT, res timeseries.Time
	for t, c := range d.CreatedAt() {
		if t >= lastT {
			lastT, res = t, c
		}
	}
	return res
}

// SinceLast returns the number of seconds since the event of an *Age series (start, finish) at the end of the window.
func SinceLast(age *timeseries.TimeSeries, now timeseries.Time) (timeseries.Duration, bool) {
	t, v := age.LastNotNull()
	if timeseries.IsNaN(v) || t.IsZero() {
		return 0, false
	}
	return timeseries.Duration(v) + now.Sub(t), true
}

func (d *DockerContainer) IsStopped() bool {
	switch d.State.Value() {
	case DockerStateExited, DockerStateDead:
		return true
	}
	return false
}

func (d *DockerContainer) LastExitCode() (int, bool) {
	v := d.ExitCode.Last()
	if timeseries.IsNaN(v) {
		return 0, false
	}
	return int(v), true
}

func (d *DockerContainer) IsOOMKilled() bool {
	return d.OOMKilled.Last() == 1
}

// Failed reports whether a stopped container terminated abnormally: it is dead, was OOM-killed,
// or exited with a non-zero code other than the ones a graceful `docker stop` produces (SIGTERM, SIGINT).
func (d *DockerContainer) Failed() (bool, string) {
	if d == nil || !d.IsStopped() {
		return false, ""
	}
	if d.State.Value() == DockerStateDead {
		return true, "dead"
	}
	if d.IsOOMKilled() {
		return true, "OOM killed"
	}
	code, ok := d.LastExitCode()
	if !ok {
		return false, ""
	}
	switch code {
	case 0, 130, 143:
		return false, ""
	}
	return true, "exit code " + strconv.Itoa(code)
}

// RestartsIncrease is the number of restarts done by dockerd within the window.
// A drop of the counter means the container has been recreated, it isn't a restart.
func (d *DockerContainer) RestartsIncrease() float32 {
	if d == nil {
		return 0
	}
	return GaugeIncrease(d.Restarts)
}

// GaugeIncrease sums the positive deltas of a monotonic gauge that may be reset.
func GaugeIncrease(ts *timeseries.TimeSeries) float32 {
	if ts.IsEmpty() {
		return 0
	}
	var res float32
	prev := timeseries.NaN
	iter := ts.Iter()
	for iter.Next() {
		_, v := iter.Value()
		if timeseries.IsNaN(v) {
			continue
		}
		if !timeseries.IsNaN(prev) && v > prev {
			res += v - prev
		}
		prev = v
	}
	return res
}

// DockerContainerOf returns the Docker-level info of any container of the instance.
func (instance *Instance) DockerContainerOf() (*Container, *DockerContainer) {
	for _, c := range instance.Containers {
		if c.Docker != nil {
			return c, c.Docker
		}
	}
	return nil, nil
}

// scrapeErrorReasons maps the error reasons exported by the shards cluster agent in the `error` label of
// pg_scrape_error, mysql_scrape_error and mongo_scrape_error to human-readable messages.
// The upstream agent exported the raw error text, which is kept as is.
var scrapeErrorReasons = map[string]string{
	"timeout":      "timeout",
	"auth":         "authentication failed",
	"connection":   "connection error",
	"permission":   "insufficient privileges",
	"not_found":    "object not found",
	"unknown":      "unknown error",
	"unreachable":  "server unreachable",
	"disconnected": "client disconnected",
	"unauthorized": "unauthorized",
	"canceled":     "request canceled",
	"closed":       "collector closed",
}

func HumanizeScrapeError(s string) string {
	if s == "" {
		return s
	}
	if m, ok := scrapeErrorReasons[s]; ok {
		return m
	}
	// mongo: "<what>: <reason>"
	if what, reason, ok := strings.Cut(s, ": "); ok {
		if m, ok := scrapeErrorReasons[reason]; ok {
			return what + ": " + m
		}
	}
	return s
}

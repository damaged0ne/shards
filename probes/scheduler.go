package probes

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/timeseries"
	"github.com/prometheus/prometheus/prompb"
	"k8s.io/klog"
)

const (
	DefaultConcurrency = 16
	tickInterval       = time.Second
	reloadInterval     = 15 * time.Second
	writeTimeout       = 10 * time.Second
	writeErrorLogEvery = 10 * time.Minute
)

// MetricsWriter stores the probe metrics in the project's metrics storage (implemented by collector.Collector).
type MetricsWriter interface {
	WriteMetrics(ctx context.Context, projectId db.ProjectId, req *prompb.WriteRequest) error
}

type Store interface {
	GetAllProbes() ([]*db.Probe, error)
	GetProjects() (map[string]*db.Project, error)
	UpdateProbeState(projectId db.ProjectId, id string, state db.ProbeState) error
	GetPrimaryLock(ctx context.Context) bool
}

type entry struct {
	probe   *db.Probe
	next    time.Time
	running bool
}

// Scheduler runs the probes of all projects on the primary instance.
type Scheduler struct {
	store  Store
	writer MetricsWriter
	runner *Runner
	sem    chan struct{}

	lock       sync.Mutex
	entries    map[string]*entry
	lastReload time.Time
	reload     chan struct{}
	writeErrAt map[db.ProjectId]time.Time
	wg         sync.WaitGroup
}

func NewScheduler(store Store, writer MetricsWriter, runner *Runner, concurrency int) *Scheduler {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	return &Scheduler{
		store:      store,
		writer:     writer,
		runner:     runner,
		sem:        make(chan struct{}, concurrency),
		entries:    map[string]*entry{},
		reload:     make(chan struct{}, 1),
		writeErrAt: map[db.ProjectId]time.Time{},
	}
}

func key(p *db.Probe) string {
	return string(p.ProjectId) + "/" + p.Id
}

// Reload makes the scheduler pick up probe changes immediately (called after API changes).
func (s *Scheduler) Reload() {
	select {
	case s.reload <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			force := false
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-s.reload:
				force = true
			}
			if !s.store.GetPrimaryLock(ctx) {
				s.lock.Lock()
				s.entries = map[string]*entry{}
				s.lock.Unlock()
				continue
			}
			if force || time.Since(s.lastReload) >= reloadInterval {
				s.sync()
			}
			s.dispatch(ctx)
		}
	}()
}

// sync reconciles the schedule with the DB: new probes are spread across their interval (jitter),
// changed probes are rescheduled to run soon, deleted probes and probes of deleted projects are dropped.
func (s *Scheduler) sync() {
	s.lastReload = time.Now()
	probes, err := s.store.GetAllProbes()
	if err != nil {
		klog.Errorln("probes: failed to load:", err)
		return
	}
	projects, err := s.store.GetProjects()
	if err != nil {
		klog.Errorln("probes: failed to load projects:", err)
		return
	}
	n := time.Now()
	s.lock.Lock()
	defer s.lock.Unlock()
	seen := map[string]bool{}
	for _, p := range probes {
		project := projects[string(p.ProjectId)]
		if project == nil || project.Multicluster() || p.Spec.Paused {
			continue
		}
		k := key(p)
		seen[k] = true
		e := s.entries[k]
		interval := p.Spec.Interval.ToStandard()
		switch {
		case e == nil:
			s.entries[k] = &entry{probe: p, next: n.Add(time.Duration(rand.Int63n(int64(interval))))}
		case e.probe.UpdatedAt != p.UpdatedAt:
			e.probe = p
			e.next = n.Add(time.Duration(rand.Int63n(int64(2 * time.Second))))
		}
	}
	for k := range s.entries {
		if !seen[k] {
			delete(s.entries, k)
		}
	}
}

func (s *Scheduler) dispatch(ctx context.Context) {
	n := time.Now()
	s.lock.Lock()
	defer s.lock.Unlock()
	for k, e := range s.entries {
		if e.running || n.Before(e.next) {
			continue
		}
		select {
		case s.sem <- struct{}{}:
		default:
			return // all workers are busy, due probes run on the next ticks
		}
		e.running = true
		interval := e.probe.Spec.Interval.ToStandard()
		e.next = e.next.Add(interval)
		if e.next.Before(n) { // fell behind (e.g. the instance was not the primary)
			e.next = n.Add(interval)
		}
		// a small jitter keeps probes with the same interval from synchronizing
		e.next = e.next.Add(time.Duration(rand.Int63n(int64(interval/50) + 1)))
		p := e.probe
		s.wg.Add(1)
		go func(k string, p *db.Probe) {
			defer func() {
				<-s.sem
				s.lock.Lock()
				if e := s.entries[k]; e != nil {
					e.running = false
				}
				s.lock.Unlock()
				s.wg.Done()
			}()
			s.runProbe(ctx, k, p)
		}(k, p)
	}
}

func (s *Scheduler) runProbe(ctx context.Context, k string, p *db.Probe) {
	res := s.runner.Run(ctx, p.Spec)
	state := NextState(p.State, res)

	s.lock.Lock()
	if e := s.entries[k]; e != nil && e.probe.UpdatedAt == p.UpdatedAt {
		e.probe.State = state
	}
	s.lock.Unlock()

	if err := s.store.UpdateProbeState(p.ProjectId, p.Id, state); err != nil {
		klog.Errorln("probes: failed to save the state:", err)
	}
	if s.writer == nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := s.writer.WriteMetrics(wctx, p.ProjectId, WriteRequest(p, res, state.ConsecutiveFailures)); err != nil {
		s.lock.Lock()
		last := s.writeErrAt[p.ProjectId]
		logIt := time.Since(last) > writeErrorLogEvery
		if logIt {
			s.writeErrAt[p.ProjectId] = time.Now()
		}
		s.lock.Unlock()
		if logIt {
			klog.Warningf("probes: failed to write metrics for project %s: %s", p.ProjectId, err)
		}
	}
}

// NextState derives the probe state from the previous state and the latest result.
func NextState(prev db.ProbeState, res *Result) db.ProbeState {
	st := db.ProbeState{
		LastRunAt:  timeseries.Time(res.Time.Unix()),
		Up:         res.Up,
		Duration:   float32(res.Phases[PhaseTotal]),
		StatusCode: res.StatusCode,
		Error:      res.Error,
		LastUpAt:   prev.LastUpAt,
	}
	if res.Up {
		st.LastUpAt = st.LastRunAt
	} else {
		st.ConsecutiveFailures = prev.ConsecutiveFailures + 1
	}
	if c := res.Cert; c != nil {
		st.CertNotAfter = timeseries.Time(c.NotAfter.Unix())
		st.CertIssuer = c.Issuer
		st.CertSubject = c.Subject
		if c.Verified {
			v := c.Valid
			st.CertValid = &v
		}
	}
	return st
}

// Wait waits for the running probes (used in tests).
func (s *Scheduler) Wait() {
	s.wg.Wait()
}

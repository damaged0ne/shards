package probes

import "sync"

var (
	defaultLock      sync.RWMutex
	defaultRunner    *Runner
	defaultScheduler *Scheduler
)

// SetDefault registers the runner and the scheduler used by the API (test runs and reloads after changes).
func SetDefault(r *Runner, s *Scheduler) {
	defaultLock.Lock()
	defer defaultLock.Unlock()
	defaultRunner, defaultScheduler = r, s
}

// DefaultRunner returns the configured runner, or a runner with the default address guard.
func DefaultRunner() *Runner {
	defaultLock.RLock()
	r := defaultRunner
	defaultLock.RUnlock()
	if r != nil {
		return r
	}
	g, _ := NewGuard(nil)
	return NewRunner(g)
}

// NotifyChanged makes the scheduler pick up probe changes immediately.
func NotifyChanged() {
	defaultLock.RLock()
	s := defaultScheduler
	defaultLock.RUnlock()
	if s != nil {
		s.Reload()
	}
}

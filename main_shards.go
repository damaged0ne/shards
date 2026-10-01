package main

import (
	"context"

	"github.com/coroot/coroot/collector"
	"github.com/coroot/coroot/config"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/probes"
	"k8s.io/klog"
)

// startProbes runs the synthetic probes scheduler (shards fork). Results are written to the projects'
// metrics storage through the collector, like the metrics of the agents.
func startProbes(cfg *config.Config, database *db.DB, coll *collector.Collector) {
	guard, err := probes.NewGuard(cfg.Probes.AllowedNetworks)
	if err != nil {
		klog.Exitln("probes:", err)
	}
	runner := probes.NewRunner(guard)
	if cfg.Probes.Disabled {
		probes.SetDefault(runner, nil)
		klog.Infoln("probes: disabled")
		return
	}
	s := probes.NewScheduler(database, coll, runner, cfg.Probes.Concurrency)
	probes.SetDefault(runner, s)
	s.Start(context.Background())
}

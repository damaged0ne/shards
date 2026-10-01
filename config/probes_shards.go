package config

import (
	"strings"

	"gopkg.in/alecthomas/kingpin.v2"
)

// Shards fork: settings of the synthetic probes (see the probes package).

type Probes struct {
	Disabled bool `yaml:"disabled"`
	// Concurrency limits the number of probes running at the same time.
	Concurrency int `yaml:"concurrency"`
	// AllowedNetworks are CIDRs (or IPs) excluded from the default SSRF block list
	// (link-local 169.254.0.0/16, fe80::/10 and the AWS IMDS fd00:ec2::254).
	AllowedNetworks []string `yaml:"allowed_networks"`
}

var (
	probesDisabled        = kingpin.Flag("disable-probes", "Disable synthetic probes (HTTP/TCP/TLS/DNS uptime checks)").Envar("DISABLE_PROBES").Bool()
	probesConcurrency     = kingpin.Flag("probes-concurrency", "The maximum number of synthetic probes running at the same time (default 16)").Envar("PROBES_CONCURRENCY").Int()
	probesAllowedNetworks = kingpin.Flag("probes-allowed-networks", "Comma-separated CIDRs that probes may connect to although they are blocked by default (link-local and cloud metadata addresses)").Envar("PROBES_ALLOWED_NETWORKS").String()
)

func (cfg *Config) applyProbesFlags() {
	if *probesDisabled {
		cfg.Probes.Disabled = true
	}
	if *probesConcurrency > 0 {
		cfg.Probes.Concurrency = *probesConcurrency
	}
	if *probesAllowedNetworks != "" {
		for _, n := range strings.Split(*probesAllowedNetworks, ",") {
			if n = strings.TrimSpace(n); n != "" {
				cfg.Probes.AllowedNetworks = append(cfg.Probes.AllowedNetworks, n)
			}
		}
	}
}

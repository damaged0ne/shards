package model

import (
	"strings"

	"github.com/coroot/coroot/utils"
)

// shards fork: service map categories and groups.

// ApplicationCategorySystem holds host infrastructure units (container runtimes, sshd, time sync, cron, logging, ...).
// They are first-class nodes in the node view, but hidden on the service map and in the applications list by
// default. Only well-known infrastructure units are matched: on VM-only hosts the business applications are
// systemd services too, so "every /system.slice/* unit" would hide them.
const ApplicationCategorySystem ApplicationCategory = "system"

// SystemCategoryPatterns are checked before the other built-in categories (so docker/containerd land in "system"
// rather than upstream's "control-plane"). Only the non-Kubernetes namespace ("_") is matched.
var SystemCategoryPatterns = []string{
	"_/docker", "_/dockerd", "_/docker.socket", "_/containerd", "_/podman", "_/crio",
	"_/ssh", "_/sshd", "_/chrony", "_/chronyd", "_/ntp", "_/ntpd", "_/ntpsec", "_/openntpd",
	"_/systemd-*", "_/init.scope", "_/system", "_/user", "_/user@*", "_/session-*",
	"_/fail2ban", "_/cron", "_/crond", "_/anacron", "_/atd",
	"_/rsyslog", "_/syslog-ng", "_/syslog", "_/journald",
	"_/watchdog", "_/snapd", "_/snap.*", "_/dbus", "_/dbus-broker", "_/polkit", "_/udisks2", "_/upower",
	"_/multipathd", "_/irqbalance", "_/unattended-upgrades", "_/networkd-dispatcher", "_/accounts-daemon",
	"_/qemu-guest-agent", "_/open-vm-tools", "_/vmtoolsd", "_/hv-kvp-daemon", "_/google-guest-agent",
	"_/amazon-ssm-agent", "_/walinuxagent", "_/waagent",
	"_/getty*", "_/serial-getty*", "_/ModemManager", "_/NetworkManager", "_/firewalld", "_/ufw", "_/nftables",
	"_/auditd", "_/tuned", "_/thermald", "_/smartd", "_/mdmonitor", "_/haveged", "_/rngd", "_/rpcbind",
	"_/lvm2-*", "_/iscsid", "_/apparmor", "_/packagekit", "_/fwupd", "_/esm-cache", "_/motd-news",
}

// shardsMonitoringPatterns extend upstream's built-in "monitoring" category with the shards stack and common
// collectors. They're matched against "<namespace>/<name>".
var shardsMonitoringPatterns = []string{
	"_/shards-*", "_/shards_*", "*/shards-node-agent*", "*/shards-cluster-agent*",
	"*/*node-agent*", "*/*cluster-agent*", "*/*node-exporter*", "*/*node_exporter*",
	"*/*-exporter", "*/*_exporter", "*/*-exporter-*", "*/*alloy*", "*/*promtail*", "*/*cadvisor*",
	"*/*otel-collector*", "*/*opentelemetry-collector*", "*/*vector-agent*", "*/*fluent-bit*", "*/*fluentbit*",
	"_/heimdall-*", "_/heimdall_*",
}

func init() {
	BuiltinCategoryPatterns[ApplicationCategorySystem] = SystemCategoryPatterns
	BuiltinCategoryPatterns[ApplicationCategoryMonitoring] = append(BuiltinCategoryPatterns[ApplicationCategoryMonitoring], shardsMonitoringPatterns...)
}

// ShardsPriorityCategory returns the built-in category that takes precedence over upstream's built-in patterns.
func ShardsPriorityCategory(id string) ApplicationCategory {
	if utils.GlobMatch(id, SystemCategoryPatterns...) {
		return ApplicationCategorySystem
	}
	return ""
}

func (c ApplicationCategory) System() bool {
	return c == ApplicationCategorySystem
}

// IsSystemdUnit reports whether the application runs as a systemd unit (not a container).
func (app *Application) IsSystemdUnit() bool {
	for _, i := range app.Instances {
		for _, c := range i.Containers {
			if strings.HasPrefix(c.Id, "/system.slice/") || strings.HasPrefix(c.Id, "/user.slice/") || strings.HasPrefix(c.Id, "/init.scope") {
				return true
			}
		}
	}
	return false
}

// ComposeProject returns the compose project reported by the shards node agent (shards_compose_info), if any.
func (app *Application) ComposeProject() string {
	for _, i := range app.Instances {
		for _, c := range i.Containers {
			if c.Docker != nil {
				if p := c.Docker.ComposeProject.Value(); p != "" {
					return p
				}
			}
		}
	}
	return ""
}

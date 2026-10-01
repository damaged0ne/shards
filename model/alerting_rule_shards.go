package model

import "github.com/coroot/coroot/timeseries"

// shardsBuiltinAlertingRules are the built-in rules for the checks added by the shards fork.
// They are created for existing projects on startup (see db.InitBuiltinAlertingRules).
func shardsBuiltinAlertingRules() []AlertingRule {
	return append([]AlertingRule{
		{
			Id:   "docker-container-health",
			Name: "Unhealthy container",
			Source: AlertSource{
				Type:  AlertSourceTypeCheck,
				Check: &CheckSource{CheckId: Checks.DockerContainerHealth.Id},
			},
			Selector:      AppSelector{Type: AppSelectorTypeAll},
			Severity:      CRITICAL,
			For:           2 * timeseries.Minute,
			KeepFiringFor: 5 * timeseries.Minute,
			Templates: AlertTemplates{
				Description: "The Docker healthcheck of a container is failing. The service may be unable to handle requests.",
			},
			Enabled: true,
			Builtin: true,
		},
		{
			Id:   "docker-container-state",
			Name: "Container stopped abnormally",
			Source: AlertSource{
				Type:  AlertSourceTypeCheck,
				Check: &CheckSource{CheckId: Checks.DockerContainerState.Id},
			},
			Selector:      AppSelector{Type: AppSelectorTypeAll},
			Severity:      CRITICAL,
			For:           timeseries.Minute,
			KeepFiringFor: 5 * timeseries.Minute,
			Templates: AlertTemplates{
				Description: "A container isn't running: it was killed by the OOM killer, is dead, or exited with a non-zero code.",
			},
			Enabled: true,
			Builtin: true,
		},
		{
			Id:   "docker-container-restarts",
			Name: "Container restart loop",
			Source: AlertSource{
				Type:  AlertSourceTypeCheck,
				Check: &CheckSource{CheckId: Checks.DockerContainerRestarts.Id},
			},
			Selector:      AppSelector{Type: AppSelectorTypeAll},
			Severity:      WARNING,
			KeepFiringFor: 15 * timeseries.Minute,
			Templates: AlertTemplates{
				Description: "dockerd keeps restarting a container according to its restart policy. The service is likely crashing on startup.",
			},
			Enabled: true,
			Builtin: true,
		},
		{
			Id:   "node-disk-space",
			Name: "Low node disk space",
			Source: AlertSource{
				Type:  AlertSourceTypeCheck,
				Check: &CheckSource{CheckId: Checks.NodeDiskSpace.Id},
			},
			Selector:      AppSelector{Type: AppSelectorTypeAll},
			Severity:      WARNING,
			For:           5 * timeseries.Minute,
			KeepFiringFor: 5 * timeseries.Minute,
			Templates: AlertTemplates{
				Description: "A filesystem of a node running the app is almost full (space or inodes). Writes will fail once it fills up.",
			},
			Enabled: true,
			Builtin: true,
		},
		{
			Id:   "node-filesystem-readonly",
			Name: "Node filesystem read-only",
			Source: AlertSource{
				Type:  AlertSourceTypeCheck,
				Check: &CheckSource{CheckId: Checks.NodeFilesystemReadonly.Id},
			},
			Selector:      AppSelector{Type: AppSelectorTypeAll},
			Severity:      CRITICAL,
			KeepFiringFor: 30 * timeseries.Minute,
			Templates: AlertTemplates{
				Description: "A filesystem of a node running the app has been remounted read-only, usually because of I/O or filesystem errors.",
			},
			Enabled: true,
			Builtin: true,
		},
	}, dbExtBuiltinAlertingRules()...)
}

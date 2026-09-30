package constructor

import (
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
)

type containerLoader func(queryName string, f func(instance *model.Instance, container *model.Container, metric *model.MetricValues))

// loadDockerContainers loads the Docker-level container metrics of the shards node agent (shards_container_*,
// shards_compose_info, shards_release_window). They are reported for stopped containers too, so a stopped
// container still shows up as an instance of its application.
func loadDockerContainers(loadContainer containerLoader) {
	docker := func(c *model.Container) *model.DockerContainer {
		if c.Docker == nil {
			c.Docker = model.NewDockerContainer()
		}
		return c.Docker
	}
	load := func(queryName string, f func(d *model.DockerContainer, instance *model.Instance, container *model.Container, m *model.MetricValues)) {
		loadContainer(queryName, func(instance *model.Instance, container *model.Container, m *model.MetricValues) {
			f(docker(container), instance, container, m)
		})
	}

	load(qShardsContainerState, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.State.Update(m.Values, m.Labels["state"])
	})
	load(qShardsContainerHealth, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		status := m.Labels["status"]
		d.Health.Update(m.Values, status)
		if status == model.DockerHealthUnhealthy {
			d.Unhealthy = merge(d.Unhealthy, m.Values, timeseries.Any)
		}
	})
	load(qShardsContainerExitCode, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.ExitCode = merge(d.ExitCode, m.Values, timeseries.Any)
	})
	load(qShardsContainerOOMKilled, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.OOMKilled = merge(d.OOMKilled, m.Values, timeseries.Any)
	})
	load(qShardsContainerStartedAge, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.StartedAge = merge(d.StartedAge, m.Values, timeseries.Any)
	})
	load(qShardsContainerFinishedAge, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.FinishedAge = merge(d.FinishedAge, m.Values, timeseries.Any)
	})
	load(qShardsContainerDockerRestart, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.Restarts = merge(d.Restarts, m.Values, timeseries.Any)
	})
	load(qShardsContainerRestartPolicy, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.RestartPolicy.Update(m.Values, m.Labels["policy"])
	})
	load(qShardsContainerImageInfo, func(d *model.DockerContainer, _ *model.Instance, container *model.Container, m *model.MetricValues) {
		d.Image.Update(m.Values, m.Labels["image"])
		d.ImageId.Update(m.Values, m.Labels["image_id"])
		d.Version.Update(m.Values, m.Labels["version"])
		d.Revision.Update(m.Values, m.Labels["revision"])
		if container.Image == "" {
			container.Image = d.Image.Value()
		}
	})
	load(qShardsComposeInfo, func(d *model.DockerContainer, instance *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.ComposeProject.Update(m.Values, m.Labels["project"])
		d.ComposeService.Update(m.Values, m.Labels["service"])
		// one-off `docker compose run` containers are grouped into the <service>-run application: they are jobs
		if service := d.ComposeService.Value(); service != "" && instance.Owner.Id.Name == service+"-run" {
			instance.Owner.PeriodicSystemdJob = true
		}
	})
	load(qShardsContainerCreatedHi, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.CreatedHi = merge(d.CreatedHi, m.Values, timeseries.Any)
	})
	load(qShardsContainerCreatedLo, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		d.CreatedLo = merge(d.CreatedLo, m.Values, timeseries.Any)
	})
	load(qShardsReleaseWindow, func(d *model.DockerContainer, _ *model.Instance, _ *model.Container, m *model.MetricValues) {
		k := model.DockerRelease{Version: m.Labels["version"], ImageId: m.Labels["image_id"]}
		d.ReleaseWindows[k] = merge(d.ReleaseWindows[k], m.Values, timeseries.Any)
	})
}

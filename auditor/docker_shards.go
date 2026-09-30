package auditor

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: Docker-level container state reported by the shards node agent (shards_container_*).

// dockerInstanceStatus returns the status of a non-Kubernetes instance that has no cgroup metrics (isn't up)
// based on its Docker state. ok is false if the Docker state is unknown.
func dockerInstanceStatus(i *model.Instance) (status model.Status, msg string, ok bool) {
	_, d := i.DockerContainerOf()
	if d == nil || d.State.Value() == "" {
		return model.UNKNOWN, "", false
	}
	switch d.State.Value() {
	case model.DockerStateExited, model.DockerStateDead:
		if failed, reason := d.Failed(); failed {
			return model.WARNING, "down (" + reason + ")", true
		}
		if code, ok := d.LastExitCode(); ok {
			return model.UNKNOWN, fmt.Sprintf("stopped (exit code %d)", code), true
		}
		return model.UNKNOWN, "stopped", true
	case model.DockerStateRestarting:
		return model.WARNING, "down (restarting)", true
	case model.DockerStatePaused:
		return model.WARNING, "down (paused)", true
	case model.DockerStateCreated:
		return model.UNKNOWN, "created (not started)", true
	case model.DockerStateRemoving:
		return model.UNKNOWN, "removing", true
	}
	return model.UNKNOWN, "", false
}

// dockerUpInstanceStatus adjusts the status of an up and running instance according to its Docker healthcheck:
// an unhealthy container is treated like a Kubernetes pod failing its readiness probe.
func dockerUpInstanceStatus(i *model.Instance) (status model.Status, msg string, ok bool) {
	_, d := i.DockerContainerOf()
	if d == nil {
		return model.UNKNOWN, "", false
	}
	switch d.Health.Value() {
	case model.DockerHealthUnhealthy:
		if d.State.Value() == model.DockerStateRunning {
			return model.WARNING, "down (healthcheck failed)", true
		}
	case model.DockerHealthStarting:
		return model.OK, "up (health: starting)", true
	case model.DockerHealthHealthy:
		return model.OK, "up (healthy)", true
	}
	return model.UNKNOWN, "", false
}

// dockerDesiredInstances returns the number of instances of a Docker/Compose app that are expected to run:
// containers stopped gracefully (exit code 0, 130, 143) or never started aren't.
func dockerDesiredInstances(app *model.Application) (float32, bool) {
	if app.IsK8s() || app.Id.Kind == model.ApplicationKindExternalService {
		return 0, false
	}
	var desired float32
	seen := false
	for _, i := range app.Instances {
		_, d := i.DockerContainerOf()
		if d != nil {
			seen = true
		}
		if i.IsUp() || d == nil {
			desired++
			continue
		}
		switch d.State.Value() {
		case model.DockerStateCreated, model.DockerStateRemoving:
			continue
		case model.DockerStateExited, model.DockerStateDead:
			if failed, _ := d.Failed(); !failed {
				continue
			}
		}
		desired++
	}
	return desired, seen
}

func (a *appAuditor) dockerContainers(report *model.AuditReport) {
	type row struct {
		instance  *model.Instance
		container *model.Container
		docker    *model.DockerContainer
	}
	var rows []row
	for _, i := range a.app.Instances {
		for _, c := range i.Containers {
			if c.Docker != nil {
				rows = append(rows, row{instance: i, container: c, docker: c.Docker})
			}
		}
	}
	if len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].instance.Name == rows[j].instance.Name {
			return rows[i].container.Name < rows[j].container.Name
		}
		return rows[i].instance.Name < rows[j].instance.Name
	})

	healthCheck := report.CreateCheck(model.Checks.DockerContainerHealth)
	stateCheck := report.CreateCheck(model.Checks.DockerContainerState)
	restartsCheck := report.CreateCheck(model.Checks.DockerContainerRestarts)

	table := report.GetOrCreateTable("Container", "State", "Health", "Docker restarts", "Restart policy", "Image", "Version")
	unhealthyChart := report.GetOrCreateChart("Unhealthy containers", nil).Stacked()
	restartsChart := report.GetOrCreateChart("Docker restarts", nil).Column()
	healthCheck.AddWidget(unhealthyChart.Widget())
	healthCheck.AddWidget(table.Widget())
	stateCheck.AddWidget(table.Widget())
	restartsCheck.AddWidget(restartsChart.Widget())

	periodicJob := a.app.PeriodicJob()
	now := a.w.Ctx.To
	for _, r := range rows {
		d := r.docker
		name := r.instance.Name
		if len(r.instance.Containers) > 1 {
			name += "/" + r.container.Name
		}

		state := model.NewTableCell(d.State.Value())
		switch d.State.Value() {
		case model.DockerStateRunning:
			state.SetStatus(model.OK, "running")
			if since, ok := model.SinceLast(d.StartedAge, now); ok {
				state.AddTag("started %s ago", utils.FormatDuration(since, 1))
			}
		case "":
		default:
			st := model.UNKNOWN
			if failed, reason := d.Failed(); failed {
				st = model.WARNING
				state.AddTag("%s", reason)
				if !periodicJob {
					stateCheck.AddItem("%s", name)
					stateCheck.AddDetail("%s: %s", name, reason)
				}
			} else if code, ok := d.LastExitCode(); ok && d.IsStopped() {
				state.AddTag("exit code %d", code)
			}
			switch d.State.Value() {
			case model.DockerStateRestarting, model.DockerStatePaused:
				st = model.WARNING
			}
			state.SetStatus(st, d.State.Value())
			if since, ok := model.SinceLast(d.FinishedAge, now); ok && d.IsStopped() {
				state.AddTag("finished %s ago", utils.FormatDuration(since, 1))
			}
		}

		health := model.NewTableCell()
		switch h := d.Health.Value(); {
		case h == "" || d.State.Value() != model.DockerStateRunning:
		case h == model.DockerHealthUnhealthy:
			health.SetStatus(model.WARNING, h)
			healthCheck.AddItem("%s", name)
			healthCheck.AddDetail("%s: unhealthy", name)
		case h == model.DockerHealthHealthy:
			health.SetStatus(model.OK, h)
		default:
			health.SetStatus(model.UNKNOWN, h)
		}
		if unhealthyChart != nil && !d.Unhealthy.IsEmpty() {
			unhealthyChart.AddSeries(name, d.Unhealthy)
		}

		restarts := model.NewTableCell()
		if total := d.Restarts.Last(); !timeseries.IsNaN(total) {
			restarts.SetValue(strconv.Itoa(int(total)))
		}
		if inc := d.RestartsIncrease(); inc > 0 {
			restarts.AddTag("+%d", int(inc))
			if !periodicJob {
				restartsCheck.Inc(int64(inc))
			}
		}
		if restartsChart != nil {
			restartsChart.AddSeries(name, dockerRestartsDelta(d.Restarts))
		}

		version := model.NewTableCell(d.Version.Value())
		if rev := d.Revision.Value(); rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			version.AddTag("rev: %s", rev)
		}
		image := model.NewTableCell(d.Image.Value())
		if p := d.ComposeProject.Value(); p != "" {
			image.AddTag("compose: %s/%s", p, d.ComposeService.Value())
		}

		table.AddRow(
			model.NewTableCell(name),
			state,
			health,
			restarts,
			model.NewTableCell(d.RestartPolicy.Value()),
			image,
			version,
		)
	}
	if periodicJob {
		stateCheck.SetStatus(model.OK, "not checked for one-off jobs")
		restartsCheck.SetStatus(model.OK, "not checked for one-off jobs")
	}
}

// dockerRestartsDelta converts the dockerd restart count (a gauge that is reset when the container is recreated)
// to the number of restarts per point.
func dockerRestartsDelta(ts *timeseries.TimeSeries) *timeseries.TimeSeries {
	if ts.IsEmpty() {
		return nil
	}
	prev := timeseries.NaN
	return ts.Map(func(t timeseries.Time, v float32) float32 {
		res := timeseries.NaN
		if !timeseries.IsNaN(v) && !timeseries.IsNaN(prev) && v > prev {
			res = v - prev
		}
		if !timeseries.IsNaN(v) {
			prev = v
		}
		return res
	})
}

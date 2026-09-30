package model

import (
	"sort"
	"strings"

	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
)

// Shards fork: releases of Docker/Compose applications.
//
// The shards node agent reports shards_release_window{version,image_id} for a while after a container is (re)created
// (Compose recreates containers only on image or config changes) and shards_container_created_seconds.
// A release is a group of containers of the application (re)created with the same image during the window.

// DockerReleaseMatchTolerance is how far apart the start of the same release may be when it is re-calculated:
// the start is the creation time of the earliest container of the release still within its window.
const DockerReleaseMatchTolerance = timeseries.Hour

type dockerReleaseGroup struct {
	key       DockerRelease
	image     string
	started   timeseries.Time
	finished  timeseries.Time
	instances map[*Instance]bool
}

// CalcDockerReleases detects releases of a non-Kubernetes application from the Docker-level metrics of its containers.
// The result is sorted by the start time.
func CalcDockerReleases(app *Application) []*ApplicationDeployment {
	if app == nil || app.Id.Kind == ApplicationKindDeployment || app.PeriodicJob() {
		return nil
	}
	groups := map[DockerRelease]*dockerReleaseGroup{}
	for _, instance := range app.Instances {
		for _, c := range instance.Containers {
			d := c.Docker
			if d == nil || len(d.ReleaseWindows) == 0 {
				continue
			}
			created := d.CreatedAt()
			for key, window := range d.ReleaseWindows {
				ct := releaseCreatedAt(window, created)
				if ct.IsZero() {
					continue
				}
				g := groups[key]
				if g == nil {
					g = &dockerReleaseGroup{key: key, instances: map[*Instance]bool{}}
					groups[key] = g
				}
				if g.started.IsZero() || ct.Before(g.started) {
					g.started = ct
				}
				if ct.After(g.finished) {
					g.finished = ct
				}
				if img := d.Image.Value(); img != "" && g.image == "" && d.ImageId.Value() == key.ImageId {
					g.image = img
				}
				g.instances[instance] = true
			}
		}
	}
	if len(groups) == 0 {
		return nil
	}

	var res []*dockerReleaseGroup
	for _, g := range groups {
		if isDockerScaleUp(app, g) {
			continue
		}
		res = append(res, g)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].started.Before(res[j].started) })

	var deployments []*ApplicationDeployment
	for i, g := range res {
		d := &ApplicationDeployment{
			ApplicationId: app.Id,
			Name:          dockerReleaseName(app, g.key),
			StartedAt:     g.started,
		}
		last := i == len(res)-1
		if !last || dockerReleaseRolledOut(app, g) {
			d.FinishedAt = g.finished
		}
		if img := dockerReleaseImage(g.image, g.key.Version); img != "" {
			d.Details = &ApplicationDeploymentDetails{ContainerImages: []string{img}}
		}
		deployments = append(deployments, d)
	}
	return deployments
}

// releaseCreatedAt returns the creation time of the container at the first point of its release window.
func releaseCreatedAt(window *timeseries.TimeSeries, created map[timeseries.Time]timeseries.Time) timeseries.Time {
	if window.IsEmpty() {
		return 0
	}
	var first timeseries.Time
	iter := window.Iter()
	for iter.Next() {
		t, v := iter.Value()
		if timeseries.IsNaN(v) {
			continue
		}
		if ct, ok := created[t]; ok {
			return ct
		}
		if first.IsZero() {
			first = t
		}
	}
	if first.IsZero() {
		return 0
	}
	// no creation time at the same points: take the closest one after the window has started
	var res, closest timeseries.Time
	for t, ct := range created {
		if t.Before(first) {
			continue
		}
		if closest.IsZero() || t.Before(closest) {
			closest, res = t, ct
		}
	}
	return res
}

// isDockerScaleUp reports whether the release group is just new replicas of the image the other, older instances
// were already running (e.g. `docker compose up --scale`), rather than a release.
func isDockerScaleUp(app *Application, g *dockerReleaseGroup) bool {
	for _, instance := range app.Instances {
		if g.instances[instance] {
			continue
		}
		_, d := instance.DockerContainerOf()
		if d == nil || d.State.Value() != DockerStateRunning || d.ImageId.Value() == "" {
			continue
		}
		if d.ImageId.Value() == g.key.ImageId && d.LastCreatedAt().Before(g.started) {
			return true
		}
	}
	return false
}

// dockerReleaseRolledOut reports whether all running containers of the app run the image of the release.
func dockerReleaseRolledOut(app *Application, g *dockerReleaseGroup) bool {
	for _, instance := range app.Instances {
		_, d := instance.DockerContainerOf()
		if d == nil || d.State.Value() != DockerStateRunning {
			continue
		}
		if id := d.ImageId.Value(); id != "" && g.key.ImageId != "" && id != g.key.ImageId {
			return false
		}
	}
	return true
}

// dockerReleaseName makes a name whose last "-"-separated part (the deployment's Hash) identifies the image.
func dockerReleaseName(app *Application, r DockerRelease) string {
	hash := strings.TrimPrefix(r.ImageId, "sha256:")
	if len(hash) > 12 {
		hash = hash[:12]
	}
	if hash == "" {
		hash = strings.ReplaceAll(r.Version, "-", "_")
	}
	return app.Id.Name + "-" + hash
}

func dockerReleaseImage(image, version string) string {
	if image == "" {
		return version
	}
	if version == "" || strings.HasSuffix(utils.FormatImage(image), ":"+version) {
		return image
	}
	return image + " (" + version + ")"
}

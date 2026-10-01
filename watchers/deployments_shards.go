package watchers

import (
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"k8s.io/klog"
)

// discoverAndSaveDockerReleases is the shards fork counterpart of discoverAndSaveDeployments for Docker/Compose
// applications: releases are detected from the Docker-level container metrics of the shards node agent.
func (w *Deployments) discoverAndSaveDockerReleases(project *db.Project, world *model.World) int {
	var apps int
	for _, app := range world.Applications {
		releases := model.CalcDockerReleases(app)
		if len(releases) == 0 {
			continue
		}
		apps++
		for _, d := range releases {
			known := matchDockerRelease(app.Deployments, d)
			if known != nil {
				// the start is recalculated from the containers still within their release window, keep the saved one
				d.StartedAt = known.StartedAt
				if known.FinishedAt == d.FinishedAt || d.FinishedAt.IsZero() || !known.FinishedAt.IsZero() {
					continue
				}
			}
			if err := w.db.SaveApplicationDeployment(project.Id, d); err != nil {
				klog.Errorln("failed to save deployment:", err)
				return apps
			}
			if known == nil {
				klog.Infof("new release detected for %s: %s", app.Id, d.Name)
				app.Deployments = append(app.Deployments, d)
			} else {
				known.FinishedAt = d.FinishedAt
			}
		}
	}
	return apps
}

func matchDockerRelease(known []*model.ApplicationDeployment, d *model.ApplicationDeployment) *model.ApplicationDeployment {
	for _, k := range known {
		if k.Name != d.Name {
			continue
		}
		diff := k.StartedAt.Sub(d.StartedAt)
		if diff < 0 {
			diff = -diff
		}
		if diff <= model.DockerReleaseMatchTolerance {
			return k
		}
	}
	return nil
}

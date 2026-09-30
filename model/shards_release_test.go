package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
)

func TestCalcDockerReleases(t *testing.T) {
	const (
		from = timeseries.Time(1_700_000_000)
		step = timeseries.Duration(60)
	)
	nan := timeseries.NaN
	var app *Application
	createApp := func() {
		app = NewApplication(NewApplicationId("", "shop", ApplicationKindDockerSwarmService, "api"))
	}
	// created: the creation time (0 = no metric) at each point, release: the image id of the release window at each point ("" = none)
	addInstance := func(name, imageId string, created []timeseries.Time, release []string) {
		i := app.GetOrCreateInstance(name, nil)
		c := i.GetOrCreateContainer("/swarm/shop/api/"+name, "api")
		d := NewDockerContainer()
		c.Docker = d
		n := len(created)
		hi, lo := make([]float32, n), make([]float32, n)
		for j, ct := range created {
			if ct == 0 {
				hi[j], lo[j] = nan, nan
				continue
			}
			hi[j], lo[j] = float32(int64(ct)/DockerCreatedSplit), float32(int64(ct)%DockerCreatedSplit)
		}
		d.CreatedHi = timeseries.NewWithData(from, step, hi)
		d.CreatedLo = timeseries.NewWithData(from, step, lo)
		ones := make([]float32, n)
		for j := range ones {
			ones[j] = 1
		}
		d.State.Update(timeseries.NewWithData(from, step, ones), DockerStateRunning)
		d.ImageId.Update(timeseries.NewWithData(from, step, ones), imageId)
		d.Image.Update(timeseries.NewWithData(from, step, ones), "registry/api:"+strings.TrimPrefix(imageId, "sha256:"))
		windows := map[string][]float32{}
		for j, r := range release {
			if r == "" {
				continue
			}
			if windows[r] == nil {
				windows[r] = make([]float32, n)
				for k := range windows[r] {
					windows[r][k] = nan
				}
			}
			windows[r][j] = 1800
		}
		for img, data := range windows {
			d.ReleaseWindows[DockerRelease{Version: strings.TrimPrefix(img, "sha256:"), ImageId: img}] = timeseries.NewWithData(from, step, data)
		}
	}
	check := func(expected string) {
		var actual []string
		for _, d := range CalcDockerReleases(app) {
			finished := int64(0)
			if !d.FinishedAt.IsZero() {
				finished = int64(d.FinishedAt.Sub(from))
			}
			actual = append(actual, fmt.Sprintf("%d-%d:%s:%s", d.StartedAt.Sub(from), finished, d.Name, strings.Join(d.Details.ContainerImages, ",")))
		}
		assert.Equal(t, expected, strings.Join(actual, ";"))
	}
	old := from.Add(-10 * timeseries.Day)
	t1, t2, t3 := from.Add(90), from.Add(95), from.Add(200)

	// no release windows
	createApp()
	addInstance("1", "sha256:a", []timeseries.Time{old, old, old, old, old}, nil)
	check("")

	// both replicas recreated with a new image
	createApp()
	addInstance("1", "sha256:b", []timeseries.Time{old, old, t1, t1, t1}, []string{"", "", "sha256:b", "sha256:b", "sha256:b"})
	addInstance("2", "sha256:b", []timeseries.Time{old, old, t2, t2, t2}, []string{"", "", "sha256:b", "sha256:b", "sha256:b"})
	check("90-95:api-b:registry/api:b")

	// in progress: the second replica still runs the previous image
	createApp()
	addInstance("1", "sha256:b", []timeseries.Time{old, old, t1, t1, t1}, []string{"", "", "sha256:b", "sha256:b", "sha256:b"})
	addInstance("2", "sha256:a", []timeseries.Time{old, old, old, old, old}, nil)
	check("90-0:api-b:registry/api:b")

	// scale up: a new replica of the image the old one runs isn't a release
	createApp()
	addInstance("1", "sha256:b", []timeseries.Time{old, old, old, old, old}, nil)
	addInstance("2", "sha256:b", []timeseries.Time{0, 0, t1, t1, t1}, []string{"", "", "sha256:b", "sha256:b", "sha256:b"})
	check("")

	// two releases within the window: the first one is finished by the second one
	createApp()
	addInstance("1", "sha256:c", []timeseries.Time{old, t1, t1, t3, t3}, []string{"", "sha256:b", "sha256:b", "sha256:c", "sha256:c"})
	check("90-90:api-b:b;200-200:api-c:registry/api:c")

	// one-off jobs are skipped
	createApp()
	app.PeriodicSystemdJob = true
	addInstance("1", "sha256:b", []timeseries.Time{old, old, t1, t1, t1}, []string{"", "", "sha256:b", "sha256:b", "sha256:b"})
	check("")
}

func TestDockerContainerFailed(t *testing.T) {
	ts := func(v float32) *timeseries.TimeSeries { return timeseries.NewWithData(0, 1, []float32{v}) }
	for _, c := range []struct {
		state    string
		exitCode float32
		oom      float32
		failed   bool
		reason   string
	}{
		{DockerStateRunning, nan32(), nan32(), false, ""},
		{DockerStateExited, 0, 0, false, ""},
		{DockerStateExited, 143, 0, false, ""},
		{DockerStateExited, 1, 0, true, "exit code 1"},
		{DockerStateExited, 137, 1, true, "OOM killed"},
		{DockerStateDead, 0, 0, true, "dead"},
	} {
		d := NewDockerContainer()
		d.State.Update(ts(1), c.state)
		d.ExitCode = ts(c.exitCode)
		d.OOMKilled = ts(c.oom)
		failed, reason := d.Failed()
		assert.Equal(t, c.failed, failed, c)
		assert.Equal(t, c.reason, reason, c)
	}
	assert.Equal(t, float32(3), GaugeIncrease(timeseries.NewWithData(0, 1, []float32{1, 2, timeseries.NaN, 3, 0, 1})))
}

func nan32() float32 { return timeseries.NaN }

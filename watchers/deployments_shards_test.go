package watchers

import (
	"testing"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
)

func TestMatchDockerRelease(t *testing.T) {
	const t0 = timeseries.Time(1_700_000_000)
	known := []*model.ApplicationDeployment{
		{Name: "api-aaa", StartedAt: t0},
		{Name: "api-bbb", StartedAt: t0.Add(2 * timeseries.Hour)},
	}
	// the start drifts when the earliest container of the release leaves its release window
	assert.Equal(t, known[0], matchDockerRelease(known, &model.ApplicationDeployment{Name: "api-aaa", StartedAt: t0.Add(5 * timeseries.Minute)}))
	// the same image redeployed later is a new release
	assert.Nil(t, matchDockerRelease(known, &model.ApplicationDeployment{Name: "api-aaa", StartedAt: t0.Add(3 * timeseries.Hour)}))
	assert.Nil(t, matchDockerRelease(known, &model.ApplicationDeployment{Name: "api-ccc", StartedAt: t0}))
	assert.Equal(t, known[1], matchDockerRelease(known, &model.ApplicationDeployment{Name: "api-bbb", StartedAt: t0.Add(2 * timeseries.Hour)}))
}

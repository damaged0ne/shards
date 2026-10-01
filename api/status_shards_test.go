package api

import (
	"testing"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterAgentStatus(t *testing.T) {
	ts := func(vs ...float32) *timeseries.TimeSeries { return timeseries.NewWithData(0, 30, vs) }
	w := model.NewWorld(0, 150, 30, 30)
	assert.Nil(t, renderClusterAgentStatus(w))

	ok := w.ClusterAgent.GetOrCreateTarget("postgres", "10.0.0.1:5432")
	ok.Success = ts(1, 1, 1, 1, 1)
	ok.Duration = ts(0.1, 0.1, 0.1, 0.1, 0.2)
	ok.Timeouts = ts(0, 0, 0, 0, 0)
	res := renderClusterAgentStatus(w)
	require.NotNil(t, res)
	assert.Equal(t, model.OK, res.Status)
	assert.Equal(t, "1 target collected", res.Message)

	bad := w.ClusterAgent.GetOrCreateTarget("kafka", "10.0.0.9:9092")
	bad.Success = ts(1, 1, 0, 0, 0)
	bad.Timeouts = ts(0, 0, 1.0/30, 1.0/30, 1.0/30)
	res = renderClusterAgentStatus(w)
	assert.Equal(t, model.WARNING, res.Status)
	assert.Equal(t, "1 target of 2 failing: their metrics are not collected", res.Message)
	require.Len(t, res.Targets, 2)
	assert.Equal(t, "kafka", res.Targets[0].Type)
	assert.Equal(t, model.WARNING, res.Targets[0].Status)
	assert.InDelta(t, 3, res.Targets[0].Timeouts, 0.01)

	st := renderStatus(&db.Project{Id: "p1"}, nil, w, nil)
	require.NotNil(t, st.ClusterAgent)
	assert.Equal(t, model.WARNING, st.Status)
}

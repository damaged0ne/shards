package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPProbes(t *testing.T) {
	e := newMCPTestEnv(t)
	var world *model.World
	e.h.Api.loadWorld = func(ctx context.Context, project *db.Project, from, to timeseries.Time) (*model.World, error) {
		return world, nil
	}
	editor := e.ctx(rbac.RoleEditor, "editor")
	viewer := e.ctx(rbac.RoleViewer, "viewer")

	res := e.call(viewer, e.h.toolCreateProbe, map[string]any{"name": "site", "type": "http", "target": "https://example.com"})
	assert.True(t, res.IsError)
	assert.Contains(t, resultText(res), "forbidden")

	res = e.call(editor, e.h.toolCreateProbe, map[string]any{"name": "site", "type": "http", "target": "ftp://example.com"})
	assert.True(t, res.IsError)
	assert.Contains(t, resultText(res), "http:// or https://")

	res = e.call(editor, e.h.toolCreateProbe, map[string]any{
		"name": "site", "type": "http", "target": "https://example.com/health", "interval": "30s",
		"headers": []any{"X-Token: secret"}, "expected_status": "200", "body_contains": "ok",
	})
	require.False(t, res.IsError, resultText(res))
	var created mcpProbe
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &created))
	assert.Equal(t, "site", created.Name)
	assert.Equal(t, "30s", created.Interval)
	assert.Equal(t, "unknown", created.Status)

	p, err := e.db.GetProbe(e.project.Id, created.Id)
	require.NoError(t, err)
	assert.Equal(t, "X-Token", p.Spec.Headers[0].Key)
	assert.Equal(t, "secret", p.Spec.Headers[0].Value)

	// results from the world
	from := timeseries.Time(1_700_000_000)
	world = model.NewWorld(from, from.Add(4*timeseries.Minute), timeseries.Minute, timeseries.Minute)
	mp := model.NewProbe(p.Id)
	mp.Up = timeseries.NewWithData(from, timeseries.Minute, []float32{1, 0, 0, 1})
	mp.Durations["total"] = timeseries.NewWithData(from, timeseries.Minute, []float32{0.1, 0.2, 0.3, 0.4})
	mp.Durations["dns"] = timeseries.NewWithData(from, timeseries.Minute, []float32{0.01, 0.01, 0.01, 0.01})
	world.Probes = append(world.Probes, mp)

	res = e.call(viewer, e.h.toolListProbes, nil)
	require.False(t, res.IsError, resultText(res))
	assert.Contains(t, resultText(res), `"uptime_percent":50`)

	res = e.call(viewer, e.h.toolGetProbeResults, map[string]any{"id": "site", "window": "24h"})
	require.False(t, res.IsError, resultText(res))
	var results mcpProbeResults
	require.NoError(t, json.Unmarshal([]byte(resultText(res)), &results))
	assert.Equal(t, "1d", results.Window)
	require.Len(t, results.Downtime, 1)
	assert.Equal(t, "2023-11-14T22:14:20Z", results.Downtime[0].From)
	assert.Equal(t, "2023-11-14T22:16:20Z", results.Downtime[0].To)
	assert.Equal(t, "10ms", results.Phases["dns"])
	assert.Equal(t, "400ms", results.LatencyMax)

	res = e.call(editor, e.h.toolUpdateProbe, map[string]any{"id": "site", "paused": true, "interval": "5m"})
	require.False(t, res.IsError, resultText(res))
	p, err = e.db.GetProbe(e.project.Id, created.Id)
	require.NoError(t, err)
	assert.True(t, p.Spec.Paused)
	assert.Equal(t, 5*timeseries.Minute, p.Spec.Interval)
	assert.Equal(t, "ok", p.Spec.BodyContains, "fields that are not passed are kept")

	res = e.call(viewer, e.h.toolDeleteProbe, map[string]any{"id": created.Id})
	assert.True(t, res.IsError)
	res = e.call(editor, e.h.toolDeleteProbe, map[string]any{"id": created.Id})
	require.False(t, res.IsError, resultText(res))
	_, err = e.db.GetProbe(e.project.Id, created.Id)
	assert.ErrorIs(t, err, db.ErrNotFound)
}

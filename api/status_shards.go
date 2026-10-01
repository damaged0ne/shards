package api

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"k8s.io/klog"

	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/dustin/go-humanize/english"
)

// Shards fork: per-target collection health of the shards cluster agent
// (coroot_cluster_agent_target_collect_{success,duration_seconds,timeouts_total}).

// clusterAgentRecentPoints is the number of the latest points a target must have failed in to be reported:
// a single slow scrape doesn't flip the project status.
const clusterAgentRecentPoints = 3

type ClusterAgent struct {
	Status  model.Status         `json:"status"`
	Message string               `json:"message"`
	Targets []ClusterAgentTarget `json:"targets"`
}

type ClusterAgentTarget struct {
	Type     string       `json:"type"`
	Address  string       `json:"address"`
	Status   model.Status `json:"status"`
	Message  string       `json:"message"`
	Duration float32      `json:"duration"` // seconds, the last collection
	Timeouts float32      `json:"timeouts"` // over the selected period
}

func renderClusterAgentStatus(w *model.World) *ClusterAgent {
	if w == nil || len(w.ClusterAgent.Targets) == 0 {
		return nil
	}
	res := &ClusterAgent{Status: model.OK}
	failing := 0
	for _, t := range w.ClusterAgent.TargetsSorted() {
		ct := ClusterAgentTarget{Type: t.Type, Address: t.Address, Status: model.OK, Message: "ok", Duration: t.Duration.Last()}
		if timeseries.IsNaN(ct.Duration) {
			ct.Duration = 0
		}
		if sum := t.Timeouts.Reduce(timeseries.NanSum); !timeseries.IsNaN(sum) {
			ct.Timeouts = sum * float32(w.Ctx.Step)
		}
		switch {
		case t.Success.TailIsEmpty():
			ct.Status = model.UNKNOWN
			ct.Message = "no recent collections"
		case t.Failing(clusterAgentRecentPoints):
			ct.Status = model.WARNING
			ct.Message = "metrics collection doesn't complete within the deadline"
			failing++
		case ct.Timeouts >= 1:
			ct.Message = fmt.Sprintf("%.0f collections timed out over the selected period", ct.Timeouts)
		}
		res.Targets = append(res.Targets, ct)
	}
	if failing > 0 {
		res.Status = model.WARNING
		res.Message = fmt.Sprintf("%s of %d failing: their metrics are not collected", english.Plural(failing, "target", ""), len(res.Targets))
	} else {
		res.Message = english.Plural(len(res.Targets), "target", "") + " collected"
	}
	return res
}

func (h *MCPHandler) registerStatusTools() {
	h.AddTool(
		mcp.NewTool("get_monitoring_status",
			mcp.WithDescription("Health of the monitoring itself in the selected project: Prometheus, node agents, kube-state-metrics, cloud integrations and the per-target collection status of the cluster agent (databases, brokers, cloud services). A target with status 'warning' is silently not monitored: its metrics are missing from every report."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolGetMonitoringStatus,
	)
}

func (h *MCPHandler) toolGetMonitoringStatus(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return errResult, nil
	}
	now := timeseries.Now()
	world, cacheStatus, err := h.Api.LoadWorld(ctx, project, now.Add(-timeseries.Hour), now)
	if err != nil {
		klog.Errorln("mcp: get_monitoring_status:", err)
		return mcp.NewToolResultError("failed to load world"), nil
	}
	return MCPJSON(renderStatus(project, cacheStatus, world, h.Api.globalPrometheus))
}

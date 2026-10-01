package api

// shards fork: agent identities on the MCP endpoint — per-tool scope enforcement (hidden from
// tools/list + rejected on call), the approval-gate hook, sessions and the audit log.

import (
	"context"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"k8s.io/klog"
)

// MCPApprovalGate, when set, is consulted before an agent's non-read-only tool call runs (after the
// scope check). Returning a non-nil result short-circuits the call with that result — e.g. a
// "pending approval <id>: retry after a human approves it" message or a denial. The approval
// system (a separate feature) installs it; nil means no gating.
var MCPApprovalGate func(ctx context.Context, agent *db.Agent, user *db.User, tool string, args map[string]any) *mcp.CallToolResult

func mcpToolReadOnly(tool mcp.Tool) bool {
	return tool.Annotations.ReadOnlyHint != nil && *tool.Annotations.ReadOnlyHint
}

// agentToolFilter hides the tools above the calling agent's scope from tools/list.
func (h *MCPHandler) agentToolFilter(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	u := mcpUserFromContext(ctx)
	if u == nil || u.Agent == nil {
		return tools
	}
	res := make([]mcp.Tool, 0, len(tools))
	for _, t := range tools {
		if u.Agent.Scope.Allows(mcpToolScope(t.Name, mcpToolReadOnly(t))) {
			res = append(res, t)
		}
	}
	return res
}

func mcpSessionInfo(ctx context.Context) (string, string, string) {
	cs := mcpserver.ClientSessionFromContext(ctx)
	if cs == nil {
		return "", "", ""
	}
	var name, version string
	if ci, ok := cs.(mcpserver.SessionWithClientInfo); ok {
		info := ci.GetClientInfo()
		name, version = info.Name, info.Version
	}
	return cs.SessionID(), name, version
}

func mcpResultError(res *mcp.CallToolResult) string {
	if res == nil || !res.IsError {
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return "error"
}

// agentToolMiddleware enforces the agent's scope, consults the approval gate, and records the call
// in the audit log and the agent's session. Calls by non-agent users pass through unchanged.
func (h *MCPHandler) agentToolMiddleware(tool mcp.Tool, next mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
	name, readOnly := tool.Name, mcpToolReadOnly(tool)
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u := mcpUserFromContext(ctx)
		if u == nil || u.Agent == nil {
			return next(ctx, req)
		}
		agent := u.Agent
		start := time.Now()
		sessionId, clientName, clientVersion := mcpSessionInfo(ctx)
		args := req.GetArguments()
		act := &db.AgentActivity{
			AgentId:   agent.Id,
			Time:      start.UnixMilli(),
			Channel:   "mcp",
			SessionId: sessionId,
			Tool:      name,
			Args:      redactArgs(map[string]any(args)),
		}
		act.TargetType, act.TargetId = agentCallTarget(args)
		if p, _ := h.currentProject(ctx); p != nil {
			act.ProjectId = p.Id
		} else if pid, ok := args["project_id"].(string); ok {
			act.ProjectId = db.ProjectId(pid)
		}
		if sessionId != "" {
			if err := h.Api.db.TouchAgentSession(agent.Id, sessionId, clientName, clientVersion, start.UnixMilli()); err != nil {
				klog.Errorln("failed to record an agent session:", err)
			}
		}

		var res *mcp.CallToolResult
		var err error
		if need := mcpToolScope(name, readOnly); !agent.Scope.Allows(need) {
			res = mcp.NewToolResultError("forbidden: the agent's scope '" + string(agent.Scope) + "' does not allow " + name + " (needs '" + string(need) + "'). Ask a human to perform it, or to raise the agent's scope.")
		} else if act.ProjectId != "" && !agent.ProjectAllowed(act.ProjectId) {
			res = mcp.NewToolResultError("forbidden: the agent is not allowed to access this project")
		} else if !readOnly && MCPApprovalGate != nil {
			res = MCPApprovalGate(ctx, agent, u, name, args)
		}
		if res == nil {
			res, err = next(ctx, req)
		}

		act.DurationMs = time.Since(start).Milliseconds()
		act.Status = agentCallOK
		switch {
		case err != nil:
			act.Status, act.Error = agentCallError, err.Error()
		case res != nil && res.IsError:
			act.Error = mcpResultError(res)
			act.Status = agentCallError
			if strings.HasPrefix(act.Error, "forbidden") {
				act.Status = agentCallDenied
			}
		}
		h.Api.recordAgentActivity(act)
		return res, err
	}
}

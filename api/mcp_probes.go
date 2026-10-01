package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/rbac"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"k8s.io/klog"
)

// Shards fork: MCP tools for synthetic probes (uptime / TLS checks run by the server).

const maxProbeResultsWindow = 7 * timeseries.Day

func probeSpecOptions() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithString("type", mcp.Description("http | tcp | tls | dns.")),
		mcp.WithString("target", mcp.Description("http: URL (http:// or https://); tcp/tls: host:port; dns: domain name.")),
		mcp.WithString("interval", mcp.Description("How often to run, e.g. '60s', '5m' (10s..1h, default 60s).")),
		mcp.WithString("timeout", mcp.Description("Timeout per run, e.g. '10s' (default 10s, at most the interval).")),
		mcp.WithString("method", mcp.Description("http: request method (default GET).")),
		mcp.WithString("expected_status", mcp.Description("http: accepted status codes, e.g. '200-399' (default) or '200,204,301-302'.")),
		mcp.WithString("body_contains", mcp.Description("http: the response body must contain this substring.")),
		mcp.WithArray("headers", mcp.Description("http: request headers as 'Name: value' strings."), mcp.WithStringItems()),
		mcp.WithBoolean("follow_redirects", mcp.Description("http: follow redirects (default false: the redirect status itself is checked).")),
		mcp.WithBoolean("tls_skip_verify", mcp.Description("http/tls: don't verify the certificate (the expiry is still reported).")),
		mcp.WithString("dns_record_type", mcp.Description("dns: A (default) | AAAA | CNAME | MX | TXT | NS.")),
		mcp.WithString("dns_server", mcp.Description("dns: resolver host[:port] (default: the server's resolver).")),
		mcp.WithString("application_id", mcp.Description("Link to an application (id from list_applications): the results appear in its Uptime report and the probe alerts fire for it. Pass '' to unlink.")),
		mcp.WithBoolean("paused", mcp.Description("Pause the probe.")),
	}
}

func (h *MCPHandler) registerProbeTools() {
	// agent scopes (see agents_scope_shards.go): the read tools are classified by their read-only hint
	for _, tool := range []string{"create_probe", "update_probe", "delete_probe"} {
		SetMCPToolScope(tool, db.AgentScopeOperator)
	}
	h.AddTool(
		mcp.NewTool("list_probes",
			mcp.WithDescription("List the synthetic probes (HTTP/TCP/TLS/DNS uptime checks run by the shards server) with their status, uptime % and p95 latency over the last hour, TLS certificate days left and the last error."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolListProbes,
	)
	h.AddTool(
		mcp.NewTool("get_probe_results",
			mcp.WithDescription("Get the results of a probe over a time window: status, uptime %, latency percentiles, downtime periods, TLS certificate details and the last error."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Probe id or name from list_probes.")),
			mcp.WithString("window", mcp.Description("Time window, e.g. '1h' (default), '24h', '7d' (max 7d).")),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolGetProbeResults,
	)
	createOpts := append([]mcp.ToolOption{
		mcp.WithDescription("Create a synthetic probe. Probes run from the shards server (link-local/cloud metadata addresses are blocked). Built-in alerts: failing 2 runs in a row (critical), latency > 2s (warning), TLS certificate expiring in < 14 days (warning) / < 3 days (critical), invalid certificate (critical). Requires the Admin or Editor role."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Unique name: letters, digits, '.', '_', '-'.")),
	}, probeSpecOptions()...)
	createOpts = append(createOpts,
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	h.AddTool(mcp.NewTool("create_probe", createOpts...), h.toolCreateProbe)

	updateOpts := append([]mcp.ToolOption{
		mcp.WithDescription("Update a probe: only the passed fields are changed. Requires the Admin or Editor role."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Probe id or name from list_probes.")),
		mcp.WithString("name", mcp.Description("New name.")),
	}, probeSpecOptions()...)
	updateOpts = append(updateOpts,
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	h.AddTool(mcp.NewTool("update_probe", updateOpts...), h.toolUpdateProbe)

	h.AddTool(
		mcp.NewTool("delete_probe",
			mcp.WithDescription("Delete a probe. Its metrics stay in the metrics storage until they expire. Requires the Admin or Editor role."+mcpApprovalNote),
			mcp.WithString("id", mcp.Required(), mcp.Description("Probe id or name from list_probes.")),
			mcp.WithString("comment", mcp.Description("Optional reason, shown to the approver if the action needs approval.")),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		h.toolDeleteProbe,
	)
}

func (h *MCPHandler) requireProbes(ctx context.Context, edit bool) (*db.User, *db.Project, *mcp.CallToolResult) {
	user, project, errResult := h.RequireUserAndProject(ctx)
	if errResult != nil {
		return nil, nil, errResult
	}
	if edit && !h.Api.IsAllowed(user, rbac.Actions.Project(string(project.Id)).Probes().Edit()) {
		return nil, nil, mcp.NewToolResultError("forbidden: creating and changing probes requires the Admin or Editor role")
	}
	return user, project, nil
}

type mcpProbe struct {
	Id              string        `json:"id"`
	Name            string        `json:"name"`
	Type            string        `json:"type"`
	Target          string        `json:"target"`
	Interval        string        `json:"interval"`
	Paused          bool          `json:"paused,omitempty"`
	ApplicationId   string        `json:"application_id,omitempty"`
	Status          string        `json:"status"`
	UptimePercent   *float32      `json:"uptime_percent,omitempty"`
	LatencyP95      string        `json:"latency_p95,omitempty"`
	LatencyLast     string        `json:"latency_last,omitempty"`
	StatusCode      int           `json:"status_code,omitempty"`
	CertDaysLeft    *float32      `json:"cert_days_left,omitempty"`
	CertSubject     *MCPUntrusted `json:"cert_subject,omitempty"`
	CertIssuer      *MCPUntrusted `json:"cert_issuer,omitempty"`
	CertNotAfter    string        `json:"cert_not_after,omitempty"`
	CertValid       *bool         `json:"cert_valid,omitempty"`
	LastError       *MCPUntrusted `json:"last_error,omitempty"`
	LastRunAt       string        `json:"last_run_at,omitempty"`
	ConsecFailures  int           `json:"consecutive_failures,omitempty"`
	ExpectedStatus  string        `json:"expected_status,omitempty"`
	BodyContains    *MCPUntrusted `json:"body_contains,omitempty"`
	FollowRedirects bool          `json:"follow_redirects,omitempty"`
	TlsSkipVerify   bool          `json:"tls_skip_verify,omitempty"`
}

func mcpProbeLatency(v *float32) string {
	if v == nil {
		return ""
	}
	return utils.FormatLatency(*v)
}

func toMCPProbe(v ProbeView) mcpProbe {
	p := mcpProbe{
		Id: v.Id, Name: v.Name, Type: string(v.Spec.Type), Target: v.Spec.Target, Interval: v.Spec.Interval.String(),
		Paused: v.Spec.Paused, Status: v.Status, UptimePercent: v.Uptime,
		LatencyP95: mcpProbeLatency(v.LatencyP95), LatencyLast: mcpProbeLatency(v.LatencyLast), StatusCode: v.StatusCode,
		CertDaysLeft: v.CertDaysLeft, CertSubject: mcpUntrustedPtr(v.CertSubject, 500), CertIssuer: mcpUntrustedPtr(v.CertIssuer, 500), CertNotAfter: v.CertNotAfter,
		CertValid: v.CertValid, LastError: mcpUntrustedPtr(v.LastError, 2000), LastRunAt: MCPFormatTime(v.LastRunAt), ConsecFailures: v.ConsecFails,
		ExpectedStatus: v.Spec.ExpectedStatus, BodyContains: mcpUntrustedPtr(v.Spec.BodyContains, 1100), FollowRedirects: v.Spec.FollowRedirects,
		TlsSkipVerify: v.Spec.TlsSkipVerify,
	}
	if v.Linked {
		p.ApplicationId = v.ApplicationId
	}
	if p.CertDaysLeft != nil {
		d := float32(int(*p.CertDaysLeft*10)) / 10
		p.CertDaysLeft = &d
	}
	if p.UptimePercent != nil {
		u := float32(int(*p.UptimePercent*100)) / 100
		p.UptimePercent = &u
	}
	return p
}

func (h *MCPHandler) probesWithResults(ctx context.Context, project *db.Project, defs []*db.Probe, window timeseries.Duration) ([]ProbeView, *model.World) {
	now := timeseries.Now()
	world, _, err := h.Api.LoadWorld(ctx, project, now.Add(-window), now)
	if err != nil {
		klog.Warningln("mcp: probes:", err)
	}
	return probeViews(project, defs, world, false), world
}

func (h *MCPHandler) toolListProbes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, errResult := h.requireProbes(ctx, false)
	if errResult != nil {
		return errResult, nil
	}
	defs, err := h.Api.db.GetProbes(project.Id)
	if err != nil {
		klog.Errorln("mcp: list_probes:", err)
		return mcp.NewToolResultError("failed to load probes"), nil
	}
	views, _ := h.probesWithResults(ctx, project, defs, timeseries.Hour)
	res := make([]mcpProbe, 0, len(views))
	for _, v := range views {
		res = append(res, toMCPProbe(v))
	}
	return mcpJSONList(res, "no probes configured: use create_probe to add one")
}

func (h *MCPHandler) getProbeArg(project *db.Project, req mcp.CallToolRequest) (*db.Probe, *mcp.CallToolResult) {
	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return nil, mcp.NewToolResultError("id is required")
	}
	p, err := h.Api.db.GetProbeByIdOrName(project.Id, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, mcp.NewToolResultError("probe not found: use list_probes")
		}
		klog.Errorln("mcp: probe:", err)
		return nil, mcp.NewToolResultError("failed to load the probe")
	}
	return p, nil
}

type mcpProbeDowntime struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type mcpProbeResults struct {
	mcpProbe
	Window     string             `json:"window"`
	LatencyP50 string             `json:"latency_p50,omitempty"`
	LatencyMax string             `json:"latency_max,omitempty"`
	Downtime   []mcpProbeDowntime `json:"downtime,omitempty"`
	Phases     map[string]string  `json:"latest_phases,omitempty"`
	Note       string             `json:"note,omitempty"`
}

func (h *MCPHandler) toolGetProbeResults(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, errResult := h.requireProbes(ctx, false)
	if errResult != nil {
		return errResult, nil
	}
	p, errResult := h.getProbeArg(project, req)
	if errResult != nil {
		return errResult, nil
	}
	window := timeseries.Hour
	if ws := strings.TrimSpace(req.GetString("window", "")); ws != "" {
		if err := window.Set(ws); err != nil || window <= 0 {
			return mcp.NewToolResultError("invalid window: use e.g. '1h', '24h', '7d'"), nil
		}
		if window > maxProbeResultsWindow {
			window = maxProbeResultsWindow
		}
	}
	views, world := h.probesWithResults(ctx, project, []*db.Probe{p}, window)
	out := mcpProbeResults{mcpProbe: toMCPProbe(views[0]), Window: window.String()}
	var mp *model.Probe
	if world != nil {
		for _, wp := range world.Probes {
			if wp.Id == p.Id {
				mp = wp
			}
		}
	}
	if mp == nil || mp.Up.IsEmpty() {
		out.Note = "no results in the metrics storage for this window (the probe may be new, paused, or the project has no metrics storage)"
		return MCPJSON(out)
	}
	if v := mp.LatencyQuantile(0.5); !timeseries.IsNaN(v) {
		out.LatencyP50 = utils.FormatLatency(v)
	}
	if v := mp.LatencyQuantile(1); !timeseries.IsNaN(v) {
		out.LatencyMax = utils.FormatLatency(v)
	}
	var downFrom, last timeseries.Time
	iter := mp.Up.Iter()
	for iter.Next() {
		t, v := iter.Value()
		if timeseries.IsNaN(v) {
			continue
		}
		if v == 0 && downFrom == 0 {
			downFrom = t
		}
		if v > 0 && downFrom != 0 {
			out.Downtime = append(out.Downtime, mcpProbeDowntime{From: MCPFormatTime(downFrom), To: MCPFormatTime(t)})
			downFrom = 0
		}
		last = t
	}
	if downFrom != 0 {
		out.Downtime = append(out.Downtime, mcpProbeDowntime{From: MCPFormatTime(downFrom), To: "ongoing (last data at " + MCPFormatTime(last) + ")"})
	}
	if len(out.Downtime) > 50 {
		out.Downtime = out.Downtime[len(out.Downtime)-50:]
	}
	out.Phases = map[string]string{}
	for phase, ts := range mp.Durations {
		if _, v := ts.LastNotNull(); !ts.IsEmpty() && !timeseries.IsNaN(v) {
			out.Phases[phase] = utils.FormatLatency(v)
		}
	}
	return MCPJSON(out)
}

// applyProbeArgs applies the passed tool arguments to the spec (partial update).
func applyProbeArgs(spec *db.ProbeSpec, req mcp.CallToolRequest) error {
	args := req.GetArguments()
	has := func(k string) bool { _, ok := args[k]; return ok }
	if has("type") {
		spec.Type = db.ProbeType(strings.ToLower(strings.TrimSpace(req.GetString("type", ""))))
	}
	if has("target") {
		spec.Target = req.GetString("target", "")
	}
	for _, k := range []string{"interval", "timeout"} {
		if !has(k) {
			continue
		}
		var d timeseries.Duration
		if s := strings.TrimSpace(req.GetString(k, "")); s != "" {
			if err := d.Set(s); err != nil {
				return fmt.Errorf("invalid %s: use e.g. '60s', '5m'", k)
			}
		}
		if k == "interval" {
			spec.Interval = d
		} else {
			spec.Timeout = d
		}
	}
	if has("method") {
		spec.Method = req.GetString("method", "")
	}
	if has("expected_status") {
		spec.ExpectedStatus = req.GetString("expected_status", "")
	}
	if has("body_contains") {
		spec.BodyContains = req.GetString("body_contains", "")
	}
	if has("headers") {
		spec.Headers = nil
		for _, h := range req.GetStringSlice("headers", nil) {
			k, v, ok := strings.Cut(h, ":")
			if !ok {
				return fmt.Errorf("invalid header %q: use 'Name: value'", h)
			}
			spec.Headers = append(spec.Headers, utils.Header{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		}
	}
	if has("follow_redirects") {
		spec.FollowRedirects = req.GetBool("follow_redirects", false)
	}
	if has("tls_skip_verify") {
		spec.TlsSkipVerify = req.GetBool("tls_skip_verify", false)
	}
	if has("dns_record_type") {
		spec.DNSRecordType = req.GetString("dns_record_type", "")
	}
	if has("dns_server") {
		spec.DNSServer = req.GetString("dns_server", "")
	}
	if has("application_id") {
		spec.ApplicationId = strings.TrimSpace(req.GetString("application_id", ""))
	}
	if has("paused") {
		spec.Paused = req.GetBool("paused", false)
	}
	return nil
}

func (h *MCPHandler) toolCreateProbe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, errResult := h.requireProbes(ctx, true)
	if errResult != nil {
		return errResult, nil
	}
	spec := db.ProbeSpec{}
	if err := applyProbeArgs(&spec, req); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	p, err := h.Api.createProbe(project, strings.TrimSpace(req.GetString("name", "")), spec)
	if err != nil {
		return mcpTargetError(err), nil
	}
	return MCPJSON(toMCPProbe(probeViews(project, []*db.Probe{p}, nil, false)[0]))
}

func (h *MCPHandler) toolUpdateProbe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_, project, errResult := h.requireProbes(ctx, true)
	if errResult != nil {
		return errResult, nil
	}
	p, errResult := h.getProbeArg(project, req)
	if errResult != nil {
		return errResult, nil
	}
	spec := p.Spec
	if err := applyProbeArgs(&spec, req); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	name := p.Name
	if n := strings.TrimSpace(req.GetString("name", "")); n != "" {
		name = n
	}
	if err := h.Api.updateProbe(project, p, name, spec); err != nil {
		return mcpTargetError(err), nil
	}
	return MCPJSON(toMCPProbe(probeViews(project, []*db.Probe{p}, nil, false)[0]))
}

func (h *MCPHandler) toolDeleteProbe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	user, project, errResult := h.requireProbes(ctx, true)
	if errResult != nil {
		return errResult, nil
	}
	// delete_probe is a gated agent action (db.AgentActionDeleteProbe, 'auto' by default)
	return h.mcpToolGated(user, project, "delete_probe", req)
}

type probeDeleteArgs struct {
	Id      string `json:"id"`
	Comment string `json:"comment,omitempty"`
}

// probeDeleteGatedCall builds the gated call of delete_probe (see mcpGatedCall).
func (h *MCPHandler) probeDeleteGatedCall(project *db.Project, req mcp.CallToolRequest, comment string) (*gatedCall, *mcp.CallToolResult) {
	p, errResult := h.getProbeArg(project, req)
	if errResult != nil {
		return nil, errResult
	}
	return &gatedCall{action: db.AgentActionDeleteProbe, args: probeDeleteArgs{Id: p.Id, Comment: comment},
		summary: "Delete the probe \"" + p.Name + "\" (" + string(p.Spec.Type) + " " + p.Spec.Target + ")", reason: comment}, nil
}

// doDeleteProbe executes an (approved) delete_probe action.
func (api *Api) doDeleteProbe(project *db.Project, args probeDeleteArgs) (any, error) {
	p, err := api.db.GetProbe(project.Id, args.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, &targetError{status: http.StatusNotFound, msg: "probe not found"}
		}
		return nil, err
	}
	if err = api.deleteProbe(project, p.Id); err != nil {
		return nil, err
	}
	return map[string]string{"deleted": p.Id, "name": p.Name}, nil
}

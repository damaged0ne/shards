package constructor

import (
	"testing"

	"github.com/coroot/coroot/auditor"
	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type probesTestDB struct {
	DB
	probes []*db.Probe
}

func (d probesTestDB) GetProbes(projectId db.ProjectId) ([]*db.Probe, error) {
	return d.probes, nil
}

func TestProbes(t *testing.T) {
	project := &db.Project{Id: "p1"}
	appId := model.NewApplicationId("p1", "shop", model.ApplicationKindDockerSwarmService, "api")
	pdb := probesTestDB{probes: []*db.Probe{
		{Id: "a1", Name: "api-health", Spec: db.ProbeSpec{Type: db.ProbeTypeHTTP, Target: "https://api.example.com/health", Interval: timeseries.Minute, ApplicationId: appId.String()},
			State: db.ProbeState{LastRunAt: testFrom.Add(4 * testStep), Error: "unexpected status code: 503 (expected 200-399)"}},
		{Id: "b2", Name: "ext-tls", Spec: db.ProbeSpec{Type: db.ProbeTypeTLS, Target: "ext.example.com:443", Interval: timeseries.Minute}},
	}}

	m := testMetrics{}
	m.add(qShardsProbeUp, "", map[string]string{"probe_id": "a1"}, 1, 1, 0, 0)
	m.add(qShardsProbeConsecutiveFailures, "", map[string]string{"probe_id": "a1"}, 0, 0, 1, 2)
	m.add(qShardsProbeDuration, "", map[string]string{"probe_id": "a1", "phase": "total"}, 0.1, 0.2, 3, 3)
	m.add(qShardsProbeDuration, "", map[string]string{"probe_id": "a1", "phase": "connect"}, 0.01, 0.01, 0.01, 0.01)
	m.add(qShardsProbeHTTPStatusCode, "", map[string]string{"probe_id": "a1"}, 200, 200, 503, 503)
	m.add(qShardsProbeCertExpiresIn, "", map[string]string{"probe_id": "a1"}, 20*86400, 20*86400, 20*86400, 20*86400)
	m.add(qShardsProbeCertValid, "", map[string]string{"probe_id": "a1"}, 1, 1, 1, 1)

	m.add(qShardsProbeUp, "", map[string]string{"probe_id": "b2"}, 1, 1, 1, 0)
	m.add(qShardsProbeConsecutiveFailures, "", map[string]string{"probe_id": "b2"}, 0, 0, 0, 1)
	m.add(qShardsProbeDuration, "", map[string]string{"probe_id": "b2", "phase": "total"}, 0.1, 0.1, 0.1, 0.1)
	m.add(qShardsProbeCertExpiresIn, "", map[string]string{"probe_id": "b2"}, 2*86400, 2*86400, 2*86400, 2*86400)
	m.add(qShardsProbeCertValid, "", map[string]string{"probe_id": "b2"}, 1, 1, 1, 0)
	m.add(qShardsProbeCertInfo, "", map[string]string{"probe_id": "b2", "issuer": "R3", "subject": "ext.example.com", "not_after": "2030-01-01T00:00:00Z"}, 1, 1, 1, 1)
	m.add(qShardsProbeUp, "", map[string]string{"probe_id": "deleted"}, 1, 1, 1, 1)

	c := New(pdb, project, nil, nil)
	w := model.NewWorld(testFrom, testFrom.Add(4*testStep), testStep, testStep)
	app := w.GetOrCreateApplication(appId, false)
	c.loadProbes(w, m, project)

	require.Len(t, w.Probes, 2)
	p := w.ProbesOf(appId)
	require.Len(t, p, 1)
	assert.True(t, p[0].Linked)
	assert.Equal(t, float32(50), p[0].UptimePercent())
	assert.Equal(t, float32(3), p[0].LatencyQuantile(0.95))
	require.NotNil(t, p[0].IsUp())
	assert.False(t, *p[0].IsUp())

	probeAppId := model.NewApplicationId("p1", model.ProbeNamespace, model.ApplicationKindProbe, "ext-tls")
	probeApp := w.GetApplication(probeAppId)
	require.NotNil(t, probeApp, "a synthetic application is created for unlinked probes")
	ext := w.ProbesOf(probeAppId)
	require.Len(t, ext, 1)
	assert.Equal(t, "ext.example.com", ext[0].CertSubject)

	auditor.Audit(w, project, app, nil)
	checks := func(a *model.Application) map[model.CheckId]*model.Check {
		res := map[model.CheckId]*model.Check{}
		for _, r := range a.Reports {
			for _, ch := range r.Checks {
				res[ch.Id] = ch
			}
		}
		return res
	}
	ac := checks(app)
	assert.Equal(t, model.WARNING, ac[model.Checks.ProbeDown.Id].Status)
	assert.Equal(t, "1 probe is failing", ac[model.Checks.ProbeDown.Id].Message)
	assert.Equal(t, []string{"api-health: 2 failed runs in a row: unexpected status code: 503 (expected 200-399)"}, ac[model.Checks.ProbeDown.Id].Details.Items())
	assert.Equal(t, model.WARNING, ac[model.Checks.ProbeLatency.Id].Status)
	assert.Equal(t, "1 probe is slower than 2s", ac[model.Checks.ProbeLatency.Id].Message)
	assert.Equal(t, model.OK, ac[model.Checks.ProbeTLSCertExpiry.Id].Status)
	assert.Equal(t, model.OK, ac[model.Checks.ProbeTLSCertInvalid.Id].Status)
	assert.Equal(t, model.CRITICAL, app.Status)

	pc := checks(probeApp)
	require.Len(t, probeApp.Reports, 1, "only the Uptime report for probe applications")
	assert.Equal(t, model.AuditReportUptime, probeApp.Reports[0].Name)
	assert.Equal(t, model.OK, pc[model.Checks.ProbeDown.Id].Status, "a single failure doesn't fire")
	assert.Equal(t, model.WARNING, pc[model.Checks.ProbeTLSCertExpiry.Id].Status)
	assert.Equal(t, "the TLS certificate of 1 probe expires in less than 14 days", pc[model.Checks.ProbeTLSCertExpiry.Id].Message)
	assert.Equal(t, model.WARNING, pc[model.Checks.ProbeTLSCertExpiryCritical.Id].Status)
	assert.Equal(t, model.WARNING, pc[model.Checks.ProbeTLSCertInvalid.Id].Status)
}

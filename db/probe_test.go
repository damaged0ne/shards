package db

import (
	"testing"

	"github.com/coroot/coroot/timeseries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeCRUD(t *testing.T) {
	database := newTestDB(t)
	p := &Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))

	spec := ProbeSpec{Type: ProbeTypeHTTP, Target: "https://example.com/health"}
	require.NoError(t, spec.Normalize())
	assert.Equal(t, ProbeDefaultInterval, spec.Interval)
	assert.Equal(t, ProbeDefaultTimeout, spec.Timeout)
	assert.Equal(t, "GET", spec.Method)
	assert.Equal(t, "200-399", spec.ExpectedStatus)

	probe := &Probe{ProjectId: p.Id, Name: "site", Spec: spec}
	require.NoError(t, database.CreateProbe(probe))
	assert.NotEmpty(t, probe.Id)
	assert.ErrorIs(t, database.CreateProbe(&Probe{ProjectId: p.Id, Name: "site", Spec: spec}), ErrConflict)

	ok := true
	state := ProbeState{LastRunAt: 100, Up: true, Duration: 0.25, StatusCode: 200, CertNotAfter: 1000, CertValid: &ok}
	require.NoError(t, database.UpdateProbeState(p.Id, probe.Id, state))

	got, err := database.GetProbe(p.Id, probe.Id)
	require.NoError(t, err)
	assert.Equal(t, "site", got.Name)
	assert.Equal(t, spec, got.Spec)
	assert.Equal(t, state, got.State)

	got, err = database.GetProbeByIdOrName(p.Id, "site")
	require.NoError(t, err)
	assert.Equal(t, probe.Id, got.Id)

	got.Name = "site2"
	got.Spec.Interval = 5 * timeseries.Minute
	require.NoError(t, database.UpdateProbe(got))
	got, err = database.GetProbe(p.Id, probe.Id)
	require.NoError(t, err)
	assert.Equal(t, "site2", got.Name)
	assert.Equal(t, 5*timeseries.Minute, got.Spec.Interval)
	assert.Equal(t, ProbeState{}, got.State, "the state is reset on update")

	all, err := database.GetAllProbes()
	require.NoError(t, err)
	assert.Len(t, all, 1)

	require.NoError(t, database.DeleteProject(p.Id))
	all, err = database.GetAllProbes()
	require.NoError(t, err)
	assert.Len(t, all, 0)
	assert.ErrorIs(t, database.DeleteProbe(p.Id, probe.Id), ErrNotFound)
}

func TestProbeSpecNormalize(t *testing.T) {
	for _, tc := range []struct {
		spec ProbeSpec
		err  string
	}{
		{ProbeSpec{Type: ProbeTypeHTTP, Target: "ftp://example.com"}, "http:// or https://"},
		{ProbeSpec{Type: ProbeTypeHTTP, Target: "http://example.com", ExpectedStatus: "2xx"}, "invalid expected status"},
		{ProbeSpec{Type: ProbeTypeHTTP, Target: "http://example.com", ExpectedStatus: "300-200"}, "invalid expected status"},
		{ProbeSpec{Type: ProbeTypeHTTP, Target: "http://example.com", Interval: timeseries.Second}, "interval"},
		{ProbeSpec{Type: ProbeTypeHTTP, Target: "http://example.com", Interval: timeseries.Minute, Timeout: 2 * timeseries.Minute}, "timeout"},
		{ProbeSpec{Type: ProbeTypeTCP, Target: "example.com"}, "host:port"},
		{ProbeSpec{Type: ProbeTypeDNS, Target: "example.com", DNSRecordType: "SRV"}, "unsupported DNS record type"},
		{ProbeSpec{Type: "icmp", Target: "example.com"}, "unknown probe type"},
		{ProbeSpec{Type: ProbeTypeTLS, Target: "example.com:443"}, ""},
		{ProbeSpec{Type: ProbeTypeHTTP, Target: "http://example.com", ExpectedStatus: "200, 301-302"}, ""},
	} {
		err := tc.spec.Normalize()
		if tc.err == "" {
			assert.NoError(t, err)
		} else if assert.Error(t, err) {
			assert.Contains(t, err.Error(), tc.err)
		}
	}
	s := ProbeSpec{Type: ProbeTypeDNS, Target: "example.com", DNSServer: "1.1.1.1", Method: "POST"}
	require.NoError(t, s.Normalize())
	assert.Equal(t, "1.1.1.1:53", s.DNSServer)
	assert.Equal(t, "A", s.DNSRecordType)
	assert.Empty(t, s.Method)

	assert.NoError(t, ValidateProbeName("api.example-1"))
	assert.Error(t, ValidateProbeName("with space"))
	assert.Error(t, ValidateProbeName(""))
}

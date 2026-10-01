package probes

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/timeseries"
	"github.com/coroot/coroot/utils"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRunner(t *testing.T, allowed ...string) *Runner {
	g, err := NewGuard(allowed)
	require.NoError(t, err)
	return NewRunner(g)
}

func httpSpec(url string) db.ProbeSpec {
	s := db.ProbeSpec{Type: db.ProbeTypeHTTP, Target: url, Timeout: 2 * timeseries.Second}
	if err := s.Normalize(); err != nil {
		panic(err)
	}
	return s
}

func TestHTTPProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			assert.Equal(t, "secret", r.Header.Get("X-Token"))
			_, _ = w.Write([]byte("status: healthy"))
		case "/500":
			w.WriteHeader(http.StatusInternalServerError)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/slow":
			time.Sleep(1500 * time.Millisecond)
		}
	}))
	defer srv.Close()
	r := testRunner(t)
	ctx := context.Background()

	spec := httpSpec(srv.URL + "/ok")
	spec.Headers = []utils.Header{{Key: "X-Token", Value: "secret"}}
	spec.BodyContains = "healthy"
	res := r.Run(ctx, spec)
	assert.True(t, res.Up, res.Error)
	assert.Equal(t, 200, res.StatusCode)
	assert.Empty(t, res.Error)
	for _, phase := range []string{PhaseDNS, PhaseConnect, PhaseTTFB, PhaseTotal} {
		assert.Contains(t, res.Phases, phase)
	}
	assert.NotContains(t, res.Phases, PhaseTLS)
	assert.Nil(t, res.Cert)

	spec.BodyContains = "unhealthy"
	res = r.Run(ctx, spec)
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, `does not contain "unhealthy"`)

	res = r.Run(ctx, httpSpec(srv.URL+"/500"))
	assert.False(t, res.Up)
	assert.Equal(t, 500, res.StatusCode)
	assert.Contains(t, res.Error, "unexpected status code: 500")

	spec = httpSpec(srv.URL + "/500")
	spec.ExpectedStatus = "500-503"
	assert.True(t, r.Run(ctx, spec).Up)

	spec = httpSpec(srv.URL + "/redirect")
	res = r.Run(ctx, spec)
	assert.True(t, res.Up) // 302 is within the default 200-399
	assert.Equal(t, 302, res.StatusCode)
	spec.FollowRedirects = true
	spec.BodyContains = "healthy"
	spec.Headers = []utils.Header{{Key: "X-Token", Value: "secret"}}
	res = r.Run(ctx, spec)
	assert.True(t, res.Up, res.Error)
	assert.Equal(t, 200, res.StatusCode)

	spec = httpSpec(srv.URL + "/slow")
	spec.Timeout = timeseries.Second
	res = r.Run(ctx, spec)
	assert.False(t, res.Up)
	assert.Equal(t, "timeout", res.Error)
	assert.GreaterOrEqual(t, res.Phases[PhaseTotal], 0.9)

	// down: nothing listens on the port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	_ = l.Close()
	res = r.Run(ctx, httpSpec("http://"+addr+"/"))
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, "refused")
}

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTestCA(t *testing.T) *testCA {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "shards test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

// leaf issues a short-lived server certificate.
func (ca *testCA) leaf(t *testing.T, notAfter time.Time, dnsNames []string, ips ...net.IP) tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "probe-test.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		DNSNames:     dnsNames,
		IPAddresses:  ips,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func tlsServer(cert tls.Certificate) *httptest.Server {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	return srv
}

func TestTLSCertificates(t *testing.T) {
	ca := newTestCA(t)
	r := testRunner(t)
	r.Roots = ca.pool
	ctx := context.Background()

	// valid, expires in 2 days
	notAfter := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	srv := tlsServer(ca.leaf(t, notAfter, nil, net.ParseIP("127.0.0.1")))
	defer srv.Close()
	res := r.Run(ctx, httpSpec(srv.URL))
	require.True(t, res.Up, res.Error)
	require.NotNil(t, res.Cert)
	assert.True(t, res.Cert.Verified)
	assert.True(t, res.Cert.Valid)
	assert.Equal(t, notAfter.Unix(), res.Cert.NotAfter.Unix())
	assert.Equal(t, "shards test CA", res.Cert.Issuer)
	assert.Equal(t, "probe-test.local", res.Cert.Subject)
	assert.Contains(t, res.Phases, PhaseTLS)

	host := strings.TrimPrefix(srv.URL, "https://")
	tlsSpec := db.ProbeSpec{Type: db.ProbeTypeTLS, Target: host}
	require.NoError(t, tlsSpec.Normalize())
	res = r.Run(ctx, tlsSpec)
	require.True(t, res.Up, res.Error)
	assert.InDelta(t, 2, res.Cert.NotAfter.Sub(time.Now()).Hours()/24, 0.01)

	// hostname mismatch: the certificate is issued for example.com only
	srv2 := tlsServer(ca.leaf(t, notAfter, []string{"example.com"}))
	defer srv2.Close()
	res = r.Run(ctx, httpSpec(srv2.URL))
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, "invalid certificate")
	require.NotNil(t, res.Cert)
	assert.True(t, res.Cert.Verified)
	assert.False(t, res.Cert.Valid)

	// skip verify: up, the certificate info is still collected
	spec := httpSpec(srv2.URL)
	spec.TlsSkipVerify = true
	res = r.Run(ctx, spec)
	assert.True(t, res.Up, res.Error)
	require.NotNil(t, res.Cert)
	assert.False(t, res.Cert.Verified)

	// expired
	srv3 := tlsServer(ca.leaf(t, time.Now().Add(-time.Minute), nil, net.ParseIP("127.0.0.1")))
	defer srv3.Close()
	res = r.Run(ctx, httpSpec(srv3.URL))
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, "expired")

	// untrusted CA (system roots)
	res = testRunner(t).Run(ctx, httpSpec(srv.URL))
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, "invalid certificate")
}

func TestTCPAndDNSProbes(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	r := testRunner(t)
	spec := db.ProbeSpec{Type: db.ProbeTypeTCP, Target: l.Addr().String()}
	require.NoError(t, spec.Normalize())
	res := r.Run(context.Background(), spec)
	assert.True(t, res.Up, res.Error)
	assert.Contains(t, res.Phases, PhaseConnect)

	spec = db.ProbeSpec{Type: db.ProbeTypeDNS, Target: "localhost"}
	require.NoError(t, spec.Normalize())
	res = r.Run(context.Background(), spec)
	assert.True(t, res.Up, res.Error)
	assert.Contains(t, res.Answers, "127.0.0.1")
}

func TestGuard(t *testing.T) {
	g, err := NewGuard(nil)
	require.NoError(t, err)
	for _, a := range []string{"169.254.169.254", "169.254.0.1", "fd00:ec2::254", "::ffff:169.254.169.254", "fe80::1"} {
		assert.ErrorIs(t, g.Check(netip.MustParseAddr(a)), ErrBlockedAddress, a)
	}
	for _, a := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "8.8.8.8", "::1", "fd00:ec2::253"} {
		assert.NoError(t, g.Check(netip.MustParseAddr(a)), a)
	}

	g, err = NewGuard([]string{"169.254.10.0/24", "fd00:ec2::254"})
	require.NoError(t, err)
	assert.NoError(t, g.Check(netip.MustParseAddr("169.254.10.5")))
	assert.NoError(t, g.Check(netip.MustParseAddr("fd00:ec2::254")))
	assert.ErrorIs(t, g.Check(netip.MustParseAddr("169.254.169.254")), ErrBlockedAddress)

	_, err = NewGuard([]string{"not-a-cidr"})
	assert.Error(t, err)
}

func TestSSRF(t *testing.T) {
	r := testRunner(t)
	ctx := context.Background()

	res := r.Run(ctx, httpSpec("http://169.254.169.254/latest/meta-data/"))
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, "blocked")

	res = r.Run(ctx, httpSpec("http://[fd00:ec2::254]/latest/meta-data/"))
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, "blocked")

	spec := db.ProbeSpec{Type: db.ProbeTypeTCP, Target: "169.254.169.254:80"}
	require.NoError(t, spec.Normalize())
	res = r.Run(ctx, spec)
	assert.False(t, res.Up)
	assert.True(t, strings.Contains(res.Error, "blocked"), res.Error)

	// a redirect to the metadata endpoint is blocked too
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()
	spec = httpSpec(srv.URL)
	spec.FollowRedirects = true
	res = r.Run(ctx, spec)
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, "blocked")

	// a custom DNS server on a blocked address
	spec = db.ProbeSpec{Type: db.ProbeTypeDNS, Target: "example.com", DNSServer: "169.254.169.253"}
	require.NoError(t, spec.Normalize())
	res = r.Run(ctx, spec)
	assert.False(t, res.Up)
	assert.Contains(t, res.Error, "blocked")
}

func TestWriteRequest(t *testing.T) {
	p := &db.Probe{Id: "abc", Name: "site", Spec: db.ProbeSpec{Type: db.ProbeTypeHTTP, Target: "https://example.com"}}
	notAfter := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	res := &Result{
		Up: true, StatusCode: 200, Time: time.UnixMilli(1700000000000),
		Phases: map[string]float64{PhaseDNS: 0.01, PhaseConnect: 0.02, PhaseTLS: 0.03, PhaseTTFB: 0.04, PhaseTotal: 0.1},
		Cert:   &CertInfo{NotAfter: notAfter, Issuer: "CA", Subject: "example.com", Verified: true, Valid: true},
	}
	req := WriteRequest(p, res, 0)
	series := map[string]prompb.TimeSeries{}
	for _, ts := range req.Timeseries {
		var name, phase string
		for i, l := range ts.Labels {
			if i > 0 {
				assert.Less(t, ts.Labels[i-1].Name, l.Name, "labels must be sorted")
			}
			switch l.Name {
			case "__name__":
				name = l.Value
			case "phase":
				phase = l.Value
			}
		}
		require.Len(t, ts.Samples, 1)
		assert.Equal(t, int64(1700000000000), ts.Samples[0].Timestamp)
		series[name+"/"+phase] = ts
	}
	assert.Equal(t, 1., series["shards_probe_up/"].Samples[0].Value)
	assert.Equal(t, 200., series["shards_probe_http_status_code/"].Samples[0].Value)
	for _, phase := range Phases {
		assert.Contains(t, series, "shards_probe_duration_seconds/"+phase)
	}
	assert.Equal(t, float64(notAfter.Unix()), series["shards_probe_tls_cert_expiry_seconds/"].Samples[0].Value)
	assert.Equal(t, 1., series["shards_probe_tls_cert_valid/"].Samples[0].Value)
	info := series["shards_probe_tls_cert_info/"]
	labels := map[string]string{}
	for _, l := range info.Labels {
		labels[l.Name] = l.Value
	}
	assert.Equal(t, map[string]string{
		"__name__": "shards_probe_tls_cert_info", "probe_id": "abc", "probe_name": "site", "probe_type": "http",
		"target": "https://example.com", "issuer": "CA", "subject": "example.com", "not_after": "2030-01-02T03:04:05Z",
	}, labels)
	assert.Contains(t, series, "shards_probe_consecutive_failures/")
}

func TestNextState(t *testing.T) {
	down := &Result{Up: false, Error: "timeout", Time: time.Unix(100, 0), Phases: map[string]float64{PhaseTotal: 1}}
	up := &Result{Up: true, Time: time.Unix(160, 0), Phases: map[string]float64{PhaseTotal: 0.2}}
	st := NextState(db.ProbeState{}, down)
	assert.Equal(t, 1, st.ConsecutiveFailures)
	st = NextState(st, down)
	assert.Equal(t, 2, st.ConsecutiveFailures)
	assert.Equal(t, "timeout", st.Error)
	st = NextState(st, up)
	assert.Equal(t, 0, st.ConsecutiveFailures)
	assert.True(t, st.Up)
	assert.Equal(t, timeseries.Time(160), st.LastUpAt)
}

type fakeStore struct {
	lock     sync.Mutex
	probes   []*db.Probe
	projects map[string]*db.Project
	states   map[string]db.ProbeState
}

func (s *fakeStore) GetAllProbes() ([]*db.Probe, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	var res []*db.Probe
	for _, p := range s.probes {
		cp := *p
		res = append(res, &cp)
	}
	return res, nil
}
func (s *fakeStore) GetProjects() (map[string]*db.Project, error) { return s.projects, nil }
func (s *fakeStore) UpdateProbeState(projectId db.ProjectId, id string, state db.ProbeState) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.states[id] = state
	return nil
}
func (s *fakeStore) GetPrimaryLock(ctx context.Context) bool { return true }

type fakeWriter struct {
	lock sync.Mutex
	reqs map[db.ProjectId]int
	err  error
}

func (w *fakeWriter) WriteMetrics(ctx context.Context, projectId db.ProjectId, req *prompb.WriteRequest) error {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.reqs[projectId]++
	return w.err
}

func TestScheduler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	store := &fakeStore{
		projects: map[string]*db.Project{"project-1": {Id: "p1", Name: "project-1"}, "project-2": {Id: "p2", Name: "project-2"}},
		states:   map[string]db.ProbeState{},
	}
	mk := func(id string, project db.ProjectId) *db.Probe {
		return &db.Probe{Id: id, ProjectId: project, Name: id, Spec: httpSpec(srv.URL), UpdatedAt: 1}
	}
	store.probes = []*db.Probe{mk("a", "p1"), mk("b", "p2")}
	paused := mk("c", "p1")
	paused.Spec.Paused = true
	store.probes = append(store.probes, paused)
	writer := &fakeWriter{reqs: map[db.ProjectId]int{}, err: errors.New("no storage")}
	s := NewScheduler(store, writer, testRunner(t), 1)

	s.sync()
	require.Len(t, s.entries, 2) // the paused probe is not scheduled
	for _, e := range s.entries {
		assert.True(t, e.next.Before(time.Now().Add(time.Minute+time.Second)), "jitter within the interval")
		e.next = time.Now().Add(-time.Second)
	}
	// concurrency 1: only one probe runs per dispatch
	s.dispatch(context.Background())
	s.Wait()
	s.dispatch(context.Background())
	s.Wait()
	assert.Len(t, store.states, 2)
	assert.True(t, store.states["a"].Up)
	assert.Equal(t, 1, writer.reqs["p1"])
	assert.Equal(t, 1, writer.reqs["p2"])
	for _, e := range s.entries {
		assert.True(t, e.next.After(time.Now()), "rescheduled")
	}

	// the project p2 is deleted: its probes are dropped
	delete(store.projects, "project-2")
	s.sync()
	require.Len(t, s.entries, 1)
	_, ok := s.entries["p1/a"]
	assert.True(t, ok)

	// a changed probe is rescheduled to run soon
	store.probes[0].UpdatedAt = 2
	store.probes[0].Spec.Interval = 10 * timeseries.Minute
	s.sync()
	assert.True(t, s.entries["p1/a"].next.Before(time.Now().Add(3*time.Second)))
	assert.Equal(t, 10*timeseries.Minute, s.entries["p1/a"].probe.Spec.Interval)

	// deleted probe
	store.probes = store.probes[1:]
	s.sync()
	assert.Len(t, s.entries, 0)
}

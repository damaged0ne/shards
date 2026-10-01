package probes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
)

const (
	PhaseDNS     = "dns"
	PhaseConnect = "connect"
	PhaseTLS     = "tls"
	PhaseTTFB    = "ttfb"
	PhaseTotal   = "total"

	maxBodySize  = 1 << 20
	maxRedirects = 10
	userAgent    = "shards-probe/1.0"
)

var (
	now   = time.Now
	since = func(t time.Time) float64 { return time.Since(t).Seconds() }
)

type CertInfo struct {
	NotAfter time.Time `json:"not_after"`
	Issuer   string    `json:"issuer"`
	Subject  string    `json:"subject"`
	// Verified is false if the verification wasn't performed (tls_skip_verify).
	Verified bool   `json:"verified"`
	Valid    bool   `json:"valid"`
	Error    string `json:"error,omitempty"`
}

type Result struct {
	Up         bool               `json:"up"`
	Error      string             `json:"error,omitempty"`
	StatusCode int                `json:"status_code,omitempty"`
	Phases     map[string]float64 `json:"phases"` // seconds
	Cert       *CertInfo          `json:"cert,omitempty"`
	Answers    []string           `json:"answers,omitempty"`
	Time       time.Time          `json:"time"`
}

func (r *Result) fail(format string, a ...any) *Result {
	r.Up = false
	r.Error = fmt.Sprintf(format, a...)
	return r
}

// Runner executes probes.
type Runner struct {
	Guard *Guard
	// Roots overrides the system root CAs (used in tests).
	Roots *x509.CertPool
}

func NewRunner(guard *Guard) *Runner {
	return &Runner{Guard: guard}
}

// Run executes the probe once. It never returns nil.
func (r *Runner) Run(ctx context.Context, spec db.ProbeSpec) *Result {
	timeout := spec.Timeout.ToStandard()
	if timeout <= 0 {
		timeout = db.ProbeDefaultTimeout.ToStandard()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res := &Result{Phases: map[string]float64{}, Time: now()}
	start := now()
	switch spec.Type {
	case db.ProbeTypeHTTP:
		r.http(ctx, spec, res)
	case db.ProbeTypeTCP:
		r.tcp(ctx, spec, res)
	case db.ProbeTypeTLS:
		r.tls(ctx, spec, res)
	case db.ProbeTypeDNS:
		r.dns(ctx, spec, res)
	default:
		res.fail("unknown probe type: %s", spec.Type)
	}
	res.Phases[PhaseTotal] = since(start)
	if !res.Up && res.Error == "" {
		res.Error = "failed"
	}
	if res.Up {
		res.Error = ""
	}
	return res
}

// verifier collects the certificate info and, unless skipVerify is set, rejects invalid certificates.
// Go's built-in verification is disabled so that the certificate info is available even for invalid certificates.
func (r *Runner) tlsConfig(serverName func() string, skipVerify bool, res *Result) *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("no certificates presented")
			}
			leaf := cs.PeerCertificates[0]
			info := &CertInfo{
				NotAfter: leaf.NotAfter,
				Issuer:   certName(leaf.Issuer.CommonName, leaf.Issuer.Organization),
				Subject:  certName(leaf.Subject.CommonName, leaf.Subject.Organization),
			}
			for _, c := range cs.PeerCertificates[1:] {
				// the earliest expiry in the presented chain: an expired intermediate breaks the chain too
				if c.NotAfter.Before(info.NotAfter) {
					info.NotAfter = c.NotAfter
				}
			}
			res.Cert = info
			if skipVerify {
				return nil
			}
			info.Verified = true
			name := cs.ServerName
			if name == "" {
				name = serverName()
			}
			opts := x509.VerifyOptions{DNSName: name, Roots: r.Roots, Intermediates: x509.NewCertPool()}
			for _, c := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(c)
			}
			if _, err := leaf.Verify(opts); err != nil {
				info.Error = err.Error()
				return fmt.Errorf("invalid certificate: %w", err)
			}
			info.Valid = true
			return nil
		},
	}
}

func certName(cn string, org []string) string {
	if cn != "" {
		return cn
	}
	return strings.Join(org, ", ")
}

func (r *Runner) http(ctx context.Context, spec db.ProbeSpec, res *Result) {
	u, err := url.Parse(spec.Target)
	if err != nil {
		res.fail("invalid url: %s", err)
		return
	}
	statusRanges, err := spec.StatusRanges()
	if err != nil {
		res.fail("%s", err)
		return
	}
	host := u.Hostname()
	timings := &dialTimings{}
	var tlsStart time.Time
	var ready, firstByte time.Time
	transport := &http.Transport{
		Proxy: nil, // probes connect directly: a proxy would bypass the address guard
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return r.Guard.dial(ctx, network, addr, timings)
		},
		TLSClientConfig:     r.tlsConfig(func() string { return host }, spec.TlsSkipVerify, res),
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: spec.Timeout.ToStandard(),
		ForceAttemptHTTP2:   true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !spec.FollowRedirects {
				return http.ErrUseLastResponse
			}
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			host = req.URL.Hostname()
			return nil
		},
	}
	method := spec.Method
	if method == "" {
		method = http.MethodGet
	}
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() { tlsStart = now() },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			if !tlsStart.IsZero() {
				res.Phases[PhaseTLS] += since(tlsStart)
			}
		},
		WroteRequest:         func(httptrace.WroteRequestInfo) { ready = now() },
		GotFirstResponseByte: func() { firstByte = now() },
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), method, spec.Target, nil)
	if err != nil {
		res.fail("invalid request: %s", err)
		return
	}
	req.Header.Set("User-Agent", userAgent)
	for _, h := range spec.Headers {
		if strings.EqualFold(h.Key, "host") {
			req.Host = h.Value
			continue
		}
		req.Header.Set(h.Key, h.Value)
	}
	resp, err := client.Do(req)
	res.Phases[PhaseDNS] = timings.dns
	res.Phases[PhaseConnect] = timings.connect
	if u.Scheme != "https" && res.Phases[PhaseTLS] == 0 {
		delete(res.Phases, PhaseTLS)
	}
	if err != nil {
		res.fail("%s", httpError(err))
		return
	}
	defer resp.Body.Close()
	if !ready.IsZero() && !firstByte.IsZero() && firstByte.After(ready) {
		res.Phases[PhaseTTFB] = firstByte.Sub(ready).Seconds()
	}
	res.StatusCode = resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		res.fail("failed to read the response body: %s", err)
		return
	}
	ok := false
	for _, sr := range statusRanges {
		if resp.StatusCode >= sr[0] && resp.StatusCode <= sr[1] {
			ok = true
			break
		}
	}
	if !ok {
		res.fail("unexpected status code: %d (expected %s)", resp.StatusCode, spec.ExpectedStatus)
		return
	}
	if spec.BodyContains != "" && !strings.Contains(string(body), spec.BodyContains) {
		res.fail("the response body does not contain %q", spec.BodyContains)
		return
	}
	res.Up = true
}

func httpError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return err.Error()
}

func netError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return err.Error()
}

func (r *Runner) tcp(ctx context.Context, spec db.ProbeSpec, res *Result) {
	timings := &dialTimings{}
	conn, err := r.Guard.dial(ctx, "tcp", spec.Target, timings)
	res.Phases[PhaseDNS] = timings.dns
	res.Phases[PhaseConnect] = timings.connect
	if err != nil {
		res.fail("%s", netError(err))
		return
	}
	_ = conn.Close()
	res.Up = true
}

func (r *Runner) tls(ctx context.Context, spec db.ProbeSpec, res *Result) {
	host, _, err := net.SplitHostPort(spec.Target)
	if err != nil {
		res.fail("invalid target: %s", err)
		return
	}
	timings := &dialTimings{}
	conn, err := r.Guard.dial(ctx, "tcp", spec.Target, timings)
	res.Phases[PhaseDNS] = timings.dns
	res.Phases[PhaseConnect] = timings.connect
	if err != nil {
		res.fail("%s", netError(err))
		return
	}
	defer conn.Close()
	cfg := r.tlsConfig(func() string { return host }, spec.TlsSkipVerify, res)
	if _, err := netip.ParseAddr(host); err != nil {
		cfg.ServerName = host
	}
	start := now()
	tlsConn := tls.Client(conn, cfg)
	err = tlsConn.HandshakeContext(ctx)
	res.Phases[PhaseTLS] = since(start)
	if err != nil {
		res.fail("%s", netError(err))
		return
	}
	res.Up = true
}

func (r *Runner) dns(ctx context.Context, spec db.ProbeSpec, res *Result) {
	resolver := net.DefaultResolver
	if spec.DNSServer != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return r.Guard.dial(ctx, network, spec.DNSServer, nil)
			},
		}
	}
	name := spec.Target
	start := now()
	var answers []string
	var err error
	switch spec.DNSRecordType {
	case "", "A", "AAAA":
		network := "ip4"
		if spec.DNSRecordType == "AAAA" {
			network = "ip6"
		}
		var ips []netip.Addr
		ips, err = resolver.LookupNetIP(ctx, network, name)
		for _, ip := range ips {
			answers = append(answers, ip.Unmap().String())
		}
	case "CNAME":
		var cname string
		cname, err = resolver.LookupCNAME(ctx, name)
		if cname != "" {
			answers = append(answers, cname)
		}
	case "MX":
		var mxs []*net.MX
		mxs, err = resolver.LookupMX(ctx, name)
		for _, mx := range mxs {
			answers = append(answers, fmt.Sprintf("%d %s", mx.Pref, mx.Host))
		}
	case "TXT":
		answers, err = resolver.LookupTXT(ctx, name)
	case "NS":
		var nss []*net.NS
		nss, err = resolver.LookupNS(ctx, name)
		for _, ns := range nss {
			answers = append(answers, ns.Host)
		}
	default:
		err = fmt.Errorf("unsupported record type: %s", spec.DNSRecordType)
	}
	res.Phases[PhaseDNS] = since(start)
	if err != nil {
		res.fail("%s", netError(err))
		return
	}
	if len(answers) == 0 {
		res.fail("no %s records found", spec.DNSRecordType)
		return
	}
	sort.Strings(answers)
	res.Answers = answers
	res.Up = true
}

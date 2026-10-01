package notifications

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBaseUrl = "https://shards.example.com"

func testAlert(status model.Status) *db.AlertNotification {
	n := &db.AlertNotification{
		ProjectId:     "p1",
		AlertId:       "a1",
		ApplicationId: model.NewApplicationId("p1", "shop", model.ApplicationKindDeployment, "api"),
		Status:        status,
		Details: &db.AlertNotificationDetails{
			ProjectName: "prod",
			RuleName:    "Probe is failing",
			Summary:     "1 probe is failing <html>",
			Details: []model.AlertDetail{
				{Name: "Findings", Value: "api-health: 2 failed runs in a row"},
				{Name: "Log", Value: "line1\nline2", Code: true},
			},
		},
	}
	if status == model.OK {
		n.Details.Duration = "5m"
		n.Details.ResolvedBy = "alice"
	}
	return n
}

func testIncident() *db.IncidentNotification {
	return &db.IncidentNotification{
		ProjectId:     "p1",
		ApplicationId: model.NewApplicationId("p1", "shop", model.ApplicationKindDeployment, "api"),
		IncidentKey:   "inc1",
		Status:        model.WARNING,
		Details: &db.IncidentNotificationDetails{Reports: []db.IncidentNotificationDetailsReport{
			{Name: model.AuditReportNetwork, Check: "Network RTT", Message: "high latency"},
		}},
	}
}

type capture struct {
	lock   sync.Mutex
	paths  []string
	bodies []map[string]any
	status int
	reply  string
}

func (c *capture) server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		require.NoError(t, json.Unmarshal(data, &body))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		c.lock.Lock()
		c.paths = append(c.paths, r.URL.Path)
		c.bodies = append(c.bodies, body)
		c.lock.Unlock()
		if c.status != 0 {
			w.WriteHeader(c.status)
		}
		_, _ = w.Write([]byte(c.reply))
	}))
}

func TestTelegram(t *testing.T) {
	c := &capture{reply: `{"ok":true}`}
	srv := c.server(t)
	defer srv.Close()
	tg := NewTelegram(&db.IntegrationTelegram{BotToken: "123:ABC", ChatId: "-100200", MessageThreadId: 7})
	tg.apiUrl = srv.URL
	ctx := context.Background()

	require.NoError(t, tg.SendAlert(ctx, testBaseUrl, testAlert(model.CRITICAL)))
	require.Len(t, c.bodies, 1)
	assert.Equal(t, "/bot123:ABC/sendMessage", c.paths[0])
	b := c.bodies[0]
	assert.Equal(t, "-100200", b["chat_id"])
	assert.Equal(t, "HTML", b["parse_mode"])
	assert.Equal(t, float64(7), b["message_thread_id"])
	text := b["text"].(string)
	assert.Contains(t, text, "<b>[CRITICAL] api: 1 probe is failing &lt;html&gt;</b>")
	assert.Contains(t, text, "<b>Project</b>: prod")
	assert.Contains(t, text, "<pre>line1\nline2</pre>")
	assert.Contains(t, text, `<a href="https://shards.example.com/p/p1/alerts?alert=a1">View alert</a>`)

	require.NoError(t, tg.SendIncident(ctx, testBaseUrl, testIncident()))
	text = c.bodies[1]["text"].(string)
	assert.Contains(t, text, "[WARNING] api is not meeting its SLOs")
	assert.Contains(t, text, "• Net / Network RTT: high latency")
	assert.Contains(t, text, "/p/p1/incidents?incident=inc1")

	require.NoError(t, tg.SendComment(ctx, CommentTemplateValues{Author: "bob", Title: "Incident inc1", Body: "restarted <db>", URL: testBaseUrl + "/x"}))
	assert.Contains(t, c.bodies[2]["text"].(string), "bob commented on Incident inc1")
	assert.Contains(t, c.bodies[2]["text"].(string), "restarted &lt;db&gt;")

	c.status, c.reply = http.StatusBadRequest, `{"ok":false,"description":"Bad Request: chat not found"}`
	err := tg.SendAlert(ctx, testBaseUrl, testAlert(model.CRITICAL))
	require.Error(t, err)
	assert.Equal(t, "telegram: Bad Request: chat not found", err.Error())

	// the bot token must not leak through connection errors
	tg.apiUrl = "http://127.0.0.1:1"
	err = tg.SendAlert(ctx, testBaseUrl, testAlert(model.CRITICAL))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "123:ABC")
}

func TestDiscord(t *testing.T) {
	c := &capture{status: http.StatusNoContent}
	srv := c.server(t)
	defer srv.Close()
	d := NewDiscord(&db.IntegrationDiscord{WebhookUrl: srv.URL + "/api/webhooks/1/token"})
	ctx := context.Background()

	require.NoError(t, d.SendAlert(ctx, testBaseUrl, testAlert(model.CRITICAL)))
	require.NoError(t, d.SendAlert(ctx, testBaseUrl, testAlert(model.OK)))
	require.NoError(t, d.SendIncident(ctx, testBaseUrl, testIncident()))
	require.Len(t, c.bodies, 3)
	assert.Equal(t, "/api/webhooks/1/token", c.paths[0])

	embed := func(i int) map[string]any { return c.bodies[i]["embeds"].([]any)[0].(map[string]any) }
	e := embed(0)
	assert.Equal(t, "[CRITICAL] api: 1 probe is failing <html>", e["title"])
	assert.Equal(t, float64(0xf44034), e["color"])
	assert.Equal(t, testBaseUrl+"/p/p1/alerts?alert=a1", e["url"])
	fields := e["fields"].([]any)
	assert.Equal(t, "Project", fields[0].(map[string]any)["name"])
	assert.Equal(t, "```\nline1\nline2\n```", fields[3].(map[string]any)["value"])
	assert.Equal(t, []any{}, c.bodies[0]["allowed_mentions"].(map[string]any)["parse"])

	e = embed(1)
	assert.Equal(t, "api alert manually resolved by alice (duration: 5m)", e["title"])
	assert.Equal(t, float64(0x23d160), e["color"])

	e = embed(2)
	assert.Equal(t, float64(0xffdd57), e["color"])
	assert.Equal(t, "• Net / Network RTT: high latency", e["description"])

	c.status, c.reply = http.StatusNotFound, `{"message": "Unknown Webhook"}`
	err := d.SendAlert(ctx, testBaseUrl, testAlert(model.CRITICAL))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown Webhook")
}

func TestMattermost(t *testing.T) {
	c := &capture{reply: "ok"}
	srv := c.server(t)
	defer srv.Close()
	mm := NewMattermost(&db.IntegrationMattermost{WebhookUrl: srv.URL + "/hooks/xyz", Channel: "ops", Username: "shards"})
	ctx := context.Background()

	require.NoError(t, mm.SendAlert(ctx, testBaseUrl, testAlert(model.CRITICAL)))
	require.Len(t, c.bodies, 1)
	b := c.bodies[0]
	assert.Equal(t, "ops", b["channel"])
	assert.Equal(t, "shards", b["username"])
	a := b["attachments"].([]any)[0].(map[string]any)
	assert.Equal(t, "#f44034", a["color"])
	assert.Equal(t, "[CRITICAL] api: 1 probe is failing <html>", a["title"])
	assert.Equal(t, testBaseUrl+"/p/p1/alerts?alert=a1", a["title_link"])
	assert.Equal(t, "[CRITICAL] api: 1 probe is failing <html>", a["fallback"])
	fields := a["fields"].([]any)
	assert.Len(t, fields, 4)
	assert.Equal(t, true, fields[0].(map[string]any)["short"])

	require.NoError(t, mm.SendIncident(ctx, testBaseUrl, testIncident()))
	a = c.bodies[1]["attachments"].([]any)[0].(map[string]any)
	assert.Equal(t, "- Net / Network RTT: high latency", a["text"])
}

// fakeSMTP is a minimal SMTP server supporting STARTTLS, implicit TLS and AUTH PLAIN.
type fakeSMTP struct {
	addr     string
	tlsCfg   *tls.Config
	implicit bool

	lock     sync.Mutex
	from     string
	rcpts    []string
	data     string
	authUser string
	authPass string
	tlsUsed  bool
}

func selfSignedTLS(t *testing.T) *tls.Config {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "smtp.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

func startFakeSMTP(t *testing.T, implicitTLS bool) *fakeSMTP {
	s := &fakeSMTP{tlsCfg: selfSignedTLS(t), implicit: implicitTLS}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	if implicitTLS {
		l = tls.NewListener(l, s.tlsCfg)
	}
	s.addr = l.Addr().String()
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	return s
}

func (s *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	secure := s.implicit
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch cmd {
		case "EHLO", "HELO":
			w("250-fake")
			if !secure {
				w("250-STARTTLS")
			}
			w("250 AUTH PLAIN")
		case "STARTTLS":
			w("220 go ahead")
			tc := tls.Server(conn, s.tlsCfg)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn = tc
			r = bufio.NewReader(conn)
			secure = true
			s.lock.Lock()
			s.tlsUsed = true
			s.lock.Unlock()
		case "AUTH":
			parts := strings.Fields(line)
			raw, _ := base64.StdEncoding.DecodeString(parts[len(parts)-1])
			creds := strings.Split(string(raw), "\x00")
			s.lock.Lock()
			s.authUser, s.authPass = creds[1], creds[2]
			s.lock.Unlock()
			w("235 ok")
		case "MAIL":
			s.lock.Lock()
			s.from = line
			s.lock.Unlock()
			w("250 ok")
		case "RCPT":
			s.lock.Lock()
			s.rcpts = append(s.rcpts, line)
			s.lock.Unlock()
			w("250 ok")
		case "DATA":
			w("354 send")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.lock.Lock()
			s.data = b.String()
			s.lock.Unlock()
			w("250 queued")
		case "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func TestEmail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, mode := range []db.EmailTLSMode{db.EmailTLSModeStartTLS, db.EmailTLSModeTLS, db.EmailTLSModeNone} {
		t.Run(string(mode), func(t *testing.T) {
			srv := startFakeSMTP(t, mode == db.EmailTLSModeTLS)
			host, port, _ := net.SplitHostPort(srv.addr)
			cfg := &db.IntegrationEmail{
				Host: host, TLSMode: mode, TlsSkipVerify: true,
				Username: "user", Password: "pass",
				From: "shards <alerts@example.com>", To: []string{"ops@example.com, oncall@example.com"},
			}
			cfg.Port = atoi(port)
			require.NoError(t, cfg.Validate())
			require.Equal(t, []string{"ops@example.com", "oncall@example.com"}, cfg.To)

			e := NewEmail(cfg)
			require.NoError(t, e.SendAlert(ctx, testBaseUrl, testAlert(model.CRITICAL)))
			srv.lock.Lock()
			defer srv.lock.Unlock()
			assert.Equal(t, mode == db.EmailTLSModeStartTLS, srv.tlsUsed)
			assert.Equal(t, "user", srv.authUser)
			assert.Equal(t, "pass", srv.authPass)
			assert.Equal(t, "MAIL FROM:<alerts@example.com>", strings.Fields(srv.from)[0]+" "+strings.Fields(srv.from)[1])
			assert.Equal(t, []string{"RCPT TO:<ops@example.com>", "RCPT TO:<oncall@example.com>"}, srv.rcpts)
			msg, err := mail.ReadMessage(strings.NewReader(srv.data))
			require.NoError(t, err)
			assert.Equal(t, "[CRITICAL] api: 1 probe is failing <html>", msg.Header.Get("Subject"))
			assert.Equal(t, "ops@example.com, oncall@example.com", msg.Header.Get("To"))
			mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
			require.NoError(t, err)
			assert.Equal(t, "multipart/alternative", mediaType)
			parts := map[string]string{}
			mr := multipart.NewReader(msg.Body, params["boundary"])
			for {
				part, err := mr.NextPart() // decodes quoted-printable
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				body, _ := io.ReadAll(part)
				parts[strings.Split(part.Header.Get("Content-Type"), ";")[0]] = string(body)
			}
			assert.Contains(t, parts["text/plain"], "View alert: https://shards.example.com/p/p1/alerts?alert=a1")
			assert.Contains(t, parts["text/plain"], "Findings: api-health: 2 failed runs in a row")
			assert.Contains(t, parts["text/html"], "1 probe is failing &lt;html&gt;")
			assert.Contains(t, parts["text/html"], `<a href="https://shards.example.com/p/p1/alerts?alert=a1"`)
		})
	}

	// STARTTLS is required by default: a server without it is rejected
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		_, _ = conn.Write([]byte("220 fake\r\n"))
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(strings.ToUpper(line), "EHLO") {
				_, _ = conn.Write([]byte("250 fake\r\n"))
			} else {
				_, _ = conn.Write([]byte("250 ok\r\n"))
			}
		}
	}()
	host, port, _ := net.SplitHostPort(l.Addr().String())
	cfg := &db.IntegrationEmail{Host: host, Port: atoi(port), From: "a@example.com", To: []string{"b@example.com"}}
	require.NoError(t, cfg.Validate())
	err = NewEmail(cfg).SendAlert(ctx, testBaseUrl, testAlert(model.CRITICAL))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "STARTTLS")
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestShardsClientRouting(t *testing.T) {
	integrations := db.Integrations{}
	integrations.Discord = &db.IntegrationDiscord{WebhookUrl: "http://example.com", ShardsNotificationSettings: db.ShardsNotificationSettings{Incidents: true}}
	d := db.IncidentNotificationDestination{IntegrationType: db.IntegrationTypeDiscord}
	assert.NotNil(t, getClient(d, integrations, NotificationTypeIncident))
	assert.Nil(t, getClient(d, integrations, NotificationTypeAlert))
	assert.Nil(t, getClient(db.IncidentNotificationDestination{IntegrationType: db.IntegrationTypeTelegram}, integrations, NotificationTypeIncident))
}

package notifications

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"html"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/coroot/coroot/db"
	"github.com/coroot/coroot/model"
)

// Email sends notifications over SMTP (STARTTLS, implicit TLS or plain) as multipart plain text + HTML.
type Email struct {
	cfg *db.IntegrationEmail
	// rootCAs overrides the system roots (used in tests).
	rootCAs *x509.CertPool
}

func NewEmail(cfg *db.IntegrationEmail) *Email {
	return &Email{cfg: cfg}
}

func (e *Email) SendIncident(ctx context.Context, baseUrl string, n *db.IncidentNotification) error {
	return e.send(ctx, incidentMessage(baseUrl, n))
}

func (e *Email) SendAlert(ctx context.Context, baseUrl string, n *db.AlertNotification) error {
	return e.send(ctx, alertMessage(baseUrl, n))
}

func (e *Email) SendDeployment(ctx context.Context, project *db.Project, ds model.ApplicationDeploymentStatus) error {
	return nil // not supported
}

func (e *Email) SendComment(ctx context.Context, values CommentTemplateValues) error {
	return e.send(ctx, commentMessage(values))
}

func emailText(m *message) string {
	var b strings.Builder
	b.WriteString(m.Title + "\n\n")
	for _, l := range m.Lines {
		b.WriteString("* " + l + "\n")
	}
	if len(m.Lines) > 0 {
		b.WriteString("\n")
	}
	for _, f := range m.Fields {
		if f.Code {
			b.WriteString(f.Name + ":\n" + f.Value + "\n\n")
		} else {
			b.WriteString(f.Name + ": " + f.Value + "\n")
		}
	}
	if m.Url != "" {
		b.WriteString("\n" + m.UrlText + ": " + m.Url + "\n")
	}
	if m.Footer != "" {
		b.WriteString("\n-- \n" + m.Footer + "\n")
	}
	return b.String()
}

func emailHTML(m *message) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;font-size:14px;color:#1f2328">`)
	b.WriteString(fmt.Sprintf(`<div style="border-left:4px solid %s;padding:4px 12px;margin-bottom:12px"><div style="font-size:16px;font-weight:600">%s</div></div>`, esc(m.Status.Color()), esc(m.Title)))
	if len(m.Lines) > 0 {
		b.WriteString(`<ul style="padding-left:20px">`)
		for _, l := range m.Lines {
			b.WriteString("<li>" + esc(l) + "</li>")
		}
		b.WriteString("</ul>")
	}
	if len(m.Fields) > 0 {
		b.WriteString(`<table cellpadding="4" style="border-collapse:collapse">`)
		for _, f := range m.Fields {
			v := esc(f.Value)
			if f.Code {
				v = `<pre style="margin:0;white-space:pre-wrap;font-family:monospace">` + v + "</pre>"
			}
			b.WriteString(fmt.Sprintf(`<tr><td style="vertical-align:top;color:#57606a;white-space:nowrap">%s</td><td>%s</td></tr>`, esc(f.Name), v))
		}
		b.WriteString("</table>")
	}
	if m.Url != "" {
		b.WriteString(fmt.Sprintf(`<p><a href="%s" style="display:inline-block;padding:6px 12px;background:#0969da;color:#fff;text-decoration:none;border-radius:6px">%s</a></p>`, esc(m.Url), esc(m.UrlText)))
	}
	if m.Footer != "" {
		b.WriteString(`<p style="color:#8c959f;font-size:12px">` + esc(m.Footer) + "</p>")
	}
	b.WriteString("</body></html>")
	return b.String()
}

func randomToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// buildEmail renders a multipart/alternative message.
func buildEmail(from string, to []string, m *message, now time.Time) []byte {
	boundary := "shards-" + randomToken()
	var b bytes.Buffer
	domain := "shards.local"
	if a, err := mail.ParseAddress(from); err == nil {
		if i := strings.LastIndex(a.Address, "@"); i >= 0 {
			domain = a.Address[i+1:]
		}
	}
	headers := [][2]string{
		{"From", from},
		{"To", strings.Join(to, ", ")},
		{"Subject", mime.QEncoding.Encode("utf-8", m.Title)},
		{"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", fmt.Sprintf("<%s@%s>", randomToken(), domain)},
		{"MIME-Version", "1.0"},
		{"Content-Type", fmt.Sprintf(`multipart/alternative; boundary="%s"`, boundary)},
		{"X-Mailer", "shards"},
	}
	for _, h := range headers {
		b.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	b.WriteString("\r\n")
	part := func(contentType, body string) {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + contentType + "; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	part("text/plain", emailText(m))
	part("text/html", emailHTML(m))
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes()
}

func (e *Email) tlsConfig() *tls.Config {
	return &tls.Config{ServerName: e.cfg.Host, InsecureSkipVerify: e.cfg.TlsSkipVerify, RootCAs: e.rootCAs}
}

func (e *Email) send(ctx context.Context, m *message) error {
	cfg := e.cfg
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{}
	var conn net.Conn
	var err error
	if cfg.TLSMode == db.EmailTLSModeTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: e.tlsConfig()}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close()
	if cfg.TLSMode == db.EmailTLSModeStartTLS || cfg.TLSMode == "" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp: the server doesn't support STARTTLS (choose another TLS mode)")
		}
		if err = c.StartTLS(e.tlsConfig()); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fmt.Errorf("smtp: the server doesn't support authentication")
		}
		if err = c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("smtp: invalid from address: %w", err)
	}
	if err = c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	for _, rcpt := range cfg.To {
		a, err := mail.ParseAddress(rcpt)
		if err != nil {
			return fmt.Errorf("smtp: invalid recipient %s: %w", rcpt, err)
		}
		if err = c.Rcpt(a.Address); err != nil {
			return fmt.Errorf("smtp: RCPT TO %s: %w", a.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err = w.Write(buildEmail(cfg.From, cfg.To, m, time.Now())); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	return c.Quit()
}

// Package mailer sends the panel's outgoing email the way Hestia's send_email
// does: through the configured SMTP server, or the local sendmail.
package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
)

const (
	defaultSendmail = "/usr/sbin/sendmail"
	dialTimeout     = 15 * time.Second
	sessionTimeout  = 60 * time.Second
)

// Message is one email. Body is plain text; it is sent as HTML with newlines
// turned into <br />, like PHP's nl2br + MsgHTML. Nothing is escaped.
type Message struct {
	To, Subject, Body string
}

// Mailer sends messages. The zero value works.
type Mailer struct {
	Sendmail string        // default /usr/sbin/sendmail
	Hostname func() string // default os.Hostname
}

// Send delivers msg using the transport selected by cfg.
func (m *Mailer) Send(ctx context.Context, cfg hestia.SysConfig, msg Message) error {
	fromAddr := strings.TrimSpace(cfg["FROM_EMAIL"])
	if fromAddr == "" {
		fromAddr = "noreply@" + m.hostname()
	}
	smtpMode := cfg["USE_SERVER_SMTP"] == "true"
	if smtpMode {
		if a := strings.TrimSpace(cfg["SERVER_SMTP_ADDR"]); a != "" {
			if _, err := mail.ParseAddress(a); err == nil {
				fromAddr = a
			}
		}
	}
	name := cfg["FROM_NAME"]
	if name == "" {
		name = cfg["APP_NAME"]
	}
	name = brand.Rewrite(name)

	for _, v := range []string{msg.To, msg.Subject, fromAddr, name} {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("mailer: header value contains a line break")
		}
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("mailer: invalid recipient: %w", err)
	}
	from, err := mail.ParseAddress(fromAddr)
	if err != nil {
		return fmt.Errorf("mailer: invalid sender: %w", err)
	}
	raw, err := m.build(from.Address, name, msg)
	if err != nil {
		return err
	}
	if smtpMode {
		return sendSMTP(ctx, cfg, m.hostname(), from.Address, to.Address, raw)
	}
	return m.sendSendmail(ctx, raw)
}

func (m *Mailer) hostname() string {
	if m.Hostname != nil {
		return m.Hostname()
	}
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "localhost"
	}
	return h
}

func (m *Mailer) build(from, fromName string, msg Message) ([]byte, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	fromHdr := from
	if fromName != "" {
		fromHdr = formatName(fromName) + " <" + from + ">"
	}
	body := strings.ReplaceAll(msg.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "<br />\n")

	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("Date", time.Now().Format(time.RFC1123Z))
	h("From", fromHdr)
	h("To", msg.To)
	h("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	h("Message-ID", "<"+hex.EncodeToString(id[:])+"@"+m.hostname()+">")
	h("MIME-Version", "1.0")
	h("Content-Type", "text/html; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	w := quotedprintable.NewWriter(&b)
	if _, err := io.WriteString(w, body); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// formatName returns a display name safe for a From header.
func formatName(n string) string {
	for _, r := range n {
		if r > 127 {
			return mime.QEncoding.Encode("utf-8", n)
		}
	}
	n = strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(n)
	return `"` + n + `"`
}

func (m *Mailer) sendSendmail(ctx context.Context, raw []byte) error {
	path := m.Sendmail
	if path == "" {
		path = defaultSendmail
	}
	cmd := exec.CommandContext(ctx, path, "-t", "-i")
	cmd.Stdin = bytes.NewReader(raw)
	var out bytes.Buffer
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mailer: sendmail failed: %w: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}

type securityMode int

const (
	tlsOpportunistic securityMode = iota // upgrade if the server offers STARTTLS (PHPMailer SMTPAutoTLS)
	tlsRequired                          // "tls" or "starttls": upgrade or fail
	tlsImplicit                          // "ssl": TLS from the first byte
)

// parseSecurity maps Hestia's free-text SERVER_SMTP_SECURITY to a mode.
func parseSecurity(s string) securityMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tls", "starttls":
		return tlsRequired
	case "ssl":
		return tlsImplicit
	}
	return tlsOpportunistic
}

func sendSMTP(ctx context.Context, cfg hestia.SysConfig, helo, from, to string, raw []byte) (err error) {
	secret := cfg["SERVER_SMTP_PASSWD"]
	defer func() {
		if err != nil && secret != "" {
			err = errors.New(strings.ReplaceAll(err.Error(), secret, "***"))
		}
	}()

	host := cfg["SERVER_SMTP_HOST"]
	if host == "" {
		return errors.New("mailer: SERVER_SMTP_HOST is not set")
	}
	security := parseSecurity(cfg["SERVER_SMTP_SECURITY"])
	port := cfg["SERVER_SMTP_PORT"]
	if port == "" {
		port = "25"
		if security == tlsImplicit {
			port = "465"
		}
	}
	addr := net.JoinHostPort(host, port)
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mailer: connect %s: %w", addr, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(sessionTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()

	var c *smtp.Client
	if security == tlsImplicit {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("mailer: TLS handshake: %w", err)
		}
		c, err = smtp.NewClient(tc, host)
	} else {
		c, err = smtp.NewClient(conn, host)
	}
	if err != nil {
		return fmt.Errorf("mailer: SMTP greeting: %w", err)
	}
	defer c.Close()

	if err := c.Hello(helo); err != nil {
		return fmt.Errorf("mailer: SMTP EHLO: %w", err)
	}
	if security != tlsImplicit {
		offered, _ := c.Extension("STARTTLS")
		switch {
		case offered:
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("mailer: STARTTLS: %w", err)
			}
		case security == tlsRequired:
			return errors.New("mailer: server does not offer STARTTLS")
		}
	}
	if user := cfg["SERVER_SMTP_USER"]; user != "" {
		if err := c.Auth(smtp.PlainAuth("", user, secret, host)); err != nil {
			return fmt.Errorf("mailer: SMTP auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mailer: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("mailer: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mailer: DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("mailer: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mailer: send: %w", err)
	}
	return c.Quit()
}

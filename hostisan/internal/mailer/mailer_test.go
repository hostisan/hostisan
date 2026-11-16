package mailer

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
)

func hostname() string { return "host.test" }

func TestSendViaSendmail(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "stdin.txt")
	args := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "sendmail")
	src := "#!/bin/sh\necho \"$@\" > " + args + "\ncat > " + out + "\n"
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Mailer{Sendmail: script, Hostname: hostname}
	cfg := hestia.SysConfig{"APP_NAME": "Hestia Control Panel"}
	err := m.Send(context.Background(), cfg, Message{To: "a@b.test", Subject: "Hi", Body: "line1\nline2"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	s := string(got)
	for _, want := range []string{
		"To: a@b.test\r\n",
		"From: \"Hostisan\" <noreply@host.test>\r\n",
		"Subject: Hi\r\n",
		"Content-Type: text/html; charset=utf-8\r\n",
		"Content-Transfer-Encoding: quoted-printable\r\n",
		"line1<br />\r\nline2",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	a, _ := os.ReadFile(args)
	if strings.TrimSpace(string(a)) != "-t -i" {
		t.Errorf("args = %q", a)
	}
}

func TestSendNonASCIIHeaders(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "stdin.txt")
	script := filepath.Join(dir, "sendmail")
	os.WriteFile(script, []byte("#!/bin/sh\ncat > "+out+"\n"), 0o755)
	m := &Mailer{Sendmail: script, Hostname: hostname}
	cfg := hestia.SysConfig{"FROM_EMAIL": "me@x.test", "FROM_NAME": "Zoë"}
	if err := m.Send(context.Background(), cfg, Message{To: "a@b.test", Subject: "Café", Body: "é"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	s := string(got)
	if !strings.Contains(s, "Subject: =?utf-8?q?Caf=C3=A9?=") || !strings.Contains(s, "From: =?utf-8?q?Zo=C3=AB?= <me@x.test>") {
		t.Errorf("headers not RFC2047 encoded:\n%s", s)
	}
	if !strings.Contains(s, "=C3=A9") {
		t.Errorf("body not quoted-printable:\n%s", s)
	}
}

func TestSendRejectsHeaderInjection(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "sendmail")
	os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	m := &Mailer{Sendmail: script, Hostname: hostname}
	cases := []struct {
		cfg hestia.SysConfig
		msg Message
	}{
		{hestia.SysConfig{}, Message{To: "a@b.test\r\nBcc: x@y.test", Subject: "s", Body: "b"}},
		{hestia.SysConfig{}, Message{To: "a@b.test", Subject: "s\nBcc: x@y.test", Body: "b"}},
		{hestia.SysConfig{"FROM_EMAIL": "a@b.test\nBcc: x"}, Message{To: "a@b.test", Subject: "s", Body: "b"}},
		{hestia.SysConfig{"FROM_NAME": "N\r\nBcc: x"}, Message{To: "a@b.test", Subject: "s", Body: "b"}},
	}
	for i, c := range cases {
		if err := m.Send(context.Background(), c.cfg, c.msg); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("sendmail ran despite injection")
	}
}

type smtpResult struct {
	lines []string
	data  string
}

// fakeSMTP serves one plain SMTP session. authReply is the reply to AUTH.
func fakeSMTP(t *testing.T, authReply string) (addr string, res chan smtpResult) {
	t.Helper()
	return fakeSMTPTLS(t, authReply, false, "")
}

// fakeSMTPTLS also controls whether STARTTLS is advertised and how it is answered.
func fakeSMTPTLS(t *testing.T, authReply string, advertise bool, starttlsReply string) (addr string, res chan smtpResult) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	res = make(chan smtpResult, 1)
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(c)
		var out smtpResult
		say := func(s string) { c.Write([]byte(s + "\r\n")) }
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimRight(line, "\r\n")
			out.lines = append(out.lines, line)
			up := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(up, "EHLO"):
				say("250-fake")
				if advertise {
					say("250-STARTTLS")
				}
				say("250 AUTH PLAIN")
			case strings.HasPrefix(up, "STARTTLS"):
				say(starttlsReply)
			case strings.HasPrefix(up, "AUTH"):
				say(authReply)
				if strings.HasPrefix(authReply, "535") {
					out2 := out
					res <- out2
					return
				}
			case strings.HasPrefix(up, "DATA"):
				say("354 go")
				var b strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				out.data = b.String()
				say("250 queued")
			case strings.HasPrefix(up, "QUIT"):
				say("221 bye")
				res <- out
				return
			default:
				say("250 ok")
			}
		}
		res <- out
	}()
	return ln.Addr().String(), res
}

func smtpCfg(addr string) hestia.SysConfig {
	host, port, _ := net.SplitHostPort(addr)
	return hestia.SysConfig{
		"USE_SERVER_SMTP":    "true",
		"SERVER_SMTP_HOST":   host,
		"SERVER_SMTP_PORT":   port,
		"SERVER_SMTP_USER":   "user",
		"SERVER_SMTP_PASSWD": "s3cret-pw",
		"SERVER_SMTP_ADDR":   "relay@example.test",
		"FROM_EMAIL":         "other@example.test",
		"APP_NAME":           "Hestia",
	}
}

func TestSendViaSMTP(t *testing.T) {
	addr, res := fakeSMTP(t, "235 ok")
	m := &Mailer{Hostname: hostname}
	err := m.Send(context.Background(), smtpCfg(addr), Message{To: "rcpt@b.test", Subject: "Sub", Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	r := <-res
	joined := strings.Join(r.lines, "\n")
	for _, want := range []string{"AUTH PLAIN", "MAIL FROM:<relay@example.test>", "RCPT TO:<rcpt@b.test>"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if !strings.Contains(r.data, "Subject: Sub") || !strings.Contains(r.data, "From: \"Hostisan\" <relay@example.test>") {
		t.Errorf("data:\n%s", r.data)
	}
}

func TestSendSMTPFailureReturnsError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	m := &Mailer{Hostname: hostname}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.Send(ctx, smtpCfg(addr), Message{To: "a@b.test", Subject: "s", Body: "b"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNoSecretsInError(t *testing.T) {
	addr, _ := fakeSMTP(t, "535 bad credentials")
	m := &Mailer{Hostname: hostname}
	err := m.Send(context.Background(), smtpCfg(addr), Message{To: "a@b.test", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if strings.Contains(err.Error(), "s3cret-pw") {
		t.Fatalf("password leaked: %v", err)
	}
}

func sendWithSecurity(t *testing.T, addr, security string) error {
	t.Helper()
	cfg := smtpCfg(addr)
	cfg["SERVER_SMTP_SECURITY"] = security
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := &Mailer{Hostname: hostname}
	return m.Send(ctx, cfg, Message{To: "rcpt@b.test", Subject: "s", Body: "b"})
}

func noMailFrom(t *testing.T, res chan smtpResult) {
	t.Helper()
	select {
	case r := <-res:
		if strings.Contains(strings.Join(r.lines, "\n"), "MAIL FROM") {
			t.Fatalf("mail was sent in clear: %v", r.lines)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fake server did not finish")
	}
}

func TestSecurityStartTLSRequiredButNotOffered(t *testing.T) {
	addr, res := fakeSMTPTLS(t, "235 ok", false, "")
	if err := sendWithSecurity(t, addr, "STARTTLS"); err == nil {
		t.Fatal("expected error")
	}
	noMailFrom(t, res)
}

func TestSecurityTLSUpgradeRefused(t *testing.T) {
	addr, res := fakeSMTPTLS(t, "235 ok", true, "502 not today")
	if err := sendWithSecurity(t, addr, "tls"); err == nil {
		t.Fatal("expected error")
	}
	noMailFrom(t, res)
}

func TestSecurityEmptyFallsBackToPlain(t *testing.T) {
	addr, res := fakeSMTPTLS(t, "235 ok", false, "")
	if err := sendWithSecurity(t, addr, ""); err != nil {
		t.Fatal(err)
	}
	r := <-res
	if !strings.Contains(strings.Join(r.lines, "\n"), "MAIL FROM") {
		t.Fatalf("not delivered: %v", r.lines)
	}
}

func TestSecurityMode(t *testing.T) {
	for in, want := range map[string]securityMode{
		"STARTTLS": tlsRequired, "Tls": tlsRequired, " tls ": tlsRequired,
		"ssl": tlsImplicit, " SSL": tlsImplicit,
		"": tlsOpportunistic, "none": tlsOpportunistic,
	} {
		if got := parseSecurity(in); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}

func TestSMTPSendsHostnameInEHLO(t *testing.T) {
	addr, res := fakeSMTP(t, "235 ok")
	m := &Mailer{Hostname: func() string { return "rec.hostisan.test" }}
	if err := m.Send(context.Background(), smtpCfg(addr), Message{To: "rcpt@b.test", Subject: "s", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	r := <-res
	if len(r.lines) == 0 || r.lines[0] != "EHLO rec.hostisan.test" {
		t.Fatalf("lines: %v", r.lines)
	}
}

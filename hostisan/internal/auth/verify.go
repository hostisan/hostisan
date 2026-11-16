package auth

import (
	"context"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/secret"
)

// VerifyPassword checks in.Password for in.Username the way Login does
// (v-check-user-password, then v-check-user-hash, both through private temp files),
// for re-authentication: changing your own password, turning off your 2FA, the 2FA
// reset. It doesn't check suspension, the login-disabled flag, the IP list or 2FA.
//
// A wrong password is logged as a failed login (`v-log-user-login <u> <ip> failed
// <fingerprint> <ua>`), so fail2ban sees it, and sleeps 2 s like a failed login. An
// invalid username sleeps too, with no command. A right password logs nothing.
func (s *Service) VerifyPassword(ctx context.Context, in LoginInput) (bool, error) {
	if !hestia.ValidUsername(in.Username) {
		s.fail(msgInvalid)
		return false, nil
	}
	ok, err := s.CheckPassword(ctx, in.Username, in.Password, in.IP)
	if err != nil || ok {
		return ok, err
	}
	s.logLogin(ctx, in, "failed")
	s.fail(msgInvalid)
	return false, nil
}

// CheckPassword checks password for user with the login commands
// (v-check-user-password, then v-check-user-hash, both through private temp files)
// and nothing else: no pause, no auth.log line, no suspension or 2FA check. It is the
// legacy API's password mode (parity decision 20), which never logged or slowed down.
// An invalid username is false with no command.
func (s *Service) CheckPassword(ctx context.Context, user, password, ip string) (bool, error) {
	if !hestia.ValidUsername(user) {
		return false, nil
	}
	hash, err := s.hashFor(ctx, user, LoginInput{Username: user, Password: password, IP: ip})
	if err != nil {
		return false, err
	}
	hashFile, cleanup, err := secret.TempFile(s.tmpDir(), hash)
	if err != nil {
		return false, err
	}
	check, err := s.R.Run(ctx, "v-check-user-hash", user, hashFile, ip)
	cleanup()
	if err != nil {
		return false, err
	}
	return check.Exit == 0, nil
}

// logUnsafe drops the characters v-log-user-login refuses in a user agent or reason
// (format_no_quotes: ' " |) and turns line breaks (is_no_new_line_format) into spaces.
var logUnsafe = strings.NewReplacer("'", "", `"`, "", "|", "", "\r", " ", "\n", " ")

// LogSafe returns s as v-log-user-login accepts it, so a user agent with quotes still
// leaves the failed-login line fail2ban watches.
func LogSafe(s string) string {
	return strings.TrimSpace(logUnsafe.Replace(s))
}

// Delay pauses for d through Sleep (time.Sleep unless a test replaces it), e.g. the
// 5 s after a wrong reset code.
func (s *Service) Delay(d time.Duration) {
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	sleep(d)
}

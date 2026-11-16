// Package auth ports Hestia's sign-in (authenticate_user in web/login/index.php).
package auth

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/secret"
)

// Hestia's English messages; the UI translates them with the gettext catalogs.
const (
	msgInvalid     = "Invalid username or password"
	msgSuspended   = "Account has been suspended"
	msgUnsupported = "Unsupported hash method"
	msgTwoFA       = "Invalid or missing 2FA token"
)

type LoginInput struct {
	Username, Password, TwoFA string
	IP, UserAgent             string
	Fingerprint               string // the session's CSRF token, as PHP's $_POST["token"]
}

type LoginOutcome int

const (
	LoginOK LoginOutcome = iota
	LoginNeedTwoFA
	LoginFailed
)

type LoginResult struct {
	Outcome  LoginOutcome
	Message  string // English msgid when Outcome is LoginFailed
	User     hestia.User
	Username string
}

// Wrong 2FA codes are counted per user. The first twoFATypos aren't logged; after
// twoFAMaxFailures the user's 2FA step is locked for TwoFALockout, so a stolen password
// can't be followed by guessing codes.
const (
	twoFATypos       = 3
	twoFAMaxFailures = 5
	TwoFALockout     = 15 * time.Minute
)

type Service struct {
	R      runner.Runner
	H      *hestia.Client
	Sleep  func(time.Duration) // default time.Sleep; slows down password guessing
	TmpDir string              // default /tmp (Hestia only reads secret files from /tmp)
	Now    func() time.Time    // default time.Now

	mu          sync.Mutex
	twoFAFailed map[string]twoFAFailures // by username
}

type twoFAFailures struct {
	count int
	last  time.Time
}

// Login checks a password (and 2FA code) the way the PHP panel does, including the
// v-log-user-login calls that Hestia's fail2ban jail watches.
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	user := in.Username
	if !hestia.ValidUsername(user) {
		return s.fail(msgInvalid), nil
	}

	salt, err := s.R.Run(ctx, "v-get-user-salt", user, in.IP, "json")
	if err != nil {
		return LoginResult{}, err
	}
	switch salt.Exit {
	case 0:
	case runner.ExitSuspended:
		return s.fail(msgSuspended), nil
	case runner.ExitArgs:
		return s.fail(msgUnsupported), nil
	default:
		return s.fail(msgInvalid), nil
	}

	// v-check-user-password hashes the password with the user's own method (yescrypt,
	// sha-512, md5 or des) and prints the hash; on a wrong password it prints nothing
	// useful, and the empty hash then fails v-check-user-hash below.
	hash, err := s.hashFor(ctx, user, in)
	if err != nil {
		return LoginResult{}, err
	}
	hashFile, cleanup, err := secret.TempFile(s.tmpDir(), hash)
	if err != nil {
		return LoginResult{}, err
	}
	check, err := s.R.Run(ctx, "v-check-user-hash", user, hashFile, in.IP)
	cleanup()
	if err != nil {
		return LoginResult{}, err
	}
	if check.Exit != 0 {
		s.logLogin(ctx, in, "failed")
		return s.fail(msgInvalid), nil
	}

	u, ok, err := s.H.User(ctx, user)
	if err != nil {
		return LoginResult{}, err
	}
	if !ok {
		return s.fail(msgInvalid), nil
	}
	if u.LoginDisabled() {
		s.logLogin(ctx, in, "failed", "yes", "Login disabled for this user")
		return s.fail(msgInvalid), nil
	}
	if u.UsesIPList() && !slices.Contains(u.AllowedIPs(), in.IP) {
		s.logLogin(ctx, in, "failed", "yes", "IP address not in allowed list")
		return s.fail(msgInvalid), nil
	}

	if u.TwoFAEnabled() {
		if in.TwoFA == "" {
			return LoginResult{Outcome: LoginNeedTwoFA, Username: user}, nil
		}
		if s.twoFALocked(user) {
			s.logLogin(ctx, in, "failed", "yes", msgTwoFA)
			return s.fail(msgTwoFA), nil
		}
		ok := len(in.TwoFA) < 10
		if ok {
			res, err := s.R.Run(ctx, "v-check-user-2fa", user, in.TwoFA)
			if err != nil {
				return LoginResult{}, err
			}
			ok = res.Exit == 0
		}
		if !ok {
			// A few typos are allowed before failures start feeding fail2ban.
			if s.twoFAFailure(user) > twoFATypos {
				s.logLogin(ctx, in, "failed", "yes", msgTwoFA)
			}
			return s.fail(msgTwoFA), nil
		}
		s.mu.Lock()
		delete(s.twoFAFailed, user)
		s.mu.Unlock()
	}

	s.logLogin(ctx, in, "success")
	return LoginResult{Outcome: LoginOK, User: u, Username: user}, nil
}

// twoFALocked reports whether the user has sent too many wrong codes recently.
func (s *Service) twoFALocked(user string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.twoFAFailed[user]
	return f.count >= twoFAMaxFailures && s.now().Before(f.last.Add(TwoFALockout))
}

// twoFAFailure counts a wrong code and returns the user's recent total. Failures older
// than TwoFALockout are forgotten.
func (s *Service) twoFAFailure(user string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.twoFAFailed == nil {
		s.twoFAFailed = map[string]twoFAFailures{}
	}
	f := s.twoFAFailed[user]
	if !s.now().Before(f.last.Add(TwoFALockout)) {
		f.count = 0
	}
	f.count++
	f.last = s.now()
	s.twoFAFailed[user] = f
	return f.count
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) hashFor(ctx context.Context, user string, in LoginInput) (string, error) {
	pwFile, cleanup, err := secret.TempFile(s.tmpDir(), in.Password)
	if err != nil {
		return "", err
	}
	defer cleanup()
	res, err := s.R.Run(ctx, "v-check-user-password", user, pwFile, in.IP, "yes")
	if err != nil {
		return "", err
	}
	if res.Exit != 0 {
		return "", nil
	}
	first, _, _ := strings.Cut(string(res.Stdout), "\n")
	return first, nil
}

// logLogin writes Hestia's auth log line. A failure to log doesn't block the login,
// as in the PHP panel. The user agent goes through LogSafe: a quote in it would make
// v-log-user-login exit before the failed line fail2ban reads.
func (s *Service) logLogin(ctx context.Context, in LoginInput, status string, extra ...string) {
	args := append([]string{in.Username, in.IP, status, in.Fingerprint, LogSafe(in.UserAgent)}, extra...)
	res, err := s.R.Run(ctx, "v-log-user-login", args...)
	if err == nil {
		err = res.Err("v-log-user-login")
	}
	if err != nil {
		slog.Warn("login log failed", "user", in.Username, "err", err)
	}
}

func (s *Service) fail(msg string) LoginResult {
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	sleep(2 * time.Second)
	return LoginResult{Outcome: LoginFailed, Message: msg}
}

func (s *Service) tmpDir() string {
	if s.TmpDir == "" {
		return "/tmp"
	}
	return s.TmpDir
}

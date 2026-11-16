package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hostisan/hostisan/internal/auth"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/httpapi"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/mailer"
	"github.com/hostisan/hostisan/internal/phpsession"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
	"github.com/hostisan/hostisan/internal/webui"
)

// serve runs the panel: the API and the embedded UI on one address.
func serve(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("serve", flag.ContinueOnError)
	fl.SetOutput(stderr)
	listenAddr := fl.String("listen", "127.0.0.1:8090", "address to listen on; unix:/path for the socket hestia-nginx proxies to")
	fixtures := fl.String("fixtures", "", "replay recorded command output from this directory (implies --dev)")
	localeDir := fl.String("locale-dir", "/usr/local/hestia/web/locale", "Hestia's gettext catalogs")
	dev := fl.Bool("dev", false, "development: non-Secure cookie, log every command")
	terminalSocket := fl.String("terminal-socket", httpapi.DefaultTerminalSocket, "the root terminal process's unix socket (hostisan terminal)")
	phpSessionDir := fl.String("php-session-dir", httpapi.DefaultPHPSessionDir, "hestia-php's session directory, where the file-manager handoff writes FileGator's session")
	stateDir := fl.String("state-dir", os.Getenv("STATE_DIRECTORY"), "where the panel keeps its state (jobs.json, sessions.json); systemd's StateDirectory sets $STATE_DIRECTORY; empty keeps jobs and sessions in memory only")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *fixtures != "" {
		*dev = true
	}

	var r runner.Runner
	if *fixtures != "" {
		f, err := runner.LoadFixtures(*fixtures)
		if err != nil {
			fmt.Fprintf(stderr, "hostisan: %v\n", err)
			return 1
		}
		r = f
	} else {
		// In production the panel runs as hestiaweb, the one user sudo lets run Hestia's
		// v-* commands (install/common/sudo/hestiaweb).
		if u, err := user.Current(); !*dev && (err != nil || u.Username != "hestiaweb") {
			fmt.Fprintln(stderr, "hostisan must run as hestiaweb (use --dev or --fixtures for development)")
			return 1
		}
		r = runner.Sudo{}
	}
	if *dev {
		slog.SetLogLoggerLevel(slog.LevelDebug)
		r = logged{r}
	}
	cmds := &runner.Inflight{Runner: r}
	r = cmds

	// FileGator sessions a previous run handed off are orphans now: nothing would
	// delete them at logout (parity decision 16).
	if n, err := phpsession.Sweep(*phpSessionDir); err != nil {
		slog.Warn("file manager sessions not cleared", "dir", *phpSessionDir, "err", err)
	} else if n > 0 {
		slog.Info("file manager sessions cleared", "count", n)
	}

	// Jobs and sessions are loaded only from a dir no one else can write to.
	if *stateDir != "" {
		if err := checkStateDir(*stateDir); err != nil {
			slog.Error("state dir not safe: jobs and sessions are not loaded or saved, a restart signs everyone out",
				"err", err)
			*stateDir = ""
		}
	}

	// Background jobs survive a restart in <state-dir>/jobs.json (Plan 6 C6 D2): the
	// ones the last run cut short answer "interrupted" and their owners get a bell.
	reg := jobs.NewRegistry(0, nil)
	if *stateDir != "" {
		// An app install the last run was killed in is undone first (its files
		// removed), before the job is marked failed.
		interrupted, err := reg.PersistRecover(filepath.Join(*stateDir, "jobs.json"), httpapi.JobFailure,
			httpapi.RecoverInterrupted(context.Background(), r))
		if err != nil {
			slog.Warn("background jobs of the last run not loaded", "err", err)
		}
		if len(interrupted) > 0 {
			slog.Warn("background jobs interrupted by the last stop", "count", len(interrupted))
			go httpapi.NotifyInterrupted(context.Background(), r, interrupted)
		}
	}

	h := &hestia.Client{R: r}
	// Signed-in sessions survive a restart in <state-dir>/sessions.json: an update
	// restarts hostisan.service, which signed everyone out (rc5 P3). Sessions idle
	// past INACTIVE_SESSION_TIMEOUT while the panel was down are dropped.
	store := session.NewMemoryStore()
	defer store.Close() // writes the last activity once requests have drained
	if *stateDir != "" {
		err := store.Persist(filepath.Join(*stateDir, "sessions.json"), time.Now(), sessionIdle(h))
		if err != nil {
			slog.Warn("sessions of the last run not loaded; everyone signs in again", "err", err)
		} else if n := store.Len(); n > 0 {
			slog.Info("sessions restored", "count", n)
		}
	}
	sessions := &session.Manager{Store: store, H: h, R: r, Dev: *dev}
	handler := httpapi.NewServer(httpapi.Deps{
		Auth:      &auth.Service{R: r, H: h},
		Sessions:  sessions,
		H:         h,
		R:         r,
		LocaleDir: *localeDir,
		UI:        webui.Handler(),
		Version:   version,
		Product:   httpapi.Product{URL: productURL, SecurityEmail: securityEmail},

		Mailer:         &mailer.Mailer{},
		EmailTemplates: mailer.Templates{},

		TerminalSocket: *terminalSocket,
		PHPSessionDir:  *phpSessionDir,

		Jobs: reg,
	})

	ln, connContext, err := listen(*listenAddr)
	if err != nil {
		fmt.Fprintf(stderr, "hostisan: %v\n", err)
		return 1
	}
	srv := &http.Server{
		Handler:           handler,
		ConnContext:       connContext,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Long enough for slow v-* commands (they time out at 60s).
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	ctx, stop := serveContext()
	defer stop()
	go func() {
		for t := time.NewTicker(time.Minute); ; {
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
				sessions.Sweep(ctx)
			}
		}
	}()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		if err := drain(srv, cmds, reg, shutdownLimit, jobGrace); err != nil {
			slog.Warn("stopped before every command finished", "err", err)
		}
	}()
	fmt.Fprintf(stdout, "hostisan %s listening on %s\n", version, *listenAddr)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "hostisan: %v\n", err)
		return 1
	}
	// Serve returns as soon as shutdown starts; exiting now would let systemd
	// (KillMode=mixed) kill the v-* commands still running.
	<-drained
	return 0
}

// sessionIdle is INACTIVE_SESSION_TIMEOUT, or 0 (keep every session; each request
// still checks it) when the config can't be read.
func sessionIdle(h *hestia.Client) time.Duration {
	cfg, err := h.Config(context.Background())
	if err != nil {
		slog.Warn("session timeout not read; restored sessions are checked per request", "err", err)
		return 0
	}
	minutes, err := strconv.Atoi(cfg["INACTIVE_SESSION_TIMEOUT"])
	if err != nil || minutes <= 0 {
		return 0
	}
	return time.Duration(minutes) * time.Minute
}

// shutdownLimit is how long a stopping panel waits for requests and v-* commands:
// the longest command timeout (legacy API and app installs, 300 s) up to the runner's
// backstop (runner.MaxRunTime: 300 + 5 + 5 + 1 s output wait = 311 s) and a margin.
// hostisan.service's TimeoutStopSec=320 is above it. Longer commands (a file restore,
// restorePathTimeout) are not waited for to the end; see there.
const shutdownLimit = 315 * time.Second

// jobGrace is how long a stopping panel lets background jobs (app installs, restores)
// run on before it asks them to stop (jobs.Interrupted). The rest of shutdownLimit is
// for the command a job is running then and its clean-up (an app install removes
// its files); a job still running at shutdownLimit is reported interrupted by the
// next start (Plan 6 C6 D2).
const jobGrace = shutdownLimit - 45*time.Second

// drain stops srv taking requests and waits, up to limit, for open requests, for the
// background jobs in reg (asked to stop after grace) and for every v-* command cmds
// is running.
func drain(srv *http.Server, cmds *runner.Inflight, reg *jobs.Registry, limit, grace time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	err := srv.Shutdown(ctx)
	if jerr := reg.Shutdown(ctx, grace); jerr != nil {
		err = jerr
	}
	// The file now says how each job ended, or "running" for one still going, which
	// the next start reports; nothing may rewrite it as the process exits.
	defer reg.Close()
	if werr := cmds.Wait(ctx); werr != nil {
		return werr
	}
	return err
}

// serveContext is how long serve runs: until SIGINT or SIGTERM. Tests replace it.
var serveContext = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// listen opens addr: "unix:/path" for the socket hestia-nginx proxies to, otherwise a
// TCP address. Requests on the socket are marked as proxied, so their X-Real-IP is
// trusted (see session.FromProxy); requests over TCP never are.
func listen(addr string) (net.Listener, func(context.Context, net.Conn) context.Context, error) {
	path, isUnix := strings.CutPrefix(addr, "unix:")
	if !isUnix {
		ln, err := net.Listen("tcp", addr)
		return ln, nil, err
	}
	// Replace a socket left behind by a crash, but never another kind of file.
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSocket != 0 {
		if err := os.Remove(path); err != nil {
			return nil, nil, err
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, err
	}
	// The owner and the proxy's group only.
	if err := os.Chmod(path, 0o660); err != nil {
		ln.Close()
		return nil, nil, err
	}
	return ln, func(ctx context.Context, _ net.Conn) context.Context { return session.FromProxy(ctx) }, nil
}

// logged logs every command in --dev mode.
type logged struct{ runner.Runner }

func (l logged) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	res, err := l.Runner.Run(ctx, cmd, args...)
	slog.Debug("command", "cmd", cmd, "args", logArgs(cmd, args), "exit", res.Exit, "err", err)
	return res, err
}

// RunAny forwards the legacy /api/ pass-through (runner.AnyRunner). Its arguments may
// be anything a caller sent (passwords, zone files), so only their count is logged.
func (l logged) RunAny(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	a, ok := l.Runner.(runner.AnyRunner)
	if !ok {
		return runner.Result{}, runner.ErrNoCommand
	}
	res, err := a.RunAny(ctx, cmd, args...)
	slog.Debug("command (any)", "cmd", cmd, "args", len(args), "exit", res.Exit, "err", err != nil)
	return res, err
}

// Exists forwards runner.AnyRunner.Exists.
func (l logged) Exists(cmd string) bool {
	a, ok := l.Runner.(runner.AnyRunner)
	return ok && a.Exists(cmd)
}

// Passwords and hashes go through temp files, but some commands take a secret as an
// argument: the 2FA code, and the session token Hestia logs as a login fingerprint.
var secretArg = map[string]int{
	"v-check-user-2fa":   1,
	"v-log-user-login":   3,
	"v-log-user-logout":  1,
	"v-check-api-key":    0, // the legacy API key's path names the key (a temp file since B2b)
	"v-check-access-key": 1, // the access key's secret (a temp file since B2b)
}

func logArgs(cmd string, args []string) []string {
	i, ok := secretArg[cmd]
	if !ok || i >= len(args) {
		return args
	}
	out := append([]string(nil), args...)
	out[i] = "[redacted]"
	return out
}

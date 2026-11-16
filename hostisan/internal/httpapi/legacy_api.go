package httpapi

// Legacy /api/ (Slice D): Hestia's web/api/index.php for its existing callers (DNS
// cluster, phpMyAdmin SSO, WHMCS-style billing modules), byte for byte (parity
// API1-API36, decisions 18-23, D10, D11). No session, no CSRF: the core's key and
// password checks are the authentication, and they log failures to auth.log with the
// real client IP for fail2ban (API12, API19, API34).
//
// This is the only handler that runs commands off the allowlist: any existing v-*,
// through runner.AnyRunner (decision 22, ruling R8). In access-key mode the core's
// v-check-access-key decides which commands a key may run before anything else does.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/secret"
	"github.com/hostisan/hostisan/internal/session"
)

const (
	legacyMaxBody      = 16 << 20 // zone files arrive through v-make-tmp-file (decision 23)
	legacyRunTimeout   = 300 * time.Second
	legacyWriteTimeout = 330 * time.Second // PHP's max_execution_time plus room for the reply
	legacyKeysDir      = "/usr/local/hestia/data/keys/"
	legacyContentType  = "text/html; charset=UTF-8" // PHP's default
	legacyJSONType     = "application/json; charset=utf-8"
	legacyMakeTmpFile  = "v-make-tmp-file"

	legacyMsgNoData       = "Error: data received is null or invalid, check https://hestiacp.com/docs/server-administration/rest-api.html"
	legacyMsgDisabledEcho = "Error: API has been disabled" // echoed before api_error in legacy mode
	legacyMsgDisabled     = "Error: API Disabled"
	legacyMsgIP           = "Error: IP is not allowed to connect with API"
	legacyMsgAuth         = "Error: authentication failed"
	legacyMsgNoCommand    = "Command not provided"
	legacyMsgKeyDisabled  = "API has been disabled"
	legacyMsgKeyIP        = "IP is not allowed to connect with API"
	legacyMsgKeyAuth      = "Authentication failed"
)

// Hestia's exit codes the API answers with (web/inc/helpers.php).
const (
	legacyExitInvalid     = 2
	legacyExitPassword    = 9
	legacyExitForbidden   = 10
	legacyExitDisabled    = 11
	legacyExitAPIDisabled = 21
	legacyExitNoData      = 405 // a code >= 100 is the HTTP status itself
)

// legacyCmdRe is the command names index.php accepts before running anything.
var legacyCmdRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// legacyRoutes mounts Hestia's legacy /api/ endpoint (Slice D, parity decisions 18-23).
// Any method: a GET simply has no data (the 405 reply).
func (s *server) legacyRoutes(mux *http.ServeMux) {
	h := http.HandlerFunc(s.legacyAPI)
	mux.Handle("/api/{$}", h)
	mux.Handle("/api/index.php", h)
}

// legacyHeaders marks a reply that may echo request text as text/html (D11) so a
// browser never runs it as a page on the panel's origin; API clients ignore both.
func legacyHeaders(w http.ResponseWriter, contentType string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "no-store")
}

func (s *server) legacyAPI(w http.ResponseWriter, r *http.Request) {
	legacyHeaders(w, legacyContentType)
	run, ok := s.R.(runner.AnyRunner)
	if !ok {
		slog.Error("legacy api: the runner cannot run commands off the allowlist")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// A pass-through command may run 300 s (PHP's max_execution_time); outlive the
	// server's write timeout for this reply only.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(legacyWriteTimeout))

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, legacyMaxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// $_POST when it names a mode, else the JSON body (API2).
	f, raw := legacyPHPPostFields(r, body)
	if !f.isset("access_key") && !f.isset("user") && !f.isset("hash") {
		if f, ok = legacyPHPJSONFields(raw); !ok {
			legacyError(w, legacyExitNoData, legacyMsgNoData, false, "")
			return
		}
	}
	// Every field read as a string must be one (decision 23; PHP failed on arrays).
	for name, v := range f {
		if v.array && legacyStringField(name) {
			legacyError(w, legacyExitNoData, legacyMsgNoData, false, "")
			return
		}
	}

	// hash=<20>:<40> is an access key and its secret (API3).
	if !f.isset("access_key") && f.isset("hash") {
		if id, secret, _ := strings.Cut(f.str("hash"), ":"); strings.Count(f.str("hash"), ":") == 1 && len(id) == 20 && len(secret) == 40 {
			f["access_key"], f["secret_key"] = legacyField{val: id}, legacyField{val: secret}
			delete(f, "hash")
		}
	}

	switch {
	case f.isset("access_key") && f.isset("secret_key"):
		s.legacyAccessKey(w, r, run, f)
	case f.isset("user") || f.isset("hash"):
		s.legacyLegacy(w, r, run, f)
	default:
		legacyError(w, legacyExitNoData, legacyMsgNoData, false, "")
	}
}

// legacyStringField reports whether index.php reads the field as a string.
func legacyStringField(name string) bool {
	switch name {
	case "access_key", "secret_key", "user", "password", "hash", "cmd", "returncode":
		return true
	}
	n, ok := strings.CutPrefix(name, "arg")
	i, err := strconv.Atoi(n)
	return ok && err == nil && i >= 1 && i <= 13 && strconv.Itoa(i) == n
}

// legacyArgs is arg1..arg13, trimmed, in order, skipping the missing ones (API5); the
// map keeps them by their original name for the access-key user check (API21).
func legacyArgs(f legacyFields) (args []string, byName map[string]string) {
	byName = map[string]string{}
	for i := 1; i <= 13; i++ {
		name := "arg" + strconv.Itoa(i)
		if f.isset(name) {
			v := legacyPHPTrim(f.str(name))
			args = append(args, v)
			byName[name] = v
		}
	}
	return args, byName
}

// legacyHTTPStatus is exit_code_to_http_code (decision 19).
func legacyHTTPStatus(exit int) int {
	switch exit {
	case 0:
		return http.StatusOK
	case 1, legacyExitDisabled:
		return http.StatusBadRequest
	case legacyExitInvalid:
		return http.StatusUnprocessableEntity
	case legacyExitPassword, 5, 6, legacyExitForbidden, legacyExitAPIDisabled:
		return http.StatusUnauthorized
	}
	return http.StatusBadRequest
}

// legacyError is api_error: Hestia-Exit-Code, the mapped status (a code >= 100 is the
// status), and the message ("Error: " added when missing) or, in code mode, the code.
// prefix is text index.php echoed before calling it.
func legacyError(w http.ResponseWriter, exit int, msg string, codeMode bool, prefix string) {
	status := legacyHTTPStatus(exit)
	if exit >= 100 {
		status = exit
	}
	legacyErrorAt(w, status, exit, msg, codeMode, prefix)
}

// legacyErrorAt is legacyError with the HTTP status chosen by the caller.
func legacyErrorAt(w http.ResponseWriter, status, exit int, msg string, codeMode bool, prefix string) {
	if status < 200 || status > 599 {
		status = http.StatusInternalServerError // net/http can't send these; PHP never did either
	}
	w.Header().Set("Hestia-Exit-Code", strconv.Itoa(exit))
	w.WriteHeader(status)
	body := strconv.Itoa(exit)
	if !codeMode {
		body = legacyPHPTrim(msg)
		if !strings.HasPrefix(body, "Error:") {
			body = "Error: " + body
		}
	}
	io.WriteString(w, prefix+body)
}

// legacyIPRefused answers the API_ALLOWED_IP check and reports whether it refused.
// A client IP that couldn't be read is refused with 403 unless every IP is allowed
// (PHP's appended "" entry let it through; review minor); msg is the mode's text.
func legacyIPRefused(w http.ResponseWriter, list, ip string, wildcard, codeMode bool, msg string) bool {
	if ip == "" && list != "allow-all" {
		legacyErrorAt(w, http.StatusForbidden, legacyExitForbidden, msg, codeMode, "")
		return true
	}
	if !legacyAllowedIP(list, ip, wildcard) {
		legacyError(w, legacyExitForbidden, msg, codeMode, "")
		return true
	}
	return false
}

// legacyAllowedIP is the API_ALLOWED_IP check: "allow-all", or ip in the comma list
// (an empty entry is always added, so an empty IP passes); "0.0.0.0" in the list
// allows everyone only when wildcard is set (access keys, API17).
func legacyAllowedIP(list, ip string, wildcard bool) bool {
	if list == "allow-all" {
		return true
	}
	entries := append(strings.Split(list, ","), "")
	return slices.Contains(entries, ip) || (wildcard && slices.Contains(entries, "0.0.0.0"))
}

// legacyLegacy is api_legacy: a user/password pair (ROOT_USER only) or a data/keys/
// hash (API8-API15).
func (s *server) legacyLegacy(w http.ResponseWriter, r *http.Request, run runner.AnyRunner, f legacyFields) {
	ctx := r.Context()
	codeMode := f.str("returncode") == "yes"
	cfg, err := s.H.Config(ctx)
	if err != nil {
		s.legacyInternal(w, "config", err)
		return
	}
	if cfg["API"] != "yes" {
		legacyError(w, legacyExitDisabled, legacyMsgDisabled, codeMode, legacyMsgDisabledEcho)
		return
	}
	ip := session.RealIP(r)
	if legacyIPRefused(w, cfg["API_ALLOWED_IP"], ip, false, codeMode, legacyMsgIP) {
		return
	}

	if hash := f.str("hash"); legacyPHPEmpty(hash) {
		// Password mode: ROOT_USER only, checked by the core as the login page does
		// (decision 20). A wrong user name runs nothing, so it never reaches auth.log.
		root := cfg["ROOT_USER"]
		if root == "" || !f.isset("user") || f.str("user") != root {
			legacyError(w, legacyExitForbidden, legacyMsgAuth, codeMode, "")
			return
		}
		if !f.isset("password") {
			legacyError(w, legacyExitPassword, legacyMsgAuth, codeMode, "")
			return
		}
		ok, err := s.Auth.CheckPassword(ctx, root, f.str("password"), ip)
		if err != nil {
			s.legacyInternal(w, "v-check-user-hash", err)
			return
		}
		if !ok {
			legacyError(w, legacyExitPassword, legacyMsgAuth, codeMode, "")
			return
		}
	} else {
		key := legacyKeysDir + legacyPHPBasename(hash)
		// The core reads the temp file's first line: a newline would cut the key.
		if strings.ContainsAny(key, "\x00\n") {
			s.legacyInternal(w, "v-check-api-key", errors.New("NUL or newline in key"))
			return
		}
		// The key goes in a temp file, not argv (O6): the core reads a /tmp path's
		// first line.
		keyFile, cleanup, err := secret.TempFile(s.tmpDir(), key)
		if err != nil {
			s.legacyInternal(w, "v-check-api-key", err)
			return
		}
		res, err := s.R.Run(ctx, "v-check-api-key", keyFile, ip)
		cleanup()
		if err != nil {
			s.legacyInternal(w, "v-check-api-key", err)
			return
		}
		if res.Exit > 0 {
			legacyError(w, legacyExitPassword, legacyMsgAuth, codeMode, "")
			return
		}
	}

	cmd := legacyPHPTrim(f.str("cmd"))
	args, byName := legacyArgs(f)
	if !legacyCheckCommand(w, cmd, codeMode) {
		return
	}

	if cmd == legacyMakeTmpFile {
		// PHP passed the name through escapeshellcmd here (not in access-key mode).
		if err := s.legacyWriteTmpFile(legacyPHPBasename(legacyPHPEscapeShellCmd(byName["arg2"])), byName["arg1"]); err != nil {
			s.legacyInternal(w, legacyMakeTmpFile, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		if !codeMode {
			io.WriteString(w, "OK") // the exit code is unset in code mode: an empty body
		}
		return
	}

	res, err := legacyRunAny(ctx, run, cmd, args)
	if err != nil {
		s.legacyInternal(w, cmd, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	if codeMode {
		io.WriteString(w, strconv.Itoa(res.Exit))
		return
	}
	// The auth step's exit code is 0 here, so: "OK" for no output, else the lines.
	if lines := legacyExecLines(res.Stdout); len(lines) > 0 {
		io.WriteString(w, strings.Join(lines, "\n")+"\n")
	} else {
		io.WriteString(w, "OK")
	}
}

// legacyAccessKey is api_connection: an access key and its secret, checked by the
// core with the command (API16-API22).
func (s *server) legacyAccessKey(w http.ResponseWriter, r *http.Request, run runner.AnyRunner, f legacyFields) {
	ctx := r.Context()
	codeMode := f.str("returncode") == "yes"
	ip := session.RealIP(r)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		s.legacyInternal(w, "config", err)
		return
	}
	root := cfg["ROOT_USER"]
	apiStatus := 0.0
	if v := cfg["API_SYSTEM"]; legacyPHPIsNumeric(v) {
		apiStatus, _ = strconv.ParseFloat(strings.Trim(v, " \t\n\r\v\f"), 64)
	}
	if apiStatus == 0 {
		legacyError(w, legacyExitDisabled, legacyMsgKeyDisabled, codeMode, "")
		return
	}
	if legacyIPRefused(w, cfg["API_ALLOWED_IP"], ip, true, codeMode, legacyMsgKeyIP) {
		return
	}

	id, sec, cmd := legacyPHPTrim(f.str("access_key")), legacyPHPTrim(f.str("secret_key")), legacyPHPTrim(f.str("cmd"))
	args, byName := legacyArgs(f)
	if !legacyCheckCommand(w, cmd, codeMode) {
		return
	}
	if legacyPHPEmpty(id) || legacyPHPEmpty(sec) {
		legacyError(w, legacyExitPassword, legacyMsgKeyAuth, codeMode, "")
		return
	}
	// The core reads the secret file's first line: a newline would cut the secret.
	if strings.ContainsAny(id+sec, "\x00\n") {
		s.legacyInternal(w, "v-check-access-key", errors.New("NUL or newline in key"))
		return
	}

	// The core checks the key, its secret, its owner and its permission for cmd, and
	// writes auth.log (API19). The secret goes in a temp file, not argv (O6): the core
	// reads a /tmp path's first line.
	secretFile, cleanup, err := secret.TempFile(s.tmpDir(), sec)
	if err != nil {
		s.legacyInternal(w, "v-check-access-key", err)
		return
	}
	res, err := s.R.Run(ctx, "v-check-access-key", id, secretFile, cmd, ip, "json")
	cleanup()
	if err != nil {
		s.legacyInternal(w, "v-check-access-key", err)
		return
	}
	if res.Exit > 0 {
		legacyError(w, res.Exit, strings.Join(legacyExecLines(res.Stdout), "\n"), codeMode, "")
		return
	}
	keyUser, pos := legacyKeyData(res.Stdout)
	rootKey := root != "" && keyUser == root
	if !rootKey && apiStatus < 2 {
		legacyError(w, legacyExitAPIDisabled, legacyMsgKeyDisabled, codeMode, "")
		return
	}
	// The command's USER argument, by its original argN name, must be the key's owner
	// (API21). Compared exactly (PHP's != was loose), and also at the argv slot the
	// command reads it from: missing argN shift later ones left (review minor).
	if !rootKey && pos != "" {
		n, _ := strconv.Atoi(pos)
		if v, ok := byName["arg"+pos]; !ok || v != keyUser || n < 1 || n > len(args) || args[n-1] != keyUser {
			legacyError(w, legacyExitForbidden, `Key `+id+` - the "user" argument doesn\'t match the key\'s user`, codeMode, "")
			return
		}
	}

	exit, output := 0, ""
	if cmd == legacyMakeTmpFile {
		if err := s.legacyWriteTmpFile(legacyPHPBasename(byName["arg2"]), byName["arg1"]); err != nil {
			s.legacyInternal(w, legacyMakeTmpFile, err)
			return
		}
	} else {
		res, err := legacyRunAny(ctx, run, cmd, args)
		if err != nil {
			s.legacyInternal(w, cmd, err)
			return
		}
		exit, output = res.Exit, legacyPHPTrim(strings.Join(legacyExecLines(res.Stdout), "\n"))
	}

	w.Header().Set("Hestia-Exit-Code", strconv.Itoa(exit))
	switch {
	case codeMode:
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, strconv.Itoa(exit))
		return
	case exit > 0:
		w.WriteHeader(legacyHTTPStatus(exit))
	case legacyPHPEmpty(output):
		w.WriteHeader(http.StatusNoContent) // a "0" output is empty() too; 204 carries no body
		return
	default:
		if legacyPHPJSONTruthy(output) {
			w.Header().Set("Content-Type", legacyJSONType)
		}
		w.WriteHeader(http.StatusOK)
	}
	io.WriteString(w, output)
}

// legacyKeyData reads v-check-access-key's {"USER": …, "USER_ARG_POSITION": n}. pos is
// the argument number as index.php built "arg{$pos}", or "" when the user check is
// skipped (missing, not numeric, or not above 0).
func legacyKeyData(stdout []byte) (user, pos string) {
	f, ok := legacyPHPJSONFields([]byte(strings.Join(legacyExecLines(stdout), "")))
	if !ok {
		return "", ""
	}
	user = f.str("USER")
	p := f["USER_ARG_POSITION"]
	if p.array || !legacyPHPIsNumeric(p.val) {
		return user, ""
	}
	if n, err := strconv.ParseFloat(strings.TrimSpace(p.val), 64); err != nil || n <= 0 {
		return user, ""
	}
	return user, strings.TrimSpace(p.val)
}

// legacyPHPIsNumericRe is PHP 8's is_numeric for strings: optional surrounding whitespace,
// a sign, digits with an optional fraction, an optional exponent.
var legacyPHPIsNumericRe = regexp.MustCompile(`^[ \t\n\r\v\f]*[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?[ \t\n\r\v\f]*$`)

func legacyPHPIsNumeric(s string) bool { return legacyPHPIsNumericRe.MatchString(s) }

// legacyCheckCommand refuses an empty or malformed command name (API7) and reports
// whether it may go on.
func legacyCheckCommand(w http.ResponseWriter, cmd string, codeMode bool) bool {
	if legacyPHPEmpty(cmd) {
		legacyError(w, legacyExitInvalid, legacyMsgNoCommand, codeMode, "")
		return false
	}
	if !legacyCmdRe.MatchString(cmd) {
		legacyError(w, legacyExitInvalid, cmd+" command invalid", codeMode, "")
		return false
	}
	return true
}

// legacyRunAny runs the pass-through command as argv (never a shell), 300 s at most.
// A name with no command behind it is what sudo's failure gave PHP: exit 1, no output
// (decision 22). An argument holding a NUL can't be passed at all.
func legacyRunAny(ctx context.Context, run runner.AnyRunner, cmd string, args []string) (runner.Result, error) {
	for _, a := range args {
		if strings.ContainsRune(a, 0) {
			return runner.Result{}, errors.New("NUL in an argument")
		}
	}
	res, err := run.RunAny(runner.WithTimeout(ctx, legacyRunTimeout), cmd, args...)
	if errors.Is(err, runner.ErrNoCommand) || errors.Is(err, runner.ErrNotAllowed) {
		return runner.Result{Exit: 1}, nil
	}
	return res, err
}

// legacyWriteTmpFile is v-make-tmp-file (API24, decision 21): content plus "\n" into
// <TmpDir>/<name>, 0600, truncating a file already there. It writes nothing for an
// empty, "." or ".." name or one starting with "." (the caller answers as if it
// worked, as PHP did), and fails (a 500) rather than follow a symlink, write to
// anything but a regular file, or lose a write.
func (s *server) legacyWriteTmpFile(name, content string) error {
	if name == "" || name[0] == '.' || strings.ContainsAny(name, "/\x00") {
		return nil
	}
	path := filepath.Join(s.tmpDir(), name)
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return err
	}
	if fi, err := fd.Stat(); err != nil || !fi.Mode().IsRegular() {
		fd.Close()
		return errors.New("not a regular file")
	}
	_ = fd.Chmod(0o600) // an existing file keeps its mode otherwise
	if _, err := io.WriteString(fd, content+"\n"); err != nil {
		fd.Close()
		return err
	}
	return fd.Close()
}

// legacyInternal answers a failure index.php never had (a runner error, an unreadable
// config): 500, nothing echoed. The log names the step, never arguments or secrets.
func (s *server) legacyInternal(w http.ResponseWriter, step string, err error) {
	slog.Error("legacy api", "step", step, "err", err)
	w.WriteHeader(http.StatusInternalServerError)
}

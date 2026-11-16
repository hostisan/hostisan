package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

// read wraps a handler for signed-in users.
func (s *server) read(h http.HandlerFunc) http.Handler {
	m := s.Sessions
	return m.Load(m.RequireAuth(h))
}

// write wraps a handler that changes data: signed in, not read-only, CSRF-checked.
func (s *server) write(h http.HandlerFunc) http.Handler {
	m := s.Sessions
	return m.Load(m.RequireAuth(m.RequireWritable(m.RequireCSRF(h))))
}

// adminWrite is write for admin-only actions: users get 403 forbidden.
func (s *server) adminWrite(h http.HandlerFunc) http.Handler {
	m := s.Sessions
	return m.Load(m.RequireAuth(m.RequireAdmin(m.RequireWritable(m.RequireCSRF(h)))))
}

// actingUser is the account a request acts on: the looked-at user while an admin uses
// login-as, else the signed-in user. Only valid behind read or write.
func (s *server) actingUser(r *http.Request) string {
	return s.Sessions.Effective(session.FromContext(r.Context()))
}

// run executes a command and turns a non-zero exit into a *runner.CommandError.
func (s *server) run(ctx context.Context, cmd string, args ...string) error {
	res, err := s.R.Run(ctx, cmd, args...)
	if err != nil {
		return err
	}
	return res.Err(cmd)
}

var exitErrors = map[int]struct {
	status int
	code   string
}{
	runner.ExitArgs:        {http.StatusBadRequest, "invalid"},
	runner.ExitInvalid:     {http.StatusBadRequest, "invalid"},
	runner.ExitParsing:     {http.StatusBadRequest, "invalid"},
	runner.ExitNotExist:    {http.StatusNotFound, "not_found"},
	runner.ExitExists:      {http.StatusConflict, "exists"},
	runner.ExitSuspended:   {http.StatusConflict, "suspended"},
	runner.ExitUnsuspended: {http.StatusConflict, "not_suspended"},
	runner.ExitInUse:       {http.StatusConflict, "in_use"},
	runner.ExitLimit:       {http.StatusConflict, "limit"},
	runner.ExitForbidden:   {http.StatusForbidden, "forbidden"},
	runner.ExitDisabled:    {http.StatusConflict, "disabled"},
	runner.ExitRestart:     {http.StatusInternalServerError, "restart_failed"},
}

// commandMessage is what Hestia printed, or "Error code: N" when it printed nothing,
// as check_return_code did.
func commandMessage(e *runner.CommandError) string {
	out := strings.TrimSpace(e.Output)
	if out == "" {
		return "Error code: " + strconv.Itoa(e.Exit)
	}
	return brand.Rewrite(out)
}

// errorMessage is the message writeCommandError would send for err; like serverError,
// it logs an error that is not Hestia's own.
func errorMessage(err error) string {
	var ce *runner.CommandError
	switch {
	case errors.As(err, &ce):
		return commandMessage(ce)
	case errors.Is(err, runner.ErrTimeout):
		return "Error: command timed out"
	}
	slog.Error("request failed", "err", err)
	return "Error: internal error"
}

// writeCommandError answers a failed command: the exit code picks the status and
// code, and Hestia's own output is the message (not translated, as in the PHP panel).
func writeCommandError(w http.ResponseWriter, err error) {
	var ce *runner.CommandError
	switch {
	case errors.As(err, &ce):
		status, code := http.StatusInternalServerError, "command_failed"
		if m, ok := exitErrors[ce.Exit]; ok {
			status, code = m.status, m.code
		}
		WriteError(w, status, code, commandMessage(ce))
	case errors.Is(err, runner.ErrTimeout):
		WriteError(w, http.StatusGatewayTimeout, "timeout", "")
	default:
		serverError(w, err)
	}
}

// maxBody caps a request body; a bigger one is answered 400 invalid.
const maxBody = 1 << 20

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v)
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		WriteError(w, http.StatusBadRequest, "invalid", requestTooBigMsg)
		return false
	case err != nil:
		WriteError(w, http.StatusBadRequest, "bad_request", badRequestMsg)
		return false
	}
	return true
}

const blankFieldMsg = `Field "%s" can not be blank.`

// blankFields answers 400 with Ruling B's message and args when any named field is
// empty; false means it did. Pairs are label, value.
func blankFields(w http.ResponseWriter, pairs ...string) bool {
	var blank []string
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			blank = append(blank, pairs[i])
		}
	}
	if len(blank) == 0 {
		return true
	}
	writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: blankFieldMsg, Args: blank}})
	return false
}

// maxBulk caps the names of one bulk run.
const maxBulk = 500

// bulk runs one command per name. argsFor builds an item's arguments from the acting
// user and the name; finish runs once after all items (nil for none). Every item is
// attempted; failures are reported. Only delete is open to users.
func (s *server) bulk(w http.ResponseWriter, r *http.Request, cmds map[string]string, valid func(string) bool,
	argsFor func(user, name string) []string, finish [][]string) {
	s.bulkFor(w, r, cmds, []string{"delete"}, valid, argsFor, finish)
}

// bulkFor is bulk with the actions users may run named in userActions; the others
// are admin-only.
func (s *server) bulkFor(w http.ResponseWriter, r *http.Request, cmds map[string]string, userActions []string,
	valid func(string) bool, argsFor func(user, name string) []string, finish [][]string) {
	var req BulkRequest
	if !decodeBody(w, r, &req) {
		return
	}
	cmd, known := cmds[req.Action]
	if !bulkShapeOK(w, known, req.Action, len(req.Names), maxBulk) {
		return
	}
	if !slices.Contains(userActions, req.Action) && session.FromContext(r.Context()).UserContext != "admin" {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return
	}
	// The cap keeps a bulk run well inside the server's write timeout.
	for _, n := range req.Names {
		if !valid(n) {
			invalid(w, invalidValueMsg, n)
			return
		}
	}
	user := s.actingUser(r)
	failed := []BulkFailure{}
	for _, n := range req.Names {
		if err := s.run(r.Context(), cmd, argsFor(user, n)...); err != nil {
			failed = append(failed, BulkFailure{Name: n, Message: errorMessage(err)})
		}
	}
	if len(finish) > 0 && !s.steps(w, r, finish) {
		return
	}
	writeJSON(w, http.StatusOK, BulkReply{Status: "ok", Failed: failed})
}

// output runs a read command and returns its stdout, answering the request itself on
// failure (ok is false then).
func (s *server) output(w http.ResponseWriter, r *http.Request, cmd string, args ...string) ([]byte, bool) {
	res, err := s.R.Run(r.Context(), cmd, args...)
	if err == nil {
		err = res.Err(cmd)
	}
	if err != nil {
		writeCommandError(w, err)
		return nil, false
	}
	return res.Stdout, true
}

// firstErr is err, or a new error with msg when err is nil.
func firstErr(err error, msg string) error {
	if err != nil {
		return err
	}
	return errors.New(msg)
}

// steps runs commands in order and answers the first failure; false means it did.
func (s *server) steps(w http.ResponseWriter, r *http.Request, steps [][]string) bool {
	for _, st := range steps {
		if err := s.run(r.Context(), st[0], st[1:]...); err != nil {
			writeCommandError(w, err)
			return false
		}
	}
	return true
}

// safeRequestHost is the request's Host without its port, when that is a valid
// hostname or IP address; "" otherwise. The Host header is client-controlled, so it
// must not reach a URL unchecked.
func safeRequestHost(r *http.Request) string {
	h := r.Host
	if host, port, err := net.SplitHostPort(h); err == nil {
		if port == "" || strings.Trim(port, "0123456789") != "" {
			return ""
		}
		h = host
	} else {
		h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	}
	if ip, err := netip.ParseAddr(h); err == nil {
		if ip.Zone() != "" {
			return ""
		}
		return h
	}
	if validHostname(h) {
		return h
	}
	return ""
}

// validHostname accepts dot-separated labels of letters, digits and inner hyphens.
func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

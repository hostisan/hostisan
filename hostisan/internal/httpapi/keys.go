package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/session"
)

// fileManagerKeyID is the file manager's own key: hidden, not counted, not deletable.
const fileManagerKeyID = "filemanager.ssh.key"

const (
	sshExistsMsg           = "SSH Key already exists."
	sshOneLineMsg          = "The SSH key must be on one line. Paste the key as a single line."
	sshOptionsMsg          = "SSH key options are not supported. Paste only the key, starting with its type."
	sshControlCharsMsg     = "The SSH key contains characters that are not allowed."
	sshReservedMsg         = "The key comment %s is reserved for the file manager."
	commentControlCharsMsg = "The comment contains characters that are not allowed."
	apiNotAllowedMsg       = "API %s not allowed"
)

// keyRoutes registers SSH keys, access keys, notifications and logs (Plan 3 Slice D). Called from registerAccountRoutes.
func (s *server) keyRoutes(mux *http.ServeMux) {
	const ssh, ak = "/api/v1/account/ssh-keys", "/api/v1/account/access-keys"
	mux.Handle("GET "+ssh, s.read(s.sshList))
	mux.Handle("POST "+ssh, s.write(s.sshAdd))
	mux.Handle("DELETE "+ssh+"/{id}", s.write(s.sshDelete))

	mux.Handle("GET "+ak, s.read(s.apiGate(s.akList)))
	mux.Handle("GET "+ak+"/{key}", s.read(s.apiGate(s.akOne)))
	mux.Handle("POST "+ak, s.write(s.apiGate(s.akCreate)))
	mux.Handle("POST "+ak+"/bulk", s.write(s.apiGate(s.akBulk)))
	mux.Handle("DELETE "+ak+"/{key}", s.write(s.apiGate(s.akDelete)))

	const n = "/api/v1/notifications"
	mux.Handle("GET "+n, s.read(s.notifList))
	mux.Handle("DELETE "+n, s.write(s.notifAction("v-delete-user-notification", false)))
	mux.Handle("DELETE "+n+"/{id}", s.write(s.notifAction("v-delete-user-notification", true)))
	mux.Handle("POST "+n+"/ack", s.write(s.notifAction("v-acknowledge-user-notification", false)))
	mux.Handle("POST "+n+"/{id}/ack", s.write(s.notifAction("v-acknowledge-user-notification", true)))

	mux.Handle("GET /api/v1/logs", s.read(s.logList))
	mux.Handle("DELETE /api/v1/logs", s.write(s.logDelete("v-delete-user-log", false)))
	mux.Handle("GET /api/v1/logs/auth", s.read(s.authLogList))
	mux.Handle("DELETE /api/v1/logs/auth", s.write(s.logDelete("v-delete-user-auth-log", true)))
}

// decodeOrEmpty decodes a list command's output; blank output is an empty list.
func decodeObjectsOrEmpty[T any, PT interface {
	*T
	SetName(string)
}](out []byte) ([]T, error) {
	if strings.TrimSpace(string(out)) == "" {
		return []T{}, nil
	}
	return hestia.DecodeObjects[T, PT](out)
}

// ---------------------------------------------------------------- SSH keys

// isFileManagerKey is true for the file manager's own entry. Its listed ID is the whole
// authorized_keys comment ("root@host TS:<expiry> filemanager.ssh.key"), so match the marker.
func isFileManagerKey(id string) bool { return strings.Contains(id, fileManagerKeyID) }

// sshKeys lists the user's keys with the file manager's own key left out.
func (s *server) sshKeys(w http.ResponseWriter, r *http.Request, user string) (listed, shown []hestia.SSHKey, ok bool) {
	out, ok := s.output(w, r, "v-list-user-ssh-key", user, "json")
	if !ok {
		return nil, nil, false
	}
	listed, err := decodeObjectsOrEmpty[hestia.SSHKey](out)
	if err != nil {
		serverError(w, err)
		return nil, nil, false
	}
	shown = []hestia.SSHKey{}
	for _, k := range listed {
		if !isFileManagerKey(k.ID) {
			shown = append(shown, k)
		}
	}
	return listed, shown, true
}

func (s *server) sshList(w http.ResponseWriter, r *http.Request) {
	_, shown, ok := s.sshKeys(w, r, s.actingUser(r))
	if ok {
		writeJSON(w, http.StatusOK, SSHKeys{Keys: shown})
	}
}

// sshFingerprint is ssh-keygen's SHA256 fingerprint of a public key's base64 body.
func sshFingerprint(body string) string {
	b, err := base64.StdEncoding.DecodeString(body)
	if err != nil || len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func (s *server) sshAdd(w http.ResponseWriter, r *http.Request) {
	var req AddSSHKeyRequest
	if !decodeBody(w, r, &req) {
		return
	}
	key := strings.TrimSpace(req.Key)
	if !blankFields(w, "SSH Key", key) {
		return
	}
	// One line only: a newline would append further authorized_keys entries.
	for _, c := range key {
		if c == '\n' || c == '\r' {
			WriteError(w, http.StatusBadRequest, "invalid", sshOneLineMsg)
			return
		}
		if (c < ' ' && c != '\t') || c == 0x7f {
			WriteError(w, http.StatusBadRequest, "invalid", sshControlCharsMsg)
			return
		}
	}
	f := strings.Fields(key)
	if len(f) < 2 {
		WriteError(w, http.StatusBadRequest, "invalid", sshOneLineMsg)
		return
	}
	// A key line starts with its type; anything else is an options prefix (command=,
	// from=, no-pty ...) that would change what the key may do.
	if !sshKeyTypeRe.MatchString(f[0]) {
		WriteError(w, http.StatusBadRequest, "invalid", sshOptionsMsg)
		return
	}
	comment := strings.Join(f[2:], " ")
	if isFileManagerKey(comment) {
		invalid(w, sshReservedMsg, comment)
		return
	}
	user := s.actingUser(r)
	listed, _, ok := s.sshKeys(w, r, user)
	if !ok {
		return
	}
	fp := sshFingerprint(f[1])
	for _, k := range listed {
		if (fp != "" && k.KEY == fp) || (comment != "" && k.ID == comment) {
			WriteError(w, http.StatusConflict, "exists", sshExistsMsg)
			return
		}
	}
	if comment == "" {
		var id [8]byte
		if _, err := rand.Read(id[:]); err != nil {
			serverError(w, err)
			return
		}
		comment = hex.EncodeToString(id[:])
	}
	// The public key is not a secret, and the bin takes it as an argument.
	if err := s.run(r.Context(), "v-add-user-ssh-key", user, f[0]+" "+f[1]+" "+comment); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) sshDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !hestia.ValidSSHKeyID(id) {
		invalidFormat(w, "id", id)
		return
	}
	user := s.actingUser(r)
	// The bin deletes every line with this comment, so only a listed ID goes through.
	_, shown, ok := s.sshKeys(w, r, user)
	if !ok {
		return
	}
	if !slices.ContainsFunc(shown, func(k hestia.SSHKey) bool { return k.ID == id }) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if err := s.run(r.Context(), "v-delete-user-ssh-key", user, id); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// ---------------------------------------------------------------- access keys

// apiGate lets a request through when API_SYSTEM allows access keys for the acting
// user: 1 or more for ROOT_USER, 2 for everyone else.
func (s *server) apiGate(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, err := s.H.Config(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		level, _ := strconv.Atoi(cfg["API_SYSTEM"])
		need := 2
		if s.actingUser(r) == rootUser(cfg) {
			need = 1
		}
		if level < need {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return
		}
		h(w, r)
	}
}

// apisFor lists the APIs the acting user's role may grant.
func (s *server) apisFor(w http.ResponseWriter, r *http.Request, user string) ([]hestia.APIInfo, bool) {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	out, ok := s.output(w, r, "v-list-apis", "json")
	if !ok {
		return nil, false
	}
	all, err := decodeObjectsOrEmpty[hestia.APIInfo](out)
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	apis := []hestia.APIInfo{}
	for _, a := range all {
		if user == rootUser(cfg) || a.ROLE == "user" {
			apis = append(apis, a)
		}
	}
	sort.Slice(apis, func(i, j int) bool { return apis[i].Name < apis[j].Name })
	return apis, true
}

func (s *server) akList(w http.ResponseWriter, r *http.Request) {
	user := s.actingUser(r)
	out, ok := s.output(w, r, "v-list-access-keys", user, "json")
	if !ok {
		return
	}
	keys, err := decodeObjectsOrEmpty[hestia.AccessKey](out)
	if err != nil {
		serverError(w, err)
		return
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return keys[i].DATE+" "+keys[i].TIME < keys[j].DATE+" "+keys[j].TIME
	})
	apis, ok := s.apisFor(w, r, user)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, AccessKeys{Keys: keys, APIs: apis})
}

// ownedKey loads one access key and answers 404 unless the acting user owns it. The
// bin's output carries the secret; only the AccessKey fields are decoded from it.
func (s *server) ownedKey(w http.ResponseWriter, r *http.Request, user, id string) (hestia.AccessKey, bool) {
	var k hestia.AccessKey
	out, ok := s.output(w, r, "v-list-access-key", id, "json")
	if !ok {
		return k, false
	}
	if err := json.Unmarshal(out, &k); err != nil {
		serverError(w, err)
		return k, false
	}
	if k.USER != user {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return k, false
	}
	return k, true
}

func keyParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("key")
	if !hestia.ValidAccessKeyID(id) {
		invalidFormat(w, "key", id)
		return "", false
	}
	return id, true
}

func (s *server) akOne(w http.ResponseWriter, r *http.Request) {
	id, ok := keyParam(w, r)
	if !ok {
		return
	}
	if k, ok := s.ownedKey(w, r, s.actingUser(r), id); ok {
		if k.PERMISSIONS == nil {
			k.PERMISSIONS = []string{}
		}
		writeJSON(w, http.StatusOK, k)
	}
}

func (s *server) akCreate(w http.ResponseWriter, r *http.Request) {
	var req AddAccessKeyRequest
	if !decodeBody(w, r, &req) {
		return
	}
	comment := strings.TrimSpace(req.Comment)
	for _, c := range comment {
		if c < ' ' || c == 0x7f {
			WriteError(w, http.StatusBadRequest, "invalid", commentControlCharsMsg)
			return
		}
	}
	if len(req.Permissions) == 0 {
		blankFields(w, "Permissions", "")
		return
	}
	user := s.actingUser(r)
	apis, ok := s.apisFor(w, r, user)
	if !ok {
		return
	}
	var perms []string
	for _, p := range req.Permissions {
		if !slices.ContainsFunc(apis, func(a hestia.APIInfo) bool { return a.Name == p }) {
			writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: apiNotAllowedMsg, Args: []string{p}}})
			return
		}
		if !slices.Contains(perms, p) {
			perms = append(perms, p)
		}
	}
	out, ok := s.output(w, r, "v-add-access-key", user, strings.Join(perms, ","), comment, "json")
	if !ok {
		return
	}
	var created struct {
		hestia.AccessKey
		Secret string `json:"SECRET_ACCESS_KEY"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		serverError(w, err)
		return
	}
	if created.PERMISSIONS == nil {
		created.PERMISSIONS = []string{}
	}
	// The one reply that carries the secret.
	writeJSON(w, http.StatusOK, AccessKeyCreated{Key: created.AccessKey, Secret: created.Secret})
}

func (s *server) akDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := keyParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	if _, ok := s.ownedKey(w, r, user, id); !ok {
		return
	}
	if err := s.run(r.Context(), "v-delete-access-key", id); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) akBulk(w http.ResponseWriter, r *http.Request) {
	var req BulkRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !bulkShapeOK(w, req.Action == "delete", req.Action, len(req.Names), maxBulk) {
		return
	}
	for _, n := range req.Names {
		if !hestia.ValidAccessKeyID(n) {
			invalidFormat(w, "key", n)
			return
		}
	}
	user := s.actingUser(r)
	failed := []BulkFailure{}
	for _, id := range req.Names {
		out, err := s.R.Run(r.Context(), "v-list-access-key", id, "json")
		if err == nil {
			err = out.Err("v-list-access-key")
		}
		var k hestia.AccessKey
		if err == nil {
			err = json.Unmarshal(out.Stdout, &k)
		}
		if err != nil {
			failed = append(failed, BulkFailure{Name: id, Message: errorMessage(err)})
			continue
		}
		if k.USER != user {
			failed = append(failed, BulkFailure{Name: id, Message: "Error: access key doesn't exist"})
			continue
		}
		if err := s.run(r.Context(), "v-delete-access-key", id); err != nil {
			failed = append(failed, BulkFailure{Name: id, Message: errorMessage(err)})
		}
	}
	writeJSON(w, http.StatusOK, BulkReply{Status: "ok", Failed: failed})
}

// ---------------------------------------------------------------- notifications

var sshKeyTypeRe = regexp.MustCompile(`^(ssh|ecdsa|sk)-[a-z0-9@.-]+$|^ecdsa-sha2-[a-z0-9]+$`)

var notifIDRe = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)

func (s *server) notifList(w http.ResponseWriter, r *http.Request) {
	out, ok := s.output(w, r, "v-list-user-notifications", s.actingUser(r), "json")
	if !ok {
		return
	}
	items, err := decodeObjectsOrEmpty[hestia.Notification](out)
	if err != nil {
		serverError(w, err)
		return
	}
	num := func(v string) int { n, _ := strconv.Atoi(v); return n }
	sort.SliceStable(items, func(i, j int) bool { return num(items[i].ID) > num(items[j].ID) })
	sort.SliceStable(items, func(i, j int) bool { return num(items[i].PRIORITY) > num(items[j].PRIORITY) })
	writeJSON(w, http.StatusOK, Notifications{Items: items})
}

// notifAction deletes or acknowledges one notification (byID) or all of them.
func (s *server) notifAction(cmd string, byID bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := "all"
		if byID {
			target = r.PathValue("id")
			if !notifIDRe.MatchString(target) {
				invalidFormat(w, "id", target)
				return
			}
		}
		if err := s.run(r.Context(), cmd, s.actingUser(r), target); err != nil {
			writeCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}

// ---------------------------------------------------------------- logs

// logTarget picks whose log a request reads or clears. ?user= (a name or "system") is
// admin-only; other users get their own log if the policy allows.
func (s *server) logTarget(w http.ResponseWriter, r *http.Request, deleting bool) (string, bool) {
	sess := session.FromContext(r.Context())
	admin := sess.UserContext == "admin"
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid", badRequestMsg)
		return "", false
	}
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return "", false
	}
	// An empty ?user= means no user: the own log.
	if u := q.Get("user"); u != "" {
		if !admin {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return "", false
		}
		if !hestia.ValidUsername(u) {
			invalidFormat(w, "user", u)
			return "", false
		}
		// With PROTECTED_ADMIN on, ROOT_USER's logs are for ROOT_USER alone.
		if cfg["POLICY_SYSTEM_PROTECTED_ADMIN"] == "yes" && u == rootUser(cfg) && sess.User != rootUser(cfg) {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return "", false
		}
		return u, true
	}
	if !admin {
		key := "POLICY_USER_VIEW_LOGS"
		if deleting {
			key = "POLICY_USER_DELETE_LOGS"
		}
		if cfg[key] == "no" {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return "", false
		}
	}
	return s.actingUser(r), true
}

// protectedAdmin reports an admin looking at ROOT_USER with PROTECTED_ADMIN on.
func (s *server) protectedAdmin(w http.ResponseWriter, r *http.Request) bool {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return true
	}
	sess := session.FromContext(r.Context())
	if sess.UserContext == "admin" && sess.Look != "" && sess.Look == rootUser(cfg) && cfg["POLICY_SYSTEM_PROTECTED_ADMIN"] == "yes" {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return true
	}
	return false
}

// demoMode answers 403 when DEMO_MODE is on: Hestia's demo hides the login history.
// It reports whether it answered.
func (s *server) demoMode(w http.ResponseWriter, r *http.Request) bool {
	cfg, err := s.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return true
	}
	if cfg["DEMO_MODE"] == "yes" {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return true
	}
	return false
}

func reversed[T any](s []T) []T {
	out := make([]T, 0, len(s))
	for i := len(s) - 1; i >= 0; i-- {
		out = append(out, s[i])
	}
	return out
}

func (s *server) logList(w http.ResponseWriter, r *http.Request) {
	user, ok := s.logTarget(w, r, false)
	if !ok {
		return
	}
	out, ok := s.output(w, r, "v-list-user-log", user, "json")
	if !ok {
		return
	}
	entries, err := decodeObjectsOrEmpty[hestia.LogEntry](out)
	if err != nil {
		// Upstream prints a message's backslashes raw ("rm {} \;"): read them as text.
		entries, err = decodeObjectsOrEmpty[hestia.LogEntry](hestia.EscapeRawBackslashes(out))
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Logs{Entries: reversed(entries), User: user})
}

func (s *server) authLogList(w http.ResponseWriter, r *http.Request) {
	user, ok := s.logTarget(w, r, false)
	if !ok || s.protectedAdmin(w, r) || s.demoMode(w, r) {
		return
	}
	out, ok := s.output(w, r, "v-list-user-auth-log", user, "json")
	if !ok {
		return
	}
	entries, err := decodeObjectsOrEmpty[hestia.AuthLogEntry](out)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AuthLog{Entries: reversed(entries), User: user})
}

// logDelete clears the user log or the login history.
func (s *server) logDelete(cmd string, auth bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.logTarget(w, r, true)
		if !ok || (auth && (s.protectedAdmin(w, r) || s.demoMode(w, r))) {
			return
		}
		if err := s.run(r.Context(), cmd, user); err != nil {
			writeCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}

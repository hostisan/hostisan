package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

const (
	// restoreMaxPaths caps one request (parity decision 27).
	restoreMaxPaths = 50
	// restorePathTimeout is each v-restore-file-restic call's limit. It is longer than
	// a stopping panel waits for commands (shutdownLimit, 315 s; systemd's
	// TimeoutStopSec 320 s): a panel stop during a long restore ends that restore.
	// That is accepted rather than holding a service stop for 30 minutes: the script
	// restores into a root temp dir first and the restore can be run again.
	restorePathTimeout = 30 * time.Minute

	restoreTitle      = "Restore files"
	restoreBusyMsg    = "A restore is already running."
	restoreMissingMsg = "%s is not in this snapshot."
	restoreLinkMsg    = "%s goes through a link on the server and can't be restored."
	restoreCheckMsg   = "%s can't be checked on the server, so it can't be restored."
	restoreTooManyMsg = "%s holds too many files to check; restore a smaller folder."
	restoreOffMsg     = "Restoring single files is turned off on this server."

	// restoreEnableEnv turns per-file restore on when set to "on" in hostisan's
	// environment. The packaged hostisan.env sets it on (V6 and PF4b passed on the
	// VPS); unset or any other value is off, so a missing file fails closed.
	restoreEnableEnv = "HOSTISAN_FILE_RESTORE"
	// restoreMaxWalk caps the live entries checked under one restored folder.
	restoreMaxWalk = 10000
)

// restoreRoot prefixes every live-tree path the checks look at: "" in production,
// a temp dir in tests. It is a package variable, so tests that set it must not use
// t.Parallel().
var restoreRoot = ""

func restoreEnabled() bool { return os.Getenv(restoreEnableEnv) == "on" }

// restoreRoutes mounts per-file snapshot restore and its job status (Slice A, parity
// decisions 27-28). The status route is /backup/restore-jobs/{id} (ruling R1): the
// planned /backup/incremental/jobs/{id} conflicts with /incremental/{snapshot}/files.
func (s *server) restoreRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/backup/incremental/{snapshot}/restore-files", s.write(s.restoreFiles))
	mux.Handle("GET /api/v1/backup/restore-jobs/{id}", s.read(s.restoreJob))
}

func restoreJobKey(user string) string { return "restore-file:" + user }

// restorePathsRefused answers 400 for paths restorePaths refused: the count, or the
// first path that isn't the user's to restore.
func restorePathsRefused(w http.ResponseWriter, user string, paths []string) {
	if len(paths) == 0 || len(paths) > restoreMaxPaths {
		bulkCount(w, restoreMaxPaths)
		return
	}
	for _, p := range paths {
		if !hestia.ValidRestorePath(user, p) {
			invalidFormat(w, "path", p)
			return
		}
	}
}

// restorePaths validates the request's paths for user (decision 28, before any
// command) and returns them deduplicated, in order.
func restorePaths(user string, paths []string) ([]string, bool) {
	if len(paths) == 0 || len(paths) > restoreMaxPaths {
		return nil, false
	}
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		if !hestia.ValidRestorePath(user, p) {
			return nil, false
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, true
}

// restoreLiveCheck looks at the live tree before p is restored (PF3, D14):
// v-restore-file-restic restores as root into it, so a link the account planted
// (web -> /etc) would be written through. It fails closed: p, each folder on its way
// from /home/<user>, /home/<user>/tmp (the core mktemps and rm -frs there as root)
// and, for a folder, every entry below it (at most restoreMaxWalk) must be no link,
// and anything that can't be looked at (except "doesn't exist yet") refuses. It
// returns "" or the refusal's msgid and the path it names. root is restoreRoot.
func restoreLiveCheck(root, user, p string) (msg, arg string) {
	home := "/home/" + user
	cur := home
	rest := strings.Split(strings.TrimPrefix(p, home+"/"), "/")
	var last os.FileInfo
	for i := -1; i < len(rest); i++ {
		if i >= 0 {
			cur += "/" + rest[i]
		}
		st, err := os.Lstat(root + cur)
		if errors.Is(err, fs.ErrNotExist) {
			last = nil
			break // nothing below exists yet; restic creates it
		}
		if err != nil {
			return restoreCheckMsg, p
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return restoreLinkMsg, p
		}
		last = st
	}
	if last != nil && last.IsDir() {
		if msg := restoreWalk(root + p); msg != "" {
			return msg, p
		}
	}
	tmp := home + "/tmp"
	st, err := os.Lstat(root + tmp)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return restoreCheckMsg, tmp
	case st.Mode()&os.ModeSymlink != 0:
		return restoreLinkMsg, tmp
	}
	return "", ""
}

// restoreWalk checks every entry under dir for links, without following any.
func restoreWalk(dir string) string {
	errTooMany := errors.New("too many")
	errLink := errors.New("link")
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if n++; n > restoreMaxWalk {
			return errTooMany
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return errLink
		}
		return nil
	})
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errLink):
		return restoreLinkMsg
	case errors.Is(err, errTooMany):
		return restoreTooManyMsg
	}
	return restoreCheckMsg
}

func writeRestoreRefusal(w http.ResponseWriter, msg, p string) {
	writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: msg, Args: []string{p}}})
}

func (s *server) restoreFiles(w http.ResponseWriter, r *http.Request) {
	if !restoreEnabled() {
		WriteError(w, http.StatusConflict, "disabled", restoreOffMsg)
		return
	}
	snap, ok := snapshotParam(w, r)
	if !ok {
		return
	}
	var req RestoreFilesRequest
	if !decodeBody(w, r, &req) {
		return
	}
	user := s.actingUser(r)
	paths, ok := restorePaths(user, req.Paths)
	if !ok {
		restorePathsRefused(w, user, req.Paths)
		return
	}
	ctx := r.Context()
	root := restoreRoot // the job keeps the request's value
	u, found, err := s.H.User(ctx, user)
	if err != nil || !found {
		serverError(w, firstErr(err, "restore files: user "+user+" not found"))
		return
	}
	// Refused while suspended (Plan 3 ruling 14) or without incremental backups.
	if u.Suspended() || u["BACKUPS_INCREMENTAL"] != "yes" {
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return
	}
	for _, p := range paths {
		if msg, arg := restoreLiveCheck(root, user, p); msg != "" {
			writeRestoreRefusal(w, msg, arg)
			return
		}
	}
	key := restoreJobKey(user)
	if _, busy := s.jobs.Running(key); busy {
		WriteError(w, http.StatusConflict, "busy", restoreBusyMsg)
		return
	}
	if !s.restoreInSnapshot(w, r, user, snap, paths) {
		return
	}

	id, err := s.jobs.Start(ctx, jobs.Spec{Key: key, Owner: user, Title: restoreTitle,
		Limit: time.Duration(len(paths)) * restorePathTimeout},
		func(jctx context.Context, j *jobs.Job) (any, error) {
			return s.restoreRun(jctx, j, root, user, snap, paths)
		})
	if errors.Is(err, jobs.ErrBusy) {
		WriteError(w, http.StatusConflict, "busy", restoreBusyMsg)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, JobRef{JobID: id})
}

// restoreInSnapshot lists each parent folder once and checks every path is a node
// of the snapshot; false means it answered.
func (s *server) restoreInSnapshot(w http.ResponseWriter, r *http.Request, user, snap string, paths []string) bool {
	listed := map[string]map[string]bool{}
	for _, p := range paths {
		dir := path.Dir(p)
		nodes, ok := listed[dir]
		if !ok {
			out, ok := s.output(w, r, "v-list-user-files-restic", user, snap, dir)
			if !ok {
				return false
			}
			all, err := hestia.DecodeSnapshotNodes(out)
			if err != nil {
				serverError(w, err)
				return false
			}
			nodes = map[string]bool{}
			for _, n := range all {
				if n.Path != dir && path.Dir(n.Path) == dir {
					nodes[n.Path] = true
				}
			}
			listed[dir] = nodes
		}
		if !nodes[p] {
			writeRestoreRefusal(w, restoreMissingMsg, p)
			return false
		}
	}
	return true
}

// restoreRun restores the paths in order and stops at the first failure (ruling R3):
// the steps are the paths tried; the failure names its path in Args.
func (s *server) restoreRun(ctx context.Context, j *jobs.Job, root, user, snap string, paths []string) (any, error) {
	for _, p := range paths {
		j.Step(p)
		// The live tree may have changed since the request was checked.
		if msg, arg := restoreLiveCheck(root, user, p); msg != "" {
			return nil, &jobs.Failure{Code: "invalid", Message: msg, Args: []string{arg}}
		}
		res, err := s.R.Run(runner.WithTimeout(ctx, restorePathTimeout), "v-restore-file-restic", user, snap, p)
		if err == nil {
			err = res.Err("v-restore-file-restic")
		}
		if err != nil {
			return nil, restoreFailure(err, res, p)
		}
		// R5: the script exits 0 even when restic restore fails; restic writes its
		// errors to stderr, which the script doesn't redirect.
		if msg := resticErrors(res.Stdout, res.Stderr); msg != "" {
			return nil, &jobs.Failure{Code: "command_failed", Message: msg, Args: []string{p}}
		}
	}
	return RestoreFilesResult{Restored: paths}, nil
}

// restoreFailure is a failed step's job failure. For a failed command the message is
// restoreMessage over its output (C3 D3), not the raw output.
func restoreFailure(err error, res runner.Result, p string) *jobs.Failure {
	var ce *runner.CommandError
	switch {
	case errors.As(err, &ce):
		code := "command_failed"
		if m, ok := exitErrors[ce.Exit]; ok {
			code = m.code
		}
		out := res.Stdout
		if len(out) == 0 {
			out = []byte(ce.Output)
		}
		msg := restoreMessage(out, res.Stderr)
		if msg == "" {
			msg = "Error code: " + strconv.Itoa(ce.Exit)
		}
		return &jobs.Failure{Code: code, Message: msg, Args: []string{p}}
	case errors.Is(err, runner.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return &jobs.Failure{Code: "timeout", Message: "Error: command timed out", Args: []string{p}}
	}
	slog.Error("file restore failed", "err", err)
	return &jobs.Failure{Code: "internal", Message: "Error: internal error", Args: []string{p}}
}

// restoreMaxCauses is how many cause lines a failure message keeps (restic reports
// one line per file it could not restore); the last "Fatal:" line is kept too.
const restoreMaxCauses = 5

var (
	// restoreTempDir is the script's root-owned work folder; restic and cp name
	// paths inside it, which are shown as the live paths they stand for.
	restoreTempDir = regexp.MustCompile(`/tmp/tmp\.[A-Za-z0-9]+(/root)?`)
	// restoreURLPassword is a password in a repository URL (user:password@host).
	restoreURLPassword = regexp.MustCompile(`(://[^/\s:@]+):[^/\s@]+@`)
)

// restoreCauses is the lines of a failed restore that say why: restic's ("Fatal: …",
// "error: …", "ignoring error for …") and the copy's (cp, mkdir, runuser, chown on
// stderr), in stdout then stderr order, scrubbed of the temp folder and passwords,
// at most restoreMaxCauses of them plus the last "Fatal:" line.
func restoreCauses(outs ...[]byte) []string {
	var lines []string
	seen := map[string]bool{}
	for _, out := range outs {
		for _, l := range strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n") {
			l = strings.TrimSpace(l)
			if !restoreCauseLine(l) {
				continue
			}
			l = restoreURLPassword.ReplaceAllString(restoreTempDir.ReplaceAllString(l, ""), "$1:***@")
			if !seen[l] {
				seen[l] = true
				lines = append(lines, l)
			}
		}
	}
	if len(lines) > restoreMaxCauses+1 {
		last := lines[len(lines)-1]
		lines = append(lines[:restoreMaxCauses:restoreMaxCauses], "…")
		if strings.HasPrefix(last, "Fatal:") {
			lines = append(lines, last)
		}
	}
	return lines
}

func restoreCauseLine(l string) bool {
	for _, pre := range []string{"Fatal:", "error:", "ignoring error for ", "cp: ", "mkdir: ", "runuser: ", "chown: "} {
		if strings.HasPrefix(l, pre) {
			return true
		}
	}
	return false
}

// restoreMessage is a failed script's message: the causes, then the script's own
// "Error: …" lines (check_result), without restic's progress output.
func restoreMessage(stdout, stderr []byte) string {
	lines := restoreCauses(stdout, stderr)
	for _, l := range strings.Split(strings.ReplaceAll(string(stdout), "\r", ""), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "Error:") {
			lines = append(lines, restoreTempDir.ReplaceAllString(l, ""))
		}
	}
	return brand.Rewrite(strings.Join(lines, "\n"))
}

// resticErrors is the causes in a run the script reported as a success (R5), or "".
func resticErrors(outs ...[]byte) string {
	return brand.Rewrite(strings.Join(restoreCauses(outs...), "\n"))
}

func (s *server) restoreJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := s.actingUser(r)
	if !jobs.ValidID(id) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	st, ok := s.jobs.Get(id)
	if !ok || st.Key != restoreJobKey(user) || st.Owner != user {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	writeJSON(w, http.StatusOK, jobStatusBody(st))
}

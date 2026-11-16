package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/secret"
)

// Messages are Hestia's msgids, typos included: the translations are keyed on them
// (Parity 32).
const (
	bkQueuedBackupMsg   = "Task has been added to the queue. You will receive an email notification when your backup is ready for download."
	bkBackupRunningMsg  = "An existing backup task is already running, please wait for it to complete."
	bkQueuedRestoreMsg  = "Task has been added to the queue. You will receive an email notification when your restore has been completed."
	bkRestoreRunningMsg = "An existing restoration task is already running. Please wait for it to finish before launching it again."
	bkDownloadMsg       = "Download of remote backup file has been scheduled."
	bkSnapshotMsg       = "Snapshot has been sheduled"
	bkNoItemsMsg        = "No items were selected to restore."
	bkBadExclusionsMsg  = "The %s exclusion list is not valid."
	bkNoDirRestoreMsg   = "Single directories can't be restored from this backup."
	bkNoOutputMsg       = "Error: Hestia did not return any output."
)

// backupRoutes registers backups and snapshots (Plan 3 Slice F). Called from registerAccountRoutes.
//
// The download is GET /backup/file/{backup}, not /backup/{backup}/file: the latter
// overlaps GET /backup/incremental/{snapshot} and would panic the router.
func (s *server) backupRoutes(mux *http.ServeMux) {
	const b = "/api/v1/backup"
	mux.Handle("GET "+b, s.read(s.backupList))
	mux.Handle("POST "+b, s.write(s.backupSchedule))
	mux.Handle("POST "+b+"/bulk", s.write(s.backupBulk))
	mux.Handle("GET "+b+"/exclusions", s.read(s.backupExclusions))
	mux.Handle("PUT "+b+"/exclusions", s.write(s.backupExclusionsSave))
	mux.Handle("GET "+b+"/file/{backup}", s.read(s.backupFile))
	mux.Handle("GET "+b+"/incremental", s.read(s.snapshotList))
	mux.Handle("POST "+b+"/incremental", s.write(s.snapshotSchedule))
	mux.Handle("POST "+b+"/incremental/bulk", s.write(s.snapshotBulk))
	mux.Handle("GET "+b+"/incremental/{snapshot}", s.read(s.snapshotView))
	mux.Handle("GET "+b+"/incremental/{snapshot}/files", s.read(s.snapshotFiles))
	mux.Handle("POST "+b+"/incremental/{snapshot}/restore", s.write(s.snapshotRestore))
	mux.Handle("GET "+b+"/{backup}", s.read(s.backupView))
	mux.Handle("DELETE "+b+"/{backup}", s.write(s.backupDelete))
	mux.Handle("POST "+b+"/{backup}/restore", s.write(s.backupRestore))
	// Preparing a download needs a session and CSRF, but not a writable account: as in
	// Hestia, a read-only user may download (Parity 28).
	m := s.Sessions
	mux.Handle("POST "+b+"/{backup}/download", m.Load(m.RequireAuth(m.RequireCSRF(http.HandlerFunc(s.backupPrepareDownload)))))
}

func backupParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	n := r.PathValue("backup")
	if !hestia.ValidBackupName(n) {
		invalidFormat(w, "backup", n)
		return "", false
	}
	return n, true
}

func snapshotParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	n := r.PathValue("snapshot")
	if !hestia.ValidSnapshotID(n) {
		invalidFormat(w, "snapshot", n)
		return "", false
	}
	return n, true
}

// commandFailed reports whether err is Hestia refusing a command (as opposed to a
// timeout or a runner failure).
func commandFailed(err error) bool {
	var ce *runner.CommandError
	return errors.As(err, &ce)
}

// queueError answers a failed scheduling command as the PHP flash did: exit 4 is the
// "already running" text, otherwise Hestia's lines, or the no-output fallback.
func queueError(w http.ResponseWriter, err error, running string) {
	var ce *runner.CommandError
	if !errors.As(err, &ce) {
		writeCommandError(w, err)
		return
	}
	status, code := queueStatus(ce)
	WriteError(w, status, code, queueMessage(ce, running))
}

func queueStatus(ce *runner.CommandError) (int, string) {
	if m, ok := exitErrors[ce.Exit]; ok {
		return m.status, m.code
	}
	return http.StatusInternalServerError, "command_failed"
}

func queueMessage(ce *runner.CommandError, running string) string {
	if running != "" && ce.Exit == runner.ExitExists {
		return running
	}
	out := strings.TrimSpace(strings.ReplaceAll(ce.Output, "\r", ""))
	if out == "" {
		return brand.Rewrite(bkNoOutputMsg)
	}
	return brand.Rewrite(out)
}

// backupDir is the directory the core writes backups and dumps to (coreBackupDir;
// hestia.conf's BACKUP is not used, D3).
func (s *server) backupDir() string {
	if s.BackupDir != "" {
		return filepath.Clean(s.BackupDir)
	}
	return coreBackupDir
}

// lookupBackup finds name in the acting user's list; ok is false after it has answered
// (404 when the user has no such backup).
func (s *server) lookupBackup(w http.ResponseWriter, r *http.Request, name string) (hestia.Backup, bool) {
	res, err := s.R.Run(r.Context(), "v-list-user-backup", s.actingUser(r), name, "json")
	if err == nil {
		err = res.Err("v-list-user-backup")
	}
	if err != nil {
		var ce *runner.CommandError
		if errors.As(err, &ce) && (ce.Exit == runner.ExitNotExist || ce.Exit == runner.ExitInvalid) {
			WriteError(w, http.StatusNotFound, "not_found", "")
			return hestia.Backup{}, false
		}
		writeCommandError(w, err)
		return hestia.Backup{}, false
	}
	list, err := hestia.DecodeObjects[hestia.Backup](res.Stdout)
	if err != nil || len(list) != 1 || list[0].NAME != name {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return hestia.Backup{}, false
	}
	return list[0], true
}

func (s *server) backupList(w http.ResponseWriter, r *http.Request) {
	user := s.actingUser(r)
	u, ok, err := s.H.User(r.Context(), user)
	if err != nil {
		serverError(w, err)
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	list := []hestia.Backup{}
	if res, err := s.R.Run(r.Context(), "v-list-user-backups", user, "json"); err == nil && res.Err("v-list-user-backups") == nil {
		if l, err := hestia.DecodeObjects[hestia.Backup](res.Stdout); err == nil {
			list = l
		}
	} else if err != nil && !commandFailed(err) {
		writeCommandError(w, err)
		return
	}
	slices.Reverse(list) // Hestia appends, so the file is oldest first
	writeJSON(w, http.StatusOK, Backups{
		Backups:     list,
		Usage:       BackupUsage{Backups: u["U_BACKUPS"], Limit: u["BACKUPS"]},
		Incremental: u["BACKUPS_INCREMENTAL"] == "yes",
	})
}

func (s *server) backupView(w http.ResponseWriter, r *http.Request) {
	name, ok := backupParam(w, r)
	if !ok {
		return
	}
	if b, ok := s.lookupBackup(w, r, name); ok {
		writeJSON(w, http.StatusOK, BackupView{Backup: b})
	}
}

func (s *server) backupSchedule(w http.ResponseWriter, r *http.Request) {
	if err := s.run(r.Context(), "v-schedule-user-backup", s.actingUser(r)); err != nil {
		queueError(w, err, bkBackupRunningMsg)
		return
	}
	writeJSON(w, http.StatusOK, QueuedReply{Status: "queued", Message: bkQueuedBackupMsg})
}

func (s *server) backupDelete(w http.ResponseWriter, r *http.Request) {
	name, ok := backupParam(w, r)
	if !ok {
		return
	}
	if err := s.run(r.Context(), "v-delete-user-backup", s.actingUser(r), name); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

func (s *server) backupBulk(w http.ResponseWriter, r *http.Request) {
	s.bulk(w, r, map[string]string{"delete": "v-delete-user-backup"}, hestia.ValidBackupName,
		func(user, n string) []string { return []string{user, n} }, nil)
}

// backupPrepareDownload is step one of Parity 28: a local file is ready to fetch, a
// remote one is queued for download.
func (s *server) backupPrepareDownload(w http.ResponseWriter, r *http.Request) {
	name, ok := backupParam(w, r)
	if !ok {
		return
	}
	dir := s.backupDir()
	if _, ok := s.lookupBackup(w, r, name); !ok {
		return
	}
	if isRegularFile(filepath.Join(dir, name)) {
		writeJSON(w, http.StatusOK, DownloadReply{Status: "ready"})
		return
	}
	if err := s.run(r.Context(), "v-schedule-user-backup-download", s.actingUser(r), name); err != nil {
		queueError(w, err, "")
		return
	}
	writeJSON(w, http.StatusOK, DownloadReply{Status: "scheduled", Message: bkDownloadMsg})
}

func isRegularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// backupFile is step two: the file is listed for the acting user and present in the
// backup directory. hestia-nginx serves it from its internal location.
func (s *server) backupFile(w http.ResponseWriter, r *http.Request) {
	if !sameOriginGET(w, r) {
		return
	}
	name, ok := backupParam(w, r)
	if !ok {
		return
	}
	dir := s.backupDir()
	if _, ok := s.lookupBackup(w, r, name); !ok {
		return
	}
	file := filepath.Join(dir, name)
	if !isRegularFile(file) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if !s.accelRedirect(w, r, file) {
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/x-tar")
	h.Set("Content-Disposition", "attachment; filename="+strconv.Quote(name))
	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------- restore

var udirRe = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_.-]*$`)

func validUserDir(s string) bool {
	return len(s) <= 255 && !strings.Contains(s, "..") && udirRe.MatchString(s)
}

// validDomainItem is a domain that can sit in a comma list.
func validDomainItem(s string) bool {
	return hestia.ValidDomainArg(s) && !strings.Contains(s, ",")
}

// validItems reports whether every item passes valid and none is empty.
// bkExclusionLabels name ExclusionsRequest's lists in bkBadExclusionsMsg.
var bkExclusionLabels = [4]string{"Web Domains", "Mail Domains", "Databases", "User Directories"}

func validItems(items []string, valid func(string) bool) bool {
	for _, it := range items {
		if !valid(it) {
			return false
		}
	}
	return true
}

// restoreSets validates a RestoreRequest; ok false means it answered 400.
func restoreSets(w http.ResponseWriter, req RestoreRequest) bool {
	if req.All {
		return true
	}
	sets := []struct {
		name  string
		items []string
		valid func(string) bool
	}{
		{"domain", req.Web, validDomainItem}, {"domain", req.DNS, validDomainItem}, {"domain", req.Mail, validDomainItem},
		{"database", req.DB, hestia.ValidDatabaseArg}, {"path", req.UDir, validUserDir},
	}
	for _, set := range sets {
		for _, it := range set.items {
			if !set.valid(it) {
				invalidFormat(w, set.name, it)
				return false
			}
		}
	}
	if len(req.Web)+len(req.DNS)+len(req.Mail)+len(req.DB)+len(req.UDir) == 0 && !req.Cron {
		WriteError(w, http.StatusBadRequest, "invalid", bkNoItemsMsg)
		return false
	}
	return true
}

func noOr(items []string) string {
	if len(items) == 0 {
		return "no"
	}
	return strings.Join(items, ",")
}

func (s *server) backupRestore(w http.ResponseWriter, r *http.Request) {
	name, ok := backupParam(w, r)
	if !ok {
		return
	}
	var req RestoreRequest
	if !decodeBody(w, r, &req) || !restoreSets(w, req) {
		return
	}
	args := []string{s.actingUser(r), name}
	if !req.All {
		cron := "no"
		if req.Cron {
			cron = "yes"
		}
		// Hestia's order: dns before mail, db after it.
		args = append(args, noOr(req.Web), noOr(req.DNS), noOr(req.Mail), noOr(req.DB), cron, noOr(req.UDir))
	}
	if err := s.run(r.Context(), "v-schedule-user-restore", args...); err != nil {
		queueError(w, err, bkRestoreRunningMsg)
		return
	}
	writeJSON(w, http.StatusOK, QueuedReply{Status: "queued", Message: bkQueuedRestoreMsg})
}

// ---------------------------------------------------------------- exclusions

// exclusionText renders entries one per line, sub-items after ":" (stored commas
// become ":"), as the edit form shows and the save parses (Parity 29).
func exclusionText(entries []hestia.ExclusionEntry) string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, exclusionEntry(e))
	}
	return strings.Join(lines, "\n")
}

func exclusionEntry(e hestia.ExclusionEntry) string {
	if e.Value == "" {
		return e.Key
	}
	return e.Key + ":" + strings.ReplaceAll(e.Value, ",", ":")
}

func (s *server) exclusions(w http.ResponseWriter, r *http.Request) (hestia.BackupExclusions, bool) {
	out, ok := s.output(w, r, "v-list-user-backup-exclusions", s.actingUser(r), "json")
	if !ok {
		return hestia.BackupExclusions{}, false
	}
	ex, err := hestia.DecodeBackupExclusions(out)
	if err != nil {
		serverError(w, err)
		return ex, false
	}
	return ex, true
}

func (s *server) backupExclusions(w http.ResponseWriter, r *http.Request) {
	ex, ok := s.exclusions(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, ExclusionsView{Exclusions: ex, Text: ExclusionsText{
		Web: exclusionText(ex.WEB), Mail: exclusionText(ex.MAIL), DB: exclusionText(ex.DB), UserDir: exclusionText(ex.USER),
	}})
}

// exclusionList turns textarea text into Hestia's comma list: one entry per non-blank
// line. ok is false for a character the single-quoted file line can't carry.
func exclusionList(text string) (string, bool) {
	var entries []string
	for _, line := range strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text), "\n") {
		// A comma separates entries, as PHP's line-break-to-comma rewrite made it.
		for _, e := range strings.Split(line, ",") {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			for _, c := range e {
				if c == '\'' || c < ' ' || c == 0x7f {
					return "", false
				}
			}
			entries = append(entries, e)
		}
	}
	return strings.Join(entries, ","), true
}

func exclusionStored(entries []hestia.ExclusionEntry) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, exclusionEntry(e))
	}
	return strings.Join(parts, ",")
}

func (s *server) backupExclusionsSave(w http.ResponseWriter, r *http.Request) {
	var req ExclusionsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	var lists [4]string
	for i, text := range []string{req.Web, req.Mail, req.DB, req.UserDir} {
		l, ok := exclusionList(text)
		if !ok {
			invalid(w, bkBadExclusionsMsg, bkExclusionLabels[i])
			return
		}
		lists[i] = l
	}
	stored, ok := s.exclusions(w, r)
	if !ok {
		return
	}
	body := "WEB='" + lists[0] + "'\n" +
		"DNS='" + exclusionStored(stored.DNS) + "'\n" +
		"MAIL='" + lists[1] + "'\n" +
		"DB='" + lists[2] + "'\n" +
		"CRON='" + exclusionStored(stored.CRON) + "'\n" +
		"USER='" + lists[3] + "'\n"
	file, cleanup, err := secret.TempFile(s.tmpDir(), strings.TrimSuffix(body, "\n")) // 0600, ends with one newline
	if err != nil {
		serverError(w, err)
		return
	}
	defer cleanup()
	if err := s.run(r.Context(), "v-update-user-backup-exclusions", s.actingUser(r), file); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// ---------------------------------------------------------------- restic snapshots

func (s *server) snapshotList(w http.ResponseWriter, r *http.Request) {
	list := []hestia.Snapshot{}
	res, err := s.R.Run(r.Context(), "v-list-user-backups-restic", s.actingUser(r), "json")
	switch {
	case err != nil:
		writeCommandError(w, err)
		return
	case res.Err("v-list-user-backups-restic") == nil:
		if l, derr := hestia.DecodeSnapshots(res.Stdout); derr == nil {
			list = l
		}
	case strings.HasPrefix(strings.TrimSpace(string(res.Stdout)), "Error:"):
		// The script's own refusal (a suspended user, say) is shown (owner ruling 14).
		writeCommandError(w, res.Err("v-list-user-backups-restic"))
		return
	}
	// restic failing on a repository that is missing or can't be opened prints its
	// message to stderr: an empty list, not a crash (Parity 31).
	slices.Reverse(list) // restic lists oldest first
	writeJSON(w, http.StatusOK, Snapshots{Snapshots: list})
}

func (s *server) snapshotView(w http.ResponseWriter, r *http.Request) {
	id, ok := snapshotParam(w, r)
	if !ok {
		return
	}
	res, err := s.R.Run(r.Context(), "v-list-user-backup-restic", s.actingUser(r), id, "json")
	if err == nil {
		err = res.Err("v-list-user-backup-restic")
	}
	if err != nil && !commandFailed(err) {
		writeCommandError(w, err)
		return
	}
	var d hestia.SnapshotDetail
	if err == nil {
		d, err = hestia.DecodeSnapshotDetail(res.Stdout)
	}
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	writeJSON(w, http.StatusOK, SnapshotView{ID: id, Detail: d})
}

func (s *server) snapshotFiles(w http.ResponseWriter, r *http.Request) {
	id, ok := snapshotParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	home := "/home/" + user
	folder := home
	if p := r.URL.Query().Get("path"); p != "" {
		if !path.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n\t *?[") || strings.IndexFunc(p, unicode.IsSpace) >= 0 {
			invalidFormat(w, "path", p)
			return
		}
		folder = path.Clean(p)
	}
	conf := home + "/conf"
	switch {
	case folder != home && !strings.HasPrefix(folder, home+"/"):
		invalidFormat(w, "path", folder)
		return
	case folder == conf || strings.HasPrefix(folder, conf+"/"):
		WriteError(w, http.StatusForbidden, "forbidden", "")
		return
	}
	out, ok := s.output(w, r, "v-list-user-files-restic", user, id, folder)
	if !ok {
		return
	}
	all, err := hestia.DecodeSnapshotNodes(out)
	if err != nil {
		serverError(w, err)
		return
	}
	nodes := []hestia.SnapshotNode{}
	for _, n := range all {
		if n.Path != folder && path.Dir(n.Path) == folder { // children only
			nodes = append(nodes, n)
		}
	}
	parent := ""
	if folder != home {
		parent = path.Dir(folder)
	}
	writeJSON(w, http.StatusOK, SnapshotFiles{Path: folder, Parent: parent, Nodes: nodes})
}

func (s *server) snapshotSchedule(w http.ResponseWriter, r *http.Request) {
	if err := s.run(r.Context(), "v-schedule-user-backup-restic", s.actingUser(r)); err != nil {
		queueError(w, err, "") // PHP showed the command's own text here
		return
	}
	writeJSON(w, http.StatusOK, QueuedReply{Status: "queued", Message: bkSnapshotMsg})
}

func (s *server) snapshotBulk(w http.ResponseWriter, r *http.Request) {
	s.bulk(w, r, map[string]string{"delete": "v-delete-user-backup-restic"}, hestia.ValidSnapshotID,
		func(user, n string) []string { return []string{user, n} }, nil)
}

// snapshotRestore queues "all" in one call, or one call per non-empty type in the
// order web, dns, db, mail, cron (Hestia's bulk order). Every call is attempted and
// the failures are reported together.
func (s *server) snapshotRestore(w http.ResponseWriter, r *http.Request) {
	id, ok := snapshotParam(w, r)
	if !ok {
		return
	}
	var req RestoreRequest
	if !decodeBody(w, r, &req) {
		return
	}
	user := s.actingUser(r)
	if req.All {
		if err := s.run(r.Context(), "v-schedule-user-restore-restic", user, id); err != nil {
			queueError(w, err, bkRestoreRunningMsg)
			return
		}
		writeJSON(w, http.StatusOK, QueuedReply{Status: "queued", Message: bkQueuedRestoreMsg})
		return
	}
	if len(req.UDir) > 0 { // the scheduler has no per-directory restore
		WriteError(w, http.StatusBadRequest, "invalid", bkNoDirRestoreMsg)
		return
	}
	req.UDir = nil
	if !restoreSets(w, req) {
		return
	}
	type call struct {
		typ   string
		items []string
	}
	calls := []call{{"web", req.Web}, {"dns", req.DNS}, {"db", req.DB}, {"mail", req.Mail}}
	var msgs []string
	queued := 0
	var first *runner.CommandError
	run := func(args ...string) bool {
		err := s.run(r.Context(), "v-schedule-user-restore-restic", args...)
		if err == nil {
			queued++
			return true
		}
		var ce *runner.CommandError
		if !errors.As(err, &ce) {
			writeCommandError(w, err)
			return false
		}
		if first == nil {
			first = ce
		}
		msgs = append(msgs, queueMessage(ce, bkRestoreRunningMsg))
		return true
	}
	for _, c := range calls {
		if len(c.items) > 0 && !run(user, id, c.typ, strings.Join(c.items, ",")) {
			return
		}
	}
	if req.Cron && !run(user, id, "cron") {
		return
	}
	if first != nil {
		status, code := queueStatus(first)
		text := strings.Join(msgs, "\n")
		if queued > 0 { // some types are already in the queue
			text = "Some items could not be queued:\n" + text
		}
		WriteError(w, status, code, text)
		return
	}
	writeJSON(w, http.StatusOK, QueuedReply{Status: "queued", Message: bkQueuedRestoreMsg})
}

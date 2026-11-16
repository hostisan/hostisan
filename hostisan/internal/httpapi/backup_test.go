package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/runner"
)

const (
	bkAdminOld = "admin.2026-10-19_03-00-02.tar"
	bkAdminNew = "admin.2026-10-20_03-00-01.tar"
	bkBobFile  = "bob.2026-10-20_03-10-00.tar"

	bkQueuedBackup  = "Task has been added to the queue. You will receive an email notification when your backup is ready for download."
	bkBackupRunning = "An existing backup task is already running, please wait for it to complete."
	bkQueuedRestore = "Task has been added to the queue. You will receive an email notification when your restore has been completed."
	bkRestoreBusy   = "An existing restoration task is already running. Please wait for it to finish before launching it again."
)

// bkCalls is every backup, snapshot and restore command so far, as "cmd arg arg".
func (h *harness) bkCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if strings.Contains(c.Cmd, "backup") || strings.Contains(c.Cmd, "restore") || strings.Contains(c.Cmd, "restic") {
			out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	return out
}

// bkWrites is bkCalls without the list commands.
func (h *harness) bkWrites() []string {
	var out []string
	for _, c := range h.bkCalls() {
		if !strings.HasPrefix(c, "v-list-") {
			out = append(out, c)
		}
	}
	return out
}

func acctWantCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("calls =\n%q\nwant\n%q", got, want)
	}
}

func bkErr(h *harness, method, path string, body any) (int, APIError) {
	h.t.Helper()
	var e APIError
	code := h.call(method, path, body, &e)
	return code, e
}

func TestBackupList(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var got Backups
	if code := h.call("GET", "/api/v1/backup", nil, &got); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(got.Backups) != 2 || got.Backups[0].NAME != bkAdminNew || got.Backups[1].NAME != bkAdminOld {
		t.Fatalf("backups (newest first) = %+v", got.Backups)
	}
	if got.Backups[0].SIZE != "1243" || got.Backups[0].TYPE != "local,sftp" {
		t.Errorf("first = %+v", got.Backups[0])
	}
	if !got.Incremental || got.Usage != (BackupUsage{Backups: "2", Limit: "10"}) {
		t.Errorf("usage/incremental = %+v %v", got.Usage, got.Incremental)
	}
	acctWantCalls(t, h.bkCalls(), "v-list-user-backups admin json")
}

func TestBackupListFailedGivesEmptyArray(t *testing.T) {
	for name, entry := range map[string]fixtureEntry{
		"failed": {Args: []string{"admin", "json"}, Stdout: "Error: boom\n", Exit: 3},
		"empty":  {Args: []string{"admin", "json"}, Stdout: "{\n}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{"v-list-user-backups": {entry}}})
			h.loginAdmin()
			var raw map[string]json.RawMessage
			if code := h.call("GET", "/api/v1/backup", nil, &raw); code != 200 {
				t.Fatalf("code %d", code)
			}
			if string(raw["backups"]) != "[]" {
				t.Fatalf("backups = %s", raw["backups"])
			}
		})
	}
}

func TestBackupDetail(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var v BackupView
	if code := h.call("GET", "/api/v1/backup/"+bkAdminNew, nil, &v); code != 200 || v.Backup.NAME != bkAdminNew || v.Backup.WEB != "example.com,shop.example.com" {
		t.Fatalf("detail %d %+v", code, v)
	}
	acctWantCalls(t, h.bkCalls(), "v-list-user-backup admin "+bkAdminNew+" json")
	if code, _ := bkErr(h, "GET", "/api/v1/backup/admin.nothere.tar", nil); code != 404 {
		t.Errorf("unknown = %d, want 404", code)
	}
	if code, _ := bkErr(h, "GET", "/api/v1/backup/x", nil); code != 400 {
		t.Errorf("one-character name = %d, want 400", code)
	}
}

func TestBackupQueueMessages(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var q QueuedReply
	if code := h.call("POST", "/api/v1/backup", nil, &q); code != 200 || q.Status != "queued" || q.Message != bkQueuedBackup {
		t.Fatalf("exit 0: %d %+v", code, q)
	}
	acctWantCalls(t, h.bkWrites(), "v-schedule-user-backup admin")

	hb := newHarness(t)
	hb.loginBob()
	if code, e := bkErr(hb, "POST", "/api/v1/backup", nil); code != 409 || e.Error.Code != "exists" || e.Error.Message != bkBackupRunning {
		t.Fatalf("exit 4: %d %+v", code, e)
	}

	he := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-schedule-user-backup": {{Args: []string{"admin"}, Stdout: "", Exit: 1}},
	}})
	he.loginAdmin()
	if code, e := bkErr(he, "POST", "/api/v1/backup", nil); code != 400 || e.Error.Message != "Error: Hostisan did not return any output." {
		t.Fatalf("empty output: %d %+v", code, e)
	}
}

func TestBackupDeleteAndBulk(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var ok OK
	if code := h.call("DELETE", "/api/v1/backup/"+bkAdminOld, nil, &ok); code != 200 {
		t.Fatalf("delete %d", code)
	}
	var br BulkReply
	if code := h.call("POST", "/api/v1/backup/bulk", BulkRequest{Action: "delete", Names: []string{bkAdminOld, bkAdminNew}}, &br); code != 200 || len(br.Failed) != 0 {
		t.Fatalf("bulk %d %+v", code, br)
	}
	if code, _ := bkErr(h, "POST", "/api/v1/backup/bulk", BulkRequest{Action: "delete", Names: []string{"../x"}}); code != 400 {
		t.Errorf("bad bulk name = %d", code)
	}
	acctWantCalls(t, h.bkWrites(),
		"v-delete-user-backup admin "+bkAdminOld,
		"v-delete-user-backup admin "+bkAdminOld,
		"v-delete-user-backup admin "+bkAdminNew)
}

// ---------------------------------------------------------------- download

func bkLocalDir(t *testing.T, names ...string) overrides {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("tar"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// hestia.conf's BACKUP is ignored: the core always writes to its own dir (D3).
	return overrides{BackupDir: dir, Config: map[string]string{"BACKUP": "/srv/bk"}}
}

func bkGet(h *harness, path string) (*http.Response, string) {
	h.t.Helper()
	res, err := h.client.Get(h.srv.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestBackupDownloadLocal(t *testing.T) {
	o := bkLocalDir(t, bkAdminNew)
	h := newHarnessWith(t, o)
	h.loginAdmin()
	var d DownloadReply
	if code := h.call("POST", "/api/v1/backup/"+bkAdminNew+"/download", nil, &d); code != 200 || d.Status != "ready" {
		t.Fatalf("prepare %d %+v", code, d)
	}
	acctWantCalls(t, h.bkCalls(), "v-list-user-backup admin "+bkAdminNew+" json")
	res, body := bkGet(h, "/api/v1/backup/file/"+bkAdminNew)
	want := accelPrefix + filepath.Join(o.BackupDir, bkAdminNew)
	if res.StatusCode != 200 || res.Header.Get("X-Accel-Redirect") != want || body != "" {
		t.Fatalf("file: %d %q %q", res.StatusCode, res.Header.Get("X-Accel-Redirect"), body)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/x-tar" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, bkAdminNew) {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestBackupDownloadRemoteSchedulesOnce(t *testing.T) {
	h := newHarnessWith(t, bkLocalDir(t)) // listed, but not on disk
	h.loginAdmin()
	var d DownloadReply
	if code := h.call("POST", "/api/v1/backup/"+bkAdminNew+"/download", nil, &d); code != 200 || d.Status != "scheduled" ||
		d.Message != "Download of remote backup file has been scheduled." {
		t.Fatalf("remote %d %+v", code, d)
	}
	acctWantCalls(t, h.bkCalls(),
		"v-list-user-backup admin "+bkAdminNew+" json",
		"v-schedule-user-backup-download admin "+bkAdminNew)
	if res, _ := bkGet(h, "/api/v1/backup/file/"+bkAdminNew); res.StatusCode != 404 || res.Header.Get("X-Accel-Redirect") != "" {
		t.Errorf("GET without the file: %d %v", res.StatusCode, res.Header)
	}
}

func TestBackupDownloadRefusals(t *testing.T) {
	h := newHarnessWith(t, bkLocalDir(t, bkBobFile, bkAdminNew))
	h.loginAdmin()
	for _, name := range []string{"..%2Fetc%2Fpasswd", "admin.x.tar%2F..%2F..%2Fy", bkBobFile, "admin.nothere.tar"} {
		for _, path := range []string{"/api/v1/backup/file/" + name} {
			res, _ := bkGet(h, path)
			if res.StatusCode != 400 && res.StatusCode != 404 || res.Header.Get("X-Accel-Redirect") != "" {
				t.Errorf("%s: %d %v", path, res.StatusCode, res.Header)
			}
		}
		if code, _ := bkErr(h, "POST", "/api/v1/backup/"+name+"/download", nil); code != 400 && code != 404 {
			t.Errorf("POST download %s = %d", name, code)
		}
	}
	for _, c := range h.bkWrites() {
		t.Errorf("unexpected write %s", c)
	}

	// Another user's file, through bob.
	hb := newHarnessWith(t, bkLocalDir(t, bkAdminNew))
	hb.loginBob()
	if res, _ := bkGet(hb, "/api/v1/backup/file/"+bkAdminNew); res.StatusCode != 404 || res.Header.Get("X-Accel-Redirect") != "" {
		t.Errorf("bob fetching admin's file: %d", res.StatusCode)
	}
}

func TestBackupDownloadNeedsCSRFNotWritable(t *testing.T) {
	o := bkLocalDir(t, bkAdminNew)
	o.Config["POLICY_SYSTEM_PROTECTED_ADMIN"] = "yes"
	o.Config["ROOT_USER"] = "admin"
	h := newHarnessWith(t, o)
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	var d DownloadReply
	if code := h.call("POST", "/api/v1/backup/"+bkAdminNew+"/download", nil, &d); code != 200 || d.Status != "ready" {
		t.Fatalf("read-only download %d %+v", code, d)
	}
	if res, _ := bkGet(h, "/api/v1/backup/file/"+bkAdminNew); res.StatusCode != 200 {
		t.Errorf("read-only file %d", res.StatusCode)
	}
	h.token = "wrong"
	if code, e := bkErr(h, "POST", "/api/v1/backup/"+bkAdminNew+"/download", nil); code != 400 || e.Error.Code != "csrf" {
		t.Errorf("no CSRF = %d %+v", code, e)
	}
}

func TestBackupRoutesRegister(t *testing.T) {
	h := newHarnessWith(t, bkLocalDir(t, bkAdminNew)) // NewServer panics on a pattern conflict
	h.loginAdmin()
	if res, _ := bkGet(h, "/api/v1/backup/file/"+bkAdminNew); res.StatusCode != 200 || res.Header.Get("X-Accel-Redirect") == "" {
		t.Errorf("/backup/file/x reaches the download handler: %d %v", res.StatusCode, res.Header)
	}
	var sv SnapshotView
	if code := h.call("GET", "/api/v1/backup/incremental/3c9b8a7e", nil, &sv); code != 200 || sv.ID != "3c9b8a7e" {
		t.Errorf("/backup/incremental/x reaches the snapshot handler: %d %+v", code, sv)
	}
	if code, e := bkErr(h, "GET", "/api/v1/backup/"+bkAdminNew+"/file", nil); code != 404 || e.Error.Code != "not_found" {
		t.Errorf("/backup/x/file = %d %+v, want the 404 catch-all", code, e)
	}
}

// ---------------------------------------------------------------- restore

func TestBackupRestore(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  RestoreRequest
		want string
	}{
		{"all", RestoreRequest{All: true, Web: []string{"ignored.com"}, Cron: true}, "v-schedule-user-restore admin " + bkAdminNew},
		{"selection", RestoreRequest{Web: []string{"example.com", "shop.example.com"}, DNS: []string{"example.com"},
			Mail: []string{"example.com"}, DB: []string{"admin_wp", "admin_shop"}, UDir: []string{"backup", ".ssh"}, Cron: true},
			"v-schedule-user-restore admin " + bkAdminNew + " example.com,shop.example.com example.com example.com admin_wp,admin_shop yes backup,.ssh"},
		{"one item", RestoreRequest{DB: []string{"admin_wp"}},
			"v-schedule-user-restore admin " + bkAdminNew + " no no no admin_wp no no"},
		{"cron only", RestoreRequest{Cron: true},
			"v-schedule-user-restore admin " + bkAdminNew + " no no no no yes no"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.loginAdmin()
			var q QueuedReply
			if code := h.call("POST", "/api/v1/backup/"+bkAdminNew+"/restore", tc.req, &q); code != 200 || q.Message != bkQueuedRestore {
				t.Fatalf("restore %d %+v", code, q)
			}
			acctWantCalls(t, h.bkWrites(), tc.want)
		})
	}
}

func TestBackupRestoreRefusals(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	path := "/api/v1/backup/" + bkAdminNew + "/restore"
	if code, e := bkErr(h, "POST", path, RestoreRequest{}); code != 400 || e.Error.Message != "No items were selected to restore." {
		t.Fatalf("empty selection %d %+v", code, e)
	}
	for _, bad := range []RestoreRequest{{Web: []string{"not a domain"}}, {DB: []string{"a;b"}}, {UDir: []string{"../etc"}}, {Web: []string{""}}, {Mail: []string{"a,b.com"}}} {
		if code, _ := bkErr(h, "POST", path, bad); code != 400 {
			t.Errorf("%+v = %d, want 400", bad, code)
		}
	}
	if code, _ := bkErr(h, "POST", "/api/v1/backup/..%2Fx/restore", RestoreRequest{All: true}); code != 400 {
		t.Errorf("bad name = %d", code)
	}
	acctWantCalls(t, h.bkWrites())
}

func TestBackupRestoreAlreadyRunning(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-schedule-user-restore": {{Args: []string{"admin", "*"}, Stdout: "Error: restore is already scheduled\n", Exit: 4}},
	}})
	h.loginAdmin()
	if code, e := bkErr(h, "POST", "/api/v1/backup/"+bkAdminNew+"/restore", RestoreRequest{All: true}); code != 409 || e.Error.Message != bkRestoreBusy {
		t.Fatalf("%d %+v", code, e)
	}
}

// ---------------------------------------------------------------- exclusions

type exclSnap struct {
	Args []string
	Body string
	Mode os.FileMode
}

type exclSpy struct {
	runner.Runner
	snaps []exclSnap
}

func (s *exclSpy) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if cmd == "v-update-user-backup-exclusions" {
		sn := exclSnap{Args: append([]string(nil), args...)}
		if len(args) == 2 {
			b, _ := os.ReadFile(args[1])
			sn.Body = string(b)
			if st, err := os.Stat(args[1]); err == nil {
				sn.Mode = st.Mode().Perm()
			}
		}
		s.snaps = append(s.snaps, sn)
	}
	return s.Runner.Run(ctx, cmd, args...)
}

func exclHarness(t *testing.T, o overrides) (*harness, *exclSpy) {
	t.Helper()
	spy := &exclSpy{}
	h := newHarnessWith(t, o, func(d *Deps) { spy.Runner = d.R; d.R = spy })
	h.loginAdmin()
	return h, spy
}

func TestBackupExclusionsView(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var v ExclusionsView
	if code := h.call("GET", "/api/v1/backup/exclusions", nil, &v); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(v.Exclusions.WEB) != 2 || v.Exclusions.DNS[0].Key != "example.org" {
		t.Errorf("exclusions = %+v", v.Exclusions)
	}
	want := ExclusionsText{
		Web:     "example.com:public_html/cache:public_html/tmp\nshop.example.com",
		Mail:    "example.com:info:support",
		DB:      "admin_wp",
		UserDir: "Downloads\n.cache",
	}
	if v.Text != want {
		t.Errorf("text = %+v, want %+v", v.Text, want)
	}
	acctWantCalls(t, h.bkCalls(), "v-list-user-backup-exclusions admin json")
}

func TestBackupExclusionsSaveKeepsDNSAndCron(t *testing.T) {
	h, spy := exclHarness(t, overrides{})
	req := ExclusionsRequest{
		Web:     "example.com:public_html/cache\r\n\r\nshop.example.com\n",
		Mail:    "example.com:info:support",
		DB:      "admin_wp",
		UserDir: "Downloads\n.cache",
	}
	var ok OK
	if code := h.call("PUT", "/api/v1/backup/exclusions", req, &ok); code != 200 {
		t.Fatalf("save %d", code)
	}
	if len(spy.snaps) != 1 {
		t.Fatalf("snaps = %+v", spy.snaps)
	}
	sn := spy.snaps[0]
	if sn.Args[0] != "admin" || sn.Mode != 0o600 {
		t.Errorf("args/mode = %v %v", sn.Args, sn.Mode)
	}
	want := "WEB='example.com:public_html/cache,shop.example.com'\n" +
		"DNS='example.org'\n" +
		"MAIL='example.com:info:support'\n" +
		"DB='admin_wp'\n" +
		"CRON='*'\n" +
		"USER='Downloads,.cache'\n"
	if sn.Body != want {
		t.Errorf("temp file =\n%s\nwant\n%s", sn.Body, want)
	}
	if _, err := os.Stat(sn.Args[1]); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: %v", err)
	}
	acctWantCalls(t, h.bkCalls(), "v-list-user-backup-exclusions admin json", "v-update-user-backup-exclusions admin "+sn.Args[1])
}

func TestBackupExclusionsSaveRefusals(t *testing.T) {
	h, spy := exclHarness(t, overrides{})
	for _, bad := range []ExclusionsRequest{{Web: "a'b.com"}, {Mail: "x\x00y"}, {DB: "a\x1bb"}} {
		if code, _ := bkErr(h, "PUT", "/api/v1/backup/exclusions", bad); code != 400 {
			t.Errorf("%+v = %d, want 400", bad, code)
		}
	}
	if len(spy.snaps) != 0 {
		t.Errorf("ran the update: %+v", spy.snaps)
	}
	// Hestia's own refusal is relayed and the temp file is still removed.
	h2, spy2 := exclHarness(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-update-user-backup-exclusions": {{Args: []string{"admin", "*"}, Stdout: "Error: invalid exclusion entry :: ..\n", Exit: 2}},
	}})
	if code, e := bkErr(h2, "PUT", "/api/v1/backup/exclusions", ExclusionsRequest{Web: "example.com:.."}); code != 400 || !strings.Contains(e.Error.Message, "invalid exclusion entry") {
		t.Fatalf("relay %d %+v", code, e)
	}
	if _, err := os.Stat(spy2.snaps[0].Args[1]); !os.IsNotExist(err) {
		t.Errorf("temp file left after a failure: %v", err)
	}
}

// ---------------------------------------------------------------- restic

const snapID = "3c9b8a7e"

func TestSnapshotList(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var s Snapshots
	if code := h.call("GET", "/api/v1/backup/incremental", nil, &s); code != 200 || len(s.Snapshots) != 2 ||
		s.Snapshots[0].ShortID != "e41f0a29" || s.Snapshots[1].ShortID != snapID {
		t.Fatalf("snapshots (newest first) %d %+v", code, s)
	}
	acctWantCalls(t, h.bkCalls(), "v-list-user-backups-restic admin json")
}

func TestSnapshotListNoRepository(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user-backups-restic": {{Args: []string{"admin", "json"}, Stdout: "", Exit: 0}},
	}})
	h.loginAdmin()
	var raw map[string]json.RawMessage
	if code := h.call("GET", "/api/v1/backup/incremental", nil, &raw); code != 200 || string(raw["snapshots"]) != "[]" {
		t.Fatalf("%d %s", code, raw["snapshots"])
	}
	// restic itself failing (no repository yet: its message goes to stderr) is still empty.
	h2 := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user-backups-restic": {{Args: []string{"admin", "json"}, Stdout: "", Exit: 10}},
	}})
	h2.loginAdmin()
	raw = nil
	if code := h2.call("GET", "/api/v1/backup/incremental", nil, &raw); code != 200 || string(raw["snapshots"]) != "[]" {
		t.Fatalf("failing list %d %s", code, raw["snapshots"])
	}
}

// The script's own refusals (a suspended user, say) are shown, not an empty list
// (owner ruling 14).
func TestSnapshotListShowsScriptErrors(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user-backups-restic": {{Args: []string{"admin", "json"}, Stdout: "Error: user admin is suspended\n", Exit: 5}},
	}})
	h.loginAdmin()
	var e APIError
	if code := h.call("GET", "/api/v1/backup/incremental", nil, &e); code < 400 || e.Error.Message != "Error: user admin is suspended" {
		t.Fatalf("%d %+v", code, e)
	}
}

func TestSnapshotDetail(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var v SnapshotView
	if code := h.call("GET", "/api/v1/backup/incremental/"+snapID, nil, &v); code != 200 || v.ID != snapID || v.Detail.WEB != "example.com,shop.example.com" {
		t.Fatalf("detail %d %+v", code, v)
	}
	acctWantCalls(t, h.bkCalls(), "v-list-user-backup-restic admin "+snapID+" json")
	// Upstream answers an unknown id with exit 0 and an empty "/backup" object (the
	// recording; restic's "no matching ID" goes to stderr): still 404.
	if code, _ := bkErr(h, "GET", "/api/v1/backup/incremental/deadbeef", nil); code != 404 {
		t.Errorf("unknown = %d", code)
	}
	if code, _ := bkErr(h, "GET", "/api/v1/backup/incremental/NOT-HEX", nil); code != 400 {
		t.Errorf("bad id = %d", code)
	}
}

func TestSnapshotFiles(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var f SnapshotFiles
	if code := h.call("GET", "/api/v1/backup/incremental/"+snapID+"/files?path=/home/admin/Documents", nil, &f); code != 200 {
		t.Fatalf("code %d", code)
	}
	if f.Path != "/home/admin/Documents" || f.Parent != "/home/admin" || len(f.Nodes) != 2 || f.Nodes[0].Name != "invoice-2026-09.pdf" {
		t.Fatalf("files = %+v", f)
	}
	var home SnapshotFiles
	if code := h.call("GET", "/api/v1/backup/incremental/"+snapID+"/files?path=/home/admin/", nil, &home); code != 200 ||
		home.Path != "/home/admin" || home.Parent != "" {
		t.Fatalf("home = %d %+v", code, home)
	}
	for _, n := range home.Nodes {
		if n.Path == "/home/admin" {
			t.Errorf("folder's own node kept: %+v", n)
		}
	}
	acctWantCalls(t, h.bkCalls(),
		"v-list-user-files-restic admin "+snapID+" /home/admin/Documents",
		"v-list-user-files-restic admin "+snapID+" /home/admin")
}

func TestSnapshotFilesConfined(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	base := "/api/v1/backup/incremental/" + snapID + "/files?path="
	for _, p := range []string{"/home/admin/../../etc", "/etc", "/home/bob", "/home/adminx", "/home/admin/conf", "/home/admin/conf/web", "/home/admin/conf/../conf"} {
		if code, _ := bkErr(h, "GET", base+p, nil); code != 400 && code != 403 {
			t.Errorf("%s = %d, want a refusal", p, code)
		}
	}
	acctWantCalls(t, h.bkCalls())
	// An empty path is the home folder.
	var f SnapshotFiles
	if code := h.call("GET", "/api/v1/backup/incremental/"+snapID+"/files", nil, &f); code != 200 || f.Path != "/home/admin" {
		t.Errorf("no path = %d %+v", code, f)
	}
}

func TestSnapshotCreate(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var q QueuedReply
	if code := h.call("POST", "/api/v1/backup/incremental", nil, &q); code != 200 || q.Message != "Snapshot has been sheduled" {
		t.Fatalf("%d %+v", code, q)
	}
	acctWantCalls(t, h.bkWrites(), "v-schedule-user-backup-restic admin")

	h2 := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-schedule-user-backup-restic": {{Args: []string{"admin"}, Stdout: "Error: backup is already scheduled\n", Exit: 4}},
	}})
	h2.loginAdmin()
	if code, e := bkErr(h2, "POST", "/api/v1/backup/incremental", nil); code != 409 || e.Error.Message != "Error: backup is already scheduled" {
		t.Fatalf("exit 4: %d %+v", code, e)
	}
}

func TestSnapshotRestore(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	path := "/api/v1/backup/incremental/" + snapID + "/restore"
	var q QueuedReply
	if code := h.call("POST", path, RestoreRequest{All: true, Web: []string{"x.com"}}, &q); code != 200 || q.Message != bkQueuedRestore {
		t.Fatalf("all %d %+v", code, q)
	}
	req := RestoreRequest{Web: []string{"example.com", "shop.example.com"}, DNS: []string{"example.com"}, Mail: []string{"example.com"},
		DB: []string{"admin_wp"}, Cron: true}
	if code := h.call("POST", path, req, &q); code != 200 || q.Message != bkQueuedRestore {
		t.Fatalf("selection %d %+v", code, q)
	}
	acctWantCalls(t, h.bkWrites(),
		"v-schedule-user-restore-restic admin "+snapID,
		"v-schedule-user-restore-restic admin "+snapID+" web example.com,shop.example.com",
		"v-schedule-user-restore-restic admin "+snapID+" dns example.com",
		"v-schedule-user-restore-restic admin "+snapID+" db admin_wp",
		"v-schedule-user-restore-restic admin "+snapID+" mail example.com",
		"v-schedule-user-restore-restic admin "+snapID+" cron")
}

func TestSnapshotRestoreRefusals(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	path := "/api/v1/backup/incremental/" + snapID + "/restore"
	if code, e := bkErr(h, "POST", path, RestoreRequest{}); code != 400 || e.Error.Message != "No items were selected to restore." {
		t.Fatalf("empty %d %+v", code, e)
	}
	if code, _ := bkErr(h, "POST", path, RestoreRequest{UDir: []string{"backup"}}); code != 400 {
		t.Errorf("udir on a snapshot = %d", code)
	}
	acctWantCalls(t, h.bkWrites())
}

func TestSnapshotRestoreReportsFailures(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-schedule-user-restore-restic": {
			{Args: []string{"admin", snapID, "db", "*"}, Stdout: "Error: Unable to download from snapshot\n", Exit: 3},
			{Args: []string{"admin", snapID, "mail", "*"}, Stdout: "", Exit: 1},
			{Args: []string{"*", "*", "*", "*"}, Stdout: ""},
			{Args: []string{"*", "*", "*"}, Stdout: ""},
		},
	}})
	h.loginAdmin()
	code, e := bkErr(h, "POST", "/api/v1/backup/incremental/"+snapID+"/restore",
		RestoreRequest{Web: []string{"example.com"}, DB: []string{"admin_wp"}, Mail: []string{"example.com"}})
	if code != 404 || !strings.HasPrefix(e.Error.Message, "Some items could not be queued:\n") ||
		!strings.Contains(e.Error.Message, "Unable to download from snapshot") ||
		!strings.Contains(e.Error.Message, "Error: Hostisan did not return any output.") {
		t.Fatalf("%d %+v", code, e)
	}
	// Every call was attempted, in order web, db, mail.
	acctWantCalls(t, h.bkWrites(),
		"v-schedule-user-restore-restic admin "+snapID+" web example.com",
		"v-schedule-user-restore-restic admin "+snapID+" db admin_wp",
		"v-schedule-user-restore-restic admin "+snapID+" mail example.com")
}

func TestSnapshotBulk(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var br BulkReply
	if code := h.call("POST", "/api/v1/backup/incremental/bulk", BulkRequest{Action: "delete", Names: []string{snapID, "e41f0a29"}}, &br); code != 200 || len(br.Failed) != 0 {
		t.Fatalf("bulk %d %+v", code, br)
	}
	if code, _ := bkErr(h, "POST", "/api/v1/backup/incremental/bulk", BulkRequest{Action: "delete", Names: []string{"nothex!"}}); code != 400 {
		t.Errorf("bad id = %d", code)
	}
	acctWantCalls(t, h.bkWrites(),
		"v-delete-user-backup-restic admin "+snapID,
		"v-delete-user-backup-restic admin e41f0a29")
}

// ---------------------------------------------------------------- read-only

func TestBackupWritesReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	b := "/api/v1/backup/"
	for _, rt := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/backup", nil},
		{"DELETE", b + bkAdminNew, nil},
		{"POST", b + "bulk", BulkRequest{Action: "delete", Names: []string{bkAdminNew}}},
		{"POST", b + bkAdminNew + "/restore", RestoreRequest{All: true}},
		{"PUT", b + "exclusions", ExclusionsRequest{Web: "a.com"}},
		{"POST", b + "incremental", nil},
		{"POST", b + "incremental/" + snapID + "/restore", RestoreRequest{All: true}},
		{"POST", b + "incremental/bulk", BulkRequest{Action: "delete", Names: []string{snapID}}},
	} {
		if code, e := bkErr(h, rt.method, rt.path, rt.body); code != 403 || e.Error.Code != "read_only" {
			t.Errorf("%s %s: %d %+v", rt.method, rt.path, code, e)
		}
	}
	acctWantCalls(t, h.bkWrites())
}

func TestBackupWritesNeedCSRF(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.token = "wrong"
	if code, e := bkErr(h, "POST", "/api/v1/backup", nil); code != 400 || e.Error.Code != "csrf" {
		t.Fatalf("%d %+v", code, e)
	}
	acctWantCalls(t, h.bkWrites())
}

func TestBackupExclusionsCommaSplits(t *testing.T) {
	h, spy := exclHarness(t, overrides{})
	if code := h.call("PUT", "/api/v1/backup/exclusions", ExclusionsRequest{Web: "a.com,b.com\nc.com", DB: "x, y"}, nil); code != 200 {
		t.Fatalf("code %d", code)
	}
	if b := spy.snaps[0].Body; !strings.Contains(b, "WEB='a.com,b.com,c.com'\n") || !strings.Contains(b, "DB='x,y'\n") || strings.Contains(b, "\n\n") {
		t.Errorf("body = %q", b)
	}
}

func TestSnapshotFilesRefusesGlobAndSpace(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	base := "/api/v1/backup/incremental/" + snapID + "/files?path="
	for _, p := range []string{"/home/admin/a%20b", "/home/admin/*", "/home/admin/a?", "/home/admin/[a]", "/home/admin/a%09b"} {
		if code, _ := bkErr(h, "GET", base+p, nil); code != 400 {
			t.Errorf("%s = %d, want 400", p, code)
		}
	}
	acctWantCalls(t, h.bkCalls())
}

func TestScheduleExit4PassesCommandText(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-schedule-user-backup-restic":   {{Args: []string{"admin"}, Stdout: "Error: backup is already scheduled\n", Exit: 4}},
		"v-schedule-user-backup-download": {{Args: []string{"admin", "*"}, Stdout: "Error: odd\n", Exit: 4}},
	}})
	h.loginAdmin()
	if code, e := bkErr(h, "POST", "/api/v1/backup/incremental", nil); code != 409 || e.Error.Message != "Error: backup is already scheduled" {
		t.Errorf("snapshot: %d %+v", code, e)
	}
	if code, e := bkErr(h, "POST", "/api/v1/backup/"+bkAdminNew+"/download", nil); code != 409 || e.Error.Message != "Error: odd" {
		t.Errorf("download: %d %+v", code, e)
	}
}

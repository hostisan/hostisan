package httpapi

import (
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
)

// keyCmdRe selects the commands this slice runs; config and session reads are left out.
var keyCmdRe = regexp.MustCompile(`^v-(list|add|delete)-(user-ssh-key|access-keys?|apis|user-notifications?|user-log|user-auth-log)$|^v-delete-user-(ssh-key|notification|log|auth-log)$|^v-acknowledge-user-notification$|^v-add-user-ssh-key$|^v-list-(user|users)-stats$|^v-list-sys-users$`)

func (h *harness) keyCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if keyCmdRe.MatchString(c.Cmd) {
			out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	return out
}

func (h *harness) rawGet(path string) string {
	h.t.Helper()
	resp, err := h.client.Get(h.srv.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		h.t.Fatalf("GET %s: %d %s", path, resp.StatusCode, b)
	}
	return string(b)
}

func (h *harness) sshAdd(key string) (int, APIError) {
	h.t.Helper()
	var e APIError
	code := h.call("POST", "/api/v1/account/ssh-keys", AddSSHKeyRequest{Key: key}, &e)
	return code, e
}

const helloKey = "ssh-ed25519 aGVsbG8="

func TestSSHKeyList(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r SSHKeys
	if code := h.call("GET", "/api/v1/account/ssh-keys", nil, &r); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(r.Keys) != 2 || r.Keys[0].ID != "admin@workstation" || r.Keys[1].ID != "deploy key 2024" {
		t.Fatalf("keys = %+v", r.Keys)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-ssh-key admin json")
}

func TestSSHKeyAddDuplicates(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	// The fingerprint of "aGVsbG8=" is listed for admin@workstation.
	code, e := h.sshAdd(helloKey + " other@host")
	if code != 409 || e.Error.Message != "SSH Key already exists." {
		t.Fatalf("fingerprint dup: %d %+v", code, e)
	}
	// The full comment, with spaces, matches too.
	code, e = h.sshAdd("ssh-ed25519 Zm9v deploy key 2024")
	if code != 409 || e.Error.Message != "SSH Key already exists." {
		t.Fatalf("comment dup: %d %+v", code, e)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-ssh-key admin json", "v-list-user-ssh-key admin json")
}

func TestSSHKeyAddBlankAndHostile(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	code, e := h.sshAdd("   ")
	if code != 400 || e.Error.Message != blankFieldMsg || len(e.Error.Args) != 1 || e.Error.Args[0] != "SSH Key" {
		t.Fatalf("blank: %d %+v", code, e)
	}
	if code, e := h.sshAdd("ssh-ed25519 Zm9v a\nssh-rsa Zm9v b"); code != 400 || e.Error.Message != sshOneLineMsg {
		t.Fatalf("newline: %d %+v", code, e)
	}
	if code, e := h.sshAdd(`command="ls" ssh-ed25519 Zm9v a`); code != 400 || e.Error.Message != sshOptionsMsg {
		t.Fatalf("options: %d %+v", code, e)
	}
	if code, _ := h.sshAdd("no-pty,from=\"1.2.3.4\" ssh-rsa Zm9v a"); code != 400 {
		t.Fatalf("options 2: %d", code)
	}
	if code, _ := h.sshAdd("ssh-ed25519 Zm9v filemanager.ssh.key"); code != 400 {
		t.Fatalf("filemanager: %d", code)
	}
	acctWantCalls(t, h.keyCalls())
}

func TestSSHKeyAddRuns(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code, e := h.sshAdd("  ssh-ed25519 Zm9vYmFy me@laptop  "); code != 200 {
		t.Fatalf("add: %d %+v", code, e)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-ssh-key admin json", "v-add-user-ssh-key admin ssh-ed25519 Zm9vYmFy me@laptop")
}

func TestSSHKeyAddNoCommentGetsHexID(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code, _ := h.sshAdd("ssh-ed25519 Zm9vYmFy"); code != 200 {
		t.Fatalf("add: %d", code)
	}
	calls := h.keyCalls()
	re := regexp.MustCompile(`^v-add-user-ssh-key admin ssh-ed25519 Zm9vYmFy [0-9a-f]{16}$`)
	if len(calls) != 2 || !re.MatchString(calls[1]) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestSSHKeyDelete(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("DELETE", "/api/v1/account/ssh-keys/deploy%20key%202024", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/account/ssh-keys/nope", nil, nil); code != 404 {
		t.Fatalf("unlisted: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/account/ssh-keys/filemanager.ssh.key", nil, nil); code != 404 {
		t.Fatalf("filemanager: %d", code)
	}
	// Its listed ID is the whole authorized_keys comment, ending in the marker.
	if code := h.call("DELETE", "/api/v1/account/ssh-keys/root@rec.hostisan.test%20TS:1790000000%20filemanager.ssh.key", nil, nil); code != 404 {
		t.Fatalf("filemanager full comment: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/account/ssh-keys/-rf", nil, nil); code != 400 {
		t.Fatalf("hostile: %d", code)
	}
	acctWantCalls(t, h.keyCalls(),
		"v-list-user-ssh-key admin json", "v-delete-user-ssh-key admin deploy key 2024",
		"v-list-user-ssh-key admin json", "v-list-user-ssh-key admin json", "v-list-user-ssh-key admin json")
}

func TestKeysReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	for _, c := range [][2]string{
		{"POST", "/api/v1/account/ssh-keys"}, {"DELETE", "/api/v1/account/ssh-keys/x"},
		{"POST", "/api/v1/account/access-keys"}, {"DELETE", "/api/v1/account/access-keys/aB3dEfGh1JkLmN0pQrSt"},
		{"POST", "/api/v1/account/access-keys/bulk"}, {"DELETE", "/api/v1/notifications"},
		{"DELETE", "/api/v1/notifications/1"}, {"POST", "/api/v1/notifications/ack"},
		{"POST", "/api/v1/notifications/1/ack"}, {"DELETE", "/api/v1/logs"}, {"DELETE", "/api/v1/logs/auth"},
	} {
		if code := h.call(c[0], c[1], map[string]any{}, nil); code != 403 {
			t.Errorf("%s %s = %d", c[0], c[1], code)
		}
	}
	acctWantCalls(t, h.keyCalls())
}

// ---------------------------------------------------------------- access keys

func TestAccessKeyGate(t *testing.T) {
	for _, tc := range []struct {
		level string
		bob   bool
		admin int
		user  int
	}{{"2", true, 200, 200}, {"1", true, 200, 403}, {"0", true, 403, 403}} {
		h := newHarnessWith(t, overrides{Config: map[string]string{"API_SYSTEM": tc.level}})
		h.loginAdmin()
		if code := h.call("GET", "/api/v1/account/access-keys", nil, nil); code != tc.admin {
			t.Errorf("level %s root = %d", tc.level, code)
		}
		h = newHarnessWith(t, overrides{Config: map[string]string{"API_SYSTEM": tc.level}})
		h.loginBob()
		if code := h.call("GET", "/api/v1/account/access-keys", nil, nil); code != tc.user {
			t.Errorf("level %s user = %d", tc.level, code)
		}
		if tc.user == 403 {
			for _, p := range [][2]string{{"POST", ""}, {"DELETE", "/aB3dEfGh1JkLmN0pQrSt"}, {"POST", "/bulk"}, {"GET", "/aB3dEfGh1JkLmN0pQrSt"}} {
				if code := h.call(p[0], "/api/v1/account/access-keys"+p[1], map[string]any{}, nil); code != 403 {
					t.Errorf("level %s %v = %d", tc.level, p, code)
				}
			}
			acctWantCalls(t, h.keyCalls())
		}
	}
}

func TestAccessKeyListSortedAndAPIsByRole(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{"v-list-access-keys": {{Args: []string{"admin", "json"}, Stdout: `{
 "b":{"ACCESS_KEY_ID":"b","USER":"admin","PERMISSIONS":[],"COMMENT":"","TIME":"10:00:00","DATE":"2026-03-15"},
 "a":{"ACCESS_KEY_ID":"a","USER":"admin","PERMISSIONS":[],"COMMENT":"","TIME":"09:00:00","DATE":"2026-03-15"},
 "c":{"ACCESS_KEY_ID":"c","USER":"admin","PERMISSIONS":[],"COMMENT":"","TIME":"23:00:00","DATE":"2026-03-14"}}`}}}})
	h.loginAdmin()
	var r AccessKeys
	if code := h.call("GET", "/api/v1/account/access-keys", nil, &r); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(r.Keys) != 3 || r.Keys[0].ACCESS_KEY_ID != "c" || r.Keys[1].ACCESS_KEY_ID != "a" || r.Keys[2].ACCESS_KEY_ID != "b" {
		t.Fatalf("order = %+v", r.Keys)
	}
	if len(r.APIs) < 3 {
		t.Fatalf("root sees all APIs: %+v", r.APIs)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-access-keys admin json", "v-list-apis json")

	h = newHarness(t)
	h.loginBob()
	h.call("GET", "/api/v1/account/access-keys", nil, &r)
	for _, a := range r.APIs {
		if a.ROLE != "user" {
			t.Fatalf("user sees admin API %+v", a)
		}
	}
	if len(r.APIs) == 0 {
		t.Fatal("no APIs for user")
	}
}

func TestAccessKeyViewOwnership(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	// The fixture key belongs to admin: 404, and the secret never leaves.
	var raw map[string]any
	if code := h.call("GET", "/api/v1/account/access-keys/aB3dEfGh1JkLmN0pQrSt", nil, &raw); code != 404 {
		t.Fatalf("other's key: %d", code)
	}
	if code := h.call("GET", "/api/v1/account/access-keys/bad", nil, nil); code != 400 {
		t.Fatalf("bad id: %d", code)
	}
	h2 := newHarness(t)
	h2.loginAdmin()
	var k map[string]any
	if code := h2.call("GET", "/api/v1/account/access-keys/aB3dEfGh1JkLmN0pQrSt", nil, &k); code != 200 || k["ACCESS_KEY_ID"] != "aB3dEfGh1JkLmN0pQrSt" {
		t.Fatalf("own key: %d %v", code, k)
	}
	for key := range k {
		if strings.Contains(strings.ToLower(key), "secret") {
			t.Fatalf("secret in view: %v", k)
		}
	}
	acctWantCalls(t, h.keyCalls(), "v-list-access-key aB3dEfGh1JkLmN0pQrSt json")
}

func TestAccessKeyDeleteOwnership(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	if code := h.call("DELETE", "/api/v1/account/access-keys/aB3dEfGh1JkLmN0pQrSt", nil, nil); code != 404 {
		t.Fatalf("other's key: %d", code)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-access-key aB3dEfGh1JkLmN0pQrSt json")
	h = newHarness(t)
	h.loginAdmin()
	if code := h.call("DELETE", "/api/v1/account/access-keys/aB3dEfGh1JkLmN0pQrSt", nil, nil); code != 200 {
		t.Fatalf("own key: %d", code)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-access-key aB3dEfGh1JkLmN0pQrSt json", "v-delete-access-key aB3dEfGh1JkLmN0pQrSt")
}

func TestAccessKeyBulk(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{"v-list-access-key": {
		{Args: []string{"Kp4Lm7Nq2Rs5Tv8Wx1Yz", "json"}, Stdout: `{"ACCESS_KEY_ID":"Kp4Lm7Nq2Rs5Tv8Wx1Yz","SECRET_ACCESS_KEY":"s","USER":"bob","PERMISSIONS":[],"COMMENT":"","TIME":"1","DATE":"2"}`},
		{Args: []string{"aB3dEfGh1JkLmN0pQrSt", "json"}, Stdout: `{"ACCESS_KEY_ID":"aB3dEfGh1JkLmN0pQrSt","SECRET_ACCESS_KEY":"s","USER":"admin","PERMISSIONS":[],"COMMENT":"","TIME":"1","DATE":"2"}`},
	}}})
	h.loginBob()
	var r BulkReply
	code := h.call("POST", "/api/v1/account/access-keys/bulk", BulkRequest{Action: "delete", Names: []string{"aB3dEfGh1JkLmN0pQrSt", "Kp4Lm7Nq2Rs5Tv8Wx1Yz"}}, &r)
	if code != 200 || len(r.Failed) != 1 || r.Failed[0].Name != "aB3dEfGh1JkLmN0pQrSt" {
		t.Fatalf("bulk: %d %+v", code, r)
	}
	acctWantCalls(t, h.keyCalls(),
		"v-list-access-key aB3dEfGh1JkLmN0pQrSt json",
		"v-list-access-key Kp4Lm7Nq2Rs5Tv8Wx1Yz json", "v-delete-access-key Kp4Lm7Nq2Rs5Tv8Wx1Yz")
	if code := h.call("POST", "/api/v1/account/access-keys/bulk", BulkRequest{Action: "suspend", Names: []string{"x"}}, nil); code != 400 {
		t.Fatalf("action: %d", code)
	}
}

func TestAccessKeyCreate(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r AccessKeyCreated
	code := h.call("POST", "/api/v1/account/access-keys", AddAccessKeyRequest{Permissions: []string{"mail-accounts", "mail-accounts"}, Comment: " New key "}, &r)
	if code != 200 || r.Key.ACCESS_KEY_ID != "Nw1Ky2Ab3Cd4Ef5Gh6Ij" || r.Secret != "DevNewSecretNotReal0123456789abcdefABCD" {
		t.Fatalf("create: %d %+v", code, r)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-apis json", "v-add-access-key admin mail-accounts New key json")
	// GET never carries the secret.
	h.call("GET", "/api/v1/account/access-keys", nil, nil)
	if body := h.rawGet("/api/v1/account/access-keys"); strings.Contains(strings.ToLower(body), "secret") {
		t.Fatalf("secret in list: %s", body)
	}
}

// The UI opens the new key's page after creating it, so the dev fixtures must list the
// key that v-add-access-key returns.
func TestAccessKeyCreatedKeyOpensInDevFixtures(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r AccessKeyCreated
	if code := h.call("POST", "/api/v1/account/access-keys", AddAccessKeyRequest{Permissions: []string{"mail-accounts"}, Comment: "New key"}, &r); code != 200 {
		t.Fatalf("create: %d", code)
	}
	var k hestia.AccessKey
	if code := h.call("GET", "/api/v1/account/access-keys/"+r.Key.ACCESS_KEY_ID, nil, &k); code != 200 || k.ACCESS_KEY_ID != r.Key.ACCESS_KEY_ID {
		t.Fatalf("open new key: %d %+v", code, k)
	}
}

func TestAccessKeyCreateRejects(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	var e APIError
	if code := h.call("POST", "/api/v1/account/access-keys", AddAccessKeyRequest{Permissions: []string{"billing"}}, &e); code != 400 ||
		e.Error.Message != "API %s not allowed" || len(e.Error.Args) != 1 || e.Error.Args[0] != "billing" {
		t.Fatalf("admin api for user: %d %+v", code, e)
	}
	if code := h.call("POST", "/api/v1/account/access-keys", AddAccessKeyRequest{Permissions: []string{}}, &e); code != 400 ||
		e.Error.Message != blankFieldMsg || e.Error.Args[0] != "Permissions" {
		t.Fatalf("empty: %d %+v", code, e)
	}
	if code := h.call("POST", "/api/v1/account/access-keys", AddAccessKeyRequest{Permissions: []string{"mail-accounts"}, Comment: "a\nb"}, nil); code != 400 {
		t.Fatalf("newline comment: %d", code)
	}
	for _, c := range h.keyCalls() {
		if strings.HasPrefix(c, "v-add-access-key") {
			t.Fatalf("ran %s", c)
		}
	}
}

func TestAccessKeyShortCommentVerbatim(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var e APIError
	code := h.call("POST", "/api/v1/account/access-keys", AddAccessKeyRequest{Permissions: []string{"mail-accounts"}, Comment: "x"}, &e)
	if code != 400 || e.Error.Message != "Error: invalid comment format :: x" {
		t.Fatalf("%d %+v", code, e)
	}
}

// ---------------------------------------------------------------- notifications

func TestNotificationsSortedAndWrites(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{"v-list-user-notifications": {{Args: []string{"admin", "json"}, Stdout: `{
 "1":{"TOPIC":"a","NOTICE":"n","TYPE":"info","ACK":"no","PRIORITY":"0","TIME":"1","DATE":"2"},
 "2":{"TOPIC":"b","NOTICE":"n","TYPE":"info","ACK":"no","PRIORITY":"1","TIME":"1","DATE":"2"},
 "3":{"TOPIC":"c","NOTICE":"n","TYPE":"info","ACK":"no","PRIORITY":"0","TIME":"1","DATE":"2"},
 "10":{"TOPIC":"d","NOTICE":"n","TYPE":"info","ACK":"no","PRIORITY":"1","TIME":"1","DATE":"2"}}`}}}})
	h.loginAdmin()
	var r Notifications
	if code := h.call("GET", "/api/v1/notifications", nil, &r); code != 200 {
		t.Fatalf("code %d", code)
	}
	var ids []string
	for _, n := range r.Items {
		ids = append(ids, n.ID)
	}
	if strings.Join(ids, ",") != "10,2,3,1" {
		t.Fatalf("order = %v", ids)
	}
	for _, c := range []struct{ m, p string }{
		{"DELETE", "/api/v1/notifications/2"}, {"DELETE", "/api/v1/notifications"},
		{"POST", "/api/v1/notifications/3/ack"}, {"POST", "/api/v1/notifications/ack"},
	} {
		if code := h.call(c.m, c.p, nil, nil); code != 200 {
			t.Fatalf("%s %s: %d", c.m, c.p, code)
		}
	}
	for _, p := range []string{"0", "abc", "-1", "1;ls", "all", "1234567890"} {
		if code := h.call("DELETE", "/api/v1/notifications/"+p, nil, nil); code != 400 {
			t.Errorf("delete %q = %d", p, code)
		}
		if code := h.call("POST", "/api/v1/notifications/"+p+"/ack", nil, nil); code != 400 {
			t.Errorf("ack %q = %d", p, code)
		}
	}
	acctWantCalls(t, h.keyCalls(),
		"v-list-user-notifications admin json",
		"v-delete-user-notification admin 2", "v-delete-user-notification admin all",
		"v-acknowledge-user-notification admin 3", "v-acknowledge-user-notification admin all")
}

// ---------------------------------------------------------------- logs

func TestLogsOwnNewestFirst(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	var r Logs
	if code := h.call("GET", "/api/v1/logs", nil, &r); code != 200 || r.User != "bob" {
		t.Fatalf("logs: %d %+v", code, r)
	}
	for i := 1; i < len(r.Entries); i++ {
		a, b := r.Entries[i-1], r.Entries[i]
		if a.DATE+a.TIME < b.DATE+b.TIME {
			t.Fatalf("not newest first: %+v", r.Entries)
		}
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-log bob json")
}

func TestLogsReadUpstreamBackslashes(t *testing.T) {
	// Upstream prints a message's backslash raw: bob's real log holds "rm {} \;", which
	// is not valid JSON. The entry is still listed, with the backslash kept.
	rec := recordedEntry(t, "v-list-user-log", "bob", "json")
	if !strings.Contains(rec.Stdout, `{} \;)"`) {
		t.Fatalf("recording changed: %q", rec.Stdout)
	}
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{"v-list-user-log": {rec}}})
	h.loginBob()
	var r Logs
	if code := h.call("GET", "/api/v1/logs", nil, &r); code != 200 || len(r.Entries) < 9 {
		t.Fatalf("logs: %d %+v", code, r)
	}
	found := false
	for _, e := range r.Entries {
		found = found || strings.HasSuffix(e.MESSAGE, `-exec rm {} \;)`)
	}
	if !found {
		t.Errorf("cron entry missing: %+v", r.Entries)
	}
}

func TestLogsPolicies(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_VIEW_LOGS": "no", "POLICY_USER_DELETE_LOGS": "no"}})
	h.loginBob()
	for _, p := range []string{"/api/v1/logs", "/api/v1/logs/auth"} {
		if code := h.call("GET", p, nil, nil); code != 403 {
			t.Errorf("GET %s = %d", p, code)
		}
		if code := h.call("DELETE", p, nil, nil); code != 403 {
			t.Errorf("DELETE %s = %d", p, code)
		}
	}
	acctWantCalls(t, h.keyCalls())
}

func TestLogsDeletePolicyOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_DELETE_LOGS": "no"}})
	h.loginBob()
	if code := h.call("GET", "/api/v1/logs", nil, nil); code != 200 {
		t.Fatalf("view: %d", code)
	}
	if code := h.call("DELETE", "/api/v1/logs", nil, nil); code != 403 {
		t.Fatalf("delete: %d", code)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-log bob json")
}

func TestLogsUserParam(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	for _, p := range []string{"/api/v1/logs?user=admin", "/api/v1/logs/auth?user=admin"} {
		if code := h.call("GET", p, nil, nil); code != 403 {
			t.Errorf("%s = %d", p, code)
		}
	}
	if code := h.call("DELETE", "/api/v1/logs?user=admin", nil, nil); code != 403 {
		t.Errorf("delete user = %d", code)
	}
	acctWantCalls(t, h.keyCalls())

	h = newHarness(t)
	h.loginAdmin()
	var r Logs
	if code := h.call("GET", "/api/v1/logs?user=system", nil, &r); code != 200 || r.User != "system" {
		t.Fatalf("system: %d %+v", code, r)
	}
	for _, bad := range []string{"-x", "a b", "x;y"} {
		if code := h.call("GET", "/api/v1/logs?user="+strings.ReplaceAll(bad, " ", "%20"), nil, nil); code != 400 {
			t.Errorf("user=%q = %d", bad, code)
		}
	}
	if code := h.call("DELETE", "/api/v1/logs?user=system", nil, nil); code != 200 {
		t.Fatalf("delete system: %d", code)
	}
	// An empty ?user= is the own log.
	if code := h.call("GET", "/api/v1/logs?user=", nil, &r); code != 200 || r.User != "admin" {
		t.Fatalf("empty user: %d %+v", code, r)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-log system json", "v-delete-user-log system", "v-list-user-log admin json")
}

func TestLogsSystemNoHideServicesCheck(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_HIDE_SERVICES": "yes"}})
	h.loginAdmin()
	if code := h.call("GET", "/api/v1/logs?user=system", nil, nil); code != 200 {
		t.Fatalf("code %d", code)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-log system json")
}

func TestLogsErrorIsNotFakeRow(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{"v-list-user-log": {{Args: []string{"*", "json"}, Stdout: "Error: boom\n", Exit: 4}}}})
	h.loginAdmin()
	var e APIError
	if code := h.call("GET", "/api/v1/logs", nil, &e); code == 200 || e.Error.Message != "Error: boom" {
		t.Fatalf("%d %+v", code, e)
	}
}

func TestAuthLog(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	body := h.rawGet("/api/v1/logs/auth")
	if strings.Contains(strings.ToUpper(body), "SESSION") || !strings.Contains(body, "198.51.100.23") {
		t.Fatalf("body = %s", body)
	}
	var r AuthLog
	h.call("GET", "/api/v1/logs/auth", nil, &r)
	if len(r.Entries) < 2 || r.Entries[0].DATE+r.Entries[0].TIME < r.Entries[1].DATE+r.Entries[1].TIME {
		t.Fatalf("order: %+v", r.Entries)
	}
	if code := h.call("DELETE", "/api/v1/logs/auth", nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	acctWantCalls(t, h.keyCalls(), "v-list-user-auth-log admin json", "v-list-user-auth-log admin json", "v-delete-user-auth-log admin")
}

func TestAuthLogProtectedAdmin(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginOps()
	// ops looks at root: protected.
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	if code := h.call("GET", "/api/v1/logs/auth", nil, nil); code != 403 {
		t.Fatalf("auth log: %d", code)
	}
	acctWantCalls(t, h.keyCalls())
}

func TestProtectedAdminLogsForOtherAdmin(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginOps() // a non-root admin
	for _, c := range [][2]string{
		{"GET", "/api/v1/logs?user=admin"}, {"GET", "/api/v1/logs/auth?user=admin"},
		{"DELETE", "/api/v1/logs?user=admin"}, {"DELETE", "/api/v1/logs/auth?user=admin"},
	} {
		if code := h.call(c[0], c[1], nil, nil); code != 403 {
			t.Errorf("%s %s = %d", c[0], c[1], code)
		}
	}
	acctWantCalls(t, h.keyCalls())
	// Others and the system log stay open to ops.
	if code := h.call("GET", "/api/v1/logs?user=bob", nil, nil); code != 200 {
		t.Fatalf("bob: %d", code)
	}
	// ROOT_USER itself may read its own.
	h = newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes"}})
	h.loginAdmin()
	if code := h.call("GET", "/api/v1/logs?user=admin", nil, nil); code != 200 {
		t.Fatalf("root own: %d", code)
	}
}

// In demo mode Hestia hides the login history (list_log.php); the API refuses it and
// bootstrap tells the UI to hide the link.
func TestAuthLogDemoMode(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"DEMO_MODE": "yes"}})
	h.loginAdmin()
	if b := h.bootstrap(); b.User == nil || b.User.Features.LoginHistory {
		t.Fatalf("loginHistory shown in demo mode: %+v", b.User)
	}
	for _, m := range []string{"GET", "DELETE"} {
		if code := h.call(m, "/api/v1/logs/auth", nil, nil); code != 403 {
			t.Fatalf("%s auth log in demo mode: %d", m, code)
		}
	}
	acctWantCalls(t, h.keyCalls())

	h = newHarness(t)
	h.loginAdmin()
	if b := h.bootstrap(); !b.User.Features.LoginHistory {
		t.Fatal("loginHistory hidden outside demo mode")
	}
}

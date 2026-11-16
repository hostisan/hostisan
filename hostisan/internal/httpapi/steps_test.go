package httpapi

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hostisan/hostisan/internal/i18n"
	"github.com/hostisan/hostisan/locale"
)

// decodeStep finds the StepMsgids entry a job title or step was filled from, the way
// the UI does: each msgid's %s matches (.+), and the first match wins.
func decodeStep(step string) (msgid, arg string, ok bool) {
	for _, m := range StepMsgids {
		re := regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(string(m)), "%s", "(.+)") + "$")
		if sub := re.FindStringSubmatch(step); sub != nil {
			return string(m), sub[1], true
		}
	}
	return "", "", false
}

func wantDecoded(t *testing.T, step, msgid, arg string) {
	t.Helper()
	got, gotArg, ok := decodeStep(step)
	if !ok || got != msgid || gotArg != arg || fmt.Sprintf(got, gotArg) != step {
		t.Errorf("%q decodes to %q(%q), %v; want %q(%q)", step, got, gotArg, ok, msgid, arg)
	}
}

func TestSysEditorStepIsMsgidWithArg(t *testing.T) {
	e := sysSvcNew(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-service-config": {{Args: []string{"*", "ssh", "yes"}, Stdout: "Error: ERROR: ssh failed to start with new configuration.\n", Exit: 20}},
	}}, nil)
	f := sysFile(e.editor("ssh"), "ssh")
	st := e.job("PUT", "/editors/ssh", EditorSaveRequest{
		Files: []EditorSaveFile{{ID: "ssh", Content: f.Content + "Bogus yes\n", Hash: f.Hash}}, Restart: true})
	if !slices.Equal(st.Steps, []string{"Save /etc/ssh/sshd_config", "Restart ssh", "Check ssh"}) {
		t.Fatalf("steps = %q", st.Steps)
	}
	wantDecoded(t, st.Title, "Save %s configuration", "SSH")
	wantDecoded(t, st.Steps[0], "Save %s", "/etc/ssh/sshd_config")
	wantDecoded(t, st.Steps[1], "Restart %s", "ssh")
	wantDecoded(t, st.Steps[2], "Check %s", "ssh")
}

func TestSysServiceStepsAreMsgidsWithArg(t *testing.T) {
	e := sysSvcNew(t, overrides{}, sysStateWrap)
	st := e.job("POST", "/services/vsftpd/start", nil)
	if !slices.Equal(st.Steps, []string{"Start vsftpd", "Check vsftpd"}) {
		t.Fatalf("steps = %q", st.Steps)
	}
	wantDecoded(t, st.Title, "Start %s", "vsftpd")
	wantDecoded(t, st.Steps[1], "Check %s", "vsftpd")
	st = e.job("POST", "/services/bulk", BulkRequest{Action: "stop", Names: []string{"nginx"}})
	// "Check services" is a msgid of its own (the UI tries a step as a msgid first).
	if st.Title != "Stop services" || !slices.Equal(st.Steps, []string{"Stop nginx", "Check services"}) {
		t.Fatalf("bulk = %q %q", st.Title, st.Steps)
	}
	wantDecoded(t, st.Steps[0], "Stop %s", "nginx")
}

func TestUsersPrecheckMsgidHasNoErrorPrefix(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r APIError
	if code := h.call("POST", "/api/v1/users/bob/unsuspend", nil, &r); code != 409 ||
		r.Error.Message != "User %s is not suspended." || !slices.Equal(r.Error.Args, []string{"bob"}) {
		t.Fatalf("%d %+v", code, r.Error)
	}
}

func TestBulkFailureCarriesMsgidAndArgs(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r BulkReply
	if code := h.call("POST", "/api/v1/users/bulk", BulkRequest{Action: "unsuspend", Names: []string{"bob"}}, &r); code != 200 ||
		len(r.Failed) != 1 || r.Failed[0].Message != "User %s is not suspended." || !slices.Equal(r.Failed[0].Args, []string{"bob"}) {
		t.Fatalf("users: %d %+v", code, r)
	}
	e := sysSvcNew(t, overrides{}, nil) // vsftpd stays stopped
	st := e.job("POST", "/services/bulk", BulkRequest{Action: "start", Names: []string{"vsftpd"}})
	var rep BulkReply
	sysResult(t, st, &rep)
	if len(rep.Failed) != 1 || rep.Failed[0].Message != `Start "%s" failed` || !slices.Equal(rep.Failed[0].Args, []string{"vsftpd"}) {
		t.Fatalf("services: %+v", rep)
	}
}

func TestStepNamesAreExtracted(t *testing.T) {
	known := map[string]bool{}
	pot, err := fs.ReadFile(locale.FS, "hostisan.pot")
	if err != nil {
		t.Fatal(err)
	}
	hestia, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "locale", "hestiacp.pot"))
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{pot, hestia} {
		ids, err := i18n.MsgIDs(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			known[id] = true
		}
	}
	want := []string{"Start services", "Stop services", "Restart services", `Start "%s" failed`, `Stop "%s" failed`}
	for _, m := range StepMsgids {
		want = append(want, string(m))
	}
	for _, id := range want {
		if !known[id] {
			t.Errorf("%q is in neither template: run make i18n-extract", id)
		}
	}
}

// The UI gets StepMsgids through tygo, which can't read a var: stepMsgidsTS
// spells the list out for it, in the same order.
func TestStepMsgidsReachTheUIInOrder(t *testing.T) {
	var want strings.Builder
	want.WriteString("export const StepMsgids: StepMsgid[] = [\n")
	for _, m := range StepMsgids {
		fmt.Fprintf(&want, "  %q,\n", string(m))
	}
	want.WriteString("];\n")
	if stepMsgidsTS != want.String() {
		t.Errorf("stepMsgidsTS differs from StepMsgids:\ngot:\n%s\nwant:\n%s", stepMsgidsTS, want.String())
	}
}

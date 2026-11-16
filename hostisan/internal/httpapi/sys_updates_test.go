package httpapi

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
)

func TestSysUpdatesView(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	from := len(h.fixture.Calls())
	var got UpdatesView
	if code := h.sysDo(base, "GET", "/api/v1/server/updates", nil, &got); code != 200 {
		t.Fatalf("%d", code)
	}
	if ran := h.sysCommands(from); !slices.Equal(ran, []string{"v-list-sys-hestia-updates json", "v-list-sys-hestia-autoupdate plain"}) {
		t.Errorf("ran %q", ran)
	}
	if !got.Autoupdate || len(got.Packages) != 3 {
		t.Fatalf("%+v", got)
	}
	var names, display []string
	for _, p := range got.Packages {
		names = append(names, p.Name)
		display = append(display, p.DisplayName)
		if strings.Contains(p.Description, "Hestia") || p.Description == "" {
			t.Errorf("description %q", p.Description)
		}
	}
	if !slices.Equal(names, []string{"hestia", "hestia-php", "hestia-nginx"}) ||
		!slices.Equal(display, []string{"hostisan", "hostisan-php", "hostisan-nginx"}) {
		t.Errorf("names %v display %v", names, display)
	}
	php := got.Packages[1]
	if php.Description != "Hostisan internal php interpreter" || php.Version != "8.3.14" || php.Arch != "amd64" || php.Updated {
		t.Errorf("php = %+v", php)
	}
	if !got.Packages[0].Updated {
		t.Errorf("hestia = %+v", got.Packages[0])
	}
}

// The Go terminal package is listed by v-list-sys-hestia-updates when installed (C8 F1).
func TestSysUpdatesListsTerminal(t *testing.T) {
	out := `{
    "hestia": {"VERSION": "1:1.10.5-101+ubuntu24.04", "ARCH": "amd64", "UPDATED": "no", "DESCR": "Hestia core package", "TIME": "06:12:01", "DATE": "2026-10-01"},
    "hestia-web-terminal": {"VERSION": "1:2.0.0-101+ubuntu24.04", "ARCH": "amd64", "UPDATED": "no", "DESCR": "Hestia web terminal", "TIME": "06:12:01", "DATE": "2026-10-01"}
}
`
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-hestia-updates": {{Args: []string{"json"}, Stdout: out}},
	}})
	var got UpdatesView
	if code := h.sysDo(base, "GET", "/api/v1/server/updates", nil, &got); code != 200 || len(got.Packages) != 2 {
		t.Fatalf("%d %+v", code, got)
	}
	term := got.Packages[1]
	if term.Name != "hestia-web-terminal" || term.DisplayName != "hostisan-web-terminal" ||
		term.Description != "Hostisan web terminal" || term.Version != "1:2.0.0-101+ubuntu24.04" || term.Updated {
		t.Errorf("terminal = %+v", term)
	}
}

func TestSysUpdatesFailureIsShown(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-hestia-updates": {{Args: []string{"json"}, Stdout: "Error: apt failed\n", Exit: 99}},
	}})
	var e APIError
	if code := h.sysDo(base, "GET", "/api/v1/server/updates", nil, &e); code != 500 || e.Error.Message != "Error: apt failed" {
		t.Errorf("%d %+v", code, e)
	}
}

func TestSysAutoupdateToggle(t *testing.T) {
	cases := []struct {
		current string
		enable  bool
		want    []string
	}{
		{"Enabled\n", true, []string{"v-list-sys-hestia-autoupdate plain"}},
		{"Disabled\n", false, []string{"v-list-sys-hestia-autoupdate plain"}},
		{"Enabled\n", false, []string{"v-list-sys-hestia-autoupdate plain", "v-delete-cron-hestia-autoupdate "}},
		{"Disabled\n", true, []string{"v-list-sys-hestia-autoupdate plain", "v-add-cron-hestia-autoupdate "}},
	}
	for _, c := range cases {
		h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
			"v-list-sys-hestia-autoupdate": {{Args: []string{"plain"}, Stdout: c.current}},
		}})
		from := len(h.fixture.Calls())
		var ok OK
		if code := h.sysDo(base, "POST", "/api/v1/server/updates/autoupdate", AutoupdateRequest{Enabled: c.enable}, &ok); code != 200 || ok.Status != "ok" {
			t.Fatalf("%+v: %d", c, code)
		}
		if ran := h.sysCommands(from); !slices.Equal(ran, c.want) {
			t.Errorf("%q -> %v: ran %q, want %q", c.current, c.enable, ran, c.want)
		}
	}
}

func TestSysAutoupdateErrorShown(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-sys-hestia-autoupdate": {{Args: []string{"plain"}, Stdout: "Disabled\n"}},
		"v-add-cron-hestia-autoupdate": {{Args: []string{}, Stdout: "Error: cron is locked\n", Exit: 99}},
	}})
	var e APIError
	if code := h.sysDo(base, "POST", "/api/v1/server/updates/autoupdate", AutoupdateRequest{Enabled: true}, &e); code != 500 ||
		e.Error.Message != "Error: cron is locked" {
		t.Errorf("%d %+v", code, e)
	}
}

func TestSysWhiteLabelView(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Config: map[string]string{"FROM_NAME": "Acme", "HIDE_DOCS": "yes"}})
	var got WhiteLabelView
	if code := h.sysDo(base, "GET", "/api/v1/server/white-label", nil, &got); code != 200 {
		t.Fatalf("%d", code)
	}
	want := WhiteLabelView{AppName: "Hostisan", Title: "{{page}} - {{hostname}} - {{appname}}", FromName: "Acme",
		SubjectEmail: "{{subject}}", HideDocs: true, Hostname: "server1.acme-hosting.net"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func whiteLabelBase() WhiteLabelRequest {
	return WhiteLabelRequest{AppName: "Hostisan", Title: "{{page}} - {{hostname}} - {{appname}}", SubjectEmail: "{{subject}}"}
}

func (h *harness) configReads() int {
	n := 0
	for _, c := range h.fixture.Calls() {
		if c.Cmd == "v-list-sys-config" {
			n++
		}
	}
	return n
}

func TestSysWhiteLabelSaveOrderAndInvalidate(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	req := WhiteLabelRequest{AppName: "Acme Panel", Title: "T", SubjectEmail: "S", HideDocs: true,
		FromName: "F", FromEmail: "f@example.com", UpdateLogo: true}
	if _, err := h.deps.H.Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := h.configReads()
	from := len(h.fixture.Calls())
	var ok OK
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", req, &ok); code != 200 || ok.Status != "ok" {
		t.Fatalf("%d", code)
	}
	want := []string{
		"v-change-sys-config-value APP_NAME Acme Panel", "v-change-sys-config-value TITLE T",
		"v-change-sys-config-value SUBJECT_EMAIL S", "v-change-sys-config-value HIDE_DOCS yes",
		"v-change-sys-config-value FROM_NAME F", "v-change-sys-config-value FROM_EMAIL f@example.com",
		"v-update-white-label-logo ",
	}
	if ran := h.sysCommands(from); !slices.Equal(ran, want) {
		t.Errorf("ran %q\nwant %q", ran, want)
	}
	if _, err := h.deps.H.Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.configReads() != before+1 {
		t.Errorf("config reads %d -> %d, want one fresh read after the save", before, h.configReads())
	}
}

func TestSysWhiteLabelHideDocsRunsOnce(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	req := whiteLabelBase()
	req.HideDocs = true
	from := len(h.fixture.Calls())
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if ran := h.sysCommands(from); !slices.Equal(ran, []string{"v-change-sys-config-value HIDE_DOCS yes"}) {
		t.Errorf("ran %q", ran)
	}
}

func TestSysWhiteLabelClearField(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Config: map[string]string{"FROM_NAME": "Acme"}})
	from := len(h.fixture.Calls())
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", whiteLabelBase(), nil); code != 200 {
		t.Fatalf("%d", code)
	}
	calls := h.fixture.Calls()[from:]
	var got []string
	for _, c := range calls {
		if c.Cmd == "v-change-sys-config-value" {
			got = append(got, strings.Join(c.Args, "|"))
		}
	}
	if !slices.Equal(got, []string{"FROM_NAME|"}) {
		t.Errorf("calls = %q", got)
	}
	for _, c := range calls {
		if c.Cmd == "v-change-sys-config-value" && (len(c.Args) != 2 || c.Args[1] != "") {
			t.Errorf("args = %#v, want [FROM_NAME \"\"]", c.Args)
		}
	}
}

func TestSysWhiteLabelUnchangedRunsNothing(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	if _, err := h.deps.H.Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := h.configReads()
	from := len(h.fixture.Calls())
	// AppName is the rewritten value the form shows; saving it back is a no-op.
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", whiteLabelBase(), nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if ran := h.sysCommands(from); len(ran) != 0 {
		t.Errorf("ran %q", ran)
	}
	_, _ = h.deps.H.Config(context.Background())
	if h.configReads() != before {
		t.Errorf("config invalidated by an unchanged save (%d -> %d)", before, h.configReads())
	}
}

func TestSysWhiteLabelValidation(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	bad := map[string]func(*WhiteLabelRequest){
		"blank app name":   func(r *WhiteLabelRequest) { r.AppName = "" },
		"spaces app name":  func(r *WhiteLabelRequest) { r.AppName = "   " },
		"pipe in app name": func(r *WhiteLabelRequest) { r.AppName = "A|B" },
		"pipe in title":    func(r *WhiteLabelRequest) { r.Title = "a|b" },
		"quote":            func(r *WhiteLabelRequest) { r.FromName = `it's` },
		"newline":          func(r *WhiteLabelRequest) { r.SubjectEmail = "a\nb" },
		"backslash":        func(r *WhiteLabelRequest) { r.FromEmail = `a\b` },
	}
	for name, mod := range bad {
		req := whiteLabelBase()
		req.HideDocs = true // would run a command if validation let it through
		mod(&req)
		from := len(h.fixture.Calls())
		var e APIError
		if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", req, &e); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("%s: %d %+v", name, code, e)
		}
		if ran := h.sysCommands(from); len(ran) != 0 {
			t.Errorf("%s ran %q", name, ran)
		}
	}
}

func TestSysWhiteLabelStopsAtFirstError(t *testing.T) {
	h, base := sysInfoServer(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-change-sys-config-value": {
			{Args: []string{"TITLE", "*"}, Stdout: "Error: can't write config\n", Exit: 99},
			{Args: []string{"*", "*"}},
		},
	}})
	req := WhiteLabelRequest{AppName: "Acme", Title: "T", SubjectEmail: "S", HideDocs: true, FromName: "F", UpdateLogo: true}
	before := h.configReads()
	_, _ = h.deps.H.Config(context.Background())
	from := len(h.fixture.Calls())
	var e APIError
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", req, &e); code != 500 || e.Error.Message != "Error: can't write config" {
		t.Fatalf("%d %+v", code, e)
	}
	want := []string{"v-change-sys-config-value APP_NAME Acme", "v-change-sys-config-value TITLE T"}
	if ran := h.sysCommands(from); !slices.Equal(ran, want) {
		t.Errorf("ran %q, want %q", ran, want)
	}
	// APP_NAME had changed before the failure, so the config is read again.
	_, _ = h.deps.H.Config(context.Background())
	if h.configReads() != before+1 {
		t.Errorf("config reads = %d, want %d", h.configReads(), before+1)
	}
}

func TestSysWhiteLabelLogoOnly(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	req := whiteLabelBase()
	req.UpdateLogo = true
	from := len(h.fixture.Calls())
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if ran := h.sysCommands(from); !slices.Equal(ran, []string{"v-update-white-label-logo "}) {
		t.Errorf("ran %q", ran)
	}
	before := h.configReads()
	_, _ = h.deps.H.Config(context.Background())
	if h.configReads() != before+1 {
		t.Errorf("config not invalidated after a logo-only save")
	}
}

func TestSysWhiteLabelTrimsValues(t *testing.T) {
	h, base := sysInfoServer(t, overrides{})
	req := whiteLabelBase()
	req.AppName = "  Hostisan \t"
	req.Title = " {{page}} - {{hostname}} - {{appname}} "
	req.FromName = "  Acme "
	from := len(h.fixture.Calls())
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if ran := h.sysCommands(from); !slices.Equal(ran, []string{"v-change-sys-config-value FROM_NAME Acme"}) {
		t.Errorf("ran %q", ran)
	}
}

// sysConfigWriter makes v-list-sys-config reflect APP_NAME changes made through
// v-change-sys-config-value, as the real core does.
type sysConfigWriter struct {
	runner.Runner
	mu      sync.Mutex
	appName string
}

func (r *sysConfigWriter) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	res, err := r.Runner.Run(ctx, cmd, args...)
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case err == nil && cmd == "v-change-sys-config-value" && len(args) == 2 && args[0] == "APP_NAME":
		r.appName = args[1]
	case err == nil && cmd == "v-list-sys-config" && r.appName != "":
		var doc map[string]any
		if json.Unmarshal(res.Stdout, &doc) == nil {
			doc["config"].(map[string]any)["APP_NAME"] = r.appName
			res.Stdout, _ = json.Marshal(doc)
		}
	}
	return res, err
}

func TestSysWhiteLabelBootstrapShowsNewAppName(t *testing.T) {
	h, base := sysInfoServer(t, overrides{}, func(d *Deps) {
		w := &sysConfigWriter{Runner: d.R}
		d.R, d.H = w, &hestia.Client{R: w}
	})
	if b := h.bootstrap(); b.AppName != "Hostisan" {
		t.Fatalf("before: %q", b.AppName)
	}
	req := whiteLabelBase()
	req.AppName = "Acme Panel"
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", req, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if b := h.bootstrap(); b.AppName != "Acme Panel" {
		t.Errorf("after: %q", b.AppName)
	}
}

func TestSysNoResetLogoPath(t *testing.T) {
	for _, p := range sysPatterns(&server{Deps: newHarness(t).deps}) {
		if strings.Contains(strings.ToLower(p), "reset") || strings.Contains(strings.ToLower(p), "logo") {
			t.Errorf("route %q", p)
		}
	}
	// A body field named resetLogo is ignored: only the logo update command ever runs.
	h, base := sysInfoServer(t, overrides{})
	from := len(h.fixture.Calls())
	body := map[string]any{"appName": "Hostisan", "title": "{{page}} - {{hostname}} - {{appname}}",
		"subjectEmail": "{{subject}}", "resetLogo": true, "reset": true}
	if code := h.sysDo(base, "PUT", "/api/v1/server/white-label", body, nil); code != 200 {
		t.Fatalf("%d", code)
	}
	if ran := h.sysCommands(from); len(ran) != 0 {
		t.Errorf("ran %q", ran)
	}
}

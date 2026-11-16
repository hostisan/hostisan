package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
)

// Fixtures used here (shapes from the core's own print code):
//   - dev v-search-object / v-search-user-object: _source bin/v-search-object:31-85
//     (json_list_search: tab indent, no ID field, "," -> ", ") and :146-266 (rows).
//   - testdata/fixtures/tools_search/v-search-*.json "www": _source bin/v-search-object
//     :204-215 (RECORD rows: RESULT=$RECORD.$DOMAIN, LINK=$ID, PARENT=$DOMAIN) for a
//     run where the line matched before the record set DOMAIN=bakery.example (web.conf
//     bakery.example with alias www.bakery.example, :178-188), so the record verifies.
//   - demo-mode refusal: _source func/main.sh:2016-2022 (check_hestia_demo_mode).

const srDemoText = "ERROR: Unable to perform operation due to security restrictions that are in place.\n"

var srToolsDir = filepath.Join("..", "..", "testdata", "fixtures", "tools_search")

// searchCalls is every search and record listing so far, as "cmd arg arg".
func (h *harness) searchCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if strings.HasPrefix(c.Cmd, "v-search-") || c.Cmd == "v-list-dns-records" {
			out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	return out
}

// srReply is a search reply or an API error.
type srReply struct {
	SearchResults
	Error ErrorBody `json:"error"`
}

func (h *harness) search(q, u string) (int, SearchResults, APIError) {
	h.t.Helper()
	p := "/api/v1/search?q=" + url.QueryEscape(q)
	if u != "" {
		p += "&u=" + url.QueryEscape(u)
	}
	var r srReply
	code := h.call("GET", p, nil, &r)
	return code, r.SearchResults, APIError{Error: r.Error}
}

func srRow(t *testing.T, rows []SearchRow, typ, key, result string) SearchRow {
	t.Helper()
	for _, r := range rows {
		if r.Type == typ && r.Key == key && r.Result == result {
			return r
		}
	}
	t.Fatalf("no %s %s %s row in %+v", typ, key, result, rows)
	return SearchRow{}
}

func TestSearchAdminAll(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	code, res, e := h.search("example", "")
	if code != 200 {
		t.Fatalf("%d %+v", code, e)
	}
	// "example" matches no record rows (R4), so no zone is listed.
	acctWantCalls(t, h.searchCalls(), "v-search-object example json")
	if res.Query != "example" || res.User != "" || len(res.Rows) != 5 {
		t.Fatalf("results = %+v", res)
	}
	for i, r := range res.Rows {
		if r.ID != fmt.Sprint(i+1) {
			t.Errorf("row %d id = %q (core order kept)", i, r.ID)
		}
	}
	admin := res.Rows[0]
	if admin.User != "admin" || admin.Route != "/web/example.net" || !admin.Linked || admin.LoginAs || !admin.Suspended {
		t.Errorf("admin row = %+v", admin)
	}
	for _, r := range res.Rows[1:] {
		if r.User != "bob" || !r.LoginAs || !r.Linked {
			t.Errorf("bob row = %+v", r)
		}
	}
	for _, c := range []struct{ typ, key, result, route string }{
		{"web", "DOMAIN", "bakery.example", "/web/bakery.example"},
		{"dns", "DOMAIN", "bakery.example", "/dns/bakery.example/edit"},
		{"mail", "DOMAIN", "bakery.example", "/mail/bakery.example/edit"},
		{"cron", "JOB", "wget -q -O /dev/null https://bakery.example/", "/cron/2"},
	} {
		if r := srRow(t, res.Rows, c.typ, c.key, c.result); r.Route != c.route {
			t.Errorf("%s %s route = %q, want %q", c.typ, c.result, r.Route, c.route)
		}
	}
	if r := srRow(t, res.Rows, "web", "DOMAIN", "bakery.example"); r.Alias != "www.bakery.example" || r.Date != "2026-10-19" || r.Time != "14:20:03" {
		t.Errorf("web row fields = %+v", r)
	}
}

func TestSearchRecordLinked(t *testing.T) {
	h := newHarnessWith(t, overrides{Dir: srToolsDir})
	h.loginAdmin()
	code, res, e := h.search("www", "")
	if code != 200 {
		t.Fatalf("%d %+v", code, e)
	}
	acctWantCalls(t, h.searchCalls(), "v-search-object www json", "v-list-dns-records bob bakery.example json")
	rec := srRow(t, res.Rows, "dns", "RECORD", "www.bakery.example")
	if rec.Route != "/dns/bakery.example/records/5" || !rec.Linked || !rec.LoginAs || rec.Link != "5" || rec.Parent != "bakery.example" {
		t.Fatalf("record row = %+v", rec)
	}
	if w := srRow(t, res.Rows, "web", "DOMAIN", "bakery.example"); w.Route != "/web/bakery.example" || !w.Linked {
		t.Errorf("web row = %+v", w)
	}
}

func TestSearchAccountRow(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	code, res, e := h.search("info", "")
	if code != 200 || len(res.Rows) != 1 {
		t.Fatalf("%d %+v %+v", code, res, e)
	}
	r := res.Rows[0]
	if r.Route != "/mail/bakery.example/accounts/info" || !r.Linked || r.LoginAs {
		t.Fatalf("account row = %+v", r)
	}
	acctWantCalls(t, h.searchCalls(), "v-search-user-object bob info json")
}

func TestSearchAdminOwnerFilter(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	code, res, e := h.search("example", "bob")
	if code != 200 {
		t.Fatalf("%d %+v", code, e)
	}
	acctWantCalls(t, h.searchCalls(), "v-search-user-object bob example json")
	if res.User != "bob" || len(res.Rows) != 4 {
		t.Fatalf("results = %+v", res)
	}
	for _, r := range res.Rows {
		if r.User != "bob" || !r.LoginAs {
			t.Errorf("row = %+v", r)
		}
	}
}

func TestSearchLookingAs(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil); code != 200 {
		t.Fatalf("login-as %d", code)
	}
	code, res, e := h.search("example", "alice")
	if code != 200 {
		t.Fatalf("%d %+v", code, e)
	}
	acctWantCalls(t, h.searchCalls(), "v-search-user-object bob example json")
	if res.User != "" || len(res.Rows) != 4 {
		t.Fatalf("results = %+v", res)
	}
	for _, r := range res.Rows {
		if r.LoginAs || !r.Linked {
			t.Errorf("row = %+v", r)
		}
	}
}

func TestSearchUser(t *testing.T) {
	h := newHarness(t)
	h.loginBob()
	code, res, e := h.search("example", "admin")
	if code != 200 {
		t.Fatalf("%d %+v", code, e)
	}
	acctWantCalls(t, h.searchCalls(), "v-search-user-object bob example json")
	if res.User != "" || len(res.Rows) != 4 {
		t.Fatalf("results = %+v", res)
	}
	for _, r := range res.Rows {
		if r.LoginAs || r.User != "bob" {
			t.Errorf("row = %+v", r)
		}
	}
}

func TestSearchRefusesBeforeCommand(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, c := range []struct{ q, u string }{
		{"-v", ""},
		{"x;y", ""},
		{strings.Repeat("a", 51), ""},
		{"", ""},      // R2: the core prints nothing for "", so it isn't asked
		{"bad:x", ""}, // R2: the core's own "invalid object format"
		{"a$(id)", ""},
		{"ex\nample", ""},
		{"exämple", ""},
		{"example", "Bad!"},
		{"example", "-bob"},
	} {
		code, _, e := h.search(c.q, c.u)
		if code != 400 || e.Error.Code != "invalid" {
			t.Errorf("q=%q u=%q: %d %+v", c.q, c.u, code, e)
		}
		if c.u == "" && e.Error.Message != "Invalid search query" {
			t.Errorf("q=%q message = %q", c.q, e.Error.Message)
		}
	}
	acctWantCalls(t, h.searchCalls())
	// A 50-character query and the bin's other characters are accepted.
	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-search-object": {{Args: []string{"*", "json"}, Stdout: "{\n\n}\n"}},
	}})
	h.loginAdmin()
	for _, q := range []string{strings.Repeat("a", 50), "a-b|c d.e_F9"} {
		if code, _, e := h.search(q, ""); code != 200 {
			t.Errorf("%q: %d %+v", q, code, e)
		}
	}
	acctWantCalls(t, h.searchCalls(), "v-search-object "+strings.Repeat("a", 50)+" json", "v-search-object a-b|c d.e_F9 json")
}

func TestSearchHiddenAdmin(t *testing.T) {
	cfg := map[string]string{"POLICY_SYSTEM_HIDE_ADMIN": "yes", "ROOT_USER": "admin"}
	h := newHarnessWith(t, overrides{Config: cfg})
	h.loginOps()
	code, res, e := h.search("example", "")
	if code != 200 || len(res.Rows) != 4 {
		t.Fatalf("ops: %d %+v %+v", code, res, e)
	}
	for _, r := range res.Rows {
		if r.User == "admin" {
			t.Errorf("hidden admin row shown: %+v", r)
		}
	}
	h2 := newHarnessWith(t, overrides{Config: cfg})
	h2.loginAdmin()
	if code, res, _ := h2.search("example", ""); code != 200 || len(res.Rows) != 5 || res.Rows[0].User != "admin" {
		t.Fatalf("admin (D2: still sees its own rows): %d %+v", code, res)
	}
}

func TestSearchProtectedAdmin(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginOps()
	code, res, e := h.search("example", "")
	if code != 200 || len(res.Rows) != 5 {
		t.Fatalf("%d %+v %+v", code, res, e)
	}
	for _, r := range res.Rows {
		switch r.User {
		case "admin":
			if r.Linked || r.LoginAs {
				t.Errorf("protected admin row = %+v", r)
			}
		case "bob":
			if !r.Linked || !r.LoginAs {
				t.Errorf("bob row = %+v", r)
			}
		}
	}
	// ROOT_USER itself is not protected from itself.
	h2 := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h2.loginAdmin()
	if _, res, _ := h2.search("example", ""); !res.Rows[0].Linked {
		t.Errorf("root's own row = %+v", res.Rows[0])
	}
}

func TestSearchRecordUnverified(t *testing.T) {
	// The dev "www" row took bakery.example as its zone (SR33); record 3 there is "@".
	h := newHarness(t)
	h.loginBob()
	code, res, e := h.search("www", "")
	if code != 200 || len(res.Rows) != 2 {
		t.Fatalf("%d %+v %+v", code, res, e)
	}
	acctWantCalls(t, h.searchCalls(), "v-search-user-object bob www json", "v-list-dns-records bob bakery.example json")
	if rec := srRow(t, res.Rows, "dns", "RECORD", "www.bakery.example"); rec.Linked || rec.Route != "/dns/bakery.example/records/3" {
		t.Errorf("stale record = %+v", rec)
	}

	// The zone listed without record 5: still shown, unlinked.
	h = newHarnessWith(t, overrides{Dir: srToolsDir, Fixtures: map[string][]fixtureEntry{
		"v-list-dns-records": {{Args: []string{"bob", "bakery.example", "json"}, Stdout: "{\n    \"1\": {\n        \"RECORD\": \"@\",\n        \"TYPE\": \"NS\",\n        \"PRIORITY\": \"\",\n        \"VALUE\": \"ns1.example.com.\",\n        \"ID\": \"1\",\n        \"SUSPENDED\": \"no\",\n        \"TIME\": \"12:00:00\",\n        \"DATE\": \"2026-01-10\",\n        \"TTL\": \"14400\"\n    }\n}\n"}},
	}})
	h.loginAdmin()
	code, res, _ = h.search("www", "")
	if code != 200 || len(res.Rows) != 2 {
		t.Fatalf("%d %+v", code, res)
	}
	if rec := srRow(t, res.Rows, "dns", "RECORD", "www.bakery.example"); rec.Linked || rec.LoginAs {
		t.Errorf("unverified record = %+v", rec)
	}

	// A listing that fails leaves the row unlinked; the search still answers.
	h = newHarnessWith(t, overrides{Dir: srToolsDir, Fixtures: map[string][]fixtureEntry{
		"v-list-dns-records": {{Args: []string{"*", "*", "json"}, Stdout: "Error: dns domain bakery.example doesn't exist\n", Exit: 3}},
	}})
	h.loginAdmin()
	if code, res, _ := h.search("www", ""); code != 200 || srRow(t, res.Rows, "dns", "RECORD", "www.bakery.example").Linked {
		t.Errorf("failed listing: %d %+v", code, res)
	}

	// 21 distinct zones: only 20 are listed; the 21st zone's row stays unlinked.
	var rows []string
	for i := 1; i <= 21; i++ {
		rows = append(rows, fmt.Sprintf("\t%q: {\n\t\t\"USER\": \"bob\",\n\t\t\"TYPE\": \"dns\",\n\t\t\"KEY\": \"RECORD\",\n\t\t\"RESULT\": \"www.z%d.example\",\n\t\t\"ALIAS\": \"\",\n\t\t\"LINK\": \"3\",\n\t\t\"PARENT\": \"z%d.example\",\n\t\t\"SUSPENDED\": \"no\",\n\t\t\"TIME\": \"12:00:01\",\n\t\t\"DATE\": \"2026-01-10\"\n\t}", fmt.Sprint(i), i, i))
	}
	zoneWithWWW := "{\n    \"3\": {\n        \"RECORD\": \"www\",\n        \"TYPE\": \"A\",\n        \"PRIORITY\": \"\",\n        \"VALUE\": \"10.0.0.5\",\n        \"ID\": \"3\",\n        \"SUSPENDED\": \"no\",\n        \"TIME\": \"12:00:01\",\n        \"DATE\": \"2026-01-10\",\n        \"TTL\": \"14400\"\n    }\n}\n"
	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-search-object":    {{Args: []string{"www", "json"}, Stdout: "{\n" + strings.Join(rows, ",\n") + "\n}\n"}},
		"v-list-dns-records": {{Args: []string{"bob", "*", "json"}, Stdout: zoneWithWWW}},
	}})
	h.loginAdmin()
	code, res, _ = h.search("www", "")
	if code != 200 || len(res.Rows) != 21 {
		t.Fatalf("21 zones: %d %d rows", code, len(res.Rows))
	}
	calls := h.searchCalls()
	if len(calls) != 21 || calls[20] != "v-list-dns-records bob z20.example json" {
		t.Fatalf("calls = %q", calls)
	}
	for i, r := range res.Rows {
		if want := i < 20; r.Linked != want {
			t.Errorf("row %d (%s) linked = %v, want %v", i, r.Parent, r.Linked, want)
		}
	}
}

// timeoutRunner records the timeout each search call carried, and can fail one.
type timeoutRunner struct {
	runner.Runner
	mu   sync.Mutex
	seen map[string]time.Duration
	fail error
}

func (r *timeoutRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if d, ok := runner.Timeout(ctx); ok {
		r.mu.Lock()
		r.seen[cmd] = d
		r.mu.Unlock()
	}
	if r.fail != nil && strings.HasPrefix(cmd, "v-search-") {
		return runner.Result{}, r.fail
	}
	return r.Runner.Run(ctx, cmd, args...)
}

func TestSearchErrors(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	// R2: a valid query that hits a conf line the core can't read. Colons no longer
	// do that (SR32, core edit B2g): "2001" finds nothing instead.
	code, _, e := h.search("unreadable", "")
	if code != 422 || e.Error.Code != "search_failed" ||
		e.Error.Message != "The search stopped on an object it could not read. Try a more specific search." {
		t.Errorf("unreadable: %d %+v", code, e)
	}
	if code, _, e := h.search("2001", ""); code != 200 {
		t.Errorf("2001: %d %+v", code, e)
	}
	// Exit 3 (unknown owner filter).
	if code, _, e := h.search("example", "nobody"); code != 404 || e.Error.Code != "not_found" {
		t.Errorf("nobody: %d %+v", code, e)
	}

	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-search-object":      {{Args: []string{"*", "json"}, Stdout: srDemoText, Exit: 1}},
		"v-search-user-object": {{Args: []string{"*", "*", "json"}, Stdout: srDemoText, Exit: 1}},
	}})
	h.loginBob()
	if code, _, e := h.search("example", ""); code != 403 || e.Error.Code != "forbidden" {
		t.Errorf("demo mode (user): %d %+v", code, e)
	}
	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-search-object": {{Args: []string{"*", "json"}, Stdout: srDemoText, Exit: 1}},
	}})
	h.loginAdmin()
	if code, _, e := h.search("example", ""); code != 403 || e.Error.Code != "forbidden" {
		t.Errorf("demo mode (admin): %d %+v", code, e)
	}

	// The core's own "invalid object format" for a query Hostisan let through.
	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-search-object": {{Args: []string{"*", "json"}, Stdout: "Error: invalid object format :: example\n", Exit: 2}},
	}})
	h.loginAdmin()
	if code, _, e := h.search("example", ""); code != 400 || e.Error.Code != "invalid" || e.Error.Message != "Invalid search query" {
		t.Errorf("invalid object format: %d %+v", code, e)
	}

	// The search runs with an 80 s timeout; the record listings too.
	tr := &timeoutRunner{seen: map[string]time.Duration{}}
	h = newHarnessWith(t, overrides{Dir: srToolsDir}, func(d *Deps) { tr.Runner = d.R; d.R = tr })
	h.loginAdmin()
	if code, _, e := h.search("www", ""); code != 200 {
		t.Fatalf("www: %d %+v", code, e)
	}
	if d := tr.seen["v-list-dns-records"]; tr.seen["v-search-object"] != 80*time.Second || d <= 0 || d > 10*time.Second {
		t.Errorf("timeouts = %v (search 80 s, each zone listing at most 10 s)", tr.seen)
	}
	tr.fail = fmt.Errorf("%w: v-search-object", runner.ErrTimeout)
	if code, _, e := h.search("www", ""); code != 504 || e.Error.Code != "timeout" {
		t.Errorf("timed out: %d %+v", code, e)
	}
}

func TestSearchEmpty(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var raw map[string]any
	if code := h.call("GET", "/api/v1/search?q=zzz-none", nil, &raw); code != 200 {
		t.Fatalf("code %d", code)
	}
	rows, ok := raw["rows"].([]any)
	if !ok || len(rows) != 0 {
		t.Fatalf("rows = %#v (want [], never null)", raw["rows"])
	}
	acctWantCalls(t, h.searchCalls(), "v-search-object zzz-none json")
}

func TestSearchGuards(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	if code, _, e := h.search("example", ""); code != 401 {
		t.Errorf("signed out: %d %+v", code, e)
	}
	acctWantCalls(t, h.searchCalls())
	// Read-only sessions may search (GET).
	h = newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginOps()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	if code, _, e := h.search("example", ""); code != 200 {
		t.Errorf("read-only: %d %+v", code, e)
	}
}

// One deadline for the whole request: zone listings left when it has passed are
// skipped and their rows stay unlinked.
func TestSearchDeadlineSkipsListings(t *testing.T) {
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	old := searchNow
	searchNow = func() time.Time { return now }
	t.Cleanup(func() { searchNow = old })
	slow := &clockRunner{advance: func() { now = now.Add(81 * time.Second) }}
	h := newHarnessWith(t, overrides{Dir: srToolsDir}, func(d *Deps) { slow.Runner = d.R; d.R = slow })
	h.loginAdmin()
	code, res, e := h.search("www", "")
	if code != 200 {
		t.Fatalf("%d %+v", code, e)
	}
	acctWantCalls(t, h.searchCalls(), "v-search-object www json")
	if srRow(t, res.Rows, "dns", "RECORD", "www.bakery.example").Linked {
		t.Error("record linked without a listing")
	}
}

// clockRunner moves the test clock while the search command runs.
type clockRunner struct {
	runner.Runner
	advance func()
}

func (c *clockRunner) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	if strings.HasPrefix(cmd, "v-search-") {
		c.advance()
	}
	return c.Runner.Run(ctx, cmd, args...)
}

package httpapi

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (h *harness) loginAdmin() {
	h.t.Helper()
	h.bootstrap()
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "admin", Password: "pw"}, nil); code != 200 {
		h.t.Fatalf("admin login: %d", code)
	}
}

func (h *harness) loginBob() {
	h.t.Helper()
	h.bootstrap()
	h.call("POST", "/api/v1/auth/login", LoginRequest{Username: "bob", Password: "pw"}, nil)
	if code := h.call("POST", "/api/v1/auth/login", LoginRequest{TwoFA: "123456"}, nil); code != 200 {
		h.t.Fatalf("bob 2fa: %d", code)
	}
}

func (h *harness) dnsCalls() []string {
	var out []string
	for _, c := range h.fixture.Calls() {
		if strings.Contains(c.Cmd, "dns") {
			out = append(out, c.Cmd+" "+strings.Join(c.Args, " "))
		}
	}
	return out
}

// userFixture is a v-list-user entry for name with the given field overrides.
func userFixture(t *testing.T, name string, fields map[string]string) []fixtureEntry {
	t.Helper()
	base := map[string]string{
		"NAME": name, "ROLE": "user", "SUSPENDED": "no", "DNS_TEMPLATE": "gmail",
		"DNS_DOMAINS": "5", "PREF_UI_SORT": "name", "LANGUAGE": "en", "DISK_QUOTA": "unlimited",
		"BANDWIDTH": "unlimited", "WEB_DOMAINS": "1",
	}
	for k, v := range fields {
		base[k] = v
	}
	out, err := json.Marshal(map[string]any{name: base})
	if err != nil {
		t.Fatal(err)
	}
	return []fixtureEntry{{Args: []string{name, "json"}, Stdout: string(out)}}
}

func TestDNSListAdmin(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r DNSZones
	if code := h.call("GET", "/api/v1/dns", nil, &r); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(r.Zones) != 2 || r.Zones[0].Name != "example.com" || r.Zones[1].Name != "example.org" {
		t.Fatalf("zones = %+v", r.Zones)
	}
	z := r.Zones[1]
	if z.IP != "203.0.113.10" || z.TPL != "child-ns" || z.TTL != "3600" || z.EXP != "2027-03-01" ||
		z.DNSSEC != "yes" || z.SUSPENDED != "yes" || z.SOA != "ns1.example.org" || z.SERIAL == "" || z.DATE == "" {
		t.Fatalf("zone fields = %+v", z)
	}
}

func TestDNSListWhileLookingUsesLookedAtUser(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	var r DNSZones
	if code := h.call("GET", "/api/v1/dns", nil, &r); code != 200 || len(r.Zones) != 1 || r.Zones[0].Name != "bakery.example" {
		t.Fatalf("%d %+v", code, r)
	}
	if calls := h.dnsCalls(); len(calls) != 1 || calls[0] != "v-list-dns-domains bob json" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestDNSZone(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var z struct{ NAME, IP string }
	if code := h.call("GET", "/api/v1/dns/example.com", nil, &z); code != 200 || z.NAME != "example.com" || z.IP != "203.0.113.10" {
		t.Fatalf("%d %+v", code, z)
	}
}

func TestDNSRecords(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r DNSRecords
	if code := h.call("GET", "/api/v1/dns/example.com/records", nil, &r); code != 200 {
		t.Fatalf("code %d", code)
	}
	if r.Zone.Name != "example.com" || len(r.Records) == 0 || r.Records[0].ID != "1" || r.Records[0].TYPE != "NS" {
		t.Fatalf("%+v", r)
	}
}

func TestDNSZoneNotFound(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, p := range []string{"", "/records", "/dnssec"} {
		var e APIError
		code := h.call("GET", "/api/v1/dns/missing.example"+p, nil, &e)
		if code != 404 || e.Error.Code != "not_found" || e.Error.Message != "Error: dns domain missing.example doesn't exist" {
			t.Errorf("%q: %d %+v", p, code, e)
		}
	}
}

func TestDNSBadDomainParam(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, p := range []string{"-h", "-h/records", "-h/dnssec"} {
		var e APIError
		if code := h.call("GET", "/api/v1/dns/"+p, nil, &e); code != 400 || e.Error.Code != "invalid" {
			t.Errorf("%s: %d %+v", p, code, e)
		}
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("commands ran: %v", calls)
	}
}

func TestDNSOptionsForAdmin(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"DNS_CLUSTER_SYSTEM": "hestia-zone", "SUPPORT_DNSSEC": "yes"}})
	h.loginAdmin()
	var o DNSOptions
	if code := h.call("GET", "/api/v1/dns/options", nil, &o); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(o.Templates) != 6 || o.Templates[0] != "default" || o.Templates[1] != "child-ns" {
		t.Errorf("templates = %v", o.Templates)
	}
	if len(o.IPs) == 0 || o.IPs[0] != "203.0.113.10" {
		t.Errorf("ips = %v", o.IPs)
	}
	if len(o.Nameservers) != 2 || o.Nameservers[0] != "ns1.example.com" {
		t.Errorf("ns = %v", o.Nameservers)
	}
	if o.DefaultTTL != "14400" || o.DNSSystem != "BIND9" || !o.CanEditTemplates || !o.SupportsDNSSEC || !o.AdminGate {
		t.Errorf("options = %+v", o)
	}
}

func TestDNSOptionsDefaultExpiry(t *testing.T) {
	h2 := newHarnessClock(t, time.Date(2026, 2, 28, 23, 0, 0, 0, time.UTC))
	h2.loginAdmin()
	var o DNSOptions
	h2.call("GET", "/api/v1/dns/options", nil, &o)
	if o.DefaultExpiry != "2027-02-28" {
		t.Fatalf("expiry = %q", o.DefaultExpiry)
	}
}

func TestDNSOptionsForUser(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_EDIT_DNS_TEMPLATES": "no"}})
	h.loginBob()
	var o DNSOptions
	if code := h.call("GET", "/api/v1/dns/options", nil, &o); code != 200 {
		t.Fatalf("code %d", code)
	}
	if o.CanEditTemplates || o.AdminGate || o.SupportsDNSSEC {
		t.Fatalf("options = %+v", o)
	}
	// Admin looking at bob keeps the admin's template right but not the admin gate.
	h = newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_EDIT_DNS_TEMPLATES": "no"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	h.call("GET", "/api/v1/dns/options", nil, &o)
	if !o.CanEditTemplates || o.AdminGate {
		t.Fatalf("login-as options = %+v", o)
	}
	// With the policy on, a plain user may edit templates.
	h = newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_EDIT_DNS_TEMPLATES": "yes"}})
	h.loginBob()
	h.call("GET", "/api/v1/dns/options", nil, &o)
	if !o.CanEditTemplates {
		t.Fatalf("policy yes: %+v", o)
	}
}

func TestDNSOptionsDefaultTemplate(t *testing.T) {
	h := newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user": append(userFixture(t, "admin", map[string]string{"ROLE": "admin", "DNS_TEMPLATE": "gmail"}),
			userFixture(t, "bob", nil)...),
	}})
	h.loginAdmin()
	var o DNSOptions
	h.call("GET", "/api/v1/dns/options", nil, &o)
	if o.DefaultTemplate != "gmail" {
		t.Fatalf("defaultTemplate = %q", o.DefaultTemplate)
	}
}

func TestDNSOptionsRouteBeatsDomainWildcard(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.call("GET", "/api/v1/dns/options", nil, nil)
	for _, c := range h.dnsCalls() {
		if strings.HasPrefix(c, "v-list-dns-domain ") {
			t.Fatalf("options handled as a zone: %v", h.dnsCalls())
		}
	}
}

func TestDNSSECLabels(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var v DNSSECView
	if code := h.call("GET", "/api/v1/dns/example.org/dnssec", nil, &v); code != 200 {
		t.Fatalf("code %d", code)
	}
	if v.Zone.Name != "example.org" || v.AlgorithmLabel != "13 - ECDSAP256/SHA256" || v.FlagLabel != "KSK (257)" || v.Key.KEYTAG != "12345" {
		t.Fatalf("%+v", v)
	}
	h.call("GET", "/api/v1/dns/example.com/dnssec", nil, &v)
	if v.AlgorithmLabel != "Unknown" || v.FlagLabel != "" || v.Key.KEY != "" {
		t.Fatalf("empty key: %+v", v)
	}
}

func TestDNSSECLabelTables(t *testing.T) {
	algs := map[string]string{"3": "3 - DSA", "5": "5 - RSA/SHA1", "6": "6 - DSA-NSEC3-SHA1", "7": "7 - RSA/SHA1-NSEC3-SHA1",
		"8": "8 - RSA/SHA256", "10": "10 - RSA/SHA512", "12": "12 - ECC-GOST", "13": "13 - ECDSAP256/SHA256",
		"14": "14 - ECDSAP384/SHA384", "15": "15 - ED25519/SHA512", "16": "16 - ED448/SHA912", "": "Unknown", "99": "Unknown"}
	for in, want := range algs {
		if got := algorithmLabel(in); got != want {
			t.Errorf("algorithm %q = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"257": "KSK (257)", "256": "ZSK (256)", "": "", "1": ""} {
		if got := flagLabel(in); got != want {
			t.Errorf("flag %q = %q", in, got)
		}
	}
}

func TestDNSRequiresAuth(t *testing.T) {
	h := newHarness(t)
	h.bootstrap()
	for _, p := range []string{"/api/v1/dns", "/api/v1/dns/example.com", "/api/v1/dns/example.com/records", "/api/v1/dns/example.com/dnssec", "/api/v1/dns/options"} {
		if code := h.call("GET", p, nil, nil); code != 401 {
			t.Errorf("%s: %d", p, code)
		}
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("commands ran: %v", calls)
	}
}

func TestBootstrapFeatures(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if b := h.bootstrap(); b.User == nil || !b.User.Features.DNS || b.User.SortOrder != "name" {
		t.Fatalf("admin: %+v", b.User)
	}

	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user": append(userFixture(t, "admin", map[string]string{"ROLE": "admin", "DNS_DOMAINS": "0", "PREF_UI_SORT": "date"}),
			userFixture(t, "bob", nil)...),
	}})
	h.loginAdmin()
	if b := h.bootstrap(); b.User.Features.DNS || b.User.SortOrder != "date" {
		t.Fatalf("package without DNS: %+v", b.User)
	}

	// The looked-at user's package decides, not the admin's.
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "bob"}, nil)
	if b := h.bootstrap(); !b.User.Features.DNS {
		t.Fatalf("looking at bob: %+v", b.User)
	}

	h = newHarnessWith(t, overrides{Config: map[string]string{"DNS_SYSTEM": ""}})
	h.loginAdmin()
	if b := h.bootstrap(); b.User.Features.DNS {
		t.Fatalf("no DNS system: %+v", b.User)
	}
}

// fixedClock pins the server clock to 2026-10-17 (default expiry 2027-10-17).
func fixedClock(d *Deps) {
	d.Now = func() time.Time { return time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC) }
}

// newHarnessClock runs the dev fixtures with the server clock fixed at now.
func newHarnessClock(t *testing.T, now time.Time) *harness {
	t.Helper()
	return newHarnessFrom(t, filepath.Join("..", "..", "testdata", "fixtures", "dev"), "",
		func(d *Deps) { d.Now = func() time.Time { return now } })
}

// cmdLine is one expected command as dnsCalls prints it.
func cmdLine(cmd string, args ...string) string {
	return cmd + " " + strings.Join(args, " ")
}

func assertCalls(t *testing.T, h *harness, want ...string) {
	t.Helper()
	got := h.dnsCalls()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%q\nwant:\n%q", got, want)
	}
}

func addReq(domain string) AddZoneRequest {
	return AddZoneRequest{Domain: domain, IP: "203.0.113.24", Nameservers: []string{"ns1", "ns2"},
		Exp: "2027-10-17", TTL: "14400"}
}

func failFixture(cmd string, exit int, out string) overrides {
	return overrides{Fixtures: map[string][]fixtureEntry{cmd: {{Args: []string{"*", "*", "*", "*"}, Stdout: out, Exit: exit}}}}
}

func TestAddZoneMinimal(t *testing.T) {
	h := newHarnessClock(t, time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC))
	h.loginAdmin()
	var r OK
	if code := h.call("POST", "/api/v1/dns", addReq("example.net"), &r); code != 200 || r.Status != "ok" {
		t.Fatalf("got %d %+v", code, r)
	}
	assertCalls(t, h,
		cmdLine("v-add-dns-domain", "admin", "example.net", "203.0.113.24", "ns1", "ns2", "", "", "", "", "", "", "no", "no"),
		cmdLine("v-restart-dns"))
}

func TestAddZoneStripsWWWAndLowercases(t *testing.T) {
	for in, want := range map[string]string{"WWW.Example.NET": "example.net", "wwwfoo.com": "wwwfoo.com"} {
		h := newHarnessClock(t, time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC))
		h.loginAdmin()
		if code := h.call("POST", "/api/v1/dns", addReq(in), nil); code != 200 {
			t.Fatalf("%s: %d", in, code)
		}
		if got := h.dnsCalls()[0]; !strings.HasPrefix(got, "v-add-dns-domain admin "+want+" ") {
			t.Fatalf("%s: %q", in, got)
		}
	}
}

func TestAddZoneChangedDefaults(t *testing.T) {
	h := newHarnessClock(t, time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC))
	h.loginAdmin()
	req := addReq("example.net")
	req.DNSSEC = true
	req.TTL, req.Exp, req.Template = "3600", "2030-01-01", "gmail"
	if code := h.call("POST", "/api/v1/dns", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h,
		cmdLine("v-add-dns-domain", "admin", "example.net", "203.0.113.24", "ns1", "ns2", "", "", "", "", "", "", "no", "yes"),
		cmdLine("v-change-dns-domain-tpl", "admin", "example.net", "gmail", "no"),
		cmdLine("v-change-dns-domain-exp", "admin", "example.net", "2030-01-01", "no"),
		cmdLine("v-change-dns-domain-ttl", "admin", "example.net", "3600", "no"),
		cmdLine("v-restart-dns"))
}

func TestAddZonePartialFailure(t *testing.T) {
	o := failFixture("v-change-dns-domain-ttl", 2, "Error: invalid ttl format :: x\n")
	h := newHarnessWith(t, o, fixedClock)
	h.loginAdmin()
	req := addReq("example.net")
	req.TTL = "x"
	var r statusReply
	if code := h.call("POST", "/api/v1/dns", req, &r); code != 400 || r.Error.Code != "invalid" || r.Error.Message != "Error: invalid ttl format :: x" {
		t.Fatalf("got %d %+v", code, r)
	}
	assertCalls(t, h,
		cmdLine("v-add-dns-domain", "admin", "example.net", "203.0.113.24", "ns1", "ns2", "", "", "", "", "", "", "no", "no"),
		cmdLine("v-change-dns-domain-ttl", "admin", "example.net", "x", "no"))
}

func TestAddZoneBlankFields(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r struct{ Error ErrorBody }
	if code := h.call("POST", "/api/v1/dns", AddZoneRequest{}, &r); code != 400 || r.Error.Code != "invalid" ||
		r.Error.Message != `Field "%s" can not be blank.` || strings.Join(r.Error.Args, "|") != "Domain|IP Address" {
		t.Fatalf("got %d %+v", code, r)
	}
	h.call("POST", "/api/v1/dns", AddZoneRequest{Domain: "a.net"}, &r)
	if strings.Join(r.Error.Args, "|") != "IP Address" {
		t.Fatalf("args = %v", r.Error.Args)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestAddZoneTooManyNameservers(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := addReq("example.net")
	req.Nameservers = make([]string, 9)
	if code := h.call("POST", "/api/v1/dns", req, nil); code != 400 || len(h.dnsCalls()) != 0 {
		t.Fatalf("got %d, calls %q", code, h.dnsCalls())
	}
}

func TestAddZoneTemplateIgnoredWithoutPermission(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_EDIT_DNS_TEMPLATES": "no"}}, fixedClock)
	h.loginBob()
	req := addReq("example.net")
	req.Template = "gmail"
	if code := h.call("POST", "/api/v1/dns", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h,
		cmdLine("v-add-dns-domain", "bob", "example.net", "203.0.113.24", "ns1", "ns2", "", "", "", "", "", "", "no", "no"),
		cmdLine("v-restart-dns"))
}

func editReq() EditZoneRequest {
	return EditZoneRequest{IP: "203.0.113.10", Template: "default", SOA: "ns1.example.com", Exp: "2027-10-17", TTL: "14400"}
}

const getZone = "v-list-dns-domain admin example.com json"

func TestEditZoneOnlyChangedFields(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := editReq()
	req.TTL = "300"
	if code := h.call("PUT", "/api/v1/dns/example.com", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, getZone,
		cmdLine("v-change-dns-domain-ttl", "admin", "example.com", "300", "no"),
		cmdLine("v-restart-dns"))
}

func TestEditZoneAllFieldsOrder(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	on := true
	req := EditZoneRequest{IP: "1.2.3.4", Template: "gmail", SOA: "ns9.example.com", Exp: "2030-01-01", TTL: "300", DNSSEC: &on}
	if code := h.call("PUT", "/api/v1/dns/example.com", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, getZone,
		cmdLine("v-change-dns-domain-ip", "admin", "example.com", "1.2.3.4", "no"),
		cmdLine("v-change-dns-domain-tpl", "admin", "example.com", "gmail", "no"),
		cmdLine("v-change-dns-domain-soa", "admin", "example.com", "ns9.example.com", "no"),
		cmdLine("v-change-dns-domain-exp", "admin", "example.com", "2030-01-01", "no"),
		cmdLine("v-change-dns-domain-ttl", "admin", "example.com", "300", "no"),
		cmdLine("v-change-dns-domain-dnssec", "admin", "example.com", "yes"),
		cmdLine("v-restart-dns"))
}

func TestEditZoneDnssecAbsentUntouched(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := EditZoneRequest{IP: "203.0.113.10", Template: "child-ns", SOA: "ns1.example.org", Exp: "2027-03-01", TTL: "3600"}
	if code := h.call("PUT", "/api/v1/dns/example.org", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, "v-list-dns-domain admin example.org json")
}

func TestEditZoneDnssecDisable(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	off := false
	req := EditZoneRequest{IP: "203.0.113.10", Template: "child-ns", SOA: "ns1.example.org", Exp: "2027-03-01", TTL: "3600", DNSSEC: &off}
	h.call("PUT", "/api/v1/dns/example.org", req, nil)
	assertCalls(t, h, "v-list-dns-domain admin example.org json",
		cmdLine("v-change-dns-domain-dnssec", "admin", "example.org", "no"),
		cmdLine("v-restart-dns"))
}

func TestEditZoneExpiryOnlyNoRestart(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := editReq()
	req.Exp = "2031-01-01"
	if code := h.call("PUT", "/api/v1/dns/example.com", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, getZone, cmdLine("v-change-dns-domain-exp", "admin", "example.com", "2031-01-01", "no"))
}

func TestEditZoneTemplateIgnoredWithoutPermission(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_USER_EDIT_DNS_TEMPLATES": "no"}})
	h.loginBob()
	req := EditZoneRequest{IP: "10.0.0.5", Template: "gmail", SOA: "ns1.example.com", Exp: "2027-01-20", TTL: "14400"}
	if code := h.call("PUT", "/api/v1/dns/bakery.example", req, nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	assertCalls(t, h, "v-list-dns-domain bob bakery.example json")
}

func TestEditZoneStopsAtFirstFailure(t *testing.T) {
	h := newHarnessWith(t, failFixture("v-change-dns-domain-ip", 2, "Error: invalid ip :: x\n"))
	h.loginAdmin()
	req := editReq()
	req.IP, req.TTL = "x", "300"
	var r statusReply
	if code := h.call("PUT", "/api/v1/dns/example.com", req, &r); code != 400 || r.Error.Message != "Error: invalid ip :: x" {
		t.Fatalf("got %d %+v", code, r)
	}
	assertCalls(t, h, getZone, cmdLine("v-change-dns-domain-ip", "admin", "example.com", "x", "no"))
}

func TestEditZoneMissing(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var r statusReply
	if code := h.call("PUT", "/api/v1/dns/missing.example", editReq(), &r); code != 404 {
		t.Fatalf("got %d %+v", code, r)
	}
}

func TestZoneWritesReadOnly(t *testing.T) {
	h := newHarnessWith(t, overrides{Config: map[string]string{"POLICY_SYSTEM_PROTECTED_ADMIN": "yes", "ROOT_USER": "admin"}})
	h.loginAdmin()
	h.call("POST", "/api/v1/auth/login-as", LoginAsRequest{User: "admin"}, nil)
	for _, rt := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/dns", addReq("example.net")},
		{"PUT", "/api/v1/dns/example.com", editReq()},
		{"DELETE", "/api/v1/dns/example.com", nil},
		{"POST", "/api/v1/dns/example.com/suspend", nil},
		{"POST", "/api/v1/dns/example.com/unsuspend", nil},
	} {
		var r statusReply
		if code := h.call(rt.method, rt.path, rt.body, &r); code != 403 || r.Error.Code != "read_only" {
			t.Fatalf("%s %s: %d %+v", rt.method, rt.path, code, r)
		}
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestZoneWritesNeedCSRF(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	h.token = ""
	var r statusReply
	if code := h.call("POST", "/api/v1/dns", addReq("example.net"), &r); code != 400 || r.Error.Code != "csrf" {
		t.Fatalf("got %d %+v", code, r)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestSuspendUnsuspendDelete(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, c := range []struct{ method, path, cmd string }{
		{"POST", "/api/v1/dns/example.com/suspend", "v-suspend-dns-domain"},
		{"POST", "/api/v1/dns/example.com/unsuspend", "v-unsuspend-dns-domain"},
		{"DELETE", "/api/v1/dns/example.com", "v-delete-dns-domain"},
	} {
		var r OK
		if code := h.call(c.method, c.path, nil, &r); code != 200 || r.Status != "ok" {
			t.Fatalf("%s: %d", c.path, code)
		}
	}
	assertCalls(t, h,
		"v-suspend-dns-domain admin example.com",
		"v-unsuspend-dns-domain admin example.com",
		"v-delete-dns-domain admin example.com")

	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-suspend-dns-domain": {{Args: []string{"*", "*"}, Stdout: "Error: dns example.com is suspended\n", Exit: 5}},
	}})
	h.loginAdmin()
	var r statusReply
	if code := h.call("POST", "/api/v1/dns/example.com/suspend", nil, &r); code != 409 || r.Error.Code != "suspended" {
		t.Fatalf("got %d %+v", code, r)
	}
}

func TestZoneWritesBadDomain(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	if code := h.call("DELETE", "/api/v1/dns/-bad", nil, nil); code != 400 || len(h.dnsCalls()) != 0 {
		t.Fatalf("got %d", code)
	}
}

func TestDNSListUsage(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	var z DNSZones
	h.call("GET", "/api/v1/dns", nil, &z)
	want := DNSUsage{Zones: "2", ZonesLimit: "unlimited", Suspended: "1", Records: "13", RecordsLimit: "unlimited"}
	if z.Usage != want {
		t.Fatalf("unlimited: %+v", z.Usage)
	}

	h = newHarnessWith(t, overrides{Fixtures: map[string][]fixtureEntry{
		"v-list-user": append(userFixture(t, "admin", map[string]string{
			"ROLE": "admin", "U_DNS_DOMAINS": "2", "DNS_DOMAINS": "5", "U_DNS_RECORDS": "13", "DNS_RECORDS": "50", "SUSPENDED_DNS": "1",
		}), userFixture(t, "bob", nil)...),
	}})
	h.loginAdmin()
	h.call("GET", "/api/v1/dns", nil, &z)
	want = DNSUsage{Zones: "2", ZonesLimit: "5", Suspended: "1", Records: "13", RecordsLimit: "250"}
	if z.Usage != want {
		t.Fatalf("numeric: %+v", z.Usage)
	}
}

func TestEditZoneBlankFields(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	req := editReq()
	req.IP = "  "
	var r struct{ Error ErrorBody }
	if code := h.call("PUT", "/api/v1/dns/example.com", req, &r); code != 400 || r.Error.Code != "invalid" ||
		r.Error.Message != `Field "%s" can not be blank.` || strings.Join(r.Error.Args, "|") != "IP Address" {
		t.Fatalf("got %d %+v", code, r)
	}
	r = struct{ Error ErrorBody }{}
	if code := h.call("PUT", "/api/v1/dns/example.com", map[string]any{}, &r); code != 400 ||
		strings.Join(r.Error.Args, "|") != "IP Address|SOA|Expiration Date|TTL" {
		t.Fatalf("empty body: %d %+v", code, r)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

func TestAddZoneTrimsDomain(t *testing.T) {
	h := newHarnessWith(t, overrides{}, fixedClock)
	h.loginAdmin()
	if code := h.call("POST", "/api/v1/dns", addReq(" example.net \t"), nil); code != 200 {
		t.Fatalf("got %d", code)
	}
	if got := h.dnsCalls()[0]; !strings.HasPrefix(got, "v-add-dns-domain admin example.net 203.0.113.24 ") {
		t.Fatalf("call = %q", got)
	}
}

func TestAddZoneInvalidDomainMessage(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for in, want := range map[string]string{
		"www.":     "Error: invalid domain format :: www.",
		" WWW. ":   "Error: invalid domain format :: WWW.",
		"-Bad.com": "Error: invalid domain format :: -bad.com",
	} {
		var r statusReply
		if code := h.call("POST", "/api/v1/dns", addReq(in), &r); code != 400 || r.Error.Code != "invalid" || r.Error.Message != want {
			t.Fatalf("%q: %d %+v", in, code, r)
		}
	}
	var r struct{ Error ErrorBody }
	if code := h.call("POST", "/api/v1/dns", addReq("   "), &r); code != 400 || strings.Join(r.Error.Args, "|") != "Domain" {
		t.Fatalf("blank: %d %+v", code, r)
	}
	if calls := h.dnsCalls(); len(calls) != 0 {
		t.Fatalf("calls = %q", calls)
	}
}

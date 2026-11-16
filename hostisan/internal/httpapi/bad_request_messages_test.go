package httpapi

import (
	"reflect"
	"testing"
)

// C7 ST3: every refused Configure Server value names the field and the value.
func TestSysSettingsInvalidValuesNameTheField(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  SettingsRequest
		msg  string
		args []string
	}{
		{"hostname", SettingsRequest{Hostname: sysP("bad_host")}, invalidFormatMsg, []string{"hostname", "bad_host"}},
		{"timezone", SettingsRequest{Timezone: sysP("Mars/Base")}, invalidFormatMsg, []string{"timezone", "Mars/Base"}},
		{"language", SettingsRequest{Language: sysP("zz")}, invalidFormatMsg, []string{"language", "zz"}},
		{"theme", SettingsRequest{Theme: sysP("nope")}, invalidFormatMsg, []string{"theme", "nope"}},
		{"webmail alias", SettingsRequest{WebmailAlias: sysP("a--b")}, invalidFormatMsg, []string{"Webmail Alias", "a--b"}},
		{"backup mode", SettingsRequest{BackupMode: sysP("lzma")}, invalidFormatMsg, []string{"BACKUP_MODE", "lzma"}},
		{"backup level", SettingsRequest{BackupLevel: sysP("42")}, invalidFormatMsg, []string{"BACKUP_GZIP", "42"}},
		{"login style", SettingsRequest{LoginStyle: sysP("fancy")}, invalidFormatMsg, []string{"LOGIN_STYLE", "fancy"}},
		{"leading dash", SettingsRequest{Hostname: sysP("-x.example.com")}, invalidValueMsg, []string{"-x.example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, srv, sys := sysSettingsHarness(t, sysSettingsOpts{})
			r := sysSettingsSave(t, h, srv, sys, tc.req)
			if r.code != 400 || r.err.Error.Code != "invalid" || r.err.Error.Message != tc.msg ||
				!reflect.DeepEqual(r.err.Error.Args, tc.args) {
				t.Fatalf("got %d %+v", r.code, r.err)
			}
		})
	}
}

// C7 U5: add user names the refused username, role or language.
func TestAddUserInvalidValuesNameTheField(t *testing.T) {
	for _, tc := range []struct {
		edit func(*AddUserRequest)
		args []string
	}{
		{func(r *AddUserRequest) { r.Username = "bad name!" }, []string{"user", "bad name!"}},
		{func(r *AddUserRequest) { r.Role = "root" }, []string{"role", "root"}},
		{func(r *AddUserRequest) { r.Language = "zz" }, []string{"language", "zz"}},
		{func(r *AddUserRequest) { r.Package = "-x" }, []string{"package", "-x"}},
	} {
		h := newHarness(t)
		h.loginAdmin()
		req := goodAddUser()
		tc.edit(&req)
		var e APIError
		if code := h.call("POST", "/api/v1/users", req, &e); code != 400 || e.Error.Message != invalidFormatMsg ||
			!reflect.DeepEqual(e.Error.Args, tc.args) {
			t.Fatalf("%v: %d %+v", tc.args, code, e)
		}
	}
	h := newHarness(t)
	h.loginAdmin()
	req := goodAddUser()
	req.Name = `Bo "B"`
	var e APIError
	if code := h.call("POST", "/api/v1/users", req, &e); code != 400 || e.Error.Message != invalidNameQuotesMsg ||
		!reflect.DeepEqual(e.Error.Args, []string{`Bo "B"`}) {
		t.Fatalf("quotes: %d %+v", code, e)
	}
}

// C7 U5/U8: edit user names the refused shell, role, language, theme or sort order.
func TestEditUserInvalidValuesNameTheField(t *testing.T) {
	for _, tc := range []struct {
		edit func(*UserEditRequest)
		args []string
	}{
		{func(r *UserEditRequest) { r.Shell = ptr("/bin/nope") }, []string{"shell", "/bin/nope"}},
		{func(r *UserEditRequest) { r.Role = ptr("root") }, []string{"role", "root"}},
		{func(r *UserEditRequest) { r.Language = "zz" }, []string{"language", "zz"}},
		{func(r *UserEditRequest) { r.SortOrder = "size" }, []string{"sort_order", "size"}},
		{func(r *UserEditRequest) { r.PHPCli = ptr("x") }, []string{"php version", "x"}},
	} {
		h := newHarness(t)
		h.loginAdmin()
		req := profSame()
		tc.edit(&req)
		var e APIError
		if code := h.call("PUT", "/api/v1/users/bob", req, &e); code != 400 || e.Error.Message != invalidFormatMsg ||
			!reflect.DeepEqual(e.Error.Args, tc.args) {
			t.Fatalf("%v: %d %+v", tc.args, code, e)
		}
	}
}

// C7 web 307: a redirect code other than 301/302 reads "invalid code format :: 307".
func TestWebEditRedirectCodeNamed(t *testing.T) {
	h := webEditHarness(t, overrides{})
	h.loginAdmin()
	req := webEditSame()
	req.Redirect, req.RedirectTarget, req.RedirectCode = true, "example.org", "307"
	var e APIError
	if code := h.call("PUT", "/api/v1/web/example.com", req, &e); code != 400 || e.Error.Message != invalidFormatMsg ||
		!reflect.DeepEqual(e.Error.Args, []string{"code", "307"}) {
		t.Fatalf("got %d %+v", code, e)
	}
	webEditLog(t, h.webCalls())
}

// Runtime half of TestNoEmptyBadRequestMessage: representative bad inputs across
// the routes all answer 400 with a message the UI can show.
func TestBadRequestsCarryAMessage(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/users/bad%20name!", nil},
		{"GET", "/api/v1/packages/-x", nil},
		{"GET", "/api/v1/dns/bad%20domain!", nil},
		{"GET", "/api/v1/db/-x", nil},
		{"POST", "/api/v1/users/bulk", map[string]any{"action": "explode", "names": []string{"bob"}}},
		{"POST", "/api/v1/users/bulk", map[string]any{"action": "suspend", "names": []string{}}},
		{"POST", "/api/v1/users/bulk", map[string]any{"action": "suspend", "names": []string{"bad name!"}}},
		{"POST", "/api/v1/packages/bulk", map[string]any{"action": "delete", "names": []string{"default"}}},
		{"DELETE", "/api/v1/packages/system", nil},
		{"POST", "/api/v1/web/bulk", map[string]any{"action": "explode", "names": []string{"example.com"}}},
		{"POST", "/api/v1/web/bulk", map[string]any{"action": "delete", "names": []string{"-x"}}},
		{"GET", "/api/v1/search?q=web&u=bad%20name!", nil},
		{"GET", "/api/v1/stats?user=bad%20name!", nil},
		{"POST", "/api/v1/users", "not an object"},
	} {
		var e APIError
		code := h.call(tc.method, tc.path, tc.body, &e)
		if code != 400 || e.Error.Message == "" {
			t.Errorf("%s %s: %d %+v", tc.method, tc.path, code, e)
		}
	}
}

package httpapi

// Updates and white label (Slice C, Task 4).

import (
	"net/http"
	"strings"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
)

func (sys *sysAPI) updatesRoutes(m *sysMux) {
	m.handle("GET /api/v1/server/updates", sys.read(sys.updatesView))
	m.handle("POST /api/v1/server/updates/autoupdate", sys.write(sys.setAutoupdate))
	m.handle("GET /api/v1/server/white-label", sys.read(sys.whiteLabelView))
	m.handle("PUT /api/v1/server/white-label", sys.write(sys.whiteLabelSave))
}

// autoupdate reads v-list-sys-hestia-autoupdate plain.
func (sys *sysAPI) autoupdate(w http.ResponseWriter, r *http.Request) (on, ok bool) {
	out, ok := sys.output(w, r, "v-list-sys-hestia-autoupdate", "plain")
	if !ok {
		return false, false
	}
	on, err := hestia.ParseAutoupdate(out)
	if err != nil {
		serverError(w, err)
		return false, false
	}
	return on, true
}

// updatesView is GET /api/v1/server/updates (decision 46: there is no update now).
func (sys *sysAPI) updatesView(w http.ResponseWriter, r *http.Request) {
	out, ok := sys.output(w, r, "v-list-sys-hestia-updates", "json")
	if !ok {
		return
	}
	list, err := hestia.DecodeObjects[hestia.HestiaUpdate](out)
	if err != nil {
		serverError(w, err)
		return
	}
	on, ok := sys.autoupdate(w, r)
	if !ok {
		return
	}
	pkgs := make([]UpdatePackage, 0, len(list))
	for _, u := range list {
		pkgs = append(pkgs, UpdatePackage{
			Name: u.NAME, DisplayName: brand.PackageName(u.NAME), Description: brand.Rewrite(u.DESCR),
			Version: u.VERSION, Arch: u.ARCH, Updated: u.UPDATED == "yes",
		})
	}
	writeJSON(w, http.StatusOK, UpdatesView{Packages: pkgs, Autoupdate: on})
}

// setAutoupdate is POST /api/v1/server/updates/autoupdate: it runs a command only
// when the setting changes, and shows its error.
func (sys *sysAPI) setAutoupdate(w http.ResponseWriter, r *http.Request) {
	var req AutoupdateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	cur, ok := sys.autoupdate(w, r)
	if !ok {
		return
	}
	if cur != req.Enabled {
		cmd := "v-delete-cron-hestia-autoupdate"
		if req.Enabled {
			cmd = "v-add-cron-hestia-autoupdate" // no arguments: apt mode, as PHP did
		}
		if err := sys.run(runner.WithTimeout(r.Context(), sysTimeoutDefault), cmd); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// whiteLabelView is GET /api/v1/server/white-label (decision 47).
func (sys *sysAPI) whiteLabelView(w http.ResponseWriter, r *http.Request) {
	cfg, err := sys.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	// A failed v-list-sys-info is ignored: the form's hostname hint is then empty.
	var host string
	if res, err := sys.R.Run(r.Context(), "v-list-sys-info", "json"); err == nil && res.Err("v-list-sys-info") == nil {
		if info, err := hestia.DecodeSysInfo(res.Stdout); err == nil {
			host = info.HOSTNAME
		}
	}
	writeJSON(w, http.StatusOK, WhiteLabelView{
		AppName: brand.Rewrite(cfg["APP_NAME"]), Title: cfg["TITLE"], FromName: cfg["FROM_NAME"],
		FromEmail: cfg["FROM_EMAIL"], SubjectEmail: cfg["SUBJECT_EMAIL"],
		HideDocs: cfg["HIDE_DOCS"] == "yes", Hostname: host,
	})
}

// whiteLabelSave is PUT /api/v1/server/white-label: changed fields in PHP order
// (APP_NAME, TITLE, SUBJECT_EMAIL, HIDE_DOCS, FROM_NAME, FROM_EMAIL), then the logo
// when asked; it stops at the first error. There is no reset-logo path (decision 48).
func (sys *sysAPI) whiteLabelSave(w http.ResponseWriter, r *http.Request) {
	var req WhiteLabelRequest
	if !decodeBody(w, r, &req) {
		return
	}
	// Values are trimmed before they are compared, checked and saved.
	req.AppName, req.Title = strings.TrimSpace(req.AppName), strings.TrimSpace(req.Title)
	req.FromName, req.FromEmail = strings.TrimSpace(req.FromName), strings.TrimSpace(req.FromEmail)
	req.SubjectEmail = strings.TrimSpace(req.SubjectEmail)
	if !blankFields(w, "Application Name", req.AppName) {
		return
	}
	for _, v := range []string{req.AppName, req.Title, req.FromName, req.FromEmail, req.SubjectEmail} {
		if !hestia.ValidConfigValue(v) {
			WriteError(w, http.StatusBadRequest, "invalid", sysForbiddenCharsMsg)
			return
		}
	}
	cfg, err := sys.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	hide := "no"
	if req.HideDocs {
		hide = "yes"
	}
	hideNow := "no"
	if cfg["HIDE_DOCS"] == "yes" {
		hideNow = "yes"
	}
	fields := []struct{ key, want, have string }{
		{"APP_NAME", req.AppName, brand.Rewrite(cfg["APP_NAME"])},
		{"TITLE", req.Title, cfg["TITLE"]},
		{"SUBJECT_EMAIL", req.SubjectEmail, cfg["SUBJECT_EMAIL"]},
		{"HIDE_DOCS", hide, hideNow},
		{"FROM_NAME", req.FromName, cfg["FROM_NAME"]},
		{"FROM_EMAIL", req.FromEmail, cfg["FROM_EMAIL"]},
	}
	ctx := runner.WithTimeout(r.Context(), sysTimeoutDefault)
	changed := false
	var failed error
	for _, f := range fields {
		if f.want == f.have {
			continue
		}
		changed = true
		if failed = sys.run(ctx, "v-change-sys-config-value", f.key, f.want); failed != nil {
			break
		}
	}
	if failed == nil && req.UpdateLogo {
		changed = true
		failed = sys.run(ctx, "v-update-white-label-logo")
	}
	if changed {
		sys.H.InvalidateConfig()
	}
	if failed != nil {
		writeCommandError(w, failed)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

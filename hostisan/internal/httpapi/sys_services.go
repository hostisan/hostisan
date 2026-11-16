package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

// servicesRoutes mounts the services, reboot and config editor routes (Slice A).
func (sys *sysAPI) servicesRoutes(m *sysMux) {
	m.handle("GET /api/v1/server/services", sys.read(sys.servicesList))
	m.handle("POST /api/v1/server/services/bulk", sys.write(sys.servicesBulk))
	m.handle("POST /api/v1/server/services/{service}/{action}", sys.write(sys.serviceAction))
	m.handle("POST /api/v1/server/reboot", sys.write(sys.reboot))
	m.handle("GET /api/v1/server/editors/{service}", sys.read(sys.editorRead))
	m.handle("PUT /api/v1/server/editors/{service}", sys.write(sys.editorSave))
}

// sysServiceName is Hestia's service name rule without the `|` its class lets through.
var sysServiceName = regexp.MustCompile(`^[A-Za-z0-9][-.A-Za-z0-9_]{0,64}$`)

// sysServiceAction holds an action's msgids: its step on one service ("Start
// nginx"), the title of a bulk job, and a start or stop that left the wrong state.
type sysServiceAction struct {
	step         StepMsgid
	bulk, failed string
}

var sysServiceActions = map[string]sysServiceAction{
	"start":   {StepStartMsgid, sysStartAllMsg, sysStartFailedMsg},
	"stop":    {StepStopMsgid, sysStopAllMsg, sysStopFailedMsg},
	"restart": {StepRestartMsgid, sysRestartAllMsg, sysRestartFailedMsg},
}

const (
	sysStartAllMsg      = "Start services"
	sysStopAllMsg       = "Stop services"
	sysRestartAllMsg    = "Restart services"
	sysStartFailedMsg   = `Start "%s" failed`
	sysStopFailedMsg    = `Stop "%s" failed`
	sysRestartFailedMsg = `Restart "%s" failed`
)

const (
	sysBulkStopMsg = `Stop "%s" from its own row.`
	sysConfirmMsg  = "Type the service name to confirm."
	sysUnknownMsg  = "Unknown service."
)

// sysServiceUnit is the unit a row acts on: the web-terminal row's buttons act on
// hestia-web-terminal (decision 5).
func sysServiceUnit(name string) string {
	if name == "web-terminal" {
		return "hestia-web-terminal"
	}
	return name
}

// sysNeedsTypedName: stopping these takes the typed service name (decision 8).
func sysNeedsTypedName(name string) bool { return name == "ssh" || name == "iptables" }

// sysServiceCommand is the command that start, stop or restart runs for a row.
// single is the one-row restart, which sends `yes` (decision 7).
func sysServiceCommand(name, action string, single bool) (string, []string) {
	if name == "iptables" {
		if action == "stop" {
			return "v-stop-firewall", nil
		}
		return "v-update-firewall", nil
	}
	unit := sysServiceUnit(name)
	switch action {
	case "start":
		return "v-start-service", []string{unit}
	case "stop":
		return "v-stop-service", []string{unit}
	}
	if single {
		return "v-restart-service", []string{unit, "yes"}
	}
	return "v-restart-service", []string{unit}
}

func sysWantState(action string) string {
	if action == "start" {
		return "running"
	}
	return "stopped"
}

// sysFailedMsg is the message of a start or stop that left the wrong state; its one
// argument is the service name, unquoted (decision 6).
func sysFailedMsg(action string) string {
	return sysServiceActions[action].failed
}

// servicesList is GET /api/v1/server/services.
func (sys *sysAPI) servicesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, err := sys.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	out, err := sys.listOutput(ctx, "v-list-sys-info")
	if err != nil {
		writeCommandError(w, err)
		return
	}
	info, err := hestia.DecodeSysInfo(out)
	if err != nil {
		serverError(w, err)
		return
	}
	out, err = sys.listOutput(ctx, "v-list-sys-php")
	if err != nil {
		writeCommandError(w, err)
		return
	}
	php, err := hestia.DecodeStringList(out)
	if err != nil {
		serverError(w, err)
		return
	}
	rows, err := sys.services(ctx)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	view := ServicesView{
		Info: hestia.SysInfo{
			HOSTNAME: info.HOSTNAME, OS: brand.Rewrite(info.OS), VERSION: brand.Rewrite(info.VERSION),
			ARCH: brand.Rewrite(info.ARCH), HESTIA: brand.Rewrite(info.HESTIA), RELEASE: brand.Rewrite(info.RELEASE),
			UPTIME: brand.Rewrite(info.UPTIME), LOADAVERAGE: brand.Rewrite(info.LOADAVERAGE),
		},
		Release:  brand.Rewrite(info.RELEASE),
		Firewall: cfg["FIREWALL_SYSTEM"] == "iptables",
		Services: []ServiceRow{},
	}
	names := make([]string, 0, len(rows))
	for n := range rows {
		names = append(names, n)
	}
	sort.Strings(names) // PHP's ksort on these non-numeric keys is a byte compare
	for _, n := range names {
		s := rows[n]
		editor := ""
		if _, ok := sysEditorFor(n); ok && sysPHPInstalled(n, php) {
			editor = n
		}
		view.Services = append(view.Services, ServiceRow{
			Name: n, System: brand.Rewrite(s.SYSTEM), State: s.STATE, CPU: s.CPU, Mem: s.MEM, RTime: s.RTIME, Editor: editor,
		})
	}
	writeJSON(w, http.StatusOK, view)
}

// sysPHPInstalled is true for every name but a phpX.Y-fpm whose version isn't installed.
func sysPHPInstalled(name string, installed []string) bool {
	m := sysPHPVersionName.FindStringSubmatch(name)
	return m == nil || slices.Contains(installed, m[1])
}

// listOutput runs a read command and returns its stdout.
func (sys *sysAPI) listOutput(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	res, err := sys.R.Run(runner.WithTimeout(ctx, sysTimeoutDefault), cmd, append(args, "json")...)
	if err != nil {
		return nil, err
	}
	return res.Stdout, res.Err(cmd)
}

// sysOptionalBody decodes a JSON body that may be absent.
func sysOptionalBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "bad_request", badRequestMsg)
		return false
	}
	return true
}

// serviceAction is POST /api/v1/server/services/{service}/{action}.
func (sys *sysAPI) serviceAction(w http.ResponseWriter, r *http.Request) {
	svc, action := r.PathValue("service"), r.PathValue("action")
	if _, ok := sysServiceActions[action]; !ok || !sysServiceName.MatchString(svc) {
		WriteError(w, http.StatusBadRequest, "invalid", sysUnknownMsg)
		return
	}
	var req ServiceActionRequest
	if !sysOptionalBody(w, r, &req) {
		return
	}
	if action == "stop" && sysNeedsTypedName(svc) && req.Confirm != svc {
		WriteError(w, http.StatusBadRequest, "confirm_required", sysConfirmMsg)
		return
	}
	rows, err := sys.services(r.Context())
	if err != nil {
		writeCommandError(w, err)
		return
	}
	if _, ok := rows[svc]; !ok {
		WriteError(w, http.StatusBadRequest, "invalid", sysUnknownMsg)
		return
	}
	step := stepName(sysServiceActions[action].step, svc)
	sys.startJob(w, r, step, func(ctx context.Context, j *jobs.Job) (any, error) {
		cmd, args := sysServiceCommand(svc, action, true)
		if _, err := sys.step(ctx, j, step, sysTimeoutService, cmd, args...); err != nil {
			return nil, err
		}
		if action == "restart" {
			return OK{Status: "ok"}, nil
		}
		j.Step(stepName(StepCheckMsgid, svc))
		after, err := sys.services(ctx)
		if err != nil {
			return nil, err
		}
		if after[svc].STATE != sysWantState(action) {
			return nil, &jobs.Failure{Code: "service_failed", Message: sysFailedMsg(action), Args: []string{svc}}
		}
		return OK{Status: "ok"}, nil
	})
}

// servicesBulk is POST /api/v1/server/services/bulk: every item is tried and the
// failures are listed (decision 7).
func (sys *sysAPI) servicesBulk(w http.ResponseWriter, r *http.Request) {
	var req BulkRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if _, ok := sysServiceActions[req.Action]; !bulkShapeOK(w, ok, req.Action, len(req.Names), maxBulk) {
		return
	}
	var names []string
	for _, n := range req.Names {
		if !sysServiceName.MatchString(n) {
			WriteError(w, http.StatusBadRequest, "invalid", sysUnknownMsg)
			return
		}
		if req.Action == "stop" && sysNeedsTypedName(n) {
			// Only the single stop can ask for the typed name.
			writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "confirm_required", Message: sysBulkStopMsg, Args: []string{n}}})
			return
		}
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	rows, err := sys.services(r.Context())
	if err != nil {
		writeCommandError(w, err)
		return
	}
	for _, n := range names {
		if _, ok := rows[n]; !ok {
			WriteError(w, http.StatusBadRequest, "invalid", sysUnknownMsg)
			return
		}
	}
	title := sysServiceActions[req.Action].bulk
	sys.startJob(w, r, title, func(ctx context.Context, j *jobs.Job) (any, error) {
		failed := []BulkFailure{}
		var tried []string
		for _, n := range names {
			cmd, args := sysServiceCommand(n, req.Action, false)
			if _, err := sys.step(ctx, j, stepName(sysServiceActions[req.Action].step, n), sysTimeoutService, cmd, args...); err != nil {
				failed = append(failed, BulkFailure{Name: n, Message: errorMessage(err)})
				continue
			}
			tried = append(tried, n)
		}
		if req.Action != "restart" && len(tried) > 0 {
			j.Step("Check services")
			after, err := sys.services(ctx)
			if err != nil {
				return nil, err
			}
			for _, n := range tried {
				if after[n].STATE != sysWantState(req.Action) {
					failed = append(failed, BulkFailure{Name: n, Message: sysFailedMsg(req.Action), Args: []string{n}})
				}
			}
		}
		return BulkReply{Status: "ok", Failed: failed}, nil
	})
}

// reboot is POST /api/v1/server/reboot (decision 10): the typed hostname must be the
// server's own.
func (sys *sysAPI) reboot(w http.ResponseWriter, r *http.Request) {
	var req RebootRequest
	if !decodeBody(w, r, &req) {
		return
	}
	out, err := sys.listOutput(r.Context(), "v-list-sys-info")
	if err != nil {
		writeCommandError(w, err)
		return
	}
	info, err := hestia.DecodeSysInfo(out)
	if err != nil {
		serverError(w, err)
		return
	}
	if req.Hostname == "" || req.Hostname != info.HOSTNAME {
		WriteError(w, http.StatusBadRequest, "invalid", "The hostname doesn't match this server.")
		return
	}
	sys.startJob(w, r, "Restart server", func(ctx context.Context, j *jobs.Job) (any, error) {
		if _, err := sys.step(ctx, j, "Restart server", sysTimeoutDefault, "v-restart-system", "yes"); err != nil {
			return nil, err
		}
		return OK{Status: "ok"}, nil
	})
}

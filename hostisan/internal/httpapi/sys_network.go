package httpapi

// Slice D: IP addresses (sys_ips.go), firewall rules, toggle, bans and IP sets
// (sys_firewall.go), with the lockout guards in sys_lockout.go.

import (
	"context"
	"net/http"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

// Field msgids shared by the IP and firewall forms.
const (
	sysNetInvalidMsg   = `Field "%s" is invalid.`
	sysNetInvalidIPMsg = "Invalid IP address: %s"
)

// networkRoutes mounts the IP address, firewall rule, ban and IP set routes, and sets
// sys.firewallEnableCheck, the "enabling the firewall" guard the settings save calls.
// DELETE /ips/{ip}, POST /ips/refresh and DELETE /firewall/ipsets/{name} take no body
// (their handlers never call decodeBody).
func (sys *sysAPI) networkRoutes(m *sysMux) {
	sys.firewallEnableCheck = sys.netFirewallEnableCheck

	m.handle("GET /api/v1/server/ips", sys.read(sys.netIPList))
	m.handle("GET /api/v1/server/ips/options", sys.read(sys.netIPOptions))
	m.handle("GET /api/v1/server/ips/{ip}", sys.read(sys.netIPGet))
	m.handle("POST /api/v1/server/ips", sys.write(sys.netIPAdd))
	m.handle("PUT /api/v1/server/ips/{ip}", sys.write(sys.netIPEdit))
	m.handle("DELETE /api/v1/server/ips/{ip}", sys.write(sys.netIPDelete))
	m.handle("POST /api/v1/server/ips/bulk", sys.write(sys.netIPBulk))
	m.handle("POST /api/v1/server/ips/refresh", sys.write(sys.netIPRefresh))

	m.handle("GET /api/v1/server/firewall", sys.read(sys.netFwList))
	m.handle("GET /api/v1/server/firewall/rules/{id}", sys.read(sys.netFwGet))
	m.handle("POST /api/v1/server/firewall/rules", sys.write(sys.netFwAdd))
	m.handle("PUT /api/v1/server/firewall/rules/{id}", sys.write(sys.netFwEdit))
	m.handle("POST /api/v1/server/firewall/rules/{id}/{action}", sys.write(sys.netFwAction))
	m.handle("POST /api/v1/server/firewall/rules/bulk", sys.write(sys.netFwBulk))
	m.handle("POST /api/v1/server/firewall/toggle", sys.write(sys.netFwToggle))

	m.handle("GET /api/v1/server/firewall/bans", sys.read(sys.netBanList))
	m.handle("POST /api/v1/server/firewall/bans", sys.write(sys.netBanAdd))
	m.handle("POST /api/v1/server/firewall/bans/delete", sys.write(sys.netBanDelete))

	m.handle("GET /api/v1/server/firewall/ipsets", sys.read(sys.netIpsetList))
	m.handle("POST /api/v1/server/firewall/ipsets", sys.write(sys.netIpsetAdd))
	m.handle("POST /api/v1/server/firewall/ipsets/bulk", sys.write(sys.netIpsetBulk))
	m.handle("DELETE /api/v1/server/firewall/ipsets/{name}", sys.write(sys.netIpsetDelete))
}

// sysNetInvalid answers 400 invalid with msg and its arguments.
func sysNetInvalid(w http.ResponseWriter, msg string, args ...string) {
	writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{Code: "invalid", Message: msg, Args: args}})
}

// sysNetFailure answers a guard's failure with status.
func sysNetFailure(w http.ResponseWriter, status int, f *jobs.Failure) {
	writeJSON(w, status, APIError{Error: ErrorBody{Code: f.Code, Message: f.Message, Args: f.Args}})
}

// netRead runs a read command with the default timeout and returns its stdout; a
// non-zero exit is a *runner.CommandError.
func (sys *sysAPI) netRead(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	res, err := sys.R.Run(runner.WithTimeout(ctx, sysTimeoutDefault), cmd, args...)
	if err == nil {
		err = res.Err(cmd)
	}
	return res.Stdout, err
}

// netRun runs a write command with the default timeout.
func (sys *sysAPI) netRun(ctx context.Context, cmd string, args ...string) error {
	_, err := sys.netRead(ctx, cmd, args...)
	return err
}

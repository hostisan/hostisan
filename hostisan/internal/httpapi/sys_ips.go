package httpapi

// IP addresses (FW §1.1–1.3, §3.1–3.4, decision 45). Add, delete, bulk delete and
// refresh run as server jobs, because the core restarts web and proxy; the edit runs
// in the request, in PHP's order, and stops at the first error.

import (
	"context"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/session"
)

func sysIPRow(ip hestia.SysIP) IPRow {
	return IPRow{
		IP: ip.IP, Owner: ip.OWNER, Status: ip.STATUS, Name: ip.NAME, Users: ip.U_SYS_USERS,
		WebDomains: ip.U_WEB_DOMAINS, Interface: ip.INTERFACE, Netmask: ip.NETMASK, NAT: ip.NAT,
		Display: ip.Display(), Time: ip.TIME, Date: ip.DATE,
	}
}

// netIPList is GET /ips. The core lists in `ls` order; "name" sorts by the address
// text (PHP ksort) and "date" reverses the listing, as PHP did (FW gotcha 10).
func (sys *sysAPI) netIPList(w http.ResponseWriter, r *http.Request) {
	out, ok := sys.output(w, r, "v-list-sys-ips", "json")
	if !ok {
		return
	}
	ips, err := hestia.DecodeObjects[hestia.SysIP](out)
	if err != nil {
		serverError(w, err)
		return
	}
	order := session.FromContext(r.Context()).SortOrder
	if order == "name" {
		slices.SortStableFunc(ips, func(a, b hestia.SysIP) int { return strings.Compare(a.IP, b.IP) })
	} else {
		order = "date"
		slices.Reverse(ips)
	}
	rows := make([]IPRow, 0, len(ips))
	for _, ip := range ips {
		rows = append(rows, sysIPRow(ip))
	}
	writeJSON(w, http.StatusOK, IPsView{IPs: rows, SortOrder: order})
}

// netIPOptions is GET /ips/options: interfaces, users and ROOT_USER.
func (sys *sysAPI) netIPOptions(w http.ResponseWriter, r *http.Request) {
	out, ok := sys.output(w, r, "v-list-sys-interfaces", "json")
	if !ok {
		return
	}
	ifaces, err := hestia.DecodeStringList(out)
	if err != nil {
		serverError(w, err)
		return
	}
	if out, ok = sys.output(w, r, "v-list-sys-users", "json"); !ok {
		return
	}
	users, err := hestia.DecodeStringList(out)
	if err != nil {
		serverError(w, err)
		return
	}
	cfg, err := sys.H.Config(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, IPOptions{Interfaces: ifaces, Users: users, RootUser: rootUser(cfg)})
}

// sysIPPath is the {ip} path value, or "" after answering 400 when it isn't IPv4.
func sysIPPath(w http.ResponseWriter, r *http.Request) string {
	ip := r.PathValue("ip")
	if !hestia.ValidIPv4(ip) {
		invalidFormat(w, "ip", ip)
		return ""
	}
	return ip
}

// netIP reads one address; it answers the request itself on failure.
func (sys *sysAPI) netIP(w http.ResponseWriter, r *http.Request, ip string) (hestia.SysIP, bool) {
	out, ok := sys.output(w, r, "v-list-sys-ip", ip, "json")
	if !ok {
		return hestia.SysIP{}, false
	}
	list, err := hestia.DecodeObjects[hestia.SysIP](out)
	if err != nil || len(list) != 1 {
		serverError(w, firstErr(err, "v-list-sys-ip: expected one address"))
		return hestia.SysIP{}, false
	}
	return list[0], true
}

// netIPGet is GET /ips/{ip}.
func (sys *sysAPI) netIPGet(w http.ResponseWriter, r *http.Request) {
	ip := sysIPPath(w, r)
	if ip == "" {
		return
	}
	cur, ok := sys.netIP(w, r, ip)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, sysIPRow(cur))
}

var sysIfaceRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@:-]{0,31}$`)

// sysValidNetmask accepts a dotted IPv4 netmask with contiguous ones.
func sysValidNetmask(s string) bool {
	if !hestia.ValidIPv4(s) {
		return false
	}
	a := netip.MustParseAddr(s).As4()
	n := uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
	return n != 0 && ^n&(^n+1) == 0 // the zeros are one trailing run
}

// sysIPCheck validates the editable fields of an IP request (owner only when it is
// sent, i.e. dedicated); false means it answered 400.
func sysIPCheck(w http.ResponseWriter, req IPRequest) bool {
	switch {
	case !req.Shared && !hestia.ValidUsername(req.Owner):
		sysNetInvalid(w, sysNetInvalidMsg, "Assigned User")
	case req.Name != "" && (!hestia.ValidDomainArg(req.Name) || strings.ContainsAny(req.Name, `'"\$`+"`")):
		sysNetInvalid(w, sysNetInvalidMsg, "Assigned Domain")
	case req.NAT != "" && !hestia.ValidIPv4(req.NAT):
		sysNetInvalid(w, sysNetInvalidIPMsg, req.NAT)
	default:
		return true
	}
	return false
}

// netIPAdd is POST /ips: one v-add-sys-ip with all seven arguments, as a job. A
// shared IP is owned by ROOT_USER, so the core never turns it dedicated (FW gotcha 7).
func (sys *sysAPI) netIPAdd(w http.ResponseWriter, r *http.Request) {
	var req IPRequest
	if !decodeBody(w, r, &req) {
		return
	}
	owner := req.Owner
	if req.Shared {
		owner = "-" // not blank; replaced by ROOT_USER below
	}
	if !blankFields(w, "IP Address", req.IP, "Netmask", req.Netmask, "Interface", req.Interface, "Assigned User", owner) {
		return
	}
	switch {
	case !hestia.ValidIPv4(req.IP):
		sysNetInvalid(w, sysNetInvalidIPMsg, req.IP)
		return
	case !sysValidNetmask(req.Netmask):
		sysNetInvalid(w, sysNetInvalidMsg, "Netmask")
		return
	case !sysIfaceRe.MatchString(req.Interface):
		sysNetInvalid(w, sysNetInvalidMsg, "Interface")
		return
	}
	if !sysIPCheck(w, req) {
		return
	}
	status := "dedicated"
	if req.Shared {
		cfg, err := sys.H.Config(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		owner, status = rootUser(cfg), "shared"
	}
	args := []string{req.IP, req.Netmask, req.Interface, owner, status, req.Name, req.NAT}
	sys.startJob(w, r, "Add IP address", func(ctx context.Context, j *jobs.Job) (any, error) {
		if _, err := sys.step(ctx, j, "Add IP address", sysTimeoutService, "v-add-sys-ip", args...); err != nil {
			return nil, err
		}
		return OK{Status: "ok"}, nil
	})
}

// netIPEdit is PUT /ips/{ip}: status, owner, name, NAT, each only when it changed,
// stopping at the first error (FW §3.2). Making an IP shared skips the owner step: the
// core hands it to ROOT_USER itself.
func (sys *sysAPI) netIPEdit(w http.ResponseWriter, r *http.Request) {
	ip := sysIPPath(w, r)
	if ip == "" {
		return
	}
	var req IPRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !req.Shared && !blankFields(w, "Assigned User", req.Owner) {
		return
	}
	if !sysIPCheck(w, req) {
		return
	}
	cur, ok := sys.netIP(w, r, ip)
	if !ok {
		return
	}
	var steps [][]string
	switch {
	case cur.STATUS == "shared" && !req.Shared:
		steps = append(steps, []string{"v-change-sys-ip-status", ip, "dedicated"})
	case cur.STATUS != "shared" && req.Shared:
		steps = append(steps, []string{"v-change-sys-ip-status", ip, "shared"})
	}
	if !req.Shared && req.Owner != cur.OWNER {
		steps = append(steps, []string{"v-change-sys-ip-owner", ip, req.Owner})
	}
	if req.Name != cur.NAME {
		steps = append(steps, []string{"v-change-sys-ip-name", ip, req.Name})
	}
	if req.NAT != cur.NAT {
		steps = append(steps, []string{"v-change-sys-ip-nat", ip, req.NAT})
	}
	for _, st := range steps {
		if err := sys.netRun(r.Context(), st[0], st[1:]...); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// netIPDelete is DELETE /ips/{ip} (no body), as a job.
func (sys *sysAPI) netIPDelete(w http.ResponseWriter, r *http.Request) {
	ip := sysIPPath(w, r)
	if ip == "" {
		return
	}
	sys.startJob(w, r, "Delete IP address", func(ctx context.Context, j *jobs.Job) (any, error) {
		if _, err := sys.step(ctx, j, "Delete IP address", sysTimeoutService, "v-delete-sys-ip", ip); err != nil {
			return nil, err
		}
		return OK{Status: "ok"}, nil
	})
}

// netIPBulk is POST /ips/bulk (action "delete"), as a job: every address is tried
// and the result lists each failure (FW gotcha 9).
func (sys *sysAPI) netIPBulk(w http.ResponseWriter, r *http.Request) {
	var req BulkRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !bulkShapeOK(w, req.Action == "delete", req.Action, len(req.Names), maxBulk) {
		return
	}
	for _, ip := range req.Names {
		if !hestia.ValidIPv4(ip) {
			invalidFormat(w, "ip", ip)
			return
		}
	}
	names := slices.Clone(req.Names)
	sys.startJob(w, r, "Delete IP addresses", func(ctx context.Context, j *jobs.Job) (any, error) {
		failed := []BulkFailure{}
		for _, ip := range names {
			if _, err := sys.step(ctx, j, "Delete IP address", sysTimeoutService, "v-delete-sys-ip", ip); err != nil {
				failed = append(failed, BulkFailure{Name: ip, Message: errorMessage(err)})
			}
		}
		return BulkReply{Status: "ok", Failed: failed}, nil
	})
}

// netIPRefresh is POST /ips/refresh (no body, no selection; FW gotcha 11), as a job.
func (sys *sysAPI) netIPRefresh(w http.ResponseWriter, r *http.Request) {
	sys.startJob(w, r, "Refresh IP addresses", func(ctx context.Context, j *jobs.Job) (any, error) {
		if _, err := sys.step(ctx, j, "Refresh IP addresses", sysTimeoutService, "v-update-sys-ip"); err != nil {
			return nil, err
		}
		return OK{Status: "ok"}, nil
	})
}

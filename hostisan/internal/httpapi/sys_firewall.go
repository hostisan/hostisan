package httpapi

// Firewall rules, on/off, bans and IP sets (FW §1.4–1.11, §3.5–3.18, decisions
// 37–44). Every write that checks data before changing it holds sys.fwMu for the
// whole read–check–write sequence, so two admins can't interleave.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

const (
	sysFwInvalidPortMsg    = "Invalid port: %s"
	sysFwInvalidCommentMsg = "Invalid comment: %s"
	sysFwInvalidIpsetMsg   = "IP list %s must exist, be IPv4 and not be suspended."
	sysFwInvalidSourceMsg  = "Invalid data source: %s"
	sysFwChangedMsg        = "The rule has changed since the page was loaded. Reload and try again."
	sysFwBanMissingMsg     = "The IP address was not banned: it is a server address, excluded, or already banned."
	sysFwMoveTopMsg        = "The rule is already at the top of the list."
	sysFwMoveBottomMsg     = "The rule is already at the bottom of the list."

	// sysFwBlacklistSource is the only script: source an IP set may use (decision 44).
	sysFwBlacklistSource = "script:/usr/local/hestia/install/common/firewall/ipset/blacklist.sh"
)

// netFwState reads FIREWALL_SYSTEM (iptables) and FIREWALL_EXTENSION (bans).
func (sys *sysAPI) netFwState(ctx context.Context) (enabled, bans bool, cfg hestia.SysConfig, err error) {
	cfg, err = sys.H.Config(ctx)
	if err != nil {
		return false, false, nil, err
	}
	return cfg["FIREWALL_SYSTEM"] == "iptables", cfg["FIREWALL_EXTENSION"] != "", cfg, nil
}

func sysFwRow(r hestia.FirewallRule) FirewallRuleRow {
	return FirewallRuleRow{
		ID: r.RULE, Action: r.ACTION, Protocol: r.PROTOCOL, Port: r.PORT, IP: r.IP, Comment: r.COMMENT,
		Suspended: r.SUSPENDED == "yes", Protected: sysFwProtected(r.COMMENT), Expect: sysFwExpect(r),
		Time: r.TIME, Date: r.DATE,
	}
}

// netFwRules reads every rule (v-list-firewall json), in file order.
func (sys *sysAPI) netFwRules(ctx context.Context) ([]hestia.FirewallRule, error) {
	out, err := sys.netRead(ctx, "v-list-firewall", "json")
	if err != nil {
		return nil, err
	}
	return hestia.DecodeOptional[hestia.FirewallRule](out)
}

// netFwRule reads one rule (v-list-firewall-rule <id> json).
func (sys *sysAPI) netFwRule(ctx context.Context, id string) (hestia.FirewallRule, error) {
	out, err := sys.netRead(ctx, "v-list-firewall-rule", id, "json")
	if err != nil {
		return hestia.FirewallRule{}, err
	}
	list, err := hestia.DecodeObjects[hestia.FirewallRule](out)
	if err == nil && len(list) != 1 {
		err = errors.New("v-list-firewall-rule: expected one rule")
	}
	if err != nil {
		return hestia.FirewallRule{}, err
	}
	return list[0], nil
}

// sysFwCallers is every address the caller may reach the server as, and the peer
// iptables sees (I1). session.RealIP is the visitor: behind Cloudflare it is
// CF-Connecting-IP, while iptables sees the edge address the proxy forwards as
// X-Real-IP (or RemoteAddr off the proxy socket). RealIP of the request without
// CF-Connecting-IP is that transport peer.
func sysFwCallers(r *http.Request) (callers []string, peer string) {
	visitor := session.RealIP(r)
	bare := r.Clone(r.Context())
	bare.Header.Del("CF-Connecting-IP")
	peer = session.RealIP(bare)
	callers = []string{visitor}
	if peer != visitor {
		callers = append(callers, peer)
	}
	return callers, peer
}

// netFwEnv gathers what the DROP and enable guards check against: the callers, the
// panel port and the live sshd ports. BACKEND_PORT is read fresh from
// v-list-sys-config, not from the 5 s config cache, so a panel port changed a moment
// ago is the one guarded (M4).
func (sys *sysAPI) netFwEnv(r *http.Request) (sysFwEnv, error) {
	out, err := sys.netRead(r.Context(), "v-list-sys-config", "json")
	if err != nil {
		return sysFwEnv{}, err
	}
	var doc struct {
		Config hestia.SysConfig `json:"config"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return sysFwEnv{}, err
	}
	if out, err = sys.netRead(r.Context(), "v-list-sys-sshd-port", "plain"); err != nil {
		return sysFwEnv{}, err
	}
	ports, err := hestia.ParseSSHDPorts(out)
	if err != nil {
		return sysFwEnv{}, err
	}
	// No port means `sshd -T` failed. Guessing 22 could guard the wrong port and let
	// the real one be closed, so every guard refuses instead (fix round 1, M3).
	if len(ports) == 0 {
		return sysFwEnv{}, sysFwLockout(sysFwLockoutNoSSHPortMsg)
	}
	panel := doc.Config["BACKEND_PORT"]
	if panel == "" {
		panel = "8083"
	}
	callers, peer := sysFwCallers(r)
	return sysFwEnv{Callers: callers, Peer: peer, PanelPort: panel, SSHPorts: ports}, nil
}

// netFwSortEval orders rules as the chain evaluates them: highest id first
// (decision 39).
func sysFwSortEval(rules []hestia.FirewallRule) {
	slices.SortStableFunc(rules, func(a, b hestia.FirewallRule) int {
		x, _ := strconv.Atoi(a.RULE)
		y, _ := strconv.Atoi(b.RULE)
		return y - x
	})
}

// netFwList is GET /firewall. While the firewall is off it runs nothing and shows the
// empty state (decision 44).
func (sys *sysAPI) netFwList(w http.ResponseWriter, r *http.Request) {
	enabled, bans, _, err := sys.netFwState(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	view := FirewallView{Enabled: enabled, Bans: bans, Rules: []FirewallRuleRow{}}
	if enabled {
		rules, err := sys.netFwRules(r.Context())
		if err != nil {
			writeCommandError(w, err)
			return
		}
		sysFwSortEval(rules)
		for _, rule := range rules {
			view.Rules = append(view.Rules, sysFwRow(rule))
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// sysFwIDPath is the {id} path value, or "" after answering 400.
func sysFwIDPath(w http.ResponseWriter, r *http.Request) string {
	id := r.PathValue("id")
	if !hestia.ValidRuleID(id) {
		invalidFormat(w, "rule", id)
		return ""
	}
	return id
}

// netFwGet is GET /firewall/rules/{id}.
func (sys *sysAPI) netFwGet(w http.ResponseWriter, r *http.Request) {
	id := sysFwIDPath(w, r)
	if id == "" {
		return
	}
	rule, err := sys.netFwRule(r.Context(), id)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sysFwRow(rule))
}

var sysFwPortSep = regexp.MustCompile(`[\s,]+`)

// sysFwNormalize upper-cases action and protocol and cleans the port as PHP did
// (spaces become commas, runs collapse, ends trimmed), then validates every field
// (decision 42). False means it answered 400.
func sysFwNormalize(w http.ResponseWriter, req *FirewallRuleRequest) bool {
	req.Action = strings.ToUpper(strings.TrimSpace(req.Action))
	req.Protocol = strings.ToUpper(strings.TrimSpace(req.Protocol))
	req.Port = strings.Trim(sysFwPortSep.ReplaceAllString(strings.TrimSpace(req.Port), ","), ",")
	req.IP = strings.TrimSpace(req.IP)
	req.Comment = strings.TrimSpace(req.Comment)
	if !blankFields(w, "Action", req.Action, "Protocol", req.Protocol, "Port", req.Port, "IP Address", req.IP) {
		return false
	}
	switch {
	case req.Action != "ACCEPT" && req.Action != "DROP":
		sysNetInvalid(w, sysNetInvalidMsg, "Action")
	case req.Protocol != "TCP" && req.Protocol != "UDP" && req.Protocol != "ICMP":
		sysNetInvalid(w, sysNetInvalidMsg, "Protocol")
	case !hestia.ValidPortExt(req.Port):
		sysNetInvalid(w, sysFwInvalidPortMsg, req.Port)
	case !sysFwValidSource(req.IP):
		sysNetInvalid(w, sysNetInvalidIPMsg, req.IP)
	case !hestia.ValidFirewallComment(req.Comment):
		sysNetInvalid(w, sysFwInvalidCommentMsg, req.Comment)
	default:
		return true
	}
	return false
}

// sysFwValidSource accepts an IPv4 CIDR or ipset:<name>.
func sysFwValidSource(ip string) bool {
	if name, ok := strings.CutPrefix(ip, "ipset:"); ok {
		return hestia.ValidIpsetName(name)
	}
	return hestia.ValidCIDR4(ip)
}

// netFwCheckIpset refuses an ipset: source unless the set exists, is v4 and isn't
// suspended (decision 42). False means it answered.
func (sys *sysAPI) netFwCheckIpset(w http.ResponseWriter, r *http.Request, ip string) bool {
	name, ok := strings.CutPrefix(ip, "ipset:")
	if !ok {
		return true
	}
	out, err := sys.netRead(r.Context(), "v-list-firewall-ipset", "json")
	if err != nil {
		writeCommandError(w, err)
		return false
	}
	sets, err := hestia.DecodeOptional[hestia.FirewallIpset](out)
	if err != nil {
		serverError(w, err)
		return false
	}
	for _, s := range sets {
		if s.LISTNAME == name && s.IP_VERSION == "v4" && s.SUSPENDED != "yes" {
			return true
		}
	}
	sysNetInvalid(w, sysFwInvalidIpsetMsg, name)
	return false
}

// netFwCheckDrop runs the DROP lockout guard on a rule that will be active. False
// means it answered.
func (sys *sysAPI) netFwCheckDrop(w http.ResponseWriter, r *http.Request, action, protocol, ip, port string) bool {
	if action != "DROP" {
		return true
	}
	env, err := sys.netFwEnv(r)
	if err != nil {
		sysFwWriteError(w, err)
		return false
	}
	if f := sysFwDropLockout(action, protocol, ip, port, env); f != nil {
		sysNetFailure(w, http.StatusConflict, f)
		return false
	}
	return true
}

// netFwCheckKeepOpen runs the keep-open guard (D5) on what change does to the
// rules: the fresh listing, read here when rules is nil. change gets a copy. False
// means it answered.
func (sys *sysAPI) netFwCheckKeepOpen(w http.ResponseWriter, r *http.Request, rules []hestia.FirewallRule,
	change func([]hestia.FirewallRule) []hestia.FirewallRule) bool {
	// While the firewall is off nothing is filtered, and the core refuses the
	// change anyway (FIREWALL_SYSTEM not enabled).
	enabled, _, _, err := sys.netFwState(r.Context())
	if err != nil {
		serverError(w, err)
		return false
	}
	if !enabled {
		return true
	}
	if rules == nil {
		if rules, err = sys.netFwRules(r.Context()); err != nil {
			writeCommandError(w, err)
			return false
		}
	}
	env, err := sys.netFwEnv(r)
	if err != nil {
		sysFwWriteError(w, err)
		return false
	}
	if f := sysFwKeepOpenLockout(rules, change(slices.Clone(rules)), env); f != nil {
		sysNetFailure(w, http.StatusConflict, f)
		return false
	}
	return true
}

// sysFwWriteError answers err: 409 for a lockout *jobs.Failure (netFwEnv's unknown
// SSH port), else as a command error.
func sysFwWriteError(w http.ResponseWriter, err error) {
	var f *jobs.Failure
	if errors.As(err, &f) {
		sysNetFailure(w, http.StatusConflict, f)
		return
	}
	writeCommandError(w, err)
}

// netFwCheckMove runs the move guard (fix round 1, M4) on the fresh rules. False
// means it answered.
func (sys *sysAPI) netFwCheckMove(w http.ResponseWriter, r *http.Request) bool {
	rules, err := sys.netFwRules(r.Context())
	if err != nil {
		writeCommandError(w, err)
		return false
	}
	env, err := sys.netFwEnv(r)
	if err != nil {
		sysFwWriteError(w, err)
		return false
	}
	if f := sysFwMoveLockout(rules, env); f != nil {
		sysNetFailure(w, http.StatusConflict, f)
		return false
	}
	return true
}

// sysFwWithout drops the rules whose id is in ids.
func sysFwWithout(rules []hestia.FirewallRule, ids map[string]bool) []hestia.FirewallRule {
	return slices.DeleteFunc(rules, func(r hestia.FirewallRule) bool { return ids[r.RULE] })
}

// sysFwSuspended marks the rules whose id is in ids suspended.
func sysFwSuspended(rules []hestia.FirewallRule, ids map[string]bool) []hestia.FirewallRule {
	for i := range rules {
		if ids[rules[i].RULE] {
			rules[i].SUSPENDED = "yes"
		}
	}
	return rules
}

// netFwAdd is POST /firewall/rules: validate, guard, then v-add-firewall-rule.
func (sys *sysAPI) netFwAdd(w http.ResponseWriter, r *http.Request) {
	var req FirewallRuleRequest
	if !decodeBody(w, r, &req) || !sysFwNormalize(w, &req) {
		return
	}
	if f := sysFwReservedCommentLockout(nil, req.Comment); f != nil {
		sysNetFailure(w, http.StatusConflict, f)
		return
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	if !sys.netFwCheckIpset(w, r, req.IP) || !sys.netFwCheckDrop(w, r, req.Action, req.Protocol, req.IP, req.Port) {
		return
	}
	if err := sys.netRun(r.Context(), "v-add-firewall-rule", req.Action, req.IP, req.Port, req.Protocol, req.Comment); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// netFwCurrent re-reads rule id and compares it with what the admin saw. False means
// it answered (409 changed on a mismatch, decision 38).
func (sys *sysAPI) netFwCurrent(w http.ResponseWriter, r *http.Request, id, expect string) (hestia.FirewallRule, bool) {
	rule, err := sys.netFwRule(r.Context(), id)
	if err != nil {
		writeCommandError(w, err)
		return rule, false
	}
	if sysFwExpect(rule) != expect {
		WriteError(w, http.StatusConflict, "changed", sysFwChangedMsg)
		return rule, false
	}
	return rule, true
}

// netFwEdit is PUT /firewall/rules/{id}: validate, check the fingerprint and the
// guards, change the rule, and suspend it again when it was suspended (the core's
// change unsuspends it; decision 40).
func (sys *sysAPI) netFwEdit(w http.ResponseWriter, r *http.Request) {
	id := sysFwIDPath(w, r)
	if id == "" {
		return
	}
	var req FirewallRuleRequest
	if !decodeBody(w, r, &req) || !sysFwNormalize(w, &req) {
		return
	}
	if req.Expect == "" {
		WriteError(w, http.StatusBadRequest, "invalid", badRequestMsg)
		return
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	cur, ok := sys.netFwCurrent(w, r, id, req.Expect)
	if !ok {
		return
	}
	if f := sysFwProtectedEditLockout(cur, req); f != nil {
		sysNetFailure(w, http.StatusConflict, f)
		return
	}
	if f := sysFwReservedCommentLockout(&cur, req.Comment); f != nil {
		sysNetFailure(w, http.StatusConflict, f)
		return
	}
	if !sys.netFwCheckIpset(w, r, req.IP) || !sys.netFwCheckDrop(w, r, req.Action, req.Protocol, req.IP, req.Port) {
		return
	}
	if sysFwMayOpen(cur) && !sys.netFwCheckKeepOpen(w, r, nil, func(rules []hestia.FirewallRule) []hestia.FirewallRule {
		for i := range rules {
			if rules[i].RULE == id {
				rules[i].ACTION, rules[i].PROTOCOL, rules[i].IP, rules[i].PORT, rules[i].COMMENT = req.Action, req.Protocol, req.IP, req.Port, req.Comment
			}
		}
		return rules
	}) {
		return
	}
	if err := sys.netRun(r.Context(), "v-change-firewall-rule", id, req.Action, req.IP, req.Port, req.Protocol, req.Comment); err != nil {
		writeCommandError(w, err)
		return
	}
	if cur.SUSPENDED == "yes" {
		if err := sys.netRun(r.Context(), "v-suspend-firewall-rule", id); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// netFwAfterDelete runs v-update-firewall when no rule is left, because the core's
// delete skips the rebuild then and iptables keeps the old rule (decision 41).
func (sys *sysAPI) netFwAfterDelete(ctx context.Context) error {
	rules, err := sys.netFwRules(ctx)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return sys.netRun(ctx, "v-update-firewall")
	}
	return nil
}

// netFwAction is POST /firewall/rules/{id}/{action}: delete, suspend, unsuspend,
// move-up or move-down, after the fingerprint check and the guards. "Move up" means
// evaluated earlier, which is the core's "down" (decision 39).
func (sys *sysAPI) netFwAction(w http.ResponseWriter, r *http.Request) {
	id := sysFwIDPath(w, r)
	if id == "" {
		return
	}
	var cmd []string
	switch action := r.PathValue("action"); action {
	case "delete":
		cmd = []string{"v-delete-firewall-rule", id}
	case "suspend":
		cmd = []string{"v-suspend-firewall-rule", id}
	case "unsuspend":
		cmd = []string{"v-unsuspend-firewall-rule", id}
	case "move-up":
		cmd = []string{"v-move-firewall-rule", id, "down"}
	case "move-down":
		cmd = []string{"v-move-firewall-rule", id, "up"}
	default:
		invalidFormat(w, "action", action)
		return
	}
	var req RuleActionRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Expect == "" {
		WriteError(w, http.StatusBadRequest, "invalid", badRequestMsg)
		return
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	cur, ok := sys.netFwCurrent(w, r, id, req.Expect)
	if !ok {
		return
	}
	if cmd[0] == "v-unsuspend-firewall-rule" {
		if !sys.netFwCheckDrop(w, r, cur.ACTION, cur.PROTOCOL, cur.IP, cur.PORT) {
			return
		}
	} else if f := sysFwProtectedLockout(cur); f != nil {
		sysNetFailure(w, http.StatusConflict, f)
		return
	}
	ids := map[string]bool{id: true}
	switch {
	case cmd[0] == "v-move-firewall-rule":
		if !sys.netFwCheckMove(w, r) {
			return
		}
	case !sysFwMayOpen(cur):
	case cmd[0] == "v-delete-firewall-rule":
		if !sys.netFwCheckKeepOpen(w, r, nil, func(rules []hestia.FirewallRule) []hestia.FirewallRule { return sysFwWithout(rules, ids) }) {
			return
		}
	case cmd[0] == "v-suspend-firewall-rule":
		if !sys.netFwCheckKeepOpen(w, r, nil, func(rules []hestia.FirewallRule) []hestia.FirewallRule { return sysFwSuspended(rules, ids) }) {
			return
		}
	}
	if err := sys.netRun(r.Context(), cmd[0], cmd[1:]...); err != nil {
		if msg := sysFwMoveEndMessage(r.PathValue("action"), err); msg != "" {
			WriteError(w, http.StatusBadRequest, "invalid", msg)
			return
		}
		writeCommandError(w, err)
		return
	}
	if cmd[0] == "v-delete-firewall-rule" {
		if err := sys.netFwAfterDelete(r.Context()); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// sysFwMoveEndMessage is the reply when v-move-firewall-rule refuses to move the end
// rule (exit 1, "Cannot move rule up|down…"). The core's direction is the reverse of
// the list's (decision 39), so its text would name the wrong one (D7). "" for any
// other error.
func sysFwMoveEndMessage(action string, err error) string {
	var ce *runner.CommandError
	if !errors.As(err, &ce) || !strings.HasPrefix(strings.TrimSpace(ce.Output), "Cannot move rule") {
		return ""
	}
	switch action {
	case "move-up":
		return sysFwMoveTopMsg
	case "move-down":
		return sysFwMoveBottomMsg
	}
	return ""
}

// netFwBulk is POST /firewall/rules/bulk (delete, suspend, unsuspend). Every item is
// checked against one fresh listing before any write; then the commands run in
// descending id order, so a delete's renumbering can't reach an item still waiting
// (decision 38). Every item is tried and failures are listed.
func (sys *sysAPI) netFwBulk(w http.ResponseWriter, r *http.Request) {
	var req RuleBulkRequest
	if !decodeBody(w, r, &req) {
		return
	}
	cmd, known := map[string]string{
		"delete": "v-delete-firewall-rule", "suspend": "v-suspend-firewall-rule", "unsuspend": "v-unsuspend-firewall-rule",
	}[req.Action]
	if !bulkShapeOK(w, known, req.Action, len(req.Items), maxBulk) {
		return
	}
	seen := map[string]bool{}
	for _, it := range req.Items {
		switch {
		case !hestia.ValidRuleID(it.ID):
			invalidFormat(w, "rule", it.ID)
			return
		case it.Expect == "" || seen[it.ID]:
			WriteError(w, http.StatusBadRequest, "invalid", badRequestMsg)
			return
		}
		seen[it.ID] = true
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	rules, err := sys.netFwRules(r.Context())
	if err != nil {
		writeCommandError(w, err)
		return
	}
	byID := map[string]hestia.FirewallRule{}
	for _, rule := range rules {
		byID[rule.RULE] = rule
	}
	var env *sysFwEnv
	mayClose := false
	for _, it := range req.Items {
		cur, ok := byID[it.ID]
		if !ok || sysFwExpect(cur) != it.Expect {
			WriteError(w, http.StatusConflict, "changed", sysFwChangedMsg)
			return
		}
		if req.Action != "unsuspend" {
			if f := sysFwProtectedLockout(cur); f != nil {
				sysNetFailure(w, http.StatusConflict, f)
				return
			}
			mayClose = mayClose || sysFwMayOpen(cur)
			continue
		}
		if cur.ACTION != "DROP" {
			continue
		}
		if env == nil {
			e, err := sys.netFwEnv(r)
			if err != nil {
				sysFwWriteError(w, err)
				return
			}
			env = &e
		}
		if f := sysFwDropLockout(cur.ACTION, cur.PROTOCOL, cur.IP, cur.PORT, *env); f != nil {
			sysNetFailure(w, http.StatusConflict, f)
			return
		}
	}
	if mayClose {
		chosen := map[string]bool{}
		for _, it := range req.Items {
			chosen[it.ID] = true
		}
		change := func(rules []hestia.FirewallRule) []hestia.FirewallRule { return sysFwWithout(rules, chosen) }
		if req.Action == "suspend" {
			change = func(rules []hestia.FirewallRule) []hestia.FirewallRule { return sysFwSuspended(rules, chosen) }
		}
		if !sys.netFwCheckKeepOpen(w, r, rules, change) {
			return
		}
	}
	ids := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		ids = append(ids, it.ID)
	}
	slices.SortFunc(ids, func(a, b string) int {
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return y - x
	})
	failed := []BulkFailure{}
	for _, id := range ids {
		if err := sys.netRun(r.Context(), cmd, id); err != nil {
			failed = append(failed, BulkFailure{Name: id, Message: errorMessage(err)})
		}
	}
	if req.Action == "delete" {
		if err := sys.netFwAfterDelete(r.Context()); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, BulkReply{Status: "ok", Failed: failed})
}

// netFirewallEnableCheck is the "enabling the firewall" guard (decision 37), set as
// sys.firewallEnableCheck: the toggle and the settings save both call it. It reads the
// rules and sshd ports fresh under fwMu and returns the lockout *jobs.Failure, a
// command error, or nil to allow.
func (sys *sysAPI) netFirewallEnableCheck(ctx context.Context, r *http.Request) error {
	missing := sys.FirewallToolsMissing
	if missing == nil {
		missing = func() []string { return sysFwToolsMissingIn(sysFwToolDirs) }
	}
	if names := missing(); len(names) > 0 {
		return &jobs.Failure{Code: SysFwToolsMissingCode, Message: SysFwToolsMissingMsg, Args: []string{strings.Join(names, ", ")}}
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	rules, err := sys.netFwRules(ctx)
	if err != nil {
		return err
	}
	env, err := sys.netFwEnv(r.WithContext(ctx))
	if err != nil {
		return err
	}
	if f := sysFwEnableLockout(rules, env); f != nil {
		return f
	}
	return nil
}

// sysFwToolDirs is where the firewall binaries are looked for: the core runs
// /sbin/iptables, and finds ipset with command -v (root's PATH has both dirs).
var sysFwToolDirs = []string{"/usr/sbin", "/sbin"}

// sysFwToolsMissingIn names the firewall binaries (iptables, ipset) that no dir holds
// as an executable file. Without them v-add-sys-firewall still sets FIREWALL_SYSTEM
// and reports success, yet nothing is filtered (D6).
func sysFwToolsMissingIn(dirs []string) []string {
	var missing []string
	for _, name := range []string{"iptables", "ipset"} {
		found := false
		for _, dir := range dirs {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, name)
		}
	}
	return missing
}

// netFwToggle is POST /firewall/toggle: turning on runs the enable guard first, then
// v-add-sys-firewall as a job; turning off runs v-delete-sys-firewall as a job. The
// config cache is cleared afterwards (FIREWALL_SYSTEM feeds the feature flags).
//
// Checking in the request and applying in the job is safe (M5): while the firewall is
// off every rule, ban and ipset write is refused by the core (FIREWALL_SYSTEM is not
// enabled, exit 11), so the rules the guard checked are the rules v-add-sys-firewall
// applies; and v-update-firewall rewrites the SSH rule to the live sshd ports at that
// moment, which is what the guard assumed. Only one server job runs at a time.
func (sys *sysAPI) netFwToggle(w http.ResponseWriter, r *http.Request) {
	var req FirewallToggleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	title, cmd := "Turn off the firewall", "v-delete-sys-firewall"
	if req.Enabled {
		title, cmd = "Turn on the firewall", "v-add-sys-firewall"
		if err := sys.checkFirewallEnable(r.Context(), r); err != nil {
			var f *jobs.Failure
			if errors.As(err, &f) {
				sysNetFailure(w, http.StatusConflict, f)
			} else {
				writeCommandError(w, err)
			}
			return
		}
	}
	sys.startJob(w, r, title, func(ctx context.Context, j *jobs.Job) (any, error) {
		defer sys.H.InvalidateConfig()
		if _, err := sys.step(ctx, j, title, sysTimeoutDefault, cmd); err != nil {
			return nil, err
		}
		return OK{Status: "ok"}, nil
	})
}

// netBans reads the ban list, keeping both chains of an IP (decision 43).
func (sys *sysAPI) netBans(ctx context.Context) ([]hestia.FirewallBan, error) {
	out, err := sys.netRead(ctx, "v-list-firewall-ban", "json")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return []hestia.FirewallBan{}, nil
	}
	return hestia.DecodeBans(out)
}

// netBanList is GET /firewall/bans, newest first.
func (sys *sysAPI) netBanList(w http.ResponseWriter, r *http.Request) {
	bans, err := sys.netBans(r.Context())
	if err != nil {
		writeCommandError(w, err)
		return
	}
	slices.Reverse(bans)
	if bans == nil {
		bans = []hestia.FirewallBan{}
	}
	writeJSON(w, http.StatusOK, BansView{Rows: bans})
}

// netBanAdd is POST /firewall/bans. A ban covering the caller is refused; after the
// add the list is read again, and a pair the core silently skipped is a 409
// (decision 43, FW gotcha 22).
func (sys *sysAPI) netBanAdd(w http.ResponseWriter, r *http.Request) {
	var req BanRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.IP = strings.TrimSpace(req.IP)
	req.Chain = strings.ToUpper(strings.TrimSpace(req.Chain))
	if !blankFields(w, "Banlist", req.Chain, "IP Address", req.IP) {
		return
	}
	switch {
	case !hestia.ValidChain(req.Chain):
		sysNetInvalid(w, sysNetInvalidMsg, "Banlist")
		return
	case !hestia.ValidCIDR4(req.IP):
		sysNetInvalid(w, sysNetInvalidIPMsg, req.IP)
		return
	}
	callers, _ := sysFwCallers(r)
	if f := sysFwBanLockout(req.IP, callers); f != nil {
		sysNetFailure(w, http.StatusConflict, f)
		return
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	if err := sys.netRun(r.Context(), "v-add-firewall-ban", req.IP, req.Chain); err != nil {
		writeCommandError(w, err)
		return
	}
	bans, err := sys.netBans(r.Context())
	if err != nil {
		writeCommandError(w, err)
		return
	}
	if !slices.ContainsFunc(bans, func(b hestia.FirewallBan) bool { return b.IP == req.IP && b.CHAIN == req.Chain }) {
		WriteError(w, http.StatusConflict, "not_banned", sysFwBanMissingMsg)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// netBanDelete is POST /firewall/bans/delete with 1–500 (ip, chain) pairs; every pair
// is tried and failures are listed by "ip chain".
func (sys *sysAPI) netBanDelete(w http.ResponseWriter, r *http.Request) {
	var req BanDeleteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.Items) == 0 || len(req.Items) > maxBulk {
		bulkCount(w, maxBulk)
		return
	}
	for _, it := range req.Items {
		if !hestia.ValidCIDR4(it.IP) {
			invalidFormat(w, "ip", it.IP)
			return
		}
		if !hestia.ValidChain(it.Chain) {
			invalidFormat(w, "chain", it.Chain)
			return
		}
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	failed := []BulkFailure{}
	for _, it := range req.Items {
		if err := sys.netRun(r.Context(), "v-delete-firewall-ban", it.IP, it.Chain); err != nil {
			failed = append(failed, BulkFailure{Name: it.IP + " " + it.Chain, Message: errorMessage(err)})
		}
	}
	writeJSON(w, http.StatusOK, BulkReply{Status: "ok", Failed: failed})
}

// netIpsetList is GET /firewall/ipsets, by name. While the firewall is off it runs
// nothing (the core would exit 11) and shows the empty state (decision 44).
func (sys *sysAPI) netIpsetList(w http.ResponseWriter, r *http.Request) {
	enabled, _, _, err := sys.netFwState(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	view := IpsetsView{Enabled: enabled, Rows: []hestia.FirewallIpset{}}
	if enabled {
		out, err := sys.netRead(r.Context(), "v-list-firewall-ipset", "json")
		if err != nil {
			writeCommandError(w, err)
			return
		}
		if view.Rows, err = hestia.DecodeOptional[hestia.FirewallIpset](out); err != nil {
			serverError(w, err)
			return
		}
		slices.SortStableFunc(view.Rows, func(a, b hestia.FirewallIpset) int { return strings.Compare(a.LISTNAME, b.LISTNAME) })
	}
	writeJSON(w, http.StatusOK, view)
}

// sysFwValidIpsetSource accepts an http(s) URL with a host, or the shipped blacklist
// script (decision 44). The core stores the source in a shell-sourced file, so quotes,
// backslashes, "$", backticks, whitespace and control characters are refused.
func sysFwValidIpsetSource(src string) bool {
	if src == sysFwBlacklistSource {
		return true
	}
	if len(src) > 255 || strings.ContainsAny(src, "'\"\\$`") {
		return false
	}
	for _, c := range src {
		if c <= ' ' || c == 0x7f {
			return false
		}
	}
	u, err := url.Parse(src)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// netIpsetAdd is POST /firewall/ipsets: validate, then v-add-firewall-ipset as a job
// with the download timeout (3 min).
func (sys *sysAPI) netIpsetAdd(w http.ResponseWriter, r *http.Request) {
	var req IpsetRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Source = strings.TrimSpace(req.Source)
	if !blankFields(w, "Name", req.Name, "Data Source", req.Source, "IP Version", req.IPVersion) {
		return
	}
	switch {
	case !hestia.ValidIpsetName(req.Name):
		sysNetInvalid(w, sysNetInvalidMsg, "Name")
		return
	case !sysFwValidIpsetSource(req.Source):
		sysNetInvalid(w, sysFwInvalidSourceMsg, req.Source)
		return
	case req.IPVersion != "v4" && req.IPVersion != "v6":
		sysNetInvalid(w, sysNetInvalidMsg, "IP Version")
		return
	}
	auto := "no"
	if req.Autoupdate {
		auto = "yes"
	}
	args := []string{req.Name, req.Source, req.IPVersion, auto}
	sys.startJob(w, r, "Add IP list", func(ctx context.Context, j *jobs.Job) (any, error) {
		if _, err := sys.step(ctx, j, "Add IP list", sysTimeoutDownload, "v-add-firewall-ipset", args...); err != nil {
			return nil, err
		}
		return OK{Status: "ok"}, nil
	})
}

// netIpsetBulk is POST /firewall/ipsets/bulk (action "delete"); every set is tried.
func (sys *sysAPI) netIpsetBulk(w http.ResponseWriter, r *http.Request) {
	var req BulkRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !bulkShapeOK(w, req.Action == "delete", req.Action, len(req.Names), maxBulk) {
		return
	}
	for _, n := range req.Names {
		if !hestia.ValidIpsetName(n) {
			invalidFormat(w, "name", n)
			return
		}
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	failed := []BulkFailure{}
	for _, n := range req.Names {
		if err := sys.netRun(r.Context(), "v-delete-firewall-ipset", n); err != nil {
			failed = append(failed, BulkFailure{Name: n, Message: errorMessage(err)})
		}
	}
	writeJSON(w, http.StatusOK, BulkReply{Status: "ok", Failed: failed})
}

// netIpsetDelete is DELETE /firewall/ipsets/{name} (no body).
func (sys *sysAPI) netIpsetDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !hestia.ValidIpsetName(name) {
		invalidFormat(w, "name", name)
		return
	}
	sys.fwMu.Lock()
	defer sys.fwMu.Unlock()
	if err := sys.netRun(r.Context(), "v-delete-firewall-ipset", name); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

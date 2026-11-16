package httpapi

// Firewall lockout guards (decision 37; FW gotchas 2, 5, 32). These are pure: the
// handlers in sys_firewall.go read fresh data under sysAPI.fwMu and pass it in. Each
// guard returns nil to allow, or the 409 lockout failure to answer with. There is no
// override; an admin who means it uses SSH.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"strconv"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
)

// Lockout msgids (English, translated by the UI). SysLockoutFirewallMsg, for turning
// the firewall on, lives in types_sys.go.
const (
	sysFwLockoutProtectedMsg  = "The panel and SSH rules can't be deleted, suspended, moved or changed."
	sysFwLockoutCallerMsg     = "This rule would block your own IP address (%s)."
	sysFwLockoutAllMsg        = "This rule would block all traffic to the server."
	sysFwLockoutPanelMsg      = "This rule would block the panel port (%s)."
	sysFwLockoutSSHMsg        = "This rule would block the SSH port (%s)."
	sysFwLockoutBanMsg        = "This ban would block your own IP address (%s)."
	sysFwLockoutReservedMsg   = "The comments HESTIA and SSH are reserved for the panel and SSH rules."
	sysFwLockoutUnknownMsg    = "Your IP address can't be determined, so this change can't be checked against it."
	sysFwLockoutClosePanelMsg = "This change would leave no active rule that opens the panel port (%s) to you."
	sysFwLockoutCloseSSHMsg   = "This change would leave no active rule that opens the SSH port (%s) to you."
	sysFwLockoutUnmodelledMsg = "The rules that let you reach the panel can't be identified, so a rule that may open it can't be deleted, suspended or changed here."
	sysFwLockoutNoSSHPortMsg  = "The SSH port can't be determined, so this change can't be checked against it."
)

// sysFwEnv is what the guards check a rule against.
//   - Callers: every address the caller may reach the server as: session.RealIP (the
//     visitor, CF-Connecting-IP behind Cloudflare) and the transport peer (X-Real-IP
//     from the proxy socket, else RemoteAddr). A DROP or ban covering any of them is
//     refused. An empty or unparsable entry makes the caller unknown (sysFwUnknown).
//   - Peer: the address iptables actually sees; the enable guard needs an ACCEPT for it.
//   - PanelPort: BACKEND_PORT, read fresh; SSHPorts: v-list-sys-sshd-port plain.
type sysFwEnv struct {
	Callers   []string
	Peer      string
	PanelPort string
	SSHPorts  []string
}

// sysFwUnknown says whether the caller can't be checked: no address, or one that
// doesn't parse (a proxied request without a usable X-Real-IP). The guards then fail
// closed: narrow DROPs and every ban are refused (M1).
func sysFwUnknown(callers []string) bool {
	if len(callers) == 0 {
		return true
	}
	for _, c := range callers {
		if _, err := netip.ParseAddr(c); err != nil {
			return true
		}
	}
	return false
}

// sysFwCoveredCaller is the first caller cidr covers, or "".
func sysFwCoveredCaller(cidr string, callers []string) string {
	for _, c := range callers {
		if sysFwCIDRCovers(cidr, c) {
			return c
		}
	}
	return ""
}

func sysFwLockout(msg string, args ...string) *jobs.Failure {
	return &jobs.Failure{Code: SysLockoutCode, Message: msg, Args: args}
}

// sysFwProtected is "panel" for the rule commented HESTIA, "ssh" for SSH, else "".
// The core finds both rules by their comment (v-change-sys-port, v-update-firewall).
func sysFwProtected(comment string) string {
	switch comment {
	case "HESTIA":
		return "panel"
	case "SSH":
		return "ssh"
	}
	return ""
}

// sysFwExpect fingerprints what the admin saw of a rule (decision 38):
// ACTION|PROTOCOL|PORT|IP|COMMENT|SUSPENDED, as 16 hex digits. The id is not part
// of it, so a renumbered rule still matches itself.
func sysFwExpect(r hestia.FirewallRule) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{r.ACTION, r.PROTOCOL, r.PORT, r.IP, r.COMMENT, r.SUSPENDED}, "|")))
	return hex.EncodeToString(sum[:8])
}

// sysFwPrefix parses a rule or ban address (a.b.c.d or a.b.c.d/n); ok is false for
// ipset: sources and anything else that isn't IPv4.
func sysFwPrefix(cidr string) (netip.Prefix, bool) {
	if !strings.Contains(cidr, "/") {
		cidr += "/32"
	}
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() {
		return netip.Prefix{}, false
	}
	return p.Masked(), true
}

// sysFwCIDRCovers says whether the rule or ban address cidr matches addr. An
// ipset: source is never known to cover it (its contents aren't read).
func sysFwCIDRCovers(cidr, addr string) bool {
	p, ok := sysFwPrefix(cidr)
	if !ok {
		return false
	}
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	a = a.Unmap()
	return a.Is4() && p.Contains(a)
}

// sysFwEveryone says whether cidr is 0.0.0.0/0 (any /0).
func sysFwEveryone(cidr string) bool {
	p, ok := sysFwPrefix(cidr)
	return ok && p.Bits() == 0
}

// sysFwPortCovers says whether the core port text ports ("0" for all, or a comma list
// of ports and a-b ranges) includes port.
func sysFwPortCovers(ports, port string) bool {
	if ports == "0" {
		return true
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	for _, item := range strings.Split(ports, ",") {
		lo, hi, isRange := strings.Cut(item, "-")
		if !isRange {
			hi = lo
		}
		a, errA := strconv.Atoi(strings.TrimSpace(lo))
		b, errB := strconv.Atoi(strings.TrimSpace(hi))
		if errA == nil && errB == nil && a <= n && n <= b {
			return true
		}
	}
	return false
}

// sysFwDropLockout refuses a DROP rule that would cut off the server: one for
// everyone (a /0 source) on every port, one for everyone on the panel or an sshd port
// (TCP), or one whose narrower source covers a caller, on any port.
//
// The asymmetry is deliberate (M7, ruled OK): a narrow source covering the caller is
// refused on every port, because the admin is blocking their own network; a /0 DROP
// on another port (say 25) is allowed, because it covers the caller only by covering
// everyone, and for /0 only the three cases above lock anyone out.
//
// ICMP rules can't block either port, so they pass. ipset: sources are never counted
// as covering a caller (M2): their contents live in kernel sets no v-* reads back,
// and checking them needs a core change (Plan 6).
func sysFwDropLockout(action, protocol, ip, port string, env sysFwEnv) *jobs.Failure {
	protocol = strings.ToUpper(protocol)
	if strings.ToUpper(action) != "DROP" || protocol == "ICMP" {
		return nil
	}
	everyone := sysFwEveryone(ip)
	if everyone && port == "0" {
		return sysFwLockout(sysFwLockoutAllMsg)
	}
	if everyone && protocol == "TCP" {
		if sysFwPortCovers(port, env.PanelPort) {
			return sysFwLockout(sysFwLockoutPanelMsg, env.PanelPort)
		}
		for _, p := range env.SSHPorts {
			if sysFwPortCovers(port, p) {
				return sysFwLockout(sysFwLockoutSSHMsg, p)
			}
		}
	}
	if _, cidr := sysFwPrefix(ip); everyone || !cidr {
		return nil
	}
	if sysFwUnknown(env.Callers) {
		return sysFwLockout(sysFwLockoutUnknownMsg)
	}
	if c := sysFwCoveredCaller(ip, env.Callers); c != "" {
		return sysFwLockout(sysFwLockoutCallerMsg, c)
	}
	return nil
}

// sysFwReservedCommentLockout refuses giving a rule the comment HESTIA or SSH unless
// it is that protected rule already (cur nil for an add). The core rewrites every rule
// commented SSH to the sshd ports (v-update-firewall) and replaces every rule
// commented HESTIA with the panel rule (v-change-sys-port), so a "DROP 25 SSH" rule
// would become a DROP on the SSH port (C1). The core's sed matches the exact,
// case-sensitive text, so only these two spellings are reserved.
func sysFwReservedCommentLockout(cur *hestia.FirewallRule, comment string) *jobs.Failure {
	if sysFwProtected(comment) == "" || (cur != nil && cur.COMMENT == comment) {
		return nil
	}
	return sysFwLockout(sysFwLockoutReservedMsg)
}

// sysFwProtectedLockout refuses deleting, suspending or moving the panel or SSH rule.
func sysFwProtectedLockout(cur hestia.FirewallRule) *jobs.Failure {
	if sysFwProtected(cur.COMMENT) != "" {
		return sysFwLockout(sysFwLockoutProtectedMsg)
	}
	return nil
}

// sysFwProtectedEditLockout refuses an edit of the panel or SSH rule that changes its
// action, protocol, IP, port or comment (req is already normalised).
func sysFwProtectedEditLockout(cur hestia.FirewallRule, req FirewallRuleRequest) *jobs.Failure {
	if sysFwProtected(cur.COMMENT) == "" {
		return nil
	}
	if !strings.EqualFold(req.Action, cur.ACTION) || !strings.EqualFold(req.Protocol, cur.PROTOCOL) ||
		req.IP != cur.IP || req.Port != cur.PORT || req.Comment != cur.COMMENT {
		return sysFwLockout(sysFwLockoutProtectedMsg)
	}
	return nil
}

// sysFwBanLockout refuses a ban whose address covers a caller, and every ban while
// the caller is unknown.
func sysFwBanLockout(ip string, callers []string) *jobs.Failure {
	if sysFwUnknown(callers) {
		return sysFwLockout(sysFwLockoutUnknownMsg)
	}
	if c := sysFwCoveredCaller(ip, callers); c != "" {
		return sysFwLockout(sysFwLockoutBanMsg, c)
	}
	return nil
}

// sysFwEnableLockout refuses turning the firewall on unless active (not suspended)
// TCP ACCEPT rules, for everyone or for the address each port is reached from (the
// peer iptables sees for the panel, the visitor for SSH; sysFwOpens), cover the panel
// port and every sshd port (an ipset: source doesn't count), and no active DROP is one the DROP guard would refuse (M3;
// whatever its place in the chain, to stay simple and safe). The rule commented SSH
// counts for the sshd ports whatever its port, because v-update-firewall rewrites it
// to the live ports.
//
// No rules at all is refused (M6). That includes an empty $HESTIA/data/firewall, where
// v-add-sys-firewall would copy the shipped defaults: v-list-firewall can't tell the
// two apart, so such a server is turned on over SSH.
func sysFwEnableLockout(rules []hestia.FirewallRule, env sysFwEnv) *jobs.Failure {
	refuse := &jobs.Failure{Code: SysLockoutCode, Message: SysLockoutFirewallMsg}
	if sysFwActiveDropLockout(rules, env) != nil {
		return refuse
	}
	ok := sysFwOpens(rules, env, env.PanelPort, false, false)
	for _, p := range env.SSHPorts {
		ok = ok && sysFwOpens(rules, env, p, true, false)
	}
	if !ok {
		return refuse
	}
	return nil
}

// sysFwActiveDropLockout is the failure of the first active DROP the DROP guard
// would refuse, whatever its place in the chain, or nil. A rule commented SSH or
// HESTIA (one made over the CLI) ends up on the sshd ports (v-update-firewall
// rewrites its PORT) or the panel port (v-change-sys-port replaces it), so a DROP is
// checked there too.
func sysFwActiveDropLockout(rules []hestia.FirewallRule, env sysFwEnv) *jobs.Failure {
	for _, r := range rules {
		if r.SUSPENDED == "yes" {
			continue
		}
		ports := []string{r.PORT}
		switch r.COMMENT {
		case "SSH":
			ports = append(ports, strings.Join(env.SSHPorts, ","))
		case "HESTIA":
			ports = append(ports, env.PanelPort)
		}
		for _, port := range ports {
			if port == "" {
				continue
			}
			if f := sysFwDropLockout(r.ACTION, r.PROTOCOL, r.IP, port, env); f != nil {
				return f
			}
		}
	}
	return nil
}

// sysFwMoveLockout refuses moving a rule while an active DROP fails the DROP guard
// (fix round 1, M4): a new order can put a CLI-made DROP ahead of the rules that open
// the panel or SSH.
func sysFwMoveLockout(rules []hestia.FirewallRule, env sysFwEnv) *jobs.Failure {
	return sysFwActiveDropLockout(rules, env)
}

// sysFwOpens says whether an active TCP ACCEPT rule opens port to the caller: one
// for everyone, or one covering the address the port is reached from. The panel is
// reached from the peer iptables sees (behind Cloudflare, the edge); SSH (sshRule)
// straight from the visitor, Callers[0] (I2). With sshRule, the rule commented SSH
// counts whatever its PORT, because v-update-firewall rewrites it to the live sshd
// ports.
//
// An ipset: source may or may not hold the caller (its contents live in the kernel):
// with maybe it counts as open, without it as closed, so the keep-open guard treats
// it as open before a change and closed after it (I1).
func sysFwOpens(rules []hestia.FirewallRule, env sysFwEnv, port string, sshRule, maybe bool) bool {
	from := env.Peer
	if sshRule {
		from = ""
		if len(env.Callers) > 0 {
			from = env.Callers[0]
		}
	}
	for _, r := range rules {
		if r.ACTION != "ACCEPT" || r.SUSPENDED == "yes" || r.PROTOCOL != "TCP" {
			continue
		}
		ipset := strings.HasPrefix(r.IP, "ipset:")
		if !sysFwEveryone(r.IP) && !sysFwCIDRCovers(r.IP, from) && !(maybe && ipset) {
			continue
		}
		if sysFwPortCovers(r.PORT, port) || (sshRule && r.COMMENT == "SSH") {
			return true
		}
	}
	return false
}

// sysFwMayOpen says whether rule r can be what keeps the panel or an sshd port open
// (an active TCP ACCEPT, from any source): only a change to such a rule needs
// sysFwKeepOpenLockout.
func sysFwMayOpen(r hestia.FirewallRule) bool {
	return r.ACTION == "ACCEPT" && r.SUSPENDED != "yes" && r.PROTOCOL == "TCP"
}

// sysFwKeepOpenLockout refuses a delete, suspend or edit (D5) that turns the active
// rules before into after when, before, the panel port or an sshd port was open to
// the caller (sysFwOpens) and, after, it no longer is. It looks at what the rules
// do, not at their comments, so it still holds once the HESTIA and SSH rules are gone
// (deleted over the CLI): deleting the last opening rule would otherwise let
// v-update-firewall close SSH and the panel.
//
// The caller reached the panel through the firewall, so a panel port the rules
// before don't open means the model is missing something (lo, a rule it can't
// read): the change is refused (I1). An sshd port that was already closed to the
// caller (SSH kept to an office network) is not checked. Call it only while the
// firewall is on.
func sysFwKeepOpenLockout(before, after []hestia.FirewallRule, env sysFwEnv) *jobs.Failure {
	if !sysFwOpens(before, env, env.PanelPort, false, true) {
		return sysFwLockout(sysFwLockoutUnmodelledMsg)
	}
	if !sysFwOpens(after, env, env.PanelPort, false, false) {
		return sysFwLockout(sysFwLockoutClosePanelMsg, env.PanelPort)
	}
	for _, p := range env.SSHPorts {
		if sysFwOpens(before, env, p, true, true) && !sysFwOpens(after, env, p, true, false) {
			return sysFwLockout(sysFwLockoutCloseSSHMsg, p)
		}
	}
	return nil
}

package httpapi

// Slice D: the pure firewall lockout guards (decision 37), tested without commands.

import (
	"slices"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
)

func TestSysFwCIDRCovers(t *testing.T) {
	cases := []struct {
		cidr, addr string
		want       bool
	}{
		{"0.0.0.0/0", "192.0.2.50", true},
		{"192.0.2.0/24", "192.0.2.50", true},
		{"192.0.2.50", "192.0.2.50", true},
		{"192.0.2.50/32", "192.0.2.50", true},
		{"192.0.2.51", "192.0.2.50", false},
		{"198.51.100.0/24", "192.0.2.50", false},
		{"192.0.2.77/24", "192.0.2.50", true}, // host bits set: the core keeps them, iptables masks them
		{"ipset:blacklist", "192.0.2.50", false},
		{"0.0.0.0/0", "::1", false},
		{"0.0.0.0/0", "", false},
		{"garbage", "192.0.2.50", false},
	}
	for _, c := range cases {
		if got := sysFwCIDRCovers(c.cidr, c.addr); got != c.want {
			t.Errorf("sysFwCIDRCovers(%q, %q) = %v, want %v", c.cidr, c.addr, got, c.want)
		}
	}
}

func TestSysFwPortCovers(t *testing.T) {
	cases := []struct {
		ports, port string
		want        bool
	}{
		{"0", "8083", true},
		{"8083", "8083", true},
		{"80,443,8083", "8083", true},
		{"8080-8090", "8083", true},
		{"21,12000-12100", "12050", true},
		{"80,443", "8083", false},
		{"8084-8090", "8083", false},
		{"22", "2222", false},
	}
	for _, c := range cases {
		if got := sysFwPortCovers(c.ports, c.port); got != c.want {
			t.Errorf("sysFwPortCovers(%q, %q) = %v, want %v", c.ports, c.port, got, c.want)
		}
	}
}

func sysWantLockout(t *testing.T, name string, f *jobs.Failure, msg string, args ...string) {
	t.Helper()
	if f == nil {
		t.Errorf("%s: no lockout, want %q", name, msg)
		return
	}
	if f.Code != SysLockoutCode || f.Message != msg || !slices.Equal(f.Args, args) {
		t.Errorf("%s: %+v, want %q %v", name, f, msg, args)
	}
}

func TestSysFwDropLockout(t *testing.T) {
	env := sysFwEnv{Callers: []string{"192.0.2.50"}, Peer: "192.0.2.50", PanelPort: "8083", SSHPorts: []string{"22", "2222"}}
	type rule struct{ action, protocol, ip, port string }
	refused := []struct {
		name string
		r    rule
		msg  string
		args []string
	}{
		{"everything", rule{"DROP", "TCP", "0.0.0.0/0", "0"}, sysFwLockoutAllMsg, nil},
		{"everything, udp", rule{"DROP", "UDP", "0.0.0.0/0", "0"}, sysFwLockoutAllMsg, nil},
		{"lowercase action", rule{"drop", "tcp", "0.0.0.0/0", "0"}, sysFwLockoutAllMsg, nil},
		{"panel port", rule{"DROP", "TCP", "0.0.0.0/0", "8080-8090"}, sysFwLockoutPanelMsg, []string{"8083"}},
		{"second sshd port", rule{"DROP", "TCP", "0.0.0.0/0", "2222"}, sysFwLockoutSSHMsg, []string{"2222"}},
		{"caller, one port", rule{"DROP", "TCP", "192.0.2.0/24", "25"}, sysFwLockoutCallerMsg, []string{"192.0.2.50"}},
		{"caller, exact", rule{"DROP", "UDP", "192.0.2.50", "53"}, sysFwLockoutCallerMsg, []string{"192.0.2.50"}},
	}
	for _, c := range refused {
		sysWantLockout(t, c.name, sysFwDropLockout(c.r.action, c.r.protocol, c.r.ip, c.r.port, env), c.msg, c.args...)
	}
	allowed := []struct {
		name string
		r    rule
	}{
		{"accept everything", rule{"ACCEPT", "TCP", "0.0.0.0/0", "0"}},
		{"icmp everywhere", rule{"DROP", "ICMP", "0.0.0.0/0", "0"}},
		{"other port", rule{"DROP", "TCP", "0.0.0.0/0", "25"}},
		{"udp panel port", rule{"DROP", "UDP", "0.0.0.0/0", "8083"}},
		{"attacker on the panel port", rule{"DROP", "TCP", "198.51.100.0/24", "8083"}},
		{"ipset", rule{"DROP", "TCP", "ipset:blacklist", "0"}},
	}
	for _, c := range allowed {
		if f := sysFwDropLockout(c.r.action, c.r.protocol, c.r.ip, c.r.port, env); f != nil {
			t.Errorf("%s: %+v, want allowed", c.name, f)
		}
	}

	// Behind Cloudflare iptables sees the edge (the peer), not CF-Connecting-IP: both
	// count.
	cf := sysFwEnv{Callers: []string{"192.0.2.50", "173.245.48.5"}, Peer: "173.245.48.5", PanelPort: "8083", SSHPorts: []string{"22"}}
	sysWantLockout(t, "cloudflare edge", sysFwDropLockout("DROP", "TCP", "173.245.48.0/20", "443", cf), sysFwLockoutCallerMsg, "173.245.48.5")
	sysWantLockout(t, "cloudflare visitor", sysFwDropLockout("DROP", "TCP", "192.0.2.0/24", "443", cf), sysFwLockoutCallerMsg, "192.0.2.50")

	// An unknown caller fails closed for narrow sources (M1).
	for _, unknown := range []sysFwEnv{
		{Callers: []string{""}, PanelPort: "8083", SSHPorts: []string{"22"}},
		{Callers: []string{"192.0.2.50", "garbage"}, Peer: "garbage", PanelPort: "8083", SSHPorts: []string{"22"}},
		{PanelPort: "8083", SSHPorts: []string{"22"}},
	} {
		sysWantLockout(t, "unknown caller", sysFwDropLockout("DROP", "TCP", "198.51.100.0/24", "25", unknown), sysFwLockoutUnknownMsg)
		sysWantLockout(t, "unknown caller, all", sysFwDropLockout("DROP", "TCP", "0.0.0.0/0", "0", unknown), sysFwLockoutAllMsg)
		for name, r := range map[string]rule{
			"/0 other port": {"DROP", "TCP", "0.0.0.0/0", "25"},
			"icmp":          {"DROP", "ICMP", "198.51.100.0/24", "0"},
			"ipset":         {"DROP", "TCP", "ipset:blacklist", "0"},
			"accept":        {"ACCEPT", "TCP", "198.51.100.0/24", "25"},
		} {
			if f := sysFwDropLockout(r.action, r.protocol, r.ip, r.port, unknown); f != nil {
				t.Errorf("unknown caller, %s: %+v", name, f)
			}
		}
	}
}

func TestSysFwReservedComment(t *testing.T) {
	ssh := sysFwTestRule("10", "ACCEPT", "TCP", "22", "0.0.0.0/0", "SSH", "no")
	panel := sysFwTestRule("2", "ACCEPT", "TCP", "8083", "0.0.0.0/0", "HESTIA", "no")
	web := sysFwTestRule("9", "ACCEPT", "TCP", "80,443", "0.0.0.0/0", "WEB", "no")
	for _, c := range []string{"SSH", "HESTIA"} {
		sysWantLockout(t, "add "+c, sysFwReservedCommentLockout(nil, c), sysFwLockoutReservedMsg)
		sysWantLockout(t, "edit web to "+c, sysFwReservedCommentLockout(&web, c), sysFwLockoutReservedMsg)
	}
	sysWantLockout(t, "ssh rule to HESTIA", sysFwReservedCommentLockout(&ssh, "HESTIA"), sysFwLockoutReservedMsg)
	for name, f := range map[string]*jobs.Failure{
		"ssh rule keeps SSH":      sysFwReservedCommentLockout(&ssh, "SSH"),
		"panel rule keeps HESTIA": sysFwReservedCommentLockout(&panel, "HESTIA"),
		"lowercase ssh":           sysFwReservedCommentLockout(nil, "ssh"), // the core's sed is case-sensitive
		"Hestia":                  sysFwReservedCommentLockout(nil, "Hestia"),
		"SSH access":              sysFwReservedCommentLockout(nil, "SSH access"),
		"no comment":              sysFwReservedCommentLockout(nil, ""),
	} {
		if f != nil {
			t.Errorf("%s: %+v", name, f)
		}
	}
}

func sysFwTestRule(id, action, proto, port, ip, comment, suspended string) hestia.FirewallRule {
	return hestia.FirewallRule{RULE: id, ACTION: action, PROTOCOL: proto, PORT: port, IP: ip, COMMENT: comment, SUSPENDED: suspended}
}

func TestSysFwProtected(t *testing.T) {
	if sysFwProtected("HESTIA") != "panel" || sysFwProtected("SSH") != "ssh" || sysFwProtected("WEB") != "" || sysFwProtected("") != "" {
		t.Fatal("sysFwProtected")
	}
	panel := sysFwTestRule("2", "ACCEPT", "TCP", "8083", "0.0.0.0/0", "HESTIA", "no")
	ssh := sysFwTestRule("10", "ACCEPT", "TCP", "22", "0.0.0.0/0", "SSH", "no")
	web := sysFwTestRule("9", "ACCEPT", "TCP", "80,443", "0.0.0.0/0", "WEB", "no")
	sysWantLockout(t, "panel", sysFwProtectedLockout(panel), sysFwLockoutProtectedMsg)
	sysWantLockout(t, "ssh", sysFwProtectedLockout(ssh), sysFwLockoutProtectedMsg)
	if f := sysFwProtectedLockout(web); f != nil {
		t.Errorf("web: %+v", f)
	}

	same := FirewallRuleRequest{Action: "ACCEPT", Protocol: "TCP", Port: "22", IP: "0.0.0.0/0", Comment: "SSH"}
	if f := sysFwProtectedEditLockout(ssh, same); f != nil {
		t.Errorf("unchanged ssh edit: %+v", f)
	}
	for name, req := range map[string]FirewallRuleRequest{
		"port":     {Action: "ACCEPT", Protocol: "TCP", Port: "2200", IP: "0.0.0.0/0", Comment: "SSH"},
		"action":   {Action: "DROP", Protocol: "TCP", Port: "22", IP: "0.0.0.0/0", Comment: "SSH"},
		"protocol": {Action: "ACCEPT", Protocol: "UDP", Port: "22", IP: "0.0.0.0/0", Comment: "SSH"},
		"ip":       {Action: "ACCEPT", Protocol: "TCP", Port: "22", IP: "192.0.2.0/24", Comment: "SSH"},
		"comment":  {Action: "ACCEPT", Protocol: "TCP", Port: "22", IP: "0.0.0.0/0", Comment: "Shell"},
	} {
		sysWantLockout(t, "ssh "+name, sysFwProtectedEditLockout(ssh, req), sysFwLockoutProtectedMsg)
	}
	if f := sysFwProtectedEditLockout(web, FirewallRuleRequest{Action: "DROP", Protocol: "TCP", Port: "80", IP: "0.0.0.0/0", Comment: "HTTP"}); f != nil {
		t.Errorf("web edit: %+v", f)
	}
}

func TestSysFwBanLockout(t *testing.T) {
	one := []string{"192.0.2.50"}
	sysWantLockout(t, "caller /24", sysFwBanLockout("192.0.2.0/24", one), sysFwLockoutBanMsg, "192.0.2.50")
	sysWantLockout(t, "caller", sysFwBanLockout("192.0.2.50", one), sysFwLockoutBanMsg, "192.0.2.50")
	if f := sysFwBanLockout("198.51.100.0/24", one); f != nil {
		t.Errorf("other net: %+v", f)
	}
	cf := []string{"192.0.2.50", "173.245.48.5"}
	sysWantLockout(t, "cloudflare edge", sysFwBanLockout("173.245.48.0/24", cf), sysFwLockoutBanMsg, "173.245.48.5")
	for _, unknown := range [][]string{{""}, {"192.0.2.50", "garbage"}, nil} {
		sysWantLockout(t, "unknown caller", sysFwBanLockout("198.51.100.0/24", unknown), sysFwLockoutUnknownMsg)
	}
}

func TestSysFwEnableLockout(t *testing.T) {
	env := sysFwEnv{Callers: []string{"192.0.2.50"}, Peer: "192.0.2.50", PanelPort: "8083", SSHPorts: []string{"22"}}
	panel := sysFwTestRule("2", "ACCEPT", "TCP", "8083", "0.0.0.0/0", "HESTIA", "no")
	ssh := sysFwTestRule("10", "ACCEPT", "TCP", "22", "0.0.0.0/0", "SSH", "no")
	web := sysFwTestRule("9", "ACCEPT", "TCP", "80,443", "0.0.0.0/0", "WEB", "no")

	if f := sysFwEnableLockout([]hestia.FirewallRule{web, panel, ssh}, env); f != nil {
		t.Errorf("defaults: %+v", f)
	}
	// The SSH rule follows the live sshd ports (v-update-firewall rewrites it).
	moved := env
	moved.SSHPorts = []string{"2222"}
	if f := sysFwEnableLockout([]hestia.FirewallRule{panel, ssh}, moved); f != nil {
		t.Errorf("ssh rule on another port: %+v", f)
	}
	// The caller's own address counts as well as 0.0.0.0/0.
	mine := sysFwTestRule("11", "ACCEPT", "TCP", "0", "192.0.2.0/24", "Office", "no")
	if f := sysFwEnableLockout([]hestia.FirewallRule{mine}, env); f != nil {
		t.Errorf("caller rule: %+v", f)
	}

	// Behind Cloudflare the panel ACCEPT must cover the peer iptables sees (the edge),
	// while SSH comes straight from the visitor (Callers[0]), not through the edge.
	cf := sysFwEnv{Callers: []string{"192.0.2.50", "173.245.48.5"}, Peer: "173.245.48.5", PanelPort: "8083", SSHPorts: []string{"22"}}
	visitorOnly := sysFwTestRule("11", "ACCEPT", "TCP", "0", "192.0.2.0/24", "Office", "no")
	edge := sysFwTestRule("12", "ACCEPT", "TCP", "0", "173.245.48.0/20", "Cloudflare", "no")
	sysWantLockout(t, "visitor only", sysFwEnableLockout([]hestia.FirewallRule{visitorOnly}, cf), SysLockoutFirewallMsg)
	sysWantLockout(t, "edge only", sysFwEnableLockout([]hestia.FirewallRule{edge}, cf), SysLockoutFirewallMsg)
	if f := sysFwEnableLockout([]hestia.FirewallRule{edge, visitorOnly}, cf); f != nil {
		t.Errorf("edge + visitor rule: %+v", f)
	}
	if f := sysFwEnableLockout([]hestia.FirewallRule{edge, ssh}, cf); f != nil {
		t.Errorf("edge + ssh rule: %+v", f)
	}

	refused := map[string][]hestia.FirewallRule{
		// An active DROP the DROP guard would refuse blocks, whatever its place (M3).
		"drop everything": {panel, ssh, sysFwTestRule("11", "DROP", "TCP", "0", "0.0.0.0/0", "Block", "no")},
		"drop caller":     {panel, ssh, sysFwTestRule("11", "DROP", "UDP", "53", "192.0.2.0/24", "Block", "no")},
		// v-update-firewall rewrites a rule commented SSH to the sshd ports, and
		// v-change-sys-port rewrites HESTIA to the panel port: check them there.
		"drop commented SSH":    {panel, ssh, sysFwTestRule("11", "DROP", "TCP", "25", "0.0.0.0/0", "SSH", "no")},
		"drop commented HESTIA": {panel, ssh, sysFwTestRule("11", "DROP", "TCP", "25", "0.0.0.0/0", "HESTIA", "no")},
		"no rules":              nil,
		"no panel rule":         {web, ssh},
		"panel suspended":       {sysFwTestRule("2", "ACCEPT", "TCP", "8083", "0.0.0.0/0", "HESTIA", "yes"), ssh},
		"panel moved":           {sysFwTestRule("2", "ACCEPT", "TCP", "8443", "0.0.0.0/0", "HESTIA", "no"), ssh},
		"panel udp":             {sysFwTestRule("2", "ACCEPT", "UDP", "8083", "0.0.0.0/0", "HESTIA", "no"), ssh},
		"panel other net":       {sysFwTestRule("2", "ACCEPT", "TCP", "8083", "198.51.100.0/24", "HESTIA", "no"), ssh},
		"no ssh rule":           {panel, web},
		"ssh suspended":         {panel, sysFwTestRule("10", "ACCEPT", "TCP", "22", "0.0.0.0/0", "SSH", "yes")},
		"ssh is a drop":         {panel, sysFwTestRule("10", "DROP", "TCP", "22", "0.0.0.0/0", "SSH", "no")},
		"panel is a drop":       {sysFwTestRule("2", "DROP", "TCP", "8083", "0.0.0.0/0", "HESTIA", "no"), ssh},
	}
	if f := sysFwEnableLockout([]hestia.FirewallRule{panel, ssh, sysFwTestRule("11", "DROP", "TCP", "0", "0.0.0.0/0", "Block", "yes")}, env); f != nil {
		t.Errorf("suspended drop: %+v", f)
	}
	for name, rules := range refused {
		sysWantLockout(t, name, sysFwEnableLockout(rules, env), SysLockoutFirewallMsg)
	}
}

func TestSysFwExpect(t *testing.T) {
	a := sysFwTestRule("4", "ACCEPT", "TCP", "143,993", "0.0.0.0/0", "IMAP", "no")
	b := a
	b.RULE = "5" // the id is not part of the fingerprint
	if sysFwExpect(a) != sysFwExpect(b) || len(sysFwExpect(a)) != 16 {
		t.Fatalf("expect = %q / %q", sysFwExpect(a), sysFwExpect(b))
	}
	for _, change := range []func(*hestia.FirewallRule){
		func(r *hestia.FirewallRule) { r.ACTION = "DROP" },
		func(r *hestia.FirewallRule) { r.PROTOCOL = "UDP" },
		func(r *hestia.FirewallRule) { r.PORT = "143" },
		func(r *hestia.FirewallRule) { r.IP = "192.0.2.0/24" },
		func(r *hestia.FirewallRule) { r.COMMENT = "IMAPS" },
		func(r *hestia.FirewallRule) { r.SUSPENDED = "yes" },
	} {
		c := a
		change(&c)
		if sysFwExpect(c) == sysFwExpect(a) {
			t.Errorf("fingerprint ignores a change: %+v", c)
		}
	}
}

// D5: a change that leaves the panel or an sshd port closed to the caller is refused,
// whatever the rules' comments; one that was already closed is not made worse.
func TestSysFwKeepOpenLockout(t *testing.T) {
	env := sysFwEnv{Callers: []string{"192.0.2.50"}, Peer: "192.0.2.50", PanelPort: "8083", SSHPorts: []string{"22"}}
	panel := sysFwTestRule("2", "ACCEPT", "TCP", "8083", "0.0.0.0/0", "HESTIA", "no")
	ssh := sysFwTestRule("10", "ACCEPT", "TCP", "22", "0.0.0.0/0", "SSH", "no")
	web := sysFwTestRule("9", "ACCEPT", "TCP", "80,443", "0.0.0.0/0", "WEB", "no")
	// The protected rules are gone (deleted over the CLI); a plain rule keeps both open.
	admin := sysFwTestRule("11", "ACCEPT", "TCP", "22,8083", "0.0.0.0/0", "Admin", "no")
	office := sysFwTestRule("12", "ACCEPT", "TCP", "0", "192.0.2.0/24", "Office", "no")
	rs := func(r ...hestia.FirewallRule) []hestia.FirewallRule { return r }
	suspended := func(r hestia.FirewallRule) hestia.FirewallRule { r.SUSPENDED = "yes"; return r }

	refused := []struct {
		name          string
		before, after []hestia.FirewallRule
		msg, arg      string
	}{
		{"delete last opener", rs(web, admin), rs(web), sysFwLockoutClosePanelMsg, "8083"},
		{"suspend last opener", rs(web, admin), rs(web, suspended(admin)), sysFwLockoutClosePanelMsg, "8083"},
		{"edit to another port", rs(admin), rs(sysFwTestRule("11", "ACCEPT", "TCP", "8083", "0.0.0.0/0", "Admin", "no")), sysFwLockoutCloseSSHMsg, "22"},
		{"edit to another net", rs(admin), rs(sysFwTestRule("11", "ACCEPT", "TCP", "22,8083", "198.51.100.0/24", "Admin", "no")), sysFwLockoutClosePanelMsg, "8083"},
		{"edit to udp", rs(admin), rs(sysFwTestRule("11", "ACCEPT", "UDP", "22,8083", "0.0.0.0/0", "Admin", "no")), sysFwLockoutClosePanelMsg, "8083"},
		{"bulk delete", rs(web, admin, office), rs(web), sysFwLockoutClosePanelMsg, "8083"},
		{"delete everything", rs(admin), nil, sysFwLockoutClosePanelMsg, "8083"},
		{"protected kept, ssh opener gone", rs(panel, admin), rs(panel), sysFwLockoutCloseSSHMsg, "22"},
	}
	for _, c := range refused {
		sysWantLockout(t, c.name, sysFwKeepOpenLockout(c.before, c.after, env), c.msg, c.arg)
	}

	allowed := []struct {
		name          string
		before, after []hestia.FirewallRule
	}{
		{"another opener remains", rs(web, admin, office), rs(web, office)},
		{"protected rules remain", rs(panel, ssh, admin), rs(panel, ssh)},
		{"unrelated rule", rs(web, admin), rs(admin)},
		{"was closed for ssh, panel kept", rs(panel, web), rs(panel)},
		// The rule commented SSH opens the sshd ports whatever its PORT.
		{"ssh rule on its old port", rs(panel, sysFwTestRule("10", "ACCEPT", "TCP", "2222", "0.0.0.0/0", "SSH", "no"), admin),
			rs(panel, sysFwTestRule("10", "ACCEPT", "TCP", "2222", "0.0.0.0/0", "SSH", "no"))},
	}
	for _, c := range allowed {
		if f := sysFwKeepOpenLockout(c.before, c.after, env); f != nil {
			t.Errorf("%s: %+v", c.name, f)
		}
	}

	// Behind Cloudflare only a rule covering the peer iptables sees (or everyone) counts.
	cf := sysFwEnv{Callers: []string{"192.0.2.50", "173.245.48.5"}, Peer: "173.245.48.5", PanelPort: "8083", SSHPorts: []string{"22"}}
	edge := sysFwTestRule("13", "ACCEPT", "TCP", "0", "173.245.48.0/20", "Cloudflare", "no")
	sysWantLockout(t, "edge opener", sysFwKeepOpenLockout(rs(edge, office), rs(office), cf), sysFwLockoutClosePanelMsg, "8083")
	// SSH is checked against the visitor: the edge rule doesn't open it (I2).
	ssh22 := sysFwTestRule("14", "ACCEPT", "TCP", "22", "0.0.0.0/0", "Shell", "no")
	sysWantLockout(t, "ssh behind cloudflare", sysFwKeepOpenLockout(rs(edge, ssh22), rs(edge), cf), sysFwLockoutCloseSSHMsg, "22")
}

// Fix round 1 (I1): an ipset: source may hold the caller, so an active TCP ACCEPT
// from one counts as open before the change and as closed after it; and a panel
// port the model calls closed (yet the caller reached it) means the model is wrong.
func TestSysFwKeepOpenUnmodelled(t *testing.T) {
	env := sysFwEnv{Callers: []string{"192.0.2.50"}, Peer: "192.0.2.50", PanelPort: "8083", SSHPorts: []string{"22"}}
	office := sysFwTestRule("11", "ACCEPT", "TCP", "0", "ipset:office", "Office", "no")
	web := sysFwTestRule("9", "ACCEPT", "TCP", "80,443", "0.0.0.0/0", "WEB", "no")
	rs := func(r ...hestia.FirewallRule) []hestia.FirewallRule { return r }
	suspended := office
	suspended.SUSPENDED = "yes"
	sysWantLockout(t, "delete ipset opener", sysFwKeepOpenLockout(rs(office, web), rs(web), env), sysFwLockoutClosePanelMsg, "8083")
	sysWantLockout(t, "suspend ipset opener", sysFwKeepOpenLockout(rs(office, web), rs(suspended, web), env), sysFwLockoutClosePanelMsg, "8083")
	// Its own edit can't be proved safe either.
	sysWantLockout(t, "edit ipset opener", sysFwKeepOpenLockout(rs(office), rs(office), env), sysFwLockoutClosePanelMsg, "8083")
	// Nothing modelled opens the panel: refuse rather than guess.
	sysWantLockout(t, "panel modelled closed", sysFwKeepOpenLockout(rs(web), nil, env), sysFwLockoutUnmodelledMsg)
	sysWantLockout(t, "unknown peer", sysFwKeepOpenLockout(rs(sysFwTestRule("12", "ACCEPT", "TCP", "0", "192.0.2.0/24", "Me", "no")), nil,
		sysFwEnv{Callers: []string{"garbage"}, Peer: "garbage", PanelPort: "8083", SSHPorts: []string{"22"}}), sysFwLockoutUnmodelledMsg)
}

// Fix round 1 (M4): a move can bring a CLI-made DROP ahead of the rules that open
// the panel or SSH, so a move is refused while any active DROP fails the DROP guard.
func TestSysFwMoveLockout(t *testing.T) {
	env := sysFwEnv{Callers: []string{"192.0.2.50"}, Peer: "192.0.2.50", PanelPort: "8083", SSHPorts: []string{"22"}}
	panel := sysFwTestRule("2", "ACCEPT", "TCP", "8083", "0.0.0.0/0", "HESTIA", "no")
	drop := sysFwTestRule("11", "DROP", "TCP", "22", "0.0.0.0/0", "Block", "no")
	sysWantLockout(t, "drop on ssh", sysFwMoveLockout([]hestia.FirewallRule{panel, drop}, env), sysFwLockoutSSHMsg, "22")
	sysWantLockout(t, "drop commented SSH", sysFwMoveLockout([]hestia.FirewallRule{panel,
		sysFwTestRule("12", "DROP", "TCP", "25", "0.0.0.0/0", "SSH", "no")}, env), sysFwLockoutSSHMsg, "22")
	drop.SUSPENDED = "yes"
	if f := sysFwMoveLockout([]hestia.FirewallRule{panel, drop, sysFwTestRule("13", "DROP", "TCP", "25", "198.51.100.0/24", "Spam", "no")}, env); f != nil {
		t.Errorf("harmless drops: %+v", f)
	}
}

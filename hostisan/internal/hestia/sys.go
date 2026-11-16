package hestia

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Server admin types (Plan 4). Each follows the key order of the core's own JSON
// (sysinfo, services, settings and firewall parity checklists, section 6). Every value
// is a string as Hestia prints it; where the object key names the value it becomes
// NAME (or the field the comment says). IP addresses use SysIP (sysip.go).

// SysInfo is `v-list-sys-info json`'s one entry (key "sysinfo"). UPTIME is minutes
// since boot; LOADAVERAGE is "0.10 / 0.08 / 0.03"; HESTIA is the core version and
// RELEASE the release branch.
type SysInfo struct {
	HOSTNAME    string `json:"HOSTNAME"`
	OS          string `json:"OS"`
	VERSION     string `json:"VERSION"`
	ARCH        string `json:"ARCH"`
	HESTIA      string `json:"HESTIA"`
	RELEASE     string `json:"RELEASE"`
	UPTIME      string `json:"UPTIME"`
	LOADAVERAGE string `json:"LOADAVERAGE"`
}

// SetName ignores the key: it is always "sysinfo".
func (s *SysInfo) SetName(string) {}

// DecodeSysInfo decodes `v-list-sys-info json`; an output without the entry is an error.
func DecodeSysInfo(stdout []byte) (SysInfo, error) {
	infos, err := DecodeObjects[SysInfo](stdout)
	if err != nil {
		return SysInfo{}, err
	}
	if len(infos) != 1 {
		return SysInfo{}, errors.New("hestia: v-list-sys-info: expected one entry")
	}
	return infos[0], nil
}

// SysService is one service from `v-list-sys-services json`; the key (the unit-ish
// name such as "nginx", "php8.3-fpm", "iptables", "web-terminal") becomes NAME. STATE
// is "running" or "stopped"; CPU is the summed ps pcpu ("0.3", may be ""); MEM is MB;
// RTIME is minutes of process uptime.
type SysService struct {
	NAME   string `json:"NAME"`
	SYSTEM string `json:"SYSTEM"`
	STATE  string `json:"STATE"`
	CPU    string `json:"CPU"`
	MEM    string `json:"MEM"`
	RTIME  string `json:"RTIME"`
}

func (s *SysService) SetName(name string) { s.NAME = name }

// SysConfigFile is the reply of `v-list-sys-{nginx,php,mysql,pgsql,dovecot,…}-config
// json`: {"CONFIG": {directive: value, …, "config_path": path}}. The keys per command
// are in the services checklist §6.3 (nginx: worker_processes … charset; php:
// memory_limit … error_reporting; mysql: max_user_connections … max_allowed_packet;
// pgsql: pg_hba_path; dovecot: config_path1..8). With two PHP versions installed the
// core prints invalid JSON, which DecodeConfigFile reports as an error.
type SysConfigFile struct {
	CONFIG map[string]string `json:"CONFIG"`
}

// DecodeConfigFile decodes a `v-list-sys-*-config json` reply.
func DecodeConfigFile(stdout []byte) (SysConfigFile, error) {
	var f SysConfigFile
	if err := json.Unmarshal(stdout, &f); err != nil {
		return SysConfigFile{}, err
	}
	if f.CONFIG == nil {
		return SysConfigFile{}, errors.New("hestia: config list without CONFIG")
	}
	return f, nil
}

// RrdChart is one chart from `v-list-sys-rrd json` (keys "1", "2", …, in file order).
// TYPE is la, mem, net, web, mail, db, ftp or ssh; RRD is the file name (eth0,
// nginx, mysql_localhost…); TIME and DATE are the time of the call.
type RrdChart struct {
	TYPE  string `json:"TYPE"`
	RRD   string `json:"RRD"`
	TITLE string `json:"TITLE"`
	TIME  string `json:"TIME"`
	DATE  string `json:"DATE"`
}

// SetName ignores the key: it is only the position.
func (c *RrdChart) SetName(string) {}

// RrdExport is `v-export-rrd <chart> <period>`: rrdtool's xport JSON. Data has one
// row per step from Meta.Start, one column per legend entry; null is a gap.
type RrdExport struct {
	Meta RrdMeta      `json:"meta"`
	Data [][]*float64 `json:"data"`
}

type RrdMeta struct {
	Start  int64    `json:"start"`
	Step   int64    `json:"step"`
	End    int64    `json:"end"`
	Legend []string `json:"legend"`
}

// DecodeRrdExport decodes a `v-export-rrd` reply.
func DecodeRrdExport(stdout []byte) (RrdExport, error) {
	var e RrdExport
	if err := json.Unmarshal(stdout, &e); err != nil {
		return RrdExport{}, err
	}
	return e, nil
}

// HestiaUpdate is one package from `v-list-sys-hestia-updates json`; the key (hestia,
// hestia-php, hestia-nginx and, when installed, hestia-web-terminal) becomes NAME.
// UPDATED is "yes" (up to date) or "no" (an update is available); DESCR is core text,
// shown through brand.Rewrite.
type HestiaUpdate struct {
	NAME    string `json:"NAME"`
	VERSION string `json:"VERSION"`
	ARCH    string `json:"ARCH"`
	UPDATED string `json:"UPDATED"`
	DESCR   string `json:"DESCR"`
	TIME    string `json:"TIME"`
	DATE    string `json:"DATE"`
}

func (u *HestiaUpdate) SetName(name string) { u.NAME = name }

// HestiaSSL is `v-list-sys-hestia-ssl json`'s one entry (key "HESTIA"): the panel's
// own certificate. KEY is the private key: never log it, never send it to the UI.
type HestiaSSL struct {
	CRT        string `json:"CRT"`
	KEY        string `json:"KEY"`
	CA         string `json:"CA"`
	SUBJECT    string `json:"SUBJECT"`
	ALIASES    string `json:"ALIASES"`
	NOT_BEFORE string `json:"NOT_BEFORE"`
	NOT_AFTER  string `json:"NOT_AFTER"`
	SIGNATURE  string `json:"SIGNATURE"`
	PUB_KEY    string `json:"PUB_KEY"`
	ISSUER     string `json:"ISSUER"`
}

// SetName ignores the key: it is always "HESTIA".
func (s *HestiaSSL) SetName(string) {}

// BackupHost is `v-list-backup-host <type> json`'s one entry (key = type). ftp and
// sftp fill HOST, USERNAME, PORT and BPATH; rclone fills HOST (a remote name) and
// BPATH; b2 fills BUCKET and B2_KEY_ID. The password and B2 key are never listed.
// The command prints nothing when the type isn't configured: use DecodeOptional.
type BackupHost struct {
	HOST      string `json:"HOST"`
	USERNAME  string `json:"USERNAME"`
	PORT      string `json:"PORT"`
	TYPE      string `json:"TYPE"`
	BPATH     string `json:"BPATH"`
	BUCKET    string `json:"BUCKET"`
	B2_KEY_ID string `json:"B2_KEY_ID"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
}

// SetName fills TYPE from the key when the entry has none.
func (b *BackupHost) SetName(typ string) {
	if b.TYPE == "" {
		b.TYPE = typ
	}
}

// ResticHost is `v-list-backup-host-restic json`'s one entry (key "restic").
type ResticHost struct {
	REPO         string `json:"REPO"`
	SNAPSHOTS    string `json:"SNAPSHOTS"`
	KEEP_DAILY   string `json:"KEEP_DAILY"`
	KEEP_WEEKLY  string `json:"KEEP_WEEKLY"`
	KEEP_MONTHLY string `json:"KEEP_MONTHLY"`
	KEEP_YEARLY  string `json:"KEEP_YEARLY"`
}

// SetName ignores the key: it is always "restic".
func (r *ResticHost) SetName(string) {}

// RemoteDNSHost is one DNS cluster host from `v-list-remote-dns-hosts json`; the key
// becomes HOST. The command prints nothing without a cluster: use DecodeOptional.
type RemoteDNSHost struct {
	HOST      string `json:"HOST"`
	PORT      string `json:"PORT"`
	TYPE      string `json:"TYPE"`
	USER      string `json:"USER"`
	DNS_USER  string `json:"DNS_USER"`
	SUSPENDED string `json:"SUSPENDED"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
}

func (h *RemoteDNSHost) SetName(host string) { h.HOST = host }

// FirewallRule is one rule from `v-list-firewall json` or `v-list-firewall-rule <id>
// json`; the key (the rule id) becomes RULE. IP is an IPv4 CIDR or "ipset:<name>";
// PORT is the core's port_ext text; COMMENT "HESTIA" and "SSH" mark the panel and
// sshd rules.
type FirewallRule struct {
	RULE      string `json:"RULE"`
	ACTION    string `json:"ACTION"`
	PROTOCOL  string `json:"PROTOCOL"`
	PORT      string `json:"PORT"`
	IP        string `json:"IP"`
	COMMENT   string `json:"COMMENT"`
	SUSPENDED string `json:"SUSPENDED"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
}

func (r *FirewallRule) SetName(id string) { r.RULE = id }

// FirewallBan is one ban from `v-list-firewall-ban json`. The core keys the object by
// IP, so an IP banned in two chains appears twice: decode with DecodeBans.
type FirewallBan struct {
	IP    string `json:"IP"`
	CHAIN string `json:"CHAIN"`
	TIME  string `json:"TIME"`
	DATE  string `json:"DATE"`
}

// SetName fills IP from the key when the entry has none.
func (b *FirewallBan) SetName(ip string) {
	if b.IP == "" {
		b.IP = ip
	}
}

// DecodeBans decodes `v-list-firewall-ban json`, keeping every (ip, chain) row even
// when the IP key repeats (decision 43, FW gotcha 21), in file order.
func DecodeBans(stdout []byte) ([]FirewallBan, error) {
	return DecodeObjects[FirewallBan](stdout)
}

// FirewallIpset is one IP list from `v-list-firewall-ipset json`; the key becomes
// LISTNAME. IP_VERSION is v4 or v6; SOURCE is a URL or "script:<path>".
type FirewallIpset struct {
	LISTNAME   string `json:"LISTNAME"`
	IP_VERSION string `json:"IP_VERSION"`
	AUTOUPDATE string `json:"AUTOUPDATE"`
	SUSPENDED  string `json:"SUSPENDED"`
	SOURCE     string `json:"SOURCE"`
	TIME       string `json:"TIME"`
	DATE       string `json:"DATE"`
}

func (s *FirewallIpset) SetName(name string) { s.LISTNAME = name }

// DecodeOptional is DecodeObjects for commands that print nothing when there is
// nothing to list (v-list-backup-host, v-list-remote-dns-hosts): blank output is an
// empty, non-nil slice.
func DecodeOptional[T any, PT interface {
	*T
	SetName(string)
}](stdout []byte) ([]T, error) {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return []T{}, nil
	}
	return DecodeObjects[T, PT](stdout)
}

// DecodeStringList decodes a flat JSON array of strings: v-get-sys-timezones,
// v-list-sys-languages, v-list-sys-interfaces (and the hook's themes, users, PHP
// versions and backend templates). Blank entries are dropped and blank output is an
// empty list; the result is never nil.
func DecodeStringList(stdout []byte) ([]string, error) {
	out := []string{}
	if len(bytes.TrimSpace(stdout)) == 0 {
		return out, nil
	}
	var raw []string
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, err
	}
	for _, s := range raw {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// ParseAutoupdate reads `v-list-sys-hestia-autoupdate plain`: "Enabled" or "Disabled".
// (The json form prints invalid JSON when disabled.)
func ParseAutoupdate(stdout []byte) (bool, error) {
	switch strings.TrimSpace(string(stdout)) {
	case "Enabled":
		return true, nil
	case "Disabled":
		return false, nil
	}
	return false, errors.New("hestia: unexpected v-list-sys-hestia-autoupdate output")
}

// ParseSSHDPorts reads `v-list-sys-sshd-port plain`: one port per line.
func ParseSSHDPorts(stdout []byte) ([]string, error) {
	out := []string{}
	for _, p := range strings.Fields(string(stdout)) {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != p {
			return nil, errors.New("hestia: unexpected v-list-sys-sshd-port output")
		}
		out = append(out, p)
	}
	return out, nil
}

// Validators. Each says whether a value may reach a v-* command as that argument; none
// accepts a leading "-", whitespace where the core forbids it, or control characters.

// ValidIPv4 accepts a dotted IPv4 address in canonical form (no CIDR, no IPv6).
func ValidIPv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4() && a.String() == s
}

// ValidCIDR4 accepts an IPv4 address with an optional /0../32 prefix (the core's
// ipv4_cidr rule, used for firewall rule and ban addresses).
func ValidCIDR4(s string) bool {
	addr, bits, found := strings.Cut(s, "/")
	if !ValidIPv4(addr) {
		return false
	}
	if !found {
		return true
	}
	n, err := strconv.Atoi(bits)
	return err == nil && n >= 0 && n <= 32 && strconv.Itoa(n) == bits
}

// ValidRuleID accepts a firewall rule id: 1 to 9 digits, no leading zero.
func ValidRuleID(s string) bool {
	if s == "" || len(s) > 9 || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ValidIpsetName accepts an IP set name: the core's domain (ip_name) rule, ASCII, at
// most 27 characters (the kernel's 31 less the "-tmp" suffix the core adds).
func ValidIpsetName(s string) bool {
	if s == "" || len(s) > 27 || s == "www" || strings.Trim(s, "0123456789") == "" {
		return false
	}
	if strings.ContainsAny(s, "][!@#$^&*()+={},<>?_/\\\"|'`;%~:") || strings.Contains(s, "..") ||
		strings.Contains(s, ".-") || strings.Contains(s, "-.") {
		return false
	}
	if s[0] == '-' || s[0] == '.' || s[len(s)-1] == '-' || s[len(s)-1] == '.' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= ' ' || c >= 0x7f {
			return false
		}
	}
	return true
}

// ValidPortExt accepts a firewall port (decision 42): "0" (all ports) alone, or a
// comma list of ports 1–65535 and ranges a-b (a < b), at most 78 characters. Unlike
// the core it refuses "|", ":" and out-of-range numbers, which iptables would reject
// after the core had already saved the rule.
func ValidPortExt(s string) bool {
	if s == "0" {
		return true
	}
	if s == "" || len(s) > 78 {
		return false
	}
	port := func(p string) (int, bool) {
		n, err := strconv.Atoi(p)
		return n, err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == p
	}
	for _, item := range strings.Split(s, ",") {
		lo, hi, isRange := strings.Cut(item, "-")
		a, ok := port(lo)
		if !ok {
			return false
		}
		if isRange {
			b, ok := port(hi)
			if !ok || b <= a {
				return false
			}
		}
	}
	return true
}

var firewallComment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,64}[A-Za-z0-9]$`)

// ValidFirewallComment accepts a rule comment as the core does (2–66 characters,
// starting and ending with a letter or digit, inner spaces, dots, underscores and
// hyphens), or "" for no comment.
func ValidFirewallComment(s string) bool {
	return s == "" || firewallComment.MatchString(s)
}

// banChains are the chains a ban may name (decision 43).
var banChains = []string{"SSH", "WEB", "FTP", "DNS", "MAIL", "DB", "HESTIA"}

// ValidChain accepts one of the ban chains SSH WEB FTP DNS MAIL DB HESTIA.
func ValidChain(s string) bool {
	for _, c := range banChains {
		if s == c {
			return true
		}
	}
	return false
}

// ValidConfigValue accepts a free-text hestia.conf value (decision 34): at most 2048
// bytes, none of ' " | & \ and no control characters (newlines included), because
// v-change-sys-config-value writes it with sed. "" is accepted (clearing a value).
func ValidConfigValue(s string) bool {
	if len(s) > 2048 || strings.ContainsAny(s, "'\"|&\\") {
		return false
	}
	for _, r := range s {
		if r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

var chartID = regexp.MustCompile(`^(la|mem|mail|ftp|ssh|nginx|apache2|httpd|net_[A-Za-z0-9.-]{1,15}|(mysql|pgsql)_[A-Za-z0-9.-]{1,64})$`)

// ValidChartID accepts a graph id (decision 22): la, mem, mail, ftp, ssh, nginx,
// apache2, httpd, net_<interface> or mysql_/pgsql_<host>. Handlers also require the
// id to be in the current catalogue.
func ValidChartID(s string) bool {
	return chartID.MatchString(s)
}

package hestia

import (
	"slices"
	"strings"
	"testing"
)

// The JSON below is each checklist's example (sysinfo, services, settings and
// firewall parity checklists, section 6), verbatim or abridged.

func TestDecodeSysInfo(t *testing.T) {
	in := `{"sysinfo": {"HOSTNAME": "srv1.example.com","OS": "Debian","VERSION": "12.8","ARCH": "x86_64","HESTIA": "1.10.5","RELEASE": "release","UPTIME": "5421","LOADAVERAGE": "0.10 / 0.08 / 0.03"}}`
	got, err := DecodeSysInfo([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := SysInfo{HOSTNAME: "srv1.example.com", OS: "Debian", VERSION: "12.8", ARCH: "x86_64", HESTIA: "1.10.5",
		RELEASE: "release", UPTIME: "5421", LOADAVERAGE: "0.10 / 0.08 / 0.03"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if _, err := DecodeSysInfo([]byte(`{}`)); err == nil {
		t.Error("an empty object decoded")
	}
}

func TestDecodeSysServices(t *testing.T) {
	in := `{
    "nginx": {"SYSTEM": "reverse proxy", "STATE": "running", "CPU": "0.1", "MEM": "57", "RTIME": "5421"},
    "php8.3-fpm": {"SYSTEM": "php interpreter", "STATE": "running", "CPU": "0", "MEM": "112", "RTIME": "5420"},
    "exim4": {"SYSTEM": "mail server", "STATE": "stopped", "CPU": "0", "MEM": "0", "RTIME": "0"},
    "iptables": {"SYSTEM": "firewall", "STATE": "running", "CPU": "0", "MEM": "0", "RTIME": "0"}
}`
	got, err := DecodeObjects[SysService]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.NAME)
	}
	if !slices.Equal(names, []string{"nginx", "php8.3-fpm", "exim4", "iptables"}) {
		t.Fatalf("order = %v", names)
	}
	want := SysService{NAME: "nginx", SYSTEM: "reverse proxy", STATE: "running", CPU: "0.1", MEM: "57", RTIME: "5421"}
	if got[0] != want {
		t.Errorf("got %+v", got[0])
	}
	if got[2].STATE != "stopped" {
		t.Errorf("exim4 = %+v", got[2])
	}
}

func TestDecodeConfigFile(t *testing.T) {
	in := `{"CONFIG": {"worker_processes": "auto", "worker_connections": "1024", "client_max_body_size": "256m", "gzip": "on", "charset": "utf-8", "config_path": "/etc/nginx/nginx.conf"}}`
	got, err := DecodeConfigFile([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got.CONFIG["worker_connections"] != "1024" || got.CONFIG["config_path"] != "/etc/nginx/nginx.conf" {
		t.Errorf("got %+v", got)
	}
	dovecot := `{"CONFIG": {"config_path": "/etc/dovecot/dovecot.conf", "config_path1": "/etc/dovecot/conf.d/10-auth.conf", "config_path8": "/etc/dovecot/conf.d/auth-passwdfile.conf.ext"}}`
	if got, err := DecodeConfigFile([]byte(dovecot)); err != nil || got.CONFIG["config_path8"] != "/etc/dovecot/conf.d/auth-passwdfile.conf.ext" {
		t.Errorf("dovecot = %+v, %v", got, err)
	}
	// Two PHP versions put a raw newline into config_path: invalid JSON (SVC gotcha 5).
	if _, err := DecodeConfigFile([]byte("{\"CONFIG\": {\"config_path\": \"/etc/php/8.2/fpm/php.ini\n/etc/php/8.3/fpm/php.ini\"}}")); err == nil {
		t.Error("invalid JSON decoded")
	}
}

func TestDecodeRrdCharts(t *testing.T) {
	in := `{
	"1": {"TYPE": "la", "RRD": "la", "TITLE": "Load Average", "TIME": "10:05:01", "DATE": "2026-10-21"},
	"3": {"TYPE": "net", "RRD": "eth0", "TITLE": "Bandwidth Usage eth0", "TIME": "10:05:01", "DATE": "2026-10-21"},
	"6": {"TYPE": "db", "RRD": "mysql_localhost", "TITLE": "MySQL Usage on localhost", "TIME": "10:05:01", "DATE": "2026-10-21"}
}`
	got, err := DecodeObjects[RrdChart]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := RrdChart{TYPE: "net", RRD: "eth0", TITLE: "Bandwidth Usage eth0", TIME: "10:05:01", DATE: "2026-10-21"}
	if len(got) != 3 || got[1] != want || got[2].RRD != "mysql_localhost" {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeRrdExport(t *testing.T) {
	in := `{ "about": "RRDtool graph JSON output",
  "meta": {
    "start": 1760954100, "step": 300, "end": 1761040500,
    "legend": ["Used","Swap","Free"]
  },
  "data": [
    [1204.0, 0.0, 2490.0],
    [1209.5, 0.0, 2484.2],
    [null, null, null]
  ]
}`
	got, err := DecodeRrdExport([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Start != 1760954100 || got.Meta.Step != 300 || got.Meta.End != 1761040500 ||
		!slices.Equal(got.Meta.Legend, []string{"Used", "Swap", "Free"}) {
		t.Errorf("meta = %+v", got.Meta)
	}
	if len(got.Data) != 3 || *got.Data[1][0] != 1209.5 || got.Data[2][0] != nil || got.Data[2][2] != nil {
		t.Errorf("data = %v", got.Data)
	}
	if _, err := DecodeRrdExport([]byte("Does not exist\n")); err == nil {
		t.Error("non-JSON decoded")
	}
}

func TestDecodeHestiaUpdates(t *testing.T) {
	in := `{
    "hestia": {"VERSION": "1.10.5", "ARCH": "amd64", "UPDATED": "yes", "DESCR": "Hestia core package", "TIME": "06:12:01", "DATE": "2026-10-01"},
    "hestia-php": {"VERSION": "1.10.5", "ARCH": "amd64", "UPDATED": "no", "DESCR": "Hestia internal php interpreter", "TIME": "06:12:01", "DATE": "2026-10-01"},
    "hestia-nginx": {"VERSION": "1.10.5", "ARCH": "amd64", "UPDATED": "yes", "DESCR": "Hestia internal web server", "TIME": "06:12:01", "DATE": "2026-10-01"}
}`
	got, err := DecodeObjects[HestiaUpdate]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := HestiaUpdate{NAME: "hestia-php", VERSION: "1.10.5", ARCH: "amd64", UPDATED: "no",
		DESCR: "Hestia internal php interpreter", TIME: "06:12:01", DATE: "2026-10-01"}
	if len(got) != 3 || got[1] != want || got[2].NAME != "hestia-nginx" {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeHestiaSSL(t *testing.T) {
	in := `{
	"HESTIA": {
        "CRT": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
        "KEY": "-----BEGIN PRIVATE KEY-----\nMIGH...\n-----END PRIVATE KEY-----",
        "CA": "",
        "SUBJECT": "host.example.com",
        "ALIASES": "host.example.com,www.host.example.com",
        "NOT_BEFORE": "Oct 17 09:41:12 2026 GMT",
        "NOT_AFTER": "Jan 15 09:41:11 2027 GMT",
        "SIGNATURE": "ecdsa-with-SHA256",
        "PUB_KEY": "256 bit",
        "ISSUER": "C = US, O = Let's Encrypt, CN = E5"
	}
}`
	got, err := DecodeObjects[HestiaSSL]([]byte(in))
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
	s := got[0]
	if !strings.HasPrefix(s.CRT, "-----BEGIN CERTIFICATE-----\n") || !strings.Contains(s.KEY, "PRIVATE KEY") || s.CA != "" ||
		s.SUBJECT != "host.example.com" || s.ALIASES != "host.example.com,www.host.example.com" ||
		s.NOT_BEFORE != "Oct 17 09:41:12 2026 GMT" || s.NOT_AFTER != "Jan 15 09:41:11 2027 GMT" ||
		s.SIGNATURE != "ecdsa-with-SHA256" || s.PUB_KEY != "256 bit" || s.ISSUER != "C = US, O = Let's Encrypt, CN = E5" {
		t.Errorf("got %+v", s)
	}
}

func TestDecodeBackupHosts(t *testing.T) {
	sftp := `{
    "sftp": {"HOST": "backup.example.net", "USERNAME": "hestia", "PORT": "22", "TYPE": "sftp", "BPATH": "/backup", "TIME": "03:12:44", "DATE": "2026-10-01"}
}`
	got, err := DecodeOptional[BackupHost]([]byte(sftp))
	if err != nil {
		t.Fatal(err)
	}
	want := BackupHost{HOST: "backup.example.net", USERNAME: "hestia", PORT: "22", TYPE: "sftp", BPATH: "/backup", TIME: "03:12:44", DATE: "2026-10-01"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("sftp = %+v", got)
	}
	b2 := `{
    "b2": {"BUCKET": "my-hestia-backups", "TYPE": "b2", "B2_KEY_ID": "0012345abcdef0000000001", "TIME": "03:12:44", "DATE": "2026-10-01"}
}`
	got, err = DecodeOptional[BackupHost]([]byte(b2))
	if err != nil || len(got) != 1 || got[0].BUCKET != "my-hestia-backups" || got[0].B2_KEY_ID != "0012345abcdef0000000001" || got[0].TYPE != "b2" {
		t.Errorf("b2 = %+v, %v", got, err)
	}
	// No config file for the type: the command prints nothing.
	got, err = DecodeOptional[BackupHost]([]byte("\n"))
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("empty = %#v, %v", got, err)
	}
}

func TestDecodeResticHost(t *testing.T) {
	in := `{
    "restic": {
	"REPO": "/mnt/restic",
	"SNAPSHOTS": "30",
	"KEEP_DAILY": "7",
	"KEEP_WEEKLY": "4",
	"KEEP_MONTHLY": "6",
	"KEEP_YEARLY": "-1"
	}
}`
	got, err := DecodeOptional[ResticHost]([]byte(in))
	want := ResticHost{REPO: "/mnt/restic", SNAPSHOTS: "30", KEEP_DAILY: "7", KEEP_WEEKLY: "4", KEEP_MONTHLY: "6", KEEP_YEARLY: "-1"}
	if err != nil || len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestDecodeRemoteDNSHosts(t *testing.T) {
	in := `{
    "ns2.example.com": {"PORT": "8083", "TYPE": "api", "USER": "dnsuser", "DNS_USER": "dns-cluster", "SUSPENDED": "no", "TIME": "09:41:12", "DATE": "2026-10-17"}
}`
	got, err := DecodeOptional[RemoteDNSHost]([]byte(in))
	want := RemoteDNSHost{HOST: "ns2.example.com", PORT: "8083", TYPE: "api", USER: "dnsuser", DNS_USER: "dns-cluster",
		SUSPENDED: "no", TIME: "09:41:12", DATE: "2026-10-17"}
	if err != nil || len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, %v", got, err)
	}
	if got, err := DecodeOptional[RemoteDNSHost](nil); err != nil || len(got) != 0 {
		t.Errorf("no cluster = %+v, %v", got, err)
	}
}

func TestDecodeFirewallRules(t *testing.T) {
	in := `{
    "1": {"ACTION": "ACCEPT", "PROTOCOL": "ICMP", "PORT": "0", "IP": "0.0.0.0/0", "COMMENT": "PING", "SUSPENDED": "no", "TIME": "17:13:48", "DATE": "2014-09-16"},
    "2": {"ACTION": "ACCEPT", "PROTOCOL": "TCP", "PORT": "8083", "IP": "0.0.0.0/0", "COMMENT": "HESTIA", "SUSPENDED": "no", "TIME": "07:40:16", "DATE": "2014-05-25"},
    "11": {"ACTION": "DROP", "PROTOCOL": "TCP", "PORT": "25", "IP": "ipset:country-cn", "COMMENT": "Block CN SMTP", "SUSPENDED": "yes", "TIME": "08:12:03", "DATE": "2026-10-02"}
}`
	got, err := DecodeObjects[FirewallRule]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := FirewallRule{RULE: "2", ACTION: "ACCEPT", PROTOCOL: "TCP", PORT: "8083", IP: "0.0.0.0/0", COMMENT: "HESTIA",
		SUSPENDED: "no", TIME: "07:40:16", DATE: "2014-05-25"}
	if len(got) != 3 || got[1] != want || got[2].RULE != "11" || got[2].IP != "ipset:country-cn" {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeBansKeepsDuplicates(t *testing.T) {
	in := `{
    "37.120.129.20": {"IP": "37.120.129.20", "CHAIN": "MAIL", "TIME": "10:22:41", "DATE": "2026-10-19"},
    "203.0.113.9": {"IP": "203.0.113.9", "CHAIN": "SSH", "TIME": "03:15:09", "DATE": "2026-10-20"},
    "203.0.113.9": {"IP": "203.0.113.9", "CHAIN": "MAIL", "TIME": "04:15:09", "DATE": "2026-10-20"}
}`
	got, err := DecodeBans([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d: %+v", len(got), got)
	}
	want := FirewallBan{IP: "203.0.113.9", CHAIN: "MAIL", TIME: "04:15:09", DATE: "2026-10-20"}
	if got[1].CHAIN != "SSH" || got[2] != want {
		t.Errorf("got %+v", got)
	}
	if got, err := DecodeBans([]byte("{\n}\n")); err != nil || len(got) != 0 {
		t.Errorf("empty = %+v, %v", got, err)
	}
}

func TestDecodeFirewallIpsets(t *testing.T) {
	in := `{
    "country-nl": {"IP_VERSION": "v4", "AUTOUPDATE": "yes", "SUSPENDED": "no", "SOURCE": "https://raw.githubusercontent.com/ipverse/rir-ip/master/country/nl/ipv4-aggregated.txt", "TIME": "04:00:11", "DATE": "2026-10-21"},
    "blacklist": {"IP_VERSION": "v4", "AUTOUPDATE": "yes", "SUSPENDED": "no", "SOURCE": "script:/usr/local/hestia/install/common/firewall/ipset/blacklist.sh", "TIME": "04:00:30", "DATE": "2026-10-21"}
}`
	got, err := DecodeObjects[FirewallIpset]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := FirewallIpset{LISTNAME: "blacklist", IP_VERSION: "v4", AUTOUPDATE: "yes", SUSPENDED: "no",
		SOURCE: "script:/usr/local/hestia/install/common/firewall/ipset/blacklist.sh", TIME: "04:00:30", DATE: "2026-10-21"}
	if len(got) != 2 || got[0].LISTNAME != "country-nl" || got[1] != want {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeStringList(t *testing.T) {
	for in, want := range map[string][]string{
		`["Africa/Abidjan","Europe/Berlin","UTC"]`: {"Africa/Abidjan", "Europe/Berlin", "UTC"},
		"[\n    \"eth0\",\n    \"eth1\"\n]":        {"eth0", "eth1"},
		"[\n\t\"en\",\n\t\"\",\n\t\"de\"\n]":       {"en", "de"},
		"":                                         {},
		"\n":                                       {},
	} {
		got, err := DecodeStringList([]byte(in))
		if err != nil || got == nil || !slices.Equal(got, want) {
			t.Errorf("%q: %#v, %v", in, got, err)
		}
	}
	if _, err := DecodeStringList([]byte(`{"a":1}`)); err == nil {
		t.Error("an object decoded")
	}
}

func TestParseAutoupdate(t *testing.T) {
	for in, want := range map[string]bool{"Enabled\n": true, "Disabled\n": false, "Enabled": true} {
		if got, err := ParseAutoupdate([]byte(in)); err != nil || got != want {
			t.Errorf("%q: %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "[\n\t\"Disabled\",\n]", "yes"} {
		if _, err := ParseAutoupdate([]byte(in)); err == nil {
			t.Errorf("%q parsed", in)
		}
	}
}

func TestParseSSHDPorts(t *testing.T) {
	for in, want := range map[string][]string{"22\n": {"22"}, "22\n2222\n": {"22", "2222"}, "": {}} {
		if got, err := ParseSSHDPorts([]byte(in)); err != nil || !slices.Equal(got, want) {
			t.Errorf("%q: %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"ssh\n", "0\n", "70000\n"} {
		if _, err := ParseSSHDPorts([]byte(in)); err == nil {
			t.Errorf("%q parsed", in)
		}
	}
}

// hostile is the shared list of values no validator here may accept.
var hostile = []string{"", "-h", "a b", "x;y", strings.Repeat("a", 65), "a\nb", "a\x00b"}

func checkValidator(t *testing.T, name string, f func(string) bool, good, bad []string) {
	t.Helper()
	for _, s := range good {
		if !f(s) {
			t.Errorf("%s(%q) = false, want true", name, s)
		}
	}
	for _, s := range bad {
		if f(s) {
			t.Errorf("%s(%q) = true, want false", name, s)
		}
	}
}

func TestValidIPv4(t *testing.T) {
	checkValidator(t, "ValidIPv4", ValidIPv4,
		[]string{"203.0.113.10", "10.0.0.5", "0.0.0.0", "255.255.255.255"},
		append([]string{"1.2.3.4/33", "1.2.3.4/24", "256.1.1.1", "01.2.3.4", "::1", "2001:db8::1", "1.2.3", "1.2.3.4 "}, hostile...))
}

func TestValidCIDR4(t *testing.T) {
	checkValidator(t, "ValidCIDR4", ValidCIDR4,
		[]string{"0.0.0.0/0", "203.0.113.9", "198.51.100.0/24", "1.2.3.4/32"},
		append([]string{"1.2.3.4/33", "1.2.3.4/", "1.2.3.4/-1", "1.2.3.4/x", "::/0", "ipset:blacklist"}, hostile...))
}

func TestValidRuleID(t *testing.T) {
	checkValidator(t, "ValidRuleID", ValidRuleID,
		[]string{"1", "2", "10", "999999999"},
		append([]string{"0", "01", "1234567890", "1a", "+1", "99999"[:0]}, hostile...))
}

func TestValidIpsetName(t *testing.T) {
	checkValidator(t, "ValidIpsetName", ValidIpsetName,
		[]string{"blacklist", "country-nl", "my.list", strings.Repeat("a", 27)},
		append([]string{strings.Repeat("a", 28), "www", "123", "a..b", "-a", "a-", ".a", "a.", "a.-b", "a_b", "a/b", "a|b", "a$b"}, hostile...))
}

func TestValidPortExt(t *testing.T) {
	checkValidator(t, "ValidPortExt", ValidPortExt,
		[]string{"0", "22", "80,443", "1-65535", "8000-8100,22", "65535"},
		append([]string{"80|443", "99999", "65536", "0-0", "0,80", "80,", ",80", "100-90", "80-", "1:2", "80, 443", "a", strings.Repeat("1,", 40) + "1"}, hostile...))
}

func TestValidFirewallComment(t *testing.T) {
	checkValidator(t, "ValidFirewallComment", ValidFirewallComment,
		[]string{"", "PING", "HESTIA", "Block CN SMTP", "web-1", "a.b_c", "ab", strings.Repeat("a", 66)},
		[]string{"x", "-h", "x;y", "a\nb", " ab", "ab ", "a\tb", "a'b", strings.Repeat("a", 67), "é1"})
}

func TestValidChain(t *testing.T) {
	checkValidator(t, "ValidChain", ValidChain,
		[]string{"SSH", "WEB", "FTP", "DNS", "MAIL", "DB", "HESTIA"},
		append([]string{"ssh", "RECIDIVE", "INPUT", "fail2ban-SSH"}, hostile...))
}

func TestValidConfigValue(t *testing.T) {
	checkValidator(t, "ValidConfigValue", ValidConfigValue,
		[]string{"", "yes", "a b", "x;y", "{{page}} - {{hostname}} - {{appname}}", "noreply@example.com", "-h", "Europe/Berlin"},
		[]string{"it's", `a"b`, "a|b", "a&b", `a\b`, "a\nb", "a\rb", "a\x00b", strings.Repeat("a", 2049)})
}

func TestValidChartID(t *testing.T) {
	checkValidator(t, "ValidChartID", ValidChartID,
		[]string{"la", "mem", "mail", "ftp", "ssh", "nginx", "apache2", "httpd", "net_eth0", "net_ens3.100", "mysql_localhost", "pgsql_db.example.com"},
		append([]string{"net_", "net_" + strings.Repeat("a", 16), "mysql_", "mysql_" + strings.Repeat("a", 65), "LA", "web", "net_a/b", "la "}, hostile...))
}

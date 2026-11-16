package runner

import (
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// recordedDir holds fixtures recorded on a real Hestia box. They must pass through
// tools/scrub-fixtures.sh before they are committed.
const recordedDir = "../../testdata/fixtures/recorded"

// secretKeys are the Hestia fields whose values are secrets in any command's output.
const secretKeys = `PHPMYADMIN_KEY|SERVER_SMTP_PASSWD|RKEY|TWOFA|QRCODE|SECRET_ACCESS_KEY|MD5|PASSWORD`

var (
	// A private key block with a body (BEGIN line then base64). Inside JSON stdout the newline is the two
	// characters `\n`, so both forms count.
	privateKeyRe = regexp.MustCompile(`BEGIN [A-Z ]*PRIVATE KEY-----(?:\s|\\[nr])*[A-Za-z0-9+/]`)
	// "RKEY": "value" inside JSON stdout.
	jsonSecretRe = regexp.MustCompile(`"(` + secretKeys + `)"\s*:\s*"([^"]*)"`)
	// RKEY='value' or RKEY="value" at the start of a line (shell/conf format).
	shellSecretRe = regexp.MustCompile(`(?m)^(` + secretKeys + `)=(?:'([^']*)'|"([^"]*)")`)
	// A crypt(3) password hash ($1$, $2y$, $5$, $6$, $y$) or argon2.
	hashRe = regexp.MustCompile(`\$(?:1|2[abxy]|5|6|y|argon2id?)\$[^\s"'\\]{8,}`)
)

// safePrefixes may appear in recorded fixtures: RFC 5737 documentation, RFC 1918
// private, loopback and the unspecified address.
var safePrefixes = []netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("0.0.0.0/32"),
}

var dottedRunRe = regexp.MustCompile(`[0-9.]+`)

// publicIPs returns every IPv4 address in s outside safePrefixes. Netmasks
// (255.255.255.0 and the like) are not addresses and pass. A run of digits and dots
// counts only when it is exactly four parts, so versions (1.10.5) and 1.2.3.4.5 pass.
func publicIPs(s string) []string {
	var found []string
	for _, run := range dottedRunRe.FindAllString(s, -1) {
		run = strings.Trim(run, ".")
		if strings.Count(run, ".") != 3 {
			continue
		}
		addr, err := netip.ParseAddr(run)
		if err != nil || isNetmask(addr) {
			continue
		}
		safe := false
		for _, p := range safePrefixes {
			safe = safe || p.Contains(addr)
		}
		if !safe {
			found = append(found, run)
		}
	}
	return found
}

// isNetmask reports whether addr is a contiguous run of one bits from the top.
func isNetmask(addr netip.Addr) bool {
	b := addr.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return v != 0 && ^v&(^v+1) == 0
}

// findSecrets returns a short description of each secret-shaped value in s. Empty
// values and the scrubber's REDACTED placeholder are not secrets.
func findSecrets(s string) []string {
	var found []string
	if privateKeyRe.MatchString(s) {
		found = append(found, "private key block")
	}
	for _, m := range jsonSecretRe.FindAllStringSubmatch(s, -1) {
		if m[2] != "" && m[2] != "REDACTED" {
			found = append(found, m[1]+" value")
		}
	}
	for _, m := range shellSecretRe.FindAllStringSubmatch(s, -1) {
		if v := m[2] + m[3]; v != "" && v != "REDACTED" {
			found = append(found, m[1]+" value")
		}
	}
	if hashRe.MatchString(s) {
		found = append(found, "password hash")
	}
	for _, ip := range publicIPs(s) {
		found = append(found, "public IP "+ip)
	}
	return found
}

// fixtureSecrets parses one fixture file and checks every entry's args, stdout and
// stderr. A file that isn't a fixture list is reported too: unparsed text is unchecked.
func fixtureSecrets(data []byte) []string {
	var list []fixtureEntry
	if err := json.Unmarshal(data, &list); err != nil {
		return []string{"not a fixture list: " + err.Error()}
	}
	var found []string
	for i, e := range list {
		text := strings.Join(e.Args, "\n") + "\n" + e.Stdout + "\n" + e.Stderr
		for _, f := range findSecrets(text) {
			found = append(found, "entry "+strconv.Itoa(i)+": "+f)
		}
	}
	return found
}

func TestRecordedFixturesHoldNoSecrets(t *testing.T) {
	if _, err := os.Stat(recordedDir); err != nil {
		t.Fatalf("recorded fixtures dir missing: %v", err)
	}
	err := filepath.WalkDir(recordedDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, f := range fixtureSecrets(data) {
			t.Errorf("%s: %s (run tools/scrub-fixtures.sh)", path, f)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// plantedFixture holds one real-looking secret of each kind the guard knows.
const plantedFixture = `[
 {"args":["json"],"stdout":"{\n    \"PHPMYADMIN_KEY\": \"a1b2c3d4e5\",\n    \"SERVER_SMTP_PASSWD\": \"hunter2\",\n    \"THEME\": \"dark\"\n}\n","exit":0},
 {"args":["admin","json"],"stdout":"{\n  \"admin\": {\n    \"RKEY\": \"Zq8xLm2P\",\n    \"TWOFA\": \"JBSWY3DPEHPK3PXP\",\n    \"QRCODE\": \"otpauth://totp/x?secret=JBSWY3DPEHPK3PXP\",\n    \"MD5\": \"$6$saltsalt$abcdefghijklmnopqrstuv\",\n    \"PASSWORD\": \"s3cret\",\n    \"CONTACT\": \"a@b.test\"\n  }\n}\n","exit":0},
 {"args":["key1","json"],"stdout":"{\"key1\": {\"SECRET_ACCESS_KEY\": \"ZmFrZXNlY3JldA\", \"USER\": \"admin\"}}\n","exit":0},
 {"args":["shell"],"stdout":"RKEY='Zq8xLm2P'\nPASSWORD=\"pw\"\nTHEME='dark'\n","exit":0}
]`

const plantedSSL = `[
 {"args":["json"],"stdout":"{\n\t\"HESTIA\": {\n        \"CRT\": \"-----BEGIN CERTIFICATE-----\\nMIIBcert\\n-----END CERTIFICATE-----\",\n        \"KEY\": \"-----BEGIN PRIVATE KEY-----\\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASC\\n-----END PRIVATE KEY-----\",\n        \"SUBJECT\": \"CN=rec.hostisan.test\"\n\t}\n}\n","exit":0},
 {"args":["plain"],"stdout":"-----BEGIN CERTIFICATE-----\nMIIBcert\n-----END CERTIFICATE-----\n\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA\n-----END RSA PRIVATE KEY-----\nCN=rec.hostisan.test\n","exit":0}
]`

func TestRecordedFixturesGuardCatchesPlantedSecrets(t *testing.T) {
	got := strings.Join(fixtureSecrets([]byte(plantedFixture)), "; ")
	for _, want := range []string{
		"entry 0: PHPMYADMIN_KEY value", "entry 0: SERVER_SMTP_PASSWD value",
		"entry 1: RKEY value", "entry 1: TWOFA value", "entry 1: QRCODE value",
		"entry 1: MD5 value", "entry 1: PASSWORD value", "entry 1: password hash",
		"entry 2: SECRET_ACCESS_KEY value",
		"entry 3: RKEY value", "entry 3: PASSWORD value",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("guard missed %q; found: %s", want, got)
		}
	}
	ssl := fixtureSecrets([]byte(plantedSSL))
	if len(ssl) != 2 || ssl[0] != "entry 0: private key block" || ssl[1] != "entry 1: private key block" {
		t.Errorf("ssl findings = %q", ssl)
	}
	if got := fixtureSecrets([]byte(`{"not":"a list"}`)); len(got) != 1 {
		t.Errorf("non-list file findings = %q", got)
	}
}

func TestRecordedFixturesGuardAllowsCleanValues(t *testing.T) {
	clean := `[{"args":["admin","json"],"stdout":"{\"admin\": {\"RKEY\": \"REDACTED\", \"TWOFA\": \"\", ` +
		`\"MD5\": \"REDACTED\", \"KEY\": \"REDACTED-KEY\", \"PUB_KEY\": \"rsaEncryption 2048 bit\", ` +
		`\"PASSWORD_RESET\": \"yes\", \"POLICY_SYSTEM_PASSWORD_RESET\": \"no\", \"PRICE\": \"$5\"}}\n` +
		`RKEY=''\nTWOFA='REDACTED'\n","exit":0}]`
	if got := fixtureSecrets([]byte(clean)); len(got) != 0 {
		t.Errorf("clean fixture flagged: %q", got)
	}
}

// TestRecordedFixturesScrubberClearsPlanted runs tools/scrub-fixtures.sh on planted
// secrets and checks the guard then passes and non-secret data survives.
func TestRecordedFixturesScrubberClearsPlanted(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	dir := t.TempDir()
	files := map[string]string{
		"v-list-sys-config.json":     plantedFixture,
		"v-list-sys-hestia-ssl.json": plantedSSL,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("bash", "../../../tools/scrub-fixtures.sh", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("scrub-fixtures.sh: %v\n%s", err, out)
	}
	for name := range files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if found := fixtureSecrets(data); len(found) != 0 {
			t.Errorf("%s after scrub: %q", name, found)
		}
		var list []fixtureEntry
		if err := json.Unmarshal(data, &list); err != nil {
			t.Fatalf("%s after scrub: %v", name, err)
		}
		all := ""
		for _, e := range list {
			all += e.Stdout
		}
		for _, keep := range map[string][]string{
			"v-list-sys-config.json":     {`"THEME":"dark"`, `"CONTACT":"a@b.test"`, `"RKEY":"REDACTED"`, "THEME='dark'"},
			"v-list-sys-hestia-ssl.json": {`"KEY":"REDACTED"`, "MIIBcert", "REDACTED-KEY\nCN=rec.hostisan.test"},
		}[name] {
			if !strings.Contains(all, keep) {
				t.Errorf("%s after scrub lacks %q:\n%s", name, keep, all)
			}
		}
	}
}

func TestRecordedFixturesGuardCatchesPublicIPs(t *testing.T) {
	text := "IP=185.161.209.107\nns: 8.8.8.8, 1.1.1.1.\n\"NAT\": \"100.64.0.1\"\n172.32.0.1\n"
	got := strings.Join(publicIPs(text), " ")
	if got != "185.161.209.107 8.8.8.8 1.1.1.1 100.64.0.1 172.32.0.1" {
		t.Errorf("publicIPs = %q", got)
	}
	f := `[{"args":["185.161.209.107"],"stdout":"","exit":0}]`
	if got := fixtureSecrets([]byte(f)); len(got) != 1 || got[0] != "entry 0: public IP 185.161.209.107" {
		t.Errorf("IP in args findings = %q", got)
	}
}

func TestRecordedFixturesGuardAllowsSafeIPs(t *testing.T) {
	text := "203.0.113.10 198.51.100.7 192.0.2.1 10.0.0.5 172.16.3.4 172.31.255.1 " +
		"192.168.1.1 127.0.0.53 0.0.0.0 255.255.255.0 255.255.240.0 255.255.255.255 " +
		"PHP-8.2 1.10.5 999.1.1.1 1.2.3.4.5"
	if got := publicIPs(text); len(got) != 0 {
		t.Errorf("safe text flagged: %q", got)
	}
}

// TestRecordedFixturesScrubberRewritesHostAndIP checks --ip and --host rewrite args,
// stdout and stderr, and leave look-alike neighbours alone.
func TestRecordedFixturesScrubberRewritesHostAndIP(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	dir := t.TempDir()
	in := `[{"args":["admin","185.161.209.107"],` +
		`"stdout":"{\"185.161.209.107\": {\"HOST\": \"vps1.real.example\", \"ALIAS\": \"mail.vps1.real.example\", ` +
		`\"OTHER\": \"185.161.209.1070 xvps1.real.example\"}}\n",` +
		`"stderr":"connect to vps1.real.example (185.161.209.107) failed\n","exit":1}]`
	path := filepath.Join(dir, "v-list-sys-ips.json")
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "../../../tools/scrub-fixtures.sh",
		"--ip", "185.161.209.107=203.0.113.10", "--host", "vps1.real.example=rec.hostisan.test", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scrub-fixtures.sh: %v\n%s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var list []fixtureEntry
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	e := list[0]
	if strings.Join(e.Args, " ") != "admin 203.0.113.10" {
		t.Errorf("args = %q", e.Args)
	}
	for _, want := range []string{`"203.0.113.10": {`, `"HOST": "rec.hostisan.test"`,
		`"ALIAS": "mail.rec.hostisan.test"`, `"185.161.209.1070 xvps1.real.example"`} {
		if !strings.Contains(e.Stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, e.Stdout)
		}
	}
	if e.Stderr != "connect to rec.hostisan.test (203.0.113.10) failed\n" {
		t.Errorf("stderr = %q", e.Stderr)
	}
	for _, bad := range [][]string{{"--ip", "nope=203.0.113.1"}, {"--host", "a b=x"}, {"--ip", "1.2.3.4"}} {
		args := append([]string{"../../../tools/scrub-fixtures.sh"}, bad...)
		if out, err := exec.Command("bash", append(args, dir)...).CombinedOutput(); err == nil {
			t.Errorf("%v accepted:\n%s", bad, out)
		}
	}
}

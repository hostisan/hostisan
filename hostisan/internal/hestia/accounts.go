package hestia

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
)

// Account types (Plan 3) follow the key order of Hestia's own JSON (users §6, account
// §6, cron §6, backup §6.3). Every value is a string as Hestia prints it, except where
// a field says otherwise. Keys a type doesn't list are skipped on purpose: none of
// these types has a field for RKEY, TWOFA, QRCODE, the auth log's SESSION or
// SECRET_ACCESS_KEY, so a reply built from them can never carry a secret.
//
// Types with SetName decode with DecodeObjects; SetName stores the JSON object key.

// UserEntry is one user from `v-list-users`. USER is the object key (the username);
// NAME is the contact name, as Hestia names it.
type UserEntry struct {
	USER           string `json:"USER"`
	NAME           string `json:"NAME"`
	PACKAGE        string `json:"PACKAGE"`
	DISK_QUOTA     string `json:"DISK_QUOTA"`
	BANDWIDTH      string `json:"BANDWIDTH"`
	CONTACT        string `json:"CONTACT"`
	ROLE           string `json:"ROLE"`
	SUSPENDED      string `json:"SUSPENDED"`
	IP_OWNED       string `json:"IP_OWNED"`
	U_DISK         string `json:"U_DISK"`
	U_BANDWIDTH    string `json:"U_BANDWIDTH"`
	U_WEB_DOMAINS  string `json:"U_WEB_DOMAINS"`
	U_DNS_DOMAINS  string `json:"U_DNS_DOMAINS"`
	U_MAIL_DOMAINS string `json:"U_MAIL_DOMAINS"`
	U_DATABASES    string `json:"U_DATABASES"`
	U_BACKUPS      string `json:"U_BACKUPS"`
	LANGUAGE       string `json:"LANGUAGE"`
	TIME           string `json:"TIME"`
	DATE           string `json:"DATE"`
}

func (u *UserEntry) SetName(name string) { u.USER = name }

// UserDetail is the one user from `v-list-user`, as the account and edit-user forms
// need it. USER is the object key; NAME is the contact name. LOGIN_ALLOW_IPS is
// comma-separated. Whether 2FA is on is reported separately, as a bool, never TWOFA.
type UserDetail struct {
	USER             string `json:"USER"`
	NAME             string `json:"NAME"`
	PACKAGE          string `json:"PACKAGE"`
	NS               string `json:"NS"`
	SHELL            string `json:"SHELL"`
	CONTACT          string `json:"CONTACT"`
	ROLE             string `json:"ROLE"`
	SUSPENDED        string `json:"SUSPENDED"`
	LANGUAGE         string `json:"LANGUAGE"`
	THEME            string `json:"THEME"`
	PREF_UI_SORT     string `json:"PREF_UI_SORT"`
	LOGIN_DISABLED   string `json:"LOGIN_DISABLED"`
	LOGIN_USE_IPLIST string `json:"LOGIN_USE_IPLIST"`
	LOGIN_ALLOW_IPS  string `json:"LOGIN_ALLOW_IPS"`
	PHPCLI           string `json:"PHPCLI"`
	TIME             string `json:"TIME"`
	DATE             string `json:"DATE"`

	// The package limits copied into the user's record, so a non-admin can see them
	// without reading the admin-only package list.
	DISK_QUOTA   string `json:"DISK_QUOTA"`
	BANDWIDTH    string `json:"BANDWIDTH"`
	WEB_DOMAINS  string `json:"WEB_DOMAINS"`
	DNS_DOMAINS  string `json:"DNS_DOMAINS"`
	MAIL_DOMAINS string `json:"MAIL_DOMAINS"`
	DATABASES    string `json:"DATABASES"`
	CRON_JOBS    string `json:"CRON_JOBS"`
	BACKUPS      string `json:"BACKUPS"`
}

func (u *UserDetail) SetName(name string) { u.USER = name }

// PackageEntry is one package from `v-list-user-packages` (newest modified first).
// NAME is the object key. Limits are a number or "unlimited".
type PackageEntry struct {
	NAME             string `json:"NAME"`
	WEB_TEMPLATE     string `json:"WEB_TEMPLATE"`
	PROXY_TEMPLATE   string `json:"PROXY_TEMPLATE"`
	DNS_TEMPLATE     string `json:"DNS_TEMPLATE"`
	WEB_DOMAINS      string `json:"WEB_DOMAINS"`
	WEB_ALIASES      string `json:"WEB_ALIASES"`
	DNS_DOMAINS      string `json:"DNS_DOMAINS"`
	DNS_RECORDS      string `json:"DNS_RECORDS"`
	MAIL_DOMAINS     string `json:"MAIL_DOMAINS"`
	MAIL_ACCOUNTS    string `json:"MAIL_ACCOUNTS"`
	DATABASES        string `json:"DATABASES"`
	CRON_JOBS        string `json:"CRON_JOBS"`
	DISK_QUOTA       string `json:"DISK_QUOTA"`
	CPU_QUOTA        string `json:"CPU_QUOTA"`
	CPU_QUOTA_PERIOD string `json:"CPU_QUOTA_PERIOD"`
	MEMORY_LIMIT     string `json:"MEMORY_LIMIT"`
	SWAP_LIMIT       string `json:"SWAP_LIMIT"`
	BANDWIDTH        string `json:"BANDWIDTH"`
	NS               string `json:"NS"`
	SHELL            string `json:"SHELL"`
	BACKUPS          string `json:"BACKUPS"`
	TIME             string `json:"TIME"`
	DATE             string `json:"DATE"`
}

func (p *PackageEntry) SetName(name string) { p.NAME = name }

// Package is the one package from `v-list-user-package`, with the keys the list
// leaves out (BACKEND_TEMPLATE, RATE_LIMIT, BACKUPS_INCREMENTAL). NAME is the object key.
type Package struct {
	NAME                string `json:"NAME"`
	WEB_TEMPLATE        string `json:"WEB_TEMPLATE"`
	BACKEND_TEMPLATE    string `json:"BACKEND_TEMPLATE"`
	PROXY_TEMPLATE      string `json:"PROXY_TEMPLATE"`
	DNS_TEMPLATE        string `json:"DNS_TEMPLATE"`
	WEB_DOMAINS         string `json:"WEB_DOMAINS"`
	WEB_ALIASES         string `json:"WEB_ALIASES"`
	DNS_DOMAINS         string `json:"DNS_DOMAINS"`
	DNS_RECORDS         string `json:"DNS_RECORDS"`
	MAIL_DOMAINS        string `json:"MAIL_DOMAINS"`
	MAIL_ACCOUNTS       string `json:"MAIL_ACCOUNTS"`
	RATE_LIMIT          string `json:"RATE_LIMIT"`
	DATABASES           string `json:"DATABASES"`
	CRON_JOBS           string `json:"CRON_JOBS"`
	DISK_QUOTA          string `json:"DISK_QUOTA"`
	CPU_QUOTA           string `json:"CPU_QUOTA"`
	CPU_QUOTA_PERIOD    string `json:"CPU_QUOTA_PERIOD"`
	MEMORY_LIMIT        string `json:"MEMORY_LIMIT"`
	SWAP_LIMIT          string `json:"SWAP_LIMIT"`
	BANDWIDTH           string `json:"BANDWIDTH"`
	NS                  string `json:"NS"`
	SHELL               string `json:"SHELL"`
	BACKUPS             string `json:"BACKUPS"`
	BACKUPS_INCREMENTAL string `json:"BACKUPS_INCREMENTAL"`
	TIME                string `json:"TIME"`
	DATE                string `json:"DATE"`
}

func (p *Package) SetName(name string) { p.NAME = name }

// CronJob is one job from `v-list-cron-jobs` / `v-list-cron-job`. CMD is verbatim
// (Hestia's %quote% already turned back into '). The object key is JOB.
type CronJob struct {
	MIN       string `json:"MIN"`
	HOUR      string `json:"HOUR"`
	DAY       string `json:"DAY"`
	MONTH     string `json:"MONTH"`
	WDAY      string `json:"WDAY"`
	CMD       string `json:"CMD"`
	JOB       string `json:"JOB"`
	SUSPENDED string `json:"SUSPENDED"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
}

func (c *CronJob) SetName(name string) { c.JOB = name }

// Backup is one backup from `v-list-user-backups` / `v-list-user-backup`. NAME is the
// file name (the object key). SIZE is MB, RUNTIME minutes, CRON a count; the lists are
// comma-joined.
type Backup struct {
	NAME    string `json:"NAME"`
	TYPE    string `json:"TYPE"`
	SIZE    string `json:"SIZE"`
	WEB     string `json:"WEB"`
	DNS     string `json:"DNS"`
	MAIL    string `json:"MAIL"`
	DB      string `json:"DB"`
	CRON    string `json:"CRON"`
	UDIR    string `json:"UDIR"`
	RUNTIME string `json:"RUNTIME"`
	TIME    string `json:"TIME"`
	DATE    string `json:"DATE"`
}

func (b *Backup) SetName(name string) { b.NAME = name }

// BackupExclusions is `v-list-user-backup-exclusions`: six sections in Hestia's order,
// each entry in file order. Never null: an empty section is []. Decode it with
// DecodeBackupExclusions.
type BackupExclusions struct {
	WEB  []ExclusionEntry `json:"WEB"`
	DNS  []ExclusionEntry `json:"DNS"`
	MAIL []ExclusionEntry `json:"MAIL"`
	DB   []ExclusionEntry `json:"DB"`
	CRON []ExclusionEntry `json:"CRON"`
	USER []ExclusionEntry `json:"USER"`
}

// ExclusionEntry is one excluded object: Key is the domain, database or folder, and
// Value its comma-separated sub-items ("" for the whole object).
type ExclusionEntry struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

// Snapshot is one restic snapshot from `v-list-user-backups-restic` (restic's own
// JSON; other restic fields are skipped). Time is RFC 3339 in the snapshot's own
// offset. Decode the list with DecodeSnapshots.
type Snapshot struct {
	ShortID  string   `json:"short_id"`
	ID       string   `json:"id"`
	Time     string   `json:"time"`
	Hostname string   `json:"hostname"`
	Paths    []string `json:"paths"`
}

// SnapshotDetail is `v-list-user-backup-restic`: what a snapshot holds (no SIZE).
// Decode it with DecodeSnapshotDetail.
type SnapshotDetail struct {
	TYPE    string `json:"TYPE"`
	WEB     string `json:"WEB"`
	DNS     string `json:"DNS"`
	MAIL    string `json:"MAIL"`
	DB      string `json:"DB"`
	CRON    string `json:"CRON"`
	UDIR    string `json:"UDIR"`
	RUNTIME string `json:"RUNTIME"`
	TIME    string `json:"TIME"`
	DATE    string `json:"DATE"`
}

// SnapshotNode is one file or folder from `v-list-user-files-restic` (restic ls JSON
// Lines). Type is "dir", "file" or "symlink". Decode with DecodeSnapshotNodes.
type SnapshotNode struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Path  string `json:"path"`
	Size  int64  `json:"size"` // bytes; restic gives it for files only (0 for folders)
	Ctime string `json:"ctime"`
}

// SSHKey is one key from `v-list-user-ssh-key`: ID is the key's comment (the object
// key), KEY its SHA256 fingerprint.
type SSHKey struct {
	ID  string `json:"ID"`
	KEY string `json:"KEY"`
}

func (k *SSHKey) SetName(name string) { k.ID = name }

// AccessKey is one API access key from `v-list-access-keys` or `v-list-access-key`.
// The secret is never decoded. PERMISSIONS is a list of API names ([] for none).
type AccessKey struct {
	ACCESS_KEY_ID string   `json:"ACCESS_KEY_ID"`
	USER          string   `json:"USER"`
	PERMISSIONS   []string `json:"PERMISSIONS"`
	COMMENT       string   `json:"COMMENT"`
	TIME          string   `json:"TIME"`
	DATE          string   `json:"DATE"`
}

func (k *AccessKey) SetName(name string) { k.ACCESS_KEY_ID = name }

// APIInfo is one API from `v-list-apis`: Name is the object key, ROLE "admin" or "user".
type APIInfo struct {
	Name     string   `json:"Name"`
	COMMANDS []string `json:"COMMANDS"`
	ROLE     string   `json:"ROLE"`
}

func (a *APIInfo) SetName(name string) { a.Name = name }

// Notification is one panel notification from `v-list-user-notifications`. ID is the
// NID (the object key). NOTICE is HTML from Hestia: render it only through the
// allowlist renderer. ACK is "yes" or "no"; PRIORITY a number ("0" when unset).
type Notification struct {
	ID       string `json:"ID"`
	TOPIC    string `json:"TOPIC"`
	NOTICE   string `json:"NOTICE"`
	TYPE     string `json:"TYPE"`
	ACK      string `json:"ACK"`
	PRIORITY string `json:"PRIORITY"`
	TIME     string `json:"TIME"`
	DATE     string `json:"DATE"`
}

func (n *Notification) SetName(name string) { n.ID = name }

// LogEntry is one line of `v-list-user-log` (oldest first). ID is the object key.
// LEVEL is Info, Warning or Error.
type LogEntry struct {
	ID       string `json:"ID"`
	DATE     string `json:"DATE"`
	TIME     string `json:"TIME"`
	LEVEL    string `json:"LEVEL"`
	CATEGORY string `json:"CATEGORY"`
	MESSAGE  string `json:"MESSAGE"`
}

func (l *LogEntry) SetName(name string) { l.ID = name }

// AuthLogEntry is one line of `v-list-user-auth-log` (oldest first). ID is the object
// key. The session ID and ACTIVE flag are never decoded.
type AuthLogEntry struct {
	ID         string `json:"ID"`
	DATE       string `json:"DATE"`
	TIME       string `json:"TIME"`
	IP         string `json:"IP"`
	ACTION     string `json:"ACTION"`
	STATUS     string `json:"STATUS"`
	USER_AGENT string `json:"USER_AGENT"`
}

func (l *AuthLogEntry) SetName(name string) { l.ID = name }

// DecodeBackupExclusions decodes `v-list-user-backup-exclusions <u> json`, keeping
// each section's entry order. A missing section is empty, never null. Of duplicate
// keys (two entries for one domain) both are kept, in order.
func DecodeBackupExclusions(stdout []byte) (BackupExclusions, error) {
	ex := BackupExclusions{WEB: []ExclusionEntry{}, DNS: []ExclusionEntry{}, MAIL: []ExclusionEntry{},
		DB: []ExclusionEntry{}, CRON: []ExclusionEntry{}, USER: []ExclusionEntry{}}
	sections := map[string]*[]ExclusionEntry{"WEB": &ex.WEB, "DNS": &ex.DNS, "MAIL": &ex.MAIL,
		"DB": &ex.DB, "CRON": &ex.CRON, "USER": &ex.USER}
	dec := json.NewDecoder(bytes.NewReader(stdout))
	if err := expectDelim(dec, '{'); err != nil {
		return ex, err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return ex, err
		}
		key, _ := tok.(string)
		if err := expectDelim(dec, '{'); err != nil {
			return ex, err
		}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return ex, err
			}
			var v string
			if err := dec.Decode(&v); err != nil {
				return ex, err
			}
			if list := sections[key]; list != nil {
				*list = append(*list, ExclusionEntry{Key: k.(string), Value: v})
			}
		}
		if _, err := dec.Token(); err != nil {
			return ex, err
		}
	}
	_, err := dec.Token()
	return ex, err
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return errors.New("hestia: unexpected JSON shape")
	}
	return nil
}

// DecodeSnapshots decodes `v-list-user-backups-restic <u> json` (restic's array,
// oldest first). An empty repository ([] or null) and no output at all (no
// repository) both give an empty list.
func DecodeSnapshots(stdout []byte) ([]Snapshot, error) {
	out := []Snapshot{}
	if len(bytes.TrimSpace(stdout)) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(stdout, &out); err != nil {
		return []Snapshot{}, err
	}
	if out == nil {
		out = []Snapshot{}
	}
	return out, nil
}

// DecodeSnapshotDetail decodes `v-list-user-backup-restic <u> <id> json`, whose one
// key is the literal "snapshot". Anything else (empty or garbled output, a snapshot
// with no backup.conf) is an error.
func DecodeSnapshotDetail(stdout []byte) (SnapshotDetail, error) {
	var wrapped map[string]*SnapshotDetail
	if err := json.Unmarshal(stdout, &wrapped); err != nil {
		return SnapshotDetail{}, err
	}
	d := wrapped["snapshot"]
	if d == nil {
		return SnapshotDetail{}, errors.New("hestia: no snapshot details")
	}
	return *d, nil
}

// DecodeSnapshotNodes decodes `v-list-user-files-restic` (JSON Lines). Line 0 is the
// snapshot itself and is skipped; blank lines are ignored. Restic lists the folder
// itself first, then its direct children; the order is kept.
func DecodeSnapshotNodes(stdout []byte) ([]SnapshotNode, error) {
	out := []SnapshotNode{}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if first {
			first = false
			continue
		}
		var n SnapshotNode
		if err := json.Unmarshal(line, &n); err != nil {
			return []SnapshotNode{}, err
		}
		out = append(out, n)
	}
	return out, sc.Err()
}

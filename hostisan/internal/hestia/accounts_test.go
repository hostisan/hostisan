package hestia

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The JSON below is the parity checklists' examples (users §6, account §6, cron §6,
// backup §6.3), trimmed where a type skips keys. Each test decodes it and checks every
// field and the order.

// jsonKeys lists a struct's JSON names in field order.
func jsonKeys(v any) []string {
	t := reflect.TypeOf(v)
	var out []string
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		out = append(out, name)
	}
	return out
}

func assertKeys(t *testing.T, v any, want string) {
	t.Helper()
	if got := strings.Join(jsonKeys(v), ", "); got != want {
		t.Errorf("%T keys\n got: %s\nwant: %s", v, got, want)
	}
}

func TestAccountTypeKeyOrder(t *testing.T) {
	assertKeys(t, UserEntry{}, "USER, NAME, PACKAGE, DISK_QUOTA, BANDWIDTH, CONTACT, ROLE, SUSPENDED, IP_OWNED, U_DISK, U_BANDWIDTH, U_WEB_DOMAINS, U_DNS_DOMAINS, U_MAIL_DOMAINS, U_DATABASES, U_BACKUPS, LANGUAGE, TIME, DATE")
	assertKeys(t, UserDetail{}, "USER, NAME, PACKAGE, NS, SHELL, CONTACT, ROLE, SUSPENDED, LANGUAGE, THEME, PREF_UI_SORT, LOGIN_DISABLED, LOGIN_USE_IPLIST, LOGIN_ALLOW_IPS, PHPCLI, TIME, DATE, DISK_QUOTA, BANDWIDTH, WEB_DOMAINS, DNS_DOMAINS, MAIL_DOMAINS, DATABASES, CRON_JOBS, BACKUPS")
	assertKeys(t, PackageEntry{}, "NAME, WEB_TEMPLATE, PROXY_TEMPLATE, DNS_TEMPLATE, WEB_DOMAINS, WEB_ALIASES, DNS_DOMAINS, DNS_RECORDS, MAIL_DOMAINS, MAIL_ACCOUNTS, DATABASES, CRON_JOBS, DISK_QUOTA, CPU_QUOTA, CPU_QUOTA_PERIOD, MEMORY_LIMIT, SWAP_LIMIT, BANDWIDTH, NS, SHELL, BACKUPS, TIME, DATE")
	assertKeys(t, Package{}, "NAME, WEB_TEMPLATE, BACKEND_TEMPLATE, PROXY_TEMPLATE, DNS_TEMPLATE, WEB_DOMAINS, WEB_ALIASES, DNS_DOMAINS, DNS_RECORDS, MAIL_DOMAINS, MAIL_ACCOUNTS, RATE_LIMIT, DATABASES, CRON_JOBS, DISK_QUOTA, CPU_QUOTA, CPU_QUOTA_PERIOD, MEMORY_LIMIT, SWAP_LIMIT, BANDWIDTH, NS, SHELL, BACKUPS, BACKUPS_INCREMENTAL, TIME, DATE")
	assertKeys(t, CronJob{}, "MIN, HOUR, DAY, MONTH, WDAY, CMD, JOB, SUSPENDED, TIME, DATE")
	assertKeys(t, Backup{}, "NAME, TYPE, SIZE, WEB, DNS, MAIL, DB, CRON, UDIR, RUNTIME, TIME, DATE")
	assertKeys(t, BackupExclusions{}, "WEB, DNS, MAIL, DB, CRON, USER")
	assertKeys(t, ExclusionEntry{}, "Key, Value")
	assertKeys(t, Snapshot{}, "short_id, id, time, hostname, paths")
	assertKeys(t, SnapshotDetail{}, "TYPE, WEB, DNS, MAIL, DB, CRON, UDIR, RUNTIME, TIME, DATE")
	assertKeys(t, SnapshotNode{}, "name, type, path, size, ctime")
	assertKeys(t, SSHKey{}, "ID, KEY")
	assertKeys(t, AccessKey{}, "ACCESS_KEY_ID, USER, PERMISSIONS, COMMENT, TIME, DATE")
	assertKeys(t, APIInfo{}, "Name, COMMANDS, ROLE")
	assertKeys(t, Notification{}, "ID, TOPIC, NOTICE, TYPE, ACK, PRIORITY, TIME, DATE")
	assertKeys(t, LogEntry{}, "ID, DATE, TIME, LEVEL, CATEGORY, MESSAGE")
	assertKeys(t, AuthLogEntry{}, "ID, DATE, TIME, IP, ACTION, STATUS, USER_AGENT")
}

// Every value of a Hestia type is decoded verbatim; none of them may hold a secret.
func TestAccountTypesHaveNoSecretFields(t *testing.T) {
	for _, v := range []any{UserEntry{}, UserDetail{}, PackageEntry{}, Package{}, CronJob{}, Backup{},
		BackupExclusions{}, ExclusionEntry{}, Snapshot{}, SnapshotDetail{}, SnapshotNode{}, SSHKey{},
		AccessKey{}, APIInfo{}, Notification{}, LogEntry{}, AuthLogEntry{}} {
		for _, k := range jsonKeys(v) {
			switch strings.ToUpper(k) {
			case "RKEY", "TWOFA", "QRCODE", "SESSION", "SECRET_ACCESS_KEY", "ACTIVE":
				t.Errorf("%T has %s", v, k)
			}
		}
	}
}

const usersJSON = `{
    "admin": {
        "NAME": "System Administrator", "PACKAGE": "system", "WEB_TEMPLATE": "default",
        "BACKEND_TEMPLATE": "default", "PROXY_TEMPLATE": "default", "DNS_TEMPLATE": "default",
        "WEB_DOMAINS": "1", "WEB_ALIASES": "1", "DNS_DOMAINS": "1", "DNS_RECORDS": "unlimited",
        "MAIL_DOMAINS": "1", "MAIL_ACCOUNTS": "1", "DATABASES": "0", "CRON_JOBS": "unlimited",
        "DISK_QUOTA": "unlimited", "CPU_QUOTA": "unlimited", "CPU_QUOTA_PERIOD": "unlimited",
        "MEMORY_LIMIT": "unlimited", "SWAP_LIMIT": "unlimited", "BANDWIDTH": "unlimited",
        "NS": "ns1.acme-hosting.net,ns2.acme-hosting.net", "SHELL": "bash", "BACKUPS": "1",
        "CONTACT": "admin@acme-hosting.net", "CRON_REPORTS": "yes", "RKEY": "k3Jx9sVq2LmT7wZp",
        "ROLE": "admin", "SUSPENDED": "no", "SUSPENDED_USERS": "1", "SUSPENDED_WEB": "0",
        "SUSPENDED_DNS": "0", "SUSPENDED_MAIL": "0", "SUSPENDED_DB": "0", "SUSPENDED_CRON": "0",
        "IP_AVAIL": "1", "IP_OWNED": "1", "U_USERS": "3", "U_DISK": "412", "U_DISK_DIRS": "412",
        "U_DISK_WEB": "0", "U_DISK_MAIL": "0", "U_DISK_DB": "0", "U_BANDWIDTH": "0",
        "U_WEB_DOMAINS": "0", "U_WEB_SSL": "0", "U_WEB_ALIASES": "0", "U_DNS_DOMAINS": "0",
        "U_DNS_RECORDS": "0", "U_MAIL_DOMAINS": "0", "U_MAIL_DKIM": "0", "U_MAIL_ACCOUNTS": "0",
        "U_DATABASES": "0", "U_CRON_JOBS": "2", "U_BACKUPS": "0", "LANGUAGE": "en",
        "TIME": "08:00:00", "DATE": "2026-01-10"
    },
    "bob": {
        "NAME": "Bob Stone", "PACKAGE": "starter", "DISK_QUOTA": "1000", "BANDWIDTH": "1000",
        "CONTACT": "bob@example.org", "RKEY": "Zx4cV6bN8mA1sDfG", "ROLE": "user", "SUSPENDED": "yes",
        "IP_OWNED": "0", "U_DISK": "1500", "U_BANDWIDTH": "120", "U_WEB_DOMAINS": "1",
        "U_DNS_DOMAINS": "0", "U_MAIL_DOMAINS": "0", "U_DATABASES": "0", "U_BACKUPS": "0",
        "LANGUAGE": "de", "TIME": "16:05:09", "DATE": "2026-04-18"
    }
}`

func TestUserEntryDecode(t *testing.T) {
	got, err := DecodeObjects[UserEntry]([]byte(usersJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []UserEntry{
		{USER: "admin", NAME: "System Administrator", PACKAGE: "system", DISK_QUOTA: "unlimited",
			BANDWIDTH: "unlimited", CONTACT: "admin@acme-hosting.net", ROLE: "admin", SUSPENDED: "no",
			IP_OWNED: "1", U_DISK: "412", U_BANDWIDTH: "0", U_WEB_DOMAINS: "0", U_DNS_DOMAINS: "0",
			U_MAIL_DOMAINS: "0", U_DATABASES: "0", U_BACKUPS: "0", LANGUAGE: "en", TIME: "08:00:00", DATE: "2026-01-10"},
		{USER: "bob", NAME: "Bob Stone", PACKAGE: "starter", DISK_QUOTA: "1000", BANDWIDTH: "1000",
			CONTACT: "bob@example.org", ROLE: "user", SUSPENDED: "yes", IP_OWNED: "0", U_DISK: "1500",
			U_BANDWIDTH: "120", U_WEB_DOMAINS: "1", U_DNS_DOMAINS: "0", U_MAIL_DOMAINS: "0",
			U_DATABASES: "0", U_BACKUPS: "0", LANGUAGE: "de", TIME: "16:05:09", DATE: "2026-04-18"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

const userJSON = `{
    "alice": {
        "NAME": "Alice Smith", "PACKAGE": "default", "WEB_TEMPLATE": "default", "HOME": "/home/alice",
        "NS": "ns1.example.com,ns2.example.com", "SHELL": "nologin", "BACKUPS": "3",
        "CONTACT": "alice@example.com", "CRON_REPORTS": "no",
        "RKEY": "$2y$10$Zt3m1bX0q9v5oQ7wKj8yXe", "TWOFA": "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
        "QRCODE": "data:image/png;base64,iVBORw0KGgo...", "ROLE": "user", "SUSPENDED": "no",
        "BACKUPS_INCREMENTAL": "no", "LANGUAGE": "en", "THEME": "dark", "NOTIFICATIONS": "yes",
        "PREF_UI_SORT": "date", "LOGIN_DISABLED": "no", "LOGIN_USE_IPLIST": "yes",
        "LOGIN_ALLOW_IPS": "203.0.113.5,198.51.100.7", "PHPCLI": "8.3",
        "TIME": "09:41:07", "DATE": "2025-03-14"
        }
}`

func TestUserDetailDecode(t *testing.T) {
	got, err := DecodeObjects[UserDetail]([]byte(userJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := UserDetail{USER: "alice", NAME: "Alice Smith", PACKAGE: "default", NS: "ns1.example.com,ns2.example.com",
		SHELL: "nologin", CONTACT: "alice@example.com", ROLE: "user", SUSPENDED: "no", LANGUAGE: "en",
		THEME: "dark", PREF_UI_SORT: "date", LOGIN_DISABLED: "no", LOGIN_USE_IPLIST: "yes",
		LOGIN_ALLOW_IPS: "203.0.113.5,198.51.100.7", PHPCLI: "8.3", TIME: "09:41:07", DATE: "2025-03-14",
		BACKUPS: "3"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	b, _ := json.Marshal(got[0])
	for _, s := range []string{"JBSWY3DP", "RKEY", "$2y$", "QRCODE", "base64"} {
		if strings.Contains(string(b), s) {
			t.Errorf("re-encoded detail holds %q: %s", s, b)
		}
	}
}

const packagesJSON = `{
    "starter": {
        "WEB_TEMPLATE": "default", "PROXY_TEMPLATE": "default", "DNS_TEMPLATE": "default",
        "WEB_DOMAINS": "1", "WEB_ALIASES": "5", "DNS_DOMAINS": "1", "DNS_RECORDS": "unlimited",
        "MAIL_DOMAINS": "1", "MAIL_ACCOUNTS": "5", "DATABASES": "1", "CRON_JOBS": "1",
        "DISK_QUOTA": "1000",
        "CPU_QUOTA":"unlimited",
        "CPU_QUOTA_PERIOD":"unlimited",
        "MEMORY_LIMIT":"unlimited",
        "SWAP_LIMIT":"unlimited",
        "BANDWIDTH": "1000", "NS": "ns1.example.tld,ns2.example.tld", "SHELL": "nologin",
        "BACKUPS": "1", "TIME": "11:30:02", "DATE": "2026-04-01"
    },
    "default": {"WEB_TEMPLATE": "default", "DISK_QUOTA": "unlimited", "TIME": "18:00:00", "DATE": "2019-01-15"}
}`

func TestPackageEntryDecode(t *testing.T) {
	got, err := DecodeObjects[PackageEntry]([]byte(packagesJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := PackageEntry{NAME: "starter", WEB_TEMPLATE: "default", PROXY_TEMPLATE: "default", DNS_TEMPLATE: "default",
		WEB_DOMAINS: "1", WEB_ALIASES: "5", DNS_DOMAINS: "1", DNS_RECORDS: "unlimited", MAIL_DOMAINS: "1",
		MAIL_ACCOUNTS: "5", DATABASES: "1", CRON_JOBS: "1", DISK_QUOTA: "1000", CPU_QUOTA: "unlimited",
		CPU_QUOTA_PERIOD: "unlimited", MEMORY_LIMIT: "unlimited", SWAP_LIMIT: "unlimited", BANDWIDTH: "1000",
		NS: "ns1.example.tld,ns2.example.tld", SHELL: "nologin", BACKUPS: "1", TIME: "11:30:02", DATE: "2026-04-01"}
	if len(got) != 2 || got[0] != want || got[1].NAME != "default" {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

const packageJSON = `{
    "starter": {
        "WEB_TEMPLATE": "default", "BACKEND_TEMPLATE": "default", "PROXY_TEMPLATE": "default",
        "DNS_TEMPLATE": "default", "WEB_DOMAINS": "1", "WEB_ALIASES": "5", "DNS_DOMAINS": "1",
        "DNS_RECORDS": "unlimited", "MAIL_DOMAINS": "1", "MAIL_ACCOUNTS": "5", "RATE_LIMIT": "200",
        "DATABASES": "1", "CRON_JOBS": "1", "DISK_QUOTA": "1000",
        "CPU_QUOTA":"unlimited",
        "CPU_QUOTA_PERIOD":"unlimited",
        "MEMORY_LIMIT":"unlimited",
        "SWAP_LIMIT":"unlimited",
        "BANDWIDTH": "1000", "NS": "ns1.example.tld,ns2.example.tld", "SHELL": "nologin", "BACKUPS": "1",
	"BACKUPS_INCREMENTAL": "no",
        "TIME": "11:30:02", "DATE": "2026-04-01"
    }
}`

func TestPackageDecode(t *testing.T) {
	got, err := DecodeObjects[Package]([]byte(packageJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := Package{NAME: "starter", WEB_TEMPLATE: "default", BACKEND_TEMPLATE: "default", PROXY_TEMPLATE: "default",
		DNS_TEMPLATE: "default", WEB_DOMAINS: "1", WEB_ALIASES: "5", DNS_DOMAINS: "1", DNS_RECORDS: "unlimited",
		MAIL_DOMAINS: "1", MAIL_ACCOUNTS: "5", RATE_LIMIT: "200", DATABASES: "1", CRON_JOBS: "1", DISK_QUOTA: "1000",
		CPU_QUOTA: "unlimited", CPU_QUOTA_PERIOD: "unlimited", MEMORY_LIMIT: "unlimited", SWAP_LIMIT: "unlimited",
		BANDWIDTH: "1000", NS: "ns1.example.tld,ns2.example.tld", SHELL: "nologin", BACKUPS: "1",
		BACKUPS_INCREMENTAL: "no", TIME: "11:30:02", DATE: "2026-04-01"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

const cronJSON = `{
    "1": {
        "MIN": "*/15", "HOUR": "*", "DAY": "*", "MONTH": "*", "WDAY": "*",
        "CMD": "sudo /usr/local/hestia/bin/v-update-sys-queue disk",
        "JOB": "1", "SUSPENDED": "no", "TIME": "06:12:44", "DATE": "2025-03-02"
    },
    "5": {
        "MIN": "0", "HOUR": "12", "DAY": "1", "MONTH": "*/3", "WDAY": "*",
        "CMD": "echo \"quarterly\" > /tmp/it's-ok.txt",
        "JOB": "5", "SUSPENDED": "yes", "TIME": "09:00:00", "DATE": "2025-06-30"
    }
}`

func TestCronJobDecode(t *testing.T) {
	got, err := DecodeObjects[CronJob]([]byte(cronJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []CronJob{
		{MIN: "*/15", HOUR: "*", DAY: "*", MONTH: "*", WDAY: "*", CMD: "sudo /usr/local/hestia/bin/v-update-sys-queue disk",
			JOB: "1", SUSPENDED: "no", TIME: "06:12:44", DATE: "2025-03-02"},
		{MIN: "0", HOUR: "12", DAY: "1", MONTH: "*/3", WDAY: "*", CMD: `echo "quarterly" > /tmp/it's-ok.txt`,
			JOB: "5", SUSPENDED: "yes", TIME: "09:00:00", DATE: "2025-06-30"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

const backupsJSON = `{
    "admin.2026-10-19_03-00-02.tar": {
        "TYPE": "local", "SIZE": "312", "WEB": "example.com,shop.example.com", "DNS": "example.com",
        "MAIL": "example.com", "DB": "admin_wp,admin_shop", "CRON": "3", "UDIR": "backup,.ssh",
        "RUNTIME": "2", "TIME": "03:02:11", "DATE": "2026-10-19"
    },
    "admin.2026-10-20_03-00-01.tar": {
        "TYPE": "local,sftp", "SIZE": "1243", "WEB": "example.com,shop.example.com", "DNS": "",
        "MAIL": "example.com", "DB": "admin_wp,admin_shop", "CRON": "3", "UDIR": "",
        "RUNTIME": "75", "TIME": "04:15:40", "DATE": "2026-10-20"
    }
}`

func TestBackupDecode(t *testing.T) {
	got, err := DecodeObjects[Backup]([]byte(backupsJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []Backup{
		{NAME: "admin.2026-10-19_03-00-02.tar", TYPE: "local", SIZE: "312", WEB: "example.com,shop.example.com",
			DNS: "example.com", MAIL: "example.com", DB: "admin_wp,admin_shop", CRON: "3", UDIR: "backup,.ssh",
			RUNTIME: "2", TIME: "03:02:11", DATE: "2026-10-19"},
		{NAME: "admin.2026-10-20_03-00-01.tar", TYPE: "local,sftp", SIZE: "1243", WEB: "example.com,shop.example.com",
			MAIL: "example.com", DB: "admin_wp,admin_shop", CRON: "3", RUNTIME: "75", TIME: "04:15:40", DATE: "2026-10-20"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

const exclusionsJSON = `{
    "WEB": {
        "example.com": "public_html/cache,public_html/tmp",
        "shop.example.com": ""
    },
    "DNS": {
    },
    "MAIL": {
        "example.com": "info,support"
    },
    "DB": {
        "admin_wp": ""
    },
    "CRON": {
        "*": ""
    },
    "USER": {
        "Downloads": "",
        ".cache": ""
    }
}`

func TestBackupExclusionsDecode(t *testing.T) {
	got, err := DecodeBackupExclusions([]byte(exclusionsJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := BackupExclusions{
		WEB:  []ExclusionEntry{{"example.com", "public_html/cache,public_html/tmp"}, {"shop.example.com", ""}},
		DNS:  []ExclusionEntry{},
		MAIL: []ExclusionEntry{{"example.com", "info,support"}},
		DB:   []ExclusionEntry{{"admin_wp", ""}},
		CRON: []ExclusionEntry{{"*", ""}},
		USER: []ExclusionEntry{{"Downloads", ""}, {".cache", ""}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	// A missing section and an empty document both give empty lists, never null.
	empty, err := DecodeBackupExclusions([]byte("{\n}"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(empty)
	if string(b) != `{"WEB":[],"DNS":[],"MAIL":[],"DB":[],"CRON":[],"USER":[]}` {
		t.Errorf("empty exclusions = %s", b)
	}
	if _, err := DecodeBackupExclusions([]byte(`[]`)); err == nil {
		t.Error("an array decoded as exclusions")
	}
}

const snapshotsJSON = `[
  {"time":"2026-10-19T03:00:04.211873519+02:00","parent":"a1b2c3d4","tree":"9f8e7d6c","paths":["/home/admin"],"hostname":"srv1.example.com","username":"root","uid":0,"gid":0,"program_version":"restic 0.16.4","id":"3c9b8a7e5d1f00000000000000000000000000000000000000000000000000aa","short_id":"3c9b8a7e"},
  {"time":"2026-10-20T03:00:05.532190021+02:00","parent":"3c9b8a7e","tree":"b7a6c5d4","paths":["/home/admin"],"hostname":"srv1.example.com","username":"root","uid":0,"gid":0,"program_version":"restic 0.16.4","id":"e41f0a29bc7700000000000000000000000000000000000000000000000000bb","short_id":"e41f0a29"}
]`

func TestSnapshotsDecode(t *testing.T) {
	got, err := DecodeSnapshots([]byte(snapshotsJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []Snapshot{
		{ShortID: "3c9b8a7e", ID: "3c9b8a7e5d1f00000000000000000000000000000000000000000000000000aa",
			Time: "2026-10-19T03:00:04.211873519+02:00", Hostname: "srv1.example.com", Paths: []string{"/home/admin"}},
		{ShortID: "e41f0a29", ID: "e41f0a29bc7700000000000000000000000000000000000000000000000000bb",
			Time: "2026-10-20T03:00:05.532190021+02:00", Hostname: "srv1.example.com", Paths: []string{"/home/admin"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	// An empty repository prints [] or null; a missing one prints nothing.
	for _, in := range []string{"[]", "null", "", " \n"} {
		got, err := DecodeSnapshots([]byte(in))
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("DecodeSnapshots(%q) = %v, %v; want [] and no error", in, got, err)
		}
	}
}

const snapshotDetailJSON = "{\n    \"snapshot\": {\n\t\t\"TYPE\": \"restic\",\n\t\t\"WEB\": \"example.com,shop.example.com\",\n\t\t\"DNS\": \"example.com\",\n\t\t\"MAIL\": \"example.com\",\n\t\t\"DB\": \"admin_wp\",\n\t\t\"CRON\": \"3\",\n\t\t\"UDIR\": \"backup,.ssh\",\n\t\t\"RUNTIME\": \"1\",\n\t\t\"TIME\": \"03:00:12\",\n\t\t\"DATE\": \"2026-10-20\"\n\t}\n}"

func TestSnapshotDetailDecode(t *testing.T) {
	got, err := DecodeSnapshotDetail([]byte(snapshotDetailJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := SnapshotDetail{TYPE: "restic", WEB: "example.com,shop.example.com", DNS: "example.com",
		MAIL: "example.com", DB: "admin_wp", CRON: "3", UDIR: "backup,.ssh", RUNTIME: "1", TIME: "03:00:12", DATE: "2026-10-20"}
	if got != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	for _, in := range []string{"", "{}", `{"other": {}}`, "garbled"} {
		if _, err := DecodeSnapshotDetail([]byte(in)); err == nil {
			t.Errorf("DecodeSnapshotDetail(%q) gave no error", in)
		}
	}
}

const snapshotFilesJSONL = `{"time":"2026-10-20T03:00:05.5321+02:00","tree":"b7a6c5d4","paths":["/home/admin"],"hostname":"srv1.example.com","username":"root","uid":0,"gid":0,"id":"e41f0a29bc77","short_id":"e41f0a29","struct_type":"snapshot"}
{"name":"admin","type":"dir","path":"/home/admin","uid":1001,"gid":1001,"mode":2147484141,"permissions":"drwxr-xr-x","mtime":"2026-09-01T10:00:00+02:00","atime":"2026-10-20T03:00:01+02:00","ctime":"2026-09-01T10:00:00+02:00","struct_type":"node"}
{"name":"Documents","type":"dir","path":"/home/admin/Documents","uid":1001,"gid":1001,"mode":2147484141,"permissions":"drwxr-xr-x","mtime":"2026-09-01T10:00:00+02:00","atime":"2026-10-20T03:00:01+02:00","ctime":"2026-09-01T10:00:00+02:00","struct_type":"node"}
{"name":"notes.txt","type":"file","path":"/home/admin/notes.txt","uid":1001,"gid":1001,"size":2048,"mode":420,"permissions":"-rw-r--r--","mtime":"x","atime":"x","ctime":"2026-10-18T09:12:44.1+02:00","struct_type":"node"}

`

func TestSnapshotNodesDecode(t *testing.T) {
	got, err := DecodeSnapshotNodes([]byte(snapshotFilesJSONL))
	if err != nil {
		t.Fatal(err)
	}
	want := []SnapshotNode{
		{Name: "admin", Type: "dir", Path: "/home/admin", Ctime: "2026-09-01T10:00:00+02:00"},
		{Name: "Documents", Type: "dir", Path: "/home/admin/Documents", Ctime: "2026-09-01T10:00:00+02:00"},
		{Name: "notes.txt", Type: "file", Path: "/home/admin/notes.txt", Size: 2048, Ctime: "2026-10-18T09:12:44.1+02:00"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	for _, in := range []string{"", "{\"struct_type\":\"snapshot\"}\n"} {
		got, err := DecodeSnapshotNodes([]byte(in))
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("DecodeSnapshotNodes(%q) = %v, %v; want []", in, got, err)
		}
	}
	if _, err := DecodeSnapshotNodes([]byte("{}\nnot json\n")); err == nil {
		t.Error("a garbled node line gave no error")
	}
}

const sshKeysJSON = `{
    "alice@laptop": {
        "ID": "alice@laptop",
        "KEY": "SHA256:Nl3rD1x0C0m8nQ8mZ7HbVd1uQ0pXy4F2tWgJ5eKsLhA"
        },
    "deploy key 2024": {
        "ID": "deploy key 2024",
        "KEY": "SHA256:eU2u7tqL0ZkQ4yW3d8p0n5bXr1sHcVjMf6aTgOiBzKo"
        }
}`

func TestSSHKeyDecode(t *testing.T) {
	got, err := DecodeObjects[SSHKey]([]byte(sshKeysJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []SSHKey{
		{ID: "alice@laptop", KEY: "SHA256:Nl3rD1x0C0m8nQ8mZ7HbVd1uQ0pXy4F2tWgJ5eKsLhA"},
		{ID: "deploy key 2024", KEY: "SHA256:eU2u7tqL0ZkQ4yW3d8p0n5bXr1sHcVjMf6aTgOiBzKo"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

const accessKeysJSON = `{
    "aB3dEfGh1JkLmN0pQrSt": {"ACCESS_KEY_ID": "aB3dEfGh1JkLmN0pQrSt", "USER": "alice", "PERMISSIONS": ["mail-accounts", "purge-nginx-cache"], "COMMENT": "CI deploy", "TIME": "09:41:07", "DATE": "2025-03-14"},
    "Zx9yWv8uTs7rQp6oNm5l": {"ACCESS_KEY_ID": "Zx9yWv8uTs7rQp6oNm5l", "USER": "alice", "PERMISSIONS": [], "COMMENT": "", "TIME": "10:02:55", "DATE": "2025-03-15"}
}`

const accessKeyJSON = `{
    "ACCESS_KEY_ID": "aB3dEfGh1JkLmN0pQrSt",
    "SECRET_ACCESS_KEY": "Qw3Rt5Yu7Io9Pa1Sd2Fg4Hj6Kl8Zx0Cv_B-n=M1b",
    "USER": "alice",
    "PERMISSIONS": ["mail-accounts", "purge-nginx-cache"],
    "COMMENT": "CI deploy",
    "TIME": "09:41:07",
    "DATE": "2025-03-14"
}`

func TestAccessKeyDecode(t *testing.T) {
	got, err := DecodeObjects[AccessKey]([]byte(accessKeysJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []AccessKey{
		{ACCESS_KEY_ID: "aB3dEfGh1JkLmN0pQrSt", USER: "alice", PERMISSIONS: []string{"mail-accounts", "purge-nginx-cache"},
			COMMENT: "CI deploy", TIME: "09:41:07", DATE: "2025-03-14"},
		{ACCESS_KEY_ID: "Zx9yWv8uTs7rQp6oNm5l", USER: "alice", PERMISSIONS: []string{}, TIME: "10:02:55", DATE: "2025-03-15"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	// The single-key view carries the secret; decoding into AccessKey drops it.
	var one AccessKey
	if err := json.Unmarshal([]byte(accessKeyJSON), &one); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one, want[0]) {
		t.Errorf("single key = %+v", one)
	}
	if b, _ := json.Marshal(one); strings.Contains(string(b), "Qw3Rt5") {
		t.Errorf("secret survived: %s", b)
	}
}

func TestAPIInfoDecode(t *testing.T) {
	got, err := DecodeObjects[APIInfo]([]byte(`{
    "billing": {"COMMANDS": ["v-add-user", "v-delete-user", "v-suspend-user"], "ROLE": "admin"},
    "mail-accounts": {"COMMANDS": ["v-list-mail-domains", "v-add-mail-account"], "ROLE": "user"}
}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []APIInfo{
		{Name: "billing", COMMANDS: []string{"v-add-user", "v-delete-user", "v-suspend-user"}, ROLE: "admin"},
		{Name: "mail-accounts", COMMANDS: []string{"v-list-mail-domains", "v-add-mail-account"}, ROLE: "user"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestNotificationDecode(t *testing.T) {
	got, err := DecodeObjects[Notification]([]byte(`{
    "1": {
        "TOPIC": "Backup failed",
        "NOTICE": "<p>Backup <span class='x'>alice</span> could not be created.</p>",
        "TYPE": "error", "ACK": "no", "PRIORITY": "1", "TIME": "03:00:12", "DATE": "2025-03-14"
    }
}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Notification{ID: "1", TOPIC: "Backup failed",
		NOTICE: "<p>Backup <span class='x'>alice</span> could not be created.</p>",
		TYPE:   "error", ACK: "no", PRIORITY: "1", TIME: "03:00:12", DATE: "2025-03-14"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestLogEntryDecode(t *testing.T) {
	got, err := DecodeObjects[LogEntry]([]byte(`{
    "41": {"DATE": "2025-03-14", "TIME": "09:41:07", "LEVEL": "Warning", "CATEGORY": "Security", "MESSAGE": "Password changed."}
}`))
	if err != nil {
		t.Fatal(err)
	}
	want := LogEntry{ID: "41", DATE: "2025-03-14", TIME: "09:41:07", LEVEL: "Warning", CATEGORY: "Security", MESSAGE: "Password changed."}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestAuthLogEntryDecode(t *testing.T) {
	got, err := DecodeObjects[AuthLogEntry]([]byte(`{
    "1": {
            "DATE": "2025-03-14", "TIME": "09:40:55", "IP": "203.0.113.5", "ACTION": "login",
            "STATUS": "success", "USER_AGENT": "Mozilla/5.0 (X11; Linux x86_64) Firefox/133.0",
            "SESSION": "6f1d2c3b4a59687776655443322110ff", "ACTIVE": "yes"
        }
}`))
	if err != nil {
		t.Fatal(err)
	}
	want := AuthLogEntry{ID: "1", DATE: "2025-03-14", TIME: "09:40:55", IP: "203.0.113.5", ACTION: "login",
		STATUS: "success", USER_AGENT: "Mozilla/5.0 (X11; Linux x86_64) Firefox/133.0"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if b, _ := json.Marshal(got[0]); strings.Contains(string(b), "6f1d2c3b") {
		t.Errorf("session id survived: %s", b)
	}
}

// The Plan 3 dev fixtures decode into these types, with the data the slices rely on.
func TestAccountDevFixtures(t *testing.T) {
	_, f := devClient(t)
	ctx := context.Background()
	out := func(cmd string, args ...string) []byte {
		t.Helper()
		res, err := f.Run(ctx, cmd, args...)
		if err != nil || res.Exit != 0 {
			t.Fatalf("%s %v: exit %d, %v", cmd, args, res.Exit, err)
		}
		return res.Stdout
	}
	exit := func(cmd string, args ...string) int {
		t.Helper()
		res, err := f.Run(ctx, cmd, args...)
		if err != nil {
			t.Fatalf("%s %v: %v", cmd, args, err)
		}
		return res.Exit
	}
	names := func(n int, err error) int {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	count := func(v any, err error) int { return names(reflect.ValueOf(v).Len(), err) }

	pkgs, err := DecodeObjects[PackageEntry](out("v-list-user-packages", "json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkgNames []string
	for _, p := range pkgs {
		pkgNames = append(pkgNames, p.NAME)
	}
	for _, want := range []string{"default", "system", "starter", "starter-plus"} {
		if !slices.Contains(pkgNames, want) {
			t.Errorf("packages %v lack %s", pkgNames, want)
		}
	}
	for _, p := range []string{"default", "system", "starter", "starter-plus"} {
		got, err := DecodeObjects[Package](out("v-list-user-package", p, "json"))
		if err != nil || len(got) != 1 || got[0].NAME != p {
			t.Errorf("package %s = %+v, %v", p, got, err)
		}
	}

	if n := count(DecodeObjects[CronJob](out("v-list-cron-jobs", "admin", "json"))); n != 2 {
		t.Errorf("admin cron jobs = %d, want 2", n)
	}
	if _, err := DecodeObjects[CronJob](out("v-list-cron-jobs", "bob", "json")); err == nil {
		t.Error("bob's cron JSON decoded; it must be Hestia's invalid JSON (a backslash in CMD)")
	}
	if plain := string(out("v-list-cron-jobs", "bob", "plain")); !strings.Contains(plain, `\`) || strings.Count(plain, "\t") < 9 {
		t.Errorf("bob's plain cron list = %q", plain)
	}
	if n := count(DecodeObjects[CronJob](out("v-list-cron-job", "admin", "1", "json"))); n != 1 {
		t.Errorf("admin cron job 1 = %d", n)
	}

	if n := count(DecodeObjects[Backup](out("v-list-user-backups", "admin", "json"))); n != 2 {
		t.Errorf("admin backups = %d, want 2", n)
	}
	if n := count(DecodeObjects[Backup](out("v-list-user-backups", "bob", "json"))); n != 1 {
		t.Errorf("bob backups = %d, want 1", n)
	}
	if _, err := DecodeBackupExclusions(out("v-list-user-backup-exclusions", "admin", "json")); err != nil {
		t.Error(err)
	}
	snaps, err := DecodeSnapshots(out("v-list-user-backups-restic", "admin", "json"))
	if err != nil || len(snaps) != 2 {
		t.Fatalf("admin snapshots = %v, %v", snaps, err)
	}
	if _, err := DecodeSnapshotDetail(out("v-list-user-backup-restic", "admin", snaps[0].ShortID, "json")); err != nil {
		t.Error(err)
	}
	if n := count(DecodeSnapshotNodes(out("v-list-user-files-restic", "admin", snaps[0].ShortID, "/home/admin"))); n < 2 {
		t.Errorf("snapshot nodes = %d", n)
	}

	// admin's list includes the File Manager key, which the API hides.
	if n := count(DecodeObjects[SSHKey](out("v-list-user-ssh-key", "admin", "json"))); n != 3 {
		t.Errorf("admin SSH keys = %d, want 3", n)
	}
	if n := count(DecodeObjects[SSHKey](out("v-list-user-ssh-key", "bob", "json"))); n != 1 {
		t.Errorf("bob SSH keys = %d, want 1", n)
	}
	if n := count(DecodeObjects[AccessKey](out("v-list-access-keys", "admin", "json"))); n != 3 {
		t.Errorf("admin access keys = %d, want 3", n)
	}
	if n := count(DecodeObjects[APIInfo](out("v-list-apis", "json"))); n == 0 {
		t.Error("no APIs")
	}
	if n := count(DecodeObjects[Notification](out("v-list-user-notifications", "admin", "json"))); n != 2 {
		t.Errorf("admin notifications = %d, want 2", n)
	}
	if n := count(DecodeObjects[LogEntry](out("v-list-user-log", "admin", "json"))); n != 5 {
		t.Errorf("admin log = %d, want 5", n)
	}
	if n := count(DecodeObjects[LogEntry](out("v-list-user-log", "system", "json"))); n == 0 {
		t.Error("empty system log")
	}
	if n := count(DecodeObjects[AuthLogEntry](out("v-list-user-auth-log", "admin", "json"))); n != 3 {
		t.Errorf("admin auth log = %d, want 3", n)
	}

	// Statistics: decoded by Slice D into its own type; here only the shape is checked.
	for _, args := range [][]string{{"v-list-user-stats", "admin", "json"}, {"v-list-user-stats", "bob", "json"}, {"v-list-users-stats", "json"}} {
		var months map[string]map[string]string
		if err := json.Unmarshal(out(args[0], args[1:]...), &months); err != nil || len(months) != 3 {
			t.Errorf("%v: %d months, %v", args, len(months), err)
		}
	}

	for _, c := range [][]string{
		{"v-list-user", "missing", "json"},
		{"v-list-user-package", "missing", "json"},
		{"v-list-user-backup", "admin", "missing", "json"},
	} {
		if e := exit(c[0], c[1:]...); e != 3 {
			t.Errorf("%v exit = %d, want 3", c, e)
		}
	}
	// An unknown snapshot exits 0 upstream with an empty "/backup" object (the recording).
	if e := exit("v-list-user-backup-restic", "admin", "missing", "json"); e != 0 {
		t.Errorf("restic missing exit = %d, want 0", e)
	}
	if _, err := DecodeSnapshotDetail(out("v-list-user-backup-restic", "admin", "missing", "json")); err == nil {
		t.Error("restic missing decoded as a snapshot")
	}
}

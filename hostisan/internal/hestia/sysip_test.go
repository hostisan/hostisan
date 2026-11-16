package hestia

import "testing"

// The example from the firewall parity checklist (`v-list-sys-ips json`).
const sysIPsExample = `{
    "10.0.0.5": {
        "OWNER": "alice",
        "STATUS": "dedicated",
        "NAME": "alice.example.net",
        "U_SYS_USERS": "alice",
        "U_WEB_DOMAINS": "3",
        "INTERFACE": "eth0",
        "NETMASK": "255.255.255.0",
        "NAT": "198.51.100.7",
        "TIME": "11:04:19",
        "DATE": "2026-09-30"
        },
    "203.0.113.10": {
        "OWNER": "admin",
        "STATUS": "shared",
        "NAME": "",
        "U_SYS_USERS": "admin,bob",
        "U_WEB_DOMAINS": "5",
        "INTERFACE": "eth0",
        "NETMASK": "255.255.255.0",
        "NAT": "",
        "TIME": "09:41:12",
        "DATE": "2026-08-17"
        }
}`

func TestSysIPDecode(t *testing.T) {
	got, err := DecodeObjects[SysIP]([]byte(sysIPsExample))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d IPs", len(got))
	}
	want := SysIP{IP: "10.0.0.5", OWNER: "alice", STATUS: "dedicated", NAME: "alice.example.net",
		U_SYS_USERS: "alice", U_WEB_DOMAINS: "3", INTERFACE: "eth0", NETMASK: "255.255.255.0",
		NAT: "198.51.100.7", TIME: "11:04:19", DATE: "2026-09-30"}
	if got[0] != want {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].IP != "203.0.113.10" || got[1].U_SYS_USERS != "admin,bob" {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[0].Display() != "198.51.100.7" {
		t.Errorf("Display with NAT = %q", got[0].Display())
	}
	if got[1].Display() != "203.0.113.10" {
		t.Errorf("Display without NAT = %q", got[1].Display())
	}
}

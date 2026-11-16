package hestia

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

const domainsJSON = `{
    "example.com": {
        "IP": "203.0.113.10",
        "TPL": "default",
        "TTL": "14400",
        "EXP": "2027-10-17",
        "SOA": "ns1.example.com",
        "SERIAL": "3000000007",
        "DNSSEC": "no",
        "SRC": "",
        "RECORDS": "21",
        "SUSPENDED": "no",
        "TIME": "09:41:12",
        "DATE": "2026-10-17"
    },
    "example.org": {
        "IP": "203.0.113.10",
        "TPL": "child-ns",
        "TTL": "3600",
        "EXP": "2027-03-01",
        "SOA": "ns1.example.org",
        "SERIAL": "3000000002",
        "DNSSEC": "yes",
        "SRC": "",
        "RECORDS": "5",
        "SUSPENDED": "yes",
        "TIME": "10:02:55",
        "DATE": "2026-10-18"
    }
}`

func TestDNSDomainFields(t *testing.T) {
	got, err := DecodeObjects[DNSDomain]([]byte(domainsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	want := DNSDomain{
		Name: "example.com", IP: "203.0.113.10", TPL: "default", TTL: "14400",
		EXP: "2027-10-17", SOA: "ns1.example.com", SERIAL: "3000000007",
		DNSSEC: "no", SRC: "", RECORDS: "21", SUSPENDED: "no",
		TIME: "09:41:12", DATE: "2026-10-17",
	}
	if got[0] != want {
		t.Errorf("got[0] = %+v, want %+v", got[0], want)
	}
	if got[1].Name != "example.org" || got[1].DNSSEC != "yes" || got[1].SUSPENDED != "yes" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestDNSRecordValueUnescaped(t *testing.T) {
	in := `{
    "9": {"RECORD": "@", "TYPE": "A", "PRIORITY": "", "VALUE": "203.0.113.10", "ID": "9",
          "SUSPENDED": "no", "TIME": "09:41:12", "DATE": "2026-10-17", "TTL": "14400"},
    "15": {"RECORD": "@", "TYPE": "TXT", "PRIORITY": "",
           "VALUE": "\"v=spf1 a mx ip4:203.0.113.10 -all\"", "ID": "15",
           "SUSPENDED": "yes", "TIME": "09:41:12", "DATE": "2026-10-17", "TTL": "300"}
}`
	got, err := DecodeObjects[DNSRecord]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != "9" || got[1].ID != "15" {
		t.Fatalf("IDs = %q, %q", got[0].ID, got[1].ID)
	}
	if want := `"v=spf1 a mx ip4:203.0.113.10 -all"`; got[1].VALUE != want {
		t.Errorf("VALUE = %q, want %q", got[1].VALUE, want)
	}
}

func TestUserIPDisplay(t *testing.T) {
	in := `{"203.0.113.10":{"OWNER":"admin","STATUS":"shared","NAME":"main","NAT":""},
	        "10.0.0.5":{"OWNER":"alice","STATUS":"dedicated","NAME":"","NAT":"198.51.100.7"}}`
	got, err := DecodeObjects[UserIP]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].IP != "203.0.113.10" || got[0].Display() != "203.0.113.10" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].IP != "10.0.0.5" || got[1].Display() != "198.51.100.7" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

// devFixture returns the stdout of the first dev-fixture entry for cmd with these args.
func devFixture(t *testing.T, cmd string, args ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "dev", cmd+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Args   []string `json:"args"`
		Stdout string   `json:"stdout"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if slices.Equal(e.Args, args) {
			return []byte(e.Stdout)
		}
	}
	t.Fatalf("no %s fixture for %v", cmd, args)
	return nil
}

// The dev data must decode with the types and agree with itself.
func TestDevDNSFixturesDecode(t *testing.T) {
	zones, err := DecodeObjects[DNSDomain](devFixture(t, "v-list-dns-domains", "admin", "json"))
	if err != nil || len(zones) != 2 || zones[0].Name != "example.com" || zones[1].Name != "example.org" {
		t.Fatalf("zones = %+v, %v", zones, err)
	}
	for _, z := range zones {
		recs, err := DecodeObjects[DNSRecord](devFixture(t, "v-list-dns-records", "admin", z.Name, "json"))
		if err != nil || strconv.Itoa(len(recs)) != z.RECORDS {
			t.Errorf("%s: %d records, RECORDS=%s, %v", z.Name, len(recs), z.RECORDS, err)
		}
	}
	if zones[1].DNSSEC != "yes" || zones[1].SUSPENDED != "yes" || zones[0].DNSSEC != "no" {
		t.Errorf("example.com/example.org flags: %+v", zones)
	}
}

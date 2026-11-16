package hestia

import (
	"slices"
	"strings"
	"testing"
)

// The JSON below is checklist section 6.2, verbatim.

const mailDomainsJSON = `{
    "example.com": {
        "ANTIVIRUS": "yes",
        "ANTISPAM": "yes",
        "REJECT": "yes",
        "RATE_LIMIT": "",
        "DKIM": "yes",
        "CATCHALL": "",
        "ACCOUNTS": "2",
        "U_DISK": "37",
        "SSL": "yes",
        "SUSPENDED": "no",
        "TIME": "09:41:07",
        "DATE": "2026-03-14",
        "WEBMAIL_ALIAS": "webmail",
        "WEBMAIL": "roundcube"
    },
    "suspended-example.org": {
        "ANTIVIRUS": "no",
        "ANTISPAM": "no",
        "REJECT": "",
        "RATE_LIMIT": "100",
        "DKIM": "no",
        "CATCHALL": "boss@suspended-example.org",
        "ACCOUNTS": "0",
        "U_DISK": "0",
        "SSL": "no",
        "SUSPENDED": "yes",
        "TIME": "11:02:55",
        "DATE": "2026-04-01",
        "WEBMAIL_ALIAS": "webmail",
        "WEBMAIL": ""
    }
}`

func TestMailDomainFields(t *testing.T) {
	got, err := DecodeObjects[MailDomain]([]byte(mailDomainsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	want := MailDomain{
		NAME: "example.com", ANTIVIRUS: "yes", ANTISPAM: "yes", REJECT: "yes", RATE_LIMIT: "",
		DKIM: "yes", CATCHALL: "", ACCOUNTS: "2", U_DISK: "37", SSL: "yes", SUSPENDED: "no",
		TIME: "09:41:07", DATE: "2026-03-14", WEBMAIL_ALIAS: "webmail", WEBMAIL: "roundcube",
	}
	if got[0] != want {
		t.Errorf("got[0] = %+v\nwant     %+v", got[0], want)
	}
	o := got[1]
	if o.NAME != "suspended-example.org" || o.REJECT != "" || o.RATE_LIMIT != "100" ||
		o.CATCHALL != "boss@suspended-example.org" || o.SUSPENDED != "yes" || o.WEBMAIL != "" {
		t.Errorf("got[1] = %+v", o)
	}
}

func TestMailDomainDetailFields(t *testing.T) {
	in := `{
    "example.com": {
        "ANTIVIRUS": "yes",
        "ANTISPAM": "yes",
        "DKIM": "yes",
        "DKIM_SELECTOR": "mail",
        "CATCHALL": "",
        "ACCOUNTS": "2",
        "RATE_LIMIT": "",
        "REJECT": "yes",
        "U_DISK": "37",
        "SSL": "yes",
        "SSL_KEY_ALGO": "ecdsa-256",
        "LETSENCRYPT": "yes",
        "SUSPENDED": "no",
        "TIME": "09:41:07",
        "DATE": "2026-03-14",
        "WEBMAIL_ALIAS": "webmail.example.com",
        "WEBMAIL": "roundcube",
        "U_SMTP_RELAY": "false",
        "U_SMTP_RELAY_HOST": "",
        "U_SMTP_RELAY_PORT": "",
        "U_SMTP_RELAY_USERNAME": ""
    }
}`
	got, err := DecodeObjects[MailDomainDetail]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := MailDomainDetail{
		NAME: "example.com", ANTIVIRUS: "yes", ANTISPAM: "yes", DKIM: "yes", DKIM_SELECTOR: "mail",
		CATCHALL: "", ACCOUNTS: "2", RATE_LIMIT: "", REJECT: "yes", U_DISK: "37", SSL: "yes",
		SSL_KEY_ALGO: "ecdsa-256", LETSENCRYPT: "yes", SUSPENDED: "no", TIME: "09:41:07",
		DATE: "2026-03-14", WEBMAIL_ALIAS: "webmail.example.com", WEBMAIL: "roundcube",
		U_SMTP_RELAY: "false", U_SMTP_RELAY_HOST: "", U_SMTP_RELAY_PORT: "", U_SMTP_RELAY_USERNAME: "",
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestMailAccountFields(t *testing.T) {
	in := `{
    "info": {
        "ALIAS": "contact,hello",
        "FWD": "",
        "FWD_ONLY": "",
        "AUTOREPLY": "yes",
        "QUOTA": "1024",
        "U_DISK": "35",
        "SUSPENDED": "no",
        "TIME": "09:45:10",
        "DATE": "2026-03-14"
    },
    "sales": {
        "ALIAS": "",
        "FWD": "owner@gmail.com,archive@example.org",
        "FWD_ONLY": "yes",
        "AUTOREPLY": "no",
        "QUOTA": "unlimited",
        "U_DISK": "2",
        "SUSPENDED": "no",
        "TIME": "10:12:33",
        "DATE": "2026-03-20"
    }
}`
	got, err := DecodeObjects[MailAccount]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []MailAccount{
		{NAME: "info", ALIAS: "contact,hello", FWD: "", FWD_ONLY: "", AUTOREPLY: "yes", QUOTA: "1024",
			U_DISK: "35", SUSPENDED: "no", TIME: "09:45:10", DATE: "2026-03-14"},
		{NAME: "sales", ALIAS: "", FWD: "owner@gmail.com,archive@example.org", FWD_ONLY: "yes",
			AUTOREPLY: "no", QUOTA: "unlimited", U_DISK: "2", SUSPENDED: "no", TIME: "10:12:33", DATE: "2026-03-20"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestMailAccountDetailFields(t *testing.T) {
	in := `{
    "info": {
        "ALIAS": "contact,hello",
        "FWD": "",
        "FWD_ONLY": "",
        "AUTOREPLY": "yes",
        "QUOTA": "1024",
        "RATE_LIMIT": "",
        "U_DISK": "35",
        "SUSPENDED": "no",
        "TIME": "09:45:10",
        "DATE": "2026-03-14",
        "WEBMAIL_ALIAS": "webmail.example.com"
    }
}`
	got, err := DecodeObjects[MailAccountDetail]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := MailAccountDetail{
		NAME: "info", ALIAS: "contact,hello", FWD: "", FWD_ONLY: "", AUTOREPLY: "yes", QUOTA: "1024",
		RATE_LIMIT: "", U_DISK: "35", SUSPENDED: "no", TIME: "09:45:10", DATE: "2026-03-14",
		WEBMAIL_ALIAS: "webmail.example.com",
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestMailAutoreplyFields(t *testing.T) {
	in := "{\n\t\"info\": {\n            \"MSG\": \"I am away until Monday.\\nBest regards,\\nInfo team\"\n\t}\n}"
	got, err := DecodeObjects[MailAutoreply]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].NAME != "info" || got[0].MSG != "I am away until Monday.\nBest regards,\nInfo team" {
		t.Errorf("got %+v", got)
	}
}

func TestMailSSLFields(t *testing.T) {
	in := `{
	"example.com": {
        "CRT": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
        "KEY": "-----BEGIN PRIVATE KEY-----\nMIGH...\n-----END PRIVATE KEY-----",
        "CA": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
        "SUBJECT": "mail.example.com",
        "ALIASES": "mail.example.com,webmail.example.com",
        "NOT_BEFORE": "Mar 14 09:41:07 2026 GMT",
        "NOT_AFTER": "Jun 12 09:41:06 2026 GMT",
        "SIGNATURE": "ecdsa-with-SHA384",
        "PUB_KEY": "256 bit",
        "ISSUER": "C=US, O=Let's Encrypt, CN=E6"
	}
}`
	got, err := DecodeObjects[MailSSL]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := MailSSL{
		CRT:        "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		KEY:        "-----BEGIN PRIVATE KEY-----\nMIGH...\n-----END PRIVATE KEY-----",
		CA:         "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
		SUBJECT:    "mail.example.com",
		ALIASES:    "mail.example.com,webmail.example.com",
		NOT_BEFORE: "Mar 14 09:41:07 2026 GMT",
		NOT_AFTER:  "Jun 12 09:41:06 2026 GMT",
		SIGNATURE:  "ecdsa-with-SHA384",
		PUB_KEY:    "256 bit",
		ISSUER:     "C=US, O=Let's Encrypt, CN=E6",
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestDKIMRecordFields(t *testing.T) {
	in := "{\n\n\t\"mail._domainkey\": {\n            \"TTL\": \"3600\",\n            \"TXT\": \"'v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...IDAQAB'\"\n\t}\n}"
	got, err := DecodeObjects[DKIMRecord]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := DKIMRecord{NAME: "mail._domainkey", TTL: "3600", TXT: "'v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...IDAQAB'"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v", got)
	}
}

func TestWebmailClients(t *testing.T) {
	for in, want := range map[string][]string{
		"[\n\t\"roundcube\",\n\t\"snappymail\"\n]": {"roundcube", "snappymail"},
		`[""]`:                {},
		`[]`:                  {},
		`[" roundcube ", ""]`: {"roundcube"},
	} {
		got, err := WebmailClients([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got == nil || !slices.Equal(got, want) {
			t.Errorf("WebmailClients(%s) = %#v, want %#v", in, got, want)
		}
	}
	if _, err := WebmailClients([]byte("nope")); err == nil {
		t.Error("bad JSON accepted")
	}
}

func TestValidMailAccountArg(t *testing.T) {
	for s, want := range map[string]bool{
		"info":                  true,
		"a.b-c_d":               true,
		"a":                     true,
		"0":                     true,
		strings.Repeat("a", 64): true,
		"-h":                    false,
		"":                      false,
		"a b":                   false,
		"x;y":                   false,
		strings.Repeat("a", 65): false,
		"é":                     false,
		"café":                  false,
		".":                     false,
		"_":                     false,
		"a/b":                   false,
		"a\nb":                  false,
		"a@b":                   false,
	} {
		if got := ValidMailAccountArg(s); got != want {
			t.Errorf("ValidMailAccountArg(%q) = %v, want %v", s, got, want)
		}
	}
}

// The dev fixtures carry the brief's data and decode with these types.
func TestMailDevFixtures(t *testing.T) {
	doms, err := DecodeObjects[MailDomain](devFixture(t, "v-list-mail-domains", "admin", "json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(doms) != 2 || doms[0].NAME != "example.com" || doms[1].NAME != "example.net" || doms[1].SUSPENDED != "yes" {
		t.Fatalf("admin domains = %+v", doms)
	}
	if d := doms[0]; d.ANTISPAM != "yes" || d.ANTIVIRUS != "yes" || d.DKIM != "yes" || d.SSL != "yes" || d.WEBMAIL != "roundcube" {
		t.Errorf("example.com = %+v", d)
	}
	bob, err := DecodeObjects[MailDomain](devFixture(t, "v-list-mail-domains", "bob", "json"))
	if err != nil || len(bob) != 1 || bob[0].NAME != "bakery.example" {
		t.Fatalf("bob domains = %+v, %v", bob, err)
	}
	accts, err := DecodeObjects[MailAccount](devFixture(t, "v-list-mail-accounts", "admin", "example.com", "json"))
	if err != nil || len(accts) != 2 || accts[0].NAME != "info" || accts[1].NAME != "sales" {
		t.Fatalf("accounts = %+v, %v", accts, err)
	}
	if s := accts[1]; s.FWD == "" || s.AUTOREPLY != "yes" {
		t.Errorf("sales = %+v", s)
	}
	for _, c := range []struct {
		cmd  string
		args []string
	}{
		{"v-list-mail-domain", []string{"admin", "example.com", "json"}},
		{"v-list-mail-domain", []string{"admin", "example.net", "json"}},
		{"v-list-mail-domain", []string{"bob", "bakery.example", "json"}},
	} {
		got, err := DecodeObjects[MailDomainDetail](devFixture(t, c.cmd, c.args...))
		if err != nil || len(got) != 1 || got[0].NAME != c.args[1] {
			t.Errorf("%s %v = %+v, %v", c.cmd, c.args, got, err)
		}
	}
	for _, a := range []string{"info", "sales"} {
		got, err := DecodeObjects[MailAccountDetail](devFixture(t, "v-list-mail-account", "admin", "example.com", a, "json"))
		if err != nil || len(got) != 1 || got[0].NAME != a {
			t.Errorf("account %s = %+v, %v", a, got, err)
		}
	}
	ar, err := DecodeObjects[MailAutoreply](devFixture(t, "v-list-mail-account-autoreply", "admin", "example.com", "sales", "json"))
	if err != nil || len(ar) != 1 || ar[0].MSG == "" {
		t.Errorf("autoreply = %+v, %v", ar, err)
	}
	ssl, err := DecodeObjects[MailSSL](devFixture(t, "v-list-mail-domain-ssl", "admin", "example.com", "json"))
	if err != nil || len(ssl) != 1 || ssl[0].CRT == "" || ssl[0].SUBJECT != "mail.example.com" {
		t.Errorf("ssl = %+v, %v", ssl, err)
	}
	dkim, err := DecodeObjects[DKIMRecord](devFixture(t, "v-list-mail-domain-dkim-dns", "admin", "example.com", "json"))
	if err != nil || len(dkim) != 1 || dkim[0].NAME != "mail._domainkey" {
		t.Errorf("dkim = %+v, %v", dkim, err)
	}
	clients, err := WebmailClients(devFixture(t, "v-list-sys-webmail", "json"))
	if err != nil || !slices.Equal(clients, []string{"roundcube", "snappymail"}) {
		t.Errorf("webmail = %v, %v", clients, err)
	}
}

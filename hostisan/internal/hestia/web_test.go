package hestia

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The JSON below is web checklist section 6, verbatim (the list example has the
// list's own key set).

const webDomainsJSON = `{
    "example.com": {
        "IP": "10.0.0.5",
        "IP6": "",
        "DOCUMENT_ROOT": "/home/alice/web/example.com/public_html/",
        "U_DISK": "128",
        "U_BANDWIDTH": "45",
        "TPL": "default",
        "ALIAS": "www.example.com,blog.example.com",
        "STATS": "awstats",
        "STATS_USER": "",
        "SSL": "yes",
        "SSL_HOME": "same",
        "LETSENCRYPT": "yes",
        "FTP_USER": "alice_dev",
        "FTP_PATH": "/public_html",
        "AUTH_USER": "",
        "BACKEND": "PHP-8_2",
        "PROXY": "default",
        "PROXY_EXT": "css,htm,html,js,mjs,json,xml,apng,avif,bmp,cur,gif,ico,jfif,jpg,jpeg,png,svg,webp",
        "SUSPENDED": "no",
        "TIME": "09:41:07",
        "DATE": "2026-03-02"
    }
}`

const webDomainJSON = `{
    "example.com": {
        "IP": "10.0.0.5",
        "IP6": "",
        "DOCUMENT_ROOT": "/home/alice/web/example.com/public_html/",
        "U_DISK": "128",
        "U_BANDWIDTH": "45",
        "TPL": "default",
        "ALIAS": "www.example.com,blog.example.com",
        "STATS": "awstats",
        "STATS_USER": "",
        "SSL": "yes",
        "SSL_FORCE": "yes",
        "SSL_HSTS": "no",
        "SSL_HOME": "same",
        "SSL_KEY_ALGO": "ecdsa-256",
        "LETSENCRYPT_FAIL_COUNT": "0",
        "LETSENCRYPT": "yes",
        "FTP_USER": "alice_dev",
        "FTP_PATH": "/public_html",
        "AUTH_USER": "",
        "BACKEND": "PHP-8_2",
        "PROXY": "default",
        "PROXY_EXT": "css,htm,html,js,mjs,json,xml,gif,jpg,png,svg,webp",
        "FASTCGI_CACHE": "yes",
        "FASTCGI_DURATION": "2m",
        "REDIRECT": "",
        "REDIRECT_CODE": "",
        "CUSTOM_DOCROOT": "",
        "SUSPENDED": "no",
        "TIME": "09:41:07",
        "DATE": "2026-03-02"
    }
}`

const webSSLJSON = `{
	"example.com": {
        "CRT": "-----BEGIN CERTIFICATE-----\nMIIF...\n-----END CERTIFICATE-----",
        "KEY": "-----BEGIN PRIVATE KEY-----\nMIGH...\n-----END PRIVATE KEY-----",
        "CA": "-----BEGIN CERTIFICATE-----\nMIIE...\n-----END CERTIFICATE-----",
        "SUBJECT": "example.com",
        "ALIASES": "example.com,blog.example.com,www.example.com",
        "NOT_BEFORE": "Mar  2 09:41:10 2026 GMT",
        "NOT_AFTER": "May 31 09:41:09 2026 GMT",
        "SIGNATURE": "ecdsa-with-SHA384",
        "PUB_KEY": "256 bit",
        "ISSUER": "C = US, O = Let's Encrypt, CN = E7"
	}
}`

// innerKeys is the key order of the first object inside stdout's top-level object.
func innerKeys(t *testing.T, stdout string) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader([]byte(stdout)))
	for i := 0; i < 3; i++ { // {, "<name>", {
		if _, err := dec.Token(); err != nil {
			t.Fatal(err)
		}
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		if _, err := dec.Token(); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

// fieldTags is v's JSON tags in declaration order, without NAME.
func fieldTags(v any) []string {
	var out []string
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		if tag := rt.Field(i).Tag.Get("json"); tag != "NAME" {
			out = append(out, tag)
		}
	}
	return out
}

func TestWebTypesFollowHestiaKeyOrder(t *testing.T) {
	for _, c := range []struct {
		name string
		v    any
		in   string
	}{
		{"WebDomain", WebDomain{}, webDomainsJSON},
		{"WebDomainDetail", WebDomainDetail{}, webDomainJSON},
		{"WebSSL", WebSSL{}, webSSLJSON},
	} {
		if got, want := fieldTags(c.v), innerKeys(t, c.in); !slices.Equal(got, want) {
			t.Errorf("%s fields = %v\nwant (Hestia order) %v", c.name, got, want)
		}
	}
}

func TestWebDomainFields(t *testing.T) {
	got, err := DecodeObjects[WebDomain]([]byte(webDomainsJSON))
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d, %v", len(got), err)
	}
	want := WebDomain{
		NAME: "example.com", IP: "10.0.0.5", IP6: "", DOCUMENT_ROOT: "/home/alice/web/example.com/public_html/",
		U_DISK: "128", U_BANDWIDTH: "45", TPL: "default", ALIAS: "www.example.com,blog.example.com",
		STATS: "awstats", STATS_USER: "", SSL: "yes", SSL_HOME: "same", LETSENCRYPT: "yes",
		FTP_USER: "alice_dev", FTP_PATH: "/public_html", AUTH_USER: "", BACKEND: "PHP-8_2", PROXY: "default",
		PROXY_EXT: "css,htm,html,js,mjs,json,xml,apng,avif,bmp,cur,gif,ico,jfif,jpg,jpeg,png,svg,webp",
		SUSPENDED: "no", TIME: "09:41:07", DATE: "2026-03-02",
	}
	if got[0] != want {
		t.Errorf("got  %+v\nwant %+v", got[0], want)
	}
}

func TestWebDomainDetailFields(t *testing.T) {
	got, err := DecodeObjects[WebDomainDetail]([]byte(webDomainJSON))
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d, %v", len(got), err)
	}
	want := WebDomainDetail{
		NAME: "example.com", IP: "10.0.0.5", IP6: "", DOCUMENT_ROOT: "/home/alice/web/example.com/public_html/",
		U_DISK: "128", U_BANDWIDTH: "45", TPL: "default", ALIAS: "www.example.com,blog.example.com",
		STATS: "awstats", STATS_USER: "", SSL: "yes", SSL_FORCE: "yes", SSL_HSTS: "no", SSL_HOME: "same",
		SSL_KEY_ALGO: "ecdsa-256", LETSENCRYPT_FAIL_COUNT: "0", LETSENCRYPT: "yes",
		FTP_USER: "alice_dev", FTP_PATH: "/public_html", AUTH_USER: "", BACKEND: "PHP-8_2", PROXY: "default",
		PROXY_EXT: "css,htm,html,js,mjs,json,xml,gif,jpg,png,svg,webp", FASTCGI_CACHE: "yes",
		FASTCGI_DURATION: "2m", REDIRECT: "", REDIRECT_CODE: "", CUSTOM_DOCROOT: "",
		SUSPENDED: "no", TIME: "09:41:07", DATE: "2026-03-02",
	}
	if got[0] != want {
		t.Errorf("got  %+v\nwant %+v", got[0], want)
	}
}

func TestWebSSLFields(t *testing.T) {
	got, err := DecodeObjects[WebSSL]([]byte(webSSLJSON))
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d, %v", len(got), err)
	}
	c := got[0]
	if !strings.HasPrefix(c.CRT, "-----BEGIN CERTIFICATE-----\nMIIF") || !strings.Contains(c.KEY, "PRIVATE KEY") ||
		!strings.HasPrefix(c.CA, "-----BEGIN CERTIFICATE-----\nMIIE") || c.SUBJECT != "example.com" ||
		c.ALIASES != "example.com,blog.example.com,www.example.com" || c.NOT_BEFORE != "Mar  2 09:41:10 2026 GMT" ||
		c.NOT_AFTER != "May 31 09:41:09 2026 GMT" || c.SIGNATURE != "ecdsa-with-SHA384" || c.PUB_KEY != "256 bit" ||
		c.ISSUER != "C = US, O = Let's Encrypt, CN = E7" {
		t.Errorf("got %+v", c)
	}
}

func TestValidFTPUserArg(t *testing.T) {
	for v, want := range map[string]bool{
		"dev": true, "a.b-c_d": true, "A9": true, strings.Repeat("a", 30): true,
		"-h": false, "": false, "a b": false, "x;y": false, strings.Repeat("a", 31): false, "é": false,
		"a\nb": false, "a/b": false,
	} {
		if got := ValidFTPUserArg(v); got != want {
			t.Errorf("ValidFTPUserArg(%q) = %v, want %v", v, got, want)
		}
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/hostisan/hostisan/internal/hestia"
)

func TestLoginOps(t *testing.T) {
	h := newHarness(t)
	h.loginOps()
	b := h.bootstrap()
	if b.User == nil || b.User.Name != "ops" || b.User.Role != "admin" {
		t.Fatalf("bootstrap user = %+v", b.User)
	}
}

func TestSharedFixturesDecode(t *testing.T) {
	h := newHarness(t)
	run := func(cmd string, args ...string) []byte {
		t.Helper()
		res, err := h.fixture.Run(context.Background(), cmd, args...)
		if err != nil || res.Exit != 0 {
			t.Fatalf("%s %v: %+v, %v", cmd, args, res, err)
		}
		return res.Stdout
	}

	ips, err := hestia.DecodeObjects[hestia.SysIP](run("v-list-sys-ips", "json"))
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, ip := range ips {
		order = append(order, ip.IP)
	}
	if !slices.Equal(order, []string{"10.0.0.5", "203.0.113.10", "203.0.113.11"}) {
		t.Fatalf("IPs = %v", order)
	}
	if ips[0].OWNER != "bob" || ips[0].Display() != "198.51.100.7" {
		t.Errorf("10.0.0.5 = %+v", ips[0])
	}
	if ips[1].Display() != "203.0.113.10" || ips[1].U_WEB_DOMAINS != "2" {
		t.Errorf("203.0.113.10 = %+v", ips[1])
	}

	for cmd, want := range map[string][]string{
		"v-list-sys-php":               {"8.2", "8.3"},
		"v-list-web-templates":         {"default", "hosting-php", "wordpress"},
		"v-list-web-templates-backend": {"default", "PHP-8_2", "PHP-8_3"},
		"v-list-web-templates-proxy":   {"caching", "default", "hosting"},
		"v-list-sys-themes":            {"dark", "default", "flat", "vestia"},
		"v-list-sys-users":             {"admin", "bob", "susp", "ops"},
	} {
		var got []string
		if err := json.Unmarshal(run(cmd, "json"), &got); err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", cmd, got, want)
		}
	}
}

package totp

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1, secret "12345678901234567890", 8 digits; the last six
// digits are the 6-digit codes.
func TestRFC6238Vectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, v := range []struct {
		unix int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		got, err := Code(secret, time.Unix(v.unix, 0))
		if err != nil || got != v.code {
			t.Errorf("t=%d: %q, %v; want %s", v.unix, got, err, v.code)
		}
	}
}

func TestVerifyWindow(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	now := time.Unix(1111111109, 0)
	prev, _ := Code(secret, now.Add(-30*time.Second))
	next, _ := Code(secret, now.Add(30*time.Second))
	far, _ := Code(secret, now.Add(90*time.Second))
	for name, c := range map[string]string{"now": "081804", "prev": prev, "next": next} {
		if !Verify(secret, c, now) {
			t.Errorf("%s rejected", name)
		}
	}
	if Verify(secret, far, now) || Verify(secret, "", now) || Verify(secret, "08180", now) || Verify(secret, "abcdef", now) {
		t.Error("bad code accepted")
	}
	if Verify("!!", "081804", now) {
		t.Error("bad secret accepted")
	}
}

func TestGenerateSecret(t *testing.T) {
	a, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateSecret()
	if len(a) != 32 || a == b || strings.Trim(a, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") != "" {
		t.Fatalf("%q %q", a, b)
	}
}

func TestURL(t *testing.T) {
	got := URL("Hostisan Panel", "bob", "JBSWY3DPEHPK3PXP")
	want := "otpauth://totp/Hostisan%20Panel:bob?issuer=Hostisan+Panel&secret=JBSWY3DPEHPK3PXP"
	if got != want {
		t.Fatalf("%s", got)
	}
}

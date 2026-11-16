package hestia

import (
	"strings"
	"testing"
)

// hostile values every account path parameter refuses. The SSH key ID is the
// exception for spaces and punctuation (a key comment may hold them), so its table
// lists its own.
var hostileArgs = []string{"-h", "a b", "x;y", "../x", "", strings.Repeat("a", 300), "é", "a\nb", "a/b", "a|b"}

func TestAccountPathValidators(t *testing.T) {
	for _, c := range []struct {
		name    string
		valid   func(string) bool
		good    []string
		hostile []string
	}{
		{"ValidPackageArg", ValidPackageArg,
			[]string{"default", "system", "starter-plus", "a.b_c-1", "ab", strings.Repeat("a", 66)},
			append(hostileArgs, "a", "-a", "a-", ".ab", strings.Repeat("a", 67))},
		{"ValidBackupName", ValidBackupName,
			[]string{"admin.2026-10-19_03-00-02.tar", "bob.2026-01-01_00-00-00.tar.zst", "ab"},
			append(hostileArgs, "admin.x.tar/../../y", "a", "x.tar.", strings.Repeat("a", 67))},
		{"ValidSnapshotID", ValidSnapshotID,
			[]string{"3c9b8a7e", "e41f0a29bc77", strings.Repeat("a", 64)},
			append(hostileArgs, "3c9b8a7", "3C9B8A7E", "3c9b8a7g", strings.Repeat("a", 65))},
		{"ValidAccessKeyID", ValidAccessKeyID,
			[]string{"aB3dEfGh1JkLmN0pQrSt", "Zx9yWv8uTs7rQp6oNm5l"},
			append(hostileArgs, "aB3dEfGh1JkLmN0pQrS", "aB3dEfGh1JkLmN0pQrStu", "aB3dEfGh1JkLmN0pQr-t")},
		{"ValidSSHKeyID", ValidSSHKeyID,
			[]string{"alice@laptop", "deploy key 2024", "no comment", "x;y", "a/b", "é", "0123456789abcdef"},
			[]string{"-h", "", strings.Repeat("a", 256), "a\nb", "a\tb", "a\x00b", "a\x7fb", "a\u0085b"}},
	} {
		for _, s := range c.good {
			if !c.valid(s) {
				t.Errorf("%s(%q) = false, want true", c.name, s)
			}
		}
		for _, s := range c.hostile {
			if c.valid(s) {
				t.Errorf("%s(%q) = true, want false", c.name, s)
			}
		}
	}
	if !ValidSSHKeyID(strings.Repeat("a", 255)) {
		t.Error("ValidSSHKeyID refuses 255 bytes")
	}
}

// The existing validators the slices use for {user} and {job} refuse the same values.
func TestAccountReusedValidators(t *testing.T) {
	for _, s := range hostileArgs {
		if ValidUsername(s) {
			t.Errorf("ValidUsername(%q) = true", s)
		}
		if ValidRecordID(s) {
			t.Errorf("ValidRecordID(%q) = true", s)
		}
	}
}

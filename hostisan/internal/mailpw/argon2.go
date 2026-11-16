package mailpw

import (
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Limits on a stored Argon2 hash's parameters. The hash comes from Dovecot, but a
// corrupt or planted one must not make one request allocate gigabytes or spin.
const (
	argon2MaxMemory  = 1 << 20 // KiB (1 GiB); libsodium's "sensitive" is 1 GiB
	argon2MaxTime    = 64
	argon2MaxThreads = 255
	argon2MinKey     = 4
	argon2MaxKey     = 1024
)

// argon2Verify checks password against libsodium's encoded form, which Dovecot stores
// after {ARGON2ID}/{ARGON2I}: $argon2id$v=19$m=<KiB>,t=<passes>,p=<lanes>$<salt>$<hash>,
// salt and hash in unpadded standard base64. Only version 19 (0x13) is accepted.
func argon2Verify(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=…,t=…,p=…", salt, hash
	if len(parts) != 6 || parts[0] != "" || parts[2] != "v=19" {
		return false
	}
	var id bool
	switch parts[1] {
	case "argon2id":
		id = true
	case "argon2i":
	default:
		return false
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return false
	}
	m, okM := argon2Param(params[0], "m=", 8, argon2MaxMemory)
	t, okT := argon2Param(params[1], "t=", 1, argon2MaxTime)
	p, okP := argon2Param(params[2], "p=", 1, argon2MaxThreads)
	if !okM || !okT || !okP || m < 8*p {
		return false
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return false
	}
	want, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(want) < argon2MinKey || len(want) > argon2MaxKey {
		return false
	}
	var got []byte
	if id {
		got = argon2.IDKey([]byte(password), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	} else {
		got = argon2.Key([]byte(password), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// argon2Param reads "<prefix><decimal>" within [lo, hi].
func argon2Param(s, prefix string, lo, hi int) (int, bool) {
	v, ok := strings.CutPrefix(s, prefix)
	if !ok || v == "" || v[0] == '+' || v[0] == '-' || (len(v) > 1 && v[0] == '0') {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil && n >= lo && n <= hi
}

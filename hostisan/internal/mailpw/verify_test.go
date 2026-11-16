package mailpw

import (
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Vectors. Every one was checked against a second implementation before it was pasted:
//   - SHA-256/SHA-512 crypt: Drepper's "Unix crypt using SHA-256 and SHA-512" examples
//     (key "Hello world!", salt "saltstring"), cross-checked with
//     `openssl passwd -5 -salt saltstring 'Hello world!'` and `openssl passwd -6 …`;
//     the rounds=10000 forms with glibc/libxcrypt
//     `perl -e 'print crypt("Hello world!", q($6$rounds=10000$saltstringsaltstring))'`.
//   - MD5-crypt: `openssl passwd -1 -salt devsalt1 'Hello world!'`, `openssl passwd -1 -salt abc ”`.
//   - bcrypt: htpasswd isn't installed here, so libxcrypt's crypt(3):
//     `perl -e 'print crypt("Hello world!", q($2y$10$abcdefghijklmnopqrstuu))'` (and $2b$04$),
//     plus the crypt_blowfish test vector ("U*U", $2a$05$CCCC…).
//   - The dev fixture's mail hash: `openssl passwd -6 -salt devsalt devpass`.
var verifyVectors = []struct {
	name, password, hash string
}{
	{"sha512", "Hello world!", "$6$saltstring$svn8UoSVapNtMuq1ukKS4tPQd8iKwSMHWjl/O817G3uBnIFNjnQJuesI68u4OTLiBFdcbYEdFCoEOfaS35inz1"},
	{"sha512 rounds", "Hello world!", "$6$rounds=10000$saltstringsaltst$OW1/O6BYHV6BcXZu8QVeXbDWra3Oeqh0sbHbbMCVNSnCM/UrjmM0Dp8vOuZeHBy/YTBmSK6H9qs/y3RnOaw5v."},
	{"sha512 explicit default rounds", "Hello world!", "$6$rounds=5000$saltstring$svn8UoSVapNtMuq1ukKS4tPQd8iKwSMHWjl/O817G3uBnIFNjnQJuesI68u4OTLiBFdcbYEdFCoEOfaS35inz1"},
	{"sha512 long salt cut to 16", "Hello world!", "$6$rounds=1000$toolongsaltstrin$sesQxVr.eO8J/1tYcwFWM0XaMVaHFLetz9ssK1oNSPUQgLRm8v3S4i6CgxB9aqGOAFFEMnB2V1gvxEm/Gv2gw/"},
	{"sha256", "Hello world!", "$5$saltstring$5B8vYYiY.CVt1RlTTf8KbXBH3hsxY/GNooZaBBGWEc5"},
	{"sha256 rounds", "Hello world!", "$5$rounds=10000$saltstringsaltst$3xv.VbSHBb41AL9AvLeujZkZRBAwqFMz2.opqey6IcA"},
	{"dev fixture", "devpass", "$6$devsalt$6h3F4aYWmAGJQrxirhIyXRggDQbnryF92SIM6pa8nAJjPCllXvq8NolAzgLrE9H/aDkTXdb4DbgkBwGKCA.sD/"},
	{"md5", "Hello world!", "$1$devsalt1$LyhAMrUxk/jc6aj07ECJk/"},
	{"md5 empty password", "", "$1$abc$Or2rbeUYTvt12aiVzMuS/."},
	{"bcrypt 2y", "Hello world!", "$2y$10$abcdefghijklmnopqrstuu0uIewG7hXDJNtk3ib048WCtX.0JsFiS"},
	{"bcrypt 2b", "Hello world!", "$2b$04$abcdefghijklmnopqrstuuyeG8laUfZvsCmc.AE6qIDYSPGM2efmK"},
	{"bcrypt 2a", "U*U", "$2a$05$CCCCCCCCCCCCCCCCCCCCC.E5YPO9kmyuRGyh0XouQYb4YMJKvyOeW"},
	// Argon2 (Dovecot's ARGON2I/ARGON2ID, libsodium's encoded form): the examples in the
	// reference implementation's README (phc-winner-argon2, `echo -n password | ./argon2
	// somesalt -t 2 -m 16 -p 4 -l 24` and `… -id -l 32`), which x/crypto/argon2 reproduces.
	{"argon2i", "password", "$argon2i$v=19$m=65536,t=2,p=4$c29tZXNhbHQ$RdescudvJCsgt3ub+b+dWRWJTmaaJObG"},
	{"argon2id", "password", "$argon2id$v=19$m=65536,t=2,p=4$c29tZXNhbHQ$GpZ3sK/oH9p7VIiV56G/64Zo/8GaUw434IimaPqxwCo"},
	// bcrypt reads 72 bytes, like PHP's password_verify: 80 x's match the 72-x hash.
	{"bcrypt 72-byte cut", strings.Repeat("x", 80), "$2b$04$abcdefghijklmnopqrstuubzadhGtS2zEF.gu0yd0opP6cVzb.e0i"},
}

func TestMailpwVerify(t *testing.T) {
	for _, v := range verifyVectors {
		if !Verify(v.password, v.hash) {
			t.Errorf("%s: Verify(right password) = false", v.name)
		}
		if Verify("y"+v.password, v.hash) || Verify(v.password+"x", v.hash) && len(v.password) < 72 {
			t.Errorf("%s: Verify(wrong password) = true", v.name)
		}
		if Verify("", v.hash) && v.password != "" {
			t.Errorf("%s: Verify(empty) = true", v.name)
		}
	}

	// A bcrypt round trip at the cost Dovecot's BLF-CRYPT uses.
	h, err := bcrypt.GenerateFromPassword([]byte("s3cret pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if !Verify("s3cret pass", string(h)) || Verify("s3cret pas", string(h)) {
		t.Error("bcrypt round trip failed")
	}

	for _, bad := range []string{
		"",
		"plain",
		"{PLAIN}devpass",
		"$6$",                       // no salt, no hash
		"$6$saltstring$",            // empty hash part
		"$6$rounds=999$ab$x",        // rounds below the minimum (libxcrypt and PHP refuse)
		"$6$rounds=x$ab$x",          // not a number
		"$6$rounds=1000000000$ab$x", // above the maximum
		"$7$saltstring$x",           // unknown scheme
		"$2x$05$CCCCCCCCCCCCCCCCCCCCC.E5YPO9kmyuRGyh0XouQYb4YMJKvyOeW", // buggy-variant marker
		"ab01FAX.bQRSU", // traditional DES: not a scheme Dovecot writes
	} {
		if Verify("devpass", bad) || Verify("", bad) {
			t.Errorf("Verify(%q) = true", bad)
		}
	}
	// A SHA-crypt hash with the right prefix and salt but a changed last character.
	mangled := verifyVectors[0].hash[:len(verifyVectors[0].hash)-1] + "2"
	if Verify("Hello world!", mangled) {
		t.Error("mangled sha512 hash verified")
	}
}

// A self-made ARGON2ID hash in Dovecot's stored form, through StripScheme.
func TestMailpwArgon2RoundTrip(t *testing.T) {
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte("mail pass"), salt, 3, 1024, 1, 32)
	stored := "{ARGON2ID}$argon2id$v=19$m=1024,t=3,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
	if !Verify("mail pass", StripScheme(stored)) || Verify("mail pas", StripScheme(stored)) {
		t.Fatal("argon2id round trip failed")
	}
	good := "$argon2id$v=19$m=65536,t=2,p=4$c29tZXNhbHQ$GpZ3sK/oH9p7VIiV56G/64Zo/8GaUw434IimaPqxwCo"
	for _, bad := range []string{
		strings.Replace(good, "v=19", "v=16", 1),                       // only version 0x13
		strings.Replace(good, "$v=19", "", 1),                          // version missing
		strings.Replace(good, "m=65536", "m=8000000", 1),               // memory above the cap
		strings.Replace(good, "t=2", "t=0", 1),                         // no passes
		strings.Replace(good, "p=4", "p=0", 1),                         // no lanes
		strings.Replace(good, "p=4", "p=300", 1),                       // too many lanes
		strings.Replace(good, "m=65536,t=2,p=4", "t=2,m=65536,p=4", 1), // order is fixed
		strings.Replace(good, "c29tZXNhbHQ", "c29tZXNhbHQ=", 1),        // padded base64
		strings.Replace(good, "c29tZXNhbHQ", "!!", 1),                  // not base64
		good + "$x",
		strings.TrimSuffix(good, "GpZ3sK/oH9p7VIiV56G/64Zo/8GaUw434IimaPqxwCo") + "QQ", // 1-byte hash
		strings.Replace(good, "$argon2id$", "$argon2d$", 1),
	} {
		if Verify("password", bad) {
			t.Errorf("Verify(%q) = true", bad)
		}
	}
}

// Passwords longer than MaxPassword never verify, and are not hashed.
func TestMailpwLongPassword(t *testing.T) {
	long := strings.Repeat("p", MaxPassword)
	h, ok := shaCrypt(long, "$6$saltstring")
	if !ok || !Verify(long, h) {
		t.Fatal("a MaxPassword-byte password must verify")
	}
	h2, _ := shaCrypt(long+"p", "$6$saltstring")
	if Verify(long+"p", h2) {
		t.Fatal("a password over MaxPassword verified")
	}
}

func TestMailpwStripScheme(t *testing.T) {
	for in, want := range map[string]string{
		"{SHA512-CRYPT}$6$a$b": "$6$a$b",
		"{BLF-CRYPT}$2y$x":     "$2y$x",
		"$6$a$b":               "$6$a$b",
		"{A}b}c":               "b}c", // only up to the first "}", like explode("}", $h, 2)
		"{}":                   "",
	} {
		if got := StripScheme(in); got != want {
			t.Errorf("StripScheme(%q) = %q, want %q", in, got, want)
		}
	}
}

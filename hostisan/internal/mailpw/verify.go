// Package mailpw checks a mail account password against the hash Dovecot stores, the
// way Hestia's /reset/mail/ did with PHP's password_verify (parity RM7, decision 25).
package mailpw

import (
	"crypto/subtle"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// MaxPassword is the longest password Verify hashes (review I2): SHA-crypt's work
// grows with the password's length, so a longer one is false without hashing.
const MaxPassword = 1024

// bcryptMaxPassword is how much of a password bcrypt reads; PHP's password_verify
// ignores the rest too.
const bcryptMaxPassword = 72

// StripScheme drops Dovecot's "{SCHEME}" prefix: everything up to and including the
// first "}", as PHP's end(explode("}", $hash, 2)) did. A hash without "}" is returned
// unchanged.
func StripScheme(stored string) string {
	if _, rest, ok := strings.Cut(stored, "}"); ok {
		return rest
	}
	return stored
}

// Verify reports whether password matches hash, a crypt(3)-style hash without the
// Dovecot prefix. It knows the schemes Hestia's mail scripts write: SHA-512-crypt
// ($6$), SHA-256-crypt ($5$), MD5-crypt ($1$, Hestia's {MD5}), bcrypt ($2a$, $2b$,
// $2y$, Dovecot's BLF-CRYPT) and Argon2 ($argon2id$, $argon2i$, Dovecot's ARGON2ID and
// ARGON2I). Anything else, or a password over MaxPassword bytes, is false. The
// comparison is constant-time.
func Verify(password, hash string) bool {
	if len(password) > MaxPassword {
		return false
	}
	switch {
	case strings.HasPrefix(hash, "$argon2id$"), strings.HasPrefix(hash, "$argon2i$"):
		return argon2Verify(password, hash)
	case strings.HasPrefix(hash, "$6$"), strings.HasPrefix(hash, "$5$"):
		got, ok := shaCrypt(password, hash)
		return ok && equal(got, hash)
	case strings.HasPrefix(hash, "$1$"):
		got, ok := md5Crypt(password, hash)
		return ok && equal(got, hash)
	case strings.HasPrefix(hash, "$2a$"), strings.HasPrefix(hash, "$2b$"), strings.HasPrefix(hash, "$2y$"):
		pw := []byte(password)
		if len(pw) > bcryptMaxPassword {
			pw = pw[:bcryptMaxPassword]
		}
		return bcrypt.CompareHashAndPassword([]byte(hash), pw) == nil // constant-time inside
	}
	return false
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// cryptB64 is crypt(3)'s base-64 alphabet.
const cryptB64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// b64From24 appends n characters encoding the 24-bit group b2 b1 b0, low bits first
// (b64_from_24bit in the reference code).
func b64From24(out []byte, b2, b1, b0 byte, n int) []byte {
	w := uint(b2)<<16 | uint(b1)<<8 | uint(b0)
	for ; n > 0; n-- {
		out = append(out, cryptB64[w&0x3f])
		w >>= 6
	}
	return out
}

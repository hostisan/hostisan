package mailpw

import (
	"crypto/sha256"
	"crypto/sha512"
	"hash"
	"strconv"
	"strings"
)

// SHA-crypt round limits (Drepper's specification). libxcrypt and PHP refuse a
// "rounds=" outside them, so such a hash never verifies.
const (
	shaRoundsDefault = 5000
	shaRoundsMin     = 1000
	shaRoundsMax     = 999999999
	shaSaltMax       = 16
)

// shaCrypt computes Ulrich Drepper's SHA-256-crypt ("$5$") or SHA-512-crypt ("$6$")
// of password with the setting at the start of setting: an optional "rounds=N$", then
// the salt (up to 16 bytes, ending at "$"). It returns the full hash string, with
// "rounds=N$" only when the setting named it.
func shaCrypt(password, setting string) (string, bool) {
	var newHash func() hash.Hash
	var order [][3]int
	var magic string
	switch {
	case strings.HasPrefix(setting, "$5$"):
		magic, newHash, order = "$5$", sha256.New, sha256Order
	case strings.HasPrefix(setting, "$6$"):
		magic, newHash, order = "$6$", sha512.New, sha512Order
	default:
		return "", false
	}
	rest := setting[len(magic):]
	rounds, custom := shaRoundsDefault, false
	if num, ok := strings.CutPrefix(rest, "rounds="); ok {
		digits, after, found := strings.Cut(num, "$")
		n, err := strconv.ParseUint(digits, 10, 64)
		if !found || err != nil || digits == "" || digits[0] == '+' || n < shaRoundsMin || n > shaRoundsMax {
			return "", false
		}
		rounds, custom, rest = int(n), true, after
	}
	salt := rest
	if i := strings.IndexByte(salt, '$'); i >= 0 {
		salt = salt[:i]
	}
	if len(salt) > shaSaltMax {
		salt = salt[:shaSaltMax]
	}
	key, s := []byte(password), []byte(salt)

	// Digest B: key, salt, key.
	h := newHash()
	h.Write(key)
	h.Write(s)
	h.Write(key)
	b := h.Sum(nil)

	// Digest A.
	h = newHash()
	h.Write(key)
	h.Write(s)
	n := len(key)
	for ; n > len(b); n -= len(b) {
		h.Write(b)
	}
	h.Write(b[:n])
	for i := len(key); i > 0; i >>= 1 {
		if i&1 != 0 {
			h.Write(b)
		} else {
			h.Write(key)
		}
	}
	a := h.Sum(nil)

	// P: the key hashed len(key) times, stretched to len(key).
	h = newHash()
	for range key {
		h.Write(key)
	}
	p := stretch(h.Sum(nil), len(key))

	// S: the salt hashed 16+A[0] times, stretched to len(salt).
	h = newHash()
	for i := 0; i < 16+int(a[0]); i++ {
		h.Write(s)
	}
	sb := stretch(h.Sum(nil), len(s))

	c := a
	for i := 0; i < rounds; i++ {
		h = newHash()
		if i&1 != 0 {
			h.Write(p)
		} else {
			h.Write(c)
		}
		if i%3 != 0 {
			h.Write(sb)
		}
		if i%7 != 0 {
			h.Write(p)
		}
		if i&1 != 0 {
			h.Write(c)
		} else {
			h.Write(p)
		}
		c = h.Sum(nil)
	}

	out := []byte(magic)
	if custom {
		out = append(out, "rounds="+strconv.Itoa(rounds)+"$"...)
	}
	out = append(out, salt...)
	out = append(out, '$')
	for _, g := range order {
		out = b64From24(out, c[g[0]], c[g[1]], c[g[2]], 4)
	}
	if magic == "$5$" {
		out = b64From24(out, 0, c[31], c[30], 3)
	} else {
		out = b64From24(out, 0, 0, c[63], 2)
	}
	return string(out), true
}

// stretch repeats digest to n bytes.
func stretch(digest []byte, n int) []byte {
	out := make([]byte, 0, n)
	for len(out) < n {
		out = append(out, digest[:min(len(digest), n-len(out))]...)
	}
	return out
}

// The byte orders of the final encoding (b64_from_24bit(B2, B1, B0, 4) groups).
var (
	sha256Order = [][3]int{
		{0, 10, 20}, {21, 1, 11}, {12, 22, 2}, {3, 13, 23}, {24, 4, 14},
		{15, 25, 5}, {6, 16, 26}, {27, 7, 17}, {18, 28, 8}, {9, 19, 29},
	}
	sha512Order = [][3]int{
		{0, 21, 42}, {22, 43, 1}, {44, 2, 23}, {3, 24, 45}, {25, 46, 4},
		{47, 5, 26}, {6, 27, 48}, {28, 49, 7}, {50, 8, 29}, {9, 30, 51},
		{31, 52, 10}, {53, 11, 32}, {12, 33, 54}, {34, 55, 13}, {56, 14, 35},
		{15, 36, 57}, {37, 58, 16}, {59, 17, 38}, {18, 39, 60}, {40, 61, 19},
		{62, 20, 41},
	}
)

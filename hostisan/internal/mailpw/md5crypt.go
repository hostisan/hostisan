package mailpw

import (
	"crypto/md5"
	"strings"
)

// md5Crypt computes Poul-Henning Kamp's MD5-crypt of password with the "$1$salt"
// setting at the start of setting (salt: up to 8 bytes, ending at "$"). It returns
// the full "$1$salt$hash" string.
func md5Crypt(password, setting string) (string, bool) {
	const magic = "$1$"
	if !strings.HasPrefix(setting, magic) {
		return "", false
	}
	salt := setting[len(magic):]
	if i := strings.IndexByte(salt, '$'); i >= 0 {
		salt = salt[:i]
	}
	if len(salt) > 8 {
		salt = salt[:8]
	}
	pw := []byte(password)

	alt := md5.New()
	alt.Write(pw)
	alt.Write([]byte(salt))
	alt.Write(pw)
	final := alt.Sum(nil)

	ctx := md5.New()
	ctx.Write(pw)
	ctx.Write([]byte(magic))
	ctx.Write([]byte(salt))
	for n := len(pw); n > 0; n -= 16 {
		ctx.Write(final[:min(n, 16)])
	}
	for i := len(pw); i > 0; i >>= 1 {
		if i&1 != 0 {
			ctx.Write([]byte{0})
		} else {
			ctx.Write(pw[:1])
		}
	}
	final = ctx.Sum(nil)

	for i := 0; i < 1000; i++ {
		c := md5.New()
		if i&1 != 0 {
			c.Write(pw)
		} else {
			c.Write(final)
		}
		if i%3 != 0 {
			c.Write([]byte(salt))
		}
		if i%7 != 0 {
			c.Write(pw)
		}
		if i&1 != 0 {
			c.Write(final)
		} else {
			c.Write(pw)
		}
		final = c.Sum(nil)
	}

	out := []byte(magic + salt + "$")
	for _, g := range [][3]int{{0, 6, 12}, {1, 7, 13}, {2, 8, 14}, {3, 9, 15}, {4, 10, 5}} {
		out = b64From24(out, final[g[0]], final[g[1]], final[g[2]], 4)
	}
	out = b64From24(out, 0, 0, final[11], 2)
	return string(out), true
}

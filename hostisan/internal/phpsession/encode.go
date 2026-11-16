// Package phpsession writes PHP session files for hestia-php, so FileGator (the file
// manager) finds a session the panel prepared for it (Plan 5 parity decision 14).
package phpsession

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// KV is one $_SESSION entry. Value is a string or an int64; nothing else is ever
// written.
type KV struct {
	Key   string
	Value any
}

// Encode serializes values in PHP's "php" session format (session.serialize_handler
// = php, hestia-php's setting): `key|` and serialize() of the value, one after the
// other, in order. Strings are `s:<bytes>:"<raw bytes>";` with nothing escaped (the
// length delimits them), integers `i:<n>;`. It panics on a key the format can't hold
// ("" or containing "|" or "!") and on any other value type: both are programming
// errors, as every key and type is fixed by the caller.
func Encode(values []KV) []byte {
	var b strings.Builder
	for _, kv := range values {
		if kv.Key == "" || strings.ContainsAny(kv.Key, "|!") {
			panic(fmt.Sprintf("phpsession: key %q can't be encoded", kv.Key))
		}
		b.WriteString(kv.Key)
		b.WriteByte('|')
		switch v := kv.Value.(type) {
		case string:
			b.WriteString("s:")
			b.WriteString(strconv.Itoa(len(v)))
			b.WriteString(`:"`)
			b.WriteString(v)
			b.WriteString(`";`)
		case int64:
			b.WriteString("i:")
			b.WriteString(strconv.FormatInt(v, 10))
			b.WriteByte(';')
		default:
			panic(fmt.Sprintf("phpsession: value of key %q is a %T", kv.Key, kv.Value))
		}
	}
	return []byte(b.String())
}

// NewID returns a fresh session id: 32 random bytes as 64 lowercase hex characters
// (valid for PHP, whose ids allow [0-9a-v] at 5 bits per character; and never the
// 26 characters hestia-php makes, so the panel's files are told apart).
func NewID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("phpsession: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b)
}

package hestia

import (
	"bytes"
	"encoding/json"
	"errors"
)

// DecodeObjects decodes a Hestia JSON object whose keys name its values (domains,
// records, IPs) into a slice in the object's key order, which Go maps would lose.
// It calls SetName with each key on the decoded value.
func DecodeObjects[T any, PT interface {
	*T
	SetName(string)
}](stdout []byte) ([]T, error) {
	dec := json.NewDecoder(bytes.NewReader(stdout))
	if tok, err := dec.Token(); err != nil {
		return nil, err
	} else if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("hestia: expected a JSON object")
	}
	out := []T{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v T
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		PT(&v).SetName(key.(string))
		out = append(out, v)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return out, nil
}

// EscapeRawBackslashes doubles each backslash in out that doesn't start a valid JSON
// escape. Hestia prints some free text (log messages) with only `"` escaped, so a
// message such as "rm {} \;" makes the whole list invalid JSON.
func EscapeRawBackslashes(out []byte) []byte {
	isHex := func(c byte) bool {
		return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
	}
	fixed := make([]byte, 0, len(out)+8)
	for i := 0; i < len(out); i++ {
		c := out[i]
		if c != '\\' {
			fixed = append(fixed, c)
			continue
		}
		valid := false
		if i+1 < len(out) {
			switch out[i+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				valid = true
			case 'u':
				valid = i+5 < len(out) && isHex(out[i+2]) && isHex(out[i+3]) && isHex(out[i+4]) && isHex(out[i+5])
			}
		}
		if valid {
			fixed = append(fixed, c, out[i+1])
			i++
			continue
		}
		fixed = append(fixed, '\\', '\\')
	}
	return fixed
}

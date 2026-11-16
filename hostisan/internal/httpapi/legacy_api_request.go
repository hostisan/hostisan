package httpapi

// Request reading for the legacy /api/ and /reset/mail/: ports of the PHP behaviour
// web/api/index.php and web/reset/mail/index.php relied on ($_POST, json_decode, trim,
// empty, basename, escapeshellcmd, exec's output lines), so callers see the same
// result (parity decisions 18 and 23).

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// legacyField is one request value as PHP saw it: a string, or an array (form
// "x[]=…", a JSON object or list), which the handlers never read as a string.
type legacyField struct {
	val   string
	array bool
}

// legacyFields is the request: field name -> value. A missing name is PHP's !isset.
type legacyFields map[string]legacyField

func (f legacyFields) isset(name string) bool { _, ok := f[name]; return ok }

// str is the field's string value ("" when missing).
func (f legacyFields) str(name string) string { return f[name].val }

// legacyPHPPostFields is PHP's $_POST: filled only for a POST with an urlencoded or
// multipart body. raw is what php://input holds for JSON decoding (empty for
// multipart, which PHP consumes).
func legacyPHPPostFields(r *http.Request, body []byte) (post legacyFields, raw []byte) {
	post, raw = legacyFields{}, body
	if r.Method != http.MethodPost {
		return post, raw
	}
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return post, raw
	}
	switch mt {
	case "application/x-www-form-urlencoded":
		return legacyPHPParseForm(string(body)), raw
	case "multipart/form-data":
		return legacyPHPParseMultipart(body, params["boundary"]), nil
	}
	return post, raw
}

// legacyPHPParseForm parses an urlencoded body as PHP filled $_POST: pairs split on "&",
// "+" and %XX decoded (a bad escape stays as is), a name without "=" gets "", the last
// of a repeated name wins.
func legacyPHPParseForm(body string) legacyFields {
	f := legacyFields{}
	for _, pair := range strings.Split(body, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		legacyPHPRegister(f, legacyPHPURLDecode(k), legacyPHPURLDecode(v))
	}
	return f
}

// legacyPHPParseMultipart reads the text fields of a multipart body (file parts went to
// $_FILES, not $_POST). A malformed body keeps the fields read before the error.
func legacyPHPParseMultipart(body []byte, boundary string) legacyFields {
	f := legacyFields{}
	if boundary == "" {
		return f
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := mr.NextPart()
		if err != nil {
			return f
		}
		if p.FileName() == "" && p.FormName() != "" {
			v, err := io.ReadAll(p)
			if err != nil {
				return f
			}
			legacyPHPRegister(f, p.FormName(), string(v))
		}
		p.Close()
	}
}

// legacyPHPRegister stores one form value under PHP's variable name rules: leading spaces
// dropped, a NUL ends the name, " " and "." become "_", "name[…]" makes an array, an
// unclosed "[" becomes "_" (with " ", "." and "[" after it).
func legacyPHPRegister(f legacyFields, name, val string) {
	name = strings.TrimLeft(name, " ")
	if i := strings.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	b := []byte(name)
	array := false
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case ' ', '.':
			b[i] = '_'
		case '[':
			if bytes.IndexByte(b[i+1:], ']') >= 0 {
				b, array = b[:i], true
			} else {
				for j := i; j < len(b); j++ {
					if b[j] == ' ' || b[j] == '.' || b[j] == '[' {
						b[j] = '_'
					}
				}
			}
			i = len(b)
		}
	}
	if len(b) == 0 {
		return
	}
	if array {
		f[string(b)] = legacyField{array: true}
		return
	}
	f[string(b)] = legacyField{val: val}
}

// legacyPHPURLDecode is PHP's urldecode: "+" is a space, %XX a byte, anything else as is.
func legacyPHPURLDecode(s string) string {
	if !strings.ContainsAny(s, "+%") {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '+':
			out = append(out, ' ')
		case c == '%' && i+2 < len(s) && legacyIsHex(s[i+1]) && legacyIsHex(s[i+2]):
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			out = append(out, byte(n))
			i += 2
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

func legacyIsHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// legacyPHPJSONFields decodes a JSON body as json_decode($input, true) did. Only an object
// can carry the fields; anything else (invalid JSON, invalid UTF-8, a scalar, a list)
// is false. Values read as PHP strings: null is unset, true "1", false "", numbers as
// PHP prints them; objects and lists are arrays.
func legacyPHPJSONFields(raw []byte) (legacyFields, bool) {
	if !utf8.Valid(raw) {
		return nil, false
	}
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	f := legacyFields{}
	for k, v := range obj {
		switch v := v.(type) {
		case nil:
		case string:
			f[k] = legacyField{val: v}
		case bool:
			if v {
				f[k] = legacyField{val: "1"}
			} else {
				f[k] = legacyField{val: ""}
			}
		case json.Number:
			f[k] = legacyField{val: legacyPHPNumberString(string(v))}
		default:
			f[k] = legacyField{array: true}
		}
	}
	return f, true
}

// legacyPHPNumberString is a JSON number as PHP's (string) cast prints it after
// json_decode: an integer that fits 64 bits as is; anything else is a float printed
// with precision 14 ("1.5", "100", "1.0E+20", "1.0E-5").
func legacyPHPNumberString(lit string) string {
	if !strings.ContainsAny(lit, ".eE") {
		if n, err := strconv.ParseInt(lit, 10, 64); err == nil {
			return strconv.FormatInt(n, 10)
		}
	}
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return lit // ±Inf: out of range for PHP too
	}
	return legacyPHPFloatString(f)
}

// legacyPHPFloatString ports zend_gcvt(value, 14, '.', 'E'): 14 significant digits, fixed
// notation unless the decimal exponent is below -4 or above 14.
func legacyPHPFloatString(f float64) string {
	if f == 0 {
		if strconv.FormatFloat(f, 'g', -1, 64) == "-0" {
			return "-0"
		}
		return "0"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	m, e, _ := strings.Cut(strconv.FormatFloat(f, 'e', 13, 64), "e")
	exp, _ := strconv.Atoi(e)
	digits := strings.TrimRight(strings.Replace(m, ".", "", 1), "0")
	decpt := exp + 1 // the decimal point is after decpt digits
	if decpt < -3 || decpt > 14 {
		rest := digits[1:]
		if rest == "" {
			rest = "0"
		}
		es := "+"
		if exp < 0 {
			es, exp = "-", -exp
		}
		return sign + digits[:1] + "." + rest + "E" + es + strconv.Itoa(exp)
	}
	if decpt <= 0 {
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	}
	if len(digits) <= decpt {
		return sign + digits + strings.Repeat("0", decpt-len(digits))
	}
	return sign + digits[:decpt] + "." + digits[decpt:]
}

// legacyPHPJSONTruthy reports whether json_decode($s, true) gives a truthy PHP value: not
// invalid, null, false, 0, 0.0, "", "0", or an empty array (so "{}" and "[]" are not).
func legacyPHPJSONTruthy(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return false
	}
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return !legacyPHPEmpty(v)
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		return err != nil || f != 0
	case map[string]any:
		return len(v) > 0
	case []any:
		return len(v) > 0
	}
	return false
}

// legacyPHPTrim is PHP's trim(): " \t\n\r\0\x0B" from both ends.
func legacyPHPTrim(s string) string { return strings.Trim(s, " \t\n\r\x00\x0b") }

// legacyPHPEmpty is PHP's empty() on a string: "" and "0".
func legacyPHPEmpty(s string) bool { return s == "" || s == "0" }

// legacyPHPBasename is PHP's basename() on Linux: trailing "/" dropped, then what follows
// the last "/".
func legacyPHPBasename(s string) string {
	s = strings.TrimRight(s, "/")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// legacyPHPEscapeShellCmd ports PHP's escapeshellcmd() on Linux (in an 8-bit-clean C
// locale): a backslash before each of #&;`|*?~<>^()[]{}$\, newline and 0xFF, and before
// a ' or " that has no partner later in the string.
func legacyPHPEscapeShellCmd(s string) string {
	out := make([]byte, 0, len(s)+8)
	pending := -1 // index of the quote that closes the open pair
	for x := 0; x < len(s); x++ {
		c := s[x]
		switch c {
		case '"', '\'':
			if pending < 0 {
				if i := strings.IndexByte(s[x+1:], c); i >= 0 {
					pending = x + 1 + i
					out = append(out, c)
					continue
				}
				out = append(out, '\\', c)
			} else if s[pending] == c {
				pending = -1
				out = append(out, c)
			} else {
				out = append(out, '\\', c)
			}
		case '#', '&', ';', '`', '|', '*', '?', '~', '<', '>', '^', '(', ')', '[', ']', '{', '}', '$', '\\', '\n', 0xff:
			out = append(out, '\\', c)
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// legacyExecLines is the $output array PHP's exec() fills: one entry per line of stdout,
// each without its trailing whitespace; no entry for an empty stdout.
func legacyExecLines(stdout []byte) []string {
	lines := []string{}
	s := string(stdout)
	for s != "" {
		line := s
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			line, s = s[:i+1], s[i+1:]
		} else {
			s = ""
		}
		lines = append(lines, strings.TrimRight(line, " \t\n\v\f\r"))
	}
	return lines
}

package hestia

import "strings"

// domainExcluded is the core's is_domain_format_valid exclude set (func/main.sh): no
// domain Hestia accepts holds one of these, and most are shell metacharacters.
const domainExcluded = "[]!@#$^&*()+={},<>?_/\\\"|'`;%"

// ValidDomainArg reports whether s is safe to pass to a v-* command as a domain: not
// empty, at most 253 bytes, no leading "-" (it would read as an option), and none of
// the characters Hestia refuses in a domain (shell metacharacters, "/", "_"…),
// whitespace or control characters. It does not check that the name is a real domain;
// Hestia does that.
func ValidDomainArg(s string) bool {
	if s == "" || len(s) > 253 || s[0] == '-' || strings.ContainsAny(s, domainExcluded) {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return false
		}
	}
	return true
}

// ValidRecordID reports whether s is a DNS record ID: 1 to 10 digits, no leading zero.
func ValidRecordID(s string) bool {
	if s == "" || len(s) > 10 || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ValidDatabaseArg reports whether s is safe to pass to a v-* command as a database
// name: not empty, under 64 bytes, no leading "-", and none of Hestia's forbidden
// characters (! | @ # $ ^ & * ( ) + = { } : , < > ? / \ " ' ; % and backtick),
// whitespace or control characters.
func ValidDatabaseArg(s string) bool {
	if s == "" || len(s) >= 64 || s[0] == '-' || strings.ContainsAny(s, "!|@#$^&*()+={}:,<>?/\\\"';%`") {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return false
		}
	}
	return true
}

// ValidMailAccountArg reports whether s is safe to pass to a v-* command as a mail
// account (local part): 1 to 64 ASCII characters from [A-Za-z0-9_.-], not starting
// with "-" (it would read as an option), and a single character must be a letter or
// digit. Hestia's own format check is stricter; this only keeps argv safe.
func ValidMailAccountArg(s string) bool {
	if s == "" || len(s) > 64 || s[0] == '-' {
		return false
	}
	alnum := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }
	if len(s) == 1 {
		return alnum(s[0])
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !alnum(c) && c != '_' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

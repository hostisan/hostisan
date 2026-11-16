package hestia

// ValidPassword ports Hestia's validate_password, ^(?=.*[a-z])(?=.*[A-Z])(?=.*\d).{8,}$:
// at least 8 characters with a lower-case letter, an upper-case letter and a digit,
// and no line break (PCRE's "." does not match one).
func ValidPassword(pw string) bool {
	var lower, upper, digit bool
	n := 0
	for _, r := range pw {
		n++
		switch {
		case r == '\n' || r == '\r':
			return false
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	return n >= 8 && lower && upper && digit
}

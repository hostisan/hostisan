package hestia

import "testing"

func TestValidPassword(t *testing.T) {
	for pw, want := range map[string]bool{
		"Passw0rd":   true,
		"aB3aaaaa":   true,
		"password1":  false,
		"PASSWORD1":  false,
		"Password":   false,
		"Pa1":        false,
		"Pass\nw0rd": false,
	} {
		if got := ValidPassword(pw); got != want {
			t.Errorf("ValidPassword(%q) = %v, want %v", pw, got, want)
		}
	}
}

package hestia

import (
	"strings"
	"testing"
)

func TestValidDomainArg(t *testing.T) {
	for s, want := range map[string]bool{
		"example.com":            true,
		"xn--bcher-kva.example":  true,
		strings.Repeat("a", 253): true,
		"-h":                     false,
		"":                       false,
		"a b":                    false,
		"../etc":                 false,
		"a/b":                    false,
		"a\nb":                   false,
		"a\tb":                   false,
		strings.Repeat("a", 254): false,
		"x;y":                    false,
		"a|b":                    false,
		"a&b":                    false,
		"a$b":                    false,
		"a`b":                    false,
		"a'b":                    false,
		`a"b`:                    false,
		`a\b`:                    false,
		"a(b)":                   false,
		"a<b>":                   false,
		"*.example.com":          false,
		"a%b":                    false,
		"a,b":                    false,
		"a_b.example":            false,
		"bücher.example":         true,
		"sub-1.example.co.uk":    true,
	} {
		if got := ValidDomainArg(s); got != want {
			t.Errorf("ValidDomainArg(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestValidRecordID(t *testing.T) {
	for s, want := range map[string]bool{
		"1": true, "42": true, "1234567890": true,
		"0": false, "01": false, "1;2": false, "abc": false, "": false, "12345678901": false,
	} {
		if got := ValidRecordID(s); got != want {
			t.Errorf("ValidRecordID(%q) = %v, want %v", s, got, want)
		}
	}
}

package main

import (
	"bytes"
	"testing"
)

func TestVersionPrintsNameAndVersion(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"version"}, &out, &out)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := out.String(); got != "hostisan dev\n" {
		t.Fatalf("output = %q, want %q", got, "hostisan dev\n")
	}
}

func TestUnknownCommandFails(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"nope"}, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if errOut.Len() == 0 {
		t.Fatal("expected a usage message on stderr")
	}
}

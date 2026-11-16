package runner

import (
	"strings"
	"testing"
)

func TestResultErrNilOnSuccess(t *testing.T) {
	if err := (Result{Exit: 0}).Err("v-list-user"); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
}

func TestCommandErrorMessageNamesCommandAndCode(t *testing.T) {
	err := (Result{Exit: ExitForbidden, Stdout: []byte("Error: forbidden\n")}).Err("v-delete-user")
	msg := err.Error()
	for _, want := range []string{"v-delete-user", "10", "Error: forbidden"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not contain %q", msg, want)
		}
	}
}

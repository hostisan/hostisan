package i18n

import "testing"

func TestNIsIdentity(t *testing.T) {
	if got := N("Check %s"); got != "Check %s" {
		t.Fatalf("N = %q", got)
	}
}

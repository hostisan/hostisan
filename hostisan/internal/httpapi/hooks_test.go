package httpapi

import (
	"testing"
	"time"
)

func TestServerNow(t *testing.T) {
	fixed := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	s := &server{Deps: Deps{Now: func() time.Time { return fixed }}}
	if got := s.now(); !got.Equal(fixed) {
		t.Errorf("now() = %v, want Deps.Now", got)
	}
	s = &server{}
	if got := s.now(); time.Since(got) > time.Minute || got.IsZero() {
		t.Errorf("now() without Deps.Now = %v", got)
	}
}

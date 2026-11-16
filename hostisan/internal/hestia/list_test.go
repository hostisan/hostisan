package hestia

import "testing"

func TestDecodeObjectsKeepsOrder(t *testing.T) {
	in := `{"b":{"IP":"1"},"a":{"IP":"2"},"10":{"IP":"3"},"9":{"IP":"4"}}`
	got, err := DecodeObjects[DNSDomain]([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	want := []string{"b", "a", "10", "9"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
	if got[1].IP != "2" {
		t.Errorf("IP = %q, want 2", got[1].IP)
	}
}

func TestDecodeObjectsEmpty(t *testing.T) {
	got, err := DecodeObjects[DNSDomain]([]byte("{\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got %#v, want empty non-nil slice", got)
	}
}

func TestDecodeObjectsRejectsNonObject(t *testing.T) {
	if _, err := DecodeObjects[DNSDomain]([]byte("[]")); err == nil {
		t.Error("expected an error for an array")
	}
}

func TestEscapeRawBackslashes(t *testing.T) {
	for in, want := range map[string]string{
		`{"M": "rm {} \;"}`:             `{"M": "rm {} \\;"}`,
		`{"M": "a \"q\" \\ \n \u00e9"}`: `{"M": "a \"q\" \\ \n \u00e9"}`,
		`{"M": "C:\x \u12"}`:            `{"M": "C:\\x \\u12"}`,
		`{"M": "end\`:                   `{"M": "end\\`,
	} {
		if got := string(EscapeRawBackslashes([]byte(in))); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}

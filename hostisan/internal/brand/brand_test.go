package brand

import "testing"

func TestBrandRewrite(t *testing.T) {
	cases := map[string]string{
		"Hestia Control Panel backend port changed": "Hostisan backend port changed",
		"Updated HestiaCP to 1.10.6":                "Updated Hostisan to 1.10.6",
		"Hestia web terminal":                       "Hostisan web terminal",
		"nothing to change":                         "nothing to change",
	}
	for in, want := range cases {
		if got := Rewrite(in); got != want {
			t.Errorf("Rewrite(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPackageName(t *testing.T) {
	cases := map[string]string{
		"hestia":              "hostisan",
		"hestia-nginx":        "hostisan-nginx",
		"hestia-php":          "hostisan-php",
		"hestia-web-terminal": "hostisan-web-terminal",
		"hestia-foo":          "hestia-foo",
		"nginx":               "nginx",
	}
	for in, want := range cases {
		if got := PackageName(in); got != want {
			t.Errorf("PackageName(%q) = %q, want %q", in, got, want)
		}
	}
}

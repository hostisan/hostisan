// Package brand replaces Hestia's name in text the untouched core produces (log
// entries, service names, notices) before it reaches the Hostisan UI.
package brand

import "strings"

// Longest forms first, so "Hestia Control Panel" becomes "Hostisan", not "Hostisan Control Panel".
var replacer = strings.NewReplacer(
	"Hestia Control Panel", "Hostisan",
	"HestiaCP", "Hostisan",
	"Hestia", "Hostisan",
)

func Rewrite(s string) string {
	return replacer.Replace(s)
}

// The package names from spec §5.10; Plan 6 updates this when the packages are renamed.
var packageNames = map[string]string{
	"hestia":              "hostisan",
	"hestia-nginx":        "hostisan-nginx",
	"hestia-php":          "hostisan-php",
	"hestia-web-terminal": "hostisan-web-terminal",
}

// PackageName is the name to show for a Hestia package (updates list, autoupdate).
func PackageName(name string) string {
	if n, ok := packageNames[name]; ok {
		return n
	}
	return name
}

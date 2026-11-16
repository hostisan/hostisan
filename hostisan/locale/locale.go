// Package locale embeds Hostisan's own message catalogue, layered over Hestia's by
// internal/i18n (owner ruling 18):
//
//   - hostisan.pot is the English template: every msgid the Hostisan code uses that
//     Hestia's web/locale/hestiacp.pot does not have. It is generated from the code
//     ("make i18n-extract", hostisan/cmd/i18n); internal/i18n/msgids_test.go fails
//     when it is out of date.
//   - <lang>/LC_MESSAGES/hostisan.po holds a language's translations ("go run
//     ./cmd/i18n init <lang>", then "make i18n-merge"). Untranslated msgids fall back
//     to Hestia's translation of the same text ignoring case, then to English.
package locale

import "embed"

// FS holds hostisan.pot and every <lang>/LC_MESSAGES/hostisan.po. The pattern takes
// the whole directory so a new language needs no change here.
//
//go:embed *
var FS embed.FS

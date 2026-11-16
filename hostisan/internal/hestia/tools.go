package hestia

import (
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SearchHit is one row of `v-search-object` / `v-search-user-object` JSON
// (json_list_search, bin/v-search-object:31-85). The core prints no ID field: its row
// counter is only the object key, which DecodeSearch copies into ID. Values have every
// "," replaced by ", " (ALIAS is "a.com, b.com"). TYPE is web, dns, mail, db or cron;
// KEY is DOMAIN, RECORD, ACCOUNT, DATABASE or JOB. LINK is a record ID, an account
// name or a cron job ID; PARENT the record's zone, the account's domain or the job ID.
type SearchHit struct {
	ID        string `json:"-"`
	USER      string `json:"USER"`
	TYPE      string `json:"TYPE"`
	KEY       string `json:"KEY"`
	RESULT    string `json:"RESULT"`
	ALIAS     string `json:"ALIAS"`
	LINK      string `json:"LINK"`
	PARENT    string `json:"PARENT"`
	SUSPENDED string `json:"SUSPENDED"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
}

func (h *SearchHit) SetName(name string) { h.ID = name }

// DecodeSearch decodes search output in the core's order (its counter's order). The
// empty result "{\n\n}\n" decodes to an empty, non-nil slice.
func DecodeSearch(out []byte) ([]SearchHit, error) {
	return DecodeObjects[SearchHit](out)
}

// searchQueryRe is is_object_name_format_valid's charset for the search object, in
// ASCII ([[:alnum:]] would admit locale letters), up to 50 characters.
var searchQueryRe = regexp.MustCompile(`^[-| ._A-Za-z0-9]{0,50}$`)

// ValidSearchQuery reports whether q may be searched (parity decision 2): the bin's
// charset and length, and no leading "-" (grep would read it as an option, SR34). ""
// is valid: the core lists everything.
func ValidSearchQuery(q string) bool {
	return searchQueryRe.MatchString(q) && !strings.HasPrefix(q, "-")
}

// MaxRestorePath is the longest path a per-file restore accepts, in bytes.
const MaxRestorePath = 4096

// ValidRestorePath reports whether p may be restored for user (parity decision 28,
// before the snapshot listing): absolute and clean, inside /home/<user>/ (not the home
// itself, not conf or anything under it), no segment starting with "-", none of
// * ? [ ] \ (v-restore-file-restic expands the path unquoted in a root shell), no
// whitespace or control characters, at most MaxRestorePath bytes.
func ValidRestorePath(user, p string) bool {
	if !ValidUsername(user) || p == "" || len(p) > MaxRestorePath || path.Clean(p) != p {
		return false
	}
	rest, ok := strings.CutPrefix(p, "/home/"+user+"/")
	if !ok || rest == "" || rest == "conf" || strings.HasPrefix(rest, "conf/") {
		return false
	}
	for _, r := range p {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == utf8.RuneError || strings.ContainsRune(`*?[]\`, r) {
			return false
		}
	}
	for _, seg := range strings.Split(rest, "/") {
		if strings.HasPrefix(seg, "-") {
			return false
		}
	}
	return true
}

// Shell is the account's login shell as v-list-user reports it ("bash", "nologin", …).
func (u User) Shell() string { return u["SHELL"] }

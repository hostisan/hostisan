package mailer

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultTemplateDir is where Hestia keeps its email templates.
const DefaultTemplateDir = "/usr/local/hestia/data/templates/email"

var subjectRe = regexp.MustCompile(`(?s)<subject>(.*?)</subject>`)

// Templates loads Hestia email templates.
type Templates struct {
	Dir string // default DefaultTemplateDir
}

// Load reads <Dir>/<lang>/<name>.html, then <Dir>/<name>.html, like Hestia's
// get_email_template. The text inside <subject>...</subject> is returned as the
// subject and the tag is removed from the body. {{placeholders}} are left for
// the caller. ok is false when neither file exists.
func (t Templates) Load(lang, name string) (subject, body string, ok bool) {
	if !safeSegment(name) || (lang != "" && !safeSegment(lang)) {
		return "", "", false
	}
	dir := t.Dir
	if dir == "" {
		dir = DefaultTemplateDir
	}
	paths := []string{filepath.Join(dir, name+".html")}
	if lang != "" {
		paths = []string{filepath.Join(dir, lang, name+".html"), paths[0]}
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		body = string(b)
		if m := subjectRe.FindStringSubmatch(body); m != nil {
			subject = m[1]
			body = subjectRe.ReplaceAllString(body, "")
		}
		return subject, body, true
	}
	return "", "", false
}

// safeSegment keeps lang and name from escaping the template directory.
func safeSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00")
}

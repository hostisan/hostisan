package extract

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/hostisan/hostisan/internal/i18n"
)

// templateHeader opens hostisan.pot.
const templateHeader = `# Hostisan messages that Hestia's catalogue (web/locale/hestiacp.pot) lacks.
# Generated from the code by "make i18n-extract": do not edit. "#. Hestia:"
# names the Hestia msgid that differs only in case: its translation is used
# until the language's hostisan.po translates the message.
msgid ""
msgstr ""
"Project-Id-Version: Hostisan\n"
"MIME-Version: 1.0\n"
"Content-Type: text/plain; charset=UTF-8\n"
"Content-Transfer-Encoding: 8bit\n"
"Plural-Forms: nplurals=INTEGER; plural=EXPRESSION;\n"
`

// Template writes hostisan.pot: every msg whose msgid is not exactly one of
// Hestia's template, sorted by msgid. Each entry lists the files that use it, and the
// Hestia msgid that matches it ignoring case and spacing (the first in byte order
// when several do).
//
// References name the file without the line, like xgettext --add-location=file, so
// the template changes only when a msgid is added, removed or moves between files.
func Template(hestiaPot []byte, msgs []Msg) ([]byte, error) {
	hestia, err := i18n.ReadPO(hestiaPot)
	if err != nil {
		return nil, fmt.Errorf("hestia template: %w", err)
	}
	exact := map[string]bool{}
	twins := map[string]string{}
	for _, e := range hestia {
		if e.ID == "" || e.Obsolete {
			continue
		}
		exact[e.ID] = true
		k := i18n.Fold(e.ID)
		if cur, ok := twins[k]; !ok || e.ID < cur {
			twins[k] = e.ID
		}
	}
	sorted := append([]Msg(nil), msgs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var b bytes.Buffer
	b.WriteString(templateHeader)
	for _, m := range sorted {
		if m.ID == "" || exact[m.ID] {
			continue
		}
		e := i18n.Entry{ID: m.ID, Strs: []string{""}, Refs: files(m.Refs)}
		if twin, ok := twins[i18n.Fold(m.ID)]; ok {
			e.Extracted = []string{"Hestia: " + quote(twin)}
		}
		b.WriteByte('\n')
		writeEntry(&b, e)
	}
	return b.Bytes(), nil
}

// files turns "path:line" references into sorted, distinct paths.
func files(refs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range refs {
		if i := strings.LastIndexByte(r, ':'); i >= 0 {
			r = r[:i]
		}
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// Merge updates a language's hostisan.po to the template, like msgmerge without
// fuzzy matching: the po's header and translator comments stay; entries follow the
// template's order with its references and extracted comments; translations carry
// over by msgid (an obsolete one comes back); new msgids are untranslated; and
// translated msgids the template no longer has become obsolete "#~" entries.
func Merge(pot, po []byte) ([]byte, error) {
	tmpl, err := i18n.ReadPO(pot)
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	old, err := i18n.ReadPO(po)
	if err != nil {
		return nil, fmt.Errorf("catalogue: %w", err)
	}
	byID := map[string]i18n.Entry{}
	var header *i18n.Entry
	for i, e := range old {
		if e.ID == "" && !e.Obsolete {
			header = &old[i]
			continue
		}
		if prev, ok := byID[e.ID]; ok && !prev.Obsolete {
			continue // the live entry wins over an obsolete one
		}
		byID[e.ID] = e
	}
	var b bytes.Buffer
	used := map[string]bool{}
	for _, t := range tmpl {
		if t.ID == "" {
			if header == nil {
				header = &t
			}
			writeEntry(&b, *header)
			continue
		}
		used[t.ID] = true
		e := i18n.Entry{ID: t.ID, Plural: t.Plural, Strs: t.Strs, Extracted: t.Extracted, Refs: t.Refs}
		if o, ok := byID[t.ID]; ok {
			e.Strs, e.Translator, e.Flags = o.Strs, o.Translator, o.Flags
		}
		b.WriteByte('\n')
		writeEntry(&b, e)
	}
	var gone []string
	for id, e := range byID {
		if !used[id] && hasText(e) {
			gone = append(gone, id)
		}
	}
	sort.Strings(gone)
	for _, id := range gone {
		o := byID[id]
		b.WriteByte('\n')
		writeEntry(&b, i18n.Entry{ID: o.ID, Plural: o.Plural, Strs: o.Strs, Translator: o.Translator, Flags: o.Flags, Obsolete: true})
	}
	return b.Bytes(), nil
}

func hasText(e i18n.Entry) bool {
	for _, s := range e.Strs {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// LangStat is one language's coverage of Hostisan's template.
type LangStat struct {
	Lang   string
	Total  int // msgids in hostisan.pot
	PO     int // translated by the language's hostisan.po
	Twin   int // not in the .po, but Hestia translates a msgid that differs only in case
	Hestia int // msgids Hestia's catalogue translates for the language
}

// Stats counts each language's coverage: every language with a hostisan.po (po,
// keyed by language) or a Hestia catalogue (hestia: msgid → translation), sorted.
func Stats(pot []byte, po map[string][]byte, hestia map[string]map[string]string) []LangStat {
	ids, _ := i18n.MsgIDs(pot)
	langs := map[string]bool{}
	for l := range po {
		langs[l] = true
	}
	for l := range hestia {
		langs[l] = true
	}
	var out []LangStat
	for l := range langs {
		st := LangStat{Lang: l, Total: len(ids), Hestia: len(hestia[l])}
		translated := map[string]bool{}
		if entries, err := i18n.ReadPO(po[l]); err == nil {
			for _, e := range entries {
				if e.Translated() {
					translated[e.ID] = true
				}
			}
		}
		folded := map[string]bool{}
		for id, tr := range hestia[l] {
			if strings.TrimSpace(tr) != "" {
				folded[i18n.Fold(id)] = true
			}
		}
		for _, id := range ids {
			switch {
			case translated[id]:
				st.PO++
			case folded[i18n.Fold(id)]:
				st.Twin++
			}
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lang < out[j].Lang })
	return out
}

// writeEntry writes e in gettext's layout: comments, then msgid, msgid_plural and
// msgstr (msgstr[n] for a plural), "#~ "-prefixed when obsolete.
func writeEntry(b *bytes.Buffer, e i18n.Entry) {
	for _, c := range e.Translator {
		if c == "" {
			b.WriteString("#\n")
		} else {
			b.WriteString("# " + c + "\n")
		}
	}
	for _, c := range e.Extracted {
		b.WriteString("#. " + c + "\n")
	}
	for _, r := range e.Refs {
		b.WriteString("#: " + r + "\n")
	}
	for _, f := range e.Flags {
		b.WriteString("#, " + f + "\n")
	}
	prefix := ""
	if e.Obsolete {
		prefix = "#~ "
	}
	writeField(b, prefix, "msgid", e.ID)
	if e.Plural != "" {
		writeField(b, prefix, "msgid_plural", e.Plural)
		for i, s := range e.Strs {
			writeField(b, prefix, fmt.Sprintf("msgstr[%d]", i), s)
		}
		return
	}
	s := ""
	if len(e.Strs) > 0 {
		s = e.Strs[0]
	}
	writeField(b, prefix, "msgstr", s)
}

// lineWidth is gettext's default page width.
const lineWidth = 79

// writeField writes `keyword "value"`, or like gettext `keyword ""` and then one
// quoted line per piece when the value has an inner line break or is too long.
// Pieces end after each \n, and long pieces break after a space.
func writeField(b *bytes.Buffer, prefix, keyword, value string) {
	q := quote(value)
	inner := strings.Index(value, "\n")
	if len(prefix)+len(keyword)+1+len(q) <= lineWidth && (inner < 0 || inner == len(value)-1) {
		b.WriteString(prefix + keyword + " " + q + "\n")
		return
	}
	b.WriteString(prefix + keyword + " \"\"\n")
	for _, piece := range splitLines(value) {
		for _, part := range wrap(piece, lineWidth-len(prefix)-2) {
			b.WriteString(prefix + quote(part) + "\n")
		}
	}
}

// splitLines splits s after each \n.
func splitLines(s string) []string {
	var out []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// wrap splits s after spaces into parts whose quoted text fits width columns. A
// word longer than width stays whole.
func wrap(s string, width int) []string {
	var out []string
	for len(escape(s)) > width {
		cut := -1
		for i := 0; i < len(s); i++ {
			if s[i] == ' ' {
				if len(escape(s[:i+1])) > width {
					break
				}
				cut = i + 1
			}
		}
		if cut <= 0 {
			break
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	return append(out, s)
}

// quote writes s as a gettext string literal.
func quote(s string) string { return `"` + escape(s) + `"` }

func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\%03o`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// Diff lists how template b differs from template a, one msgid per line: "+" added,
// "-" removed, "~" changed (references, comments). "" when they are the same.
func Diff(a, b []byte) string {
	index := func(data []byte) (map[string]string, []string) {
		entries, _ := i18n.ReadPO(data)
		m, order := map[string]string{}, []string(nil)
		for _, e := range entries {
			if e.ID == "" || e.Obsolete {
				continue
			}
			var buf bytes.Buffer
			writeEntry(&buf, e)
			m[e.ID] = buf.String()
			order = append(order, e.ID)
		}
		return m, order
	}
	am, aIDs := index(a)
	bm, bIDs := index(b)
	var lines []string
	for _, id := range aIDs {
		if _, ok := bm[id]; !ok {
			lines = append(lines, "- "+quote(id))
		}
	}
	for _, id := range bIDs {
		switch old, ok := am[id]; {
		case !ok:
			lines = append(lines, "+ "+quote(id))
		case old != bm[id]:
			lines = append(lines, "~ "+quote(id))
		}
	}
	if len(lines) == 0 && !bytes.Equal(a, b) {
		lines = append(lines, "~ header or layout")
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i][2:] < lines[j][2:] })
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

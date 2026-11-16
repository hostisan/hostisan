package i18n

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// Entry is one message of a .po or .pot file, with the comments gettext tools keep.
type Entry struct {
	ID, Plural string
	Strs       []string // msgstr, or msgstr[0..n] for a plural
	Fuzzy      bool
	Context    bool // has a msgctxt; Hostisan's lookups are by msgid alone

	Translator []string // "# " lines, without the "#"
	Extracted  []string // "#." lines, without the "#."
	Refs       []string // "#:" lines, without the "#:"
	Flags      []string // "#," lines, without the "#,"
	Obsolete   bool     // written as "#~ msgid …": kept by merge, never shown

	// Err is why one of the entry's strings could not be read (an unknown escape).
	// ReadPO fails on it; the catalogue the server reads skips the entry.
	Err error
}

// ReadPO reads the entries of a .po/.pot file: comments, msgctxt, msgid,
// msgid_plural, msgstr and msgstr[n], each followed by any "..." continuation lines.
// Obsolete entries ("#~ msgid …") come back with Obsolete set. The header entry
// (empty msgid) is included. A string it can't read is an error, so the tools that
// write catalogues never lose an entry.
func ReadPO(data []byte) ([]Entry, error) {
	entries, err := readPO(data)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Err != nil {
			return nil, e.Err
		}
	}
	return entries, nil
}

// readPO is ReadPO that keeps going past a string it can't read: it sets that
// entry's Err. Only a broken file structure is an error.
func readPO(data []byte) ([]Entry, error) {
	var (
		out     []Entry
		cur     Entry
		next    Entry   // the comments, msgctxt and obsolete mark of the next entry
		field   *string // the field continuation lines append to
		inUse   bool    // cur has a msgid
		lineNo  int
		pending bool // next has comments
	)
	flush := func() {
		if inUse {
			out = append(out, cur)
		}
		cur, field, inUse = Entry{}, nil, false
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		obsolete := false
		if rest, ok := strings.CutPrefix(line, "#~"); ok {
			obsolete, line = true, strings.TrimSpace(rest)
		}
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "#"):
			if inUse && !pending {
				flush() // a comment starts the next entry
			}
			pending = true
			body := line[1:]
			switch {
			case strings.HasPrefix(body, "."):
				next.Extracted = append(next.Extracted, strings.TrimSpace(body[1:]))
			case strings.HasPrefix(body, ":"):
				next.Refs = append(next.Refs, strings.TrimSpace(body[1:]))
			case strings.HasPrefix(body, ","):
				next.Flags = append(next.Flags, strings.TrimSpace(body[1:]))
				if strings.Contains(body, "fuzzy") {
					next.Fuzzy = true
				}
			case strings.HasPrefix(body, "|"):
				// previous msgid: dropped, Hostisan never writes it
			default:
				next.Translator = append(next.Translator, strings.TrimPrefix(body, " "))
			}
			continue
		case strings.HasPrefix(line, `"`):
			if field == nil {
				return nil, fmt.Errorf("line %d: string outside an entry", lineNo)
			}
			s, err := unquote(line)
			if err != nil && cur.Err == nil {
				cur.Err = fmt.Errorf("line %d: %w", lineNo, err)
			}
			*field += s
			continue
		}
		keyword, rest, _ := strings.Cut(line, " ")
		value, err := unquote(strings.TrimSpace(rest))
		if err != nil {
			err = fmt.Errorf("line %d: %w", lineNo, err)
			if !errors.Is(err, errEscape) {
				return nil, err
			}
		}
		switch {
		case keyword == "msgctxt":
			flush()
			next.Context, pending, field = true, true, new(string)
		case keyword == "msgid":
			flush()
			cur, next, pending = next, Entry{}, false
			cur.ID, cur.Obsolete, inUse = value, obsolete, true
			field = &cur.ID
		case keyword == "msgid_plural" && inUse:
			cur.Plural = value
			field = &cur.Plural
		case (keyword == "msgstr" || strings.HasPrefix(keyword, "msgstr[")) && inUse:
			cur.Strs = append(cur.Strs, value)
			field = &cur.Strs[len(cur.Strs)-1]
		default:
			return nil, fmt.Errorf("line %d: unexpected %q", lineNo, keyword)
		}
		if err != nil {
			if keyword == "msgctxt" {
				next.Err = err
			} else if cur.Err == nil {
				cur.Err = err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()
	return out, nil
}

// Translated reports whether e is shown: a live, non-fuzzy entry without a msgctxt
// whose first msgstr is not blank.
func (e Entry) Translated() bool {
	return e.ID != "" && !e.Obsolete && !e.Fuzzy && !e.Context && len(e.Strs) > 0 && strings.TrimSpace(e.Strs[0]) != ""
}

// parsePO returns msgid → translation like parseMO: plural entries keep their first
// form; the header, untranslated, fuzzy, obsolete and msgctxt entries are skipped. An
// entry with a string it can't read is skipped and logged under name, so one bad
// entry never costs the whole language.
func parsePO(name string, data []byte) (map[string]string, error) {
	entries, err := readPO(data)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.Err != nil {
			slog.Warn("i18n: skipped a .po entry", "file", name, "msgid", e.ID, "err", e.Err)
			continue
		}
		if e.Translated() {
			out[e.ID] = e.Strs[0]
		}
	}
	return out, nil
}

// MsgIDs lists the live msgids of a .po or .pot file in file order, without the
// header or obsolete entries.
func MsgIDs(data []byte) ([]string, error) {
	entries, err := ReadPO(data)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.ID != "" && !e.Obsolete {
			ids = append(ids, e.ID)
		}
	}
	return ids, nil
}

var errEscape = errors.New("unknown escape")

// unquote reads a quoted .po string with gettext's escapes: Go's strconv.Unquote
// rejects some that gettext writes (\' \? and short octal forms).
func unquote(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", strconv.ErrSyntax
	}
	s = s[1 : len(s)-1]
	if !strings.ContainsRune(s, '\\') {
		if strings.ContainsRune(s, '"') {
			return "", strconv.ErrSyntax
		}
		return s, nil
	}
	var b strings.Builder
	var bad error
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			return "", strconv.ErrSyntax
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i == len(s) {
			return "", strconv.ErrSyntax
		}
		switch e := s[i]; e {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '\\', '"', '\'', '?':
			b.WriteByte(e)
		case 'x':
			j := i + 1
			for j < len(s) && j < i+3 && isHex(s[j]) {
				j++
			}
			if j == i+1 {
				bad = fmt.Errorf("%w \\x", errEscape)
				continue
			}
			v, _ := strconv.ParseUint(s[i+1:j], 16, 8)
			b.WriteByte(byte(v))
			i = j - 1
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j := i
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
				j++
			}
			v, _ := strconv.ParseUint(s[i:j], 8, 16)
			if v > 0xff {
				bad = fmt.Errorf("%w \\%s", errEscape, s[i:j])
			}
			b.WriteByte(byte(v))
			i = j - 1
		default:
			bad = fmt.Errorf("%w \\%c", errEscape, e)
		}
	}
	return b.String(), bad
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

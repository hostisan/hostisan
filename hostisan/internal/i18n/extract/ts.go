package extract

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// TSDir is where the UI's TypeScript lives.
const TSDir = "ui/src"

// TS extracts the msgids of the .ts/.tsx files under root/ui/src, skipping tests
// (*.test.ts(x) and src/test/). A msgid is the string literal passed to t(…),
// x.t(…) or msgid(…): '…', "…", or a template literal without ${…}. A template
// literal with ${…}, or a literal followed by +, is a Problem.
//
// The scanner tracks strings, template literals, comments and regular expressions,
// so t( inside any of them is not a call. It does not parse JSX; a quote in JSX text
// opens no string when it follows a letter ("Don't": in code a string never follows
// a name) or has no closing quote on its line (a '…' or "…" string never spans one).
func TS(root string) ([]Msg, []Problem, error) {
	var c collector
	err := walk(root, TSDir, func(rel string) bool {
		if !strings.HasSuffix(rel, ".ts") && !strings.HasSuffix(rel, ".tsx") {
			return false
		}
		return !strings.HasSuffix(rel, ".d.ts") && !strings.Contains(rel, ".test.") &&
			!strings.HasPrefix(rel, TSDir+"/test/")
	}, func(rel string, data []byte) error {
		s := &tsScanner{src: data, line: 1, file: rel, c: &c}
		s.code(false)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	msgs, problems := c.result()
	return msgs, problems, nil
}

type tsScanner struct {
	src  []byte
	i    int
	line int
	file string
	c    *collector

	prev     byte   // the last significant character, 0 at the start
	prevWord string // the word that ended at prev, if any
}

func (s *tsScanner) peek(off int) byte {
	if s.i+off < len(s.src) {
		return s.src[s.i+off]
	}
	return 0
}

func (s *tsScanner) next() byte {
	b := s.src[s.i]
	s.i++
	if b == '\n' {
		s.line++
	}
	return b
}

func isIdent(b byte) bool {
	return b == '_' || b == '$' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b >= 0x80
}

// regexKeywords may come right before a regular expression literal.
var regexKeywords = map[string]bool{"return": true, "typeof": true, "case": true, "do": true, "else": true, "in": true, "of": true, "new": true, "delete": true, "void": true, "throw": true, "yield": true, "await": true}

// code scans code until the end, or with inTemplate until the } closing a ${.
func (s *tsScanner) code(inTemplate bool) {
	depth := 0
	for s.i < len(s.src) {
		b := s.peek(0)
		switch {
		case b == '/' && s.peek(1) == '/':
			for s.i < len(s.src) && s.peek(0) != '\n' {
				s.i++
			}
		case b == '/' && s.peek(1) == '*':
			s.i += 2
			for s.i < len(s.src) && !(s.peek(0) == '*' && s.peek(1) == '/') {
				s.next()
			}
			s.i += 2
		case (b == '\'' || b == '"') && s.i > 0 && isIdent(s.src[s.i-1]):
			s.i++ // JSX text such as "Don't": code never has a quote right after a name
		case b == '\'' || b == '"':
			if _, _, ok := s.quoted(); !ok {
				s.i++ // JSX text: the quote opens nothing
			}
			s.mark(b, "")
		case b == '`':
			s.template()
			s.mark('`', "")
		case b == '/' && s.regexAllowed():
			s.regex()
			s.mark('/', "")
		case isIdent(b):
			start := s.i
			for s.i < len(s.src) && isIdent(s.peek(0)) {
				s.i++
			}
			word := string(s.src[start:s.i])
			dotted := s.prev == '.' && s.prevWord == ""
			s.mark(word[len(word)-1], word)
			if word == "t" || (word == "msgid" && !dotted) {
				s.call(word)
			}
		case b == '{' && inTemplate:
			depth++
			s.next()
			s.mark(b, "")
		case b == '}' && inTemplate:
			s.next()
			if depth == 0 {
				return
			}
			depth--
			s.mark(b, "")
		default:
			s.next()
			if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
				s.mark(b, "")
			}
		}
	}
}

func (s *tsScanner) mark(b byte, word string) { s.prev, s.prevWord = b, word }

func (s *tsScanner) regexAllowed() bool {
	if s.prevWord != "" {
		return regexKeywords[s.prevWord]
	}
	return s.prev == 0 || strings.IndexByte("(,=:[!&|?{};+-*%~^", s.prev) >= 0
}

// regex skips a regular expression literal; one that meets a line break was a
// division after all, and only its slash is skipped.
func (s *tsScanner) regex() {
	j, class := s.i+1, false
	for ; j < len(s.src); j++ {
		switch c := s.src[j]; {
		case c == '\n':
			s.i++
			return
		case c == '\\':
			j++
		case c == '[':
			class = true
		case c == ']':
			class = false
		case c == '/' && !class:
			s.i = j + 1
			return
		}
	}
	s.i++
}

// skipSpace skips white space and comments.
func (s *tsScanner) skipSpace() {
	for s.i < len(s.src) {
		switch b := s.peek(0); {
		case b == ' ' || b == '\t' || b == '\n' || b == '\r':
			s.next()
		case b == '/' && s.peek(1) == '/':
			for s.i < len(s.src) && s.peek(0) != '\n' {
				s.i++
			}
		case b == '/' && s.peek(1) == '*':
			s.i += 2
			for s.i < len(s.src) && !(s.peek(0) == '*' && s.peek(1) == '/') {
				s.next()
			}
			s.i += 2
		default:
			return
		}
	}
}

// call reads the first argument of a t( or msgid( call just scanned.
func (s *tsScanner) call(word string) {
	save, saveLine := s.i, s.line
	s.skipSpace()
	if s.peek(0) != '(' {
		s.i, s.line = save, saveLine
		return
	}
	s.next()
	s.mark('(', "")
	s.skipSpace()
	line, start := s.line, s.i
	var (
		text   string
		ok     bool
		hasArg bool
	)
	switch s.peek(0) {
	case '\'', '"':
		text, _, ok = s.quoted()
	case '`':
		text, hasArg = s.template()
		ok = true
	default:
		return
	}
	if !ok {
		return
	}
	raw := string(s.src[start:s.i])
	s.mark(raw[len(raw)-1], "")
	if hasArg {
		s.c.problem(s.file, line, raw)
		return
	}
	save, saveLine = s.i, s.line
	s.skipSpace()
	after := s.peek(0)
	s.i, s.line = save, saveLine
	switch after {
	case ',', ')':
		s.c.add(text, s.file, line)
	case '+':
		s.c.problem(s.file, line, raw+" + …")
	}
}

// quoted reads a '…' or "…" string at s.i and returns its value. ok is false, and
// nothing is consumed, when a line break comes before the closing quote.
func (s *tsScanner) quoted() (string, int, bool) {
	q := s.src[s.i]
	var b strings.Builder
	j := s.i + 1
	for j < len(s.src) {
		c := s.src[j]
		switch {
		case c == q:
			s.i = j + 1
			return b.String(), j, true
		case c == '\n':
			return "", 0, false
		case c == '\\':
			n, adv := unescape(s.src[j:])
			b.WriteString(n)
			j += adv
			if adv >= 2 && s.src[j-1] == '\n' {
				s.line++ // a line continuation
			}
			continue
		default:
			b.WriteByte(c)
		}
		j++
	}
	return "", 0, false
}

// template reads a template literal at s.i, scanning the code in each ${…}. It
// returns the literal's value and whether it has a ${…}.
func (s *tsScanner) template() (string, bool) {
	s.next()
	var b strings.Builder
	hasArg := false
	for s.i < len(s.src) {
		c := s.peek(0)
		switch {
		case c == '`':
			s.next()
			return b.String(), hasArg
		case c == '\\':
			n, adv := unescape(s.src[s.i:])
			b.WriteString(n)
			for k := 0; k < adv; k++ {
				s.next()
			}
		case c == '$' && s.peek(1) == '{':
			hasArg = true
			s.i += 2
			s.mark('{', "")
			s.code(true)
		default:
			b.WriteByte(s.next())
		}
	}
	return b.String(), hasArg
}

// unescape decodes the JavaScript escape at the start of src (which starts with a
// backslash) and returns its text and length.
func unescape(src []byte) (string, int) {
	if len(src) < 2 {
		return "", len(src)
	}
	switch c := src[1]; c {
	case 'n':
		return "\n", 2
	case 't':
		return "\t", 2
	case 'r':
		return "\r", 2
	case 'b':
		return "\b", 2
	case 'f':
		return "\f", 2
	case 'v':
		return "\v", 2
	case '0':
		return "\x00", 2
	case '\n':
		return "", 2
	case 'x':
		if len(src) >= 4 {
			if n, err := strconv.ParseUint(string(src[2:4]), 16, 8); err == nil {
				return string(rune(n)), 4
			}
		}
	case 'u':
		if len(src) >= 3 && src[2] == '{' {
			if end := strings.IndexByte(string(src), '}'); end > 3 {
				if n, err := strconv.ParseUint(string(src[3:end]), 16, 32); err == nil {
					return string(rune(n)), end + 1
				}
			}
		} else if len(src) >= 6 {
			if n, err := strconv.ParseUint(string(src[2:6]), 16, 16); err == nil {
				return string(rune(n)), 6
			}
		}
	}
	r, size := utf8.DecodeRune(src[1:])
	return string(r), 1 + size
}

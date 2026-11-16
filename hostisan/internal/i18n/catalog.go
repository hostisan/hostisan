// Package i18n serves gettext translations to the UI as JSON. UI strings are English
// msgids: Hestia's where Hestia has the message (web/locale/<lang>/LC_MESSAGES/
// hestiacp.mo, so every existing translation carries over), Hostisan's own otherwise
// (hostisan/locale/<lang>/LC_MESSAGES/hostisan.po, layered over Hestia's).
package i18n

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrUnknownLanguage = errors.New("i18n: unknown language")

type Catalogs struct {
	Dir string // Hestia's catalogues: /usr/local/hestia/web/locale in production

	// Overlay holds Hostisan's template hostisan.pot and its catalogues as
	// <lang>/LC_MESSAGES/hostisan.po (the embedded hostisan/locale in production).
	// Its translations win over Hestia's. nil means none.
	Overlay fs.FS

	mu    sync.Mutex
	cache map[string]map[string]string
}

// Messages returns msgid → translation for lang. From the highest priority down:
// Hostisan's overlay, then for a msgid of Hostisan's template Hestia's translation of
// the msgid that differs only in case and spacing (owner decision O2: "Cron jobs"
// takes "Cron Jobs"), then Hestia's catalogue. English has no catalogue: the msgids are the English text. lang
// must name a language one of the two has, which also rules out path tricks like
// "../etc".
func (c *Catalogs) Messages(lang string) (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.cache[lang]; ok {
		return m, nil
	}
	if lang == "en" {
		return map[string]string{}, nil
	}
	if lang == "" || lang == "." || lang == ".." || strings.ContainsAny(lang, "/\\") {
		return nil, fmt.Errorf("%w: %q", ErrUnknownLanguage, lang)
	}
	overlay, err := c.overlay(lang)
	if err != nil {
		return nil, fmt.Errorf("i18n: %s overlay: %w", lang, err)
	}
	m := map[string]string{}
	switch {
	case c.isLanguage(lang):
		data, err := os.ReadFile(filepath.Join(c.Dir, lang, "LC_MESSAGES", "hestiacp.mo"))
		if err != nil {
			return nil, err
		}
		if m, err = parseMO(data); err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", lang, err)
		}
	case overlay == nil:
		return nil, fmt.Errorf("%w: %q", ErrUnknownLanguage, lang)
	}
	if err := c.fillCaseTwins(m); err != nil {
		return nil, fmt.Errorf("i18n: hostisan.pot: %w", err)
	}
	for id, tr := range overlay {
		m[id] = tr
	}
	if c.cache == nil {
		c.cache = map[string]map[string]string{}
	}
	c.cache[lang] = m
	return m, nil
}

// overlay reads lang's Hostisan catalogue: nil when there is none.
func (c *Catalogs) overlay(lang string) (map[string]string, error) {
	if c.Overlay == nil {
		return nil, nil
	}
	data, err := fs.ReadFile(c.Overlay, lang+"/LC_MESSAGES/hostisan.po")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parsePO(lang+"/LC_MESSAGES/hostisan.po", data)
}

// fillCaseTwins gives each msgid of Hostisan's template that m lacks the translation
// of m's msgid that folds to the same text. When several do, the first in byte order
// wins, so the choice never depends on map order.
func (c *Catalogs) fillCaseTwins(m map[string]string) error {
	if c.Overlay == nil || len(m) == 0 {
		return nil
	}
	data, err := fs.ReadFile(c.Overlay, "hostisan.pot")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	ids, err := MsgIDs(data)
	if err != nil {
		return err
	}
	twins := make(map[string]string, len(m))
	for id := range m {
		k := Fold(id)
		if cur, ok := twins[k]; !ok || id < cur {
			twins[k] = id
		}
	}
	for _, id := range ids {
		if _, ok := m[id]; ok {
			continue
		}
		if twin, ok := twins[Fold(id)]; ok {
			m[id] = m[twin]
		}
	}
	return nil
}

// Fold is the key under which msgids differing only in case and spacing match:
// lower case, with each run of whitespace collapsed to one space and the ends trimmed.
func Fold(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func (c *Catalogs) isLanguage(lang string) bool {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() == lang {
			return true
		}
	}
	return false
}

// parseMO reads a GNU .mo file: a header, then tables of (length, offset) pairs for
// the original strings and their translations. Plural entries keep their first form;
// the header entry (empty msgid) and untranslated entries are skipped.
func parseMO(data []byte) (map[string]string, error) {
	if len(data) < 28 {
		return nil, errors.New("not a .mo file")
	}
	var order binary.ByteOrder
	switch binary.LittleEndian.Uint32(data) {
	case 0x950412de:
		order = binary.LittleEndian
	case 0xde120495:
		order = binary.BigEndian
	default:
		return nil, errors.New("not a .mo file")
	}
	n := int(order.Uint32(data[8:]))
	origTable, transTable := int(order.Uint32(data[12:])), int(order.Uint32(data[16:]))
	str := func(table, i int) (string, error) {
		pos := table + i*8
		if pos+8 > len(data) {
			return "", errors.New("truncated table")
		}
		length, offset := int(order.Uint32(data[pos:])), int(order.Uint32(data[pos+4:]))
		if offset+length > len(data) {
			return "", errors.New("truncated string")
		}
		return string(data[offset : offset+length]), nil
	}
	out := make(map[string]string, n)
	for i := 0; i < n; i++ {
		id, err := str(origTable, i)
		if err != nil {
			return nil, err
		}
		tr, err := str(transTable, i)
		if err != nil {
			return nil, err
		}
		id, _, _ = strings.Cut(id, "\x00")
		first, _, _ := strings.Cut(tr, "\x00")
		if id == "" || strings.TrimSpace(first) == "" {
			continue
		}
		out[id] = first
	}
	return out, nil
}

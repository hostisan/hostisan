// Package extract finds the msgids Hostisan's code uses and keeps Hostisan's message
// catalogue (hostisan/locale) in step with them: the template hostisan.pot, the
// per-language hostisan.po files and their coverage. The code is the single source
// of msgids; the template is generated from it, like ui/src/api/types.gen.ts.
package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Msg is one msgid and where the code uses it, as repo-relative "path:line"
// references sorted by path, then line.
type Msg struct {
	ID   string
	Refs []string
}

// Problem is a msgid the extractor cannot read because the code builds it at run
// time: a template literal with ${…}, a concatenation, or i18n.N on a variable.
type Problem struct {
	File string
	Line int
	Text string
}

// collector gathers msgids and problems while walking files.
type collector struct {
	refs     map[string]map[string]bool
	problems []Problem
}

func (c *collector) add(id, file string, line int) {
	if id == "" {
		return
	}
	if c.refs == nil {
		c.refs = map[string]map[string]bool{}
	}
	if c.refs[id] == nil {
		c.refs[id] = map[string]bool{}
	}
	c.refs[id][fmt.Sprintf("%s:%d", file, line)] = true
}

func (c *collector) problem(file string, line int, text string) {
	c.problems = append(c.problems, Problem{File: file, Line: line, Text: text})
}

// result returns the msgids sorted by text, and the problems by file and line.
func (c *collector) result() ([]Msg, []Problem) {
	msgs := make([]Msg, 0, len(c.refs))
	for id, set := range c.refs {
		refs := make([]string, 0, len(set))
		for r := range set {
			refs = append(refs, r)
		}
		sortRefs(refs)
		msgs = append(msgs, Msg{ID: id, Refs: refs})
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].ID < msgs[j].ID })
	sort.SliceStable(c.problems, func(i, j int) bool {
		a, b := c.problems[i], c.problems[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return msgs, c.problems
}

// sortRefs sorts "path:line" references by path, then by line as a number.
func sortRefs(refs []string) {
	split := func(r string) (string, int) {
		i := strings.LastIndexByte(r, ':')
		if i < 0 {
			return r, 0
		}
		n, _ := strconv.Atoi(r[i+1:])
		return r[:i], n
	}
	sort.Slice(refs, func(i, j int) bool {
		pi, li := split(refs[i])
		pj, lj := split(refs[j])
		if pi != pj {
			return pi < pj
		}
		return li < lj
	})
}

// walk calls fn for every regular file under root/dir that keep accepts, with its
// path relative to root in slash form. A missing dir has no files.
func walk(root, dir string, keep func(rel string) bool, fn func(rel string, data []byte) error) error {
	base := filepath.Join(root, filepath.FromSlash(dir))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !keep(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return fn(rel, data)
	})
}

// Code extracts every msgid of the Go and TypeScript code under the repo root.
func Code(root string) ([]Msg, []Problem, error) {
	gm, gp, err := Go(root)
	if err != nil {
		return nil, nil, err
	}
	tm, tp, err := TS(root)
	if err != nil {
		return nil, nil, err
	}
	var c collector
	for _, list := range [][]Msg{gm, tm} {
		for _, m := range list {
			for _, r := range m.Refs {
				i := strings.LastIndexByte(r, ':')
				line, _ := strconv.Atoi(r[i+1:])
				c.add(m.ID, r[:i], line)
			}
		}
	}
	c.problems = append(append(c.problems, gp...), tp...)
	msgs, problems := c.result()
	return msgs, problems, nil
}

// TemplatePath and HestiaTemplatePath are the templates' paths in the repo.
const (
	TemplatePath       = "hostisan/locale/hostisan.pot"
	HestiaTemplatePath = "web/locale/hestiacp.pot"
)

// Run extracts the repo's msgids and returns the hostisan.pot they make.
func Run(root string) ([]byte, []Problem, error) {
	msgs, problems, err := Code(root)
	if err != nil {
		return nil, nil, err
	}
	hestia, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(HestiaTemplatePath)))
	if err != nil {
		return nil, nil, err
	}
	pot, err := Template(hestia, msgs)
	return pot, problems, err
}

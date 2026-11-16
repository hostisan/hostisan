package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNoFixture means no recorded output matches the call. It fails loudly so missing
// data is never mistaken for "no results".
var ErrNoFixture = errors.New("runner: no fixture")

type fixtureEntry struct {
	Args   []string `json:"args"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr,omitempty"`
	Exit   int      `json:"exit"`
}

// Call is one command a Fixture was asked to run.
type Call struct {
	Cmd  string
	Args []string
	// Secrets holds, by argument position, the first line of each secret temp file
	// argument (see secretFileArgs) and "" elsewhere; nil when there is none.
	Secrets []string
}

// Fixture replays recorded command output from <dir>/<cmd>.json, for development and
// tests. An "*" in a recorded argument list matches any single argument; the first
// matching entry wins.
type Fixture struct {
	entries map[string][]fixtureEntry
	mu      sync.Mutex
	calls   []Call
}

func LoadFixtures(dir string) (*Fixture, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	f := &Fixture{entries: map[string][]fixtureEntry{}}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var list []fixtureEntry
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("runner: fixture %s: %w", filepath.Base(path), err)
		}
		f.entries[strings.TrimSuffix(filepath.Base(path), ".json")] = list
	}
	return f, nil
}

func (f *Fixture) Run(_ context.Context, cmd string, args ...string) (Result, error) {
	if !Allowed(cmd) {
		return Result{}, fmt.Errorf("%w: %s", ErrNotAllowed, cmd)
	}
	return f.replay(cmd, args)
}

func (f *Fixture) replay(cmd string, args []string) (Result, error) {
	match := secretFileArgs(args)
	call := Call{Cmd: cmd, Args: append([]string(nil), args...)}
	for i := range args {
		if match[i] != args[i] {
			if call.Secrets == nil {
				call.Secrets = make([]string, len(args))
			}
			call.Secrets[i] = match[i]
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	for _, e := range f.entries[cmd] {
		if matches(e.Args, match) {
			res := Result{Stdout: []byte(e.Stdout), Exit: e.Exit}
			if e.Stderr != "" {
				res.Stderr = []byte(e.Stderr)
			}
			return res, nil
		}
	}
	return Result{}, fmt.Errorf("%w for %s %v", ErrNoFixture, cmd, args)
}

// RunAny replays a fixture for any v-* name with a fixture file, allowlisted or not
// (AnyRunner); a name with no file is ErrNoCommand, as Sudo's missing command.
func (f *Fixture) RunAny(_ context.Context, cmd string, args ...string) (Result, error) {
	if !anyNameRe.MatchString(cmd) {
		return Result{}, fmt.Errorf("%w: %q", ErrNotAllowed, cmd)
	}
	if !f.Exists(cmd) {
		return Result{}, fmt.Errorf("%w: %s", ErrNoCommand, cmd)
	}
	return f.replay(cmd, args)
}

// Exists reports whether a fixture file was loaded for cmd (a valid v-* name).
func (f *Fixture) Exists(cmd string) bool {
	if !anyNameRe.MatchString(cmd) {
		return false
	}
	_, ok := f.entries[cmd]
	return ok
}

// Calls returns every call made so far, in order.
func (f *Fixture) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// secretFileArgs is args with each secret temp file (secret.TempFile: an absolute
// path to a regular file named hostisan-*) replaced by its first line, which is what
// Hestia's scripts read from a /tmp path. A fixture then keys on the secret itself,
// while Calls still records the path.
func secretFileArgs(args []string) []string {
	var out []string
	for i, a := range args {
		if !filepath.IsAbs(a) || !strings.HasPrefix(filepath.Base(a), "hostisan-") {
			continue
		}
		st, err := os.Lstat(a)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(a)
		if err != nil {
			continue
		}
		if out == nil {
			out = append([]string(nil), args...)
		}
		line, _, _ := strings.Cut(string(data), "\n")
		out[i] = line
	}
	if out == nil {
		return args
	}
	return out
}

func matches(pattern, args []string) bool {
	if len(pattern) != len(args) {
		return false
	}
	for i, p := range pattern {
		if p != "*" && p != args[i] {
			return false
		}
	}
	return true
}

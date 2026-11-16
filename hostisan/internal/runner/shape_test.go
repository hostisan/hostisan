package runner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// devDir holds the hand-written fixtures `make dev` replays.
const devDir = "../../testdata/fixtures/dev"

// shapeExceptions lists accepted shape differences, one per line:
// <cmd> <dotted.key> <reason>. The key is a JSON path, "." (the whole stdout),
// "exit", "columns" (plain output) or "shell" (KEY: value output); "<key>@<arg>" limits it to entries with that arg.
const shapeExceptions = recordedDir + "/shape-exceptions.txt"

// shapeDiff compares the JSON shape of a dev value with a recorded one and returns
// each difference, keyed by its dotted path. Objects compare as key sets; an object
// whose values are all objects is a map of records (its keys are data), so the
// union of its records' keys is compared under "*". Arrays compare their first
// elements. A container empty on either side holds no shape to compare, and null
// matches any type.
func shapeDiff(dev, rec any, path string) map[string]string {
	out := map[string]string{}
	add := func(p, msg string) { out[p] = msg }
	join := func(k string) string {
		if path == "" {
			return k
		}
		return path + "." + k
	}
	if dev == nil || rec == nil {
		return out // null stands in for a missing value of any type (rrd gaps)
	}
	if dt, rt := jsonType(dev), jsonType(rec); dt != rt {
		p := path
		if p == "" {
			p = "."
		}
		add(p, fmt.Sprintf("type: dev %s, recorded %s", dt, rt))
		return out
	}
	switch r := rec.(type) {
	case map[string]any:
		d := dev.(map[string]any)
		if len(d) == 0 || len(r) == 0 {
			return out
		}
		if isRecordMap(d) && isRecordMap(r) {
			for k, v := range shapeDiff(mergeRecords(d), mergeRecords(r), join("*")) {
				out[k] = v
			}
			return out
		}
		for k, rv := range r {
			dv, ok := d[k]
			if !ok {
				add(join(k), "dev lacks")
				continue
			}
			for p, v := range shapeDiff(dv, rv, join(k)) {
				out[p] = v
			}
		}
		for k := range d {
			if _, ok := r[k]; !ok {
				add(join(k), "dev has extra")
			}
		}
	case []any:
		d := dev.([]any)
		if len(d) > 0 && len(r) > 0 {
			for p, v := range shapeDiff(d[0], r[0], join("[]")) {
				out[p] = v
			}
		}
	}
	return out
}

func jsonType(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", v)
}

func isRecordMap(m map[string]any) bool {
	for _, v := range m {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// mergeRecords is the union of a record map's records (the first value of a key wins).
func mergeRecords(m map[string]any) map[string]any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]any{}
	for _, k := range keys {
		for f, v := range m[k].(map[string]any) {
			if _, ok := out[f]; !ok {
				out[f] = v
			}
		}
	}
	return out
}

// loadExceptions reads an exceptions file. With optional set, a missing file is an
// empty list (a setup's own <setup>/shape-exceptions.txt).
func loadExceptions(t *testing.T, path string, optional bool) map[string]bool {
	t.Helper()
	ex := map[string]bool{}
	f, err := os.Open(path)
	if optional && os.IsNotExist(err) {
		return ex
	}
	if err != nil {
		t.Fatalf("exceptions: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			t.Fatalf("exceptions: %q needs <cmd> <dotted.key> <reason>", line)
		}
		ex[parts[0]+" "+parts[1]] = false // true once used
	}
	return ex
}

func readEntries(t *testing.T, path string) []fixtureEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var list []fixtureEntry
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return list
}

// parseStdout parses JSON stdout, or NDJSON (one JSON value per line), returned as
// its first line plus the union of the other lines' records ("rest", nil if none).
// ok is false for any other text.
func parseStdout(s string) (v, rest any, ndjson, ok bool) {
	if json.Unmarshal([]byte(s), &v) == nil {
		return v, nil, false, true
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	others := map[string]any{}
	for i, l := range lines {
		var lv any
		if json.Unmarshal([]byte(l), &lv) != nil {
			return nil, nil, false, false
		}
		if i == 0 {
			v = lv
		} else if m, isObj := lv.(map[string]any); isObj {
			others[fmt.Sprint(i)] = m
		}
	}
	if len(others) > 0 {
		rest = mergeRecords(others)
	}
	return v, rest, true, lines[0] != ""
}

func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}

var shellLineRe = regexp.MustCompile(`^([A-Z][A-Z0-9_]*):\s`)

// shellKeys is the key column of shell-format output ("KEY:   value" lines), or ""
// when s is not in that format.
func shellKeys(s string) string {
	var keys []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		m := shellLineRe.FindStringSubmatch(line + " ")
		if m == nil {
			return ""
		}
		keys = append(keys, m[1])
	}
	return strings.Join(keys, " ")
}

// plainColumns is the tab-separated column count of plain output's first line.
func plainColumns(s string) int {
	line, _, _ := strings.Cut(s, "\n")
	if line == "" {
		return 0
	}
	return strings.Count(line, "\t") + 1
}

// devFixtureDiffs compares every recorded entry with the dev entry that would replay
// for the same args (the first recorded entry per args, as replay picks), and returns
// the differences not listed as exceptions plus the number of recorded entries with
// no dev entry. When the dev entry is an error but the recording succeeded (an
// object only the recording world has), the shape is compared with the first
// successful dev entry of the same arity instead.
func devFixtureDiffs(t *testing.T, recDir string, ex map[string]bool) (diffs, missing []string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(recDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		cmd := strings.TrimSuffix(filepath.Base(path), ".json")
		devPath := filepath.Join(devDir, cmd+".json")
		if _, err := os.Stat(devPath); err != nil {
			missing = append(missing, cmd+" (no dev file)")
			continue
		}
		dev := readEntries(t, devPath)
		seen := map[string]bool{}
		for _, rec := range readEntries(t, path) {
			argKey := strings.Join(rec.Args, "\x00")
			if seen[argKey] {
				continue
			}
			seen[argKey] = true
			var d *fixtureEntry
			for i := range dev {
				if matches(dev[i].Args, rec.Args) {
					d = &dev[i]
					break
				}
			}
			if d == nil {
				missing = append(missing, fmt.Sprintf("%s %q", cmd, rec.Args))
				if rec.Exit != 0 {
					continue
				}
				d = &fixtureEntry{Exit: rec.Exit} // compare the shape with a sibling below
			}
			report := func(key, msg string) {
				if _, ok := ex[cmd+" "+key]; ok {
					ex[cmd+" "+key] = true
					return
				}
				for _, a := range rec.Args {
					if _, ok := ex[cmd+" "+key+"@"+a]; ok {
						ex[cmd+" "+key+"@"+a] = true
						return
					}
				}
				diffs = append(diffs, fmt.Sprintf("%s %q: %s %s", cmd, rec.Args, key, msg))
			}
			if d.Exit != rec.Exit {
				report("exit", fmt.Sprintf("dev %d, recorded %d", d.Exit, rec.Exit))
			}
			if rec.Exit != 0 {
				continue
			}
			if d.Exit != 0 || d.Args == nil {
				d = nil
				for i := range dev {
					if dev[i].Exit == 0 && len(dev[i].Args) == len(rec.Args) &&
						lastArg(dev[i].Args) == lastArg(rec.Args) {
						d = &dev[i]
						break
					}
				}
				if d == nil {
					continue
				}
			}
			rv, rrest, rnd, ok := parseStdout(rec.Stdout)
			if !ok {
				if rk := shellKeys(rec.Stdout); rk != "" {
					if dk := shellKeys(d.Stdout); dk != rk {
						report("shell", fmt.Sprintf("dev keys [%s], recorded [%s]", dk, rk))
					}
				}
				if lastArg(rec.Args) == "plain" {
					if dc, rc := plainColumns(d.Stdout), plainColumns(rec.Stdout); dc != rc && dc > 0 && rc > 0 {
						report("columns", fmt.Sprintf("dev %d, recorded %d", dc, rc))
					}
				}
				continue
			}
			dv, drest, dnd, ok := parseStdout(d.Stdout)
			switch {
			case !ok && strings.TrimSpace(d.Stdout) == "":
				// dev has no data where the recording has some: not a shape.
			case !ok:
				report(".", "dev stdout is not JSON")
			case dnd != rnd:
				report(".", fmt.Sprintf("dev NDJSON %v, recorded NDJSON %v", dnd, rnd))
			default:
				for key, msg := range shapeDiff(dv, rv, "") {
					report(key, msg)
				}
				if drest != nil && rrest != nil {
					for key, msg := range shapeDiff(drest, rrest, "") {
						report("lines."+key, msg)
					}
				}
			}
		}
	}
	sort.Strings(diffs)
	return diffs, missing
}

func TestDevFixturesMatchRecordedShapes(t *testing.T) {
	ex := loadExceptions(t, shapeExceptions, false)
	dirs, err := filepath.Glob(filepath.Join(recordedDir, "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		// A setup's own exceptions (per-setup variation, such as a minimal install's
		// "not enabled" errors) apply to that setup only, on top of the shared ones.
		ownPath := filepath.Join(dir, "shape-exceptions.txt")
		own := loadExceptions(t, ownPath, true)
		dirEx := map[string]bool{}
		for k, v := range ex {
			dirEx[k] = v
		}
		for k := range own {
			dirEx[k] = false
		}
		diffs, missing := devFixtureDiffs(t, dir, dirEx)
		for k := range ex {
			ex[k] = ex[k] || dirEx[k]
		}
		for k := range own {
			if !dirEx[k] {
				t.Errorf("%s: %q matches no difference (remove it)", ownPath, k)
			}
		}
		t.Logf("%s: %d recorded entries have no dev entry: %s", filepath.Base(dir), len(missing), strings.Join(missing, "; "))
		for _, d := range diffs {
			t.Errorf("%s: %s", filepath.Base(dir), d)
		}
	}
	for k, used := range ex {
		if !used {
			t.Errorf("shape-exceptions.txt: %q matches no difference (remove it)", k)
		}
	}
}

func TestShapeDiff(t *testing.T) {
	parse := func(s string) any {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	got := shapeDiff(
		parse(`{"a.test":{"IP":"1","OLD":"x","N":{"Z":1}},"b.test":{"IP":"2"}}`),
		parse(`{"c.test":{"IP":"3","NEW":"y","N":{"Z":"1"}}}`), "")
	want := map[string]string{"*.OLD": "dev has extra", "*.NEW": "dev lacks", "*.N.Z": "type: dev number, recorded string"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("record map diff = %v, want %v", got, want)
	}
	if got := shapeDiff(parse(`[{"A":1}]`), parse(`[{"A":1,"B":2}]`), ""); fmt.Sprint(got) != "map[[].B:dev lacks]" {
		t.Errorf("array diff = %v", got)
	}
	if got := shapeDiff(parse(`{}`), parse(`[]`), ""); fmt.Sprint(got) != "map[.:type: dev object, recorded array]" {
		t.Errorf("empty forms diff = %v", got)
	}
	if got := shapeDiff(parse(`{}`), parse(`{"x":{"A":1}}`), ""); len(got) != 0 {
		t.Errorf("empty dev map diff = %v", got)
	}
}

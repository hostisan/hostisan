package extract

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// GoDir is where Go msgids live: the messages the UI shows come from the panel's
// internal packages.
const GoDir = "hostisan/internal"

// argSink is a function whose arguments at the given indexes are msgids. every > 0
// also takes every every-th argument after the last index (blankFields' labels).
type argSink struct {
	args   []int
	every  int
	method bool   // only called as x.f(…), x not a package
	plain  bool   // only called as f(…) or pkg.f(…)
	pkg    string // only called as pkg.f(…)
	n      int    // the call has exactly n arguments; 0 means any
	strict bool   // a non-constant argument is a Problem
}

// goSinks are the functions whose arguments the UI shows translated. They are
// matched by name, without types, so each is narrowed as far as its calls allow.
var goSinks = map[string][]argSink{
	// An API error's message.
	"WriteError":    {{args: []int{3}, n: 4}},
	"invalid":       {{args: []int{1}, plain: true}},
	"sysNetInvalid": {{args: []int{1}, plain: true}},
	"sysBadRequest": {{args: []int{1}, plain: true}},
	"sysReject":     {{args: []int{2}, plain: true}},
	// A failed job's message (webapp.Fail builds a jobs.Failure).
	"Fail": {{args: []int{0}, plain: true}},
	// An account email's text.
	"acctTranslate": {{args: []int{1}, method: true}},
	// A job's title and step names, and the settings planner's step names.
	"Step":      {{args: []int{0}, method: true, n: 1}},
	"step":      {{args: []int{2}, method: true}},
	"startJob":  {{args: []int{2}, method: true}},
	"add":       {{args: []int{0}, method: true}},
	"config":    {{args: []int{0}, method: true, n: 3}},
	"trueFalse": {{args: []int{0}, method: true, n: 3}},
	"yesNo":     {{args: []int{0}, method: true, n: 3}},
	"yesOnly":   {{args: []int{0}, method: true, n: 3}},
	"enum":      {{args: []int{0}, method: true, n: 4}},
	"toggle":    {{args: []int{3, 5}, method: true, n: 7}},
	// Field labels, filled into "Field "%s" can not be blank." and translated.
	"sysBlank":    {{args: []int{0}, plain: true, n: 1}},
	"blankFields": {{args: []int{1}, every: 2, plain: true}},
	// A web socket's close reason (the terminal's).
	"Close": {{args: []int{1}, method: true, n: 2}},
	// The marker for any other msgid.
	"N": {{args: []int{0}, pkg: "i18n", n: 1, strict: true}},
}

// fieldSinks are struct fields that hold a msgid, by type name.
var fieldSinks = map[string]string{
	"Failure":     "Message", // jobs.Failure, a failed job's message
	"ErrorBody":   "Message", // an API error written by hand
	"BulkFailure": "Message", // one refused item of a bulk action
	"Spec":        "Title",   // jobs.Spec, a job's title
}

// fieldSinkPkg narrows a fieldSinks type to its package: "Failure" is jobs.Failure.
var fieldSinkPkg = map[string]string{"Failure": "jobs", "Spec": "jobs"}

// Go extracts the msgids of the Go files under root/hostisan/internal, skipping
// tests. A string is a msgid when it is a constant passed to one of the sinks the UI
// translates (goSinks, fieldSinks), or the value of a const or var named <x>Msg or
// <x>Msgid. A constant is a literal, a sum of literals, or the name of a string
// constant of a scanned package (restoreTitle, terminal.ReasonTooMany). i18n.N("…")
// marks any other msgid.
//
// A sink given a concatenation with a non-constant part, or i18n.N given anything
// but a constant, is a Problem: the msgid is built at run time.
//
// Text starting with "Error: " is the core's own output (or Hostisan's copy of it,
// such as "Error: invalid domain format :: x"). The UI shows it as sent, as Hestia
// did, so it is neither a msgid nor a Problem.
func Go(root string) ([]Msg, []Problem, error) {
	type file struct {
		rel string
		f   *ast.File
	}
	var files []file
	fset := token.NewFileSet()
	err := walk(root, GoDir, func(rel string) bool {
		return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
	}, func(rel string, data []byte) error {
		f, err := parser.ParseFile(fset, rel, data, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files = append(files, file{rel, f})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	// Every package's string constants, by directory and by package name.
	consts := map[string]map[string]string{}
	for _, f := range files {
		for _, k := range []string{"dir:" + path.Dir(f.rel), "pkg:" + f.f.Name.Name} {
			if consts[k] == nil {
				consts[k] = map[string]string{}
			}
			for name, v := range fileConsts(f.f) {
				consts[k][name] = v
			}
		}
	}
	var c collector
	for _, f := range files {
		g := &goScan{c: &c, fset: fset, f: f.f, rel: f.rel, local: consts["dir:"+path.Dir(f.rel)], consts: consts, imports: imports(f.f)}
		g.file()
	}
	msgs, problems := c.result()
	return msgs, problems, nil
}

// fileConsts lists the file's package-level string constants.
func fileConsts(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					if s, ok := constString(vs.Values[i]); ok {
						out[name.Name] = s
					}
				}
			}
		}
	}
	return out
}

// imports names a file's imported packages, by the name the file uses.
func imports(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := path.Base(p)
		if im.Name != nil {
			name = im.Name.Name
		}
		out[name] = true
	}
	return out
}

type goScan struct {
	c       *collector
	fset    *token.FileSet
	f       *ast.File
	rel     string
	local   map[string]string            // this package's constants
	consts  map[string]map[string]string // "pkg:<name>" → constants
	imports map[string]bool
}

func (g *goScan) line(n ast.Node) int { return g.fset.Position(n.Pos()).Line }

// value evaluates e as a constant string: literals, sums, and named constants.
func (g *goScan) value(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		s, ok := g.local[x.Name]
		return s, ok
	case *ast.SelectorExpr:
		if pkg, ok := x.X.(*ast.Ident); ok && g.imports[pkg.Name] {
			s, ok := g.consts["pkg:"+pkg.Name][x.Sel.Name]
			return s, ok
		}
		return "", false
	case *ast.ParenExpr:
		return g.value(x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		a, ok := g.value(x.X)
		if !ok {
			return "", false
		}
		b, ok := g.value(x.Y)
		return a + b, ok
	}
	return constString(e)
}

// sink records the msgid e; strict also reports a non-constant e.
func (g *goScan) sink(e ast.Expr, strict bool) {
	s, ok := g.value(e)
	switch {
	case ok && strings.HasPrefix(s, "Error: "), !ok && coreOutput(e):
		return
	case ok:
		g.c.add(s, g.rel, g.line(e))
	case strict || hasStringLit(e):
		var b bytes.Buffer
		_ = printer.Fprint(&b, g.fset, e)
		g.c.problem(g.rel, g.line(e), b.String())
	}
}

func (g *goScan) file() {
	ast.Inspect(g.f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if (strings.HasSuffix(name.Name, "Msg") || strings.HasSuffix(name.Name, "Msgid")) && i < len(n.Values) {
					if s, ok := constString(n.Values[i]); ok && !strings.HasPrefix(s, "Error: ") {
						g.c.add(s, g.rel, g.line(n.Values[i]))
					}
				}
			}
		case *ast.CompositeLit:
			name, pkg := callee(n.Type)
			field, ok := fieldSinks[name]
			if !ok {
				break
			}
			if want, ok := fieldSinkPkg[name]; ok && pkg != want && !(pkg == "" && g.f.Name.Name == want) {
				break
			}
			for _, el := range n.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == field {
						g.sink(kv.Value, false)
					}
				}
			}
		case *ast.CallExpr:
			name, recv := callee(n.Fun)
			for _, s := range goSinks[name] {
				if s.matches(n, recv, g.imports[recv]) {
					for _, i := range s.indexes(len(n.Args)) {
						g.sink(n.Args[i], s.strict)
					}
				}
			}
		}
		return true
	})
}

// matches reports whether call, named with receiver or package recv, is this sink.
func (s argSink) matches(call *ast.CallExpr, recv string, recvIsPkg bool) bool {
	if s.n > 0 && len(call.Args) != s.n {
		return false
	}
	switch {
	case s.pkg != "":
		return recv == s.pkg && recvIsPkg
	case s.method:
		return recv != "" && !recvIsPkg
	case s.plain:
		return recv == "" || recvIsPkg
	}
	return true
}

// indexes lists the argument indexes of a call with n arguments.
func (s argSink) indexes(n int) []int {
	var out []int
	for _, i := range s.args {
		if i < n {
			out = append(out, i)
		}
	}
	if s.every > 0 && len(s.args) > 0 {
		for i := s.args[len(s.args)-1] + s.every; i < n; i += s.every {
			out = append(out, i)
		}
	}
	return out
}

// callee names a call's function: "WriteError" for WriteError(…), and for x.Step(…)
// "Step" with recv "x" (or "." when x is not a plain name).
func callee(fun ast.Expr) (name, recv string) {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name, ""
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			return f.Sel.Name, id.Name
		}
		return f.Sel.Name, "."
	}
	return "", ""
}

// constString evaluates a string literal or a sum of them.
func constString(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return constString(e.X)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		a, ok := constString(e.X)
		if !ok {
			return "", false
		}
		b, ok := constString(e.Y)
		return a + b, ok
	}
	return "", false
}

// hasStringLit reports whether e is a concatenation with a string literal in it.
func hasStringLit(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.ParenExpr:
		return hasStringLit(e.X)
	case *ast.BinaryExpr:
		return e.Op == token.ADD && (hasStringLit(e.X) || hasStringLit(e.Y))
	}
	return false
}

// coreOutput reports whether e starts with the literal "Error: ".
func coreOutput(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.BinaryExpr:
			e = x.X
		case *ast.BasicLit:
			s, err := strconv.Unquote(x.Value)
			return x.Kind == token.STRING && err == nil && strings.HasPrefix(s, "Error: ")
		default:
			return false
		}
	}
}

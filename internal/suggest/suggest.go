// Package suggest finds small tasks in a workspace: TODO / FIXME style
// comments and Go code that is likely slow (loops nested three deep, a
// regexp compiled or a defer inside a loop, a string grown with += in a
// loop). Tasks.Suggested lists them as a pick list, most useful first.
//
// It only reads files. It never runs code and never sends anything away.
package suggest

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Kind names what a task is about.
type Kind string

const (
	KindFixme Kind = "FIXME"
	KindBug   Kind = "BUG"
	KindTodo  Kind = "TODO"
	KindHack  Kind = "HACK"
	KindXXX   Kind = "XXX"
	KindPerf  Kind = "PERF" // a PERF / OPTIMIZE comment
	KindSlow  Kind = "SLOW" // found in the Go code itself
)

// weight orders the kinds: bugs first, then work someone wrote down,
// then code that may be slow.
var weight = map[Kind]int{
	KindFixme: 5, KindBug: 5,
	KindPerf: 4,
	KindTodo: 3,
	KindHack: 2, KindXXX: 2,
	KindSlow: 1,
}

// Task is one suggestion.
type Task struct {
	Kind Kind
	File string // slash path, relative to the scanned root
	Line int    // 1-based
	Text string // the comment text after the marker, or why the code is slow
}

// Weight is how useful the task is (higher first).
func (t Task) Weight() int { return weight[t.Kind] }

// Where is "file:line".
func (t Task) Where() string { return t.File + ":" + strconv.Itoa(t.Line) }

// Options limit a scan. Zero values use the defaults.
type Options struct {
	MaxFiles int   // files read (default 5000)
	MaxTasks int   // tasks kept (default 500)
	MaxBytes int64 // bigger files are skipped (default 1 MiB)
}

func (o Options) withDefaults() Options {
	if o.MaxFiles <= 0 {
		o.MaxFiles = 5000
	}
	if o.MaxTasks <= 0 {
		o.MaxTasks = 500
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 1 << 20
	}
	return o
}

// Result is what Scan found.
type Result struct {
	Tasks     []Task
	Files     int  // files read
	Truncated bool // a limit was hit: there may be more
}

// skipDirs are folders that hold no code of the project.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "third_party": true,
	"dist": true, "build": true, "target": true, "out": true, "coverage": true,
	"__pycache__": true, ".venv": true, "venv": true, ".tox": true,
	".idea": true, ".vscode": true, ".termocode": true, ".claude": true,
}

// Scan walks root and returns the tasks, most useful first.
func Scan(root string, opt Options) (Result, error) {
	opt = opt.withDefaults()
	var res Result
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // an unreadable folder is left out
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || skipFile(name) {
			return nil
		}
		if res.Files >= opt.MaxFiles {
			res.Truncated = true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil || info.Size() > opt.MaxBytes {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil || isBinary(src) {
			return nil
		}
		res.Files++
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		res.Tasks = append(res.Tasks, ScanComments(rel, src)...)
		if strings.HasSuffix(name, ".go") {
			res.Tasks = append(res.Tasks, ScanGo(rel, src)...)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	Sort(res.Tasks)
	if len(res.Tasks) > opt.MaxTasks {
		res.Tasks = res.Tasks[:opt.MaxTasks]
		res.Truncated = true
	}
	return res, nil
}

// Sort orders tasks: higher weight first, then by file and line.
func Sort(ts []Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if a.Weight() != b.Weight() {
			return a.Weight() > b.Weight()
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
}

// skipFile is true for lock files, minified code and other files whose
// markers are not the project's own.
func skipFile(name string) bool {
	switch {
	case strings.HasSuffix(name, ".min.js"), strings.HasSuffix(name, ".min.css"),
		strings.HasSuffix(name, ".map"), strings.HasSuffix(name, ".lock"),
		strings.HasSuffix(name, ".sum"), strings.HasSuffix(name, ".svg"),
		name == "package-lock.json", name == "pnpm-lock.yaml":
		return true
	}
	return false
}

func isBinary(src []byte) bool {
	head := src
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// markerRe finds a marker word inside a comment. The comment token
// (//, #, /*, *, --, ;, <!--) must come before it on the line, so a
// "TODO" in a string or a name is not a task.
var markerRe = regexp.MustCompile(`(?://|#|/\*|^\s*\*|--|;|<!--)\s*(?:@)?\b(TODO|FIXME|BUG|HACK|XXX|OPTIMIZE|PERF)\b(?:\([^)]*\))?\s*[:!-]?\s*(.*)$`)

// commentEnd is cut from the end of the text.
var commentEnd = regexp.MustCompile(`\s*(?:\*/|-->)\s*$`)

// ScanComments finds the marker comments in one file.
func ScanComments(file string, src []byte) []Task {
	var out []Task
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if !strings.Contains(text, "TODO") && !strings.Contains(text, "FIXME") && !strings.Contains(text, "BUG") &&
			!strings.Contains(text, "HACK") && !strings.Contains(text, "XXX") && !strings.Contains(text, "OPTIMIZE") &&
			!strings.Contains(text, "PERF") {
			continue
		}
		mm := markerRe.FindStringSubmatch(text)
		if mm == nil {
			continue
		}
		kind := Kind(mm[1])
		if kind == "OPTIMIZE" {
			kind = KindPerf
		}
		body := strings.TrimSpace(commentEnd.ReplaceAllString(mm[2], ""))
		if body == "" {
			body = "(no text)"
		}
		out = append(out, Task{Kind: kind, File: file, Line: line, Text: clip(body, 160)})
	}
	return out
}

// ScanGo finds likely slow code in one Go file. A file that does not
// parse gives no tasks.
func ScanGo(file string, src []byte) []Task {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	w := &goWalker{fset: fset, file: file, seen: map[int]bool{}}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			w.walk(fd.Body, 0)
		}
	}
	return w.out
}

type goWalker struct {
	fset *token.FileSet
	file string
	out  []Task
	seen map[int]bool // one task per line
}

func (w *goWalker) add(pos token.Pos, why string) {
	line := w.fset.Position(pos).Line
	if w.seen[line] {
		return
	}
	w.seen[line] = true
	w.out = append(w.out, Task{Kind: KindSlow, File: w.file, Line: line, Text: why})
}

// walk visits n; depth is how many loops are around it. A function
// literal starts again at depth 0 (it may run outside the loop).
func (w *goWalker) walk(n ast.Node, depth int) {
	ast.Inspect(n, func(c ast.Node) bool {
		if c == nil || c == n {
			return true
		}
		switch x := c.(type) {
		case *ast.FuncLit:
			w.walk(x.Body, 0)
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			if depth+1 >= 3 {
				w.add(x.Pos(), "loops nested "+strconv.Itoa(depth+1)+" deep: the work grows fast with the input")
			}
			w.walk(x, depth+1)
			return false
		case *ast.DeferStmt:
			if depth > 0 {
				w.add(x.Pos(), "defer inside a loop: nothing runs until the function returns")
			}
		case *ast.CallExpr:
			if depth > 0 && isRegexpCompile(x) {
				w.add(x.Pos(), "regexp compiled inside a loop: compile it once, outside")
			}
		case *ast.AssignStmt:
			if depth > 0 && x.Tok == token.ADD_ASSIGN && len(x.Rhs) == 1 && isStringish(x.Rhs[0]) {
				w.add(x.Pos(), "string grown with += inside a loop: use a strings.Builder")
			}
		}
		return true
	})
}

func isRegexpCompile(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "regexp" {
		return false
	}
	switch sel.Sel.Name {
	case "Compile", "MustCompile", "CompilePOSIX", "MustCompilePOSIX":
		return true
	}
	return false
}

// isStringish is true when e is plainly a string: a string literal, a
// string conversion, fmt.Sprint* or a + with a string literal.
func isStringish(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING
	case *ast.BinaryExpr:
		return x.Op == token.ADD && (isStringish(x.X) || isStringish(x.Y))
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "string" {
			return true
		}
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Sprint") {
				return true
			}
		}
	case *ast.ParenExpr:
		return isStringish(x.X)
	}
	return false
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

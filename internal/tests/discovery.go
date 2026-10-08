package tests

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ── Framework detection ──────────────────────────────────────────────────

// Detect returns the test framework for root by marker file (first match
// wins): go.mod, Cargo.toml, Python markers, package.json.
func Detect(root string) Framework {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	switch {
	case has("go.mod"):
		return FrameworkGo
	case has("Cargo.toml"):
		return FrameworkCargo
	case has("pyproject.toml"), has("setup.py"), has("pytest.ini"), has("setup.cfg"), has("tox.ini"):
		return FrameworkPytest
	case has("package.json"):
		data, _ := os.ReadFile(filepath.Join(root, "package.json"))
		return detectJS(string(data))
	}
	return FrameworkNone
}

// detectJS picks vitest when package.json mentions it, else jest.
func detectJS(pkgJSON string) Framework {
	if strings.Contains(pkgJSON, `"vitest"`) || strings.Contains(pkgJSON, "vitest ") {
		return FrameworkVitest
	}
	return FrameworkJest
}

// ── Test-file predicates ─────────────────────────────────────────────────

// IsTestFile reports whether name (basename or path) may hold tests for fw.
func IsTestFile(fw Framework, name string) bool {
	base := filepath.Base(name)
	switch fw {
	case FrameworkGo:
		return strings.HasSuffix(base, "_test.go")
	case FrameworkPytest:
		return strings.HasSuffix(base, ".py") &&
			(strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py"))
	case FrameworkCargo:
		return strings.HasSuffix(base, ".rs")
	case FrameworkJest, FrameworkVitest:
		for _, ext := range []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts"} {
			if !strings.HasSuffix(base, ext) {
				continue
			}
			stem := strings.TrimSuffix(base, ext)
			if strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") {
				return true
			}
			slash := filepath.ToSlash(name)
			return strings.Contains(slash, "/__tests__/")
		}
	}
	return false
}

// ── Per-language parsers (pure: path + content in, cases out) ────────────

// goTestDeclRe matches `func TestXxx(` / `func FuzzXxx(`. Go requires the
// suffix to not start with a lower-case letter.
var goTestDeclRe = regexp.MustCompile(`^func\s+((?:Test|Fuzz)(?:[^a-z(\s][A-Za-z0-9_]*)?)\s*\(`)

// ParseGoTests returns the TestXxx / FuzzXxx functions of a _test.go file.
// Helpers (func setup()) and benchmarks are ignored — `go test` does not
// run benchmarks without -bench.
func ParseGoTests(path, content string) []*Case {
	var out []*Case
	for i, line := range strings.Split(content, "\n") {
		if m := goTestDeclRe.FindStringSubmatch(line); m != nil && m[1] != "TestMain" {
			out = append(out, &Case{Name: m[1], File: path, Line: i + 1})
		}
	}
	return out
}

var (
	pyClassRe = regexp.MustCompile(`^class\s+(Test\w*)\s*[:(]`)
	pyFuncRe  = regexp.MustCompile(`^(\s*)(?:async\s+)?def\s+(test\w*)\s*\(`)
)

// ParsePytest returns top-level `def test_*` functions and `test*`
// methods of top-level `class Test*` classes. Names follow pytest node
// ids without the file: "test_x" or "TestCls::test_x".
func ParsePytest(path, content string) []*Case {
	var out []*Case
	class := ""
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent == 0 {
			class = ""
			if m := pyClassRe.FindStringSubmatch(line); m != nil {
				class = m[1]
				continue
			}
		}
		m := pyFuncRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		switch {
		case m[1] == "":
			out = append(out, &Case{Name: m[2], File: path, Line: i + 1})
		case class != "":
			out = append(out, &Case{Name: class + "::" + m[2], File: path, Line: i + 1})
		}
	}
	return out
}

var (
	rustTestAttrRe = regexp.MustCompile(`^#\[(?:[\w]+::)*test\b`)
	rustFnRe       = regexp.MustCompile(`^(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+(\w+)`)
	rustModRe      = regexp.MustCompile(`^(?:pub(?:\([^)]*\))?\s+)?mod\s+(\w+)\s*\{`)
)

// ParseRustTests returns `#[test]` (and `#[tokio::test]`-style) functions.
// Names carry the inline-module path inside the file ("tests::it_works")
// so they can be suffix-matched against libtest's full paths.
func ParseRustTests(path, content string) []*Case {
	type mod struct {
		name   string
		indent int
	}
	var (
		out     []*Case
		mods    []mod
		pending bool
	)
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		// Leaving a module: a closing brace at (or left of) its indent.
		for len(mods) > 0 && indent <= mods[len(mods)-1].indent && strings.HasPrefix(trimmed, "}") {
			mods = mods[:len(mods)-1]
		}
		if m := rustModRe.FindStringSubmatch(trimmed); m != nil {
			mods = append(mods, mod{name: m[1], indent: indent})
			continue
		}
		if rustTestAttrRe.MatchString(trimmed) {
			pending = true
			continue
		}
		if strings.HasPrefix(trimmed, "#[") {
			continue // other attributes (#[ignore], #[should_panic]) keep pending
		}
		if pending {
			if m := rustFnRe.FindStringSubmatch(trimmed); m != nil {
				var parts []string
				for _, md := range mods {
					parts = append(parts, md.name)
				}
				parts = append(parts, m[1])
				out = append(out, &Case{Name: strings.Join(parts, "::"), File: path, Line: i + 1})
			}
			pending = false
		}
	}
	return out
}

// jsCallRe matches describe/it/test (with .only/.skip/… modifiers) and a
// literal first argument in any of the three quote styles.
var jsCallRe = regexp.MustCompile(`\b(describe|it|test)(?:\.(?:only|skip|concurrent|todo|failing|sequential))?\s*\(\s*(?:'([^']*)'|"([^"]*)"|` + "`([^`]*)`" + `)`)

// JSNameSep joins describe titles and the test title — jest's own
// separator in its reporters.
const JSNameSep = " › "

// ParseJSTests returns it()/test() calls of a jest/vitest file. Names are
// the enclosing describe() titles plus the test title joined by JSNameSep.
// Nesting is inferred from indentation, which holds for formatted code.
func ParseJSTests(path, content string) []*Case {
	type block struct {
		title  string
		indent int
	}
	var (
		out   []*Case
		stack []block
	)
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		m := jsCallRe.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		kind := line[m[2]:m[3]]
		title := ""
		for g := 2; g <= 4; g++ {
			if m[2*g] >= 0 {
				title = line[m[2*g]:m[2*g+1]]
				break
			}
		}
		if kind == "describe" {
			stack = append(stack, block{title: title, indent: indent})
			continue
		}
		parts := make([]string, 0, len(stack)+1)
		for _, b := range stack {
			parts = append(parts, b.title)
		}
		parts = append(parts, title)
		out = append(out, &Case{Name: strings.Join(parts, JSNameSep), File: path, Line: i + 1})
	}
	return out
}

// ParseFile dispatches to the parser for fw.
func ParseFile(fw Framework, path, content string) []*Case {
	switch fw {
	case FrameworkGo:
		return ParseGoTests(path, content)
	case FrameworkPytest:
		return ParsePytest(path, content)
	case FrameworkCargo:
		return ParseRustTests(path, content)
	case FrameworkJest, FrameworkVitest:
		return ParseJSTests(path, content)
	}
	return nil
}

// ── Workspace walk ───────────────────────────────────────────────────────

// skipDirs are never descended into.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true,
	"build": true, "__pycache__": true, "venv": true, "env": true,
	"coverage": true, "out": true,
}

const (
	maxScanFiles = 20000   // walk budget
	maxFileBytes = 2 << 20 // skip huge generated files
)

// GoModulePath reads the `module` line of root/go.mod ("" when missing).
func GoModulePath(root string) string {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(ln, "module ") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(ln, "module")), `"`)
		}
	}
	return ""
}

// GoImportPath maps a package directory (relative, slash-separated) to its
// import path under module.
func GoImportPath(module, relDir string) string {
	if relDir == "." || relDir == "" {
		return module
	}
	if module == "" {
		return relDir
	}
	return module + "/" + relDir
}

// Discover walks root and returns the test tree for fw, packages sorted by
// name and files by path. Hidden directories, dependency/build dirs and
// (for Go) testdata are skipped.
func Discover(root string, fw Framework) ([]*Package, error) {
	if fw == FrameworkNone {
		return nil, nil
	}
	module := ""
	if fw == FrameworkGo {
		module = GoModulePath(root)
	}
	byDir := map[string]*Package{}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip, keep walking
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if strings.HasPrefix(name, ".") || skipDirs[name] || (fw == FrameworkGo && name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !IsTestFile(fw, path) {
			return nil
		}
		scanned++
		if scanned > maxScanFiles {
			return filepath.SkipAll
		}
		if info, err := d.Info(); err != nil || info.Size() > maxFileBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		cases := ParseFile(fw, path, string(data))
		if len(cases) == 0 {
			return nil
		}
		dir := filepath.Dir(path)
		pkg := byDir[dir]
		if pkg == nil {
			relDir, _ := filepath.Rel(root, dir)
			relDir = filepath.ToSlash(relDir)
			pkg = &Package{Name: relDir, Dir: dir}
			if fw == FrameworkGo {
				pkg.Name = GoImportPath(module, relDir)
			}
			byDir[dir] = pkg
		}
		rel, _ := filepath.Rel(root, path)
		pkg.Files = append(pkg.Files, &File{Path: path, Rel: filepath.ToSlash(rel), Cases: cases})
		return nil
	})
	pkgs := make([]*Package, 0, len(byDir))
	for _, p := range byDir {
		sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
		pkgs = append(pkgs, p)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs, err
}

// CarryOver copies results from an older tree into a freshly discovered
// one, matched by Case.Key, so a Refresh keeps the last run's statuses.
func CarryOver(old, fresh []*Package) {
	prev := map[string]*Case{}
	for _, p := range old {
		for _, f := range p.Files {
			for _, c := range f.Cases {
				prev[c.Key()] = c
			}
		}
	}
	for _, p := range fresh {
		for _, f := range p.Files {
			for _, c := range f.Cases {
				if o, ok := prev[c.Key()]; ok {
					c.Status, c.Duration, c.Message = o.Status, o.Duration, o.Message
					c.FailFile, c.FailLine, c.Output = o.FailFile, o.FailLine, o.Output
					if c.Status == StatusRunning {
						c.Status = StatusPending
					}
				}
			}
		}
	}
}

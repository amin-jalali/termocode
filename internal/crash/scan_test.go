package crash

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// bareGoAllowed lists files allowed to start goroutines with a bare `go`
// statement, and how many (none now). A panic in such a goroutine ends
// the whole process: the UI dies without its hot-exit flush. Use
// crash.Go / crash.GoWith (or defer crash.Guard) instead.
var bareGoAllowed = map[string]int{}

// bareGo matches `go f(…)`, `go x.f(…)` and `go func…`.
var bareGo = regexp.MustCompile(`^\s*go\s+(func\b|[A-Za-z_][A-Za-z0-9_.]*\()`)

// TestNoBareGoroutines: every goroutine of termocode recovers its panics
// (crash.Go / crash.Guard), except the counted ones above.
func TestNoBareGoroutines(t *testing.T) {
	root := filepath.Join("..", "..")
	found := map[string]int{}
	lines := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case "third_party", "e2e", ".git", ".claude", "node_modules", "docs-local",
				"testkit", "devtools", "mcptest", "dbtest", "extproctest": // test helpers
				return filepath.SkipDir
			}
			if rel == "internal/crash" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if bareGo.MatchString(line) {
				found[rel]++
				lines[rel] = append(lines[rel], strings.TrimSpace(line)+"  (line "+strconv.Itoa(i+1)+")")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for f := range found {
		files = append(files, f)
	}
	for f := range bareGoAllowed {
		if _, ok := found[f]; !ok {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	for _, f := range files {
		switch got, want := found[f], bareGoAllowed[f]; {
		case got > want:
			t.Errorf("%s: %d bare go statements (allowed %d); use crash.Go:\n  %s", f, got, want, strings.Join(lines[f], "\n  "))
		case got < want:
			t.Errorf("%s: %d bare go statements, the list allows %d: lower it in bareGoAllowed", f, got, want)
		}
	}
}

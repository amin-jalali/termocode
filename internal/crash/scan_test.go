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

// bareGoAllowed lists the files that still start goroutines with a bare
// `go` statement, and how many. A panic in such a goroutine ends the
// whole process: the UI dies without its hot-exit flush. New code uses
// crash.Go (or defers crash.Guard); this list only shrinks.
var bareGoAllowed = map[string]int{
	"cmd/termocode/main.go":            1,
	"cmd/termocode/mcp_relay.go":       2,
	"cmd/termocode/session_unix.go":    1,
	"internal/agentrun/run.go":         2,
	"internal/ai/anthropic.go":         1,
	"internal/ai/openai.go":            1,
	"internal/clilogin/flow.go":        3,
	"internal/clilogin/session.go":     3,
	"internal/clistep/clistep.go":      1,
	"internal/clistep/diff.go":         1,
	"internal/clistep/status.go":       1,
	"internal/containers/engine.go":    1,
	"internal/dbclient/sqlitecli.go":   1,
	"internal/dbclient/sqlsession.go":  1,
	"internal/dbclient/tunnel.go":      2,
	"internal/nerdterm/nerdterm.go":    3,
	"internal/search/search.go":        1,
	"internal/secrets/keyring.go":      1,
	"internal/services/events.go":      1,
	"internal/services/start_linux.go": 1,
	"internal/services/start_other.go": 1,
	"internal/transfer/kitty.go":       1,
}

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

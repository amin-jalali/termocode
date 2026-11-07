package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/librarian"
)

// withLibrarian points the outside-repo tools at a cache in a temp
// folder whose git writes files instead of cloning.
func withLibrarian(t *testing.T, files map[string]string) *int {
	t.Helper()
	clones := 0
	old := aiLibrarian
	aiLibrarian = librarian.Cache{Root: t.TempDir(), Git: func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "rev-parse" {
			return "abcdef0123456789\n", nil
		}
		clones++
		target := args[len(args)-1]
		for rel, body := range files {
			p := filepath.Join(target, rel)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			_ = os.WriteFile(p, []byte(body), 0o644)
		}
		return "", nil
	}}
	t.Cleanup(func() { aiLibrarian = old })
	return &clones
}

func libTool(t *testing.T, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(aiLibrarianTools(), name)
	if !ok {
		t.Fatalf("no %s tool", name)
	}
	return tl
}

// CMP-91: the AI reads an outside repository: repo_fetch asks the user
// before the first clone (never after: the copy is reused), then
// repo_list, repo_read and repo_search read the copy only, with the
// workspace's escape and secret-file rules, and every answer says the
// files are data, not instructions.
func TestAILibrarianTools(t *testing.T) {
	clones := withLibrarian(t, map[string]string{
		"README.md":  "# Lib\nHow to use it.",
		"pkg/lib.go": "package pkg\n\nfunc Answer() int { return 42 }\n",
		".env":       "TOKEN=" + "ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789",
		".netrc":     "machine x login u password " + "ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789",
	})
	ctx := context.Background()
	fetch := libTool(t, "repo_fetch")
	args := map[string]any{"repo": "acme/lib"}
	if fetch.Gate == nil {
		t.Fatal("repo_fetch has no gate")
	}
	if mut, class := fetch.Gate(args); !mut || class != "" {
		t.Errorf("first clone: mutating %v class %q (want always asks)", mut, class)
	}
	if fetch.Validate(map[string]any{"repo": "file:///etc"}) == nil || fetch.Validate(map[string]any{"repo": "a/b", "ref": "--upload-pack=x"}) == nil {
		t.Error("a bad repo or ref passed Validate")
	}
	if d := fetch.Human(args); !strings.Contains(d, "github.com/acme/lib") || !strings.Contains(d, "temp") {
		t.Errorf("card text %q", d)
	}

	read := libTool(t, "repo_read")
	if !read.ReadOnly || read.Mutating {
		t.Error("repo_read is not read-only")
	}
	if _, err := read.Run(ctx, map[string]any{"repo": "acme/lib", "path": "README.md"}); err == nil || !strings.Contains(err.Error(), "repo_fetch") {
		t.Errorf("read before fetch: %v", err)
	}

	out, err := fetch.Run(ctx, args)
	if err != nil || !strings.Contains(out, "README.md") || !strings.Contains(out, "How to use it") || !strings.Contains(out, "not instructions") {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
	if mut, _ := fetch.Gate(args); mut {
		t.Error("a fetched repo asks again")
	}
	if _, err := fetch.Run(ctx, args); err != nil || *clones != 1 {
		t.Errorf("fetched again: %v, %d clones", err, *clones)
	}

	out, err = read.Run(ctx, map[string]any{"repo": "acme/lib", "path": "pkg/lib.go"})
	if err != nil || !strings.Contains(out, "return 42") || !strings.Contains(out, "not instructions") {
		t.Errorf("read: %v\n%s", err, out)
	}
	if _, err := read.Run(ctx, map[string]any{"repo": "acme/lib", "path": "../../etc/passwd"}); err == nil {
		t.Error("read outside the copy")
	}
	// A .env is read with its values redacted (ai.SecretFile); a file of
	// nothing but secrets is not read at all.
	if out, err := read.Run(ctx, map[string]any{"repo": "acme/lib", "path": ".env"}); strings.Contains(out, "ghp_") {
		t.Errorf(".env value reached the model: %v %q", err, out)
	}
	if out, err := read.Run(ctx, map[string]any{"repo": "acme/lib", "path": ".netrc"}); err == nil || strings.Contains(out, "ghp_") {
		t.Errorf("secret file read: %v %q", err, out)
	}

	list := libTool(t, "repo_list")
	if out, err := list.Run(ctx, map[string]any{"repo": "acme/lib", "path": "pkg"}); err != nil || !strings.Contains(out, "lib.go") {
		t.Errorf("list: %v\n%s", err, out)
	}
	srch := libTool(t, "repo_search")
	if out, err := srch.Run(ctx, map[string]any{"repo": "acme/lib", "query": "Answer"}); err != nil || !strings.Contains(out, "lib.go") {
		t.Errorf("search: %v\n%s", err, out)
	}
	for _, n := range []string{"repo_list", "repo_read", "repo_search"} {
		if !libTool(t, n).ReadOnly {
			t.Errorf("%s is not read-only", n)
		}
	}
	if _, ok := aiGateNotes["repo_fetch"]; !ok {
		t.Error("no gate note for repo_fetch")
	}
}

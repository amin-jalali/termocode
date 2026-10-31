package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRule(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec (OMX-9): CLAUDE.md in the root is read like AGENTS.md; a
// .claude/rules file with alwaysApply (or no front matter) joins the
// prompt text; one with globs and every AGENTS.md / CLAUDE.md below the
// root are scoped rules. All of them are in the one trust hash.
func TestLoadProjectRulesScoped(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // the Go walker
	root := t.TempDir()
	writeRule(t, root, "CLAUDE.md", "root claude\n")
	writeRule(t, root, ".claude/rules/always.md", "---\nalwaysApply: true\n---\nalways rule\n")
	writeRule(t, root, ".claude/rules/plain.md", "plain rule\n")
	writeRule(t, root, ".claude/rules/go.mdc", "---\ndescription: Go\nglobs: \"*.go, cmd/**\"\n---\ngo rule\n")
	writeRule(t, root, ".claude/rules/web.md", "---\npaths:\n  - web/**/*.ts\n---\nweb rule\n")
	writeRule(t, root, "sub/AGENTS.md", "sub agents\n")
	writeRule(t, root, "sub/deep/CLAUDE.md", "deep claude\n")

	r := LoadProjectRules(root)
	for _, s := range []string{"# CLAUDE.md\nroot claude", "# .claude/rules/always.md\nalways rule", "# .claude/rules/plain.md\nplain rule"} {
		if !strings.Contains(r.Text, s) {
			t.Errorf("prompt text lacks %q:\n%s", s, r.Text)
		}
	}
	if strings.Contains(r.Text, "go rule") || strings.Contains(r.Text, "sub agents") || strings.Contains(r.Text, "alwaysApply") {
		t.Errorf("scoped or front matter text in the prompt:\n%s", r.Text)
	}
	if len(r.Scoped) != 4 {
		t.Fatalf("scoped %+v", r.Scoped)
	}
	files := func(rel string) string {
		var out []string
		for _, s := range r.ScopedFor(rel) {
			out = append(out, s.File)
		}
		return strings.Join(out, ",")
	}
	for rel, want := range map[string]string{
		"main.go":            ".claude/rules/go.mdc",
		"sub/x.txt":          "sub/AGENTS.md",
		"sub/deep/y.go":      ".claude/rules/go.mdc,sub/AGENTS.md,sub/deep/CLAUDE.md",
		"web/app/z.ts":       ".claude/rules/web.md",
		"cmd/tool/README.md": ".claude/rules/go.mdc",
		"subway/a.txt":       "",
	} {
		if got := files(rel); got != want {
			t.Errorf("ScopedFor(%s) = %q, want %q", rel, got, want)
		}
	}
	h := r.Hash
	writeRule(t, root, "sub/AGENTS.md", "sub agents 2\n")
	if LoadProjectRules(root).Hash == h {
		t.Error("a changed folder rule kept the trust hash")
	}
	if !strings.Contains(strings.Join(r.Files, " "), "sub/AGENTS.md") {
		t.Errorf("files %v lack the scoped rules (the card lists them)", r.Files)
	}
}

func TestLoadProjectRulesOnlyScopedHasHash(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	writeRule(t, root, "pkg/AGENTS.md", "pkg rule\n")
	r := LoadProjectRules(root)
	if r.Text != "" || r.Hash == "" || r.Preview() != "# pkg/AGENTS.md\npkg rule" {
		t.Errorf("rules %+v preview %q", r, r.Preview())
	}
}

// Spec (OMX-9): a scoped rule is added once per chat, and the added
// text is capped.
func TestRulesInjectorOncePerChatAndCapped(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	writeRule(t, root, "a/AGENTS.md", "rule a\n")
	writeRule(t, root, "b/AGENTS.md", strings.Repeat("b", RulesMaxBytes)+"\n")
	r := LoadProjectRules(root)
	var j RulesInjector
	first := j.Inject(r, "a/x.go")
	if !strings.Contains(first, "a/AGENTS.md") || !strings.Contains(first, "rule a") {
		t.Fatalf("first read = %q", first)
	}
	if again := j.Inject(r, "a/y.go"); again != "" {
		t.Errorf("second read added the rule again: %q", again)
	}
	big := j.Inject(r, "b/x.go")
	if len(big) > RulesScopedMaxBytes+300 || !strings.Contains(big, "cut") {
		t.Errorf("big rule not capped: %d bytes", len(big))
	}
	if j.Inject(Rules{}, "a/x.go") != "" {
		t.Error("no rules, but text")
	}
}

package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (OM-1): AGENTS.md and .termocode/rules.md from the workspace root
// are read for the system prompt, capped at 16 KB (cut at a line end),
// with escape, control and bidi characters removed.
func TestLoadProjectRules(t *testing.T) {
	write := func(t *testing.T, root, name, text string) {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no files", func(t *testing.T) {
		r := LoadProjectRules(t.TempDir())
		if r.Text != "" || len(r.Files) != 0 || r.Hash != "" {
			t.Errorf("rules %+v", r)
		}
	})

	t.Run("AGENTS.md only", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "AGENTS.md", "Use tabs.\nRun go test.\n")
		r := LoadProjectRules(root)
		if len(r.Files) != 1 || r.Files[0] != "AGENTS.md" || r.Text != "Use tabs.\nRun go test." || r.Truncated {
			t.Errorf("rules %+v", r)
		}
		if r.Bytes != len("Use tabs.\nRun go test.\n") || r.Hash == "" {
			t.Errorf("bytes %d hash %q", r.Bytes, r.Hash)
		}
	})

	t.Run("both files with headers", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "AGENTS.md", "one\n")
		write(t, root, ".termocode/rules.md", "two\n")
		r := LoadProjectRules(root)
		if len(r.Files) != 2 || r.Files[1] != ".termocode/rules.md" {
			t.Fatalf("files %v", r.Files)
		}
		if r.Text != "# AGENTS.md\none\n\n# .termocode/rules.md\ntwo" {
			t.Errorf("text %q", r.Text)
		}
	})

	t.Run("an empty file is no rules", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "AGENTS.md", " \n\n")
		if r := LoadProjectRules(root); r.Text != "" || len(r.Files) != 0 {
			t.Errorf("rules %+v", r)
		}
	})

	t.Run("a big file is cut at a line end", func(t *testing.T) {
		root := t.TempDir()
		line := strings.Repeat("x", 99) + "\n"
		write(t, root, "AGENTS.md", strings.Repeat(line, 200)) // 20 KB
		r := LoadProjectRules(root)
		if !r.Truncated || !strings.HasSuffix(r.Text, "\n(cut at 16 KB)") {
			t.Fatalf("truncated %v, tail %q", r.Truncated, r.Text[len(r.Text)-30:])
		}
		body := strings.TrimSuffix(r.Text, "\n(cut at 16 KB)")
		if len(body) > RulesMaxBytes {
			t.Errorf("body %d bytes > %d", len(body), RulesMaxBytes)
		}
		for _, l := range strings.Split(body, "\n") {
			if len(l) != 99 {
				t.Fatalf("a line was cut in the middle: %d bytes", len(l))
			}
		}
	})

	t.Run("escape, control and bidi characters are stripped", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "AGENTS.md", "a\x1b[2Jb\x07c‮d⁦e\tf\r\ng\n")
		r := LoadProjectRules(root)
		if r.Text != "abcde\tf\ng" {
			t.Errorf("text %q", r.Text)
		}
	})

	t.Run("same text, same hash", func(t *testing.T) {
		a, b := t.TempDir(), t.TempDir()
		write(t, a, "AGENTS.md", "rule\n")
		write(t, b, "AGENTS.md", "rule\n")
		ra, rb := LoadProjectRules(a), LoadProjectRules(b)
		if ra.Hash != rb.Hash {
			t.Error("same text, different hash")
		}
		write(t, b, "AGENTS.md", "rule 2\n")
		if LoadProjectRules(b).Hash == ra.Hash {
			t.Error("changed text, same hash")
		}
	})

	t.Run("a symlink out of the root is ignored", func(t *testing.T) {
		root, outside := t.TempDir(), t.TempDir()
		write(t, outside, "secret.md", "steal\n")
		if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "AGENTS.md")); err != nil {
			t.Skip(err)
		}
		if r := LoadProjectRules(root); r.Text != "" {
			t.Errorf("followed a symlink out of the root: %+v", r)
		}
	})

	t.Run("a symlink inside the root is read", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "docs/agents.md", "inside\n")
		if err := os.Symlink(filepath.Join(root, "docs", "agents.md"), filepath.Join(root, "AGENTS.md")); err != nil {
			t.Skip(err)
		}
		if r := LoadProjectRules(root); r.Text != "inside" {
			t.Errorf("rules %+v", r)
		}
	})
}

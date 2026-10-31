package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilePromptsReloadAndTrust(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "prompt.md")
	if err := os.WriteFile(path, []byte("First prompt"), 0600); err != nil {
		t.Fatal(err)
	}
	p := Profile{Name: "test", Source: ProfileSourceUser, Steps: []ProfileStep{{ID: "one", System: "file://prompt.md"}}, Loop: &ProfileLoop{Fix: ProfileStep{ID: "fix", System: "file://prompt.md"}}}
	first, err := ResolveFilePrompts(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != ProfileSourceProject || first.Steps[0].System != "First prompt" || first.Loop.Fix.System != "First prompt" {
		t.Fatal("file not loaded as untrusted repo text")
	}
	if p.Steps[0].System != "file://prompt.md" || p.Loop.Fix.System != "file://prompt.md" {
		t.Fatal("source profile mutated")
	}
	os.WriteFile(path, []byte("Edited prompt"), 0600)
	second, err := ResolveFilePrompts(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if ProfileHash(first) == ProfileHash(second) {
		t.Fatal("file edit did not change trust hash")
	}
}

func TestFilePromptsRejectEscapeAndLargeFiles(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	secret := filepath.Join(out, "secret.md")
	os.WriteFile(secret, []byte("private"), 0600)
	os.Symlink(secret, filepath.Join(root, "escape.md"))
	os.WriteFile(filepath.Join(root, "large.md"), []byte(strings.Repeat("x", MaxPromptBytes+1)), 0600)
	for _, path := range []string{"../outside.md", secret, "escape.md", "missing.md", "large.md"} {
		p := Profile{Steps: []ProfileStep{{ID: "one", System: "file://" + path}}}
		if _, err := ResolveFilePrompts(root, p); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
}

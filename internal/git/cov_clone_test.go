package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanCRSplitsGitProgress(t *testing.T) {
	adv, tok, err := scanCR([]byte("last line"), true)
	if adv != 9 || string(tok) != "last line" || err != nil {
		t.Errorf("at EOF = %d %q %v", adv, tok, err)
	}
	if adv, tok, _ := scanCR([]byte("partial"), false); adv != 0 || tok != nil {
		t.Errorf("a partial line asks for more, got %d %q", adv, tok)
	}
	if adv, tok, _ := scanCR([]byte("a\rb"), false); adv != 2 || string(tok) != "a" {
		t.Errorf("CR splits, got %d %q", adv, tok)
	}
	if adv, tok, _ := scanCR(nil, true); adv != 0 || tok != nil {
		t.Errorf("end of input = %d %q", adv, tok)
	}
}

func TestCloneDestAndFolderErrors(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckCloneDest(file); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("a file as dest = %v", err)
	}
	// A path under a file cannot be looked at: the error is git's reason, not "missing".
	if err := CheckCloneDest(filepath.Join(file, "x")); err == nil {
		t.Error("a path under a file: want error")
	}
	if err := Clone(CloneOptions{URL: " ", Dest: filepath.Join(root, "d")}); err == nil {
		t.Error("an empty URL: want error")
	}
	if err := Clone(CloneOptions{URL: "/nowhere", Dest: filepath.Join(file, "sub", "d")}); err == nil {
		t.Error("a dest under a file: want error")
	}
	readOnlyRoot(t)
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if err := CheckCloneDest(filepath.Join(locked, "d")); err == nil {
		t.Error("an unreadable parent: want error")
	}
	if err := CheckCloneDest(locked); err == nil {
		t.Error("an unreadable empty folder: want error")
	}
}

func TestCloneWithTokenNeedsATempFolder(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	err := Clone(CloneOptions{URL: "https://example.invalid/x.git", Dest: filepath.Join(t.TempDir(), "d"), Token: "t"})
	if err == nil {
		t.Error("no temp folder for the askpass helper: want error before any network work")
	}
}

func TestRunUndoRefusals(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	p, err := PlanUndo(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.dir, ".git", "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = RunUndo(r.dir, p, UndoOptions{})
	if e, ok := err.(*BusyError); !ok || e.Op == "" {
		t.Errorf("RunUndo during a rebase = %v", err)
	}
	if headOf(t, r) != h[2] {
		t.Error("a refused undo moved HEAD")
	}
}

package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (WFX-6, plan §3.7/§4.3): workflow state lives under
// $XDG_STATE_HOME/termocode/workflows/<workspace key>, never in the repo.
func TestRootIsOutsideTheRepo(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	repo := t.TempDir()
	root, err := Root(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(root, filepath.Join(state, "termocode", "workflows")+string(filepath.Separator)) {
		t.Fatalf("root %q is not under the state dir", root)
	}
	if strings.HasPrefix(root, repo) {
		t.Fatalf("root %q is inside the repo", root)
	}
	other, _ := Root(t.TempDir())
	if other == root {
		t.Fatal("two workspaces share one state dir")
	}
}

// Spec: a workflow name or run id never escapes the state dir.
func TestNamesCannotEscape(t *testing.T) {
	for _, bad := range []string{"", "..", "../x", "a/b", `a\b`, ".hidden", strings.Repeat("a", 65)} {
		if err := checkName(bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	for _, good := range []string{"weekday-bugs", "a", "Run_2026-11-06T09.00"} {
		if err := checkName(good); err != nil {
			t.Errorf("name %q refused: %v", good, err)
		}
	}
}

// Spec: a write is atomic (temp file + rename) and private (0600).
func TestWriteAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	p := filepath.Join(dir, "x.json")
	if err := writeAtomic(p, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(p, []byte("two")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "two" {
		t.Fatalf("got %q", b)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("temp files left: %v", ents)
	}
}

// Spec: the file hash is the sha256 of the file bytes.
func TestFileHash(t *testing.T) {
	if got := FileHash([]byte("abc")); got != "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal(got)
	}
}

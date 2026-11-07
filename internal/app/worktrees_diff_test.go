package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/textdiff"
)

// wtDiffRepo is a lane worktree that left main: one committed edit, one
// uncommitted edit and one new file. It returns the lane folder.
func wtDiffRepo(t *testing.T) string {
	t.Helper()
	main := wtRepo(t)
	lane := filepath.Join(filepath.Dir(main), "app.worktrees", "x")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = lane
		cmd.Env = append(cmd.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(lane, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// the files live on main first, so the lane's base has them
	mainRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = main
		cmd.Env = append(cmd.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(main, "a.txt"), []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainRun("add", "a.txt")
	mainRun("commit", "-q", "-m", "a")
	run("merge", "-q", "--ff-only", "main")
	write("a.txt", "ONE\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	run("commit", "-q", "-am", "lane edit")
	write("a.txt", "ONE\n2\n3\n4\n5\n6\n7\n8\n9\nTEN\n")
	write("new.txt", "made by the agent\n")
	return lane
}

// The diff vs base of a worktree lists every file the lane changed since
// it left main, the old side at the merge base and the new side on disk.
func TestWorktreeDiffLoadsChangesSinceBase(t *testing.T) {
	lane := wtDiffRepo(t)
	msg, ok := wtDiffLoad(lane, "main", "x")().(wtDiffReadyMsg)
	if !ok {
		t.Fatal("not a diff result")
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.base != "main" || len(msg.files) != 2 {
		t.Fatalf("base %q files %+v", msg.base, msg.files)
	}
	a, n := msg.files[0], msg.files[1]
	if a.rel != "a.txt" || !strings.HasPrefix(a.old, "1\n") || !strings.HasPrefix(a.new, "ONE\n") || !a.atBase || !a.exists {
		t.Errorf("a.txt: %+v", a)
	}
	if n.rel != "new.txt" || n.atBase || !n.exists || n.new != "made by the agent\n" {
		t.Errorf("new.txt: %+v", n)
	}
}

// Rejecting one hunk writes the base text back for that hunk only; the
// accepted hunk stays as the agent left it.
func TestWorktreeDiffRevertsRejectedHunks(t *testing.T) {
	lane := wtDiffRepo(t)
	msg := wtDiffLoad(lane, "main", "x")().(wtDiffReadyMsg)
	m := wtTestModel(t, lane)
	f := msg.files[0]
	it := wtDiffItem(f)
	d := textdiff.Compute(f.old, f.new)
	if len(d.Hunks) != 2 {
		t.Fatalf("%d hunks, want 2", len(d.Hunks))
	}
	sum, _ := it.finish(m, d, map[int]bool{0: true}, false)
	got, _ := os.ReadFile(f.abs)
	if string(got) != "ONE\n2\n3\n4\n5\n6\n7\n8\n9\n10\n" {
		t.Errorf("after revert: %q", got)
	}
	if !strings.Contains(sum, "Reverted 1 of 2") {
		t.Errorf("summary %q", sum)
	}
}

// Keeping every hunk writes nothing; rejecting a new file removes it.
func TestWorktreeDiffKeepAndRemoveNewFile(t *testing.T) {
	lane := wtDiffRepo(t)
	msg := wtDiffLoad(lane, "main", "x")().(wtDiffReadyMsg)
	m := wtTestModel(t, lane)
	a := msg.files[0]
	d := textdiff.Compute(a.old, a.new)
	if sum, _ := wtDiffItem(a).finish(m, d, map[int]bool{0: true, 1: true}, false); sum != "Kept" {
		t.Errorf("keep all: %q", sum)
	}
	if got, _ := os.ReadFile(a.abs); string(got) != a.new {
		t.Errorf("keep all changed the file: %q", got)
	}
	n := msg.files[1]
	if sum, _ := wtDiffItem(n).finish(m, textdiff.Compute(n.old, n.new), map[int]bool{}, false); !strings.Contains(sum, "removed") {
		t.Errorf("reject new file: %q", sum)
	}
	if _, err := os.Stat(n.abs); !os.IsNotExist(err) {
		t.Errorf("the new file is still there: %v", err)
	}
}

// The agent may still be working: a file that changed after the diff was
// read is never overwritten.
func TestWorktreeDiffRefusesStaleFile(t *testing.T) {
	lane := wtDiffRepo(t)
	msg := wtDiffLoad(lane, "main", "x")().(wtDiffReadyMsg)
	m := wtTestModel(t, lane)
	f := msg.files[0]
	if err := os.WriteFile(f.abs, []byte("the agent wrote again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, _ := wtDiffItem(f).finish(m, textdiff.Compute(f.old, f.new), map[int]bool{}, false)
	if !strings.Contains(sum, "changed") {
		t.Errorf("summary %q", sum)
	}
	if got, _ := os.ReadFile(f.abs); string(got) != "the agent wrote again\n" {
		t.Errorf("a stale file was overwritten: %q", got)
	}
}

// d is on the key card and the palette has the command.
func TestWorktreeDiffKeyAndPalette(t *testing.T) {
	found := false
	for _, r := range worktreeKeyRows() {
		if r.Key == "d" && strings.Contains(r.What, "base") {
			found = true
		}
	}
	if !found {
		t.Error("no d row on the key card")
	}
	found = false
	for _, it := range worktreePaletteItems() {
		if it.ID == "worktree-diff" {
			found = true
		}
	}
	if !found {
		t.Error("no palette row")
	}
}

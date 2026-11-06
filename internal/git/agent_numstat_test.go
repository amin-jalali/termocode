package git

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Spec (AGX-5): the race compare view shows, per lane, the files the
// agent changed with their added / deleted lines. AgentNumstat reads them
// in one git call after the snapshot (the agent's work is then safe on
// its branch); a binary file counts as changed with no line numbers.
func TestAgentNumstat(t *testing.T) {
	r := committedRepo(t)
	w, err := CreateAgentWorktreeFromHead(r.dir, "race-1-1-claude")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(w.Path, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "one\nTWO\nthree\nfour\n") // 1 changed + 1 added: +2 -1
	write("new.txt", "x\ny\n")                 // +2
	write("bin.dat", "\x00\x01\x02")           // binary
	if err := os.Remove(filepath.Join(w.Path, "b.txt")); err != nil {
		t.Fatal(err)
	}
	got, err := AgentNumstat(r.dir, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, s := range got {
		b.WriteString(s.Status + " " + s.Path)
		if s.Binary {
			b.WriteString(" binary\n")
			continue
		}
		b.WriteString(" +" + strconv.Itoa(s.Add) + " -" + strconv.Itoa(s.Del) + "\n")
	}
	want := "M a.txt +2 -1\nD b.txt +0 -1\nA bin.dat binary\nA new.txt +2 -0\n"
	if b.String() != want {
		t.Errorf("numstat:\n%s\nwant:\n%s", b.String(), want)
	}
	// Nothing changed: no rows, no error.
	w2, err := CreateAgentWorktreeFromHead(r.dir, "race-1-2-codex")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := AgentNumstat(r.dir, w2.ID); err != nil || len(got) != 0 {
		t.Errorf("clean lane: %v %v", got, err)
	}
	if _, err := AgentNumstat(r.dir, "race-9-9-none"); err == nil {
		t.Error("an unknown lane gave no error")
	}
}

// Spec (AGX-4): the race records live next to the lanes' worktrees, in
// the repository's git dir (<git-common-dir>/termocode), never in the
// project; from a linked worktree it is the same folder.
func TestAgentStateDir(t *testing.T) {
	r := committedRepo(t)
	d, err := AgentStateDir(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(d), "/.git/termocode") {
		t.Errorf("state dir %q", d)
	}
	w, err := CreateAgentWorktreeFromHead(r.dir, "race-1-1-x")
	if err != nil {
		t.Fatal(err)
	}
	if d2, err := AgentStateDir(w.Path); err != nil || d2 != d {
		t.Errorf("from the lane: %q %v, want %q", d2, err, d)
	}
	if _, err := AgentStateDir(t.TempDir()); err == nil {
		t.Error("no error outside a repository")
	}
}

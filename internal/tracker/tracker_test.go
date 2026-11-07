package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

// CMP-81: Add makes "0001-fix-the-login.md" with a header; List reads
// it back; Claim, Note and SetStatus rewrite it.
func TestAddListClaimNote(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tasks")
	a, err := Add(dir, "Fix the login\x1b[31m!", "Users see a blank page.", t0)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || filepath.Base(a.File) != "0001-fix-the-login-31m.md" {
		t.Errorf("first task %d %s", a.ID, a.File)
	}
	if strings.ContainsRune(a.Title, '\x1b') {
		t.Errorf("title %q", a.Title)
	}
	b, err := Add(dir, "Dark mode", "", t0)
	if err != nil || b.ID != 2 {
		t.Fatalf("second %+v %v", b, err)
	}
	if _, err := Add(dir, " \n ", "", t0); err == nil {
		t.Error("a task without a title")
	}
	ts, err := List(dir)
	if err != nil || len(ts) != 2 || ts[0].Title != a.Title || ts[0].Status != Todo || ts[0].Body != "Users see a blank page." {
		t.Fatalf("list %+v %v", ts, err)
	}
	c, err := Claim(dir, 1, "claude", "/src/app.worktrees/fix", t0.Add(time.Minute))
	if err != nil || c.Status != Doing || c.Owner != "claude" || c.Worktree != "/src/app.worktrees/fix" || !c.Updated.After(c.Created) {
		t.Fatalf("claim %+v %v", c, err)
	}
	if _, err := Claim(dir, 1, "codex", "", t0); err == nil || !strings.Contains(err.Error(), "taken by claude") {
		t.Errorf("a doing task was taken again: %v", err)
	}
	if _, err := Claim(dir, 1, "claude", "", t0); err != nil {
		t.Errorf("the owner claims again: %v", err)
	}
	n, err := Note(dir, 1, "claude", "found it\nin auth.go", t0)
	if err != nil || !strings.Contains(n.Body, "claude: found it in auth.go") || !strings.HasPrefix(n.Body, "Users see a blank page.") {
		t.Fatalf("note %q %v", n.Body, err)
	}
	if _, err := SetStatus(dir, 1, "waiting", "", "", t0); err == nil {
		t.Error("a bad status was set")
	}
	d, err := SetStatus(dir, 1, Done, "", "", t0)
	if err != nil || d.Status != Done {
		t.Fatalf("done %+v %v", d, err)
	}
	if _, err := Claim(dir, 1, "x", "", t0); err == nil {
		t.Error("a done task was claimed")
	}
	if _, err := Find(dir, 9); err == nil {
		t.Error("no error for a missing task")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".task-*"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

// A hand-made file is read; bad, big and non-Markdown files are
// skipped; an unknown status reads as todo; a missing folder is empty.
func TestListSkipsBadFiles(t *testing.T) {
	if ts, err := List(filepath.Join(t.TempDir(), "none")); err != nil || ts != nil {
		t.Fatalf("missing folder: %v %v", ts, err)
	}
	dir := t.TempDir()
	w := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("0003-hand.md", "---\nid: 3\ntitle: By hand\nstatus: Later\n---\ntext\n")
	w("0004-nohead.md", "just text\n")
	w("0005-noid.md", "---\ntitle: x\n---\n")
	w("0006-big.md", "---\nid: 6\ntitle: big\n---\n"+strings.Repeat("x", MaxFileBytes))
	w("notes.txt", "---\nid: 7\ntitle: txt\n---\n")
	ts, err := List(dir)
	if err != nil || len(ts) != 1 || ts[0].ID != 3 || ts[0].Status != Todo || ts[0].Body != "text" {
		t.Fatalf("list %+v %v", ts, err)
	}
	// The next id follows the highest one.
	a, err := Add(dir, "next", "", t0)
	if err != nil || a.ID != 4 {
		t.Errorf("next id %d %v", a.ID, err)
	}
}

// Format and Parse round-trip; header values stay one line.
func TestFormatParse(t *testing.T) {
	in := Task{ID: 12, Title: "a\nstatus: done", Status: Blocked, Owner: "me", Created: t0, Updated: t0, Body: "x\ny"}
	out, err := Parse(Format(in))
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != 12 || out.Status != Blocked || out.Title != "a status: done" || out.Owner != "me" || out.Body != "x\ny" || !out.Created.Equal(t0) {
		t.Errorf("round trip %+v", out)
	}
}

// Agents adding at the same time never share an id.
func TestAddRace(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	ids := make(chan int, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tk, err := Add(dir, "same", "", t0); err == nil {
				ids <- tk.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[int]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("id %d twice", id)
		}
		seen[id] = true
	}
	if len(seen) != 20 {
		t.Errorf("%d tasks, want 20", len(seen))
	}
}

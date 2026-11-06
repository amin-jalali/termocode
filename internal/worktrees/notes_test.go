package worktrees

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func noRedact(s string) string { return s }

// WTX-12: a note is one line of plain text: control characters and
// newlines go, the redactor runs, empty and over-long notes are refused.
func TestCleanNote(t *testing.T) {
	redact := func(s string) string { return strings.ReplaceAll(s, "sk-"+"test123", "[redacted]") }
	for _, c := range []struct{ in, want string }{
		{"  adding the pin row  ", "adding the pin row"},
		{"two\nlines\r\there", "two lines here"},
		{"bell\x07 and \x1b[31mred\x1b[0m", "bell and [31mred[0m"},
		{"key sk-" + "test123 used", "key [redacted] used"},
	} {
		got, err := CleanNote(c.in, redact)
		if err != nil || got != c.want {
			t.Errorf("CleanNote(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := CleanNote(" \n\t ", noRedact); !errors.Is(err, ErrNoteEmpty) {
		t.Errorf("empty note: %v", err)
	}
	if _, err := CleanNote(strings.Repeat("é", MaxNoteLen), noRedact); err != nil {
		t.Errorf("a note of exactly %d characters: %v", MaxNoteLen, err)
	}
	if _, err := CleanNote(strings.Repeat("a", MaxNoteLen+1), noRedact); !errors.Is(err, ErrNoteLong) {
		t.Errorf("long note: %v", err)
	}
}

// At most MaxNotes notes are kept, the oldest dropped; each has its time.
func TestAddNoteCap(t *testing.T) {
	var m Meta
	t0 := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	for i := range MaxNotes + 7 {
		m.AddNote("note "+strconv.Itoa(i), t0.Add(time.Duration(i)*time.Minute))
	}
	if len(m.Notes) != MaxNotes {
		t.Fatalf("%d notes kept", len(m.Notes))
	}
	if m.Notes[0].Text != "note 7" || m.Notes[MaxNotes-1].Text != "note 56" {
		t.Errorf("kept %q … %q", m.Notes[0].Text, m.Notes[MaxNotes-1].Text)
	}
	if !m.Notes[MaxNotes-1].At.Equal(t0.Add(56 * time.Minute)) {
		t.Errorf("time %v", m.Notes[MaxNotes-1].At)
	}
	if last, ok := m.LastNote(); !ok || last.Text != "note 56" {
		t.Errorf("LastNote %+v %v", last, ok)
	}
	if _, ok := (Meta{}).LastNote(); ok {
		t.Error("an empty meta has a last note")
	}
}

// A note written as a plain string (an older file) still reads.
func TestNoteReadsPlainString(t *testing.T) {
	var m Meta
	if err := json.Unmarshal([]byte(`{"notes":["old one",{"text":"new one","at":"2026-11-06T10:00:00Z"}]}`), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Notes) != 2 || m.Notes[0].Text != "old one" || m.Notes[1].Text != "new one" || m.Notes[1].At.IsZero() {
		t.Errorf("notes %+v", m.Notes)
	}
}

// WTX-12: the status checkpoints an agent may set.
func TestCheckpoint(t *testing.T) {
	for _, s := range []string{CheckWorking, CheckBlocked, CheckDone} {
		if !ValidCheckpoint(s) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range []string{"", "waiting", "DONE", "rm -rf"} {
		if ValidCheckpoint(s) {
			t.Errorf("%q allowed", s)
		}
	}
	var m Meta
	at := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	if err := m.SetCheckpoint("blocked", at); err != nil || m.Checkpoint != CheckBlocked || !m.CheckpointAt.Equal(at) {
		t.Errorf("%+v %v", m, err)
	}
	if err := m.SetCheckpoint("nope", at); err == nil || m.Checkpoint != CheckBlocked {
		t.Errorf("bad checkpoint stored: %+v %v", m, err)
	}
}

// WTX-11: children follow their parent, one level deeper; a child whose
// parent is not in the list is a root; a cycle never loops and its
// members become roots.
func TestNest(t *testing.T) {
	paths := []string{"/a", "/b", "/a1", "/a1x", "/orphan", "/c1", "/c2", "/b1"}
	parents := []string{"", "", "/a", "/a1", "/gone", "/c2", "/c1", "/b"}
	order, depth := Nest(paths, parents)
	var got []string
	for i, o := range order {
		got = append(got, paths[o]+":"+strconv.Itoa(depth[i]))
	}
	want := "/a:0 /a1:1 /a1x:2 /b:0 /b1:1 /orphan:0 /c1:0 /c2:0"
	if strings.Join(got, " ") != want {
		t.Errorf("Nest = %s\nwant   %s", strings.Join(got, " "), want)
	}
	if len(order) != len(paths) {
		t.Errorf("lost rows: %v", order)
	}
	// A worktree that names itself as parent is a root.
	order, depth = Nest([]string{"/x"}, []string{"/x"})
	if len(order) != 1 || depth[0] != 0 {
		t.Errorf("self parent: %v %v", order, depth)
	}
}

// WTX-11: SetParent refuses a parent that would make a cycle.
func TestSetParentRefusesCycle(t *testing.T) {
	metas := map[string]Meta{"/b": {Parent: "/a"}, "/c": {Parent: "/b"}}
	if err := SetParent(metas, "/a", "/c"); err == nil {
		t.Error("a → c → b → a allowed")
	}
	if err := SetParent(metas, "/a", "/a"); err == nil {
		t.Error("a worktree its own parent")
	}
	if err := SetParent(metas, "/d", "/c"); err != nil || metas["/d"].Parent != "/c" {
		t.Errorf("good parent: %v %+v", err, metas["/d"])
	}
}

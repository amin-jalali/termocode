package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/debounce"
)

func writeBak(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseHotBackup(t *testing.T) {
	b, ok := parseHotBackup([]byte("/w/a.txt\none\n\nthree"))
	if !ok || b.Path != "/w/a.txt" || !reflect.DeepEqual(b.Lines, []string{"one", "", "three"}) {
		t.Errorf("got %+v %v", b, ok)
	}
	// An empty buffer is one empty line.
	if b, ok := parseHotBackup([]byte("/w/e.txt\n")); !ok || !reflect.DeepEqual(b.Lines, []string{""}) {
		t.Errorf("empty buffer: %+v %v", b, ok)
	}
	if _, ok := parseHotBackup([]byte("no newline")); ok {
		t.Error("accepted a file without lines")
	}
}

// TestCollectHotBackups: backups of dead runs and of our own pid (a
// re-exec) are moved to previous/ and returned; a live instance keeps its
// own; a newer backup of the same file wins.
func TestCollectHotBackups(t *testing.T) {
	root := t.TempDir()
	writeBak(t, filepath.Join(root, "100"), "aaa.bak", "/w/a.txt\ndead run")
	writeBak(t, filepath.Join(root, "200"), "bbb.bak", "/w/b.txt\nlive instance")
	writeBak(t, filepath.Join(root, "300"), "ccc.bak", "/w/c.txt\nbefore reload")
	writeBak(t, filepath.Join(root, "previous"), "aaa.bak", "/w/a.txt\nolder")
	writeBak(t, filepath.Join(root, "100"), "x.tmp", "half written")

	alive := func(pid int) bool { return pid == 200 || pid == 300 }
	got, err := collectHotBackups(root, 300, alive)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })
	want := []hotBackup{
		{Path: "/w/a.txt", Lines: []string{"dead run"}},
		{Path: "/w/c.txt", Lines: []string{"before reload"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "200", "bbb.bak")); err != nil {
		t.Errorf("live instance lost its backup: %v", err)
	}
	for _, gone := range []string{"100", "300"} {
		if _, err := os.Stat(filepath.Join(root, gone)); !os.IsNotExist(err) {
			t.Errorf("dir %s not removed", gone)
		}
	}
}

func TestCollectHotBackupsEmpty(t *testing.T) {
	got, err := collectHotBackups(filepath.Join(t.TempDir(), "none"), 1, func(int) bool { return false })
	if err != nil || got != nil {
		t.Errorf("got %v %v", got, err)
	}
}

func TestHotExitRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	got, err := hotExitRoot("/w/proj")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != "/state/termocode/backup" || len(filepath.Base(got)) != 16 {
		t.Errorf("root %s", got)
	}
}

// TestCollectHotBackupsAgesOut: backups nobody restored ("Later") do not
// pile up in previous/ forever. Older than 7 days: deleted. More than 50:
// only the newest 50 stay.
func TestCollectHotBackupsAgesOut(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))

	root := t.TempDir()
	prev := filepath.Join(root, "previous")
	age := func(name string, d time.Duration) {
		t.Helper()
		if err := os.Chtimes(filepath.Join(prev, name), now.Add(-d), now.Add(-d)); err != nil {
			t.Fatal(err)
		}
	}
	writeBak(t, prev, "old.bak", "/w/old.txt\neight days")
	age("old.bak", 8*24*time.Hour)
	writeBak(t, prev, "week.bak", "/w/week.txt\nsix days")
	age("week.bak", 6*24*time.Hour)
	got, err := collectHotBackups(root, 1, func(int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "/w/week.txt" {
		t.Errorf("got %+v, want only week.txt", got)
	}
	if _, err := os.Stat(filepath.Join(prev, "old.bak")); !os.IsNotExist(err) {
		t.Errorf("8-day-old backup not deleted: %v", err)
	}

	// The newest hotExitMaxKept stay; the oldest go.
	for i := range hotExitMaxKept + 5 {
		name := fmt.Sprintf("n%03d.bak", i)
		writeBak(t, prev, name, fmt.Sprintf("/w/n%03d.txt\nx", i))
		age(name, time.Duration(i)*time.Minute)
	}
	got, err = collectHotBackups(root, 1, func(int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != hotExitMaxKept {
		t.Fatalf("kept %d backups, want %d", len(got), hotExitMaxKept)
	}
	for _, b := range got {
		if b.Path == "/w/week.txt" || b.Path >= fmt.Sprintf("/w/n%03d.txt", hotExitMaxKept) {
			t.Errorf("kept an old backup %s", b.Path)
		}
	}

	// All aged out: previous/ itself is gone.
	now = now.Add(30 * 24 * time.Hour)
	if got, err := collectHotBackups(root, 1, func(int) bool { return false }); err != nil || len(got) != 0 {
		t.Errorf("after 30 days: %+v %v", got, err)
	}
	if _, err := os.Stat(prev); !os.IsNotExist(err) {
		t.Errorf("empty previous/ not removed: %v", err)
	}
}

// TestHotExitPromptWaitsForOtherDialog: the backups are found while
// another question is on screen. The Restore prompt must not replace it,
// nor be skipped: it shows once that dialog closes.
func TestHotExitPromptWaitsForOtherDialog(t *testing.T) {
	m := Model{w: 120, h: 40, debounce: debounce.New(), ai: newAIState(), actionsWidth: defaultActionsWidth}
	m.confirm = confirm.New("Delete?", "Delete a.txt?", []confirm.Button{{ID: "delete", Title: "Delete"}, {ID: "cancel", Title: "Cancel"}})
	m.confirmOpen, m.confirmKind = true, confirmKindDeletePath

	next, _ := m.Update(hotExitFoundMsg{backups: []hotBackup{{Path: "/w/a.txt", Lines: []string{"x"}}}})
	m = next.(Model)
	if !m.confirmOpen || m.confirmKind != confirmKindDeletePath {
		t.Fatalf("the open dialog was replaced: open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	next, _ = m.Update(confirm.SelectMsg{ID: "cancel"})
	m = next.(Model)
	if !m.confirmOpen || m.confirmKind != confirmKindHotExit {
		t.Fatalf("no Restore prompt after the dialog closed: open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	if len(m.hotExitPending) != 1 || m.hotExitPending[0].Path != "/w/a.txt" {
		t.Errorf("pending %+v", m.hotExitPending)
	}
	// Answered once: it does not come back.
	next, _ = m.Update(confirm.SelectMsg{ID: "later"})
	if m = next.(Model); m.confirmOpen {
		t.Errorf("Restore prompt shown again")
	}
}

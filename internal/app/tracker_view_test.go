package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/tracker"
)

// CMP-81: Tracker: Show Tasks… lists doing, blocked, todo, then done
// tasks with their owner, plus Add Task…; a pick offers the file, the
// other statuses and the claim command. The AI never runs these.
func TestTrackerShow(t *testing.T) {
	m, _ := covGitRepo(t)
	dir := t.TempDir()
	old := trackerDir
	trackerDir = func(string) string { return dir }
	t.Cleanup(func() { trackerDir = old })
	now := time.Now()
	for _, title := range []string{"Write docs", "Fix login", "Old one"} {
		if _, err := tracker.Add(dir, title, "", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tracker.Claim(dir, 2, "claude", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.SetStatus(dir, 3, tracker.Done, "", "", now); err != nil {
		t.Fatal(err)
	}
	covMsg[gitxDoneMsg](t, m.dispatchPaletteAction(trackerShowPaletteID)).apply(m)
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "task-2,task-1,task-3,add" {
		t.Fatalf("tasks %v", gitxPickerIDs(m))
	}
	if h := m.gitx.items[0].Hint; !strings.Contains(h, "doing") || !strings.Contains(h, "claude") {
		t.Errorf("hint %q", h)
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "task-1"})
	if got := strings.Join(gitxPickerIDs(m), ","); got != "open,status-doing,status-blocked,status-done,copy" {
		t.Fatalf("actions %s", got)
	}
	m.pickerOpen = false
	covMsg[gitxDoneMsg](t, m.handlePickerSelect(picker.SelectMsg{ID: "status-blocked"})).apply(m)
	if tk, _ := tracker.Find(dir, 1); tk.Status != tracker.Blocked {
		t.Errorf("status %s", tk.Status)
	}
	if !toastHas(m, "#1 is blocked") {
		t.Error("no toast")
	}
	for _, id := range []string{trackerShowPaletteID, trackerAddPaletteID} {
		if p := aiActionPolicies[id]; p.Gate != aiGateNever {
			t.Errorf("%s policy %+v", id, p)
		}
	}
}

// Tracker: Add Task… asks a title and adds a todo.
func TestTrackerAdd(t *testing.T) {
	m, _ := covGitRepo(t)
	dir := t.TempDir()
	old := trackerDir
	trackerDir = func(string) string { return dir }
	t.Cleanup(func() { trackerDir = old })
	m.dispatchPaletteAction(trackerAddPaletteID)
	if !m.promptOpen {
		t.Fatal("no title prompt")
	}
	m.promptOpen = false
	covMsg[gitxDoneMsg](t, m.handlePromptSubmit("Ship the tracker")).apply(m)
	ts, _ := tracker.List(dir)
	if len(ts) != 1 || ts[0].Title != "Ship the tracker" || ts[0].Status != tracker.Todo {
		t.Fatalf("tasks %+v", ts)
	}
	if !toastHas(m, "Added #1") {
		t.Error("no toast")
	}
}

package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// Spec: Given a.txt and b.txt open, b.txt active with the cursor on line 3,
// and the folder notes expanded,
// when the user quits and starts termocode again in the same folder,
// then both tabs come back, b.txt is active with the cursor on line 3,
// and notes is still expanded.
func TestSessionRestoreFilesAndCursor(t *testing.T) {
	d := editorDriver(t, nil)
	clickExplorer(d, "notes")
	open(d, "a.txt")
	open(d, "b.txt")
	d.Key("down", "down")
	if !d.WaitFor(func() bool { return cursor(d) == "3:1" }, 2*time.Second) {
		t.Fatalf("cursor at %s before restart", cursor(d))
	}
	d.Settle(300 * time.Millisecond) // let the session save the cursor

	d.Restart()
	if !waitTabs(d, "a.txt", "b.txt") {
		got, _ := tabs(d)
		t.Fatalf("restored tabs = %v, want [a.txt b.txt]", got)
	}
	if _, a := tabs(d); a != "b.txt" {
		t.Errorf("restored active tab %q, want b.txt", a)
	}
	if got := cursor(d); !strings.HasPrefix(got, "3:") {
		t.Errorf("restored cursor at %s, want line 3", got)
	}
	if !explorerShows(d, "todo.md") {
		t.Errorf("notes not expanded after restart:\n%s", screen(d))
	}
}

// Spec: Given the user opened a.txt and c.txt and then closed all tabs,
// when the dashboard shows, then RECENT FILES lists both;
// and when the user clicks a.txt there, then it opens.
func TestDashboardRecentFiles(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	open(d, "c.txt")
	d.Key("ctrl+w")
	d.Key("ctrl+w")
	if !waitTabs(d) {
		got, _ := tabs(d)
		t.Fatalf("tabs left open: %v", got)
	}
	if !d.WaitForText("R E C E N T", 3*time.Second) {
		t.Fatalf("no dashboard after closing all tabs:\n%s", screen(d))
	}
	_, _, okA := findIn(d, "a.txt", sidebarW, d.W)
	_, _, okC := findIn(d, "c.txt", sidebarW, d.W)
	if !okA || !okC {
		t.Fatalf("recent files not listed:\n%s", screen(d))
	}
	xa, ya, _ := findIn(d, "a.txt", sidebarW, d.W)
	d.Click(xa+1, ya)
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "a.txt" }, 3*time.Second) {
		got, a := tabs(d)
		t.Fatalf("click on recent a.txt: active %q tabs %v", a, got)
	}
}

// Spec: Given termocode was used in another folder before (the app records
// each folder it starts in),
// when the dashboard shows, then WORKSPACES lists that folder.
func TestDashboardRecentWorkspaces(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	other := filepath.Join(filepath.Dir(d.Dir), "older-project")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	app.PushWorkspace(other)
	d.Restart()
	if _, _, ok := findIn(d, "older-project", sidebarW, d.W); !ok {
		t.Fatalf("WORKSPACES does not list older-project:\n%s", screen(d))
	}
}

// Spec: Given a.txt open,
// when the user splits the editor (Ctrl+\) and opens b.txt in the new
// pane, then two editor windows show a.txt and b.txt;
// when the user runs Close Split, then one window is left.
func TestSplitEditor(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Key("ctrl+\\")
	wins := func() string {
		return lua(d, `local n = {}
for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
  local b = vim.api.nvim_win_get_buf(w)
  if vim.bo[b].buftype == '' then table.insert(n, vim.fn.fnamemodify(vim.api.nvim_buf_get_name(b), ':t')) end
end
table.sort(n)
return table.concat(n, ',')`)
	}
	if got := wins(); got != "a.txt,a.txt" {
		t.Fatalf("after split: editor windows %q, want a.txt twice", got)
	}
	open(d, "b.txt")
	if got := wins(); got != "a.txt,b.txt" {
		t.Fatalf("after opening b.txt in the split: %q", got)
	}
	if !d.WaitForText("bravo", 2*time.Second) || !strings.Contains(screen(d), "one") {
		t.Errorf("both files not on screen:\n%s", screen(d))
	}
	d.Palette("close-split")
	if got := wins(); strings.Count(got, ",") != 0 {
		t.Errorf("after Close Split: editor windows %q, want one", got)
	}
}

// Spec: Given a.txt then b.txt opened,
// when the user presses Ctrl+Tab, then a.txt is active;
// and Ctrl+Tab again goes back to b.txt.
func TestAlternateFile(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	open(d, "b.txt")
	d.Key("ctrl+tab")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "a.txt" }, 2*time.Second) {
		_, a := tabs(d)
		t.Fatalf("Ctrl+Tab: active %q, want a.txt", a)
	}
	d.Key("ctrl+tab")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "b.txt" }, 2*time.Second) {
		_, a := tabs(d)
		t.Fatalf("Ctrl+Tab again: active %q, want b.txt", a)
	}
}

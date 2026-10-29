package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// SCN-14: a workspace folder that was moved or deleted.

// Spec: Given the dashboard lists the recent workspace older-project and
// that folder was then deleted,
// when the user clicks it, then a card says the folder was not found,
// with Close first; Close keeps the entry; Remove from List drops it from
// the dashboard and from the saved list.
func TestRecentWorkspaceGoneOffersRemove(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	other := filepath.Join(filepath.Dir(d.Dir), "older-project")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	app.PushWorkspace(other)
	d.Restart()
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}
	click := func() {
		x, y, ok := findIn(d, "older-project", sidebarW, d.W)
		if !ok {
			t.Fatalf("WORKSPACES does not list older-project:\n%s", screen(d))
		}
		d.Click(x+1, y)
	}
	click()
	if !d.WaitForText("Folder not found", 3*time.Second) {
		t.Fatalf("no clear message for the deleted folder:\n%s", screen(d))
	}
	d.Key("enter") // Close is first
	if strings.Contains(screen(d), "Folder not found") {
		t.Fatalf("Enter did not close the card:\n%s", screen(d))
	}
	if _, _, ok := findIn(d, "older-project", sidebarW, d.W); !ok {
		t.Fatalf("Close removed the entry:\n%s", screen(d))
	}
	click()
	if !d.WaitForText("Folder not found", 3*time.Second) {
		t.Fatalf("no card the second time:\n%s", screen(d))
	}
	d.Key("tab", "enter") // Remove from List
	if !d.WaitFor(func() bool { d.render(); _, _, ok := findIn(d, "older-project", sidebarW, d.W); return !ok }, 3*time.Second) {
		t.Errorf("the entry is still listed:\n%s", screen(d))
	}
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "workspaces.json"))
	if strings.Contains(string(b), "older-project") {
		t.Errorf("workspaces.json still has it: %s", b)
	}
}

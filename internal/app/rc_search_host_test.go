package app

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/search"
)

func rcSearchFiles() fstest.MapFS {
	return fstest.MapFS{
		"remote.go":           {Data: []byte("package main\n// a needle in the container\n")},
		"cmd/tool.go":         {Data: []byte("package cmd\n\nvar needle = 1\n")},
		"node_modules/x/x.js": {Data: []byte("needle")},
		"other.go":            {Data: []byte("package main\n")},
	}
}

// Spec (FIX-3): Find in Files in a container workspace searches the
// container's files through the Host FS, never the local disk (the same
// path holds decoys locally).
func TestRCFindInFilesSearchesHostFS(t *testing.T) {
	withConfigDir(t)
	ws, h := newRCWorkspace(t, rcSearchFiles())
	m := Model{ws: ws}
	cmd := m.runFindInFilesWith("needle", search.Options{})
	if cmd == nil {
		t.Fatal("no search started")
	}
	var paths []string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		fb, ok := cmd().(findBatchMsg)
		if !ok {
			t.Fatal("not a find batch")
		}
		for _, r := range fb.batch.Results {
			paths = append(paths, r.Path)
		}
		if fb.batch.Done {
			if fb.batch.Err != nil {
				t.Fatalf("search error: %v", fb.batch.Err)
			}
			break
		}
		cmd = waitFindBatch(fb.gen, fb.ch)
	}
	m.findRun.cancel()
	got := strings.Join(paths, " ")
	want := filepath.Join(ws.Root, "cmd/tool.go") + " " + filepath.Join(ws.Root, "remote.go")
	if got != want {
		t.Errorf("results %q, want %q", got, want)
	}
	if len(h.calls()) == 0 {
		t.Error("the Host FS was never asked")
	}
}

// Spec (FIX-3): the search overlay of a container workspace searches the
// container's files too.
func TestRCSearchOverlaySearchesHostFS(t *testing.T) {
	withConfigDir(t)
	ws, _ := newRCWorkspace(t, rcSearchFiles())
	m := Model{ws: ws, w: 120, h: 40}
	m.explorer = newExplorerFor(ws)
	m.dispatchPaletteAction("find-files-overlay")
	if !m.searchOpen {
		t.Fatal("overlay not open")
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("needle")})
	for i := 0; cmd != nil && i < 4; i++ {
		msg := cmd()
		m.search, cmd = m.search.Update(msg)
	}
	view := m.search.View()
	if !strings.Contains(view, "remote.go") || !strings.Contains(view, "tool.go") {
		t.Error("the container files are missing from the overlay")
	}
	if strings.Contains(view, "local.go") || strings.Contains(view, "local.txt") {
		t.Error("the overlay read the local disk")
	}
}

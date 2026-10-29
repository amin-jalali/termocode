package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/layout"
)

func writeLayoutFiles(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(root, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// openBufs lists the listed buffers' paths relative to root.
func openBufs(t *testing.T, m Model, root string) (files []string, current string) {
	t.Helper()
	bufs, active, err := m.nvim.Buffers()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bufs {
		if rel := layoutRelPath(root, b.Path); rel != "" {
			files = append(files, rel)
			if b.ID == active {
				current = rel
			}
		}
	}
	return files, current
}

func TestOpenLayoutOpensFilesActiveLastAndSaysWhichAreMissing(t *testing.T) {
	m, root := layoutNvimModel(t)
	writeLayoutFiles(t, root, "a.txt", "src/b.txt", "c.txt")
	cmd := m.openLayout(layout.Layout{Editor: &layout.Editor{
		Files:  []string{"a.txt", "src/b.txt", "gone.txt", "also/gone.txt", "c.txt"},
		Active: "src/b.txt",
	}})
	files, cur := openBufs(t, m, root)
	if want := []string{"a.txt", "src/b.txt", "c.txt"}; !reflect.DeepEqual(files, want) {
		t.Errorf("open files = %v, want %v", files, want)
	}
	if cur != "src/b.txt" {
		t.Errorf("active = %q", cur)
	}
	if cmd == nil {
		t.Fatal("no toast command")
	}
	if got := toastText(m); strings.Count(got, "not found") != 1 || !strings.Contains(got, "2 files not found") {
		t.Errorf("toasts = %q", got)
	}
}

func TestOpenLayoutWithAllFilesThereShowsNoWarning(t *testing.T) {
	m, root := layoutNvimModel(t)
	writeLayoutFiles(t, root, "a.txt")
	m.openLayout(layout.Layout{Editor: &layout.Editor{Files: []string{"a.txt"}}})
	if !m.toast.Empty() {
		t.Errorf("toast: %q", toastText(m))
	}
	m.openLayout(layout.Layout{Editor: &layout.Editor{Files: []string{"zzz.txt"}}})
	if got := toastText(m); !strings.Contains(got, "1 file not found") {
		t.Errorf("toast = %q", got)
	}
}

func TestOpenLayoutSplitsTheEditorAndCaptureReadsItBack(t *testing.T) {
	m, root := layoutNvimModel(t)
	writeLayoutFiles(t, root, "a.txt", "b.txt", "c.txt")
	m.openLayout(layout.Layout{Editor: &layout.Editor{Split: "vertical", Panes: []string{"a.txt", "b.txt", "gone.txt"}}})
	split, panes := m.captureEditorSplit(root)
	if split != "vertical" || !reflect.DeepEqual(panes, []string{"a.txt", "b.txt"}) {
		t.Fatalf("split %q panes %v", split, panes)
	}
	// Another layout with a different split replaces the old windows.
	m.openLayout(layout.Layout{Editor: &layout.Editor{Split: "horizontal", Panes: []string{"c.txt", "a.txt"}}})
	split, panes = m.captureEditorSplit(root)
	if split != "horizontal" || !reflect.DeepEqual(panes, []string{"c.txt", "a.txt"}) {
		t.Fatalf("split %q panes %v", split, panes)
	}
	// No split in the layout: one window again.
	m.openLayout(layout.Layout{Editor: &layout.Editor{Active: "b.txt"}})
	if split, panes = m.captureEditorSplit(root); split != "" || panes != nil {
		t.Fatalf("still split: %q %v", split, panes)
	}
}

func TestOpenLayoutSplitKeepsThePanelWindow(t *testing.T) {
	m, root := layoutNvimModel(t)
	writeLayoutFiles(t, root, "a.txt", "b.txt")
	m.openLayout(layout.Layout{
		Panel:  &layout.Panel{Open: true, Tabs: []string{"output"}, Active: "output"},
		Editor: &layout.Editor{Split: "vertical", Panes: []string{"a.txt", "b.txt"}},
	})
	if !m.termOpen {
		t.Fatal("panel closed by the split")
	}
	out, _ := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_win_is_valid(` + intToLua(m.terminalWinID) + `))`)
	if out != "true" {
		t.Fatalf("panel window gone: %q", out)
	}
	if split, panes := m.captureEditorSplit(root); split != "vertical" || len(panes) != 2 {
		t.Fatalf("split %q panes %v", split, panes)
	}
}

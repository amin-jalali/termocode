package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// In a batch the buffers of a path come from one list of all buffers,
// with the same answer as asking nvim for that path; a delete in the
// batch reads the list again.
func TestAIBatchBuffersUnder(t *testing.T) {
	m, c, dir := aiSyncModel(t, nil, map[string]string{"a/x.txt": "x\n", "a/y.txt": "y\n", "ab.txt": "ab\n", "b.txt": "b\n"})
	for _, f := range []string{"a/x.txt", "a/y.txt", "ab.txt"} {
		m.openFileAtLine(filepath.Join(dir, f), 1)
	}
	if err := c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {"edited"})`); err != nil {
		t.Fatal(err)
	}
	show := func(bs []aiBuf) string {
		var out []string
		for _, b := range bs {
			out = append(out, fmt.Sprintf("%s:%v:%v", strings.TrimPrefix(b.Path, dir), b.Modified, b.Loaded))
		}
		return strings.Join(out, ",")
	}
	paths := []string{"a", "a/x.txt", "a/y.txt", "ab.txt", "b.txt", "a/", ""}
	want := map[string]string{}
	for _, p := range paths {
		bs, err := m.aiBuffersUnder(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		want[p] = show(bs)
	}
	if !strings.Contains(want["ab.txt"], "true") || want["a"] == "" || strings.Contains(want["a"], "ab.txt") {
		t.Fatalf("setup: %v", want)
	}
	end := m.aiBeginBatch()
	for _, p := range paths {
		bs, err := m.aiBuffersUnder(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		if got := show(bs); got != want[p] {
			t.Errorf("%q in a batch: %s, want %s", p, got, want[p])
		}
	}
	rel := aiRel(dir)
	if _, _, err := m.aiDeletePath(filepath.Join(dir, "a"), rel); err != nil {
		t.Fatal(err)
	}
	if bs, _ := m.aiBuffersUnder(filepath.Join(dir, "a")); len(bs) != 0 {
		t.Errorf("deleted folder still has buffers in the batch: %s", show(bs))
	}
	if cmd := end(); cmd == nil {
		t.Error("the batch did not refresh the IDE at its end")
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); err == nil {
		t.Error("a still there")
	}
}

// Spec: Undo still sees a tab with unsaved edits: it asks, and Restore
// leaves that file as it is.
func TestAIUndoTurnUnsavedTab(t *testing.T) {
	m, dir, _ := undoTurnModel(t)
	if err := m.nvim.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {"typing"})`); err != nil {
		t.Fatal(err)
	}
	m.aiUndoTurn(-1)
	if box := ansi.Strip(m.confirm.Box()); !m.confirmOpen || !strings.Contains(box, "unsaved edits") || !strings.Contains(box, "a.txt") {
		t.Fatalf("no dialog naming the unsaved tab:\n%s", m.confirm.Box())
	}
	m.confirmOpen = false
	m.handleAIConfirm("restore")
	if got := readFile(filepath.Join(dir, "a.txt")); got != "agent\n" {
		t.Errorf("a.txt overwritten under unsaved edits: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err == nil {
		t.Error("new.txt still there")
	}
	last := m.ai.conv.Messages[len(m.ai.conv.Messages)-1].Text
	if !strings.Contains(last, "not restored: a.txt") {
		t.Errorf("chat line %q", last)
	}
}

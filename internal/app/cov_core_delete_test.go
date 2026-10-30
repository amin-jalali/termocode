package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// covFailRemove makes deleting any path that contains bad fail.
func covFailRemove(t *testing.T, bad string) {
	t.Helper()
	old := osRemoveAll
	osRemoveAll = func(p string) error {
		if strings.Contains(p, bad) {
			return errors.New("permission denied")
		}
		return old(p)
	}
	t.Cleanup(func() { osRemoveAll = old })
}

// Deleting several items: the toast counts what went and what failed;
// buffers inside a deleted folder close.
func TestCovCoreBulkDelete(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	_ = os.MkdirAll(sub, 0o755)
	inner := filepath.Join(sub, "inner.go")
	ok1 := filepath.Join(dir, "ok1.txt")
	locked := filepath.Join(dir, "locked.txt")
	other := filepath.Join(dir, "other.txt")
	for _, p := range []string{inner, ok1, locked, other} {
		_ = os.WriteFile(p, []byte("x\n"), 0o644)
	}
	if err := m.nvim.OpenFile("edit", inner); err != nil {
		t.Fatal(err)
	}
	if err := m.nvim.OpenFile("edit", other); err != nil {
		t.Fatal(err)
	}
	innerBuf, _ := m.nvim.EvalLuaString(`return tostring(vim.fn.bufnr('` + inner + `'))`)
	otherBuf, _ := m.nvim.EvalLuaString(`return tostring(vim.fn.bufnr('` + other + `'))`)
	m.bufs = []nvim.BufferInfo{{ID: parseInt(innerBuf), Path: inner}, {ID: parseInt(otherBuf), Path: other}, {ID: 999}}
	covFailRemove(t, "locked")

	m.openBulkDeleteConfirm([]string{sub, "", ok1, locked})
	if !m.confirmOpen {
		t.Fatal("no delete question")
	}
	m, _ = m.update(confirm.SelectMsg{ID: "delete"})
	if !strings.Contains(toastText(m), "Deleted 2, 1 failed") || !strings.Contains(m.err, "locked.txt: permission denied") {
		t.Errorf("mixed result: toast %q err %q", toastText(m), m.err)
	}
	for _, p := range []string{sub, ok1} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s still there", p)
		}
	}
	covWaitLua(t, m, `return vim.fn.buflisted(`+innerBuf+`) == 1 and '1' or '0'`, "0")
	if got, _ := m.nvim.EvalLuaString(`return vim.fn.buflisted(` + otherBuf + `) == 1 and '1' or '0'`); got != "1" {
		t.Error("a buffer outside the deleted folder was closed")
	}

	// Everything fails.
	_ = m.runBulkDelete([]string{locked})
	if !strings.Contains(toastText(m), "Delete failed") {
		t.Errorf("all failed: %q", toastText(m))
	}
	// One item, deleted.
	_ = m.runBulkDelete([]string{other})
	if !strings.Contains(toastText(m), "Deleted 1 item") || m.err != "" {
		t.Errorf("one item: %q err %q", toastText(m), m.err)
	}
	if m.runBulkDelete(nil) != nil {
		t.Error("an empty delete did something")
	}
}

// A single delete that fails keeps the file and says why.
func TestCovCoreSingleDeleteFails(t *testing.T) {
	m := covNvimModel(t, 100, 30)
	p := filepath.Join(t.TempDir(), "locked.go")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	covFailRemove(t, "locked")
	m.openDeleteConfirm(p)
	m, _ = m.update(confirm.SelectMsg{ID: "delete"})
	if m.err != "permission denied" {
		t.Errorf("err = %q", m.err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Error("the file went although the delete failed")
	}
	// Cancel deletes nothing.
	m.openDeleteConfirm(p)
	m, _ = m.update(confirm.SelectMsg{ID: "cancel"})
}

// liveBuffer asks nvim; without nvim it uses the tab list.
func TestCovCoreLiveBuffer(t *testing.T) {
	m := covNvimModel(t, 100, 30)
	if _, ok := m.liveBuffer(987654); ok {
		t.Error("a buffer nvim does not have")
	}
	var none Model
	none.bufs = []nvim.BufferInfo{{ID: 3, Path: "/x"}}
	if b, ok := none.liveBuffer(3); !ok || b.Path != "/x" {
		t.Errorf("no nvim: %+v %v", b, ok)
	}
	// A dirty unnamed buffer asks with its placeholder name.
	_ = m.nvim.ExecLua(`vim.cmd('enew') vim.api.nvim_buf_set_lines(0, 0, -1, false, {'draft'})`)
	id, _ := m.nvim.EvalLuaString(`return tostring(vim.api.nvim_get_current_buf())`)
	m.bufs = append(m.bufs, nvim.BufferInfo{ID: parseInt(id)})
	if !m.promptCloseBufferIfDirty(parseInt(id)) {
		t.Fatal("no question for a dirty unnamed buffer")
	}
	if v := ansiPlain(m.confirm.View()); !strings.Contains(v, "[unnamed]") {
		t.Errorf("question:\n%s", v)
	}
}

package nvim

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuffersLinesAndMarks(t *testing.T) {
	c := isolatedClient(t)
	a := editTemp(t, c, "a.go", "package a\n\nfunc A() {}\n")
	b := editTemp(t, c, "b.txt", "one\ntwo\nthree\n")
	if err := c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, 1, false, {'ONE'})`); err != nil {
		t.Fatal(err)
	}
	bufs, active, err := c.Buffers()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]BufferInfo{}
	for _, x := range bufs {
		byPath[x.Path] = x
	}
	if len(bufs) != 2 || byPath[a].Filetype != "go" || byPath[a].Modified || !byPath[b].Modified || byPath[b].ID != active {
		t.Fatalf("buffers %+v active %d", bufs, active)
	}

	lines, err := c.CurrentBufLines()
	if err != nil || strings.Join(lines, "|") != "ONE|two|three" {
		t.Fatalf("lines %q %v", lines, err)
	}
	// nil lines delete the range; the cursor goes to the first changed line.
	if err := c.ReplaceCurrentBufLines(1, 2, nil); err != nil {
		t.Fatal(err)
	}
	if text, _ := c.BufferContent(); text != "ONE\nthree" {
		t.Fatalf("after delete %q", text)
	}
	if row, _ := c.EvalLuaString(`return tostring(vim.api.nvim_win_get_cursor(0)[1])`); row != "2" {
		t.Fatalf("cursor row %q", row)
	}
	if err := c.ReplaceCurrentBufLines(0, 1, []string{"x", "y"}); err != nil {
		t.Fatal(err)
	}
	if text, _ := c.BufferContent(); text != "x\ny\nthree" {
		t.Fatalf("after replace %q", text)
	}

	// Global marks in two files, listed A first.
	if err := c.Command("normal! gg"); err != nil {
		t.Fatal(err)
	}
	if err := c.Command("mark C"); err != nil {
		t.Fatal(err)
	}
	if err := c.OpenFile("edit", a); err != nil {
		t.Fatal(err)
	}
	if err := c.Command("3mark A"); err != nil {
		t.Fatal(err)
	}
	marks, err := c.Bookmarks()
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 2 || marks[0].Letter != "A" || marks[0].File != a || marks[0].Line != 3 || marks[1].Letter != "C" || marks[1].File != b {
		t.Fatalf("marks %+v", marks)
	}

	// Diagnostics for the scrollbar and the line count in one call.
	if err := c.ExecLua(`vim.diagnostic.set(vim.api.nvim_create_namespace('t'), 0, {{ lnum = 2, col = 0, severity = 2, message = 'w' }})`); err != nil {
		t.Fatal(err)
	}
	marksD, total, err := c.BufferDiagnosticsAndLines()
	if err != nil || total != 3 || len(marksD) != 1 || marksD[0].Line != 3 || marksD[0].Severity != 2 {
		t.Fatalf("diag marks %+v total %d err %v", marksD, total, err)
	}
}

func TestMouseClickMovesCursorAndMode(t *testing.T) {
	c := isolatedClient(t)
	editTemp(t, c, "m.txt", "first line\nsecond line\nthird line\n")
	if mode, blocking, err := c.Mode(); err != nil || mode != "n" || blocking {
		t.Fatalf("mode %q blocking %v err %v", mode, blocking, err)
	}
	if err := c.Mouse("left", "press", "", 1, 3); err != nil {
		t.Fatal(err)
	}
	if err := c.Mouse("left", "release", "", 1, 3); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pos, _ := c.EvalLuaString(`local p = vim.api.nvim_win_get_cursor(0); return p[1] .. ':' .. p[2]`)
		if pos == "2:3" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cursor at %s, want 2:3", pos)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := c.Resize(60, 10); err != nil {
		t.Fatal(err)
	}
}

func TestRPCErrorsComeBack(t *testing.T) {
	c := isolatedClient(t)
	if err := c.Command("NoSuchCommandHere"); err == nil || !strings.Contains(err.Error(), "E492") {
		t.Fatalf("command err = %v", err)
	}
	if err := c.ExecLua("this is not lua"); err == nil {
		t.Fatal("bad Lua gave no error")
	}
	if _, err := c.EvalLuaStringArgs(`error('boom ' .. ...)`, "x"); err == nil || !strings.Contains(err.Error(), "boom x") {
		t.Fatalf("lua error = %v", err)
	}
	if s, err := c.EvalLuaStringArgs(`return select(1, ...) .. '!'`, "hi"); err != nil || s != "hi!" {
		t.Fatalf("args = %q %v", s, err)
	}
	if err := c.OpenFile("edit", filepath.Join(t.TempDir(), "100%.txt")); err != nil {
		t.Fatal(err)
	}
	if name, _ := c.EvalLuaString(`return vim.fn.fnamemodify(vim.api.nvim_buf_get_name(0), ':t')`); name != "100%.txt" {
		t.Fatalf("opened %q", name)
	}
}

func TestCallsAfterNvimQuitFailFast(t *testing.T) {
	c := isolatedClient(t)
	_ = c.Input(":qa!<CR>")
	deadline := time.Now().Add(10 * time.Second)
	for !c.Exited() {
		if time.Now().After(deadline) {
			t.Fatal("nvim did not quit")
		}
		time.Sleep(5 * time.Millisecond)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := c.Buffers()
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Buffers on a dead nvim gave no error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("call on a dead nvim hung")
	}
	if _, _, err := c.BufferDiagnosticsAndLines(); err == nil {
		t.Fatal("diagnostics on a dead nvim gave no error")
	}
	if _, err := c.AllDiagnostics(); err == nil {
		t.Fatal("all diagnostics on a dead nvim gave no error")
	}
	if _, err := c.Bookmarks(); err == nil {
		t.Fatal("bookmarks on a dead nvim gave no error")
	}
	if _, err := c.References(); err == nil {
		t.Fatal("references on a dead nvim gave no error")
	}
	if _, err := c.WorkspaceSymbols(""); err == nil {
		t.Fatal("workspace symbols on a dead nvim gave no error")
	}
	if _, err := c.FetchCodeActions(); err == nil {
		t.Fatal("code actions on a dead nvim gave no error")
	}
	if _, err := c.DocumentSymbols(); err == nil || IsNoLSP(err) {
		t.Fatalf("document symbols on a dead nvim: %v", err)
	}
	if _, err := c.Hover(); err == nil {
		t.Fatal("hover on a dead nvim gave no error")
	}
	if _, err := c.GotoTypeDefinition(); err == nil {
		t.Fatal("goto on a dead nvim gave no error")
	}
}

func TestConversionHelpers(t *testing.T) {
	for _, v := range []interface{}{int(3), int32(3), int64(3), uint32(3), uint64(3), float64(3.9)} {
		if toInt(v) != 3 {
			t.Errorf("toInt(%T) = %d", v, toInt(v))
		}
	}
	if toInt("3") != 0 || toString([]byte("ab")) != "ab" || toString(5) != "" {
		t.Fatal("toInt/toString odd input")
	}
	if !toBool(int64(1)) || toBool(int64(0)) || toBool("true") {
		t.Fatal("toBool")
	}
}

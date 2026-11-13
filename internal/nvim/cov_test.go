package nvim

import (
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Without an nvim on PATH, New fails with a clear error and leaves nothing
// running.
func TestNewWithoutNvim(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if c, err := New(); err == nil || c != nil || !strings.Contains(err.Error(), "spawn nvim") {
		t.Fatalf("New: %v %v", c, err)
	}
}

func TestIsFetchBusy(t *testing.T) {
	if !IsFetchBusy(errFetchBusy) || IsFetchBusy(nil) {
		t.Fatal("IsFetchBusy")
	}
}

// ExecLuaResult and EvalLuaArgs run a chunk with arguments and decode
// what it returns.
func TestExecLuaResultAndEvalLuaArgs(t *testing.T) {
	c := isolatedClient(t)
	var n int64
	if err := c.ExecLuaResult("return select(1, ...) + 1", &n, 41); err != nil || n != 42 {
		t.Fatalf("ExecLuaResult: %d %v", n, err)
	}
	var s string
	if err := c.EvalLuaArgs("return ... .. '!'", &s, "hi"); err != nil || s != "hi!" {
		t.Fatalf("EvalLuaArgs: %q %v", s, err)
	}
}

// Negative line windows count as zero: only the cursor line comes back.
func TestCursorContextWindowNegativeWindows(t *testing.T) {
	c := isolatedClient(t)
	if err := c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'one', 'two', 'three'})`); err != nil {
		t.Fatal(err)
	}
	info, err := c.CursorContextWindow(-5, -5)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(info.Before, "one") || strings.Contains(info.After, "three") && info.Line != 3 {
		t.Fatalf("window not clamped: %+v", info)
	}
}

// Unblock ends a half-typed key sequence (the char after f) so calls
// answer again; on a nil client it does nothing.
func TestUnblockEndsPendingKeys(t *testing.T) {
	var none *Client
	none.Unblock()
	(&Client{}).Unblock()
	c := isolatedClient(t)
	if _, err := c.nv.Input("f"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m, err := c.nv.Mode(); err == nil && m.Blocking {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.Unblock()
	if m, err := c.nv.Mode(); err != nil || m.Blocking {
		t.Fatalf("still blocked: %+v %v", m, err)
	}
}

// A full queue drops a mouse drag but never blocks on it.
func TestMouseAsyncDropsDragWhenQueueFull(t *testing.T) {
	c := &Client{mouseQueue: make(chan mouseEvent, 1)}
	c.MouseAsync("left", "drag", "", 1, 1)
	done := make(chan struct{})
	go func() { c.MouseAsync("left", "drag", "", 2, 2); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a drag blocked on a full queue")
	}
	if len(c.mouseQueue) != 1 {
		t.Fatalf("queue %d", len(c.mouseQueue))
	}
	c.closed.Store(true)
	c.MouseAsync("left", "press", "", 1, 1) // after Close: ignored, no panic
}

// The leak report names who started a client, also when the starter is
// a closure or a goroutine outside any test function.
func TestLeakCreatorNames(t *testing.T) {
	TrackClients()
	func() {
		c := isolatedClient(t)
		if r := LeakReport(0); !strings.Contains(r, "TestLeakCreatorNames") {
			t.Errorf("closure: %q", r)
		}
		_ = c.Close()
	}()
	var wg sync.WaitGroup
	wg.Add(1)
	go outsideTest(t, &wg)
	wg.Wait()
	if r := LeakReport(2 * time.Second); r != "" {
		t.Logf("other clients still live: %q", r)
	}
}

// outsideTest runs on its own goroutine: no Test function is on its stack.
func outsideTest(t *testing.T, wg *sync.WaitGroup) {
	defer wg.Done()
	if got := creator(); got == "" {
		t.Error("goroutine creator is empty")
	}
}

// The state lists the attached language servers and the cursor of each
// open file.
func TestFetchStateListsLSPClientsAndCursors(t *testing.T) {
	c := isolatedClient(t)
	p := editTemp(t, c, "main.go", "package main\n\nfunc main() {}\n")
	startFakeLSP(t, c, map[string]any{})
	st, err := c.FetchState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.LSPClients) != 1 || st.LSPClients[0] != "fakels" {
		t.Fatalf("LSPClients %v", st.LSPClients)
	}
	if _, ok := st.Cursors[p]; !ok {
		t.Fatalf("no cursor for %s in %v", p, st.Cursors)
	}
}

// A server that answers with no symbols gives an empty list, not an error.
func TestDocumentSymbolsEmptyAnswer(t *testing.T) {
	c := isolatedClient(t)
	editTemp(t, c, "main.go", "package main\n")
	startFakeLSP(t, c, map[string]any{"textDocument/documentSymbol": []any{}})
	syms, err := c.DocumentSymbols()
	if err != nil || len(syms) != 0 {
		t.Fatalf("%v %v", syms, err)
	}
}

// Calls on a closed client report an error instead of hanging.
func TestCallsAfterCloseReturnErrors(t *testing.T) {
	c := isolatedClient(t)
	if err := c.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	if _, _, err := c.Mode(); err == nil {
		t.Error("Mode after Close")
	}
	if _, err := c.CursorContextWindow(1, 1); err == nil {
		t.Error("CursorContextWindow after Close")
	}
	if err := c.Attach(80, 24); err == nil {
		t.Error("Attach after Close")
	}
}

// Next gives nil when the event stream is closed.
func TestNextOnClosedEventStream(t *testing.T) {
	c := &Client{events: make(chan tea.Msg), exited: make(chan struct{})}
	close(c.events)
	if c.Next()() != nil {
		t.Fatal("a closed stream gave a message")
	}
}

// startProc gives a command a parent-death signal even when it had no
// process attributes, and reports a failed start.
func TestStartProcSetsAttrsAndReportsFailure(t *testing.T) {
	reaped := make(chan struct{})
	cmd := exec.Command("true")
	if err := startProc(cmd, reaped); err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Pdeathsig == 0 {
		t.Fatal("no parent-death signal")
	}
	_ = cmd.Wait()
	close(reaped)
	if err := startProc(exec.Command("/no/such/binary"), make(chan struct{})); err == nil {
		t.Fatal("a missing binary started")
	}
}

// A long message list opens a "more" prompt; the guard answers it too.
func TestPromptGuardAnswersMorePrompt(t *testing.T) {
	c := isolatedClient(t)
	_ = c.Command("set more")
	_ = c.Input(`<Esc>:lua for i = 1, 200 do print(i) end<CR>`)
	done := make(chan string, 1)
	go func() {
		out, _ := c.EvalLuaString(`return vim.api.nvim_get_mode().mode`)
		done <- out
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("nvim stayed in the more prompt")
	}
}

// A document-symbol answer of "nothing" is an empty list.
func TestDocumentSymbolsNullAnswer(t *testing.T) {
	c := isolatedClient(t)
	editTemp(t, c, "main.go", "package main\n")
	startFakeLSP(t, c, map[string]any{})
	syms, err := c.DocumentSymbols()
	if err != nil || len(syms) != 0 {
		t.Fatalf("%v %v", syms, err)
	}
}

// An nvim that exits at once (a broken install) makes New fail or give a
// client that reports its exit; either way nothing keeps running.
func TestNewWithNvimThatExitsAtOnce(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nvim"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	c, err := New()
	if err != nil {
		if !strings.Contains(err.Error(), "spawn nvim") {
			t.Fatalf("error %v", err)
		}
		return
	}
	defer c.Close()
	if err := c.Attach(80, 24); err == nil {
		t.Fatal("Attach worked against a dead nvim")
	}
}

// When the language server request fails or gives nothing, the symbol
// list is empty and not an error.
func TestDocumentSymbolsRequestFails(t *testing.T) {
	c := isolatedClient(t)
	editTemp(t, c, "main.go", "package main\n")
	startFakeLSP(t, c, map[string]any{})
	if err := c.ExecLua(`vim.lsp.buf_request_sync = function() return nil end`); err != nil {
		t.Fatal(err)
	}
	if syms, err := c.DocumentSymbols(); err != nil || len(syms) != 0 {
		t.Fatalf("nil answer: %v %v", syms, err)
	}
	if err := c.ExecLua(`vim.lsp.buf_request_sync = function() error('boom') end`); err != nil {
		t.Fatal(err)
	}
	if syms, err := c.DocumentSymbols(); err != nil || len(syms) != 0 {
		t.Fatalf("failing request: %v %v", syms, err)
	}
}

// Publishing the channel id fails cleanly when nvim is gone.
func TestExportChannelWithoutAConnection(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Skipf("no nvim: %v", err)
	}
	_ = c.Close()
	if err := c.exportChannel(); err == nil {
		t.Fatal("exportChannel on a closed client")
	}
}

// Next waits a moment for nvim's exit status; a Close in that time makes
// it a normal quit.
func TestNextCloseDuringExitWait(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 3 s")
	}
	c := &Client{events: make(chan tea.Msg), exited: make(chan struct{}), reaped: make(chan struct{})}
	close(c.exited)
	got := make(chan tea.Msg, 1)
	go func() { got <- c.Next()() }()
	time.Sleep(100 * time.Millisecond)
	c.closed.Store(true)
	select {
	case msg := <-got:
		if msg != nil {
			t.Fatalf("got %T", msg)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Next never returned")
	}
}

// A row that nvim hands over broken (a missing field) is skipped, never a
// crash and never a half-filled entry.
func TestBrokenRowsFromNvimAreSkipped(t *testing.T) {
	c := isolatedClient(t)
	p := editTemp(t, c, "main.go", "package main\n\nfunc main() {}\n")
	if err := c.ExecLua(`vim.diagnostic.set(vim.api.nvim_create_namespace('cov'), 0, {{ lnum = 0, col = 0, message = 'm', severity = 1 }})`); err != nil {
		t.Fatal(err)
	}
	// One diagnostic without a severity: the (line, severity) pair is short.
	if err := c.ExecLua(`_G.orig_get = vim.diagnostic.get; vim.diagnostic.get = function() return {{ lnum = 0 }} end`); err != nil {
		t.Fatal(err)
	}
	marks, total, err := c.BufferDiagnosticsAndLines()
	if err != nil || len(marks) != 0 || total < 1 {
		t.Fatalf("short row: %v %d %v", marks, total, err)
	}
	if err := c.ExecLua(`vim.diagnostic.get = _G.orig_get`); err != nil {
		t.Fatal(err)
	}
	// A diagnostic whose buffer has no name still reads.
	if err := c.ExecLua(`_G.orig_name = vim.api.nvim_buf_get_name; vim.api.nvim_buf_get_name = function() return nil end`); err != nil {
		t.Fatal(err)
	}
	diags, err := c.AllDiagnostics()
	if err := c.ExecLua(`vim.api.nvim_buf_get_name = _G.orig_name`); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatalf("hole row: %v %v", diags, err)
	}
	// A reference whose file name cannot be made.
	startFakeLSP(t, c, map[string]any{"textDocument/references": []any{map[string]any{"uri": "file://" + p, "range": lspRange(0, 0)}}})
	if err := c.ExecLua(`_G.orig_uri = vim.uri_to_fname; vim.uri_to_fname = function() return nil end`); err != nil {
		t.Fatal(err)
	}
	refs, err := c.References()
	if err := c.ExecLua(`vim.uri_to_fname = _G.orig_uri`); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatalf("references: %v %v", refs, err)
	}
}

// LeakedClients waits for a live client up to the time it is given, then
// names it.
func TestLeakedClientsWaitsThenReports(t *testing.T) {
	TrackClients()
	c := isolatedClient(t)
	got := LeakedClients(60 * time.Millisecond)
	if len(got) == 0 {
		t.Fatalf("a live client is not listed: %v", got)
	}
	_ = c.Close()
}

// An nvim that does not leave when its pipe closes is killed after ten
// seconds, and Close returns.
func TestCloseKillsAnNvimThatStays(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 10 s")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nvim"), []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Close never returned")
	}
}

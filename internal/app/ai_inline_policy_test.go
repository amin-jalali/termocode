package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/debounce"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// countingProvider answers every request with a fixed text and counts calls.
type countingProvider struct {
	calls atomic.Int32
	reply string
}

func (p *countingProvider) Name() string        { return "fake" }
func (p *countingProvider) Configured() bool    { return true }
func (p *countingProvider) SupportsTools() bool { return false }
func (p *countingProvider) Stream(ctx context.Context, req ai.Request) <-chan ai.Chunk {
	p.calls.Add(1)
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: p.reply}
	ch <- ai.Chunk{Done: true}
	close(ch)
	return ch
}

// inlineNvim opens src in a real nvim with the AI Lua loaded and puts the
// caret in insert mode at (row, col) (1-based row, 0-based byte col).
func inlineNvim(t *testing.T, src string, row, col int) (*nvim.Client, *Model, *countingProvider) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	withConfigDir(t)
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	for _, meth := range []string{"termocode_ai_changed", "termocode_ai_cancel"} {
		_ = c.RegisterNotify(meth, nil)
	}
	if err := c.ExecLua(aiLua); err != nil {
		t.Fatalf("aiLua: %v", err)
	}
	path := filepath.Join(t.TempDir(), "main.go")
	_ = os.WriteFile(path, []byte(src), 0o644)
	if err := c.Command("edit " + path); err != nil {
		t.Fatal(err)
	}
	_ = c.Input("i")
	mode := ""
	for i := 0; i < 50 && !strings.HasPrefix(mode, "i"); i++ {
		mode, _ = c.EvalLuaString(`return vim.api.nvim_get_mode().mode`)
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.HasPrefix(mode, "i") {
		t.Fatalf("not in insert mode: %q", mode)
	}
	if err := c.ExecLuaArgs(`vim.api.nvim_win_set_cursor(0, {...})`, row, col); err != nil {
		t.Fatal(err)
	}
	prov := &countingProvider{}
	s := newAIState()
	s.provider = prov
	m := &Model{w: 100, h: 30, nvim: c, debounce: debounce.New(), ai: s}
	return c, m, prov
}

// No request on the empty line right after a closed function: this is
// where models invent a "new function" nobody asked for.
func TestAIInlineSkipsLineAfterClosedBlockNvim(t *testing.T) {
	_, m, prov := inlineNvim(t, "package main\n\nfunc a() {\n}\n\n", 5, 0)
	if cmd := m.startAIInline(false, false); cmd != nil {
		t.Fatal("auto trigger after a closed block should do nothing")
	}
	if cmd := m.startAIInline(true, true); cmd == nil {
		t.Fatal("manual trigger must still ask")
	} else {
		cmd()
	}
	if n := prov.calls.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1 (manual only)", n)
	}
}

// An empty line in a body waits for a longer pause before it asks.
func TestAIInlineBlankLineWaitsLongerNvim(t *testing.T) {
	_, m, prov := inlineNvim(t, "package main\n\nfunc a() {\n\tx := 1\n\t\n}\n", 5, 1)
	if cmd := m.startAIInline(false, false); cmd == nil {
		t.Fatal("want a second, longer debounce")
	}
	if n := prov.calls.Load(); n != 0 {
		t.Fatalf("asked before the long pause: %d", n)
	}
	cmd := m.startAIInline(false, true)
	if cmd == nil {
		t.Fatal("settled: should ask")
	}
	cmd()
	if n := prov.calls.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
}

// A reply that starts a new function on an empty body line is not shown.
func TestAIInlineDropsInventedFunctionNvim(t *testing.T) {
	c, m, _ := inlineNvim(t, "package main\n\nfunc a() {\n\tx := 1\n\t\n}\n", 5, 1)
	snap, ok := m.aiInlineSnapshotNow()
	if !ok {
		t.Fatal("snapshot")
	}
	gen := m.debounce.Next(aiInlineReqID)
	m.onAIInlineResult(aiInlineResultMsg{gen: gen, snap: snap, raw: "func helper() int {\n\treturn 1\n}"})
	if has, _ := c.EvalLuaString(`return _G._termocode_ai.has() and 'y' or ''`); has == "y" {
		t.Fatal("invented function shown as ghost text")
	}
}

// Typing the start of the ghost keeps the rest on screen, no new request.
func TestAIInlineTypeThroughNvim(t *testing.T) {
	c, m, prov := inlineNvim(t, "package main\n\nfunc a() {\n\tfmt.Pri\n}\n", 4, 8)
	snap, ok := m.aiInlineSnapshotNow()
	if !ok || snap.LineBefore != "\tfmt.Pri" {
		t.Fatalf("snapshot %+v", snap)
	}
	gen := m.debounce.Next(aiInlineReqID)
	m.onAIInlineResult(aiInlineResultMsg{gen: gen, snap: snap, raw: "fmt.Println(x)"})
	ghost := func() string {
		s, _ := c.EvalLuaString(`local g = _G._termocode_ai.has() and _G._termocode_ai.ghost; return g and g.text or ''`)
		return s
	}
	if g := ghost(); g != "ntln(x)" {
		t.Fatalf("ghost = %q", g)
	}
	changed := notifyHandlers["termocode_ai_changed"]
	_ = c.Input("n")
	if cmd := changed(m, nil); cmd != nil {
		t.Fatal("type-through should not schedule a request")
	}
	if g := ghost(); g != "tln(x)" {
		t.Fatalf("after typing n: ghost = %q", g)
	}
	_ = c.Input("z")
	if cmd := changed(m, nil); cmd == nil {
		t.Fatal("diverged: should schedule a new request")
	}
	if n := prov.calls.Load(); n != 0 {
		t.Fatalf("requests = %d", n)
	}
}

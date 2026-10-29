package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/debounce"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// nesProvider records each request and answers with reply.
type nesProvider struct {
	mu    sync.Mutex
	reqs  []ai.Request
	ctxs  []context.Context
	reply string
}

func (p *nesProvider) Name() string        { return "fake" }
func (p *nesProvider) Configured() bool    { return true }
func (p *nesProvider) SupportsTools() bool { return false }
func (p *nesProvider) Stream(ctx context.Context, req ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.ctxs = append(p.ctxs, ctx)
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: p.reply}
	ch <- ai.Chunk{Done: true}
	close(ch)
	return ch
}

func (p *nesProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.reqs)
}

// nesModel opens name (with src) in a real nvim, next edit on, and a
// Model wired to a recording provider. The buffer is seeded.
func nesModel(t *testing.T, name, src string) (*nvim.Client, *Model, *nesProvider) {
	t.Helper()
	c, _ := nesNvim(t, "")
	path := filepath.Join(t.TempDir(), name)
	_ = os.WriteFile(path, []byte(src), 0o644)
	if err := c.Command("edit " + path); err != nil {
		t.Fatal(err)
	}
	prov := &nesProvider{}
	s := newAIState()
	s.provider = prov
	m := &Model{w: 100, h: 30, nvim: c, debounce: debounce.New(), ai: s}
	buf, _ := c.EvalLuaString(`return tostring(vim.api.nvim_get_current_buf())`)
	m.aiNESSeed(atoiOr(buf, 0))
	return c, m, prov
}

func atoiOr(s string, d int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return d
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func runNES(t *testing.T, cmd tea.Cmd) aiNESResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no request sent")
	}
	msg, ok := cmd().(aiNESResultMsg)
	if !ok {
		t.Fatalf("got %T", msg)
	}
	return msg
}

// TestNextEditRequestAndShow: an edit is diffed against the seeded copy,
// the prompt carries the edit and the numbered window, and a valid answer
// shows the marker on the target line.
func TestNextEditRequestAndShow(t *testing.T) {
	c, m, prov := nesModel(t, "main.go", "package main\n\nfunc area(w, h int) int {\n\treturn w * h\n}\n")
	_ = c.Command("call setline(3, 'func area(width, h int) int {')")
	_ = c.Command("call cursor(3, 10)")
	prov.reply = `{"start":4,"end":4,"lines":["\treturn width * h"]}`
	msg := runNES(t, m.startAINES())
	if prov.calls() != 1 {
		t.Fatalf("calls %d", prov.calls())
	}
	req := prov.reqs[0]
	u := req.Messages[len(req.Messages)-1].Content
	for _, want := range []string{"-func area(w, h int) int {", "+func area(width, h int) int {", "4|\treturn w * h"} {
		if !strings.Contains(u, want) {
			t.Fatalf("prompt misses %q:\n%s", want, u)
		}
	}
	if req.Model != m.aiS().cfg.InlineModel {
		t.Fatalf("model %q, want the inline model %q", req.Model, m.aiS().cfg.InlineModel)
	}
	m.onAINESResult(msg)
	if got := nesMarks(t, c); !strings.Contains(got, "3:  ⇥ next edit") {
		t.Fatalf("marker not shown:\n%s", got)
	}
	// Nothing new since: no second request.
	if m.startAINES() != nil {
		t.Fatal("no change must send nothing")
	}
}

// TestNextEditBadAnswerShowsNothing: the edited line or junk is refused.
func TestNextEditBadAnswerShowsNothing(t *testing.T) {
	for _, reply := range []string{`{"start":3,"end":3,"lines":["x"]}`, "Sure, change line 4.", `{"none":true}`} {
		c, m, prov := nesModel(t, "main.go", "package main\n\nfunc area(w, h int) int {\n\treturn w * h\n}\n")
		_ = c.Command("call setline(3, 'func area(width, h int) int {')")
		prov.reply = reply
		m.onAINESResult(runNES(t, m.startAINES()))
		if got := nesMarks(t, c); got != "" {
			t.Fatalf("%q shown:\n%s", reply, got)
		}
	}
}

// TestNextEditStaleAndSuperseded: a reply for an older text or an older
// request is dropped; a new request cancels the one in flight.
func TestNextEditStaleAndSuperseded(t *testing.T) {
	c, m, prov := nesModel(t, "main.go", "package main\n\nfunc area(w, h int) int {\n\treturn w * h\n}\n")
	prov.reply = `{"start":4,"end":4,"lines":["\treturn width * h"]}`
	_ = c.Command("call setline(3, 'func area(width, h int) int {')")
	first := runNES(t, m.startAINES())
	_ = c.Command("call setline(1, 'package foo')")
	second := runNES(t, m.startAINES())
	if prov.ctxs[0].Err() == nil {
		t.Fatal("the first request was not cancelled")
	}
	m.onAINESResult(first)
	if got := nesMarks(t, c); got != "" {
		t.Fatalf("superseded reply shown:\n%s", got)
	}
	_ = c.Command("call setline(2, '// typed more')")
	m.onAINESResult(second)
	if got := nesMarks(t, c); got != "" {
		t.Fatalf("stale reply shown:\n%s", got)
	}
}

// TestNextEditPolicyBlocksSecretFiles: a secret-looking file is never sent.
func TestNextEditPolicyBlocksSecretFiles(t *testing.T) {
	c, m, prov := nesModel(t, ".env", "A=1\nB=2\n")
	_ = c.Command("call setline(1, 'A=3')")
	if cmd := m.startAINES(); cmd != nil {
		cmd()
	}
	if prov.calls() != 0 {
		t.Fatal("a secret file was sent to the model")
	}
}

// TestNextEditNotifyDebounces: an edit notify waits for the pause only
// when next edit is on.
func TestNextEditNotifyDebounces(t *testing.T) {
	withConfigDir(t)
	s := newAIState()
	s.provider = &nesProvider{}
	m := &Model{w: 100, h: 30, debounce: debounce.New(), ai: s}
	h := notifyHandlers["termocode_ai_edit"]
	if h == nil {
		t.Fatal("no termocode_ai_edit handler")
	}
	if h(m, []any{int64(1), "change"}) == nil {
		t.Fatal("an edit must start the pause")
	}
	off := false
	s.settings.NextEdit = &off
	if h(m, []any{int64(1), "change"}) != nil {
		t.Fatal("next edit off: no pause")
	}
	notifyHandlers["termocode_ai_nes"](m, []any{"shown"})
	if s.nes.shown != "shown" {
		t.Fatalf("state %q", s.nes.shown)
	}
	notifyHandlers["termocode_ai_nes"](m, []any{"gone"})
	if s.nes.shown != "" {
		t.Fatalf("state %q", s.nes.shown)
	}
}

// TestNextEditDiskReloadIsNotAnEdit: when the file changes on disk (git
// stash, an AI write, another tool) and nvim reloads the buffer, that is
// not the user's edit: no next edit request is sent for it. It used to
// send one with the reload as a "recent edit" (and on a fake AI server
// it took a reply meant for the chat).
func TestNextEditDiskReloadIsNotAnEdit(t *testing.T) {
	c, m, prov := nesModel(t, "a.txt", "alpha\nbravo\ncharlie\n")
	var mu sync.Mutex
	var notes [][]any
	_ = c.RegisterNotify("termocode_ai_edit", func(a []any) bool {
		mu.Lock()
		notes = append(notes, a)
		mu.Unlock()
		return false
	})
	path, _ := c.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`)
	if err := os.WriteFile(path, []byte("alpha\nBRAVO\ncharlie\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = c.Command("silent! checktime")
	waitLua(t, c, `vim.api.nvim_buf_get_lines(0, 1, 2, false)[1]`, "BRAVO")
	// Wait (a bounded time) for the reload's notify; without one the
	// request below shows the bug.
	var got [][]any
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		got = append([][]any(nil), notes...)
		mu.Unlock()
		if slices.ContainsFunc(got, func(a []any) bool { return len(a) > 1 && a[1] == "reload" }) {
			break
		}
	}
	for _, a := range got {
		notifyHandlers["termocode_ai_edit"](m, a)
	}
	if cmd := m.startAINES(); cmd != nil {
		cmd()
	}
	if prov.calls() != 0 {
		t.Fatalf("a disk reload sent %d next edit request(s); notifies %v", prov.calls(), got)
	}
	// A real edit after the reload still asks, and diffs against the
	// reloaded text.
	_ = c.Command("call setline(3, 'CHARLIE')")
	runNES(t, m.startAINES())
	u := prov.reqs[0].Messages[len(prov.reqs[0].Messages)-1].Content
	if strings.Contains(u, "-bravo") || !strings.Contains(u, "+CHARLIE") {
		t.Fatalf("prompt edits:\n%s", u)
	}
}

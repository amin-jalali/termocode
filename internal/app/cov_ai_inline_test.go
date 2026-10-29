package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/debounce"
)

// Inline ghost text (ai_inline.go): the toggle, the manual trigger, and
// every way an answer is dropped or shown.

// Every dropped answer: stale, cancelled, a provider error (toast only
// when asked by hand), no parse, filtered out, empty.
func TestCovAIInlineResultEnds(t *testing.T) {
	m := aiTestModel(t)
	m.aiS().provider = covScriptedProvider{}
	gen := m.debounce.Next(aiInlineReqID)
	if m.onAIInlineResult(aiInlineResultMsg{gen: gen + 5, raw: "x"}) != nil {
		t.Error("a stale answer did something")
	}
	if m.onAIInlineResult(aiInlineResultMsg{gen: gen, err: context.DeadlineExceeded}) != nil {
		t.Error("a timeout must be silent")
	}
	if m.onAIInlineResult(aiInlineResultMsg{gen: gen, err: errors.New("overloaded")}) != nil {
		t.Error("an automatic request error must be silent")
	}
	if m.onAIInlineResult(aiInlineResultMsg{gen: gen, manual: true, err: errors.New("overloaded")}) == nil || !strings.Contains(covToastText(&m), "AI completion failed") {
		t.Errorf("manual error toast = %q", covToastText(&m))
	}
	dir, _ := configDir()
	if b, _ := os.ReadFile(filepath.Join(dir, "errors.log")); !strings.Contains(string(b), "[ai] inline: overloaded") {
		t.Error("the error is not in errors.log")
	}
	m.toast = m.toast.Dismiss("")
	snap := aiInlineSnapshot{Filetype: "go", LineBefore: "\tx := ", Before: "package main\nfunc main() {\n\tx := "}
	for _, raw := range []string{"", "   "} {
		if m.onAIInlineResult(aiInlineResultMsg{gen: gen, snap: snap, raw: raw}) != nil {
			t.Errorf("automatic empty answer %q did something", raw)
		}
		if m.onAIInlineResult(aiInlineResultMsg{gen: gen, manual: true, snap: snap, raw: raw}) == nil {
			t.Errorf("manual empty answer %q: no toast", raw)
		}
	}
	if !strings.Contains(covToastText(&m), "AI has no suggestion here") {
		t.Errorf("toast = %q", covToastText(&m))
	}
	// A suggestion that only repeats what is already typed shows nothing.
	if m.onAIInlineResult(aiInlineResultMsg{gen: gen, snap: aiInlineSnapshot{LineBefore: "x := 1", LineAfter: ""}, raw: "x := 1"}) != nil {
		t.Error("an empty insert did something")
	}
}

// Toggle: without a provider it only explains; on / off are saved; a
// config that can not be written is an error toast.
func TestCovAIInlineToggleAndTrigger(t *testing.T) {
	m := aiTestModel(t)
	if m.aiToggleInline() == nil || m.aiTriggerInline() == nil {
		t.Error("no configure toast without a provider")
	}
	m.aiS().provider = covScriptedProvider{}
	was := m.aiS().settings.InlineEnabled()
	m.aiToggleInline()
	p, _ := ai.ConfigPath()
	if ai.LoadSettings(p).InlineEnabled() == was {
		t.Error("the toggle was not saved")
	}
	m.aiToggleInline()
	if ai.LoadSettings(p).InlineEnabled() != was {
		t.Error("the second toggle was not saved")
	}
	if m.aiTriggerInline() == nil || !strings.Contains(covToastText(&m), "works in insert mode in a file") {
		t.Errorf("trigger without an editor: %q", covToastText(&m))
	}
	_ = os.Remove(p)
	_ = os.MkdirAll(p, 0o755)
	if m.aiToggleInline() == nil || !strings.Contains(covToastText(&m), "Settings error") {
		t.Errorf("save error: %q", covToastText(&m))
	}
}

// The notify handlers: a change starts the debounce only when inline is
// on; Esc with "leave" forgets the last ghost.
func TestCovAIInlineNotifyHandlers(t *testing.T) {
	m := aiTestModel(t)
	changed, cancel := notifyHandlers["termocode_ai_changed"], notifyHandlers["termocode_ai_cancel"]
	if changed(&m, nil) != nil {
		t.Error("a change with inline off started a request")
	}
	m.aiS().provider = covScriptedProvider{}
	on := true
	m.aiS().settings.Inline = &on
	if changed(&m, nil) == nil {
		t.Error("a change with inline on did not start the debounce")
	}
	m.aiS().inlineCache.Remember(1, 1, "x", "yz")
	cancel(&m, []any{"leave"})
	if !m.aiS().inlineCache.Empty() {
		t.Error("leaving insert mode kept the ghost cache")
	}
	m.aiS().inlineCancel = func() {}
	m.aiS().inlineBusy = true
	cancel(&m, nil)
	if m.aiS().inlineBusy || m.aiS().inlineCancel != nil {
		t.Error("cancel left a request running")
	}
}

// With nvim: the manual trigger asks the model and shows the ghost; the
// automatic one waits longer on an empty line in a body; typing the
// start of the ghost keeps the rest without a new request.
func TestCovAIInlineFlowNvim(t *testing.T) {
	withConfigDir(t)
	c := covNvim(t)
	for _, meth := range []string{"termocode_ai_changed", "termocode_ai_cancel"} {
		_ = c.RegisterNotify(meth, nil)
	}
	m := Model{w: 100, h: 30, nvim: c, debounce: debounce.New(), ai: newAIState()}
	if _, ok := m.aiInlineSnapshotNow(); ok {
		t.Error("a snapshot before the Lua side is loaded")
	}
	if m.aiInlineTypeThrough() {
		t.Error("type-through with no ghost")
	}
	if err := c.ExecLua(aiLua); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.go")
	_ = os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tx := 1\n\t\n\tprintln(x)\n}\n"), 0o644)
	if err := c.Command("edit " + path); err != nil {
		t.Fatal(err)
	}
	if m.startAIInline(false, false) != nil {
		t.Error("an automatic request outside insert mode")
	}
	_ = c.Command("call cursor(5, 1)")
	_ = c.Input("A")
	if !covWaitMode(c, "i") {
		t.Fatal("not in insert mode")
	}
	m.aiS().provider = covScriptedProvider{text: "fmt.Println(x)"}
	if cmd := m.startAIInline(false, false); cmd == nil {
		t.Error("an empty body line did not wait for the longer pause")
	}
	cmd := m.startAIInline(true, true)
	res, ok := covCollect[aiInlineResultMsg](cmd)
	if !ok || res.raw != "fmt.Println(x)" {
		t.Fatalf("result %+v", res)
	}
	m.onAIInlineResult(res)
	if has, _ := c.EvalLuaString(`return _G._termocode_ai.has() and 'y' or ''`); has != "y" {
		t.Fatal("ghost not shown")
	}
	_ = c.Input("fm")
	covWaitLine(t, c, "\tfm")
	if !m.aiInlineTypeThrough() {
		t.Error("typing the start of the ghost did not keep it")
	}
	_ = c.Input("Z")
	covWaitLine(t, c, "\tfmZ")
	if m.aiInlineTypeThrough() || !m.aiS().inlineCache.Empty() {
		t.Error("typing something else kept the ghost")
	}
	_ = c.Input("\x1b")
}

func covWaitMode(c interface {
	EvalLuaString(string) (string, error)
}, prefix string) bool {
	for i := 0; i < 200; i++ {
		if mode, _ := c.EvalLuaString(`return vim.api.nvim_get_mode().mode`); strings.HasPrefix(mode, prefix) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func covWaitLine(t *testing.T, c interface {
	EvalLuaString(string) (string, error)
}, want string) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if l, _ := c.EvalLuaString(`return vim.api.nvim_get_current_line()`); l == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the line never became %q", want)
}

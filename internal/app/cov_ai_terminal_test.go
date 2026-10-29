package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// terminal_* AI tools (ai_terminal_tools.go): what they say without the
// editor, the card texts, empty text, and the shell-state helper.

func covTermTool(t *testing.T, call aiIDECaller, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(aiTerminalTools(call), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

func TestCovAITerminalWithoutEditor(t *testing.T) {
	m := &Model{}
	call := covSyncCall(m)
	ctx := context.Background()
	send := covTermTool(t, call, "terminal_send")
	if got := send.Describe(map[string]any{"text": "ls", "new_tab": true}); got != "Run in terminal: ls (new tab)" {
		t.Errorf("card = %q", got)
	}
	if got := send.Describe(map[string]any{"text": "ls", "tab": "2"}); got != "Run in terminal: ls (tab 2)" {
		t.Errorf("card = %q", got)
	}
	if _, err := send.Run(ctx, map[string]any{"text": "", "enter": false}); err == nil || err.Error() != "text is empty" {
		t.Errorf("empty text: %v", err)
	}
	if _, err := send.Run(ctx, map[string]any{"text": "ls"}); err == nil {
		t.Error("send without the editor ran")
	}
	read := covTermTool(t, call, "terminal_read")
	for _, a := range []map[string]any{{"lines": 100000, "wait_ms": -5}, {"wait_ms": 10_000_000}} {
		if _, err := read.Run(ctx, a); err == nil {
			t.Errorf("read %v without the editor ran", a)
		}
	}
	if got := read.Describe(map[string]any{"tab": "2"}); got != "Read terminal 2" {
		t.Errorf("read card = %q", got)
	}
	if _, err := m.aiTermTabOp("new", ""); err == nil || err.Error() != "the editor is not running" {
		t.Errorf("tab op without the editor: %v", err)
	}
	if _, _, err := m.aiTermPick("", false); err == nil {
		t.Error("pick without the editor")
	}
	if _, err := m.aiTermType(42, "x", &aiTermTurn{sent: map[int]aiTermSent{}}); err == nil {
		t.Error("typing into a tab that does not exist")
	}
	if _, err := m.aiTermSnapshot("", 5); err == nil {
		t.Error("snapshot without the editor")
	}
	stopped := func(context.Context, aiIDEFunc) (string, error) { return "", errAIChatStopped }
	if _, err := aiTermSend(ctx, stopped, &aiTermTurn{sent: map[int]aiTermSent{}}, "", false, "ls\r"); err != errAIChatStopped {
		t.Errorf("stopped chat: %v", err)
	}
	if _, err := aiTermList(ctx, stopped); err != errAIChatStopped {
		t.Errorf("stopped list: %v", err)
	}
	if out, err := aiTermList(ctx, call); err != nil || !strings.HasPrefix(out, "no terminal tabs") {
		t.Errorf("list without tabs: %q %v", out, err)
	}
	for b, want := range map[int]string{1: "running a command", 0: "idle", -1: "state unknown"} {
		if aiTermBusyWord(b) != want {
			t.Errorf("busy word %d", b)
		}
	}
	if aiTermBusy(ctx, 0) != -1 || aiTermBusy(ctx, 1<<22+7) != -1 {
		t.Error("a missing process must be state unknown")
	}
}

// With the editor: an unknown tab op and a tab that does not exist.
func TestCovAITerminalTabOpsNvim(t *testing.T) {
	c := covNvim(t)
	m := &Model{w: 100, h: 30, nvim: c}
	if _, err := m.aiTermTabOp("spin", ""); err == nil || !strings.Contains(err.Error(), `unknown op "spin"`) {
		t.Errorf("unknown op: %v", err)
	}
	if _, err := m.aiTermTabOp("focus", "9"); err == nil {
		t.Error("focus of a missing tab")
	}
	if _, err := m.aiTermSnapshot("", 5); err == nil {
		t.Error("snapshot with no tabs")
	}
}

// A real round trip: send opens the panel and a tab, read waits for the
// output, list and the tab ops see the tab, close removes it.
func TestCovAITerminalRoundTripNvim(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("SHELL", "/bin/sh")
	c := covNvim(t)
	_ = c.ExecLua(`vim.o.shell = '/bin/sh'`)
	m := &Model{w: 100, h: 30, nvim: c, terminalRows: 8, output: newOutputStore(), tasks: newTaskRunner(), problems: newProblemsPanel()}
	call := covSyncCall(m)
	ctx := context.Background()
	send, read := covTermTool(t, call, "terminal_send"), covTermTool(t, call, "terminal_read")
	if out, err := send.Run(ctx, map[string]any{"text": "echo cov-round-trip"}); err != nil || out == "" {
		t.Fatalf("send: %q %v", out, err)
	}
	out, err := read.Run(ctx, map[string]any{"wait_ms": 5000, "lines": 20})
	if err != nil || !strings.Contains(out, "cov-round-trip") {
		t.Errorf("read: %q %v", out, err)
	}
	if out, err := aiTermList(ctx, call); err != nil || !strings.Contains(out, "1. ") {
		t.Errorf("list: %q %v", out, err)
	}
	if out, err := m.aiTermTabOp("new", ""); err != nil || !strings.HasPrefix(out, "opened terminal tab 2") {
		t.Errorf("new: %q %v", out, err)
	}
	if out, err := m.aiTermTabOp("focus", "1"); err != nil || !strings.HasPrefix(out, "showing terminal tab 1") {
		t.Errorf("focus: %q %v", out, err)
	}
	if _, err := send.Run(ctx, map[string]any{"text": "echo two", "tab": "2"}); err != nil {
		t.Errorf("send to tab 2: %v", err)
	}
	if out, err := m.aiTermTabOp("close", "2"); err != nil || !strings.HasPrefix(out, "closed terminal tab 2") {
		t.Errorf("close: %q %v", out, err)
	}
	if _, err := send.Run(ctx, map[string]any{"text": "echo three", "new_tab": true}); err != nil {
		t.Errorf("send in a new tab: %v", err)
	}
}

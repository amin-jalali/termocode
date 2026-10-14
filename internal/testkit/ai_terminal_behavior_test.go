package testkit

import (
	"strings"
	"testing"
	"time"
)

// K3: the AI uses the visible integrated terminal (terminal_send,
// terminal_read, terminal_tabs). The shell is /bin/sh with a fixed
// prompt, so the screen is the same on every machine.

// plainShell makes the terminal tabs start /bin/sh with the prompt "$ ".
func plainShell(t testing.TB) {
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PS1", "$ ")
	t.Setenv("ENV", "") // no rc file for sh
}

// terminalRow reports whether a screen row shows the shell line want
// ("$ echo hello-k3"). The chat transcript never draws the "$ " prompt.
func terminalRow(d *Driver, want string) bool {
	return strings.Contains(screen(d), want)
}

// terminalBufsLua counts the terminal buffers in nvim.
const terminalBufsLua = `local n = 0
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == 'terminal' then n = n + 1 end
end
return tostring(n)`

// K3 rows of TestAIToolsTable (a separate block, so other Group K steps
// can add rows without merge conflicts).
var aiTerminalToolCases = []aiToolCase{
	{
		name: "terminal_read with no terminal is a clear error",
		call: toolCall{"terminal_read", map[string]any{}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "no terminal is open") || !strings.Contains(r[0], "terminal_send") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "terminal_tabs list with no terminal",
		call: toolCall{"terminal_tabs", map[string]any{"op": "list"}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "no terminal tabs") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name:    "terminal_send asks, then types in the visible terminal",
		call:    toolCall{"terminal_send", map[string]any{"text": "echo table-k3"}},
		setup:   func(d *Driver) { plainShell(d.T) },
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "sent to terminal tab 1") {
				t.Errorf("results %q", r)
			}
			if !eventually(d, func() bool { return terminalRow(d, "$ echo table-k3") }) {
				t.Errorf("the command is not in the terminal panel:\n%s", screen(d))
			}
		},
	},
	{
		name:  "terminal_tabs new opens a shell without asking",
		call:  toolCall{"terminal_tabs", map[string]any{"op": "new"}},
		setup: func(d *Driver) { plainShell(d.T) },
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "opened terminal tab 1") {
				t.Errorf("results %q", r)
			}
			if n := lua(d, terminalBufsLua); n != "1" {
				t.Errorf("%s terminal buffers, want 1:\n%s", n, screen(d))
			}
		},
	},
}

func init() { aiToolCases = append(aiToolCases, aiTerminalToolCases...) }

// Spec: Given the AI chat, when the model sends `echo hello-k3` to the
// terminal and the user clicks Allow, then the terminal panel opens and
// shows the command and its output, the editor keeps the keyboard (the
// shell does not steal it), and terminal_read returns "hello-k3".
func TestAITerminalSendThenRead(t *testing.T) {
	plainShell(t)
	s, srv := fakeToolModel(t,
		toolCall{"terminal_send", map[string]any{"text": "echo hello-k3"}},
		toolCall{"terminal_read", map[string]any{"lines": 20, "wait_ms": 5000}},
	)
	d := aiDriver(t, srv.URL, nil, false)
	open(d, "a.txt")
	askAI(d, "run it")
	if !d.WaitForText("AI wants to", 5*time.Second) {
		t.Fatalf("no confirm dialog:\n%s", screen(d))
	}
	clickAIAllow(d)
	if !d.WaitForText("fake-done", 10*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	if !eventually(d, func() bool { return terminalRow(d, "$ echo hello-k3") }) {
		t.Errorf("the command is not in the terminal panel:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 2 {
		t.Fatalf("results %q", r)
	}
	lines := strings.Split(r[1], "\n")
	found := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "hello-k3" {
			found = true
		}
	}
	if !found || !strings.Contains(r[1], "idle") {
		t.Errorf("terminal_read did not return the output or the idle state: %q", r[1])
	}
	if strings.HasSuffix(r[1], "\n") {
		t.Errorf("trailing blank lines were not stripped: %q", r[1])
	}
	if bt := lua(d, `return vim.bo.buftype`); bt == "terminal" {
		t.Errorf("the terminal took the nvim focus from the editor")
	}
}

// Spec: terminal_read with wait_ms waits for a running command to finish
// (the shell is idle again) and returns its last output, so the model can
// run tests and read the result in two calls.
func TestAITerminalReadWaitsForTheCommand(t *testing.T) {
	plainShell(t)
	s, srv := fakeToolModel(t,
		toolCall{"terminal_send", map[string]any{"text": "sleep 1; echo slow-k3-done"}},
		toolCall{"terminal_read", map[string]any{"wait_ms": 10000}},
	)
	d := aiDriver(t, srv.URL, nil, false, "run")
	open(d, "a.txt")
	askAI(d, "run it")
	if !d.WaitForText("fake-done", 15*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 2 || !strings.Contains(r[1], "\nslow-k3-done") || !strings.Contains(r[1], "idle") {
		t.Errorf("results %q", r)
	}
}

// Spec: terminal_tabs new / list / focus / close through the real app.
// Closing an idle shell asks (class ide); the tab is gone afterwards.
func TestAITerminalTabsNewListClose(t *testing.T) {
	plainShell(t)
	s, srv := fakeToolModel(t,
		toolCall{"terminal_tabs", map[string]any{"op": "new"}},
		toolCall{"terminal_tabs", map[string]any{"op": "new"}},
		toolCall{"terminal_tabs", map[string]any{"op": "focus", "tab": "1"}},
		toolCall{"terminal_tabs", map[string]any{"op": "list"}},
		toolCall{"terminal_tabs", map[string]any{"op": "close", "tab": 2}},
		toolCall{"terminal_tabs", map[string]any{"op": "list"}},
	)
	d := aiDriver(t, srv.URL, nil, false)
	open(d, "a.txt")
	askAI(d, "tabs")
	if !d.WaitForText("AI wants to", 10*time.Second) {
		t.Fatalf("no confirm dialog for close:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "Close terminal tab 2") {
		t.Errorf("the dialog does not say which tab closes:\n%s", screen(d))
	}
	clickAIAllow(d)
	if !d.WaitForText("fake-done", 10*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 6 {
		t.Fatalf("results %q", r)
	}
	if !strings.Contains(r[0], "opened terminal tab 1") || !strings.Contains(r[1], "opened terminal tab 2") {
		t.Errorf("new: %q", r[:2])
	}
	if !strings.Contains(r[2], "tab 1") {
		t.Errorf("focus: %q", r[2])
	}
	if !strings.Contains(r[3], "1. ") || !strings.Contains(r[3], "2. ") || !strings.Contains(r[3], "(active)") {
		t.Errorf("list: %q", r[3])
	}
	if !strings.Contains(r[4], "closed terminal tab 2") {
		t.Errorf("close: %q", r[4])
	}
	if strings.Contains(r[5], "2. ") || !strings.Contains(r[5], "1. ") {
		t.Errorf("list after close: %q", r[5])
	}
}

// Spec: closing a tab whose shell runs a command always asks, with no
// "Always allow" (even with the ide class auto-approved).
func TestAITerminalCloseBusyTabAlwaysAsks(t *testing.T) {
	plainShell(t)
	_, srv := fakeToolModel(t,
		toolCall{"terminal_send", map[string]any{"text": "sleep 30"}},
		toolCall{"terminal_read", map[string]any{"wait_ms": 800}},
		toolCall{"terminal_tabs", map[string]any{"op": "close", "tab": 1}},
	)
	d := aiDriver(t, srv.URL, nil, false, "run", "ide")
	open(d, "a.txt")
	askAI(d, "stop it")
	if !d.WaitForText("AI wants to", 10*time.Second) {
		t.Fatalf("no confirm dialog for closing a busy tab:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Always allow") {
		t.Errorf("a busy tab close offers Always allow:\n%s", screen(d))
	}
	clickAIAllow(d)
	if !d.WaitForText("fake-done", 10*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
}

// Spec: after a command typed in the terminal creates a file, the next
// terminal_read refreshes the IDE (the explorer shows the file), as
// run_command does.
func TestAITerminalReadRefreshesExplorer(t *testing.T) {
	plainShell(t)
	_, srv := fakeToolModel(t,
		toolCall{"terminal_send", map[string]any{"text": "echo hi > made-by-term.txt"}},
		toolCall{"terminal_read", map[string]any{"wait_ms": 5000}},
	)
	d := aiDriver(t, srv.URL, nil, false, "run")
	open(d, "a.txt")
	askAI(d, "make it")
	if !d.WaitForText("fake-done", 10*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	if !eventually(d, func() bool { return explorerShows(d, "made-by-term.txt") }) {
		t.Errorf("explorer does not show the new file:\n%s", screen(d))
	}
}

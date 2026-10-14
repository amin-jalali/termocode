package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func TestAITermResolve(t *testing.T) {
	tabs := []terminalTab{{BufID: 5, Name: "web"}, {BufID: 7, Name: "api"}, {BufID: 9, Name: "api"}}
	cases := []struct {
		arg     string
		want    int
		wantErr string
	}{
		{"", 1, ""},
		{"1", 0, ""},
		{" 3 ", 2, ""},
		{"web", 0, ""},
		{"4", 0, "no terminal tab 4"},
		{"0", 0, "no terminal tab 0"},
		{"api", 0, "use the tab number"},
		{"db", 0, `no terminal tab named "db"`},
	}
	for _, c := range cases {
		got, err := aiTermResolve(tabs, 1, c.arg)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%q: err %v, want %q", c.arg, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: %d %v, want %d", c.arg, got, err, c.want)
		}
	}
	if _, err := aiTermResolve(nil, 0, ""); err != errAINoTerminal {
		t.Errorf("no tabs: %v", err)
	}
}

func TestAITermTail(t *testing.T) {
	lines := []string{"a", "b  ", "c", "", "   ", ""}
	if got := aiTermTail(lines, 2); got != "b\nc" {
		t.Errorf("tail %q", got)
	}
	if got := aiTermTail([]string{"", " "}, 5); got != "" {
		t.Errorf("blank %q", got)
	}
	long := []string{strings.Repeat("x", aiTermMaxBytes+10), "end"}
	if got := aiTermTail(long, 10); len(got) > aiTermMaxBytes+20 || !strings.HasSuffix(got, "\nend") || !strings.HasPrefix(got, "… [cut]") {
		t.Errorf("cap: len %d", len(got))
	}
}

// The terminal echoes typed text before a busy (or slow) shell reads it;
// that echo alone must not count as "the command ran" (a read with
// wait_ms then returned before `sleep 1; echo x` had started).
func TestAITermStarted(t *testing.T) {
	s := aiTermSent{tick: 10, line: "sleep 1; echo x"}
	cases := []struct {
		name  string
		tick  int
		lines []string
		want  bool
	}{
		{"nothing changed", 10, []string{"$ "}, false},
		{"only the echo", 11, []string{"$ sleep 1; echo x", ""}, false},
		{"output", 12, []string{"$ sleep 1; echo x", "x", "$"}, true},
		{"new prompt", 12, []string{"$ sleep 1; echo x", "$"}, true},
	}
	for _, c := range cases {
		if got := aiTermStarted(s, true, c.tick, c.lines); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if !aiTermStarted(aiTermSent{}, false, 3, nil) {
		t.Error("a tab the agent never typed in counts as started")
	}
}

// terminal_send asks (class run); terminal_read never asks; terminal_tabs
// list / new / focus run at once and close always asks.
func TestAITerminalToolGates(t *testing.T) {
	call := func(ctx context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&Model{})
		return out, err
	}
	tools := aiTerminalTools(call)
	send, _ := ai.FindTool(tools, "terminal_send")
	if !send.Mutating || send.Class != ai.ClassRun || send.Gate != nil {
		t.Errorf("terminal_send: mutating %v class %q", send.Mutating, send.Class)
	}
	read, _ := ai.FindTool(tools, "terminal_read")
	if read.Mutating || read.Gate != nil {
		t.Error("terminal_read must not ask")
	}
	tabs, _ := ai.FindTool(tools, "terminal_tabs")
	for _, op := range []string{"list", "new", "focus"} {
		if mut, _ := tabs.Gate(map[string]any{"op": op}); mut {
			t.Errorf("terminal_tabs %s asks", op)
		}
	}
	// No such tab (bare Model): close still asks, with no "Always allow".
	if mut, class := tabs.Gate(map[string]any{"op": "close", "tab": "1"}); !mut || class != "" {
		t.Errorf("close of an unknown tab: %v %q", mut, class)
	}
	if err := tabs.Validate(map[string]any{"op": "rename"}); err == nil {
		t.Error("unknown op accepted")
	}
	if err := tabs.Validate(map[string]any{"op": "close"}); err == nil {
		t.Error("close without tab accepted")
	}
}

// run_command tells the model when to use the visible terminal instead.
func TestRunCommandPointsToTerminalSend(t *testing.T) {
	rc, _ := ai.FindTool(ai.WorkspaceTools(ai.Workspace{Root: "."}), "run_command")
	if !strings.Contains(rc.Description, "terminal_send") || !strings.Contains(rc.Description, "HIDDEN") {
		t.Errorf("run_command description: %q", rc.Description)
	}
}

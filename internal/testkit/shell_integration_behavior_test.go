package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// Shell integration (U9a), the "✦ Fix?" chip (U9b) and CLI agent marks
// (U8) with a real nvim and a real bash. bash loads the integration
// script; the sandbox HOME has no ~/.bashrc.

// needBash makes terminal tabs start bash (skips without bash).
func needBash(t *testing.T) {
	t.Helper()
	out, err := spawn.Command("sh", "-c", "command -v bash").Output()
	p := strings.TrimSpace(string(out))
	if err != nil || p == "" {
		t.Skip("bash is not installed")
	}
	t.Setenv("SHELL", p)
	t.Setenv("PS1", "$ ")
}

// termTabs is the terminal tab state.
func termTabs(d *Driver) []app.TerminalTabState { return d.Model().TerminalTabStates() }

// waitTerm waits (up to 10 s) until tab i satisfies cond.
func waitTerm(d *Driver, i int, what string, cond func(app.TerminalTabState) bool) app.TerminalTabState {
	d.T.Helper()
	var last app.TerminalTabState
	ok := d.WaitFor(func() bool {
		tabs := termTabs(d)
		if i >= len(tabs) {
			return false
		}
		last = tabs[i]
		return cond(last)
	}, 10*time.Second)
	if !ok {
		d.T.Fatalf("terminal tab %d: %s; last state %+v\n%s", i, what, last, screen(d))
	}
	return last
}

// termSend types text into the shell of terminal tab buffer i (in the
// order the tabs were opened).
func termSend(d *Driver, i int, text string) {
	d.T.Helper()
	lua(d, `local i, text = ...
local n = 0
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == 'terminal' then
    n = n + 1
    if n == i + 1 then vim.fn.chansend(vim.bo[b].channel, text) end
  end
end
return ''`, i, text)
}

// openBashTerminal opens the panel and waits for the integrated prompt.
func openBashTerminal(d *Driver) {
	d.T.Helper()
	d.Palette("toggle-terminal")
	waitTerm(d, 0, "no shell integration", func(s app.TerminalTabState) bool { return s.Integrated })
}

// Spec (T1): Given a bash terminal tab, when the user runs `false`, then
// the tab knows the command and exit code 1 from the shell itself (no
// title poll), and the chip is set but not drawn while AI is not set up.
// When the user runs `cd notes`, the tab follows the folder (OSC 7).
func TestShellIntegrationBashFalseAndCd(t *testing.T) {
	needBash(t)
	d := editorDriver(t, nil)
	open(d, "a.txt")
	openBashTerminal(d)
	termSend(d, 0, "false\n")
	st := waitTerm(d, 0, "no failed `false`", func(s app.TerminalTabState) bool {
		return s.LastCmd == "false" && s.LastExit == 1 && !s.Busy
	})
	if !st.FixChip {
		t.Errorf("no fix chip after a failed command: %+v", st)
	}
	if strings.Contains(screen(d), "Fix?") {
		t.Errorf("the Fix chip shows while AI is not set up:\n%s", screen(d))
	}
	termSend(d, 0, "cd notes\n")
	st = waitTerm(d, 0, "cwd did not follow cd", func(s app.TerminalTabState) bool {
		return s.Cwd == filepath.Join(d.Dir, "notes") && s.Name == "notes"
	})
	if st.LastExit != 0 || st.FixChip {
		t.Errorf("a good command keeps the failure: %+v", st)
	}
}

// Spec (T1): "terminal_shell_integration": false starts bash as before:
// no events, the tab keeps its old state.
func TestShellIntegrationOff(t *testing.T) {
	needBash(t)
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles(),
		Config: map[string]string{"config.json": `{"animations": "off", "images": "blocks", "setup_done": true, "terminal_shell_integration": false}`}})
	d.Palette("toggle-terminal")
	waitTerm(d, 0, "no tab", func(app.TerminalTabState) bool { return true })
	termSend(d, 0, "false; echo off-$((40+2))\n")
	if !d.WaitForText("off-42", 10*time.Second) {
		t.Fatalf("the shell did not run:\n%s", screen(d))
	}
	d.Settle(300 * time.Millisecond)
	if st := termTabs(d)[0]; st.Integrated || st.LastCmd != "" || st.LastExit != -1 {
		t.Errorf("integration ran while off: %+v", st)
	}
}

// Spec (T2): Given AI is set up and `ls` of a missing folder fails in
// the terminal, then the tab shows "✦ Fix?"; nothing is sent yet. A
// click sends the command, the exit code and the output (secrets
// redacted) to the chat, and the chip goes away.
func TestFixChipSendsTheFailedCommand(t *testing.T) {
	needBash(t)
	s, srv := newAIScript(t, aiReply{text: "fake-fix-answer"})
	d := aiDriver(t, srv.URL, nil, false)
	open(d, "a.txt")
	openBashTerminal(d)
	termSend(d, 0, "echo token sk-ant-abcdefghijklmnop; ls /nonexistent-u9b\n")
	waitTerm(d, 0, "no failed ls", func(st app.TerminalTabState) bool { return st.LastExit == 2 && st.FixChip })
	if !d.WaitForText("Fix?", 5*time.Second) {
		t.Fatalf("no Fix chip on the tab:\n%s", screen(d))
	}
	s.mu.Lock()
	n := len(s.bodies)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d AI requests before the click", n)
	}
	// The chip comes with the shell's end event; nvim draws the output a
	// moment later. A person clicks after reading it.
	if !d.WaitForText("No such file", 5*time.Second) {
		t.Fatalf("no ls error on screen:\n%s", screen(d))
	}
	clickTextOn(d, "Fix?")
	if !d.WaitForText("fake-fix-answer", 10*time.Second) {
		t.Fatalf("no answer in the chat:\n%s", screen(d))
	}
	s.mu.Lock()
	body := strings.Join(s.bodies, "\n")
	s.mu.Unlock()
	for _, want := range []string{"exit code 2", "nonexistent-u9b", "No such file", "[REDACTED]"} {
		if !strings.Contains(body, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "sk-ant-abcdefghijklmnop") {
		t.Errorf("a secret reached the AI")
	}
	if termTabs(d)[0].FixChip {
		t.Error("the chip stays after the click")
	}
}

// fakeClaude puts a `claude` script on PATH that prints Claude Code's
// permission prompt after delay and waits for an answer.
func fakeClaude(t *testing.T, delay string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\nsleep " + delay + "\nprintf '\\n Bash command\\n\\n   rm -rf build\\n\\n Do you want to proceed?\\n \\342\\235\\257 1. Yes\\n   2. No, and tell Claude what to do differently (esc)\\n'\nread answer\necho answered-$answer\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Spec (U8): Given `claude` runs in terminal tab 1 and the user moved to
// tab 2, when Claude Code shows a permission prompt, then tab 1 is named
// "Claude Code" with the waiting mark and a toast says "Claude Code is
// waiting for you". After the answer the agent quits and the tab is a
// plain shell again.
func TestAgentWaitingMarkAndToast(t *testing.T) {
	needBash(t)
	fakeClaude(t, "1")
	d := editorDriver(t, nil)
	open(d, "a.txt")
	openBashTerminal(d)
	termSend(d, 0, "claude\n")
	waitTerm(d, 0, "claude not seen", func(s app.TerminalTabState) bool { return s.Agent == "claude" })
	d.Palette("terminal-new-tab")
	st := waitTerm(d, 0, "not waiting", func(s app.TerminalTabState) bool { return s.AgentState == "waiting" })
	if st.Name != "Claude Code" {
		t.Errorf("tab name %q", st.Name)
	}
	if !d.WaitForText("Claude Code is waiting for you", 5*time.Second) {
		t.Errorf("no toast:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "Claude Code") {
		t.Errorf("tab bar lacks the agent name:\n%s", screen(d))
	}
	termSend(d, 0, "1\n")
	st = waitTerm(d, 0, "agent did not end", func(s app.TerminalTabState) bool { return s.Agent == "" && !s.Busy })
	if st.Name == "Claude Code" {
		t.Errorf("tab still named after the agent: %+v", st)
	}
}

// Spec (U8): "Agent: Start Claude Code" opens a terminal tab in the
// workspace running claude; its screen state is tracked, and no toast
// shows while the user looks at that tab. A second start shows the
// same tab instead of a second copy.
func TestAgentStartFromPalette(t *testing.T) {
	needBash(t)
	fakeClaude(t, "0.3")
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Palette("agent-start-claude")
	st := waitTerm(d, 0, "not waiting", func(s app.TerminalTabState) bool {
		return s.Agent == "claude" && s.AgentState == "waiting"
	})
	if st.Name != "Claude Code" || st.Cwd != d.Dir {
		t.Errorf("tab %+v, want Claude Code in %s", st, d.Dir)
	}
	d.Settle(200 * time.Millisecond)
	if strings.Contains(screen(d), "is waiting for you") {
		t.Errorf("toast while the agent tab is in focus:\n%s", screen(d))
	}
	d.Palette("agent-start-claude")
	d.Settle(200 * time.Millisecond)
	if n := len(termTabs(d)); n != 1 {
		t.Errorf("%d tabs after a second start, want 1", n)
	}
}

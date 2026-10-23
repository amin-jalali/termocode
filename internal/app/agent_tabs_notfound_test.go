package app

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// Spec (MCP-8): starting an agent whose program is not on PATH says what
// to do next, for every agent: install it, then start it again, and where
// the guide is. Nothing starts.
func TestStartAgentNotInstalledSaysWhatNext(t *testing.T) {
	defer func(f func(string) (string, error)) { agentLookPath = f }(agentLookPath)
	agentLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	for _, a := range agents.Registry {
		m := aiTestModel(t)
		m.nvim = &nvim.Client{}
		if cmd := m.startAgent(a); cmd == nil {
			t.Errorf("%s: no toast command", a.ID)
		}
		got := strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(
			ansi.Strip(strings.Join(m.toast.View(200, 40), " ")))), " ")
		for _, want := range []string{a.Title + " is not installed", "`" + a.Bin + "` is not on PATH", "Install " + a.Title + ", then start it again", "guides/agents.md"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: toast has no %q: %q", a.ID, want, got)
			}
		}
		if len(m.terminalTabs) != 0 {
			t.Errorf("%s: a tab opened", a.ID)
		}
	}
}

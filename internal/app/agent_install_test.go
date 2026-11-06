package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/profileform"
)

// Spec (AGX-3): a CLI termocode installed (a shim in its tools/bin) counts
// as installed, as one on PATH does; a missing one does not.
func TestAgentLookPathFindsManagedShim(t *testing.T) {
	tools := t.TempDir()
	t.Setenv(lspinstall.ToolsDirEnv, tools)
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(tools, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(tools, "bin", "amp")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err := agentLookPathDefault("amp"); err != nil || p != shim {
		t.Errorf("amp = %q, %v", p, err)
	}
	if _, err := agentLookPathDefault("qwen"); err == nil {
		t.Error("qwen found")
	}
	if err := os.WriteFile(filepath.Join(tools, "bin", "crush"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := agentLookPathDefault("crush"); err == nil {
		t.Error("a file that is not executable counts")
	}
}

// agentInstallModel is a model with no agent on PATH, a fake installer
// that records what it installs, and a recorder of sign-in lines.
func agentInstallModel(t *testing.T) (*Model, *[]string, *[]string) {
	t.Helper()
	fakeAgentLookPath(t)
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	var installed, lines []string
	restore := SetToolInstaller(func(_ context.Context, tl lspinstall.Tool, _ lspinstall.Progress) (string, error) {
		installed = append(installed, tl.Name)
		return "", nil
	})
	t.Cleanup(restore)
	old := agentPickRunLine
	agentPickRunLine = func(_ *Model, line string) tea.Cmd { lines = append(lines, line); return nil }
	t.Cleanup(func() { agentPickRunLine = old })
	m := aiTestModel(t)
	return &m, &installed, &lines
}

// Spec (AGX-3): Enter on a NOT INSTALLED row opens the install card: Not
// now first and focused, the exact command and folder, no sudo. Not now
// installs nothing; Install runs the installer of the agent's tool; when
// it is done the sign-in card shows the CLI's own login line, Not now
// first; Sign in runs that line in a new terminal tab.
func TestAgentInstallThenSignIn(t *testing.T) {
	m, installed, lines := agentInstallModel(t)
	m.onAgentPicked("agent:amp")
	if !m.confirmOpen || m.confirmKind != confirmKindAgentInstall || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Not-now-first install card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	box := strings.Join(strings.Fields(ansi.Strip(m.confirm.Box())), " ")
	for _, want := range []string{"Install Amp?", "npm install @sourcegraph/amp@latest", "tools", "No sudo"} {
		if !strings.Contains(box, want) {
			t.Errorf("card has no %q:\n%s", want, box)
		}
	}
	m.answerAIRulesForTest("cancel")
	if len(*installed) != 0 || m.lspMgr != nil && m.lspMgr.installing["amp"] {
		t.Fatal("Not now installed")
	}

	m.onAgentPicked("agent:amp")
	m.answerAIRulesForTest("install")
	if m.lspMgr == nil || !m.lspMgr.installing["amp"] {
		t.Fatal("Install did not start the installer")
	}
	m.onToolInstallEvent(lspInstallEventMsg{name: "amp", done: true})
	if !m.confirmOpen || m.confirmKind != confirmKindAgentSignIn || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no Not-now-first sign-in card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "amp login") {
		t.Errorf("sign-in card:\n%s", box)
	}
	m.answerAIRulesForTest("cancel")
	if len(*lines) != 0 {
		t.Fatal("Not now ran the sign-in")
	}
	m.openAgentSignIn(mustAgent(t, "amp"), true)
	m.answerAIRulesForTest("signin")
	if strings.Join(*lines, ";") != "amp login" {
		t.Errorf("lines %v", *lines)
	}
}

func mustAgent(t *testing.T, id string) agents.Agent {
	t.Helper()
	a, ok := agents.ByID(id)
	if !ok {
		t.Fatalf("no agent %s", id)
	}
	return a
}

// Spec (AGX-3): a CLI with no sign-in command (Qwen Code asks on its
// first start) gets a Start card after the install; Start starts it.
func TestAgentInstallThenStart(t *testing.T) {
	m, _, lines := agentInstallModel(t)
	var started []string
	old := agentPickStart
	agentPickStart = func(_ *Model, a agents.Agent) tea.Cmd { started = append(started, a.ID); return nil }
	t.Cleanup(func() { agentPickStart = old })
	m.onAgentPicked("agent:qwen")
	m.answerAIRulesForTest("install")
	m.onToolInstallEvent(lspInstallEventMsg{name: "qwen-code", done: true})
	if !m.confirmOpen || m.confirmKind != confirmKindAgentSignIn || m.confirm.SafeID() != "cancel" {
		t.Fatal("no card after the install")
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "first start") {
		t.Errorf("card:\n%s", box)
	}
	m.answerAIRulesForTest("start")
	if strings.Join(started, ",") != "qwen" || len(*lines) != 0 {
		t.Errorf("started %v lines %v", started, *lines)
	}
}

// Spec (AGX-3): an agent with no package-manager install (Cursor CLI)
// opens no card and runs nothing: a toast says to install it by hand.
func TestAgentWithoutOneClickInstall(t *testing.T) {
	m, installed, _ := agentInstallModel(t)
	m.onAgentPicked("agent:cursor")
	if m.confirmOpen || len(*installed) != 0 {
		t.Fatal("a card or an install for cursor")
	}
	got := strings.Join(strings.Fields(ansi.Strip(strings.Join(m.toast.View(200, 40), " "))), " ")
	if !strings.Contains(got, "Cursor CLI has no one-click install") {
		t.Errorf("toast %q", got)
	}
}

// Spec (AGX-3): Enter on an installed Codex that is signed out opens the
// sign-in card with `codex login` (not a start); a signed-in one starts.
func TestAgentPickerSignedOutAsksToSignIn(t *testing.T) {
	m, _, lines := agentInstallModel(t)
	fakeAgentLookPath(t, "codex")
	var started []string
	old := agentPickStart
	agentPickStart = func(_ *Model, a agents.Agent) tea.Cmd { started = append(started, a.ID); return nil }
	t.Cleanup(func() { agentPickStart = old })
	m.aiS().cliStates = map[string]profileform.CLIState{ai.CLICodex: profileform.CLILoggedOut}
	m.onAgentPicked("agent:codex")
	if !m.confirmOpen || m.confirmKind != confirmKindAgentSignIn || len(started) != 0 {
		t.Fatalf("no sign-in card: open %v started %v", m.confirmOpen, started)
	}
	m.answerAIRulesForTest("signin")
	if strings.Join(*lines, ";") != "codex login" {
		t.Errorf("lines %v", *lines)
	}
	m.aiS().cliStates[ai.CLICodex] = profileform.CLIReady
	m.onAgentPicked("agent:codex")
	if m.confirmOpen || strings.Join(started, ",") != "codex" {
		t.Errorf("signed-in codex: card %v started %v", m.confirmOpen, started)
	}
}

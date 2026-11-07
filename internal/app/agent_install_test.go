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

// Spec (AGX-3, CMP-3): an agent with no package-manager install (Cursor
// CLI) installs nothing: a card (Not now first) names its own install
// page; Not now opens nothing, Open install page opens it.
func TestAgentWithoutOneClickInstall(t *testing.T) {
	m, installed, lines := agentInstallModel(t)
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { osOpen = old })
	cursor, _ := agents.ByID("cursor")
	m.onAgentPicked("agent:cursor")
	if !m.confirmOpen || m.confirmKind != confirmKindAgentInstallPage || m.confirm.SafeID() != "cancel" {
		t.Fatalf("no install-page card: open %v kind %v", m.confirmOpen, m.confirmKind)
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"Cursor CLI has no one-click install", cursor.InstallURL} {
		if !strings.Contains(strings.Join(strings.Fields(box), " "), want) {
			t.Errorf("card has no %q:\n%s", want, box)
		}
	}
	m.answerAIRulesForTest("cancel")
	if len(opened) != 0 || len(*installed) != 0 || len(*lines) != 0 {
		t.Fatalf("Not now did something: opened %v installed %v lines %v", opened, *installed, *lines)
	}
	m.onAgentPicked("agent:cursor")
	m.answerAIRulesForTest("open")
	if strings.Join(opened, ",") != cursor.InstallURL || len(*installed) != 0 || len(*lines) != 0 {
		t.Errorf("opened %v installed %v lines %v", opened, *installed, *lines)
	}
}

// Spec (CMP-3): every preset without a one-click install gets the
// install-page card, never a dead end.
func TestEveryManualPresetHasInstallPageCard(t *testing.T) {
	m, _, _ := agentInstallModel(t)
	for _, a := range agents.Registry {
		if a.Tool != "" {
			continue
		}
		m.confirmOpen = false
		m.onAgentPicked("agent:" + a.ID)
		if !m.confirmOpen || m.confirmKind != confirmKindAgentInstallPage {
			t.Errorf("%s: no install-page card", a.ID)
		}
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

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── One-click install and sign-in (AGX-3) ───────────────────────────────
//
// Enter on a NOT INSTALLED row of Agent: Start… shows the install card
// (Not now first): the exact npm / pip command and termocode's tools
// folder. Install runs the language-server installer (lsp_manager.go)
// in the background. When it is done a second card offers the CLI's own
// sign-in command in a new terminal tab, or a start for a CLI that asks on
// its first start. termocode never sees the password or token.

const (
	confirmKindAgentInstall confirmKindEnum = 3701
	confirmKindAgentSignIn  confirmKindEnum = 3702
)

// agentLookPathDefault finds an agent's program on PATH, then in
// termocode's tools/bin (a one-click install puts its shim there; the
// terminal tabs have it on PATH).
func agentLookPathDefault(bin string) (string, error) {
	if p, err := exec.LookPath(bin); err == nil {
		return p, nil
	}
	if dir, err := lspinstall.BinDir(); err == nil {
		p := filepath.Join(dir, bin)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// agentPickRunLine types line into a new terminal tab (a var for tests).
var agentPickRunLine = func(m *Model, line string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.debugRunInTerminal(aiWorkspaceRoot(), line)
	return nil
}

// agentForTool is the preset whose one-click install is tool.
func agentForTool(tool string) (agents.Agent, bool) {
	for _, a := range agents.Registry {
		if a.Tool != "" && a.Tool == tool {
			return a, true
		}
	}
	return agents.Agent{}, false
}

// onAgentPickedRow is Enter on an agent row: start, sign in or install.
func (m *Model) onAgentPickedRow(a agents.Agent) tea.Cmd {
	if agentInstalled(a) {
		if agentSignInState(m.aiS().cliStates, a.ID) == profileform.CLILoggedOut && a.Login != "" {
			m.openAgentSignIn(a, false)
			return nil
		}
		return agentPickStart(m, a)
	}
	t, ok := lspinstall.Lookup(a.Tool)
	if a.Tool == "" || !ok {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, a.Title+" has no one-click install",
			"Install it with its own installer, then F1 → Agent: Start…")
		return c
	}
	m.ensureLSPMgr()
	if m.lspMgr.installing[t.Name] {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, a.Title+" is installing…", "progress shows in toasts")
		return c
	}
	if err := toolInstaller().check(t); err != nil {
		return m.toolErrorToast(t, err, "") // no npm / python here: say what to do
	}
	dir, _ := lspinstall.ToolDir(t.Name)
	plan, err := lspinstall.BuildPlan(t, dir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return m.toolErrorToast(t, err, "")
	}
	body := "Runs: " + plan.HumanCommand() + "\nInto: " + dir +
		"\n\nNo sudo and nothing global: only termocode's tools folder changes."
	if a.Login != "" {
		body += "\nThen you can sign in with `" + a.Login + "` in a terminal tab (you see it first)."
	}
	m.aiS().agentPickPending = a.ID
	m.confirm = confirm.Risky("Install "+a.Title+"?", body,
		confirm.Button{ID: "cancel", Title: "Not now"},
		confirm.Button{ID: "install", Title: "Install", Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentInstall
	return nil
}

// openAgentSignIn shows the sign-in card of a (or the Start card of a CLI
// that signs in on its first start). justInstalled words the title.
func (m *Model) openAgentSignIn(a agents.Agent, justInstalled bool) {
	title, body := "Sign in to "+a.Title+"?", ""
	btn := confirm.Button{ID: "signin", Title: "Sign in", Style: confirm.StylePrimary}
	if a.Login != "" {
		body = "Runs in a new terminal tab:\n\n    " + a.Login +
			"\n\nThe CLI opens its own sign-in page. termocode never sees your password or token."
	} else {
		title = "Start " + a.Title + "?"
		body = a.Title + " asks you to sign in on its first start, in its own terminal tab."
		btn = confirm.Button{ID: "start", Title: "Start", Style: confirm.StylePrimary}
	}
	if justInstalled {
		body = a.Title + " is installed.\n\n" + body
	}
	m.aiS().agentPickPending = a.ID
	m.confirm = confirm.Risky(title, body, confirm.Button{ID: "cancel", Title: "Not now"}, btn).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentSignIn
}

// answerAgentPickCard runs the install / sign-in card's answer. Not now
// and Esc do nothing.
func (m *Model) answerAgentPickCard(kind confirmKindEnum, id string) tea.Cmd {
	s := m.aiS()
	a, ok := agents.ByID(s.agentPickPending)
	s.agentPickPending = ""
	if !ok {
		return nil
	}
	switch {
	case kind == confirmKindAgentInstall && id == "install":
		t, ok := lspinstall.Lookup(a.Tool)
		if !ok {
			return nil
		}
		return m.startToolInstall(t)
	case kind == confirmKindAgentSignIn && id == "signin" && a.Login != "":
		return agentPickRunLine(m, a.Login)
	case kind == confirmKindAgentSignIn && id == "start":
		return agentPickStart(m, a)
	}
	return nil
}

// onAgentInstalled follows a finished one-click install: the picker rows
// update and the sign-in (or Start) card opens.
func (m *Model) onAgentInstalled(t lspinstall.Tool) tea.Cmd {
	a, ok := agentForTool(t.Name)
	if !ok {
		return nil
	}
	m.refreshAgentPicker()
	if m.confirmOpen { // never replace a card the user is answering
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Info, "✓ "+a.Title+" installed", "F1 → Agent: Start… to sign in and start it")
		return c
	}
	m.openAgentSignIn(a, true)
	return nil
}

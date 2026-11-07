package app

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CTX-33: deploy a compose stack from git. A prompt takes "url" or
// "url#ref"; a Cancel-first card shows the exact clone (or pull) and
// `compose up -d` commands; Deploy runs them in a visible tab. The stacks
// live under services.GitDeployBase(), never in the workspace.

const (
	promptKindServicesDeployGit  promptKindEnum  = 5341
	confirmKindServicesDeployGit confirmKindEnum = 5341
)

const svcTabDeployGit = "services:deploy-git"

func (m *Model) askDeployGit() tea.Cmd {
	m.prompt = prompt.New("Deploy stack from Git", "Repository URL (add #branch or #tag to pick one):", "https://")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindServicesDeployGit
	return nil
}

// onDeployGitPrompt plans the deploy and asks with the card.
func (m *Model) onDeployGitPrompt(value string) tea.Cmd {
	d, err := services.PlanGitDeploy(value, services.GitDeployBase())
	if err != nil {
		return m.servicesToast(toast.Info, "Not deployed", err.Error())
	}
	steps := d.Steps("")
	parts := make([]string, len(steps))
	for i, s := range steps {
		parts[i] = svcShellLine(s)
	}
	line := strings.Join(parts, " && ")
	what := "clones the repository into " + d.Dir
	if d.Exists {
		what = "updates the earlier clone in " + d.Dir + " (fast-forward only)"
	}
	body := "This " + what + " and starts its compose services in the background. " +
		"The compose file of that repository decides what runs: deploy only repositories you trust.\n\nCommand: " + line
	m.services.deployGitLine, m.services.deployGitName = line, d.Name
	m.confirm = confirm.New("Deploy "+sanitizeOutputLine(d.Name)+" from Git?", body,
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "deploy", Title: "Deploy", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindServicesDeployGit
	return nil
}

// answerDeployGit runs the planned commands in a tab on Deploy.
func (m *Model) answerDeployGit(id string) tea.Cmd {
	st := m.services
	line, name := st.deployGitLine, st.deployGitName
	st.deployGitLine, st.deployGitName = "", ""
	if id != "deploy" || line == "" {
		return nil
	}
	if _, running := m.svcTabRunning(svcTabDeployGit); running {
		return m.servicesToast(toast.Info, "A deploy is still running", "see the deploy tab")
	}
	base := services.GitDeployBase()
	if err := os.MkdirAll(base, 0o755); err != nil {
		return m.servicesToast(toast.Errr, "Cannot deploy "+name, err.Error())
	}
	if _, err := m.spawnTerminalJob(terminalJob{Label: "deploy: " + name, Cmdline: line, Dir: base, Service: svcTabDeployGit}); err != nil {
		return m.servicesToast(toast.Errr, "Cannot deploy "+name, err.Error())
	}
	return nil
}

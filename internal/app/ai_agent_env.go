package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentenv"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/workflow"
)

// Agent environment (CMP-63): .termocode/agent-env.json holds the setup
// commands, plain values, setup secrets and network rule of the outside
// agents in workflows (internal/agentenv). It is code from the repo, so
// workflows use it only after the user trusts its exact content (a
// Deny-first card, like the project hooks). AI: Agent Environment…
// shows that card, or offers a starter file on a Cancel-first card.

const (
	confirmKindAgentEnvCreate confirmKindEnum = 8630
	confirmKindAgentEnvTrust  confirmKindEnum = 8631
)

// agentEnvPath is root's agent environment file.
func agentEnvPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(agentenv.File))
}

// openAgentEnv is AI: Agent Environment…
func (m *Model) openAgentEnv() tea.Cmd {
	root := aiWorkspaceRoot()
	e, err := agentenv.Load(root)
	if err != nil {
		return m.aiInfo("Agent environment file problem: " + agentdag.Clean(err.Error()))
	}
	if e == nil {
		m.confirm = confirm.Risky("Create an agent environment?",
			agentenv.File+" sets what outside agents in workflows get: setup commands (once per worktree), "+
				"plain values, secrets from the secret store (setup only, never the agent) and a network rule "+
				"(open, off, package registries, or an allow list).\n\nThe starter file allows only the package registries.",
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "create", Title: "Create starter file", Style: confirm.StylePrimary})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindAgentEnvCreate
		return nil
	}
	m.confirm = confirm.Risky("Use this project's agent environment?", agentEnvCardText(e),
		confirm.Button{ID: "deny", Title: "Not in this project"},
		confirm.Button{ID: "use", Title: "Always in this project", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentEnvTrust
	m.aiS().agentEnvHash = e.Hash
	return nil
}

// agentEnvCardText is the trust card body: setup, values, secret names
// (never values) and the network rule.
func agentEnvCardText(e *agentenv.Env) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %d setup command%s", agentenv.File, len(e.Setup), plural(len(e.Setup)))
	for i, c := range e.Setup {
		if i == aiSkillsCardLines {
			b.WriteString("\n    …")
			break
		}
		b.WriteString("\n    $ " + runewidth.Truncate(agentdag.Clean(c), 64, "…"))
	}
	if vars := e.VarEnv(); len(vars) > 0 {
		names := make([]string, len(vars))
		for i, kv := range vars {
			names[i], _, _ = strings.Cut(kv, "=")
		}
		b.WriteString("\nValues: " + runewidth.Truncate(strings.Join(names, ", "), 64, "…"))
	}
	if s := e.SecretNames(); len(s) > 0 {
		b.WriteString("\nSecrets (setup only): " + runewidth.Truncate(strings.Join(s, ", "), 56, "…"))
	}
	b.WriteString("\nNetwork: " + runewidth.Truncate(agentdag.Clean(e.Network.Summary()), 64, "…"))
	b.WriteString("\n\nThe setup commands run on your computer in each agent worktree. Use them only if you trust this project.")
	return b.String()
}

// answerAgentEnvCreate writes the starter file and opens it.
func (m *Model) answerAgentEnvCreate(id string) tea.Cmd {
	if id != "create" {
		return nil
	}
	p := agentEnvPath(aiWorkspaceRoot())
	if _, err := os.Stat(p); err == nil {
		return m.aiInfo(agentenv.File + " already exists")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return m.aiInfo("Could not create " + agentenv.File + ": " + err.Error())
	}
	if err := os.WriteFile(p, []byte(agentenv.Starter), 0o644); err != nil {
		return m.aiInfo("Could not create " + agentenv.File + ": " + err.Error())
	}
	m.openFileAtLine(p, 1)
	return m.aiInfo("Created " + agentenv.File + " · trust it with AI: Agent Environment… before workflows use it")
}

// answerAgentEnvTrust stores the answer for the shown file version; ""
// (Esc) keeps the old answer.
func (m *Model) answerAgentEnvTrust(id string) tea.Cmd {
	hash := m.aiS().agentEnvHash
	m.aiS().agentEnvHash = ""
	if hash == "" || (id != "use" && id != "deny") {
		return nil
	}
	sess := loadSession()
	if id == "use" {
		sess.AgentEnvTrusted, sess.AgentEnvRefused = hash, ""
	} else {
		sess.AgentEnvTrusted, sess.AgentEnvRefused = "", hash
	}
	saveSession(sess)
	if id == "use" {
		return m.aiInfo("Workflows' agent steps use this project's agent environment")
	}
	return m.aiInfo("Workflows with agent steps do not start until you trust " + agentenv.File)
}

// agentEnvForRun is root's agent environment for a workflow run: nil
// with no file; why is set when the run must not start (a broken or
// not trusted file: never a silent run without the network rule).
func agentEnvForRun(root string) (e *agentenv.Env, why string) {
	e, err := agentenv.Load(root)
	if err != nil {
		return nil, "the agent environment file has a problem: " + err.Error()
	}
	if e == nil {
		return nil, ""
	}
	sess := loadSession()
	switch e.Hash {
	case sess.AgentEnvTrusted:
		return e, ""
	case sess.AgentEnvRefused:
		return nil, "you chose not to use this project's " + agentenv.File + "; change it with AI: Agent Environment… or remove the file"
	}
	return nil, "this project's " + agentenv.File + " is new or changed: trust it with AI: Agent Environment… first"
}

// wfxHasAgentStep reports an agent step anywhere in f (each, else).
func wfxHasAgentStep(f *workflow.File) bool {
	if f == nil {
		return false
	}
	var walk func([]workflow.Step) bool
	walk = func(steps []workflow.Step) bool {
		for _, s := range steps {
			if s.Agent != nil || walk(s.Else) {
				return true
			}
			if s.Each != nil && walk(s.Each.Steps) {
				return true
			}
		}
		return false
	}
	return walk(f.Steps)
}

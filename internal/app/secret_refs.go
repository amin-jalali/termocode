package app

import (
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── References in secret prompts (U19 SS-5) ────────────────────────────
//
// Every secret prompt (AI key, MCP secret, DB password, DB SSH secret,
// registry token, MCP connect card) takes a value or a reference:
// ${env:NAME}, ${dotenv:NAME} or ${cmd:line}. A reference is stored as
// text (the store keeps it in its index) and resolved on use. One helper
// here gives the hint and the check, so no prompt has its own copy.

// secretPromptKind reports a prompt kind that asks for a secret.
func secretPromptKind(k promptKindEnum) bool {
	switch k {
	case promptKindAIKey, promptKindMCPSecret, promptKindDBPassword, promptKindDBSSHSecret, promptKindRegistryToken:
		return true
	}
	return false
}

// secretPromptNote is the hint of a secret prompt, then the prompt's own
// words (which may be empty).
func secretPromptNote(own string) string {
	if own == "" {
		return "Accepts: " + secrets.PromptHint + "."
	}
	return "Accepts: " + secrets.PromptHint + ". " + own
}

// refusePromptSecret keeps the prompt open with a clear message when the
// typed text looks like a reference but is not a valid one. Nothing is
// saved: the caller returns before any handler runs.
func (m *Model) refusePromptSecret(value string) bool {
	if !secretPromptKind(m.promptKind) {
		return false
	}
	msg := secrets.TypedProblem(value)
	if msg == "" {
		return false
	}
	m.prompt.SetError(msg)
	m.promptOpen = true
	return true
}

// newRefResolver is the one way the app resolves a reference it holds
// outside the store (a registry login, a database password being added).
func newRefResolver(root string) *secrets.Resolver {
	return &secrets.Resolver{Workspace: root, Getenv: os.Getenv, Redact: ai.Redact}
}

// resolveTypedSecret returns what a typed secret stands for: the text
// itself, or the value its reference points at (registered for redaction).
func resolveTypedSecret(root, typed string) (string, error) {
	ref, err := secrets.ParseRef(typed)
	if err != nil {
		return typed, nil // a plain value (a bad reference was refused earlier)
	}
	v, err := newRefResolver(root).Resolve(ref)
	if err != nil {
		return "", err
	}
	ai.RegisterSecret(v)
	return v, nil
}

// ── ${cmd:} first-use notices ──────────────────────────────────────────

// cmdNotices holds command texts waiting for their toast. The resolver
// may run on any goroutine; the next Update shows them.
var cmdNotices struct {
	sync.Mutex
	texts []string
}

func init() { secrets.SetCmdNotifier(queueCmdNotice) }

func queueCmdNotice(text string) {
	cmdNotices.Lock()
	cmdNotices.texts = append(cmdNotices.texts, text)
	cmdNotices.Unlock()
}

// drainCmdNotices shows "Running: <command text>" once per new command.
// It shows the text of the command, never its output.
func (m *Model) drainCmdNotices() tea.Cmd {
	cmdNotices.Lock()
	texts := cmdNotices.texts
	cmdNotices.texts = nil
	cmdNotices.Unlock()
	if len(texts) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range texts {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "Running: "+sanitizeOutputLine(ai.Redact(t)))
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

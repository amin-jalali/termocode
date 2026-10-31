package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aihooks"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
)

// User hooks on AI tool events (OMX-12): after an AI tool call succeeds,
// the matching post_tool commands run (a formatter after a write). Each
// run is one line in the AI audit channel; a failed run's output goes
// back to the model with the tool result, so it can fix a lint error.
// The user's hooks file runs at once; a project's .termocode/hooks.json
// runs only after the user trusts its exact content in this workspace
// (a Deny-first card before the first message, like the project rules).

const confirmKindAIHooksTrust confirmKindEnum = 2900

// aiHooksTrustPending is the open hooks card.
type aiHooksTrustPending struct {
	hash string
	send bool
	text string
}

// aiHooksUserFile is the user's hooks file ("" without a config folder).
func aiHooksUserFile() string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ai", "hooks.json")
}

// aiLoadHooks reads the hooks of the user and of root.
func aiLoadHooks(root string) aihooks.Set { return aihooks.Load(root, aiHooksUserFile()) }

// aiHooksTrust is the user's answer for the project hooks of s.
func aiHooksTrust(s aihooks.Set) string {
	if s.ProjectHash == "" {
		return aiTrustAsk
	}
	sess := loadSession()
	switch s.ProjectHash {
	case sess.AIHooksTrusted:
		return aiTrustUse
	case sess.AIHooksRefused:
		return aiTrustRefused
	}
	return aiTrustAsk
}

// aiActiveHooks are the hooks that may run: the user's, and the
// project's when trusted.
func aiActiveHooks(root string) []aihooks.Hook {
	s := aiLoadHooks(root)
	if aiHooksTrust(s) == aiTrustUse {
		return s.PostTool
	}
	var out []aihooks.Hook
	for _, h := range s.PostTool {
		if h.Source == aihooks.SourceUser {
			out = append(out, h)
		}
	}
	return out
}

// aiWithHooks wraps the Run of every tool a hook matches: after a call
// that worked, the hooks run in order (in the agent's goroutine, so the
// next step sees the formatted file). out takes the audit lines.
func aiWithHooks(root string, tools []ai.Tool, out *outputStore) []ai.Tool {
	hooks := aiActiveHooks(root)
	if len(hooks) == 0 {
		return tools
	}
	wrapped := make([]ai.Tool, len(tools))
	for i, t := range tools {
		wrapped[i] = t
		var match []aihooks.Hook
		for _, h := range hooks {
			if h.Matches(t.Name) {
				match = append(match, h)
			}
		}
		if len(match) == 0 || t.Run == nil {
			continue
		}
		run, name := t.Run, t.Name
		wrapped[i].Run = func(ctx context.Context, args map[string]any) (string, error) {
			res, err := run(ctx, args)
			if err != nil {
				return res, err
			}
			call := aihooks.Call{Tool: name, Root: root}
			if p, _ := args["path"].(string); p != "" {
				if !filepath.IsAbs(p) {
					p = filepath.Join(root, filepath.FromSlash(p))
				}
				call.Path = p
			}
			for _, h := range match {
				at := clock.Now()
				r := aihooks.Run(ctx, h, call)
				out.Append(aiAuditChannel, aiHookAuditLine(at, name, h, r))
				if r.Exit != 0 {
					res += fmt.Sprintf("\n\nHook %q failed (%s):\n%s", h.Run, aiHookState(r), r.Output)
				}
			}
			return res, nil
		}
	}
	return wrapped
}

// aiHookState is "exit 0", "exit 3" or the start / timeout error.
func aiHookState(r aihooks.Result) string {
	if r.Err != nil {
		return r.Err.Error()
	}
	return fmt.Sprintf("exit %d", r.Exit)
}

// aiHookAuditLine is the audit line of one hook run.
func aiHookAuditLine(at time.Time, tool string, h aihooks.Hook, r aihooks.Result) string {
	first, _, _ := strings.Cut(r.Output, "\n")
	line := at.Format("15:04:05") + "  hook  after=" + tool + "  run=" + runewidth.Truncate(agentdag.Clean(h.Run), 60, "…") +
		"  by=" + h.Source + "  → " + aiHookState(r) + " · " + r.Duration.Round(time.Millisecond).String()
	if first != "" {
		line += " · " + runewidth.Truncate(agentdag.Clean(first), 80, "…")
	}
	return line
}

// askAIHooksTrust opens the project hooks card (Not in this project is
// first and focused: the hooks are commands from the repository).
func (m *Model) askAIHooksTrust(s aihooks.Set, send bool, text string) tea.Cmd {
	m.aiS().hooksTrust = &aiHooksTrustPending{hash: s.ProjectHash, send: send, text: text}
	proj := s.Project()
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %d hook%s", aihooks.ProjectFile, len(proj), plural(len(proj)))
	for i, h := range proj {
		if i == aiSkillsCardLines {
			b.WriteString("\n    …")
			break
		}
		tools := strings.Join(h.Tools, ", ")
		if tools == "" {
			tools = "*"
		}
		b.WriteString("\n    after " + runewidth.Truncate(tools, 24, "…") + ": " + runewidth.Truncate(agentdag.Clean(h.Run), 56, "…"))
	}
	b.WriteString("\n\nThese commands run on your computer after AI tool calls. Use them only if you trust this project.")
	m.confirm = confirm.Risky("Run this project's hooks?", b.String(),
		confirm.Button{ID: "deny", Title: "Not in this project"},
		confirm.Button{ID: "use", Title: "Always in this project", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIHooksTrust
	return nil
}

// answerAIHooksTrust stores the answer and sends the waiting message;
// "" (Esc) is no answer and puts the message back.
func (m *Model) answerAIHooksTrust(id string) tea.Cmd {
	s := m.aiS()
	p := s.hooksTrust
	s.hooksTrust = nil
	if p == nil {
		return nil
	}
	if id != "use" && id != "deny" {
		if p.send {
			m.aiPutBackInput(p.text)
		}
		return nil
	}
	sess := loadSession()
	if id == "use" {
		sess.AIHooksTrusted, sess.AIHooksRefused = p.hash, ""
	} else {
		sess.AIHooksTrusted, sess.AIHooksRefused = "", p.hash
	}
	saveSession(sess)
	if p.send {
		return m.sendAIChat(p.text)
	}
	if id == "use" {
		return m.aiInfo("This project's hooks run after AI tool calls")
	}
	return m.aiInfo("This project's hooks do not run")
}

// openAIHooks is AI: Hooks…: the trust card when the project has hooks,
// else a short note of what runs.
func (m *Model) openAIHooks() tea.Cmd {
	s := aiLoadHooks(aiWorkspaceRoot())
	if len(s.Errors) > 0 {
		return m.aiInfo("Hooks file problem: " + agentdag.Clean(s.Errors[0]))
	}
	if s.ProjectHash != "" {
		return m.askAIHooksTrust(s, false, "")
	}
	if n := len(s.PostTool); n > 0 {
		return m.aiInfo(fmt.Sprintf("%d user hook%s run after AI tool calls (%s)", n, plural(n), aiHooksUserFile()))
	}
	return m.aiInfo("No hooks · add " + aihooks.ProjectFile + " or " + aiHooksUserFile())
}

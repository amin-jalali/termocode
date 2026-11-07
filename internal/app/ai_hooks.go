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
	"github.com/amin-jalali/termocode/internal/toast"
)

// User hooks on AI tool events (OMX-12, CMP-59: every event of
// aihooks — before a call, on save, on stop, around a summary): after an AI tool call succeeds,
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
// project's when trusted (every event).
func aiActiveHooks(root string) []aihooks.Hook {
	s := aiLoadHooks(root)
	if aiHooksTrust(s) == aiTrustUse {
		return s.All()
	}
	var out []aihooks.Hook
	for _, h := range s.All() {
		if h.Source == aihooks.SourceUser {
			out = append(out, h)
		}
	}
	return out
}

// aiWithHooks wraps the Run of every tool a post_tool hook matches.
func aiWithHooks(root string, tools []ai.Tool, out *outputStore) []ai.Tool {
	return aiWithHookList(root, aiActiveHooks(root), tools, out)
}

// aiWithHookList wraps the Run of every tool a post_tool hook of hooks
// matches: after a call that worked, the hooks run in order (in the
// agent's goroutine, so the next step sees the formatted file). out
// takes the audit lines.
func aiWithHookList(root string, hooks []aihooks.Hook, tools []ai.Tool, out *outputStore) []ai.Tool {
	hooks = aihooks.Of(hooks, aihooks.EventPostTool)
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
			call := aiHookCall(root, name, args)
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

// aiHookCall is the hook input of a call of tool with args.
func aiHookCall(root, tool string, args map[string]any) aihooks.Call {
	call := aihooks.Call{Tool: tool, Root: root, Args: args}
	if p, _ := args["path"].(string); p != "" {
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, filepath.FromSlash(p))
		}
		call.Path = p
	}
	return call
}

// aiPreToolHooks is the agent's PreTool of hooks (CMP-59): the matching
// pre_tool hooks run in order before the call's gate; the first that
// blocks wins, a changed argument set goes on to the next hook, and
// "allow" skips the approval card only when it comes from the user's
// own hooks (a project file never approves for the user). nil without
// pre_tool hooks.
func aiPreToolHooks(root string, hooks []aihooks.Hook, out *outputStore) func(context.Context, ai.Tool, map[string]any) ai.PreToolVerdict {
	hooks = aihooks.Of(hooks, aihooks.EventPreTool)
	if len(hooks) == 0 {
		return nil
	}
	return func(ctx context.Context, t ai.Tool, args map[string]any) ai.PreToolVerdict {
		var v ai.PreToolVerdict
		allow := false
		for _, h := range hooks {
			if !h.Matches(t.Name) {
				continue
			}
			at := clock.Now()
			r := aihooks.Run(ctx, h, aiHookCall(root, t.Name, args))
			d := aihooks.Decide(r)
			out.Append(aiAuditChannel, aiHookAuditLine(at, t.Name, h, r)+aiHookDecisionNote(d))
			switch {
			case d.Block:
				return ai.PreToolVerdict{Block: d.Reason, By: "hook"}
			case d.Args != nil:
				args, v.Args = d.Args, d.Args
			}
			if d.Allow && h.Source == aihooks.SourceUser {
				allow = true
			}
		}
		v.Allow, v.By = allow, "hook"
		return v
	}
}

// aiHookDecisionNote is the audit tail of a decision ("" for none).
func aiHookDecisionNote(d aihooks.Decision) string {
	switch {
	case d.Block:
		return " · blocked: " + runewidth.Truncate(agentdag.Clean(d.Reason), 60, "…")
	case d.Args != nil && d.Allow:
		return " · changed args, allow"
	case d.Args != nil:
		return " · changed args"
	case d.Allow:
		return " · allow"
	}
	return ""
}

// aiWithCompactHooks wraps compact (CMP-59): pre_compact hooks run
// first and one that blocks keeps the full chat (the summary is not
// made); post_compact hooks get the new summary. nil stays nil.
func aiWithCompactHooks(root string, hooks []aihooks.Hook, out *outputStore, compact func(context.Context, []ai.Message) (string, error)) func(context.Context, []ai.Message) (string, error) {
	pre, post := aihooks.Of(hooks, aihooks.EventPreCompact), aihooks.Of(hooks, aihooks.EventPostCompact)
	if compact == nil || len(pre)+len(post) == 0 {
		return compact
	}
	return func(ctx context.Context, older []ai.Message) (string, error) {
		for _, h := range pre {
			at := clock.Now()
			r := aihooks.Run(ctx, h, aihooks.Call{Root: root})
			d := aihooks.Decide(r)
			out.Append(aiAuditChannel, aiHookAuditLine(at, aihooks.EventPreCompact, h, r)+aiHookDecisionNote(d))
			if d.Block {
				return "", fmt.Errorf("a pre_compact hook kept the full chat: %s", d.Reason)
			}
		}
		sum, err := compact(ctx, older)
		if err != nil || sum == "" {
			return sum, err
		}
		for _, h := range post {
			at := clock.Now()
			r := aihooks.Run(ctx, h, aihooks.Call{Root: root, Summary: sum})
			out.Append(aiAuditChannel, aiHookAuditLine(at, aihooks.EventPostCompact, h, r))
		}
		return sum, nil
	}
}

// aiStopHookMax is how many turns in a row stop hooks may ask for.
const aiStopHookMax = 3

// aiRunStopHooks runs the stop hooks when a turn ends (CMP-59) and
// returns the reason of the first that asks the agent to go on ("" for
// none). The hooks get their own time limit: the turn's context is done.
func aiRunStopHooks(root string, hooks []aihooks.Hook, out *outputStore) string {
	for _, h := range aihooks.Of(hooks, aihooks.EventStop) {
		at := clock.Now()
		r := aihooks.Run(context.Background(), h, aihooks.Call{Root: root})
		d := aihooks.Decide(r)
		out.Append(aiAuditChannel, aiHookAuditLine(at, aihooks.EventStop, h, r)+aiHookDecisionNote(d))
		if d.Block {
			return d.Reason
		}
	}
	return ""
}

// aiStopHookText is the message that sends the agent on after a stop
// hook said the work is not done.
func aiStopHookText(reason string) string {
	return "A stop hook says the work is not done yet:\n" + reason + "\nPlease go on and finish it."
}

// aiOnStopHook is onAIChatDone's part for stop hooks: it sends the
// agent on when a hook asked for it, at most aiStopHookMax turns in a
// row, and says so in the chat.
func (m *Model) aiOnStopHook(reason string) tea.Cmd {
	s := m.aiS()
	if reason == "" {
		s.stopHookPushes = 0
		return nil
	}
	if s.stopHookPushes >= aiStopHookMax {
		s.stopHookPushes = 0
		m.aiNote(fmt.Sprintf("⏹ A stop hook still says the work is not done (%d turns in a row); stopped here", aiStopHookMax))
		return nil
	}
	s.stopHookPushes++
	m.aiNote("↻ A stop hook sends the agent on: " + runewidth.Truncate(agentdag.Clean(reason), 80, "…"))
	return m.sendAIChat(aiStopHookText(reason))
}

// aiFileSaveHookFiles is the file_save hooks of hooks that match path
// in root.
func aiFileSaveHookFiles(root, path string, hooks []aihooks.Hook) []aihooks.Hook {
	rel := filepath.ToSlash(path)
	if root != "" {
		if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
	}
	var out []aihooks.Hook
	for _, h := range aihooks.Of(hooks, aihooks.EventFileSave) {
		if h.MatchesFile(rel) {
			out = append(out, h)
		}
	}
	return out
}

// aiFileSaveHooks runs the file_save hooks of a file the user saved
// (CMP-59) in the background: each run is an audit line, a failed one
// is a toast. nil without matching hooks.
func (m *Model) aiFileSaveHooks(path string) tea.Cmd {
	root := aiWorkspaceRoot()
	match := aiFileSaveHookFiles(root, path, aiActiveHooks(root))
	if len(match) == 0 {
		return nil
	}
	out := m.output
	return func() tea.Msg {
		var failed []string
		for _, h := range match {
			at := clock.Now()
			r := aihooks.Run(context.Background(), h, aihooks.Call{Path: path, Root: root})
			out.Append(aiAuditChannel, aiHookAuditLine(at, filepath.Base(path), h, r))
			if r.Exit != 0 {
				first, _, _ := strings.Cut(r.Output, "\n")
				failed = append(failed, runewidth.Truncate(agentdag.Clean(h.Run), 40, "…")+" ("+aiHookState(r)+") "+runewidth.Truncate(agentdag.Clean(first), 80, "…"))
			}
		}
		if len(failed) == 0 {
			return nil
		}
		return ToastMsg{Level: toast.Errr, Title: "Save hook failed", Body: strings.Join(failed, "\n")}
	}
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
	when := "after=" + tool
	switch h.Event {
	case aihooks.EventPreTool:
		when = "before=" + tool
	case aihooks.EventFileSave:
		when = "saved=" + tool
	case aihooks.EventStop, aihooks.EventPreCompact, aihooks.EventPostCompact:
		when = "on=" + h.Event
	}
	line := at.Format("15:04:05") + "  hook  " + when + "  run=" + runewidth.Truncate(agentdag.Clean(h.Run), 60, "…") +
		"  by=" + h.Source + "  → " + aiHookState(r) + " · " + r.Duration.Round(time.Millisecond).String()
	if first != "" {
		line += " · " + runewidth.Truncate(agentdag.Clean(first), 80, "…")
	}
	return line
}

// aiHookWhen is when h runs, in short words ("after write_file",
// "before run_command", "on save *.go", "on stop").
func aiHookWhen(h aihooks.Hook) string {
	list := func(xs []string) string {
		if len(xs) == 0 {
			return "*"
		}
		return strings.Join(xs, ", ")
	}
	switch h.Event {
	case aihooks.EventPreTool:
		return "before " + list(h.Tools)
	case aihooks.EventFileSave:
		return "on save " + list(h.Files)
	case aihooks.EventStop:
		return "on stop"
	case aihooks.EventPreCompact:
		return "before summary"
	case aihooks.EventPostCompact:
		return "after summary"
	}
	return "after " + list(h.Tools)
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
		b.WriteString("\n    " + runewidth.Truncate(aiHookWhen(h), 30, "…") + ": " + runewidth.Truncate(agentdag.Clean(h.Run), 50, "…"))
	}
	b.WriteString("\n\nThese commands run on your computer on AI events and file saves. They can block or change AI tool calls, but never approve one for you. Use them only if you trust this project.")
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
		return m.aiInfo("This project's hooks run on AI events and file saves")
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
	if n := len(s.All()); n > 0 {
		return m.aiInfo(fmt.Sprintf("%d user hook%s run on AI events (%s)", n, plural(n), aiHooksUserFile()))
	}
	return m.aiInfo("No hooks · add " + aihooks.ProjectFile + " or " + aiHooksUserFile())
}

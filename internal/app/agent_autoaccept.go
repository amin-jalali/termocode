package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Opt-in auto-accept for outside agents' prompts (CMP-64) ─────────────
//
// Agent: Toggle Auto-Accept on an agent tab opens a Cancel-first card;
// after Turn on, termocode answers that tab's permission prompts with
// "yes, this once" (agents.AcceptOnce), so a background run goes on
// hands-off. It is per tab, off by default, and has no setting.
// Guards: never an "always" choice; never a question with its own
// choices; never a prompt that shows a risky command (agents.RiskyPrompt:
// rm -rf, sudo, force push, …) — that one waits for the user with a
// toast; one answer per prompt (the screen must leave the prompt before
// the next answer); it turns itself off after agentAutoAcceptMax answers.
// A tab whose agent reports its own state (AgentHook) is not read, so
// the toggle refuses it. The AI may never turn it on.

const (
	agentAutoAcceptPaletteID                   = "agent-auto-accept-toggle"
	confirmKindAgentAutoAccept confirmKindEnum = 6464
	// agentAutoAcceptMax is how many prompts one turn-on answers.
	agentAutoAcceptMax = 50
)

// acceptState is what auto-accept did with the prompt on screen.
type acceptState int8

const (
	acceptNone    acceptState = iota // no prompt handled yet
	acceptTyped                      // answered it
	acceptSkipped                    // left it to the user
)

// toggleAgentAutoAccept is Agent: Toggle Auto-Accept on the active tab.
func (m *Model) toggleAgentAutoAccept() tea.Cmd {
	t := m.activeTerminalTab()
	if t == nil || t.Agent == "" {
		return m.pushToast(toast.Info, "Auto-accept works on an agent tab: open one first")
	}
	if t.AutoAccept {
		t.AutoAccept = false
		return m.pushToast(toast.Info, "Auto-accept off for "+t.agentTitle())
	}
	if !agents.HasAcceptRules(t.Agent) {
		return m.pushToast(toast.Info, "termocode cannot read "+t.agentTitle()+"'s prompts yet: answer them yourself")
	}
	if t.AgentHook {
		return m.pushToast(toast.Info, t.agentTitle()+" reports its own state: auto-accept reads the screen, so it stays off")
	}
	m.agentAcceptAsk = t.BufID
	body := strings.Join([]string{
		"termocode will answer " + t.agentTitle() + "'s permission prompts in this tab with \"yes, this once\", so it can run without you.",
		"",
		"• Never an \"always\" or \"don't ask again\" choice.",
		"• Never a question with its own choices.",
		"• Never a prompt that shows a risky command (rm -rf, sudo, force push, reset --hard, a download piped into a shell, keys, kubectl delete, …): that one waits for you.",
		fmt.Sprintf("• Only this tab; it turns itself off after %d answers.", agentAutoAcceptMax),
		"",
		"The agent can still edit files and run commands you did not read. Use it on a worktree you can throw away.",
	}, "\n")
	m.confirm = confirm.Risky("Auto-accept prompts in "+t.agentTitle()+"?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "on", Title: "Turn on", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAgentAutoAccept
	return nil
}

// answerAgentAutoAccept answers the card ("" = Esc = Cancel).
func (m *Model) answerAgentAutoAccept(id string) tea.Cmd {
	buf := m.agentAcceptAsk
	m.agentAcceptAsk = 0
	if id != "on" {
		return nil
	}
	t, _ := m.termTabByBuf(buf)
	if t == nil || t.Agent == "" {
		return m.pushToast(toast.Info, "The agent tab is closed: auto-accept stays off")
	}
	t.AutoAccept, t.acceptCount, t.accept = true, 0, acceptNone
	return m.pushToast(toast.Info, "Auto-accept on for "+t.agentTitle()+": risky prompts still wait for you")
}

// agentAutoAcceptSee runs on each screen read of an agent tab with its
// detected state st. done: it typed the answer (or already did for this
// prompt), so the "waiting for you" news is not shown.
func (m *Model) agentAutoAcceptSee(t *terminalTab, st agents.State, lines []string) (tea.Cmd, bool) {
	if !t.AutoAccept {
		return nil, false
	}
	if st != agents.Waiting {
		if st != agents.Unknown {
			t.accept = acceptNone // the prompt is gone: the next one may be answered
		}
		return nil, false
	}
	switch t.accept {
	case acceptTyped:
		return nil, true // the same prompt, still on screen
	case acceptSkipped:
		return nil, false
	}
	keys, ok := agents.AcceptOnce(t.Agent, lines)
	if !ok {
		t.accept = acceptSkipped // a question: the user answers
		return nil, false
	}
	if w, risky := agents.RiskyPrompt(lines); risky {
		t.accept = acceptSkipped
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Auto-accept did not answer "+t.agentTitle()+": \""+w+"\"", "A risky command is on the prompt: answer it yourself.")
		return c, false
	}
	t.accept = acceptTyped
	t.acceptCount++
	first, enter := keys, ""
	if len(keys) > 1 && strings.HasSuffix(keys, "\r") {
		first, enter = strings.TrimSuffix(keys, "\r"), "\r"
	}
	agentChatSend(m, t.BufID, first)
	var cmds []tea.Cmd
	if enter != "" {
		cmds = append(cmds, agentTypeLater(m, t.BufID, enter))
	}
	var c tea.Cmd
	if t.acceptCount >= agentAutoAcceptMax {
		t.AutoAccept = false
		m.toast, c = m.toast.Push(toast.Warn, fmt.Sprintf("Auto-accepted a prompt in %s; auto-accept is off now (%d answers)", t.agentTitle(), agentAutoAcceptMax))
	} else {
		m.toast, c = m.toast.Push(toast.Info, fmt.Sprintf("Auto-accepted a prompt in %s (%d/%d)", t.agentTitle(), t.acceptCount, agentAutoAcceptMax))
	}
	return tea.Batch(append(cmds, c)...), true
}

func init() {
	aiActionPolicies[agentAutoAcceptPaletteID] = aiNever(aiNoAIConfig + "; it lets termocode answer an outside agent's permission prompts")
}

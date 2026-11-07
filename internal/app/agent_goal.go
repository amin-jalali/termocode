package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentgoal"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Loop until verify passes, with caps (CMP-19) ─────────────────────────
//
// Agent: Loop Until Check Passes… on an agent tab asks for a check
// command, then a Cancel-first card picks the rounds. Each time the
// agent's turn ends (Working → Done, read from its screen or reported
// over the control socket), termocode runs the check in a task tab in the
// agent's folder. Exit 0 ends the loop with a toast. Any other exit types
// the end of the check's output (redacted, one line) into the agent and
// asks it to fix the cause, until a cap ends the loop: the rounds, the
// time (20 min a round) or agent_goal_max_tokens (counted only from the
// session files, agent_transcripts = read). It never types while the
// agent works, while a question is on its screen or while a usage limit
// is on. Agent: Stop Check Loop ends it. The AI can start neither.

const (
	agentGoalPaletteID     = "agent-goal-loop"
	agentGoalStopPaletteID = "agent-goal-stop"

	promptKindAgentGoalVerify promptKindEnum  = 4181
	confirmKindAgentGoalStart confirmKindEnum = 4182

	// agentGoalTaskPfx names a check's task tab; a new round reuses it.
	agentGoalTaskPfx = "agent-goal:"
)

// agentGoalSpawn runs the check of the agent tab agentBuf in a task tab
// and returns that tab's buffer (a var for tests).
var agentGoalSpawn = func(m *Model, agentBuf int, title string, g *agentgoal.Goal) (int, error) {
	return m.spawnTerminalJob(terminalJob{Label: "check · " + title, Cmdline: g.Verify, Dir: g.Dir,
		Task: agentGoalTaskPfx + strconv.Itoa(agentBuf)})
}

// agentGoalDir is where the check of tab t runs: its worktree, else the
// workspace.
func agentGoalDir(t *terminalTab) string {
	if t.Worktree != "" {
		return t.Worktree
	}
	return aiWorkspaceRoot()
}

// agentGoalMaxTokens is agent_goal_max_tokens (0: no token cap).
func agentGoalMaxTokens() int {
	if v := loadSettings().AgentGoalMaxTokens; v != nil && *v > 0 {
		return *v
	}
	return 0
}

// agentGoalTokens is the tokens today of tab t's account (-1: not known).
func (m *Model) agentGoalTokens(t *terminalTab) int {
	k := agentUsageKey(t)
	for _, w := range m.agentUsageBook().Windows(clock.Now()) {
		if w.Key == k {
			return w.TokensToday
		}
	}
	return -1
}

// openAgentGoal is Agent: Loop Until Check Passes… on the active tab.
func (m *Model) openAgentGoal() tea.Cmd {
	t := m.activeTerminalTab()
	if t == nil || t.Agent == "" {
		return m.pushToast(toast.Info, "The check loop works on an agent tab: open one first")
	}
	if t.Goal != nil {
		return m.pushToast(toast.Info, "A check loop already runs for "+t.agentTitle()+" ("+t.Goal.Label()+"): Agent: Stop Check Loop ends it")
	}
	m.agentGoalPending = t.BufID
	m.prompt = prompt.New(" Loop until a check passes ", "Check command (exit 0 = done):", m.agentGoalLast)
	m.prompt.SetNote("Runs in " + agentGoalDir(t) + " each time " + t.agentTitle() + " ends its turn. Nothing runs before you pick the rounds.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAgentGoalVerify
	return nil
}

// onAgentGoalVerify shows the Cancel-first start card for the command.
func (m *Model) onAgentGoalVerify(value string) tea.Cmd {
	line := strings.TrimSpace(value)
	if line == "" {
		return nil
	}
	t, _ := m.termTabByBuf(m.agentGoalPending)
	if t == nil || t.Agent == "" {
		return m.pushToast(toast.Info, "The agent tab is gone: nothing was started")
	}
	m.agentGoalLast = line
	title := t.agentTitle()
	var b strings.Builder
	fmt.Fprintf(&b, "Each time %s ends its turn, termocode runs:\n\n    %s\n\nin %s, in a task tab.\n\n", title, line, agentGoalDir(t))
	b.WriteString("Exit 0: the loop ends and a toast says so.\n")
	fmt.Fprintf(&b, "Any other exit: termocode types the last %d lines of the output (secrets hidden) into %s and asks it to fix the cause.\n\n", agentgoal.TailLines, title)
	fmt.Fprintf(&b, "Stops after the rounds you pick, after %d minutes a round", int(agentgoal.PerRound.Minutes()))
	if n := agentGoalMaxTokens(); n > 0 {
		fmt.Fprintf(&b, ", or after %d agent tokens (counted with agent_transcripts = read)", n)
	}
	b.WriteString(".\nIt never types while the agent works, asks a question or waits for a usage limit. Agent: Stop Check Loop ends it at any time.")
	var btns []confirm.Button
	for _, n := range agentgoal.RoundChoices {
		bt := confirm.Button{ID: "r" + strconv.Itoa(n), Title: strconv.Itoa(n) + " rounds"}
		if n == agentgoal.DefaultRounds {
			bt.Style = confirm.StylePrimary
		}
		btns = append(btns, bt)
	}
	m.confirm = confirm.Risky("Loop "+title+" until the check passes?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"}, btns...).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentGoalStart
	return nil
}

// answerAgentGoalStart starts the loop on a rounds button.
func (m *Model) answerAgentGoalStart(id string) tea.Cmd {
	buf := m.agentGoalPending
	m.agentGoalPending = 0
	rs, ok := strings.CutPrefix(id, "r")
	rounds, err := strconv.Atoi(rs)
	if !ok || err != nil || rounds < 1 || m.agentGoalLast == "" {
		return nil
	}
	t, _ := m.termTabByBuf(buf)
	if t == nil || t.Agent == "" {
		return m.pushToast(toast.Info, "The agent tab is gone: nothing was started")
	}
	t.Goal = agentgoal.New(m.agentGoalLast, agentGoalDir(t), rounds, agentGoalMaxTokens(), max(m.agentGoalTokens(t), 0), clock.Now())
	cmds := []tea.Cmd{m.pushToast(toast.Info, fmt.Sprintf("Check loop on for %s: %s, up to %d rounds", t.agentTitle(), t.Goal.Verify, rounds))}
	if t.AgentState == agents.Done { // its turn is already over: check now
		cmds = append(cmds, m.agentGoalTurnEnded(buf))
	}
	return tea.Batch(cmds...)
}

// stopAgentGoal is Agent: Stop Check Loop: the active tab's loop, else
// the first tab with one.
func (m *Model) stopAgentGoal() tea.Cmd {
	t := m.activeTerminalTab()
	if t == nil || t.Goal == nil {
		t = nil
		for i := range m.terminalTabs {
			if m.terminalTabs[i].Goal != nil {
				t = &m.terminalTabs[i]
				break
			}
		}
	}
	if t == nil {
		return m.pushToast(toast.Info, "No check loop runs")
	}
	label := t.Goal.Label()
	t.Goal = nil
	return m.pushToast(toast.Info, "Check loop stopped for "+t.agentTitle()+" ("+label+")")
}

// agentGoalTurnEnded runs the check when the turn of agent tab buf
// ended and a loop is on.
func (m *Model) agentGoalTurnEnded(buf int) tea.Cmd {
	t, _ := m.termTabByBuf(buf)
	if t == nil || t.Agent == "" || t.Goal == nil || t.Goal.Verifying || t.agentLimitActive() {
		return nil
	}
	g, title := t.Goal, t.agentTitle()
	g.Verifying = true
	vbuf, err := agentGoalSpawn(m, buf, title, g) // t may move: m.terminalTabs grows
	if err != nil {
		g.Verifying = false
		return m.toastErr("Could not run the check for "+title, err)
	}
	if m.agentGoalVerify == nil {
		m.agentGoalVerify = map[int]int{}
	}
	m.agentGoalVerify[vbuf] = buf
	return nil
}

// agentGoalVerifyExit handles a check's exit (ok=false: not a check).
func (m *Model) agentGoalVerifyExit(vbuf, code int) (tea.Cmd, bool) {
	buf, ok := m.agentGoalVerify[vbuf]
	if !ok {
		return nil, false
	}
	delete(m.agentGoalVerify, vbuf)
	t, _ := m.termTabByBuf(buf)
	if t == nil || t.Goal == nil {
		return nil, true
	}
	g, title := t.Goal, t.agentTitle()
	out := g.Next(code, clock.Now(), m.agentGoalTokens(t))
	switch {
	case out == agentgoal.Met:
		t.Goal = nil
		return tea.Batch(m.pushToast(toast.Info, fmt.Sprintf("Check passed for %s after %d round%s: %s", title, g.Round, plural(g.Round), g.Verify)),
			m.notifyOS("An agent's check loop passed")), true
	case out.Stopped():
		t.Goal = nil
		return tea.Batch(m.pushToast(toast.Warn, fmt.Sprintf("Check loop stopped for %s: %s; %s still fails (exit %d)", title, out.Reason(), g.Verify, code)),
			m.notifyOS("An agent's check loop stopped")), true
	}
	if t.AgentState == agents.Working {
		return nil, true // someone typed: the next turn end checks again
	}
	if raw, ok := agentReadTail(m, buf); !ok || agents.IsPrompt(t.Agent, strings.Split(raw, "\n")) {
		return m.pushToast(toast.Info, title+" asks something: the check loop typed nothing (it checks again after the next turn)"), true
	}
	tail, _ := agentReadTail(m, vbuf)
	g.Round++
	agentChatSend(m, buf, agentgoal.Prompt(g.Verify, code, g.Round, g.Rounds, ai.Redact(tail)))
	enter := agentTypeLater(m, buf, "\r")
	return tea.Batch(enter, m.pushToast(toast.Info, fmt.Sprintf("Check failed (exit %d): typed it into %s, %s", code, title, g.Label()))), true
}

// agentGoalLabel is the tab bar mark of a running loop ("" for none).
func (t terminalTab) agentGoalLabel() string {
	if t.Goal == nil {
		return ""
	}
	return t.Goal.Label()
}

func init() {
	aiActionPolicies[agentGoalPaletteID] = aiNever(aiNoAIConfig + "; it lets termocode run a command and type into an outside agent's tab")
	aiActionPolicies[agentGoalStopPaletteID] = aiNever(aiNoAIConfig + "; the check loop is the user's own")
	aiSettingPolicies["agent_goal_max_tokens"] = aiNever(aiNoAIConfig + "; it caps the user's own check loop")
}

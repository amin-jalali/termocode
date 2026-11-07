package app

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agents"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Agent: Chat View · native panels (CMP-14) ───────────────────────────
//
// While a Claude Code or Codex tab asks something, the chat view draws the
// question as a panel of its own above the status line: what is asked
// (from the session file: the question with its options, the plan, or
// the tool call) and the numbered answers (from the tab's screen, so they
// are exactly the menu the CLI shows). A number key answers:
//
//   - a question (AskUserQuestion) at once: the user picked an answer;
//   - a plan approval or a permission prompt only through a card with
//     Cancel first, and only while the screen still shows the same answer
//     under the same number when Send is pressed.
//
// The answer is the CLI's own keys (arrows to the row, then Enter), sent
// to the tab one at a time. o opens a plan whole in a read-only editor
// tab. The AI never answers: the chat view stays aiNever (agent_chat.go).

const confirmKindAgentChatAnswer confirmKindEnum = 6146

// agentChatPanelKind is what the agent asks.
type agentChatPanelKind int

const (
	agentChatAskPermission agentChatPanelKind = iota
	agentChatAskQuestion
	agentChatAskPlan
)

// agentChatPanel is the panel on show: what is asked and the answers.
type agentChatPanel struct {
	kind    agentChatPanelKind
	call    agentlog.Turn // the pending call (Tool "" when none is known)
	choices []agents.Choice
}

// agentChatAsk is the answer a card asks about.
type agentChatAsk struct {
	buf   int
	n     int
	label string
}

// agentChatSyncChoices reads the menu on the tab's screen while the tab
// waits (none otherwise). It runs on the UI thread after each poll.
func (m *Model) agentChatSyncChoices(c *agentChatState) {
	if c == nil {
		return
	}
	c.choices = nil
	if c.readOnly || c.demo || c.buf <= 0 || m.agentChatAgentState(c) != agents.Waiting {
		return
	}
	raw, ok := agentReadTail(m, c.buf)
	if !ok {
		return
	}
	c.choices = agents.Choices(c.agent, strings.Split(raw, "\n"))
}

// agentChatPanelFor is the panel of c, nil when nothing is asked.
func agentChatPanelFor(c *agentChatState) *agentChatPanel {
	if c == nil || c.readOnly || len(c.choices) == 0 {
		return nil
	}
	p := &agentChatPanel{kind: agentChatAskPermission, choices: c.choices}
	if call, ok := c.sess.Pending(); ok {
		p.call = call
		switch call.Tool {
		case "AskUserQuestion":
			p.kind = agentChatAskQuestion
		case "ExitPlanMode":
			p.kind = agentChatAskPlan
		}
	}
	return p
}

func (p *agentChatPanel) title() string {
	switch p.kind {
	case agentChatAskQuestion:
		return "? QUESTION"
	case agentChatAskPlan:
		return "▤ PLAN"
	}
	return "◐ NEEDS YOUR OK"
}

func (p *agentChatPanel) choice(n int) (agents.Choice, bool) {
	for _, ch := range p.choices {
		if ch.N == n {
			return ch, true
		}
	}
	return agents.Choice{}, false
}

// questionText splits a question Detail into its question lines and the
// description of each option label.
func questionText(detail string) (lines []string, desc map[string]string) {
	desc = map[string]string{}
	for _, l := range strings.Split(detail, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(l, "  ") {
			if _, rest, ok := strings.Cut(t, ". "); ok {
				label, d, _ := strings.Cut(rest, " — ")
				desc[label] = d
				continue
			}
		}
		if t != "" {
			lines = append(lines, t)
		}
	}
	return lines, desc
}

// agentChatPanelRows draws the panel at width w in at most maxH rows.
func agentChatPanelRows(p *agentChatPanel, cwd string, w, maxH int) []string {
	if p == nil || w < 20 || maxH < 3 {
		return nil
	}
	bg := theme.BgEditor
	edge := theme.FgBg(theme.DiagWarning, bg)
	text := theme.FgBg(theme.TextPrimary, bg)
	dim := theme.FgBg(theme.TextDim, bg)
	cut := func(s string, room int) string {
		if runewidth.StringWidth(s) > room {
			return runewidth.Truncate(s, max(room, 1), "…")
		}
		return s
	}
	room := w - 4 // " │ " and one cell to spare
	wrap := func(s string) []string { return strings.Split(ansi.Wrap(s, room, " "), "\n") }

	// What is asked.
	var detail []string
	desc := map[string]string{}
	switch p.kind {
	case agentChatAskQuestion:
		var q []string
		q, desc = questionText(p.call.Detail)
		for _, l := range q {
			detail = append(detail, wrap(l)...)
		}
	case agentChatAskPlan:
		for _, l := range strings.Split(strings.TrimRight(p.call.Detail, "\n"), "\n") {
			detail = append(detail, wrap(l)...)
		}
	default:
		if p.call.Tool != "" {
			detail = append(detail, p.call.Tool+"  "+agentChatArgs(p.call.Args, cwd, w < 80))
		}
	}
	// The answers.
	var answers []string
	for _, ch := range p.choices {
		lead := "  "
		if ch.Cursor {
			lead = "❯ "
		}
		row := lead + strconv.Itoa(ch.N) + "  " + ch.Label
		if d := desc[ch.Label]; d != "" {
			row += " — " + d
		}
		answers = append(answers, row)
	}
	hint := "1–" + strconv.Itoa(p.choices[len(p.choices)-1].N) + " answers"
	if p.kind != agentChatAskQuestion {
		hint += " (asks first)"
	}
	if p.kind == agentChatAskPlan {
		hint += " · o full plan"
	}
	hint += " · v terminal"

	// Fit: the head, the answers and the hint always; the detail gets the
	// rows left (its end is cut, a plan says how much more there is).
	left := maxH - 2 - len(answers)
	if left < 0 {
		answers = answers[:max(len(answers)+left, 1)]
		left = 0
	}
	if len(detail) > left {
		more := len(detail) - left + 1
		if left > 0 {
			detail = append(detail[:left-1], "⋯ "+strconv.Itoa(more)+" more lines")
		} else {
			detail = nil
		}
	}
	rows := []string{edge.Render(" ┌ "+cut(p.title(), room)) + dim.Render(" "+strings.Repeat("─", max(w-runewidth.StringWidth(p.title())-5, 1)))}
	for _, l := range detail {
		rows = append(rows, edge.Render(" │ ")+text.Render(cut(l, room)))
	}
	for _, a := range answers {
		rows = append(rows, edge.Render(" │ ")+text.Bold(true).Render(cut(a, room)))
	}
	rows = append(rows, edge.Render(" └ ")+dim.Render(cut(hint, room)))
	return rows
}

// agentChatPanelKey handles a key of an empty box while a panel is on
// show: a number answers, o opens the plan. ok is false for other keys.
func (m *Model) agentChatPanelKey(c *agentChatState, s string) (tea.Cmd, bool) {
	p := agentChatPanelFor(c)
	if p == nil || c.input != "" {
		return nil, false
	}
	if s == "o" && p.kind == agentChatAskPlan {
		if err := agentChatOpenPage(m, "Plan · "+c.title, p.call.Detail); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not open the plan", err.Error()), true
		}
		return nil, true
	}
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return nil, false
	}
	n := int(s[0] - '0')
	ch, ok := p.choice(n)
	if !ok {
		return nil, true // not on the menu
	}
	if p.kind == agentChatAskQuestion {
		return m.agentChatAnswer(c, p.choices, n), true
	}
	m.openAgentChatAnswerCard(c, p, ch)
	return nil, true
}

// openAgentChatAnswerCard asks before an answer to a plan or a
// permission prompt (Cancel first).
func (m *Model) openAgentChatAnswerCard(c *agentChatState, p *agentChatPanel, ch agents.Choice) {
	c.ask = &agentChatAsk{buf: c.buf, n: ch.N, label: ch.Label}
	what := "the plan"
	if p.kind == agentChatAskPermission {
		what = "this request"
		if p.call.Tool != "" {
			what = p.call.Tool + " " + agentChatOneLine(p.call.Args, 80)
		}
	}
	body := "Send answer " + strconv.Itoa(ch.N) + " to " + c.title + " for " + what + ":\n\n  " + ch.Label + "\n\n" +
		"It is the same as choosing it in the terminal. termocode checks the screen again first and sends nothing if the question changed."
	m.confirm = confirm.Risky("Answer "+c.title+"?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "send", Title: "Send " + strconv.Itoa(ch.N), Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAgentChatAnswer
}

// answerAgentChatAnswer is the card's answer: Send re-reads the screen
// and answers only while the same answer is on show; Cancel and Esc send
// nothing.
func (m *Model) answerAgentChatAnswer(id string) tea.Cmd {
	c := m.agentChat
	if c == nil || c.ask == nil {
		return nil
	}
	a := c.ask
	c.ask = nil
	if id != "send" || a.buf != c.buf {
		return nil
	}
	m.agentChatSyncChoices(c)
	for _, ch := range c.choices {
		if ch.N == a.n && ch.Label == a.label {
			return m.agentChatAnswer(c, c.choices, a.n)
		}
	}
	return m.pushToastDetail(toast.Info, "The question changed: nothing was sent", "Look at the terminal (v) or answer the new panel.")
}

// agentChatAnswer moves the menu's cursor to answer n and presses Enter.
func (m *Model) agentChatAnswer(c *agentChatState, choices []agents.Choice, n int) tea.Cmd {
	keys := agentChatMenuKeys(choices, n)
	c.choices = nil // the screen moves on; the next poll reads it again
	c.scroll = 0
	return m.agentChatSendKeys(c.buf, keys)
}

// agentChatMenuKeys are the keys that pick answer n: arrows from the
// cursor's row, then Enter; the number itself when no cursor is seen.
func agentChatMenuKeys(choices []agents.Choice, n int) []string {
	cur := 0
	for _, ch := range choices {
		if ch.Cursor {
			cur = ch.N
		}
	}
	if cur == 0 {
		return []string{strconv.Itoa(n)}
	}
	var keys []string
	for ; cur < n; cur++ {
		keys = append(keys, "\x1b[B")
	}
	for ; cur > n; cur-- {
		keys = append(keys, "\x1b[A")
	}
	return append(keys, "\r")
}

// agentChatSendKeys sends keys to the tab one at a time (a CLI reads a
// burst of escape codes as a paste).
func (m *Model) agentChatSendKeys(buf int, keys []string) tea.Cmd {
	if len(keys) == 0 {
		return nil
	}
	agentChatSend(m, buf, keys[0])
	rest := keys[1:]
	if len(rest) == 0 {
		return nil
	}
	return tea.Tick(agentChatEnterDelay, func(time.Time) tea.Msg {
		return applyMsg(func(m *Model) tea.Cmd { return m.agentChatSendKeys(buf, rest) })
	})
}

// agentChatOpenPage opens text read only in an editor tab (a var for
// tests).
var agentChatOpenPage = func(m *Model, name, text string) error {
	if m.nvim == nil {
		return nil
	}
	m.focus = FocusEditor
	m.ensureEditorWindowCurrent()
	return m.openReadOnlyPage(name, text, "", "markdown")
}

// agentChatOneLine is s on one line, cut to n runes.
func agentChatOneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}

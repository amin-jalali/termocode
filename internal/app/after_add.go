package app

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/afteradd"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// After add (owner 2026-11-15): every "add" event goes through one flow
// (internal/afteradd): read-only checks → links to what termocode
// already has → at most one suggestion card. The card is the same on the
// laptop (a confirm dialog, Not now first and focused) and on the web
// (the cards topic, kind suggest). Its one action is an existing palette
// op that runs only on the user's press; Not now is remembered per item
// in after_add.json in the config folder.

const confirmKindAfterAdd confirmKindEnum = 2710

// afterAddCeiling bounds one whole run of checks.
const afterAddCeiling = 30 * time.Second

// afterAddRetryEvery is how often a waiting card looks for a free screen.
const afterAddRetryEvery = 2 * time.Second

// afterAddRules are the rules every engine starts with (the rule files
// add theirs in init).
var afterAddRules []afteradd.Rule

// afterAddRunOp runs a suggestion's action (a test seam).
var afterAddRunOp = afterAddRunOpReal

func afterAddRunOpReal(m *Model, op, _ string) tea.Cmd { return m.dispatchPaletteAction(op) }

// afterAddState is the engine and the cards waiting to be answered: the
// first is the open one (when shown), the rest wait for it.
type afterAddState struct {
	eng      *afteradd.Engine
	cards    []*afterAddCard
	shown    bool // cards[0] is the open laptop dialog
	retrying bool // a retry tick is on its way
}

// afterAddCard is one suggestion waiting for an answer.
type afterAddCard struct{ s afteradd.Suggestion }

// afterAddMsg carries one run's result back to Update.
type afterAddMsg struct{ res afteradd.Result }

// afterAddRetryMsg asks a waiting card to look for a free screen again.
type afterAddRetryMsg struct{}

func (m *Model) afterAddState() *afterAddState {
	if m.afterAddS == nil {
		var later *afteradd.Later
		if dir, err := configDir(); err == nil {
			later = afteradd.OpenLater(filepath.Join(dir, "after_add.json"))
		}
		m.afterAddS = &afterAddState{eng: afteradd.New(later, afterAddRules...)}
	}
	return m.afterAddS
}

// afterAddEngine is the Model's engine, made on first use.
func (m *Model) afterAddEngine() *afteradd.Engine { return m.afterAddState().eng }

// afterAdd runs ev's checks in the background; the result comes back as
// an afterAddMsg.
func (m *Model) afterAdd(ev afteradd.Event) tea.Cmd {
	eng := m.afterAddEngine()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), afterAddCeiling)
		defer cancel()
		return afterAddMsg{res: eng.Run(ctx, ev)}
	}
}

// onAfterAdd queues the run's suggestion and shows it when the screen is
// free.
func (m *Model) onAfterAdd(msg afterAddMsg) tea.Cmd {
	s := msg.res.Suggestion
	if s == nil {
		return nil
	}
	st := m.afterAddState()
	for _, c := range st.cards {
		if c.s.Key() == s.Key() {
			return m.afterAddShowNext() // the same card already waits
		}
	}
	st.cards = append(st.cards, &afterAddCard{s: *s})
	return m.afterAddShowNext()
}

func (m *Model) onAfterAddRetry(afterAddRetryMsg) tea.Cmd {
	m.afterAddState().retrying = false
	return m.afterAddShowNext()
}

// afterAddShowNext opens the first waiting card when no dialog is open,
// else comes back in a moment.
func (m *Model) afterAddShowNext() tea.Cmd {
	st := m.afterAddState()
	if len(st.cards) == 0 || st.shown && m.confirmOpen && m.confirmKind == confirmKindAfterAdd {
		return nil
	}
	if m.confirmOpen || m.connectOpen { // a dialog or the connect card is open
		st.shown = false
		if st.retrying {
			return nil
		}
		st.retrying = true
		return tea.Tick(afterAddRetryEvery, func(time.Time) tea.Msg { return afterAddRetryMsg{} })
	}
	m.openAfterAddCard(st.cards[0])
	st.shown = true
	return nil
}

// afterAddBody is the card's text: the findings, one per line, then what
// the action does and what Not now does.
func afterAddBody(s afteradd.Suggestion) string {
	var b strings.Builder
	for _, f := range s.Findings {
		mark := "•"
		switch f.Level {
		case afteradd.OK:
			mark = "✓"
		case afteradd.Warn:
			mark = "!"
		}
		b.WriteString(mark + " " + f.Text + "\n")
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString("Next: " + s.Action.Label + ".\n")
	b.WriteString("Not now: termocode does not ask again for " + s.Event.Item + ".")
	return ai.Redact(b.String())
}

func (m *Model) openAfterAddCard(c *afterAddCard) {
	m.confirm = confirm.Risky(aiSpark()+" "+c.s.Title, afterAddBody(c.s),
		confirm.Button{ID: "skip", Title: "Not now"},
		confirm.Button{ID: "do", Title: c.s.Action.Label, Style: confirm.StylePrimary}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindAfterAdd
}

// answerAfterAdd answers the first waiting card: "do" runs its action,
// anything else is Not now. The next card shows after it.
func (m *Model) answerAfterAdd(id string) tea.Cmd {
	st := m.afterAddState()
	if len(st.cards) == 0 {
		return nil
	}
	return m.afterAddAnswer(st.cards[0], id == "do")
}

// afterAddAnswer answers card c (the laptop's or the web's answer).
func (m *Model) afterAddAnswer(c *afterAddCard, do bool) tea.Cmd {
	st := m.afterAddState()
	i := 0
	for i < len(st.cards) && st.cards[i] != c {
		i++
	}
	if i == len(st.cards) {
		return nil
	}
	if i == 0 && st.shown {
		if m.confirmOpen && m.confirmKind == confirmKindAfterAdd {
			m.confirmOpen = false
		}
		st.shown = false
	}
	st.cards = append(st.cards[:i], st.cards[i+1:]...)
	var cmds []tea.Cmd
	if do {
		cmds = append(cmds, afterAddRunOp(m, c.s.Action.Op, c.s.Action.Arg))
	} else if err := st.eng.NotNow(c.s); err != nil {
		var t tea.Cmd
		m.toast, t = m.toast.PushDetail(toast.Info, "Could not save Not now", err.Error())
		cmds = append(cmds, t)
	}
	cmds = append(cmds, m.afterAddShowNext())
	return tea.Batch(cmds...)
}

// webAfterAddCards are the waiting suggestions as web cards.
func (m *Model) webAfterAddCards(add func(obj any, c webproto.Card)) {
	if m.afterAddS == nil {
		return
	}
	for _, c := range m.afterAddS.cards {
		add(c, webproto.Card{Kind: webproto.CardSuggest, Title: c.s.Title, Body: afterAddBody(c.s),
			Choices: []webproto.CardChoice{{ID: webproto.ChoiceLater, Label: "Not now", Safe: true}, {ID: webproto.ChoiceDo, Label: c.s.Action.Label}}})
	}
}

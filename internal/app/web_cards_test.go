package app

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestCards decodes the cards snap of c's first frame.
func webTestCards(t *testing.T, c *webConn) []webproto.Card {
	t.Helper()
	var s webproto.CardsSnap
	for _, f := range webTestNow(t, c) {
		if f["t"] == "snap" && f["topic"] == "cards" {
			webTestData(t, f, &s)
			return s.Cards
		}
	}
	t.Fatal("no cards snap")
	return nil
}

// webTestToolCard opens the laptop's tool card as onAIChatConfirm does;
// the returned channel gets the answer.
func webTestToolCard(m *Model, tool ai.Tool) chan bool {
	reply := make(chan bool, 1)
	m.ai.confirm = &aiPendingConfirm{tool: tool, human: "Write a.go", reply: reply}
	m.confirm = confirm.New("✦ AI wants to: "+tool.Label(), "Write a.go", aiToolCardButtons(tool))
	m.confirmOpen, m.confirmKind = true, confirmKindAITool
	return reply
}

// webTestMCPAsk queues an outside agent's call (active: its card is open).
func webTestMCPAsk(t *testing.T, m *Model, always string, active bool) *mcpServeAsk {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &mcpServeAsk{ctx: ctx, client: "codex", tool: ai.Tool{Name: "run_task"}, human: "Run task: build", always: always, reply: make(chan bool, 1)}
	if m.mcpServe == nil {
		m.mcpServe = &mcpServeState{}
	}
	if active {
		m.mcpServe.active = a
		m.confirmOpen, m.confirmKind = true, confirmKindMCPServe
	} else {
		m.mcpServe.queue = append(m.mcpServe.queue, a)
	}
	return a
}

// webTestReview opens a review with one file; done records the decision.
func webTestReview(m *Model, name, old, new string, done *map[int]bool) {
	it := &aiReviewItem{file: diffview.File{Name: name, Old: old, New: new},
		finish: func(_ *Model, _ *textdiff.Diff, acc map[int]bool, _ bool) (string, tea.Cmd) {
			*done = acc
			return "done", nil
		}}
	m.addAIReview(it)
}

// Spec (WEB-15): every card a browser may answer lists the safe answer
// first (Deny, Reject); tool and outside-agent cards offer Allow once.
func TestCardsDenyIsFirst(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestToolCard(&m, ai.Tool{Name: "write_file", Class: "write"})
	webTestMCPAsk(t, &m, "", false)
	var done map[int]bool
	webTestReview(&m, "a.go", "a\n", "b\n", &done)
	c := webTestSub(t, &m, "cards")
	cards := webTestCards(t, c)
	if len(cards) != 3 {
		t.Fatalf("cards %+v", cards)
	}
	for _, card := range cards {
		if len(card.Choices) < 2 || !card.Choices[0].Safe || card.Choices[1].Safe {
			t.Errorf("%s card: choices %+v", card.Kind, card.Choices)
		}
		if card.ID == "" || card.Since == 0 {
			t.Errorf("%s card has no id or time: %+v", card.Kind, card)
		}
	}
	if cards[0].Kind != "tool" || cards[0].Choices[1].Label != "Allow once" || cards[1].Kind != "mcp" || cards[2].Kind != "review" {
		t.Errorf("kinds %+v", cards)
	}
}

// Spec (WEB-15): a new card is an attention event for every browser.
func TestCardAttention(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	m.webAfterUpdate(nil)
	webTestToolCard(&m, ai.Tool{Name: "write_file"})
	m.webAfterUpdate(nil)
	m.webAfterUpdate(nil)
	if ev := webTestEvents(t, c); len(ev) != 1 || ev[0].Kind != "card" || ev[0].ID == "" {
		t.Fatalf("events %v", ev)
	}
}

// Spec (WEB-15): the cards replay golden: a tool card and an outside
// agent's card appear, the laptop answers the tool card.
func TestCardsReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	webTestClock(t)
	h := webTestHub(t, &m)
	c := webReplayRecord(t, &m, h, "cards", func() {
		webTestToolCard(&m, ai.Tool{Name: "write_file", Class: "write"})
		m.webAfterUpdate(nil)
		webTestMCPAsk(t, &m, "", false)
		m.webAfterUpdate(nil)
		m.handleAIConfirm("deny")
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["cards"], c)
	webReplayGolden(t, webReplayPath("cards", "appear_answered"), c)
}

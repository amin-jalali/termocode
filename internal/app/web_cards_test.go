package app

import (
	"context"
	"strings"
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

// Spec (WEB-15, T42): "Always allow" is never a web choice, even where the
// laptop card has it; sending it is forbidden and saves nothing.
func TestCardsNoAlwaysAllowForWeb(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	reply := webTestToolCard(&m, ai.Tool{Name: "write_file", Class: "write"})
	webTestMCPAsk(t, &m, "run_task", false)
	c := webTestSub(t, &m, "cards")
	for _, card := range webTestCards(t, c) {
		for _, ch := range card.Choices {
			if strings.Contains(strings.ToLower(ch.Label), "always") || ch.ID == "always" || ch.ID == "project" {
				t.Errorf("%s card offers %+v", card.Kind, ch)
			}
		}
		for _, choice := range []string{"always", "project"} {
			if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+card.ID+`","choice":"`+choice+`"}`); f["code"] != "forbidden" {
				t.Errorf("%s %s: %v", card.Kind, choice, f)
			}
		}
	}
	if len(reply) != 0 || len(m.ai.settings.AutoApprove) != 0 || m.ai.confirm == nil {
		t.Fatalf("a forbidden answer changed something: reply=%d auto=%v", len(reply), m.ai.settings.AutoApprove)
	}
}

// Spec (WEB-15): the first answer wins. A browser's Allow once answers the
// AI, closes the laptop's dialog, shows "Answered on phone" on the laptop,
// writes an audit line by=web:phone, and every browser gets the remove
// and an answered event by "phone"; a second answer is gone. A card the
// laptop answers reaches the browsers as answered by "laptop".
func TestAnswerOnceWinsAndEvAnswered(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	reply := webTestToolCard(&m, ai.Tool{Name: "write_file", Class: "write"})
	phone := webTestSub(t, &m, "cards")
	other := webTestSub(t, &m)
	id := webTestCards(t, phone)[0].ID
	m.webAfterUpdate(nil)
	webTestNow(t, other)
	f, _ := webTestCall(t, &m, phone, webproto.OpCardsAnswer, `{"id":"`+id+`","choice":"allow"}`)
	if f["t"] != "res" || !<-reply || m.confirmOpen || m.ai.confirm != nil {
		t.Fatalf("answer: %v open=%v", f, m.confirmOpen)
	}
	if lines := m.output.Lines(aiAuditChannel); len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "by=web:phone") || !strings.Contains(lines[len(lines)-1], "tool=write_file") {
		t.Errorf("audit %q", lines)
	}
	if !strings.Contains(webTestToasts(m), "Answered on phone") {
		t.Errorf("laptop toast: %q", webTestToasts(m))
	}
	m.webAfterUpdate(nil)
	var removed *webproto.CardsDelta
	for _, f := range webTestNow(t, phone) {
		if f["t"] == "delta" {
			var d webproto.CardsDelta
			webTestData(t, f, &d)
			removed = &d
		}
	}
	if removed == nil || removed.Op != "remove" || removed.ID != id || removed.By != "phone" {
		t.Fatalf("remove %+v", removed)
	}
	if ev := webTestAnswered(t, other); len(ev) != 1 || ev[0] != (webproto.Answered{ID: id, By: "phone"}) {
		t.Fatalf("other browser: %v", ev)
	}
	if f, _ := webTestCall(t, &m, other, webproto.OpCardsAnswer, `{"id":"`+id+`","choice":"deny"}`); f["code"] != "gone" {
		t.Fatalf("second answer: %v", f)
	}

	reply = webTestToolCard(&m, ai.Tool{Name: "write_file"})
	m.webAfterUpdate(nil)
	webTestNow(t, other)
	m.handleAIConfirm("deny") // on the laptop
	m.webAfterUpdate(nil)
	if ev := webTestAnswered(t, other); len(ev) != 1 || ev[0].By != "laptop" || <-reply {
		t.Fatalf("laptop answer: %v", ev)
	}
}

func webTestToasts(m Model) string {
	var b strings.Builder
	for _, t := range m.toast.Live() {
		b.WriteString(t.Title + " " + t.Detail + "\n")
	}
	return b.String()
}

func webTestAnswered(t *testing.T, c *webConn) []webproto.Answered {
	t.Helper()
	var out []webproto.Answered
	for _, f := range webTestNow(t, c) {
		if f["t"] == "ev" && f["kind"] == "answered" {
			var a webproto.Answered
			webTestData(t, f, &a)
			out = append(out, a)
		}
	}
	return out
}

// Spec (WEB-15, T42): trust cards are shown read-only with no choice;
// any answer is forbidden. A plan's Edit needs the laptop.
func TestTrustCardReadOnly(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	m.ai.rulesTrust = &aiRulesTrustPending{hash: "h"}
	c := webTestSub(t, &m, "cards")
	cards := webTestCards(t, c)
	if len(cards) != 1 || cards[0].Kind != "trust" || !cards[0].ReadOnly || len(cards[0].Choices) != 0 {
		t.Fatalf("trust %+v", cards)
	}
	for _, choice := range []string{"trust", "allow", "deny", "use"} {
		if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[0].ID+`","choice":"`+choice+`"}`); f["code"] != "forbidden" {
			t.Errorf("%s: %v", choice, f)
		}
	}
	if m.ai.rulesTrust == nil {
		t.Fatal("the trust question was answered from the web")
	}
}

// Spec (WEB-15): a review card carries the diff rows, cut at 256 KiB; the
// web's Accept applies the whole file, Reject none of it.
func TestReviewDiffCapped(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	big := strings.Repeat(strings.Repeat("x", 99)+"\n", 4000) // ~400 KiB
	var doneA, doneB map[int]bool
	webTestReview(&m, "big.txt", "", big, &doneA)
	webTestReview(&m, "small.go", "a\nb\n", "a\nc\n", &doneB)
	c := webTestSub(t, &m, "cards")
	cards := webTestCards(t, c)
	if len(cards) != 2 || cards[0].Diff == nil || !cards[0].Diff.Truncated || cards[0].Diff.Added != 4000 {
		t.Fatalf("big diff: %+v", cards[0].Diff)
	}
	size := 0
	for _, r := range cards[0].Diff.Rows {
		size += len(r.Text)
	}
	if size > webproto.MaxCardDiff {
		t.Fatalf("rows carry %d bytes", size)
	}
	if d := cards[1].Diff; d == nil || d.Truncated || len(d.Rows) != 3 || d.Rows[1].K != "del" || d.Rows[2].K != "add" {
		t.Fatalf("small diff: %+v", cards[1].Diff)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[1].ID+`","choice":"accept"}`); f["t"] != "res" || len(doneB) != 1 {
		t.Fatalf("accept: %v %v", f, doneB)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[0].ID+`","choice":"reject"}`); f["t"] != "res" || doneA == nil || len(doneA) != 0 {
		t.Fatalf("reject: %v %v", f, doneA)
	}
	if m.ai.review != nil {
		t.Fatal("the review stays open after both files")
	}
}

// Spec (WEB-15, T42): the web cannot widen the AI: an unknown choice, a
// plan's Edit and a choice of another card's kind are forbidden; a card
// that is not waiting is gone.
func TestWebCannotWidenAI(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	m.mcpServe = &mcpServeState{}
	a := webTestMCPAsk(t, &m, "run_task", false)
	c := webTestSub(t, &m, "cards")
	id := webTestCards(t, c)[0].ID
	for _, choice := range []string{"edit", "approve", "accept", "trust", "pair", "give_control"} {
		if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+id+`","choice":"`+choice+`"}`); f["code"] != "forbidden" && f["code"] != "bad_request" {
			t.Errorf("%q: %v", choice, f)
		}
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"card-999","choice":"deny"}`); f["code"] != "gone" {
		t.Errorf("unknown card: %v", f)
	}
	// A queued call answered from the web leaves the line, answered.
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+id+`","choice":"deny"}`); f["t"] != "res" || <-a.reply || len(m.mcpServe.queue) != 0 {
		t.Fatalf("deny queued: %v", f)
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

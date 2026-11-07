package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/textdiff"
)

// orderTestItem is a review file whose finish records its name.
func orderTestItem(name, old, new string, finished *[]string) *aiReviewItem {
	return &aiReviewItem{file: diffview.File{Name: name, Old: old, New: new},
		finish: func(_ *Model, _ *textdiff.Diff, _ map[int]bool, _ bool) (string, tea.Cmd) {
			*finished = append(*finished, name)
			return "Applied the change", nil
		}}
}

func reviewNames(m *Model) string {
	var out []string
	for _, it := range m.aiS().review.items {
		out = append(out, it.file.Name)
	}
	return strings.Join(out, ",")
}

// Spec (CMP-58): the agent's files are reviewed explain-first: the doc,
// then the file that defines a new name, then its user, the test last.
// The card and the web cards follow; each moved file says why. The
// toggle brings back the agent's order and is saved.
func TestAIReviewExplainFirstOrder(t *testing.T) {
	m := aiTestModel(t)
	m.w, m.h = 160, 40
	var done []string
	m.addAIReview(orderTestItem("app/handler_test.go", "", "func TestServe(t *testing.T) { Serve() }\n", &done))
	m.addAIReview(orderTestItem("app/handler.go", "package app\n", "package app\n\nfunc Serve() { ParseQuery(q) }\n", &done))
	m.addAIReview(orderTestItem("query/parse.go", "", "package query\n\nfunc ParseQuery(s string) {}\n", &done))
	r := m.aiS().review
	if f, _ := r.card.FileAt(r.card.Current()); f.Name != "app/handler_test.go" {
		t.Errorf("an open card keeps showing its file, shows %s", f.Name)
	}
	m.reviewOpen = false // hidden (Esc): the next file opens the card on the first file
	m.addAIReview(orderTestItem("docs/query.md", "", "# Query\n", &done))
	if got := reviewNames(&m); got != "docs/query.md,query/parse.go,app/handler.go,app/handler_test.go" {
		t.Fatalf("review order: %s", got)
	}
	for i, it := range r.items {
		if f, _ := r.card.FileAt(i); f.Name != it.file.Name {
			t.Fatalf("card file %d is %s, item is %s", i, f.Name, it.file.Name)
		}
	}
	if r.card.Current() != 0 {
		t.Errorf("the hidden card must open on the first file, shows %d", r.card.Current())
	}
	r.card.Select(1)
	frame := ansi.Strip(r.card.Box())
	if !strings.Contains(frame, "defines ParseQuery, used by handler.go") {
		t.Errorf("the reason is not on screen:\n%s", frame)
	}
	cards := m.webCards(&webCardsPub{}, false)
	if len(cards) != 4 || cards[0].card.File != "docs/query.md" || !strings.Contains(cards[1].card.Body, "defines ParseQuery") {
		t.Errorf("web cards: %+v", cards)
	}

	// A key's FinishMsg names the file by id: a reorder before the
	// message arrives must not finish another file.
	_, cmd := r.card.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	msg := cmd()
	m.toggleAIReviewOrder() // back to the agent's order: parse.go moves
	if got := reviewNames(&m); got != "app/handler_test.go,app/handler.go,query/parse.go,docs/query.md" {
		t.Fatalf("proposed order: %s", got)
	}
	if _, ok := m.handleAIMsg(msg); !ok {
		t.Fatal("finish not routed")
	}
	if len(done) != 1 || done[0] != "query/parse.go" {
		t.Errorf("finished %v, want query/parse.go", done)
	}
	if !strings.Contains(covToastText(&m), "order the agent proposed") {
		t.Errorf("toast: %q", covToastText(&m))
	}
	p, _ := ai.ConfigPath()
	if ai.LoadSettings(p).ReviewOrder != ai.ReviewOrderProposed {
		t.Error("the setting was not saved")
	}
	m.toggleAIReviewOrder()
	if got := reviewNames(&m); got != "docs/query.md,query/parse.go,app/handler.go,app/handler_test.go" || !m.aiS().settings.ExplainFirst() {
		t.Errorf("explain order again: %s", got)
	}
}

// With ai_review_order = proposed the files stay in the agent's order.
func TestAIReviewProposedOrder(t *testing.T) {
	m := aiTestModel(t)
	m.aiS().settings.ReviewOrder = ai.ReviewOrderProposed
	var done []string
	m.addAIReview(orderTestItem("a_test.go", "", "x\n", &done))
	m.addAIReview(orderTestItem("README.md", "", "y\n", &done))
	if got := reviewNames(&m); got != "a_test.go,README.md" {
		t.Errorf("order: %s", got)
	}
}

// The toggle is a palette command the AI may never run; the web offers
// it behind a laptop card.
func TestAIReviewOrderCommand(t *testing.T) {
	found := false
	for _, it := range paletteItems() {
		if it.ID == "ai-review-order" {
			found = true
		}
	}
	if !found {
		t.Fatal("ai-review-order is not in the palette")
	}
	if p, ok := aiActionPolicies["ai-review-order"]; !ok || p.Gate != aiGateNever {
		t.Errorf("policy: %+v %v", p, ok)
	}
	if p, ok := aiSettingPolicies["ai_review_order"]; !ok || p.Gate != aiGateNever {
		t.Errorf("setting policy: %+v %v", p, ok)
	}
	if card, ok := webPaletteAllow["ai-review-order"]; !ok || !card || webPaletteUserOnly["ai-review-order"] == "" {
		t.Error("the web must offer it after a laptop card")
	}
	m := aiTestModel(t)
	if cmd, ok := m.dispatchAIPalette("ai-review-order"); !ok || cmd == nil || m.aiS().settings.ExplainFirst() {
		t.Errorf("dispatch: ok=%v explain=%v", ok, m.aiS().settings.ExplainFirst())
	}
}

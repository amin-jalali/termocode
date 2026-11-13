//go:build unix

package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (MOBUX-12): a tool card for a destructive call (mutating with no
// "Always allow" class, or a database write) is marked destructive, so
// the phone's approval sheet offers only "Approve on laptop". A call with
// an "Always allow" class is not destructive. The safe Deny stays first.
func TestCardsDestructiveToolMarked(t *testing.T) {
	for _, w := range []struct {
		name string
		tool ai.Tool
		want bool
	}{
		{"no class", ai.Tool{Name: "git_discard", Mutating: true}, true},
		{"db write", ai.Tool{Name: "db_exec", Mutating: true, Class: "db"}, true},
		{"file write", ai.Tool{Name: "write_file", Mutating: true, Class: ai.ClassFileWrite}, false},
		{"run", ai.Tool{Name: "run_command", Mutating: true, Class: ai.ClassRun}, false},
	} {
		t.Run(w.name, func(t *testing.T) {
			m := aiTestModel(t)
			webTestHub(t, &m)
			webTestToolCard(&m, w.tool)
			c := webTestSub(t, &m, "cards")
			cards := webTestCards(t, c)
			if len(cards) != 1 {
				t.Fatalf("cards %+v", cards)
			}
			got := cards[0]
			if got.Destructive != w.want {
				t.Errorf("destructive = %v, want %v", got.Destructive, w.want)
			}
			if len(got.Choices) == 0 || !got.Choices[0].Safe {
				t.Errorf("choices %+v: the safe answer is not first", got.Choices)
			}
		})
	}
}

// Spec (MOBUX-12): only tool cards are marked; outside agents' cards and
// reviews are not.
func TestCardsDestructiveOnlyOnToolCards(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestMCPAsk(t, &m, "", false)
	var done map[int]bool
	webTestReview(&m, "a.go", "a\n", "b\n", &done)
	c := webTestSub(t, &m, "cards")
	for _, card := range webTestCards(t, c) {
		if card.Destructive {
			t.Errorf("%s card is marked destructive", card.Kind)
		}
	}
}

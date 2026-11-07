package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/difforder"
)

// Explain-first review order (CMP-58). When the agent proposes several
// files, the review card lists them so that the "why" comes before the
// "how": notes and docs first, then schemas and the files that define new
// names, then the code that uses those names, and tests last. Each moved
// file says why it is where it is ("defines Foo, used by bar.go"). The
// web's review cards follow the same order. ai_review_order = proposed
// keeps the agent's order; AI: Toggle Review Order switches it.

// orderAIReview puts the review's files in explain-first order (when the
// setting is on) and keeps r.items in step with the card. Done files
// move too: the list shows the whole change in one reading order.
func (m *Model) orderAIReview() {
	s := m.aiS()
	r := s.review
	if r == nil || !r.card.Review() || len(r.items) < 2 || r.card.Files() != len(r.items) {
		return
	}
	files := make([]difforder.File, len(r.items))
	for i, it := range r.items {
		files[i] = difforder.File{Name: it.file.Name, Old: it.file.Old, New: it.file.New}
	}
	var placed []difforder.Placed
	if s.settings.ExplainFirst() {
		placed = difforder.Order(files)
	} else {
		placed = proposedOrder(r)
	}
	perm := make([]int, len(placed))
	whys := make([]string, len(placed))
	for k, p := range placed {
		perm[k] = p.Index
		whys[k] = p.Reason
	}
	if !r.card.Reorder(perm, whys) {
		return
	}
	items := make([]*aiReviewItem, len(perm))
	for k, i := range perm {
		items[k] = r.items[i]
		items[k].file.Why = whys[k]
	}
	r.items = items
}

// proposedOrder is the order the files were proposed in (each item's
// seq), with no reasons: it undoes an explain-first order.
func proposedOrder(r *aiReview) []difforder.Placed {
	out := make([]difforder.Placed, len(r.items))
	for i := range r.items {
		out[i] = difforder.Placed{Index: i}
	}
	// insertion sort by seq: reviews hold a handful of files
	for a := 1; a < len(out); a++ {
		for b := a; b > 0 && r.items[out[b].Index].seq < r.items[out[b-1].Index].seq; b-- {
			out[b], out[b-1] = out[b-1], out[b]
		}
	}
	return out
}

// toggleAIReviewOrder flips ai_review_order between explain and proposed
// and reorders an open review at once.
func (m *Model) toggleAIReviewOrder() tea.Cmd {
	s := m.aiS()
	next := ai.ReviewOrderProposed
	if !s.settings.ExplainFirst() {
		next = ai.ReviewOrderExplain
	}
	if path, err := ai.ConfigPath(); err == nil {
		cur := ai.LoadSettings(path)
		cur.ReviewOrder = next
		if err := ai.SaveSettings(path, cur); err != nil {
			return m.aiToastErr("Could not save the setting", err)
		}
	}
	s.settings.ReviewOrder = next
	m.orderAIReview()
	if next == ai.ReviewOrderProposed {
		return m.aiInfo("AI review: files in the order the agent proposed them")
	}
	return m.aiInfo("AI review: files that explain the change first (docs, definitions, code, tests last)")
}

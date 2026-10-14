package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/textdiff"
)

// The review card (Group R): every proposed change — a one-shot AI edit,
// a chat code block, or (R2) a file the agent wants to write — is shown
// in the diff card (internal/diffview) with Accept / Reject per change
// and per file. Port of mobocode's review-then-apply flow.
//
// An aiReviewItem knows how to apply itself (finish). The card can hold
// several files; Esc hides it while files are still waiting, and the AI
// panel shows a "N changes to review" bar to bring it back.

// aiReviewItem is one file in the review card.
type aiReviewItem struct {
	file diffview.File
	// gen is the chat turn that proposed it (0: not the agent).
	gen uint64
	// finish applies the user's choice and returns the summary shown on
	// the card. It runs on Update.
	finish func(m *Model, d *textdiff.Diff, acc map[int]bool, edit bool) (string, tea.Cmd)
	// cancel runs when the review is dropped (Stop, New Chat) before the
	// user decided.
	cancel func()
	done   bool
}

// aiReview is the open review: its files and the card.
type aiReview struct {
	items []*aiReviewItem
	card  diffview.Model
}

// addAIReview adds a file to the review and shows the card.
func (m *Model) addAIReview(it *aiReviewItem) {
	s := m.aiS()
	if s.review == nil {
		s.review = &aiReview{card: diffview.New("Review AI changes", nil, true)}
	}
	s.review.items = append(s.review.items, it)
	i := s.review.card.AddFile(it.file)
	if s.review.card.Pending() == 1 || !m.reviewOpen {
		s.review.card.Select(i)
	}
	m.showAIReview()
}

// showAIReview opens the card (when a review is waiting).
func (m *Model) showAIReview() tea.Cmd {
	s := m.aiS()
	if s.review == nil {
		return m.aiInfo("No AI changes are waiting for review")
	}
	s.review.card.SetSize(m.w, m.h)
	m.reviewOpen = true
	return nil
}

// aiReviewPending is the number of files waiting for a decision.
func (m Model) aiReviewPending() int {
	if m.ai == nil || m.ai.review == nil || !m.ai.review.card.Review() {
		return 0
	}
	return m.ai.review.card.Pending()
}

// routeToReview sends keys, mouse and resizes to the open card.
func (m Model) routeToReview(msg tea.Msg) (Model, tea.Cmd) {
	r := m.aiS().review
	if r == nil {
		m.reviewOpen = false
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		r.card.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		if m.mouseProbe != nil {
			if id := r.card.ButtonAt(msg.X, msg.Y); id != "" {
				m.probed("review:" + id)
			} else {
				m.probed("review")
			}
			return m, nil
		}
		var cmd tea.Cmd
		r.card, cmd, _ = r.card.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	case tea.KeyMsg:
		var cmd tea.Cmd
		r.card, cmd = r.card.Update(msg)
		return m, cmd
	}
	return m, nil
}

// onAIReviewFinish applies one file's decision.
func (m *Model) onAIReviewFinish(msg diffview.FinishMsg) tea.Cmd {
	s := m.aiS()
	r := s.review
	if r == nil || msg.File < 0 || msg.File >= len(r.items) {
		return nil
	}
	it := r.items[msg.File]
	if it.done {
		return nil
	}
	it.done = true
	summary, cmd := it.finish(m, r.card.Diff(msg.File), msg.Accepted, msg.Edit)
	r.card.MarkDone(msg.File, summary)
	if msg.Edit {
		m.reviewOpen = false
	}
	if r.card.Pending() == 0 {
		m.closeAIReview()
	}
	return cmd
}

// closeAIReview drops the review (every file is done, or it was
// cancelled) and hides the card.
func (m *Model) closeAIReview() {
	s := m.aiS()
	if r := s.review; r != nil {
		for _, it := range r.items {
			if !it.done {
				it.done = true
				if it.cancel != nil {
					it.cancel()
				}
			}
		}
	}
	s.review = nil
	m.reviewOpen = false
}

// cancelAIReviewTurn drops the files an agent turn proposed (Stop, New
// Chat): they are marked "not applied" and the turn's tool calls return.
func (m *Model) cancelAIReviewTurn() {
	s := m.aiS()
	r := s.review
	if r == nil {
		return
	}
	for i, it := range r.items {
		if it.gen != 0 && !it.done {
			it.done = true
			if it.cancel != nil {
				it.cancel()
			}
			r.card.MarkDone(i, "Stopped: not applied")
		}
	}
	if r.card.Pending() == 0 {
		m.closeAIReview()
	}
}

// reviewAcceptCurrent / reviewRejectCurrent are the palette commands.
func (m *Model) reviewDecideCurrent(accept bool) tea.Cmd {
	s := m.aiS()
	if s.review == nil {
		return m.aiInfo("No AI edit is pending")
	}
	m.showAIReview()
	id := "reject-file"
	if accept {
		id = "accept-file"
	}
	return s.review.card.Action(id)
}

// aiReviewSummary describes the accepted part of a change for the card
// and for the model.
func aiReviewSummary(d *textdiff.Diff, acc map[int]bool) string {
	n := max(len(d.Hunks), 1)
	k := 0
	for i := 0; i < n; i++ {
		if acc[i] {
			k++
		}
	}
	switch {
	case k == 0:
		return "Rejected: not applied"
	case k == n && n == 1:
		return "Applied the change"
	case k == n:
		return fmt.Sprintf("Applied all %d changes", n)
	}
	return fmt.Sprintf("Applied %d of %d changes", k, n)
}

// joinLines turns buffer lines into file text (with a final newline).
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// openBufferProposal reviews new text for an open buffer (one-shot AI
// edits, chat code blocks). Accepting changes the buffer in ONE undo
// step and leaves it unsaved, as an edit by hand would.
func (m *Model) openBufferProposal(p aiProposal) tea.Cmd {
	t := p.target
	proposed := make([]string, 0, len(t.full)-(t.end-t.start)+len(p.repl))
	proposed = append(proposed, t.full[:t.start]...)
	proposed = append(proposed, p.repl...)
	proposed = append(proposed, t.full[t.end:]...)
	name := aiRel(aiWorkspaceRoot())(t.path)
	it := &aiReviewItem{file: diffview.File{Name: name, Old: joinLines(t.full), New: joinLines(proposed), Tag: p.title}}
	it.finish = func(m *Model, d *textdiff.Diff, acc map[int]bool, edit bool) (string, tea.Cmd) {
		sum := aiReviewSummary(d, acc)
		if len(acc) == 0 {
			return sum, m.aiInfo("AI edit discarded")
		}
		final := textdiff.SplitLines(d.Apply(acc))
		res, err := m.applyLinesToBuffer(t.buf, t.full, final)
		switch {
		case err != nil:
			return "Apply failed", m.aiToastErr("Apply failed", err)
		case res == "stale":
			return "Not applied: the file changed", m.aiInfo("The file changed since the AI read it — edit discarded")
		}
		m.focus = FocusEditor
		return sum, m.aiInfo(sum + " (one undo step)")
	}
	m.addAIReview(it)
	return nil
}

// applyLinesToBuffer replaces the changed middle of buf (old → new) in
// one nvim_buf_set_lines call (one undo step). "stale" when the buffer
// no longer holds old.
func (m *Model) applyLinesToBuffer(buf int, old, new []string) (string, error) {
	if m.nvim == nil {
		return "", nil
	}
	p := 0
	for p < len(old) && p < len(new) && old[p] == new[p] {
		p++
	}
	s := 0
	for s < len(old)-p && s < len(new)-p && old[len(old)-1-s] == new[len(new)-1-s] {
		s++
	}
	var res string
	err := m.nvim.ExecLuaResult(aiProposalApplyLua, &res, buf, p, len(old)-s, nonNil(new[p:len(new)-s]), nonNil(old[p:len(old)-s]), len(old))
	return res, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

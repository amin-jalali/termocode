package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// GITX-22: new line comments from the PR diff card. c comments on the
// focused change (n / p move it), s suggests new text for its line. The
// comment goes on the change's last line in the new file (its last
// removed line in the old file for a pure removal), at the head commit.

const (
	promptKindPRLineComment promptKindEnum = 3211
	promptKindPRSuggest     promptKindEnum = 3212
)

// prLineTarget is the line a new comment goes on.
type prLineTarget struct {
	path string
	line int
	side string
	text string // the line's text (a suggestion starts from it)
}

func (t prLineTarget) where() string { return fmt.Sprintf("%s:%d", t.path, t.line) }

// prFocusedLine finds the real line of the focused change in the card.
func (m *Model) prFocusedLine() (prLineTarget, bool) {
	s := m.prS()
	r := m.aiS().review
	if r == nil {
		return prLineTarget{}, false
	}
	i := r.card.Current()
	if i < 0 || i >= len(s.diff) {
		return prLineTarget{}, false
	}
	f := s.diff[i]
	h, ok := r.card.FocusedHunk()
	if !ok {
		return prLineTarget{}, false
	}
	pick := func(text string, nums []int, idx int, side string) (prLineTarget, bool) {
		lines := strings.Split(text, "\n")
		if idx < 0 || idx >= len(nums) || idx >= len(lines) || nums[idx] <= 0 {
			return prLineTarget{}, false
		}
		path := f.Path
		if side == "LEFT" && f.OldPath != "" {
			path = f.OldPath
		}
		return prLineTarget{path: path, line: nums[idx], side: side, text: lines[idx]}, true
	}
	if h.NewCount > 0 {
		return pick(f.New, f.NewLines, h.NewStart+h.NewCount-1, "RIGHT")
	}
	if h.OldCount > 0 {
		return pick(f.Old, f.OldLines, h.OldStart+h.OldCount-1, "LEFT")
	}
	return prLineTarget{}, false
}

// prAskLineComment opens the prompt of a comment (suggest false) or a
// suggestion for the focused change.
func (m *Model) prAskLineComment(suggest bool) tea.Cmd {
	d := m.prS().detail
	if d == nil || d.State != "OPEN" || d.HeadSHA == "" {
		return m.aiInfo("Line comments need an open pull request (read it again with Git: Pull Requests…)")
	}
	t, ok := m.prFocusedLine()
	if !ok {
		return m.aiInfo("Move to a change first (n / p), then press c or s")
	}
	if suggest && t.side != "RIGHT" {
		return m.aiInfo("A suggestion needs a line of the new file: this change only removes lines")
	}
	m.prS().line = &t
	if suggest {
		m.prompt = prompt.New("Suggest a change for "+t.where(), "New text of the line:", t.text)
		m.prompt.SetAllowEmpty(true) // empty: suggest deleting the line
		m.promptKind = promptKindPRSuggest
	} else {
		m.prompt = prompt.New("Comment on "+t.where(), "Your comment:", "")
		m.promptKind = promptKindPRLineComment
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	return nil
}

// handlePRLineCommentSubmit sends a comment or suggestion. ok=false: not ours.
func (m *Model) handlePRLineCommentSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindPRLineComment && m.promptKind != promptKindPRSuggest {
		return nil, false
	}
	s := m.prS()
	t, d := s.line, s.detail
	s.line = nil
	if t == nil || d == nil {
		return nil, true
	}
	body := value
	if m.promptKind == promptKindPRSuggest {
		body = ghci.Suggestion(value)
	}
	c := ghci.LineComment{Commit: d.HeadSHA, Path: t.path, Line: t.line, Side: t.side, Body: body}
	n, where := d.Number, t.where()
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		err := ghci.AddComment(ctx, run, dir, n, c)
		if errors.Is(err, ghci.ErrNeedsText) {
			err = errors.New("write a comment first")
		}
		return prDoneMsg{number: n, ok: "Commented on " + where, detail: "It shows in the PR view under Review threads",
			failed: "Could not comment on " + where, err: err}
	}), true
}

package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/ghci"
)

// Merge…: a card with the three merge methods. Cancel is the first,
// focused button (AH-1), so a stray Enter never merges. The card shows
// the checks and warns when they are red or still running. There is no
// admin override: a merge the repository's rules refuse stays refused.

const confirmKindPRMerge confirmKindEnum = 3201

// prMergeMethods maps a button id to its merge method.
var prMergeMethods = map[string]ghci.MergeMethod{
	"squash": ghci.MergeSquash, "merge": ghci.MergeCommit, "rebase": ghci.MergeRebase,
}

// prMergeText is the card body: what is merged and what is wrong.
func prMergeText(d ghci.PRDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Merge %s %q from %s into %s?\n\n", prRef(d.Number), d.Title, d.Branch, d.Base)
	if w := prMergeWarnings(d); len(w) > 0 {
		b.WriteString(strings.Join(w, "\n") + "\n\n")
	} else if d.ChecksState() == ghci.StatePass {
		b.WriteString("All checks passed.\n\n")
	}
	b.WriteString("Squash: one commit on " + d.Base + ". Merge: a merge commit. Rebase: each commit on top of " + d.Base + ".\n")
	b.WriteString("The branch stays. Rules of the repository still apply.")
	return b.String()
}

// prMergeWarnings lists what the user should know before merging.
func prMergeWarnings(d ghci.PRDetail) []string {
	var w []string
	switch d.ChecksState() {
	case ghci.StateFail:
		n := 0
		for _, c := range d.Checks {
			if c.State == ghci.StateFail {
				n++
			}
		}
		w = append(w, fmt.Sprintf("Warning: %d of %d checks failed.", n, len(d.Checks)))
	case ghci.StatePending:
		w = append(w, "Warning: checks are still running.")
	}
	if d.Mergeable == "CONFLICTING" {
		w = append(w, "Warning: the branch has conflicts with "+d.Base+".")
	}
	if d.Review == "CHANGES_REQUESTED" {
		w = append(w, "Warning: a reviewer asked for changes.")
	}
	return w
}

// prAskMerge opens the merge card.
func (m *Model) prAskMerge() tea.Cmd {
	d := m.prS().detail
	if d == nil {
		return nil
	}
	if d.State != "OPEN" {
		return m.aiInfo(prRef(d.Number) + " is not open")
	}
	if d.Draft {
		return m.aiInfo(prRef(d.Number) + " is a draft: mark it ready for review first")
	}
	style := confirm.StyleDefault
	if prMergeWarnings(*d) != nil {
		style = confirm.StyleDestructive
	}
	m.confirm = confirm.Risky("Merge "+prRef(d.Number), prMergeText(*d),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "squash", Title: "Squash and merge", Style: style},
		confirm.Button{ID: "merge", Title: "Merge commit", Style: style},
		confirm.Button{ID: "rebase", Title: "Rebase and merge", Style: style},
	).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindPRMerge
	return nil
}

// answerPRMerge runs the chosen method; Cancel and Esc do nothing.
func (m *Model) answerPRMerge(id string) tea.Cmd {
	method, ok := prMergeMethods[id]
	d := m.prS().detail
	if !ok || d == nil || d.State != "OPEN" || d.Draft {
		return nil
	}
	n := d.Number
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		err := ghci.Merge(ctx, run, dir, n, method)
		return prDoneMsg{number: n, ok: "Merged " + prRef(n), detail: string(method), failed: "Could not merge " + prRef(n), err: err, reopen: true}
	})
}

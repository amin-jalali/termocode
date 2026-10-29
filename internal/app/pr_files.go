package app

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/ghci"
)

// Files…: the diff of a pull request in the diff card (the same card as
// Compare Schemas), read-only. gh gives only the changed hunks, so each
// file shows those hunks with "…" between them.

// prDiffMsg is a finished `gh pr diff`.
type prDiffMsg struct {
	number int
	files  []ghci.FileDiff
	err    error
}

// prDiffCmd runs `gh pr diff` off the main loop.
func (m *Model) prDiffCmd(n int) tea.Cmd {
	return m.prRun(func(ctx context.Context, run ghci.Runner, dir string) tea.Msg {
		files, err := ghci.PRDiff(ctx, run, dir, n)
		return prDiffMsg{number: n, files: files, err: err}
	})
}

// prOpenFiles reads the diff of the pull request in the view.
func (m *Model) prOpenFiles() tea.Cmd {
	d := m.prS().detail
	if d == nil {
		return nil
	}
	fetch := m.prDiffCmd(d.Number)
	if fetch == nil {
		return nil
	}
	return tea.Batch(m.aiInfo("Reading the changes of "+prRef(d.Number)+"…"), fetch)
}

// onPRDiff opens the diff card.
func (m *Model) onPRDiff(msg prDiffMsg) tea.Cmd {
	if msg.err != nil {
		return m.prFailToast("Could not read the changes of "+prRef(msg.number), msg.err)
	}
	s := m.prS()
	if s.detail == nil || s.detail.Number != msg.number {
		return nil
	}
	if ai := m.aiS(); ai.review != nil && m.aiReviewPending() > 0 {
		return m.aiInfo("Finish the AI review first (AI: Review Pending Changes)")
	}
	s.diff = msg.files
	d := s.detail
	m.confirmOpen = false
	files, add, del := prDiffCardFiles(msg.files)
	card := diffview.NewCommit(diffview.Header{
		Title: prRef(d.Number) + " " + d.Title, Subject: prStateText(*d) + " · " + d.Branch + " → " + d.Base,
		Author: d.Author, Files: len(files), Add: add, Del: del,
		OldLabel: "− " + d.Base, NewLabel: "+ " + d.Branch,
	}, files, []diffview.Button{{ID: "copy-link", Label: "Copy link", Kind: "primary"}})
	card.SetCustom(map[string]string{"y": "copy-link"},
		[]string{"y", "copy link", "n/p", "change", "tab", "file", "t", "split/unified", "esc", "close"})
	m.aiS().review = &aiReview{card: card, onAction: func(m *Model, id string) tea.Cmd { return m.prDiffAction(id) }}
	return m.showAIReview()
}

// prDiffCardFiles maps the parsed diff to card files and sums the lines.
func prDiffCardFiles(fs []ghci.FileDiff) (files []diffview.File, add, del int) {
	for _, f := range fs {
		df := diffview.File{Name: f.Path, Old: f.Old, New: f.New, Add: f.Add, Del: f.Del, Binary: f.Binary, Status: 'M'}
		switch {
		case f.Created:
			df.Status, df.Tag = 'A', "new file"
		case f.Deleted:
			df.Status, df.Tag = 'D', "deleted"
		case f.OldPath != f.Path:
			df.Status, df.Tag = 'R', "from "+f.OldPath
		}
		add += f.Add
		del += f.Del
		files = append(files, df)
	}
	return files, add, del
}

// prDiffAction runs the card's buttons.
func (m *Model) prDiffAction(id string) tea.Cmd {
	d := m.prS().detail
	if d == nil {
		return nil
	}
	switch id {
	case "copy-link":
		return m.copyWithToast(d.URL, fmt.Sprintf("Copied the link of %s", prRef(d.Number)))
	}
	return nil
}

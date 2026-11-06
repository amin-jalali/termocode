package app

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Diff options (GITX-26): w in a diff card hides whitespace-only changes
// (internal/diffview), a merge commit diffs against the parent you pick,
// and the active file compares with the clipboard. Compare Two Files was
// there already.

// gitxClipboardRead reads the clipboard (tests swap it: they never touch
// the real one).
var gitxClipboardRead = readClipboardText

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-diff-merge-parent", Title: "Git: Diff Merge Commit Against Parent…"},
			{ID: "git-diff-clipboard", Title: "Git: Compare Active File with Clipboard"},
		},
		policies: map[string]aiActionPolicy{
			"git-diff-merge-parent": aiNever(aiNoPicker),
			"git-diff-clipboard":    aiNever("reads the user's clipboard; only the user does that"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-diff-merge-parent":
				return m.openMergeParentPicker(), true
			case "git-diff-clipboard":
				return m.compareWithClipboard(), true
			}
			return nil, false
		},
	})
}

// openMergeParentPicker lists the parents of the merge commit on show in
// the commit view (or HEAD); a pick opens the compare parent → merge.
func (m *Model) openMergeParentPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	var full, short string
	var parents []string
	if cv := m.commitView; cv != nil && cv.open && cv.compare == nil {
		full, short, parents = cv.info.FullHash, cv.info.Hash, cv.info.Parents
	} else if c := m.gitxHeadCommit(); c != nil {
		full, short, parents = c.FullHash, c.Hash, c.Parents
	}
	if full == "" {
		return m.gitxInfo("Open a merge commit in the commit view first")
	}
	if len(parents) < 2 {
		return m.gitxInfo(short + " is not a merge commit (one parent)")
	}
	var items []picker.Item
	for i, p := range parents {
		hint := "the branch it merged into"
		if i > 0 {
			hint = "a branch it merged"
		}
		items = append(items, picker.Item{ID: fmt.Sprintf("parent-%d", i+1), Title: fmt.Sprintf("Parent %d: %s", i+1, p), Hint: hint})
	}
	m.gitxPick(" Diff "+short+" against which parent? ", items, func(m *Model, id string) tea.Cmd {
		for i, p := range parents {
			if id == fmt.Sprintf("parent-%d", i+1) {
				return m.gitOpenCompareCmd(commitRange{a: p, b: full, sa: p, sb: short})
			}
		}
		return nil
	})
	return nil
}

// gitxHeadCommit is the GRAPH row of HEAD (nil: not loaded).
func (m *Model) gitxHeadCommit() *git.GraphCommit {
	for i := range m.gitCommits {
		if m.gitCommits[i].IsHead {
			return &m.gitCommits[i]
		}
	}
	return nil
}

// compareWithClipboard diffs the clipboard text (old) against the active
// file on disk (new) in a read-only card.
func (m *Model) compareWithClipboard() tea.Cmd {
	path := m.editor.Path()
	if path == "" {
		return m.gitxInfo("Open a file first")
	}
	clip := gitxClipboardRead()
	if clip == "" {
		return m.gitxInfo("The clipboard is empty")
	}
	name := filepath.Base(path)
	return func() tea.Msg {
		fi, err := os.Stat(path)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("compare: %w", err)}
		}
		if fi.Size() > maxCompareBytes || len(clip) > maxCompareBytes {
			return ErrMsg{Err: fmt.Errorf("compare: too big (over %d MB)", maxCompareBytes>>20)}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("compare: %w", err)}
		}
		return compareReadyMsg{title: "Compare with Clipboard", file: diffview.File{
			Name: "Clipboard ↔ " + name, Old: clip, New: string(b)}}
	}
}

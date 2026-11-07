package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── WORKTREES view: diff vs base (CMP-55) ────────────────────────────────
//
// d on a card shows everything the worktree changed since it left its
// base (the parent worktree's branch, else main): commits, uncommitted
// edits and new files, whoever made them (the built-in AI, an outside
// agent in a terminal tab, or the user). The files open in the review
// card the AI edits use: per hunk, Accept keeps the change and Reject
// writes the base text back for that hunk. Nothing is written until the
// user decides a file, and a file that changed after it was read (the
// agent is still at work) is never overwritten.

const wtDiffPaletteID = "worktree-diff"

func init() {
	// Opens the review card only; every revert is the user's choice there.
	aiActionPolicies[wtDiffPaletteID] = aiAuto()
}

// Caps of one diff vs base: the files listed and the size of each side.
const (
	wtDiffMaxFiles = 200
	wtDiffMaxBytes = 1 << 20
)

// wtDiffFile is one changed file of a worktree.
type wtDiffFile struct {
	rel, abs string
	old, new string
	atBase   bool // the file is in the base commit
	exists   bool // the file is on disk now
}

// wtDiffReadyMsg is the diff vs base, read in the background.
type wtDiffReadyMsg struct {
	path, title, base string
	files             []wtDiffFile
	skipped           int // binary, too big, a link, or past wtDiffMaxFiles
	err               error
}

// worktreeDiff reads the cursor's changes since its base (d,
// worktree-diff).
func (m *Model) worktreeDiff() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	if why := wtUsable(it); why != "" {
		return m.pushToastDetail(toast.Info, "No diff for this worktree", why)
	}
	if it.e.Meta.State == "archived" {
		return m.pushToastDetail(toast.Info, "No diff for this worktree", "It is archived: its folder is gone. Restore it first (x).")
	}
	return wtDiffLoad(it.e.Path, m.wtDiffBase(it), wtTitle(it))
}

// wtDiffBase is the branch a worktree is compared with: its parent's
// branch (a child worktree, WTX-11), else "" (the repository's main
// branch, found when the diff is read).
func (m *Model) wtDiffBase(it wtItem) string {
	if p := it.e.Meta.Parent; p != "" {
		if par, ok := m.wtItemByPath(p); ok && par.e.Branch != "" && !par.e.Gone {
			return par.e.Branch
		}
	}
	return ""
}

// wtDiffLoad reads the diff of the worktree at path vs base ("" = main).
func wtDiffLoad(path, base, title string) tea.Cmd {
	return func() tea.Msg {
		msg := wtDiffReadyMsg{path: path, title: title, base: base}
		if msg.base == "" {
			b, err := git.DefaultBase(path)
			if err != nil {
				msg.err = err
				return msg
			}
			msg.base = b
		}
		mb, files, err := git.BaseDiffFiles(path, msg.base)
		if err != nil {
			msg.err = err
			return msg
		}
		for _, rel := range files {
			if len(msg.files) >= wtDiffMaxFiles {
				msg.skipped++
				continue
			}
			f, ok, err := wtDiffReadFile(path, mb, rel)
			switch {
			case err != nil:
				msg.skipped++
			case ok:
				msg.files = append(msg.files, f)
			}
		}
		return msg
	}
}

// wtDiffReadFile reads both sides of one file. ok=false: nothing to show
// (the two sides are the same); an error: the file is skipped (binary,
// too big, not a plain file).
func wtDiffReadFile(dir, mb, rel string) (wtDiffFile, bool, error) {
	f := wtDiffFile{rel: rel, abs: filepath.Join(dir, filepath.FromSlash(rel))}
	old, found, cut, err := git.FileAt(dir, mb, rel, wtDiffMaxBytes)
	if err != nil {
		return f, false, err
	}
	if cut {
		return f, false, errors.New("too big")
	}
	f.old, f.atBase = old, found
	fi, err := os.Lstat(f.abs)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return f, false, err
	case !fi.Mode().IsRegular():
		return f, false, errors.New("not a plain file") // a link or a folder: never written through
	case fi.Size() > wtDiffMaxBytes:
		return f, false, errors.New("too big")
	default:
		b, err := os.ReadFile(f.abs)
		if err != nil {
			return f, false, err
		}
		f.new, f.exists = string(b), true
	}
	if bytes.IndexByte([]byte(f.old), 0) >= 0 || bytes.IndexByte([]byte(f.new), 0) >= 0 {
		return f, false, errors.New("binary")
	}
	if f.atBase == f.exists && f.old == f.new {
		return f, false, nil
	}
	return f, true, nil
}

// handleWTDiffMsg opens the review card when the diff is read.
func (m *Model) handleWTDiffMsg(msg tea.Msg) (tea.Cmd, bool) {
	d, ok := msg.(wtDiffReadyMsg)
	if !ok {
		return nil, false
	}
	return m.onWTDiffReady(d), true
}

func (m *Model) onWTDiffReady(msg wtDiffReadyMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the diff of "+msg.title, msg.err.Error())
	}
	skipped := ""
	if msg.skipped > 0 {
		skipped = fmt.Sprintf(" (%d left out: binary, too big or a link)", msg.skipped)
	}
	if len(msg.files) == 0 {
		return m.pushToastDetail(toast.Info, "No changes in "+msg.title+" since "+msg.base, skipped)
	}
	s := m.aiS()
	if s.review != nil && m.aiReviewPending() > 0 {
		return m.pushToastDetail(toast.Info, "Finish the open review first", "AI: Review Pending Changes shows it")
	}
	title := "Changes vs " + msg.base + " · " + msg.title
	s.review = &aiReview{card: diffview.New(title, nil, true)}
	for _, f := range msg.files {
		m.addAIReview(wtDiffItem(f))
	}
	s.review.card.Select(0)
	if skipped != "" {
		return m.pushToastDetail(toast.Info, fmt.Sprintf("%d files changed since %s", len(msg.files), msg.base), skipped)
	}
	return nil
}

// wtDiffItem is one file of the diff vs base in the review card. Accept
// keeps the worktree's change; a rejected hunk is written back to its
// base text.
func wtDiffItem(f wtDiffFile) *aiReviewItem {
	tag := ""
	switch {
	case !f.atBase:
		tag = "new file"
	case !f.exists:
		tag = "deleted"
	}
	it := &aiReviewItem{file: diffview.File{Name: f.rel, Old: f.old, New: f.new, Tag: tag}}
	it.finish = func(m *Model, d *textdiff.Diff, acc map[int]bool, edit bool) (string, tea.Cmd) {
		n := max(len(d.Hunks), 1)
		k := 0
		for i := 0; i < n; i++ {
			if acc[i] {
				k++
			}
		}
		if k == n {
			if edit && f.exists {
				m.openFileAtLine(f.abs, 1)
				m.focus = FocusEditor
			}
			return "Kept", nil
		}
		// The agent may still be at work: never write over a newer file.
		cur, err := os.ReadFile(f.abs)
		if (err == nil) != f.exists || (err == nil && string(cur) != f.new) {
			return "Not reverted: the file changed", m.pushToastDetail(toast.Info, f.rel+" changed while you reviewed it", "Nothing was written. Press d again to read it anew.")
		}
		if open, why := m.wtDiffBufferBusy(f.abs); open {
			return "Not reverted: unsaved in the editor", m.pushToastDetail(toast.Info, "Not reverted: "+f.rel, why)
		}
		if k == 0 && !f.atBase {
			if err := os.Remove(f.abs); err != nil {
				return "Not reverted: " + err.Error(), m.pushToastDetail(toast.Errr, "Could not remove "+f.rel, err.Error())
			}
			return "Reverted: removed the new file", nil
		}
		final := d.Apply(acc)
		if err := os.MkdirAll(filepath.Dir(f.abs), 0o755); err != nil {
			return "Not reverted: " + err.Error(), m.pushToastDetail(toast.Errr, "Could not write "+f.rel, err.Error())
		}
		if err := os.WriteFile(f.abs, []byte(final), 0o644); err != nil {
			return "Not reverted: " + err.Error(), m.pushToastDetail(toast.Errr, "Could not write "+f.rel, err.Error())
		}
		m.wtDiffReload(f.abs)
		if edit {
			line := 1
			for i, h := range d.Hunks {
				if !acc[i] {
					line = h.OldStart + 1
					break
				}
			}
			m.openFileAtLine(f.abs, line)
			m.focus = FocusEditor
		}
		if k == 0 {
			return wtDiffRevertedAll(n), nil
		}
		return fmt.Sprintf("Reverted %d of %d changes", n-k, n), nil
	}
	return it
}

func wtDiffRevertedAll(n int) string {
	if n == 1 {
		return "Reverted the change"
	}
	return fmt.Sprintf("Reverted all %d changes", n)
}

// wtDiffBufferBusy: the file is open in the editor with unsaved edits;
// a revert would lose them.
func (m *Model) wtDiffBufferBusy(abs string) (bool, string) {
	bufs, err := m.aiBuffersUnder(abs)
	if err != nil {
		return false, ""
	}
	for _, b := range bufs {
		if b.Path == abs && b.Loaded && b.Modified {
			return true, "It is open in the editor with unsaved changes. Save or discard them first."
		}
	}
	return false, ""
}

// wtDiffReload shows the reverted text in an open editor tab.
func (m *Model) wtDiffReload(abs string) {
	if m.nvim == nil {
		return
	}
	var r string
	_ = m.nvim.ExecLuaResult(aiReloadBufferLua, &r, abs) // best effort: the file on disk is right
}

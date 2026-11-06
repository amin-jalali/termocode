package app

import (
	"errors"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Delete (WTX-14) ──────────────────────────────────────────────────────
//
// D in WORKTREES (Worktree: Delete Selected…) removes a worktree's folder
// and deletes its branch (on an archived row: the branch only). It asks
// in up to three steps, each with Cancel first and focused, and Esc
// cancels at every step:
//
//  1. what goes: the folder, the branch, and whether the branch is
//     merged into the default branch ("not merged into main: 3 commits
//     would be lost");
//  2. a dirty folder: "Delete anyway (loses N changed files)" — a folder
//     with uncommitted changes is never removed without this card;
//  3. a branch that is not merged (and has no merged PR): its name must
//     be typed.
//
// The default branch, the main checkout, the worktree this window is in,
// agent lanes and locked worktrees are refused without a card.

const (
	confirmKindWTDelete      confirmKindEnum = 3606
	confirmKindWTDeleteDirty confirmKindEnum = 3607
	wtDeletePaletteID                        = "worktree-delete"
	wtDeleteTypeTitle                        = "Type the branch name to delete it"
)

// wtDeletePending is the delete in its steps.
type wtDeletePending struct {
	title, path, branch, base string
	folder                    bool // the folder is there (not archived)
	dirty                     int
	unmerged                  int // commits not on base; -1 unknown
	prMerged                  int // the linked PR's number when it is merged
	discard                   bool
	typing                    bool
	fail                      string
}

// needsName: the branch name must be typed.
func (p *wtDeletePending) needsName() bool { return p.unmerged != 0 && p.prMerged == 0 }

// wtDeletePrepMsg: the dirty and merge counts are read.
type wtDeletePrepMsg struct {
	p   *wtDeletePending
	err error
}

// wtDeletedMsg is the end of a delete.
type wtDeletedMsg struct {
	title, path, branch string
	err                 error
}

// Tests swap these.
var (
	wtDeleteBranchRun = git.DeleteBranch
	wtUnmergedRun     = git.BranchUnmerged
	wtDefaultBranch   = git.DefaultBranch
)

// worktreeDelete is D.
func (m *Model) worktreeDelete() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	archived := it.e.Meta.State == worktrees.StateArchived
	if it.e.Gone && !archived {
		return m.pushToastDetail(toast.Info, "Its folder is gone", "F forgets it.")
	}
	if t, d := wtRefuse(it); t != "" {
		return m.pushToastDetail(toast.Info, t, d)
	}
	if it.e.Branch == "" {
		return m.pushToastDetail(toast.Info, "It has no branch", "x archives it (removes its folder).")
	}
	p := &wtDeletePending{title: wtTitle(it), path: it.e.Path, branch: it.e.Branch, folder: !archived}
	if it.e.Meta.PR > 0 && it.e.Meta.Link.State == "merged" {
		p.prMerged = it.e.Meta.PR
	}
	root, off := m.wtRootDir(), m.wt.off
	return func() tea.Msg {
		if off {
			p.base = "main"
			return wtDeletePrepMsg{p: p}
		}
		p.base = wtDefaultBranch(root)
		if p.base == p.branch {
			return wtDeletePrepMsg{p: p, err: errors.New(p.branch + " is the default branch; it is never deleted here")}
		}
		if p.folder {
			n, err := wtDirtyRun(p.path)
			if err != nil {
				return wtDeletePrepMsg{p: p, err: err}
			}
			p.dirty = n
		}
		p.unmerged = -1
		if p.base != "" {
			if n, err := wtUnmergedRun(root, p.branch, p.base); err == nil {
				p.unmerged = n
			}
		}
		return wtDeletePrepMsg{p: p}
	}
}

// onWTDeletePrep opens step 1.
func (m *Model) onWTDeletePrep(msg wtDeletePrepMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Warn, "The worktree cannot be deleted", msg.err.Error())
	}
	if m.confirmOpen || m.connectOpen {
		return nil
	}
	p := msg.p
	m.wt.del = p
	var b strings.Builder
	if p.folder {
		b.WriteString("Its folder is removed:\n" + wtShortPath(p.path) + "\n\n")
	}
	b.WriteString("The branch " + p.branch + " is deleted.")
	switch {
	case p.unmerged == 0:
		b.WriteString(" It is merged into " + p.base + ".")
	case p.prMerged > 0:
		b.WriteString(" Its PR #" + strconv.Itoa(p.prMerged) + " is merged.")
	case p.unmerged < 0:
		b.WriteString("\n\ntermocode cannot tell whether it is merged (no main or master branch): its commits may be lost.")
	default:
		b.WriteString("\n\nIt is not merged into " + p.base + ": " + wtPlural(p.unmerged, "commit") + " would be lost.")
	}
	if p.dirty > 0 {
		b.WriteString("\n\nIt has " + wtPlural(p.dirty, "changed file") + "; the next card asks about " + map[bool]string{true: "it", false: "them"}[p.dirty == 1] + ".")
	}
	if p.needsName() {
		b.WriteString("\n\nYou type the branch name to delete it.")
	}
	m.confirm = confirm.Risky("Delete "+p.title+" and its branch?", b.String(), confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "delete", Title: "Delete…", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTDelete
	return nil
}

// answerWTDelete answers steps 1 and 2 ("" = Esc = Cancel).
func (m *Model) answerWTDelete(kind confirmKindEnum, id string) tea.Cmd {
	p := m.wt.del
	if p == nil {
		return nil
	}
	switch {
	case kind == confirmKindWTDelete && id == "delete":
		if p.dirty > 0 {
			m.confirm = confirm.Risky(p.title+" has "+wtPlural(p.dirty, "changed file"),
				wtShortPath(p.path)+"\n\n"+map[bool]string{true: "It is", false: "They are"}[p.dirty == 1]+
					" not committed. Deleting the worktree loses "+map[bool]string{true: "it", false: "all " + strconv.Itoa(p.dirty) + " of them"}[p.dirty == 1]+".",
				confirm.Button{ID: "cancel", Title: "Cancel"},
				confirm.Button{ID: "discard", Title: "Delete anyway (loses " + wtPlural(p.dirty, "changed file") + ")", Style: confirm.StyleDestructive})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindWTDeleteDirty
			return nil
		}
		return m.wtDeleteNext()
	case kind == confirmKindWTDeleteDirty && id == "discard":
		p.discard = true
		return m.wtDeleteNext()
	}
	m.wt.del = nil
	return nil
}

// wtDeleteNext asks for the name when the branch is not merged, else
// deletes.
func (m *Model) wtDeleteNext() tea.Cmd {
	p := m.wt.del
	if !p.needsName() {
		m.wt.del = nil
		return m.wtDeleteRun(p)
	}
	p.typing = true
	m.connect = connectcard.New(wtDeleteTypeCard(p))
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return nil
}

func wtDeleteTypeCard(p *wtDeletePending) connectcard.Card {
	lost := "Its commits may not be on any other branch; they are lost with it."
	if p.unmerged > 0 {
		lost = wtPlural(p.unmerged, "commit") + " on " + p.branch + " " + map[bool]string{true: "is", false: "are"}[p.unmerged == 1] +
			" not on " + p.base + "; they are lost with the branch."
	}
	rows := []connectcard.Row{
		{Kind: connectcard.Text, Text: lost},
		{Kind: connectcard.Blank},
		{Kind: connectcard.Input, ID: "name", Text: "Branch", Inline: true, Placeholder: p.branch},
	}
	if p.fail != "" {
		rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: p.fail})
	}
	return connectcard.Card{Title: wtDeleteTypeTitle, Rows: rows, Form: true,
		Buttons: []connectcard.Button{{ID: "cancel", Title: "Cancel"}, {ID: "delete", Title: "Delete"}}}
}

func (m Model) wtDeleteTypeOpen() bool {
	return m.wt != nil && m.wt.del != nil && m.wt.del.typing && m.connectOpen && m.connect.Card().Title == wtDeleteTypeTitle
}

// wtDeleteTyped checks the typed name: the exact branch deletes.
func (m *Model) wtDeleteTyped() tea.Cmd {
	p := m.wt.del
	value := m.connect.InputValue("name")
	if value != p.branch {
		p.fail = "Type " + p.branch + " exactly to delete it."
		m.connect.SetCard(wtDeleteTypeCard(p))
		m.connect.SetInputValue("name", value)
		return nil
	}
	m.closeWTDeleteType()
	return m.wtDeleteRun(p)
}

func (m *Model) closeWTDeleteType() {
	if m.wtDeleteTypeOpen() {
		m.connectOpen = false
	}
	m.wt.del = nil
}

// handleWTDeleteMsg runs the type-the-name card. ok=false: not ours.
func (m *Model) handleWTDeleteMsg(msg tea.Msg) (tea.Cmd, bool) {
	if !m.wtDeleteTypeOpen() {
		return nil, false
	}
	switch msg := msg.(type) {
	case connectcard.ButtonMsg:
		if msg.ID == "delete" {
			return m.wtDeleteTyped(), true
		}
		m.closeWTDeleteType()
		return nil, true
	case connectcard.SubmitMsg:
		return m.wtDeleteTyped(), true
	case connectcard.CloseMsg:
		m.closeWTDeleteType()
		return nil, true
	case connectcard.KeyMsg:
		return nil, true
	}
	return nil, false
}

// wtDeleteRun removes the folder (when there) and the branch, then drops
// the metadata.
func (m *Model) wtDeleteRun(p *wtDeletePending) tea.Cmd {
	root, common, off := m.wtRootDir(), m.wt.common, m.wt.off
	return func() tea.Msg {
		msg := wtDeletedMsg{title: p.title, path: p.path, branch: p.branch}
		if off {
			return msg
		}
		if p.folder {
			if err := wtRemoveRun(root, p.path, p.discard); err != nil {
				msg.err = err
				return msg
			}
		}
		if err := wtDeleteBranchRun(root, p.branch, p.unmerged != 0); err != nil {
			msg.err = err
			return msg
		}
		if common != "" {
			msg.err = worktrees.Update(common, func(mm map[string]worktrees.Meta) { delete(mm, p.path) })
		}
		return msg
	}
}

func (m *Model) onWTDeleted(msg wtDeletedMsg) tea.Cmd {
	if msg.err != nil {
		detail := msg.err.Error()
		if errors.Is(msg.err, git.ErrWorktreeDirty) {
			detail = "It has changed files now; D asks again."
		}
		return tea.Batch(m.pushToastDetail(toast.Warn, "The worktree could not be deleted", detail), m.worktreesReload())
	}
	return tea.Batch(m.pushToastDetail(toast.Info, "Deleted "+msg.title, "Its folder and the branch "+msg.branch+" are gone."), m.worktreesReload())
}

func init() {
	aiActionPolicies[wtDeletePaletteID] = aiDestructive()
}

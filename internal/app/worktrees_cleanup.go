package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Bulk cleanup (WTX-15) ────────────────────────────────────────────────
//
// C in WORKTREES (Worktree: Clean Up…) lists the worktrees that can go
// with a box each, pre-ticked by worktrees.CleanupReason (merged, PR
// merged, idle and clean — never one with changed files), each row
// saying why; plus one row for git's records of folders that are gone
// (`git worktree prune`). Space (or Enter on a row) ticks; Enter on the
// first row opens one card that lists every worktree that will be
// archived (Cancel first). A ticked dirty worktree then asks on its own
// card (Keep it first: Esc keeps it). Archive is WTX-14's: the folders
// go, the branches stay, x restores. The main checkout, the one this
// window is in, agent lanes and locked worktrees are never listed.

const (
	pickerKindWTCleanup       pickerKindEnum  = 3602
	confirmKindWTCleanup      confirmKindEnum = 3608
	confirmKindWTCleanupDirty confirmKindEnum = 3609
	wtCleanupPaletteID                        = "worktree-cleanup"
	wtCleanupGoID                             = "go"
	wtCleanupPruneID                          = "prune"
)

// wtCleanRow is one worktree of the list.
type wtCleanRow struct {
	path, title, branch, why string
	dirty                    int
	tick                     bool
}

// wtCleanup is the open cleanup.
type wtCleanup struct {
	rows  []wtCleanRow
	gone  int  // git records of gone folders
	prune bool // prune them
	queue []wtArchiveTarget
	dirty []wtCleanRow // ticked dirty ones still to ask about
}

// wtCleanupPrepMsg: the facts are read.
type wtCleanupPrepMsg struct {
	c   *wtCleanup
	err error
}

// wtCleanedMsg is the end of a cleanup.
type wtCleanedMsg struct {
	archived int
	pruned   bool
	err      error
}

// wtPruneRun drops git's records of gone folders (a var: tests).
var wtPruneRun = git.PruneWorktrees

// worktreeCleanup is C.
func (m *Model) worktreeCleanup() tea.Cmd {
	st := m.wt
	if st == nil || !st.loaded {
		return m.revealWorktrees()
	}
	items, _ := m.wtList()
	var cands []wtItem
	gone := 0
	for _, it := range items {
		switch {
		case it.state == wtStateArchived:
		case it.e.Gone:
			if it.e.Prunable {
				gone++
			}
		default:
			if t, _ := wtRefuse(it); t == "" {
				cands = append(cands, it)
			}
		}
	}
	if len(cands) == 0 && gone == 0 {
		return m.pushToastDetail(toast.Info, "Nothing to clean up", "Only the main checkout and the worktree you are in are left.")
	}
	root, off := m.wtRootDir(), st.off
	now := clock.Now()
	return func() tea.Msg {
		c := &wtCleanup{gone: gone, prune: gone > 0}
		base := "main"
		if !off {
			base = wtDefaultBranch(root)
		}
		for _, it := range cands {
			f := worktrees.CleanupFacts{Base: base, PR: it.e.Meta.PR, PRMerged: it.e.Meta.PR > 0 && it.e.Meta.Link.State == "merged"}
			last := it.e.Meta.LastSeen
			if it.e.Meta.Created.After(last) {
				last = it.e.Meta.Created
			}
			if !off {
				n, err := wtDirtyRun(it.e.Path)
				if err != nil {
					n = -1
				}
				f.Dirty = n
				if base != "" && it.e.Branch != "" && it.e.Branch != base {
					if u, err := wtUnmergedRun(root, it.e.Branch, base); err == nil {
						f.Merged = u == 0
					}
				}
				if ht := git.HeadCommitTime(it.e.Path); ht.After(last) {
					last = ht
				}
			}
			f.LastActive = last
			why, tick := worktrees.CleanupReason(f, now)
			if f.Dirty < 0 {
				why, tick = "kept: its status could not be read", false
			}
			c.rows = append(c.rows, wtCleanRow{path: it.e.Path, title: wtTitle(it), branch: it.e.Branch, why: why, dirty: max(f.Dirty, 0), tick: tick})
		}
		return wtCleanupPrepMsg{c: c}
	}
}

func (c *wtCleanup) ticked() (n int) {
	for _, r := range c.rows {
		if r.tick {
			n++
		}
	}
	return n
}

func (c *wtCleanup) items() []picker.Item {
	box := func(on bool) string {
		if on {
			return "[x] "
		}
		return "[ ] "
	}
	n := c.ticked()
	goTitle := "Archive the " + wtPlural(n, "ticked worktree") + "…"
	if n == 0 {
		goTitle = "Go on…"
	}
	items := []picker.Item{{ID: wtCleanupGoID, Title: goTitle, Hint: "a card lists them first"}}
	for _, r := range c.rows {
		items = append(items, picker.Item{ID: "wt:" + r.path, Title: box(r.tick) + r.title, Hint: r.why})
	}
	if c.gone > 0 {
		items = append(items, picker.Item{ID: wtCleanupPruneID, Title: box(c.prune) + "Forget " + wtPlural(c.gone, "gone folder"), Hint: "git worktree prune: no file is touched"})
	}
	return items
}

func (m *Model) onWTCleanupPrep(msg wtCleanupPrepMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Warn, "The worktrees could not be read", msg.err.Error())
	}
	if m.pickerOpen || m.confirmOpen {
		return nil
	}
	m.wt.cleanup = msg.c
	m.picker = picker.NewItems(" Clean up worktrees ", msg.c.items())
	m.picker.SetWidth(min(m.w-4, 110))
	m.picker.SetRowAction("space", "tick")
	m.picker.SetNote([]string{"[x] is archived: its folder goes, its branch stays (x restores it). A worktree with changed files is never pre-ticked."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWTCleanup
	return nil
}

// onWTCleanupToggle ticks or unticks a row (Space).
func (m *Model) onWTCleanupToggle(id string) tea.Cmd {
	c := m.wt.cleanup
	if c == nil {
		return nil
	}
	if id == wtCleanupPruneID {
		c.prune = !c.prune
	}
	for i := range c.rows {
		if "wt:"+c.rows[i].path == id {
			c.rows[i].tick = !c.rows[i].tick
		}
	}
	m.picker.SetItems(c.items())
	m.picker.SetCursorID(id)
	return nil
}

// onWTCleanupPicked is Enter: a row ticks (the list stays), the first
// row opens the summary card.
func (m *Model) onWTCleanupPicked(id string) tea.Cmd {
	c := m.wt.cleanup
	if c == nil {
		return nil
	}
	if id != wtCleanupGoID {
		m.onWTCleanupToggle(id)
		m.pickerOpen = true
		return nil
	}
	n := c.ticked()
	if n == 0 && !c.prune {
		m.wt.cleanup = nil
		return m.pushToastDetail(toast.Info, "Nothing ticked", "Nothing was archived.")
	}
	var b strings.Builder
	if n > 0 {
		b.WriteString("Their folders are removed (git worktree remove); their branches stay, and x on a row restores one.\n")
		for _, r := range c.rows {
			if !r.tick {
				continue
			}
			line := "\n• " + r.title + " — " + r.why
			if r.dirty > 0 && !strings.Contains(r.why, "changed file") {
				line += " · " + wtPlural(r.dirty, "changed file")
			}
			b.WriteString(line)
		}
		var dirty int
		for _, r := range c.rows {
			if r.tick && r.dirty > 0 {
				dirty++
			}
		}
		if dirty > 0 {
			b.WriteString("\n\n" + wtPlural(dirty, "worktree") + " with changed files " + map[bool]string{true: "asks", false: "ask"}[dirty == 1] + " again on " +
				map[bool]string{true: "its", false: "their"}[dirty == 1] + " own card.")
		}
	}
	if c.prune {
		b.WriteString("\n\ngit forgets " + wtPlural(c.gone, "gone folder") + " (git worktree prune).")
	}
	title := "Archive " + wtPlural(n, "worktree") + "?"
	btn := "Archive " + strconv.Itoa(n)
	if n == 0 {
		title, btn = "Forget the gone folders?", "Forget them"
	}
	m.confirm = confirm.Risky(title, strings.TrimSpace(b.String()), confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "archive", Title: btn, Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTCleanup
	return nil
}

// answerWTCleanup answers the summary card and the dirty cards ("" =
// Esc: Cancel on the summary, Keep it on a dirty card).
func (m *Model) answerWTCleanup(kind confirmKindEnum, id string) tea.Cmd {
	c := m.wt.cleanup
	if c == nil {
		return nil
	}
	switch kind {
	case confirmKindWTCleanup:
		if id != "archive" {
			m.wt.cleanup = nil
			return nil
		}
		c.queue, c.dirty = nil, nil
		for _, r := range c.rows {
			switch {
			case !r.tick:
			case r.dirty > 0:
				c.dirty = append(c.dirty, r)
			default:
				c.queue = append(c.queue, wtArchiveTarget{path: r.path, branch: r.branch})
			}
		}
	case confirmKindWTCleanupDirty:
		r := c.dirty[0]
		c.dirty = c.dirty[1:]
		if id == "discard" {
			c.queue = append(c.queue, wtArchiveTarget{path: r.path, branch: r.branch, dirty: r.dirty})
		}
	}
	if len(c.dirty) > 0 {
		r := c.dirty[0]
		m.confirm = confirm.Risky(r.title+" has "+wtPlural(r.dirty, "changed file"),
			wtShortPath(r.path)+"\n\n"+map[bool]string{true: "It is", false: "They are"}[r.dirty == 1]+" not committed. Archive loses "+
				map[bool]string{true: "it", false: "them"}[r.dirty == 1]+"; Keep it leaves this worktree as it is.",
			confirm.Button{ID: "keep", Title: "Keep it"},
			confirm.Button{ID: "discard", Title: "Archive anyway (loses " + wtPlural(r.dirty, "changed file") + ")", Style: confirm.StyleDestructive})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindWTCleanupDirty
		return nil
	}
	m.wt.cleanup = nil
	return m.wtCleanupRun(c.queue, c.prune)
}

// wtCleanupRun archives the queue and prunes.
func (m *Model) wtCleanupRun(queue []wtArchiveTarget, prune bool) tea.Cmd {
	archive := m.wtArchiveRun("", queue)
	root, off := m.wtRootDir(), m.wt.off
	return func() tea.Msg {
		a := archive().(wtArchivedMsg)
		msg := wtCleanedMsg{archived: len(a.paths), err: a.err}
		if prune && msg.err == nil && !off {
			if err := wtPruneRun(root); err != nil {
				msg.err = err
			} else {
				msg.pruned = true
			}
		}
		return msg
	}
}

func (m *Model) onWTCleaned(msg wtCleanedMsg) tea.Cmd {
	var parts []string
	if msg.archived > 0 {
		parts = append(parts, wtPlural(msg.archived, "worktree")+" archived (branches kept)")
	}
	if msg.pruned {
		parts = append(parts, "gone folders forgotten")
	}
	if msg.err != nil {
		return tea.Batch(m.pushToastDetail(toast.Warn, "The cleanup stopped", strings.Join(append(parts, msg.err.Error()), "; ")), m.worktreesReload())
	}
	if len(parts) == 0 {
		parts = append(parts, "Nothing changed")
	}
	return tea.Batch(m.pushToastDetail(toast.Info, "Cleaned up", strings.Join(parts, "; ")+"."), m.worktreesReload())
}

func init() {
	aiActionPolicies[wtCleanupPaletteID] = aiDestructive()
}

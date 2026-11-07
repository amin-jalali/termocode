package app

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktreeform"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Archive and restore (WTX-14) ─────────────────────────────────────────
//
// x in WORKTREES (Worktree: Archive or Restore Selected) archives a
// worktree: `git worktree remove` takes its folder, the branch stays and
// the row stays under ARCHIVED (the branch is kept in worktrees.json).
// x on an archived row restores the folder from the branch (`git
// worktree add`). A card always asks first, Cancel first and focused;
// Esc cancels. A dirty folder's card says how many changed files are
// lost, and its button says it again: "Archive anyway (loses N changed
// files)". A parent's card offers "Archive with its N children" when
// they are all clean. The main checkout, the worktree this window is in,
// an agent lane (its AI run discards it) and a locked worktree (never
// --force) are refused without a card.

const (
	confirmKindWTArchive confirmKindEnum = 3605
	wtArchivePaletteID                   = "worktree-archive"
	wtStateArchived                      = "archived"
)

// wtArchiveTarget is one worktree an archive removes.
type wtArchiveTarget struct {
	path, branch string
	dirty        int
}

// wtArchivePending is the open archive card.
type wtArchivePending struct {
	title    string
	main     wtArchiveTarget
	children []wtArchiveTarget
	script   wtArchiveScript // the repository's archive script (CMP-15)
}

// wtArchivePrepMsg: the dirty counts are read; the card can open.
type wtArchivePrepMsg struct {
	title    string
	external bool
	main     wtArchiveTarget
	children []wtArchiveTarget
	err      error
}

// wtArchivedMsg is the end of an archive or a restore.
type wtArchivedMsg struct {
	restore      bool
	title        string
	paths        []string
	scriptFailed []string // the archive script lines that failed (CMP-15)
	err          error
}

// Tests swap these: the git calls of archive and restore.
var (
	wtDirtyRun   = git.WorktreeDirty
	wtRemoveRun  = git.RemoveWorktree
	wtRestoreRun = git.RestoreWorktree
)

// wtRefuse says why the cursor's worktree cannot be archived or deleted
// ("" = it can).
func wtRefuse(it wtItem) (title, detail string) {
	switch {
	case it.e.Kind == worktrees.KindMain:
		return "The main checkout stays", "Archive and Delete are for the other worktrees."
	case it.here:
		return "You are in this worktree", "Open another worktree first (Enter on its card), then archive this one."
	case it.e.Kind == worktrees.KindAgent:
		return "This is an agent lane", "Its AI run keeps or discards it (Worktree changes in the chat)."
	case it.e.Locked:
		why := "git worktree unlock it first; termocode never forces a locked worktree."
		if it.e.LockReason != "" {
			why = "Locked: " + it.e.LockReason + ". " + why
		}
		return "This worktree is locked", why
	}
	return "", ""
}

// wtRootDir is the folder git runs in for the view.
func (m *Model) wtRootDir() string {
	if m.wt.root != "" {
		return m.wt.root
	}
	root, _ := m.rootDir()
	return root
}

// worktreeArchive is x: archive the cursor's worktree, or restore an
// archived one.
func (m *Model) worktreeArchive() tea.Cmd {
	it, ok := m.wtSelected()
	if !ok {
		return m.revealWorktrees()
	}
	return m.wtArchiveItem(it)
}

// wtArchiveItem archives (or restores) it.
func (m *Model) wtArchiveItem(it wtItem) tea.Cmd {
	if it.e.Meta.State == worktrees.StateArchived {
		return m.wtRestore(it)
	}
	if it.e.Gone {
		return m.pushToastDetail(toast.Info, "Its folder is gone", "F forgets it.")
	}
	if t, d := wtRefuse(it); t != "" {
		return m.pushToastDetail(toast.Info, t, d)
	}
	var kids []worktrees.Entry
	for _, k := range wtDescendants(m.wt.entries, it.e.Path) {
		if k.Kind != worktrees.KindAgent && !k.Locked && k.Path != wtOwner(m.wt.entries, m.wt.root) {
			kids = append(kids, k)
		}
	}
	msg := wtArchivePrepMsg{title: wtTitle(it), external: it.e.Kind == worktrees.KindExternal && !it.e.Meta.Adopted,
		main: wtArchiveTarget{path: it.e.Path, branch: it.e.Branch}}
	off := m.wt.off
	return func() tea.Msg {
		if off {
			return msg
		}
		n, err := wtDirtyRun(msg.main.path)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.main.dirty = n
		for _, k := range kids {
			d, err := wtDirtyRun(k.Path)
			if err != nil {
				d = -1 // unknown: it never goes along
			}
			msg.children = append(msg.children, wtArchiveTarget{path: k.Path, branch: k.Branch, dirty: d})
		}
		return msg
	}
}

// onWTArchivePrep opens the card.
func (m *Model) onWTArchivePrep(msg wtArchivePrepMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Warn, "The worktree could not be read", msg.err.Error())
	}
	if m.confirmOpen {
		return nil
	}
	p := &wtArchivePending{title: msg.title, main: msg.main, script: m.wtArchiveScriptLoad()}
	var b strings.Builder
	branch := msg.main.branch
	if branch == "" {
		branch = "its commit"
	}
	b.WriteString("Its folder is removed (git worktree remove):\n" + wtShortPath(msg.main.path) +
		"\n\nThe branch " + branch + " stays; x on its row brings the folder back.")
	if msg.external {
		b.WriteString("\n\nThis worktree was not made by termocode.")
	}
	if n := msg.main.dirty; n > 0 {
		b.WriteString("\n\nIt has " + wtPlural(n, "changed file") + " that " + map[bool]string{true: "is", false: "are"}[n == 1] +
			" not committed. Archive loses " + map[bool]string{true: "it", false: "them"}[n == 1] + ".")
	}
	var clean, dirty []string
	for _, c := range msg.children {
		if c.dirty == 0 {
			p.children = append(p.children, c)
			clean = append(clean, wtShortPath(c.path))
		} else {
			dirty = append(dirty, wtShortPath(c.path))
		}
	}
	if len(clean) > 0 {
		b.WriteString("\n\nIts " + wtChildWord(len(clean)) + ": " + strings.Join(clean, ", ") + ".")
	}
	if len(dirty) > 0 {
		b.WriteString("\n\nChanged files in " + strings.Join(dirty, ", ") + ": archive " + map[bool]string{true: "it", false: "them"}[len(dirty) == 1] + " on " +
			map[bool]string{true: "its", false: "their"}[len(dirty) == 1] + " own.")
	}
	if len(p.script.lines) > 0 {
		if p.script.trusted {
			b.WriteString("\n\nThe archive script of " + worktreeform.ConfigFile + " runs first in the folder:" + wtArchiveScriptLines(p.script))
		} else {
			b.WriteString("\n\nThe archive script of " + worktreeform.ConfigFile + " is not trusted:" + wtArchiveScriptLines(p.script) +
				"\n\nArchive skips it. Trust script and archive runs it first, with your environment minus termocode's secrets; a changed file asks again.")
		}
	}
	var buttons []confirm.Button
	title := "Archive " + msg.title + "?"
	switch {
	case msg.main.dirty > 0:
		buttons = append(buttons, confirm.Button{ID: "archive", Title: "Archive anyway (loses " + wtPlural(msg.main.dirty, "changed file") + ")", Style: confirm.StyleDestructive})
	case len(p.children) > 0:
		buttons = append(buttons, confirm.Button{ID: "archive", Title: "Archive only this one"},
			confirm.Button{ID: "all", Title: "Archive with its " + wtChildWord(len(p.children)), Style: confirm.StylePrimary})
	default:
		buttons = append(buttons, confirm.Button{ID: "archive", Title: "Archive", Style: confirm.StylePrimary})
	}
	if len(p.script.lines) > 0 && !p.script.trusted {
		tb := confirm.Button{ID: "trust", Title: "Trust script and archive"}
		if n := msg.main.dirty; n > 0 {
			tb = confirm.Button{ID: "trust", Title: "Trust script and archive anyway (loses " + wtPlural(n, "changed file") + ")", Style: confirm.StyleDestructive}
		}
		buttons = append(buttons, tb)
	}
	m.wt.archiveAsk = p
	m.confirm = confirm.Risky(title, b.String(), confirm.Button{ID: "cancel", Title: "Cancel"}, buttons...).WithWideText().WithScrollText() // the folder and script stay readable
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWTArchive
	return nil
}

// answerWTArchive answers the card ("" = Esc = Cancel).
func (m *Model) answerWTArchive(id string) tea.Cmd {
	p := m.wt.archiveAsk
	m.wt.archiveAsk = nil
	if p == nil || (id != "archive" && id != "all" && id != "trust") {
		return nil
	}
	if id == "trust" && p.script.hash != "" {
		sess := loadSession()
		sess.WorktreeSetupTrusted = p.script.hash
		saveSession(sess)
	}
	targets := []wtArchiveTarget{p.main}
	if id == "all" {
		targets = append(targets, p.children...)
	}
	return m.wtArchiveRun(p.title, targets)
}

// wtArchiveRun removes the folders and marks them archived.
func (m *Model) wtArchiveRun(title string, targets []wtArchiveTarget) tea.Cmd {
	root, common, off := m.wtRootDir(), m.wt.common, m.wt.off
	script := m.wtArchiveScriptLoad()
	return func() tea.Msg {
		msg := wtArchivedMsg{title: title}
		for _, t := range targets {
			if !off {
				if f := wtRunArchiveScript(script, common, t.path); f != "" {
					msg.scriptFailed = append(msg.scriptFailed, f)
				}
				if err := wtRemoveRun(root, t.path, t.dirty > 0); err != nil {
					msg.err = err
					return msg
				}
			}
			msg.paths = append(msg.paths, t.path)
			if off || common == "" {
				continue
			}
			now := clock.Now()
			if err := worktrees.Update(common, func(mm map[string]worktrees.Meta) {
				md := mm[t.path]
				md.State, md.Branch, md.LastSeen = worktrees.StateArchived, t.branch, now
				mm[t.path] = md
			}); err != nil {
				msg.err = err
				return msg
			}
		}
		return msg
	}
}

// wtRestore makes an archived worktree's folder again from its branch.
func (m *Model) wtRestore(it wtItem) tea.Cmd {
	branch := it.e.Meta.Branch
	if branch == "" {
		return m.pushToastDetail(toast.Warn, "Nothing to restore from", "termocode did not keep its branch; F forgets the row.")
	}
	root, common, off, path, title := m.wtRootDir(), m.wt.common, m.wt.off, it.e.Path, wtTitle(it)
	return func() tea.Msg {
		msg := wtArchivedMsg{restore: true, title: title, paths: []string{path}}
		if off {
			return msg
		}
		if err := wtRestoreRun(root, path, branch); err != nil {
			msg.err = err
			return msg
		}
		if common != "" {
			msg.err = worktrees.Update(common, func(mm map[string]worktrees.Meta) {
				md := mm[path]
				md.State, md.Branch, md.LastSeen = "", "", clock.Now()
				mm[path] = md
			})
		}
		return msg
	}
}

func (m *Model) onWTArchived(msg wtArchivedMsg) tea.Cmd {
	what := "archived"
	if msg.restore {
		what = "restored"
	}
	if msg.err != nil {
		detail := msg.err.Error()
		if errors.Is(msg.err, git.ErrWorktreeDirty) {
			detail = "It has changed files now; x asks again."
		}
		return tea.Batch(m.pushToastDetail(toast.Warn, "The worktree could not be "+what, detail), m.worktreesReload())
	}
	if len(msg.paths) > 0 {
		m.wt.cursorPath = msg.paths[0]
	}
	if d := wtArchiveScriptToast(msg.scriptFailed); d != "" {
		return tea.Batch(m.pushToastDetail(toast.Warn, "Archived "+msg.title+"; its archive script failed", d), m.worktreesReload())
	}
	detail := "x on its row brings the folder back from its branch."
	if msg.restore {
		detail = "Its folder is back on its branch."
	} else if len(msg.paths) > 1 {
		detail = "With its " + wtChildWord(len(msg.paths)-1) + ". " + detail
	}
	return tea.Batch(m.pushToastDetail(toast.Info, strings.ToUpper(what[:1])+what[1:]+" "+msg.title, detail), m.worktreesReload())
}

func init() {
	aiActionPolicies[wtArchivePaletteID] = aiDestructive()
}

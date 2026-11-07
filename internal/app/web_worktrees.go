package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// Worktrees from the page (WTX-16): the laptop's WORKTREES list (m.wt)
// with each card's group, agent state, branch status, note and link. The
// page names a worktree by an id the laptop makes from its path (a hash:
// the page never sends a path). Pin, note, sleep and wake run the
// laptop's own actions; a sleep that would end agent tabs or take
// children along opens the laptop's sleep card (Keep running first)
// and answers asked. Archive and delete wait for a laptop card; after
// yes the laptop's own flow runs on that worktree, with its own cards
// (changed files, the branch name). A listening browser makes the laptop
// read the list once; Refresh (and every action) reads it again.

func init() {
	webTopics["worktrees"] = webTopicDef{snapshot: webWorktreesSnapshot, apply: webApplyReplace[webproto.WorktreesSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishWorktrees)
	webOps[webproto.OpWorktreesRefresh] = (*Model).webWorktreesRefresh
	webOps[webproto.OpWorktreesPin] = (*Model).webWorktreesPin
	webOps[webproto.OpWorktreesSleep] = (*Model).webWorktreesSleep
	webOps[webproto.OpWorktreesNote] = (*Model).webWorktreesNote
	webAskOps[webproto.OpWorktreesArchive] = func(m *Model, call *webCall) *webAsk { return m.webWorktreesAsk(call, "archive") }
	webAskOps[webproto.OpWorktreesDelete] = func(m *Model, call *webCall) *webAsk { return m.webWorktreesAsk(call, "delete") }
}

// webWorktreeID is the page's name for the worktree at path.
func webWorktreeID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:webproto.WorktreeIDLen]
}

type webWTPub struct {
	key     string
	last    *webproto.WorktreesSnap
	loading bool // a load for the page is on its way
}

type webWorktreesDelta webproto.WorktreesSnap

func (webWorktreesDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webWorktreesDelta)
	return n, ok
}

// webWTState is the laptop's WORKTREES state, made on first use.
func (m *Model) webWTState() *wtState {
	if m.wt == nil {
		m.wt = newWTState()
	}
	return m.wt
}

func (m *Model) webWorktreesKey() string {
	st := m.webWTState()
	return strconv.FormatBool(st.loaded) + "|" + strconv.FormatUint(st.seq, 10) + "|" + st.err + "|" + m.wtTabSig()
}

func webWorktreesSnapshot(m *Model, _ string) (any, error) {
	snap := m.webWorktreesBuild()
	if m.web != nil {
		m.web.wt.key, m.web.wt.last = m.webWorktreesKey(), &snap
	}
	return snap, nil
}

// webPublishWorktrees sends the list when it changed; the first
// listening browser makes the laptop read it.
func (m *Model) webPublishWorktrees(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.wt
	if !h.webListens("worktrees") {
		p.last = nil
		return nil
	}
	var cmd tea.Cmd
	if st := m.webWTState(); !st.loaded && !st.off && !p.loading {
		p.loading = true
		cmd = m.worktreesLoadWeb()
	}
	key := m.webWorktreesKey()
	if p.last != nil && key == p.key {
		return cmd
	}
	p.key = key
	cur := m.webWorktreesBuild()
	if p.last != nil && reflect.DeepEqual(*p.last, cur) {
		return cmd
	}
	p.last = &cur
	h.publish("worktrees", webWorktreesDelta(cur))
	return cmd
}

// worktreesLoadWeb reads the list for the page (the view may be hidden).
func (m *Model) worktreesLoadWeb() tea.Cmd {
	load := m.worktreesLoadFor(false)
	return func() tea.Msg {
		msg := load().(wtLoadMsg)
		msg.web = true
		return msg
	}
}

// webWorktreesBuild is the worktrees topic now.
func (m *Model) webWorktreesBuild() webproto.WorktreesSnap {
	st := m.webWTState()
	s := webproto.WorktreesSnap{State: "loading", Worktrees: []webproto.WorktreeItem{}}
	switch {
	case !st.loaded:
		return s
	case st.err != "":
		s.State = "none"
		return s
	}
	s.State = "ready"
	items := wtItems(st.entries, m.wtTabs(), st.others, st.root)
	rows := wtRows(items, wtKindAll, "", st.dirty)
	home, _ := os.UserHomeDir()
	for _, r := range rows {
		if r.item < 0 {
			continue
		}
		if len(s.Worktrees) == webproto.MaxWorktrees {
			s.More++
			continue
		}
		it := items[r.item]
		w := webproto.WorktreeItem{ID: webWorktreeID(it.e.Path), Title: webName(wtTitle(it)), Branch: webName(it.e.Branch),
			Where: webName(homeShort(it.e.Path, home)), Kind: it.e.Kind.String(), Group: strings.ToLower(it.group), State: it.state,
			Pinned: it.e.Meta.Pinned, Here: it.here, Locked: it.e.Locked, Gone: it.e.Gone, Agent: webName(it.agent.name), Agents: it.agents,
			Checkpoint: it.e.Meta.Checkpoint, Link: webName(wtLinkLine(it.e.Meta)), Depth: r.depth}
		if st := st.status(it); st.known && st.Err == "" && !it.e.Gone {
			w.Status, w.Ahead, w.Behind, w.Dirty = true, st.Ahead, st.Behind, st.Dirty
		}
		if n, ok := it.e.Meta.LastNote(); ok {
			w.Note = webName(n.Text)
		}
		if p := it.e.Meta.Parent; p != "" && r.depth > 0 {
			w.Parent = webWorktreeID(p)
		}
		if !it.e.Meta.LastSeen.IsZero() {
			w.LastSeen = it.e.Meta.LastSeen.UnixMilli()
		}
		s.Worktrees = append(s.Worktrees, w)
	}
	return s
}

// webWorktreeFind is the card of an id, or a not_found answer.
func (m *Model) webWorktreeFind(call *webCall, id string) (wtItem, bool) {
	st := m.webWTState()
	if st.loaded {
		for _, e := range st.entries {
			if webWorktreeID(e.Path) == id {
				if it, ok := m.wtItemByPath(e.Path); ok {
					return it, true
				}
			}
		}
	}
	call.fail(webproto.CodeNotFound, "That worktree is not in the list.")
	return wtItem{}, false
}

// webWorktreesRefresh reads the list again.
func (m *Model) webWorktreesRefresh(call *webCall) tea.Cmd {
	call.reply(nil)
	return m.webWorktreesReload()
}

func (m *Model) webWorktreesReload() tea.Cmd {
	st := m.webWTState()
	if st.off {
		return nil
	}
	for _, e := range st.entries {
		st.cache.Invalidate(e.Path)
	}
	if st.visible {
		return m.worktreesReload()
	}
	return m.worktreesLoadWeb()
}

// webWorktreesPin pins or unpins a worktree.
func (m *Model) webWorktreesPin(call *webCall) tea.Cmd {
	a := call.args.(*webproto.WorktreeOnArgs)
	it, ok := m.webWorktreeFind(call, a.ID)
	if !ok {
		return nil
	}
	if err := m.wtTouchMeta(it.e.Path, func(md *worktrees.Meta) {
		if md.Pinned != a.On {
			md.Pinned, md.Order = a.On, 0 // it joins the other group in the automatic sort
		}
	}); err != nil {
		call.fail(webproto.CodeConflict, "The pin could not be saved: "+err.Error())
		return nil
	}
	call.reply(webproto.WorktreeDone{})
	return nil
}

// webWorktreesNote leaves a note (cleaned and redacted as an agent's).
func (m *Model) webWorktreesNote(call *webCall) tea.Cmd {
	a := call.args.(*webproto.WorktreeNoteArgs)
	it, ok := m.webWorktreeFind(call, a.ID)
	if !ok {
		return nil
	}
	st := m.webWTState()
	if st.off || st.common == "" {
		call.fail(webproto.CodeConflict, "Notes cannot be saved here.")
		return nil
	}
	if err := wtAddNote(st.common, it.e.Path, a.Text, ""); err != nil {
		call.fail(webproto.CodeBadRequest, webSentence(err.Error()))
		return nil
	}
	if all, err := worktrees.Load(st.common); err == nil {
		for i := range st.entries {
			if st.entries[i].Path == it.e.Path {
				st.entries[i].Meta = all[it.e.Path]
			}
		}
		st.touch()
	}
	m.webAuditOp(call, wtTitle(it), "ok")
	call.reply(webproto.WorktreeDone{})
	return m.webWorktreesReload()
}

// webWorktreesSleep sleeps (on) or wakes a worktree.
func (m *Model) webWorktreesSleep(call *webCall) tea.Cmd {
	a := call.args.(*webproto.WorktreeOnArgs)
	it, ok := m.webWorktreeFind(call, a.ID)
	if !ok {
		return nil
	}
	asleep := it.e.Meta.State == worktrees.StateAsleep
	switch {
	case a.On == asleep:
		call.reply(webproto.WorktreeDone{Note: map[bool]string{true: "It is asleep already.", false: "It is awake already."}[asleep]})
		return nil
	case m.confirmOpen:
		call.fail(webproto.CodeConflict, "The laptop is asking something else; try again.")
		return nil
	}
	cmd := m.wtSleepItem(it)
	if m.confirmOpen && m.confirmKind == confirmKindWTSleep {
		m.webAuditOp(call, wtTitle(it), "asked on the laptop")
		call.reply(webproto.WorktreeDone{Asked: true})
		return cmd
	}
	m.webAuditOp(call, wtTitle(it), "ok")
	call.reply(webproto.WorktreeDone{})
	return cmd
}

// webWorktreesAsk is a laptop card for archive or delete; after yes the
// laptop's own flow (and its cards) runs on that worktree.
func (m *Model) webWorktreesAsk(call *webCall, verb string) *webAsk {
	a := call.args.(*webproto.WorktreeArgs)
	it, ok := m.webWorktreeFind(call, a.ID)
	if !ok {
		return nil
	}
	if t, d := wtRefuse(it); t != "" {
		call.fail(webproto.CodeConflict, t+". "+d)
		return nil
	}
	if it.e.Gone && it.e.Meta.State != worktrees.StateArchived {
		call.fail(webproto.CodeConflict, "Its folder is gone.")
		return nil
	}
	return m.webWorktreesAskFor(it, verb)
}

// webWorktreesAskFor is the laptop card of a page's archive, restore or
// delete of it (webAskOpen adds who asked).
func (m *Model) webWorktreesAskFor(it wtItem, verb string) *webAsk {
	where := wtShortPath(it.e.Path)
	title, action, body := "Archive worktree?", "Go on", "The page asks to archive "+wtTitle(it)+":\n"+where+"\n\nThe next card says what goes."
	if verb == "delete" {
		title, body = "Delete worktree?", "The page asks to delete "+wtTitle(it)+" and its branch:\n"+where+"\n\nThe next cards say what is lost."
	}
	if it.e.Meta.State == worktrees.StateArchived && verb == "archive" {
		title, body = "Restore worktree?", "The page asks to restore "+wtTitle(it)+" from its branch:\n"+where
	}
	path := it.e.Path
	ask := &webAsk{summary: wtTitle(it), title: title, action: action, body: body}
	ask.run = func(m *Model) tea.Cmd {
		it, ok := m.wtItemByPath(path)
		if !ok {
			return m.pushToastDetail(toast.Info, "The worktree is not in the list any more", "Nothing was done.")
		}
		m.wt.cursorPath = path
		if verb == "delete" {
			return m.wtDeleteItem(it)
		}
		return m.wtArchiveItem(it)
	}
	return ask
}

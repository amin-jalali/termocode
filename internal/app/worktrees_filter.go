package app

// ── WORKTREES view: filters (WTX-4) ──────────────────────────────────────
//
// f cycles All / Mine / Agents / Dirty / Asleep / External; / filters by
// name, branch, folder or agent. Both are kept in the workspace's
// session, so the next start shows the same list.

func wtKindFrom(s string) wtKind {
	for k := wtKindAll; k < wtKindCount; k++ {
		if k.String() == s {
			return k
		}
	}
	return wtKindAll
}

// worktreesLoadSession reads the saved filters once.
func (m *Model) worktreesLoadSession() {
	st := m.wt
	if st == nil || st.sessLoaded || st.off {
		return
	}
	st.sessLoaded = true
	sess := loadSession()
	st.kind, st.filter = wtKindFrom(sess.WorktreesKind), sess.WorktreesFilter
	st.touch()
}

// worktreesSaveSession writes the filters.
func (m *Model) worktreesSaveSession() {
	st := m.wt
	if st.off {
		return
	}
	sess := loadSession()
	sess.WorktreesKind, sess.WorktreesFilter = "", st.filter
	if st.kind != wtKindAll {
		sess.WorktreesKind = st.kind.String()
	}
	saveSession(sess)
}

// worktreeNextFilter is f.
func (m *Model) worktreeNextFilter() {
	st := m.wt
	st.kind = (st.kind + 1) % wtKindCount
	m.wtKeepCursor()
	st.touch()
	m.worktreesSaveSession()
}

// worktreeSetFilter applies one step of the / filter; the text is saved
// when typing ends.
func (m *Model) worktreeSetFilter(text string, typing bool) {
	st := m.wt
	ended := st.filterTyping && !typing
	st.filter, st.filterTyping = text, typing
	m.wtKeepCursor()
	st.touch()
	if ended || (!typing && text == "") {
		m.worktreesSaveSession()
	}
}

// wtKeepCursor keeps the cursor on its worktree when it is still shown,
// else puts it on the first card.
func (m *Model) wtKeepCursor() {
	st := m.wt
	items, rows := m.wtList()
	if cur := st.cursorIndex(items, rows); cur >= 0 {
		st.cursorPath = items[rows[cur].item].e.Path
	}
}

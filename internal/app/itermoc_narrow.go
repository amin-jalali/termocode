package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc in a narrow window: the list and the page toggle ─────────────
//
// Below itermocNarrowW columns (the same rule as Runs) there is no room for
// the sidebar list next to a page. Itermoc is then one full-width page that
// starts on the list:
//
//	ITERMOC                                          ? keys  / filter
//	✦ Overview                                                    3 !
//	✦ PROFILES                                                    1 !
//	  ● review        Reviewer ▶ Fixer                          active
//	  ⚠ house         2 steps · not trusted yet           ▐ Trust… (t) ▌
//	Enter open · u use · Space fold · / filter · ? keys
//
// Rows gain a middle column and the inline fix pill. Enter opens the row's
// page full-width; Esc on the page returns to the list. Below 60 columns
// the right column drops first, then (below 44) the middle one; the glyph
// and the name always stay.

const itermocNarrowW = runsNarrowW

// itermocNarrow reports whether Itermoc opens as the one-page toggle.
func (m Model) itermocNarrow() bool { return m.w > 0 && m.w < itermocNarrowW }

// itermocShowList opens the page tab on the list (the narrow form).
func (m *Model) itermocShowList() tea.Cmd {
	p := m.itermocPage
	if p == nil {
		p = &itermocPageState{}
		m.itermocPage = p
	}
	p.list, p.scroll = true, 0
	m.focus = FocusEditor
	m.itermocOpenPageTab()
	return m.fetchStateCmd()
}

// itermocListMiddle is the middle column of a row: the steps of a profile,
// the provider and model of a slot, what a service is or waits for.
func (sn itermocSnap) listMiddle(r imRow) string {
	switch r.kind {
	case imRowProfile:
		p, _ := sn.profile(r.name)
		if p.trust == aiTrustAsk {
			if len(p.p.Steps) == 1 {
				return "1 step · not trusted yet"
			}
			return strconv.Itoa(len(p.p.Steps)) + " steps · not trusted yet"
		}
		var names []string
		for _, st := range p.p.Steps {
			t := st.Title
			if t == "" {
				t = st.ID
			}
			names = append(names, t)
		}
		if p.p.Planned() && len(names) > 0 {
			names = []string{names[0], "steps"}
		}
		return strings.Join(names, " "+imArrow(1)+" ")
	case imRowSlot:
		s, _ := sn.slot(r.name)
		if s.provider == "" {
			return "not set"
		}
		if s.model == "" {
			return s.provider
		}
		return s.provider + " · " + s.model
	case imRowService:
		s, _ := sn.service(r.name)
		for _, n := range sn.needs() {
			if n.row.kind == imRowService && n.row.name == s.Name {
				w, _, _ := strings.Cut(n.what, " · ")
				return w
			}
		}
		// Where it runs and what it may do (the tools are the right word).
		var parts []string
		if in, ok := sn.svc[s.Name]; ok {
			for _, p := range []string{in.where, in.access} {
				if p != "" {
					parts = append(parts, p)
				}
			}
		}
		return strings.Join(parts, " · ")
	case imRowProblem:
		if i, err := strconv.Atoi(r.name); err == nil && i >= 0 && i < len(sn.problems) {
			return imProblemWhy(sn.problems[i])
		}
	case imRowAgentStatus:
		if sn.listening {
			return strconv.Itoa(len(sn.agents)) + " connected"
		}
		return sn.whyTitle
	case imRowAgent:
		a, _ := sn.agent(r.name)
		return "since " + a.since
	}
	return ""
}

// listRight is the right column of a row: its one word (as in the
// sidebar) or, when it needs the user, the fix pill.
func (m Model) listRight(sn itermocSnap, r imRow) (word string, tok theme.Color256, fix *imPill) {
	for _, n := range sn.needs() {
		if n.row.kind == r.kind && n.row.name == r.name {
			f := n.fix
			return "", 0, &f
		}
	}
	switch r.kind {
	case imRowProfile:
		p, _ := sn.profile(r.name)
		switch {
		case p.active:
			return "active", theme.AccentGreen, nil
		case p.p.Source == ai.ProfileSourceProject:
			return "project", theme.TextMuted, nil
		}
	case imRowSlot:
		s, _ := sn.slot(r.name)
		w, t := itermocSlotWord(s)
		return w, t, nil
	case imRowService:
		s, _ := sn.service(r.name)
		w, t := itermocServiceWord(s)
		return w, t, nil
	case imRowAgentStatus:
		return sn.mode, theme.TextMuted, nil
	case imRowAgent:
		a, _ := sn.agent(r.name)
		return callsText(a.calls), theme.TextMuted, nil
	}
	return "", 0, nil
}

// itermocListLayout draws the full-width list (the narrow form).
func (m Model) itermocListLayout(w, h int) imPageLayout {
	st := m.itermoc
	if st == nil {
		st = newItermocState()
		st.refresh(m, false)
	} else if st.stale() {
		st.refresh(m, false)
	}
	sn := st.snap
	focused := m.focus == FocusEditor
	cv := newImCanvas(w, h, theme.BgSidebar)
	var hits []imPageHit
	hit := func(id string, x, y, hw int) {
		hits = append(hits, imPageHit{id: "im:pg:" + id, x: x, y: y, w: hw, h: 1})
	}

	// Header and hairline.
	cv.text(2, 0, "ITERMOC", imTokSecondary().B())
	right := inlineFilterTitle(st.filter, st.typing, theme.CurrentIconMode == theme.IconModeASCII)
	if right == "" && w >= 30 {
		right = "? keys  / filter"
	}
	if right != "" {
		cv.textR(w-1, 0, imTrunc(right, max(w-12, 1)), imTokMuted())
	}
	cv.hline(1, 1, w-2, imHair(), imFg(theme.BorderSubtle))

	bodyTop, bodyH := 2, max(h-3, 1)
	st.clamp()
	rows := st.rows()
	st.fitScroll(len(rows), bodyH)
	if len(rows) == 0 {
		msg := "Nothing to show"
		if strings.TrimSpace(st.filter) != "" {
			msg = "No rows match \"" + sanitizeOutputLine(st.filter) + "\" · Esc clears"
		}
		cv.text(2, bodyTop, imTrunc(theme.ASCIIText(msg), max(w-3, 1)), imTokMuted())
	}

	showRight, showMid := w >= 60, w >= 44
	nameW := 8
	for _, r := range rows {
		if r.kind == imRowProfile || r.kind == imRowSlot || r.kind == imRowService || r.kind == imRowAgent {
			nameW = max(nameW, lenR(m.itermocRowName(sn, r)))
		}
	}
	nameW = min(nameW, 18)
	midX := 5 + nameW + 2

	for i := st.scroll; i < len(rows) && i-st.scroll < bodyH; i++ {
		r := rows[i]
		y := bodyTop + i - st.scroll
		active := i == st.cursor && r.selectable()
		bg := gitSelBg(active, focused)
		if r.kind != imRowGap {
			cv.fill(0, y, w, 1, bg)
		}
		on := func(s imSt) imSt { s.bg = bg; return s }
		if active && focused {
			cv.put(0, y, '▌', on(imFg(theme.BorderFocus)))
		}
		switch r.kind {
		case imRowGap:
			continue
		case imRowOverview, imRowSection:
			icon, title := aiSpark(), "Overview"
			badge, needs := "", false
			if r.kind == imRowSection {
				icon, title = imSecIcon(r.sec), itermocSectionTitle(r.sec)
				badge, needs = st.sectionBadge(r.sec)
			} else if n := len(sn.needs()); n > 0 {
				badge, needs = strconv.Itoa(n)+" !", true
			}
			x := cv.text(2, y, icon+" ", on(imFg(theme.AIAccent)))
			tok := imTokSecondary().B()
			if active && focused {
				tok = imFg(theme.BorderFocus).B()
			}
			x = cv.text(x, y, title, on(tok))
			if r.kind == imRowSection && st.collapse[r.sec] && strings.TrimSpace(st.filter) == "" {
				cv.text(x+1, y, theme.GlyphTreeCollapsed.String(), on(imTokDim()))
			}
			if badge != "" {
				bt := imTokDim()
				if needs {
					bt = imTokWarn().B()
				}
				cv.textR(w-1, y, badge, on(bt))
			}
			id := "row:" + r.sec + "/" + r.name
			if r.kind == imRowSection {
				id = "sec:" + r.sec
			}
			hit(id, 0, y, w)
		case imRowAdd:
			cv.text(4, y, imText(imAddText(r.sec)), on(imTokDim()))
			hit("row:"+r.sec+"/"+r.name, 0, y, w)
		case imRowNote, imRowCall:
			cv.text(6, y, imTrunc(theme.ASCIIText(r.text), max(w-7, 1)), on(imTokDim()))
		default:
			g, gtok := m.itermocRowGlyph(sn, r)
			cv.text(3, y, g, on(imFg(gtok)))
			name := m.itermocRowName(sn, r)
			nameTok := imTokPrimary()
			if active {
				nameTok = nameTok.B()
			}
			// The right column first: what is left is the middle's room.
			rightX := w - 1
			if showRight {
				word, wtok, fix := m.listRight(sn, r)
				switch {
				case fix != nil:
					label := fix.text()
					px := w - 1 - imPillW(label)
					cv.pill(px, y, label, theme.BgSectionHdr, theme.AccentAmber, active)
					hit("rowfix:"+r.sec+"/"+r.name, px, y, imPillW(label))
					rightX = px
				case word != "":
					word = imTrunc(word, 16)
					rightX = w - 1 - lenR(word)
					cv.text(rightX, y, word, on(imFg(wtok)))
				}
			}
			cv.text(5, y, imTrunc(name, min(nameW, max(rightX-7, 4))), on(nameTok))
			if mid := theme.ASCIIText(sn.listMiddle(r)); showMid && mid != "" && rightX-2-midX >= 6 {
				cv.text(midX, y, imTrunc(mid, rightX-2-midX), on(imTokMuted()))
			}
			hit("row:"+r.sec+"/"+r.name, 0, y, rightX)
		}
	}

	foot := "Enter open · u use · Space fold · / filter · ? keys"
	if w < 56 {
		foot = "Enter open · / filter · ? keys"
	}
	cv.text(2, h-1, imTrunc(theme.ASCIIText(foot), max(w-3, 1)), imTokMuted())
	return imPageLayout{rows: cv.rows(0, h), hits: hits}
}

// itermocRowName is the name a row shows.
func (m Model) itermocRowName(sn itermocSnap, r imRow) string {
	switch r.kind {
	case imRowAgent:
		a, _ := sn.agent(r.name)
		return a.name
	case imRowAgentStatus:
		return "listening"
	case imRowProblem:
		if i, err := strconv.Atoi(r.name); err == nil && i >= 0 && i < len(sn.problems) {
			return imProblemTitle(sn.problems[i])
		}
	}
	return r.name
}

// itermocRowGlyph is the state mark of an object row (as in the sidebar).
func (m Model) itermocRowGlyph(sn itermocSnap, r imRow) (string, theme.Color256) {
	switch r.kind {
	case imRowProfile:
		p, _ := sn.profile(r.name)
		switch {
		case p.trust == aiTrustAsk:
			return imGlyph("untrusted")
		case p.active && p.trust != aiTrustRefused:
			return imGlyph("ready")
		}
		return imGlyph("idle")
	case imRowSlot:
		s, _ := sn.slot(r.name)
		if s.provider == "" || s.key == imKeyMissing || s.key == imKeyUnset {
			return imGlyph("needs")
		}
		return imGlyph("ready")
	case imRowService:
		s, _ := sn.service(r.name)
		return imGlyph(itermocServiceGlyph(s))
	case imRowAgentStatus:
		if sn.listening {
			return imGlyph("ready")
		}
		return imGlyph("idle")
	case imRowAgent:
		return imGlyph("ready")
	case imRowProblem:
		return imGlyph("needs")
	}
	return " ", theme.TextMuted
}

// ── Keys and clicks of the list ──────────────────────────────────────────

// itermocListPageKey runs a key on the full-width list. ok=false: the key
// is not the list's (it goes on, like ctrl+p).
func (m *Model) itermocListPageKey(k tea.KeyMsg) (tea.Cmd, bool) {
	st := m.itermocS()
	if m.itermocFilterKey(k) {
		return nil, true
	}
	st.clamp()
	row, have := st.current()
	switch k.String() {
	case "enter", "o":
		if !have {
			return nil, true
		}
		switch row.kind {
		case imRowSection:
			if k.String() == "enter" {
				st.toggleSection(row.sec)
				return nil, true
			}
		case imRowAdd:
			return m.itermocAdd(row.sec), true
		case imRowProblem:
			return m.itermocRun("fixfile", row), true
		}
		return m.itermocShowPage(row, true), true
	case "esc":
		return nil, true
	}
	switch {
	case k.Type == tea.KeyRunes && !k.Alt && !k.Paste, k.Type == tea.KeySpace:
	default:
		switch k.String() {
		case "up", "down", "home", "end", "tab", "shift+tab", "left", "right":
		default:
			return nil, false
		}
	}
	nm, cmd := m.itermocSidebarKey(k)
	*m = nm
	return cmd, true
}

// itermocListMouse is the wheel and the clicks on the full-width list.
func (m Model) itermocListMouse(msg tea.MouseMsg, pane editorPane) (Model, tea.Cmd, bool) {
	st := m.itermocS()
	switch {
	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		for i := 0; i < 3; i++ {
			st.move(1)
		}
		return m, nil, true
	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		for i := 0; i < 3; i++ {
			st.move(-1)
		}
		return m, nil, true
	case msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress:
		return m, nil, false
	}
	m.focus = FocusEditor
	var best *hitTarget
	for _, h := range m.itermocPageTargets(pane) {
		if msg.X >= h.X && msg.X < h.X+h.W && msg.Y >= h.Y && msg.Y < h.Y+h.H {
			h := h
			best = &h // the pill comes after its row: the last hit wins
		}
	}
	if best == nil {
		return m, nil, true
	}
	if m.probed(best.ID) {
		return m, nil, true
	}
	return m, m.itermocListClick(strings.TrimPrefix(best.ID, "im:pg:")), true
}

// itermocListClick runs a click target of the full-width list (the id
// without "im:pg:").
func (m *Model) itermocListClick(id string) tea.Cmd {
	st := m.itermocS()
	kind, key, _ := strings.Cut(id, ":")
	if kind == "sec" {
		st.toggleSection(key)
		return nil
	}
	sec, name, _ := strings.Cut(key, "/")
	for i, r := range st.rows() {
		if r.sec != sec || r.name != name || !r.selectable() || r.kind == imRowSection {
			continue
		}
		st.cursor = i
		st.touch()
		switch {
		case kind == "rowfix":
			for _, n := range st.snap.needs() {
				if n.row.kind == r.kind && n.row.name == r.name {
					cmd := m.itermocRun(n.fix.verb, r)
					m.itermocRefreshNow()
					return cmd
				}
			}
			return nil
		case r.kind == imRowAdd:
			return m.itermocAdd(r.sec)
		case r.kind == imRowProblem:
			return m.itermocRun("fixfile", r)
		}
		return m.itermocShowPage(r, true)
	}
	return nil
}

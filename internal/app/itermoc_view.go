package app

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc: layout and drawing ──────────────────────────────────────────
//
//	0  ITERMOC                          ? keys  / filter
//	1  hairline
//	2… rows (one line each; sections fold)
//	h-1 footer: Enter actions · Space fold · ? keys
//
// Row anatomy: lead, glyph, name, what it is, state words, and at most one
// inline fix button when something is wrong. At a narrow width the detail
// goes first, then the state words; the glyph and the name always stay.

const (
	itermocTopRows    = 2 // header + hairline
	itermocFooterRows = 1
)

// itermocBodyH is the number of row lines for a sidebar of height h.
func itermocBodyH(h int) int { return max(h-itermocTopRows-itermocFooterRows, 1) }

// imBtn is one clickable [Label (key)] of a buttons row or an inline fix.
type imBtn struct {
	id, label, key string
	start, end     int // sidebar-local cells, end exclusive
}

func (b imBtn) text() string {
	return "[" + imText(b.label) + " (" + b.key + ")]"
}

// imText is a label ready for the icon mode ("Trust…" is "Trust..." in ASCII).
func imText(s string) string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return strings.ReplaceAll(s, "…", "...")
	}
	return s
}

// imTrunc cuts s to w cells with the icon mode's ellipsis.
func imTrunc(s string, w int) string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return runewidth.Truncate(s, w, "...")
	}
	return runewidth.Truncate(s, w, "…")
}

// imLine is one drawn line: the styled text and its click targets.
type imLine struct {
	row  int // index into rows()
	text string
	fix  *imBtn
	btns []imBtn
}

type imSeg struct {
	t    string
	tok  theme.Color256
	bold bool
}

// imBuilder lays out the segments of one row, cell by cell.
type imBuilder struct {
	segs []imSeg
	used int
	w    int
}

func (b *imBuilder) room() int { return max(b.w-b.used, 0) }

// add appends t (cut to the room left) and returns its start cell.
func (b *imBuilder) add(t string, tok theme.Color256, bold bool) int {
	at := b.used
	if r := b.room(); r > 0 && t != "" {
		t = imTrunc(t, r)
		b.segs = append(b.segs, imSeg{t, tok, bold})
		b.used += runewidth.StringWidth(t)
	}
	return at
}

func (b *imBuilder) render(bg lipgloss.Color) string {
	var sb strings.Builder
	for _, s := range b.segs {
		st := lipgloss.NewStyle().Background(bg).Foreground(theme.LG(s.tok))
		if s.bold {
			st = st.Bold(true)
		}
		sb.WriteString(st.Render(s.t))
	}
	return clampSidebarRow(sb.String(), b.used, b.w, lipgloss.NewStyle().Background(bg))
}

// imGlyphs: the state marks and their ASCII twins.
func imGlyph(name string) (string, theme.Color256) {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	pick := func(u, a string) string {
		if ascii {
			return a
		}
		return u
	}
	switch name {
	case "ready":
		return pick("●", "*"), theme.AccentGreen
	case "idle":
		return pick("○", "o"), theme.TextMuted
	case "needs":
		return "!", theme.DiagWarning
	case "untrusted":
		return pick("⚠", "!"), theme.DiagWarning
	case "broken":
		return pick("✗", "x"), theme.DiagError
	case "agent":
		return pick("⇄", "<>"), theme.AIAccent
	}
	return " ", theme.TextMuted
}

func imKeyText(k imKeyState) string {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	switch k {
	case imKeyOK:
		if ascii {
			return "key ok"
		}
		return "key ✓"
	case imKeyMissing:
		if ascii {
			return "key missing"
		}
		return "key ✗"
	case imKeyNone:
		return "no key needed"
	}
	return "no key set"
}

// itermocButtonDefs are the buttons of a section, in order.
func itermocButtonDefs(sec string) []imBtn {
	switch sec {
	case imSecProfiles:
		return []imBtn{{id: "new", label: "New profile", key: "n"}, {id: "json", label: "Edit JSON", key: "J"}}
	case imSecModels:
		return []imBtn{{id: "slots", label: "Edit slots", key: "e"}}
	case imSecServices:
		return []imBtn{{id: "connect", label: "Connect a service", key: "c"}, {id: "log", label: "Log", key: "l"}}
	case imSecAgents:
		return []imBtn{{id: "start-claude", label: "Start Claude Code", key: "s"}, {id: "start-codex", label: "Start Codex", key: "S"},
			{id: "copy", label: "Copy config", key: "y"}, {id: "status", label: "Status", key: "T"}}
	}
	return nil
}

// itermocButtonLines packs a section's buttons into lines of width w: two
// spaces in front, two between, a button that does not fit starts a new
// line. start / end are filled in.
func itermocButtonLines(sec string, w int) [][]imBtn {
	var lines [][]imBtn
	var cur []imBtn
	col := 3
	for _, b := range itermocButtonDefs(sec) {
		bw := runewidth.StringWidth(b.text())
		if len(cur) > 0 && col+bw > w {
			lines, cur, col = append(lines, cur), nil, 3
		}
		b.start, b.end = col, col+bw
		cur = append(cur, b)
		col += bw + 2
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// itermocNameW is the width of the name column of a section's rows.
func (st *itermocState) nameW(sec string, w int) int {
	longest := 4
	cap := max(min(14, w/3), 4)
	for _, r := range st.sectionItems(sec) {
		n := 0
		switch r.kind {
		case imRowProfile, imRowSlot, imRowService:
			n = runewidth.StringWidth(r.name)
		case imRowAgentStatus:
			n = len("not listening")
		case imRowAgent:
			if a, ok := st.snap.agent(r.name); ok {
				n = runewidth.StringWidth(a.name)
			}
		}
		longest = max(longest, n)
	}
	return min(longest, cap)
}

// sectionNote is the quiet text at the right of a section header: the one
// number that matters, also when the section is folded.
func (st *itermocState) sectionNote(sec string) string {
	sn := st.snap
	switch sec {
	case imSecProfiles:
		for _, p := range sn.profiles {
			if p.active {
				return "active: " + p.p.Name
			}
		}
	case imSecModels:
		miss := 0
		for _, s := range sn.slots {
			if s.key == imKeyMissing {
				miss++
			}
		}
		if miss > 0 {
			return strconv.Itoa(miss) + " " + imKeyText(imKeyMissing)
		}
		return "how steps answer"
	case imSecServices:
		if len(sn.services) == 0 {
			return "none yet"
		}
		needs := 0
		for _, s := range sn.services {
			if itermocServiceNeeds(s) {
				needs++
			}
		}
		if needs > 0 {
			return strconv.Itoa(needs) + " needs you"
		}
		return "the chat can reach"
	case imSecAgents:
		mark, _ := imGlyph("agent")
		switch {
		case !sn.listening:
			return sn.mode + " · not listening"
		case len(sn.agents) == 0:
			return sn.mode + " · no agent"
		case len(sn.agents) == 1:
			return sn.mode + " · 1 agent " + mark
		}
		return sn.mode + " · " + strconv.Itoa(len(sn.agents)) + " agents " + mark
	}
	return ""
}

// itermocServiceNeeds: the service waits for the user (sign-in, a failure).
func itermocServiceNeeds(s mcp.Status) bool {
	return s.Expired || s.State == mcp.StateSignIn || s.State == mcp.StateFailed || s.State == mcp.StateInvalid
}

// itermocServiceGlyph picks the mark of a service row.
func itermocServiceGlyph(s mcp.Status) string {
	switch {
	case s.State == mcp.StateDisabled:
		return "idle"
	case s.State == mcp.StateFailed || s.State == mcp.StateInvalid:
		return "broken"
	case s.Expired || s.State == mcp.StateSignIn:
		return "needs"
	case s.State == mcp.StateRunning:
		return "ready"
	case !s.Trusted:
		return "untrusted"
	}
	return "idle"
}

// itermocLines lays out every row for width w. Each row is one line.
func (m Model) itermocLines(st *itermocState, w int, focused bool) []imLine {
	rows := st.rows()
	out := make([]imLine, 0, len(rows))
	nameW := map[string]int{}
	for _, sec := range itermocSectionOrder {
		nameW[sec] = st.nameW(sec, w)
	}
	sn := st.snap
	for i, r := range rows {
		active := i == st.cursor && r.selectable()
		bg := gitSelBg(active, focused)
		b := &imBuilder{w: w}
		l := imLine{row: i}
		lead := gitLead(active, focused, bg)
		b.used = 1
		switch r.kind {
		case imRowSection:
			fold := theme.GlyphExpand.String()
			if st.collapse[r.sec] && strings.TrimSpace(st.filter) == "" {
				fold = theme.GlyphTreeCollapsed.String()
			}
			b.add(fold+" ", theme.TextDim, false)
			col := theme.TextSecondary
			if active && focused {
				col = theme.BorderFocus
			}
			b.add(itermocSectionTitle(r.sec), col, true)
			if note := theme.ASCIIText(st.sectionNote(r.sec)); note != "" && b.room() > 4 {
				note = imTrunc(note, b.room()-2)
				gap := b.room() - runewidth.StringWidth(note) - 1
				b.add(strings.Repeat(" ", max(gap, 2)), theme.TextMuted, false)
				b.add(note, theme.TextMuted, false)
			}
		case imRowProfile:
			p, _ := sn.profile(r.name)
			m.itermocProfileLine(b, &l, p, nameW[r.sec])
		case imRowSlot:
			s, _ := sn.slot(r.name)
			itermocSlotLine(b, &l, s, nameW[r.sec])
		case imRowService:
			s, _ := sn.service(r.name)
			itermocServiceLine(b, &l, s, nameW[r.sec])
		case imRowAgentStatus:
			itermocStatusLine(b, sn, nameW[r.sec])
		case imRowAgent:
			a, _ := sn.agent(r.name)
			g, tok := imGlyph("agent")
			b.add(" ", theme.TextMuted, false)
			b.add(g+" ", tok, false)
			b.add(imPad(a.name, nameW[r.sec]), theme.TextPrimary, false)
			b.add("  "+theme.ASCIIText("since "+a.since+" · "+callsText(a.calls)+", "+strconv.Itoa(a.asked)+" asked"), theme.TextMuted, false)
		case imRowCall:
			b.add("    "+theme.ASCIIText(r.text), theme.TextMuted, false)
		case imRowNote:
			if r.strong {
				b.add("   "+r.text, theme.TextSecondary, true)
			} else {
				b.add("   "+r.text, theme.TextDim, false)
			}
		case imRowButtons:
			line, _ := strconv.Atoi(r.name)
			defs := itermocButtonLines(r.sec, w)
			if line < len(defs) {
				b.used = 0
				b.add("  ", theme.TextMuted, false)
				for k, btn := range defs[line] {
					if k > 0 {
						b.add("  ", theme.TextMuted, false)
					}
					btn.start = b.add(btn.text(), theme.TextPrimary, true)
					btn.end = b.used
					l.btns = append(l.btns, btn)
				}
				lead = ""
			}
		}
		l.text = lead + b.render(bg)
		out = append(out, l)
	}
	return out
}

func imPad(s string, w int) string {
	s = imTrunc(s, w)
	if pad := w - runewidth.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func callsText(n int) string {
	if n == 1 {
		return "1 call"
	}
	return strconv.Itoa(n) + " calls"
}

// itermocFix appends an inline fix button when it fits, and records it.
func itermocFix(b *imBuilder, l *imLine, id, label, key string) {
	btn := imBtn{id: id, label: label, key: key}
	t := " " + btn.text()
	if b.room() < runewidth.StringWidth(t) {
		return
	}
	b.add(" ", theme.TextMuted, false)
	btn.start = b.add(btn.text(), theme.TextPrimary, true)
	btn.end = b.used
	l.fix = &btn
}

func (m Model) itermocProfileLine(b *imBuilder, l *imLine, p imProfile, nameW int) {
	glyph, tok := " ", theme.TextMuted
	switch {
	case p.trust == aiTrustAsk:
		glyph, tok = imGlyph("untrusted")
	case p.trust == aiTrustRefused:
		glyph, tok = imGlyph("idle")
	case p.active:
		glyph, tok = imGlyph("ready")
	}
	b.add(" ", theme.TextMuted, false)
	b.add(glyph+" ", tok, false)
	nameTok := theme.TextPrimary
	b.add(imPad(p.p.Name, nameW), nameTok, p.active)
	room := b.room() - 2
	// A fix button first: it only appears when there is space left for it.
	fixW := 0
	if p.trust == aiTrustAsk {
		fixW = runewidth.StringWidth(" [Trust… (t)]") + 1
	}
	detailRoom := room - fixW
	if detailRoom >= 6 && m.ai != nil {
		state := ""
		switch {
		case p.p.Source == ai.ProfileSourceProject && p.trust == aiTrustAsk:
			state = "project · not asked yet"
		case p.p.Source == ai.ProfileSourceProject && p.trust == aiTrustRefused:
			state = "project · not used here"
		case p.p.Source == ai.ProfileSourceProject:
			state = "project"
		}
		state = theme.ASCIIText(state)
		text := ""
		if state != "" && detailRoom >= runewidth.StringWidth(state)+3+8 {
			text = state + theme.ASCIIText(" · ") + theme.ASCIIText(m.ai.aiPipelineText(p.p, detailRoom-runewidth.StringWidth(state)-3))
		} else {
			text = theme.ASCIIText(m.ai.aiPipelineText(p.p, detailRoom))
		}
		b.add("  "+text, theme.TextMuted, false)
	}
	if p.trust == aiTrustAsk {
		itermocFix(b, l, "trust", "Trust…", "t")
	}
}

func itermocSlotLine(b *imBuilder, l *imLine, s imSlot, nameW int) {
	glyph, tok := imGlyph("ready")
	switch s.key {
	case imKeyMissing, imKeyUnset:
		glyph, tok = imGlyph("needs")
	case imKeyNone:
		glyph, tok = imGlyph("idle")
	}
	b.add(" ", theme.TextMuted, false)
	b.add(glyph+" ", tok, false)
	b.add(imPad(s.name, nameW), theme.TextPrimary, false)
	detail := "not set"
	if s.provider != "" {
		detail = s.provider
		if s.model != "" {
			detail += theme.ASCIIText(" · ") + s.model
		}
	}
	state := imKeyText(s.key)
	fixW := 0
	if s.key == imKeyMissing {
		fixW = runewidth.StringWidth(" [Set key (k)]") + 1
	}
	room := b.room() - 2
	showState := room-fixW >= runewidth.StringWidth(state)+10
	stateW := 0
	if showState {
		stateW = runewidth.StringWidth(state) + 2
	}
	if d := room - fixW - stateW; d >= 6 {
		b.add("  "+imTrunc(detail, d), theme.TextMuted, false)
	}
	if showState {
		stok := theme.TextMuted
		switch s.key {
		case imKeyOK:
			stok = theme.AccentGreen
		case imKeyMissing:
			stok = theme.DiagWarning
		}
		b.add("  "+state, stok, false)
	}
	if s.key == imKeyMissing {
		itermocFix(b, l, "setkey", "Set key", "k")
	}
}

func itermocServiceLine(b *imBuilder, l *imLine, s mcp.Status, nameW int) {
	g, tok := imGlyph(itermocServiceGlyph(s))
	b.add(" ", theme.TextMuted, false)
	b.add(g+" ", tok, false)
	b.add(imPad(s.Name, nameW), theme.TextPrimary, false)
	fixW := 0
	if s.Expired || s.State == mcp.StateSignIn {
		fixW = runewidth.StringWidth(" [Sign in (i)]") + 1
	}
	if d := b.room() - 2 - fixW; d >= 6 {
		b.add("  "+imTrunc(theme.ASCIIText(mcpStatusHint(s)), d), theme.TextMuted, false)
	}
	if s.Expired || s.State == mcp.StateSignIn {
		itermocFix(b, l, "signin", "Sign in", "i")
	}
}

func itermocStatusLine(b *imBuilder, sn itermocSnap, nameW int) {
	g, tok := imGlyph("idle")
	name, detail := "not listening", sn.whySentence
	if sn.listening {
		g, tok = imGlyph("ready")
		name = "listening"
		detail = "mode " + sn.mode
		switch len(sn.agents) {
		case 0:
			detail += " · no agent connected"
		case 1:
			detail += " · 1 agent connected"
		default:
			detail += " · " + strconv.Itoa(len(sn.agents)) + " agents connected"
		}
	}
	b.add(" ", theme.TextMuted, false)
	b.add(g+" ", tok, false)
	b.add(imPad(name, max(nameW, len(name))), theme.TextPrimary, false)
	if d := b.room() - 2; d >= 6 {
		b.add("  "+imTrunc(theme.ASCIIText(detail), d), theme.TextMuted, false)
	}
}

// ── The whole view ───────────────────────────────────────────────────────

// itermocFitScroll keeps the cursor inside the window of bodyH lines.
func (st *itermocState) fitScroll(nRows, bodyH int) {
	st.scroll = max(0, min(st.scroll, max(nRows-bodyH, 0)))
	if st.cursor < st.scroll {
		st.scroll = st.cursor
	} else if st.cursor >= st.scroll+bodyH {
		st.scroll = st.cursor - bodyH + 1
	}
	// Show a section header above its first row when the cursor is there.
}

// itermocHeader is "ITERMOC" with the filter or the hint at the right.
func itermocHeader(w int, filter string, typing bool) string {
	fill := theme.Bg(theme.BgSidebar)
	label := theme.FgBg(theme.TextSecondary, theme.BgSidebar).Bold(true)
	muted := theme.FgBg(theme.TextMuted, theme.BgSidebar)
	left := fill.Render("  ") + label.Render("ITERMOC")
	leftW := 2 + len("ITERMOC")
	right := ""
	if text := inlineFilterTitle(filter, typing, theme.CurrentIconMode == theme.IconModeASCII); text != "" {
		right = imTrunc(text, max(w-leftW-3, 1))
	} else if w >= leftW+len("? keys  / filter")+2 {
		right = "? keys  / filter"
	} else if w >= leftW+6 {
		right = "?  /"
	}
	rw := runewidth.StringWidth(right)
	gap := max(w-leftW-rw-1, 0)
	if right == "" {
		return padBgToWidth(left, w)
	}
	return left + fill.Render(strings.Repeat(" ", gap)) + muted.Render(right) + fill.Render(" ")
}

func (m Model) renderItermocSidebar(w, h int) string {
	st := m.itermoc
	if st == nil {
		st = newItermocState()
		st.refresh(m, false)
	} else if st.stale() {
		st.refresh(m, false)
	}
	st.w = w
	focused := m.focus == FocusExplorer
	if h <= 0 {
		return ""
	}
	fill := func() string { return theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", max(w, 0))) }
	lines := []string{itermocHeader(w, st.filter, st.typing), theme.PaneHairline(w, theme.BgSidebar, focused)}
	bodyH := itermocBodyH(h)
	st.clamp()
	rows := st.rows()
	st.fitScroll(len(rows), bodyH)
	if len(rows) == 0 {
		msg := "Nothing to show"
		if strings.TrimSpace(st.filter) != "" {
			msg = "No rows match \"" + sanitizeOutputLine(st.filter) + "\" · Esc clears"
		}
		lines = append(lines, testsPad(theme.FgBg(theme.TextMuted, theme.BgSidebar).Render("  "+imTrunc(theme.ASCIIText(msg), max(w-2, 1))), w))
	}
	all := m.itermocLines(st, w, focused)
	for i := st.scroll; i < len(all) && len(lines) < itermocTopRows+bodyH; i++ {
		lines = append(lines, all[i].text)
	}
	for len(lines) < h-itermocFooterRows {
		lines = append(lines, fill())
	}
	foot := theme.ASCIIText("Enter actions · Space fold · ? keys")
	foot = imTrunc(foot, max(w-2, 1))
	lines = append(lines, testsPad(theme.Bg(theme.BgSidebar).Render(" ")+theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(foot), w))
	return strings.Join(lines[:h], "\n")
}

// itermocHits are the view's click targets, sidebar-local x: section
// headers, rows, inline fixes and buttons.
func (m Model) itermocHits(w int) []hitTarget {
	st := m.itermoc
	if st == nil || !st.loaded {
		return nil
	}
	rows := st.rows()
	bodyH := itermocBodyH(m.h)
	all := m.itermocLines(st, w, m.focus == FocusExplorer)
	var out []hitTarget
	for i := st.scroll; i < len(all) && i-st.scroll < bodyH; i++ {
		y := itermocTopRows + i - st.scroll
		r := rows[all[i].row]
		switch r.kind {
		case imRowSection:
			out = append(out, hitTarget{ID: "im:sec:" + r.sec, Label: itermocSectionTitle(r.sec), X: 0, Y: y, W: w, H: 1})
		case imRowButtons:
			for _, b := range all[i].btns {
				out = append(out, hitTarget{ID: "im:btn:" + r.sec + ":" + b.id, Label: b.text(), X: b.start, Y: y, W: b.end - b.start, H: 1})
			}
		default:
			if !r.selectable() {
				continue
			}
			if f := all[i].fix; f != nil {
				out = append(out, hitTarget{ID: "im:fix:" + r.sec + "/" + r.name + ":" + f.id, Label: f.text(), X: f.start, Y: y, W: f.end - f.start, H: 1})
			}
			out = append(out, hitTarget{ID: "im:row:" + r.sec + "/" + r.name, X: 0, Y: y, W: w, H: 1})
		}
	}
	return out
}

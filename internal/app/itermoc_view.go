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

// ── Itermoc: the list ────────────────────────────────────────────────────
//
//	0  ITERMOC                          ? keys  / filter
//	1  hairline
//	2… rows (one line each; groups fold)
//	h-1 footer: Enter actions · o page · ? keys
//
// The list is calm: a group header (icon, name, a count or "N !"), then one
// row per object: a state glyph, the name and ONE word at the right
// (active, project, sign in, off, a model name, N calls). The last row of
// a group is the dim "+ new profile…". No buttons: the detail page in the
// editor area (itermoc_page.go) has the verbs. At a narrow width the word
// is cut first; the glyph and the name always stay.

const (
	itermocTopRows    = 2 // header + hairline
	itermocFooterRows = 1
)

// itermocBodyH is the number of row lines for a sidebar of height h.
func itermocBodyH(h int) int { return max(h-itermocTopRows-itermocFooterRows, 1) }

// imBtn is one verb of a group (its key works from any row of the view).
type imBtn struct{ id, label, key string }

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

// imLine is one drawn line of the list.
type imLine struct {
	row  int // index into rows()
	text string
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

// addRight puts word at the right edge (one cell of margin); the word is
// cut when the row is too narrow, and dropped below 3 cells.
func (b *imBuilder) addRight(word string, tok theme.Color256, bold bool) {
	if word == "" {
		return
	}
	room := b.room() - 1
	if room < 3 {
		return
	}
	word = imTrunc(word, room)
	ww := runewidth.StringWidth(word)
	b.add(strings.Repeat(" ", room-ww), theme.TextMuted, false)
	b.add(word, tok, bold)
}

// addNameWord writes a name and, at the right edge, a word. The word is
// cut first so the name stays whole (to eight cells at least).
func (b *imBuilder) addNameWord(name string, nameTok theme.Color256, bold bool, word string, wordTok theme.Color256) {
	total := b.room() - 1
	nw := runewidth.StringWidth(name)
	if word != "" {
		if maxWord := total - min(nw, 8) - 2; maxWord >= 3 {
			word = imTrunc(word, maxWord)
		} else {
			word = ""
		}
	}
	if ww := runewidth.StringWidth(word); ww > 0 {
		name = imTrunc(name, max(total-ww-2, 1))
	}
	b.add(name, nameTok, bold)
	b.addRight(word, wordTok, false)
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

// imGlyph: the state marks and their ASCII twins.
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

// The group icons (the activity bar's own icon is the Itermoc one).
var (
	imIconProfiles = theme.GlyphAI
	imIconModels   = theme.Icon{NerdFont: "", Unicode: "ϟ", ASCII: "~"} // nf-fa-bolt
	imIconServices = theme.Icon{NerdFont: "", Unicode: "⌁", ASCII: "o"} // nf-fa-plug
	imIconAgents   = theme.Icon{NerdFont: "⇄", Unicode: "⇄", ASCII: ">"}
)

func imSecIcon(sec string) string {
	switch sec {
	case imSecModels:
		return imIconModels.String()
	case imSecServices:
		return imIconServices.String()
	case imSecAgents:
		return imIconAgents.String()
	}
	return imIconProfiles.String()
}

// itermocButtonDefs are the verbs of a group, in order. Their keys work
// from any row; the detail page and the key card show them.
func itermocButtonDefs(sec string) []imBtn {
	switch sec {
	case imSecProfiles:
		return []imBtn{{id: "new", label: "New profile", key: "n"}, {id: "json", label: "Edit JSON", key: "J"}}
	case imSecModels:
		return []imBtn{{id: "add", label: "Add slot", key: "a"}, {id: "slots", label: "Edit JSON", key: "E"}}
	case imSecServices:
		return []imBtn{{id: "connect", label: "Connect a service", key: "c"}, {id: "log", label: "Log", key: "l"}}
	case imSecAgents:
		return []imBtn{{id: "start-claude", label: "Start Claude Code", key: "s"}, {id: "start-codex", label: "Start Codex", key: "S"},
			{id: "copy", label: "Copy config", key: "y"}, {id: "status", label: "Status", key: "T"}}
	}
	return nil
}

// imAddText is the dim row at the end of a group.
func imAddText(sec string) string {
	switch sec {
	case imSecProfiles:
		return "+ new profile…"
	case imSecModels:
		return "+ add slot…"
	case imSecServices:
		return "+ connect…"
	}
	return "+ start one…"
}

// sectionBadge is the quiet text at the right of a group header: the
// number of rows, or "N !" when N of them need the user. Also shown when
// the group is folded.
func (st *itermocState) sectionBadge(sec string) (text string, needs bool) {
	sn := st.snap
	count, need := 0, 0
	switch sec {
	case imSecProfiles:
		count = len(sn.profiles)
		for _, p := range sn.profiles {
			if p.trust == aiTrustAsk {
				need++
			}
		}
		need += len(sn.problems)
	case imSecModels:
		count = len(sn.slots)
		for _, s := range sn.slots {
			if s.key == imKeyMissing || s.key == imKeyUnset {
				need++
			}
		}
	case imSecServices:
		count = len(sn.services)
		for _, s := range sn.services {
			if itermocServiceNeeds(s) {
				need++
			}
		}
	case imSecAgents:
		count = len(sn.agents)
	}
	if need > 0 {
		return strconv.Itoa(need) + " !", true
	}
	return strconv.Itoa(count), false
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

// itermocServiceWord is the one word of a service row.
func itermocServiceWord(s mcp.Status) (string, theme.Color256) {
	switch {
	case s.Expired || s.State == mcp.StateSignIn:
		return "sign in", theme.DiagWarning
	case s.State == mcp.StateFailed || s.State == mcp.StateInvalid:
		return "failed", theme.DiagError
	case s.State == mcp.StateDisabled:
		return "off", theme.TextMuted
	case !s.Trusted:
		return "not trusted", theme.DiagWarning
	case s.State == mcp.StateRunning && s.Tools > 0:
		return strconv.Itoa(s.Tools) + " tools", theme.TextMuted
	case s.State == mcp.StateRunning:
		return "on", theme.TextMuted
	}
	return s.State, theme.TextMuted
}

// itermocSlotWord is the one word of a model row: the short model name,
// or what is missing.
func itermocSlotWord(s imSlot) (string, theme.Color256) {
	switch {
	case s.provider == "":
		return "not set", theme.DiagWarning
	case s.key == imKeyMissing || s.key == imKeyUnset:
		return "no key", theme.DiagWarning
	case s.model != "":
		return itermocShortModel(s.model), theme.TextMuted
	}
	return s.provider, theme.TextMuted
}

// itermocShortModel is the model id as the list shows it: "sonnet-4-5",
// "qwen 7b" (an Ollama tag "qwen2.5-coder:7b").
func itermocShortModel(model string) string {
	short := aiShortModel(model)
	if name, tag, ok := strings.Cut(short, ":"); ok && tag != "" {
		if i := strings.IndexAny(name, "0123456789.-"); i > 0 {
			name = name[:i]
		}
		return name + " " + tag
	}
	return short
}

func callsText(n int) string {
	if n == 1 {
		return "1 call"
	}
	return strconv.Itoa(n) + " calls"
}

// itermocLines lays out every row for width w. Each row is one line.
func (m Model) itermocLines(st *itermocState, w int, focused bool) []imLine {
	rows := st.rows()
	out := make([]imLine, 0, len(rows))
	sn := st.snap
	for i, r := range rows {
		active := i == st.cursor && r.selectable()
		bg := gitSelBg(active, focused)
		b := &imBuilder{w: w}
		lead := gitLead(active, focused, bg)
		b.used = 1
		switch r.kind {
		case imRowGap:
			lead = ""
			b.used = 0
		case imRowSection:
			col, icon := theme.TextSecondary, theme.AIAccent
			if active && focused {
				col = theme.BorderFocus
			}
			folded := st.collapse[r.sec] && strings.TrimSpace(st.filter) == ""
			if folded {
				icon = theme.TextDim
			}
			b.add(imSecIcon(r.sec)+" ", icon, false)
			b.add(itermocSectionTitle(r.sec), col, true)
			if folded {
				b.add(" "+theme.GlyphTreeCollapsed.String(), theme.TextDim, false)
			}
			if text, needs := st.sectionBadge(r.sec); text != "" {
				tok := theme.TextDim
				if needs {
					tok = theme.DiagWarning
				}
				b.addRight(text, tok, needs)
			}
		case imRowOverview:
			itermocOverviewLine(b, sn, active && focused)
		case imRowProfile:
			p, _ := sn.profile(r.name)
			m.itermocProfileLine(b, p)
		case imRowSlot:
			s, _ := sn.slot(r.name)
			itermocSlotLine(b, s)
		case imRowService:
			s, _ := sn.service(r.name)
			itermocServiceLine(b, s)
		case imRowProblem:
			b.add("  ", theme.TextMuted, false)
			g, tok := imGlyph("needs")
			b.add(g+" ", tok, false)
			title := ""
			if i, err := strconv.Atoi(r.name); err == nil && i >= 0 && i < len(sn.problems) {
				title = imProblemTitle(sn.problems[i])
			}
			b.addNameWord(title, theme.TextPrimary, active, "Fix", theme.DiagWarning)
		case imRowAgentStatus:
			itermocStatusLine(b, sn)
		case imRowAgent:
			a, _ := sn.agent(r.name)
			b.add("  ", theme.TextMuted, false)
			g, tok := imGlyph("ready")
			b.add(g+" ", tok, false)
			b.addNameWord(a.name, theme.TextPrimary, active, callsText(a.calls), theme.TextMuted)
		case imRowCall:
			b.add("     "+theme.ASCIIText(r.text), theme.TextMuted, false)
		case imRowNote:
			b.add("     "+r.text, theme.TextDim, false)
		case imRowAdd:
			b.add("  ", theme.TextMuted, false)
			col := theme.TextDim
			if active && focused {
				col = theme.TextSecondary
			}
			b.add(imText(imAddText(r.sec)), col, false)
		}
		out = append(out, imLine{row: i, text: lead + b.render(bg)})
	}
	return out
}

// itermocOverviewLine is the top row: "✦ Overview" and, at the right, how
// many things need the user ("3 !"), or nothing.
func itermocOverviewLine(b *imBuilder, sn itermocSnap, lit bool) {
	col := theme.TextSecondary
	if lit {
		col = theme.BorderFocus
	}
	b.add(aiSpark()+" ", theme.AIAccent, false)
	word := ""
	if n := len(sn.needs()); n > 0 {
		word = strconv.Itoa(n) + " !"
	}
	b.addNameWord("Overview", col, true, word, theme.DiagWarning)
}

func (m Model) itermocProfileLine(b *imBuilder, p imProfile) {
	glyph, tok := imGlyph("idle")
	switch {
	case p.trust == aiTrustAsk:
		glyph, tok = imGlyph("untrusted")
	case p.active && p.trust != aiTrustRefused:
		glyph, tok = imGlyph("ready")
	}
	word, wtok := "", theme.TextMuted
	switch {
	case p.active:
		word, wtok = "active", theme.AccentGreen
	case p.p.Source == ai.ProfileSourceProject:
		word = "project"
		if p.trust == aiTrustAsk {
			wtok = theme.DiagWarning
		}
	}
	b.add("  ", theme.TextMuted, false)
	b.add(glyph+" ", tok, false)
	b.addNameWord(p.p.Name, theme.TextPrimary, p.active, word, wtok)
}

func itermocSlotLine(b *imBuilder, s imSlot) {
	glyph, tok := imGlyph("ready")
	switch s.key {
	case imKeyMissing, imKeyUnset:
		glyph, tok = imGlyph("needs")
	}
	if s.provider == "" {
		glyph, tok = imGlyph("needs")
	}
	word, wtok := itermocSlotWord(s)
	b.add("  ", theme.TextMuted, false)
	b.add(glyph+" ", tok, false)
	b.addNameWord(s.name, theme.TextPrimary, false, word, wtok)
}

func itermocServiceLine(b *imBuilder, s mcp.Status) {
	g, tok := imGlyph(itermocServiceGlyph(s))
	word, wtok := itermocServiceWord(s)
	b.add("  ", theme.TextMuted, false)
	b.add(g+" ", tok, false)
	b.addNameWord(s.Name, theme.TextPrimary, false, word, wtok)
}

func itermocStatusLine(b *imBuilder, sn itermocSnap) {
	g, tok := imGlyph("idle")
	if sn.listening {
		g, tok = imGlyph("ready")
	}
	b.add("  ", theme.TextMuted, false)
	b.add(g+" ", tok, false)
	b.addNameWord("listening", theme.TextPrimary, false, sn.mode, theme.TextMuted)
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
	// A header is the row above its first item: show it when the cursor is there.
	rows := st.rows()
	if st.scroll > 0 && st.scroll == st.cursor && st.cursor < len(rows) {
		for back := 1; back <= 2 && st.cursor-back >= 0; back++ {
			if k := rows[st.cursor-back].kind; k == imRowSection {
				st.scroll = st.cursor - back
				break
			} else if k != imRowGap {
				break
			}
		}
	}
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
	foot := ""
	for _, f := range []string{"Enter actions · o page · ? keys", "Enter menu · o page · ? keys", "Enter menu · o page · ?", "o page · ? keys"} {
		if foot = theme.ASCIIText(f); runewidth.StringWidth(foot) <= w-2 {
			break
		}
	}
	foot = imTrunc(foot, max(w-2, 1))
	lines = append(lines, testsPad(theme.Bg(theme.BgSidebar).Render(" ")+theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(foot), w))
	return strings.Join(lines[:h], "\n")
}

// itermocHits are the list's click targets, sidebar-local x: the group
// headers and the rows.
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
		default:
			if !r.selectable() {
				continue
			}
			out = append(out, hitTarget{ID: "im:row:" + r.sec + "/" + r.name, X: 0, Y: y, W: w, H: 1})
		}
	}
	return out
}

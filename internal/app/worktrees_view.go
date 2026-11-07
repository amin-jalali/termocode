package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── WORKTREES view: rendering (WTX-2) ────────────────────────────────────
//
// Layout (sidebar-local rows):
//
//	0     WORKTREES · filter
//	1     hairline
//	2…    PINNED ──── 2 / cards / ACTIVE … (cards: up to 4 lines)
//	h-2   footer: the cursor's folder
//	h-1   footer: keys
//
// A card's lines (each one goes first when the panel is narrow):
//
//	1  pin · name · agent state      ⚐ feat/wt-sidebar   ● working
//	2  branch · ahead/behind · dirty   ⑂ feat/wt-sidebar · 3↑ · 4 dirty
//	3  agent · last note               ✦ Claude Code · "adding the pin row"
//	4  links · last seen               #412 · 2m ago
//
// The compact layout (a short window, or a panel under wtCardMinW) is one
// line per worktree, the state glyph first; Enter there opens the card
// page (st.detail).

const (
	wtListTop    = 2
	wtFooterRows = 2
	// wtCardMinW / wtCardFullW: panel widths for 2-line and 4-line cards;
	// wtCardMinH: below this window height the list is compact.
	wtCardMinW  = 22
	wtCardFullW = 28
	wtCardMinH  = 30
)

func wtBodyH(h int) int { return max(h-wtListTop-wtFooterRows, 1) }

// wtCompact: one line per worktree.
func wtCompact(w, h int) bool { return w < wtCardMinW || h < wtCardMinH }

// wtTabs is this window's terminal tabs as the view needs them.
func (m Model) wtTabs() []wtTabAgent {
	if m.wt != nil && m.wt.off {
		return m.wt.demoTabs
	}
	out := make([]wtTabAgent, 0, len(m.terminalTabs))
	for _, t := range m.terminalTabs {
		cwd := t.Cwd
		if t.Worktree != "" { // an agent started in a worktree belongs to it (AGX-2)
			cwd = t.Worktree
		}
		out = append(out, wtTabAgent{name: t.agentSessionTitle(), cwd: cwd, state: t.AgentState})
	}
	return out
}

// wtTabSig changes when a tab's agent, folder or state does (the render
// cache key: the tabs live outside wtState).
func (m Model) wtTabSig() string {
	var b strings.Builder
	for _, t := range m.terminalTabs {
		if t.Agent != "" {
			fmt.Fprintf(&b, "%s@%s=%d;", t.Agent, t.Cwd, t.AgentState)
		}
	}
	return b.String()
}

// dirty reads the cached dirty count (ok: known).
func (st *wtState) dirty(path string) (int, bool) {
	s, ok := st.cache.Get(path)
	if !ok || s.Err != "" {
		return 0, false
	}
	return s.Dirty, true
}

// list is the cards and the rows that pass the filters.
func (m Model) wtList() ([]wtItem, []wtRow) {
	st := m.wt
	items := wtItems(st.entries, m.wtTabs(), st.others, st.root)
	for i := range items {
		if r := st.setups[items[i].e.Path]; r != nil {
			items[i].setup = r.state
		}
		if r := st.runs[items[i].e.Path]; r != nil {
			items[i].runURL = r.url
		}
	}
	return items, wtRows(items, st.kind, st.filter, st.dirty)
}

func (m Model) renderWorktreesSidebar(w, h int) string {
	st := m.wt
	focused := m.focus == FocusExplorer
	now := clock.Now()
	key := fmt.Sprintf("%d|%s|%d|%d|%v|%v|%d|%s", st.seq, st.cursorPath, w, h, focused, theme.CurrentRenderKey(), now.Unix()/60, m.wtTabSig())
	st.render.Lock()
	defer st.render.Unlock()
	if st.render.key == key && st.render.out != "" {
		return st.render.out
	}
	items, rows := m.wtList()
	out := st.renderRaw(items, rows, w, h, focused, now)
	st.render.key, st.render.out = key, out
	return out
}

func (st *wtState) titleSuffix() string {
	var parts []string
	if st.kind != wtKindAll {
		parts = append(parts, st.kind.String())
	}
	if st.filter != "" || st.filterTyping {
		parts = append(parts, inlineFilterTitle(st.filter, st.filterTyping, theme.CurrentIconMode == theme.IconModeASCII))
	}
	return strings.Join(parts, " ")
}

func (st *wtState) renderRaw(items []wtItem, rows []wtRow, w, h int, focused bool, now time.Time) string {
	fill := func() string { return theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", max(w, 0))) }
	muted := func(s string) string { return theme.FgBg(theme.TextMuted, theme.BgSidebar).Render(s) }
	lines := []string{
		sidebarTitle("WORKTREES", w, focused, st.titleSuffix()),
		theme.PaneHairline(w, theme.BgSidebar, focused),
	}
	done := func() string {
		for len(lines) < h {
			lines = append(lines, fill())
		}
		return strings.Join(lines[:h], "\n")
	}
	switch {
	case !st.loaded:
		lines = append(lines, testsPad(muted("  Reading the worktrees…"), w))
		return done()
	case st.err != "":
		lines = append(lines, renderEmptyState(w, h-2, theme.BgSidebar, emptyState{Glyph: theme.GlyphWorktree,
			Title: "Not a git repository", Hint: "Worktrees show here when the folder is in a git repository.", Key: "R", Label: "Look again"})...)
		return done()
	}
	cur := st.cursorIndex(items, rows)
	var body []string
	if st.detail && wtCompact(w, h) && cur >= 0 {
		body = st.renderDetail(items[rows[cur].item], w, now)
	} else {
		body, _ = st.bodyLines(items, rows, cur, w, h, focused, now)
		if len(rows) == 0 {
			body = append(body, testsPad(muted("  No worktree matches"), w))
		}
	}
	for len(body) < wtBodyH(h) {
		body = append(body, fill())
	}
	lines = append(lines, body[:wtBodyH(h)]...)
	for _, f := range st.footer(items, rows, cur, w, h) {
		f = runewidth.Truncate(f, max(w-2, 1), "…")
		lines = append(lines, testsPad(theme.Bg(theme.BgSidebar).Render(" ")+muted(f), w))
	}
	return done()
}

// bodyLines draws the list scrolled so the cursor's card shows; owner[i]
// is the row index line i belongs to (-1: a blank line).
func (st *wtState) bodyLines(items []wtItem, rows []wtRow, cur, w, h int, focused bool, now time.Time) (lines []string, owner []int) {
	compact := wtCompact(w, h)
	fill := theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", max(w, 0)))
	var all []string
	var own []int
	first, last := -1, -1
	for i, r := range rows {
		if r.item < 0 {
			if compact {
				continue
			}
			if i > 0 {
				all, own = append(all, fill), append(own, -1)
			}
			all, own = append(all, renderServiceSection(r.group, strconv.Itoa(r.count), false, false, focused, w)), append(own, i)
			continue
		}
		active := i == cur
		it := items[r.item]
		it.depth = r.depth
		var card []string
		if compact {
			card = []string{wtCompactLine(it, st.status(it), active, focused, w)}
		} else {
			card = wtCardLines(it, st.status(it), active, focused, w, now)
		}
		if active {
			first, last = len(all), len(all)+len(card)-1
		}
		for _, l := range card {
			all, own = append(all, l), append(own, i)
		}
	}
	bh := wtBodyH(h)
	top := 0
	if last >= bh {
		top = last - bh + 1
	}
	if first >= 0 && first < top {
		top = first
	}
	// Keep the group header above the cursor's card in view.
	if first > 0 && top == first && own[first-1] >= 0 && rows[own[first-1]].item < 0 && last-first+1 < bh {
		top--
	}
	end := min(top+bh, len(all))
	return all[top:end], own[top:end]
}

// status is the cached status of a card (ok: known).
type wtStatus struct {
	worktrees.Status
	known bool
}

func (st *wtState) status(it wtItem) wtStatus {
	s, ok := st.cache.Get(it.e.Path)
	return wtStatus{Status: s, known: ok}
}

// wtStateGlyph is the state mark and its colour.
func wtStateGlyph(state string) (theme.Icon, theme.Color256) {
	switch state {
	case wtStateWorking:
		return theme.GlyphSvcUp, theme.GitAdded
	case wtStateWaiting:
		return theme.GlyphAgentWaiting, theme.AccentLavender
	case wtStateDone:
		return theme.GlyphAgentDone, theme.AccentGreen
	case wtStateAsleep:
		return theme.GlyphAsleep, theme.TextDim
	case wtStateGone:
		return theme.GlyphFail, theme.DiagError
	case wtStateArchived:
		return theme.GlyphStop, theme.TextDim
	}
	return theme.GlyphSvcStopped, theme.TextDim
}

// wtSeg is one coloured piece of a line.
type wtSeg struct {
	text string
	fg   theme.Color256
}

// wtPaint draws a line: lead, left pieces, right pieces flush right. The
// left side is cut (with …) so the right side always shows whole.
func wtPaint(left, right []wtSeg, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	row := lipgloss.NewStyle().Background(bg)
	paint := func(s wtSeg) string {
		return lipgloss.NewStyle().Background(bg).Foreground(theme.LG(s.fg)).Render(s.text)
	}
	width := func(ss []wtSeg) int {
		n := 0
		for _, s := range ss {
			n += runewidth.StringWidth(s.text)
		}
		return n
	}
	rw := width(right)
	if rw > w-4 {
		right, rw = nil, 0
	}
	room := w - 1 - rw
	if rw > 0 {
		room--
	}
	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	used := 1
	for _, s := range left {
		if room <= 0 {
			break
		}
		t := s.text
		if tw := runewidth.StringWidth(t); tw > room {
			t = runewidth.Truncate(t, room, "…")
		}
		sb.WriteString(paint(wtSeg{t, s.fg}))
		tw := runewidth.StringWidth(t)
		used += tw
		room -= tw
	}
	if rw > 0 {
		sb.WriteString(row.Render(strings.Repeat(" ", max(w-used-rw, 1))))
		used = max(used+1, w-rw)
		for _, s := range right {
			sb.WriteString(paint(s))
		}
		used += rw
	}
	return clampSidebarRow(sb.String(), used, w, row)
}

// wtAheadBehind is "3↑ 1↓" ("+3 -1" with ASCII icons); "" when even.
func wtAheadBehind(s wtStatus) string {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	var parts []string
	if s.Ahead > 0 {
		if ascii {
			parts = append(parts, "+"+strconv.Itoa(s.Ahead))
		} else {
			parts = append(parts, strconv.Itoa(s.Ahead)+"↑")
		}
	}
	if s.Behind > 0 {
		if ascii {
			parts = append(parts, "-"+strconv.Itoa(s.Behind))
		} else {
			parts = append(parts, strconv.Itoa(s.Behind)+"↓")
		}
	}
	return strings.Join(parts, " ")
}

// wtBranch is the branch a card shows: the name, "detached abc1234", or
// the folder for one that is gone.
func wtBranch(e worktrees.Entry) string {
	switch {
	case e.Meta.State == worktrees.StateArchived && e.Branch != "":
		return e.Branch
	case e.Gone:
		return "folder gone"
	case e.Branch != "":
		return e.Branch
	case len(e.Head) >= 7:
		return "detached " + e.Head[:7]
	}
	return "detached"
}

// wtTitle is the card's name: an agent lane is "agent <id>" (its
// branch, on line 2, says termocode/agent-<id>).
func wtTitle(it wtItem) string {
	if it.e.Kind == worktrees.KindAgent && it.e.Meta.Name == "" && it.e.AgentID != "" {
		return "agent " + it.e.AgentID
	}
	return it.e.Title()
}

func wtPinGlyph(it wtItem) wtSeg {
	if it.e.Meta.Pinned {
		return wtSeg{theme.GlyphPin.String(), theme.AccentAmber}
	}
	return wtSeg{" ", theme.TextDim}
}

// wtCardLines is a card: up to 4 lines, fewer when the panel is narrow
// or a line has nothing to say.
func wtCardLines(it wtItem, s wtStatus, active, focused bool, w int, now time.Time) []string {
	dot := theme.ASCIIText(" · ")
	g, gc := wtStateGlyph(it.state)
	word := it.state
	if it.agents > 1 {
		word = fmt.Sprintf("%d %s", it.agents, it.state)
	}
	titleFg := theme.TextPrimary
	if it.e.Gone || it.state == wtStateAsleep || it.state == wtStateArchived {
		titleFg = theme.TextMuted
	}
	right := []wtSeg{{g.String(), gc}, {" " + word, theme.TextMuted}}
	hereUp := it.here && it.state == wtStateIdle // "you are here" on line 1, where it always fits
	switch {
	case hereUp:
		right = []wtSeg{{"you are here", theme.AccentGreen}}
	case it.state == wtStateIdle:
		right = []wtSeg{{g.String(), gc}}
	}
	out := []string{wtPaint([]wtSeg{wtPinGlyph(it), {" " + wtTreeMark(it.depth) + wtTitle(it), titleFg}}, right, active, focused, w)}
	ind := "" // the lines under a child's name start under it
	if it.depth > 0 {
		ind = "  "
	}

	// 2: branch · ahead/behind · dirty (· you are here).
	var tail []wtSeg
	switch {
	case it.e.Gone:
	case !s.known:
		tail = append(tail, wtSeg{dot + "…", theme.TextDim})
	case s.Err != "":
		tail = append(tail, wtSeg{dot + "no status", theme.DiagError})
	default:
		if ab := wtAheadBehind(s); ab != "" {
			tail = append(tail, wtSeg{dot + ab, theme.TextSecondary})
		}
		if s.Dirty > 0 {
			tail = append(tail, wtSeg{dot + strconv.Itoa(s.Dirty) + " dirty", theme.GitModified})
		} else {
			tail = append(tail, wtSeg{dot + "clean", theme.TextDim})
		}
	}
	if it.here && !hereUp {
		tail = append(tail, wtSeg{dot + "you are here", theme.AccentGreen})
	}
	if it.state == wtStateArchived {
		tail = append(tail, wtSeg{dot + "archived · x restores", theme.TextDim})
	}
	line2 := append([]wtSeg{{ind + "  " + theme.GlyphWorktree.String() + " ", theme.TextDim}, {wtBranch(it.e), theme.TextSecondary}}, tail...)
	out = append(out, wtPaintTail(line2, active, focused, w))
	if w < wtCardFullW {
		return out
	}
	// 3: agent · last note.
	note := ""
	if n, ok := it.e.Meta.LastNote(); ok {
		note = `"` + n.Text + `"`
	}
	switch {
	case it.agent.name != "" && note != "":
		out = append(out, wtPaint([]wtSeg{{ind + "  " + theme.GlyphAI.String() + " ", theme.AccentLavender}, {it.agent.name, theme.TextSecondary}, {dot + note, theme.TextMuted}}, nil, active, focused, w))
	case it.agent.name != "":
		out = append(out, wtPaint([]wtSeg{{ind + "  " + theme.GlyphAI.String() + " ", theme.AccentLavender}, {it.agent.name, theme.TextSecondary}, {dot + wtAgentLine(it), theme.TextMuted}}, nil, active, focused, w))
	case note != "":
		out = append(out, wtPaint([]wtSeg{{ind + "  " + note, theme.TextMuted}}, nil, active, focused, w))
	}
	// 4: links · last seen · locked.
	var links []string
	if c := it.e.Meta.Checkpoint; c != "" {
		links = append(links, c)
	}
	if l := wtLinkLine(it.e.Meta); l != "" {
		links = append(links, l)
	}
	if wtNoMeta(it) {
		links = append(links, "— no meta (A adopts)")
	}
	if it.e.Locked {
		links = append(links, "locked")
	}
	if s := wtSetupLine(it.setup); s != "" {
		links = append(links, theme.ASCIIText(s))
	}
	if s := wtRunLine(it.e.Meta.Port, it.runURL); s != "" {
		links = append(links, theme.ASCIIText(s))
	}
	if !it.e.Meta.LastSeen.IsZero() {
		links = append(links, formatRecentTime(it.e.Meta.LastSeen, now))
	}
	if len(links) > 0 {
		out = append(out, wtPaint([]wtSeg{{ind + "  " + strings.Join(links, dot), theme.TextMuted}}, nil, active, focused, w))
	}
	return out
}

// wtAgentLine says what the card's agent does in plain words.
func wtAgentLine(it wtItem) string {
	switch it.agent.state {
	case wtStateWaiting:
		return "waits for you"
	case wtStateDone:
		return "done"
	}
	if it.agent.tab < 0 {
		return "working (other window)"
	}
	return "working"
}

// wtPaintTail draws line 2: the branch is cut first so the counts and
// "you are here" stay whole; the last pieces go when even that is short.
func wtPaintTail(segs []wtSeg, active, focused bool, w int) string {
	width := func(ss []wtSeg) int {
		n := 0
		for _, s := range ss {
			n += runewidth.StringWidth(s.text)
		}
		return n
	}
	const minBranch = 6
	for len(segs) > 2 && width(segs[:1])+minBranch+width(segs[2:]) > w-1 {
		segs = segs[:len(segs)-1]
	}
	if room := w - 1 - width(segs[:1]) - width(segs[2:]); room > 0 && runewidth.StringWidth(segs[1].text) > room {
		segs[1].text = runewidth.Truncate(segs[1].text, room, "…")
	}
	return wtPaint(segs, nil, active, focused, w)
}

// wtCompactLine is the one-line card: pin, state glyph, name, counts.
func wtCompactLine(it wtItem, s wtStatus, active, focused bool, w int) string {
	g, gc := wtStateGlyph(it.state)
	name := wtTitle(it)
	if it.e.Kind == worktrees.KindExternal && !it.e.Gone {
		name += " (ext)"
	}
	var right string
	switch {
	case it.state == wtStateAsleep && !it.e.Meta.LastSeen.IsZero():
		right = strings.TrimSuffix(formatRecentTime(it.e.Meta.LastSeen, clock.Now()), " ago")
	case it.state == wtStateArchived:
		right = "archived"
	case it.e.Gone:
		right = "gone"
	case !s.known:
		right = "…"
	case s.Err != "":
		right = "?"
	default:
		var parts []string
		if ab := wtAheadBehind(s); ab != "" {
			parts = append(parts, ab)
		}
		if s.Dirty > 0 {
			parts = append(parts, "~"+strconv.Itoa(s.Dirty))
		}
		right = strings.Join(parts, " ")
		if right == "" {
			right = "clean"
		}
	}
	if it.agents > 1 {
		right = fmt.Sprintf("%d agents", it.agents)
	}
	return wtPaint([]wtSeg{wtPinGlyph(it), {g.String(), gc}, {" " + wtTreeMark(it.depth) + name, theme.TextPrimary}}, []wtSeg{{right, theme.TextMuted}}, active, focused, w)
}

// renderDetail is the card page of the compact layout: the full card and
// what the keys do.
func (st *wtState) renderDetail(it wtItem, w int, now time.Time) []string {
	fill := theme.Bg(theme.BgSidebar).Render(strings.Repeat(" ", max(w, 0)))
	lines := wtCardLines(it, st.status(it), false, false, max(w, wtCardFullW), now)
	for i := range lines {
		lines[i] = testsPad(lines[i], w)
	}
	lines = append(lines, fill)
	home, _ := os.UserHomeDir()
	lines = append(lines, wtPaint([]wtSeg{{" " + homeShort(it.e.Path, home), theme.TextMuted}}, nil, false, false, w), fill)
	for _, k := range [][2]string{{"Enter", "open it here"}, {"a", "add as a folder"}, {"p", "pin or unpin"}, {"Esc", "back to the list"}} {
		lines = append(lines, wtPaint([]wtSeg{{" " + k[0], theme.TextSecondary}, {strings.Repeat(" ", max(7-len(k[0]), 1)) + k[1], theme.TextMuted}}, nil, false, false, w))
	}
	return lines
}

// footer is two lines: the cursor's folder and the keys.
func (st *wtState) footer(items []wtItem, rows []wtRow, cur, w, h int) []string {
	keys := "Enter open · a add folder · n new · p pin · f filter · ? keys"
	if wtCompact(w, h) {
		keys = "Enter card · n new · ? keys"
		if st.detail {
			keys = "Esc back · ? keys"
		}
	}
	keys = dbFitFooter(theme.ASCIIText(keys), max(w-2, 1))
	if cur < 0 {
		return []string{"", keys}
	}
	home, _ := os.UserHomeDir()
	e := items[rows[cur].item].e
	where := homeShort(e.Path, home)
	if e.Kind == worktrees.KindAgent {
		where = "agent lane " + e.AgentID
	}
	return []string{theme.ASCIIText(where), keys}
}

// cursorIndex is the row of the cursor: the card of st.cursorPath, else
// the first card (-1 when there is none).
func (st *wtState) cursorIndex(items []wtItem, rows []wtRow) int {
	first := -1
	for i, r := range rows {
		if r.item < 0 {
			continue
		}
		if first < 0 {
			first = i
		}
		if items[r.item].e.Path == st.cursorPath {
			return i
		}
	}
	return first
}

// wtShortPath is a worktree folder for a toast.
func wtShortPath(p string) string {
	home, _ := os.UserHomeDir()
	return homeShort(filepath.Clean(p), home)
}

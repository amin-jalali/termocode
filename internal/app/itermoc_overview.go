package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc: the overview ────────────────────────────────────────────────
//
// The list's top row "Overview" shows one calm page:
//
//	✦ AI  profiles, models, services and outside agents     listening auto
//	╭ ✦ PROFILES ─ 1 ╮ ╭ ϟ MODELS ─ 2 ╮ ╭ ⌁ SERVICES ─ 3 ╮ ╭ ⇄ OUTSIDE ─ 4 ╮
//	NEEDS YOU  2 things
//	! linear        sign-in expired · the chat lost 12 tools   ▐ Sign in (i) ▌
//	⚠ house-review  a profile from this project · not trusted  ▐ Trust… (t) ▌
//	ACTIVE FLOW  ✦ review · Review then fix
//	you ──▶ 1 Reviewer ──▶ 2 Fixer ──▶ answer
//
// 1–4 (or a click on a tile) go into that group; Tab picks a "needs you"
// line and Enter (or a click on it) opens its page; its pill runs the fix.

// imNeed is one thing that waits for the user.
type imNeed struct {
	row        imRow
	glyph      string
	tok        theme.Color256
	name, what string
	fix        imPill
}

// needs lists what waits for the user, the most urgent first: services
// (the chat lost their tools), then AIs (signed out, or named by a step
// and not connected), then config.json slots without a key, then project
// profiles not trusted yet. The same rules as the group badges.
func (sn itermocSnap) needs() []imNeed {
	var out []imNeed
	for _, s := range sn.services {
		if !itermocServiceNeeds(s) {
			continue
		}
		g, tok := imGlyph(itermocServiceGlyph(s))
		n := imNeed{row: imRow{kind: imRowService, sec: imSecServices, name: s.Name}, glyph: g, tok: tok, name: s.Name}
		switch {
		case s.State == mcp.StateFailed || s.State == mcp.StateInvalid:
			n.what = "does not start · the chat has none of its tools"
			n.fix = imPill{verb: "log", label: "Log", key: "l", warn: true}
		default:
			n.what = "waits for a sign-in"
			if s.Expired {
				n.what = "sign-in expired"
			}
			if s.Tools > 0 {
				n.what += " · the chat lost " + strconv.Itoa(s.Tools) + " tools"
			}
			n.fix = imPill{verb: "signin", label: "Sign in", key: "i", warn: true}
		}
		out = append(out, n)
	}
	out = append(out, sn.providerNeeds()...)
	for _, s := range sn.userSlots() {
		if s.key != imKeyMissing && s.key != imKeyUnset {
			continue
		}
		g, tok := imGlyph("needs")
		n := imNeed{row: imRow{kind: imRowSlot, sec: imSecModels, name: s.name}, glyph: g, tok: tok, name: s.name,
			what: "no key · this model cannot answer yet", fix: imPill{verb: "setkey", label: "Set key", key: "k", warn: true}}
		if s.provider == "" {
			n.what = "no provider set"
		}
		if users := sn.slotUsers(s.name); len(users) > 0 {
			n.what += " · used by " + strings.Join(users, ", ")
		}
		out = append(out, n)
	}
	for _, p := range sn.profiles {
		if p.trust != aiTrustAsk {
			continue
		}
		g, tok := imGlyph("untrusted")
		out = append(out, imNeed{row: imRow{kind: imRowProfile, sec: imSecProfiles, name: p.p.Name}, glyph: g, tok: tok,
			name: p.p.Name, what: "a profile from this project · not trusted yet",
			fix: imPill{verb: "trust", label: "Trust…", key: "t", warn: true}})
	}
	for i, p := range sn.problems {
		g, tok := imGlyph("needs")
		out = append(out, imNeed{row: imRow{kind: imRowProblem, sec: imSecProfiles, name: strconv.Itoa(i)}, glyph: g, tok: tok,
			name: imProblemTitle(p), what: imProblemWhy(p),
			fix: imPill{verb: "fixfile", label: "Fix", key: "f", warn: true}})
	}
	return out
}

// imTile is one group's tile: a title, one strong value, one state line.
type imTile struct {
	n           int
	icon, title string
	value, line string
	tok         theme.Color256
	warn        bool
}

// imOverview is what the overview page draws.
type imOverview struct {
	tiles     []imTile
	needs     []imNeed
	sel       int // the picked "needs you" line
	flowName  string
	flowTitle string
	steps     []aiPipeStep
	haveFlow  bool
}

func (m Model) overviewPageContent(sn itermocSnap) imContent {
	ov := &imOverview{needs: sn.needs()}
	if p := m.itermocPage; p != nil && len(ov.needs) > 0 {
		ov.sel = min(max(p.sel, 0), len(ov.needs)-1)
	}
	ov.tiles = overviewTiles(sn)
	for _, p := range sn.profiles {
		if p.active {
			ov.flowName, ov.flowTitle = p.p.Name, p.p.Title
			if m.ai != nil {
				ov.steps, ov.haveFlow = m.ai.aiProfileSteps(p.p), true
			}
		}
	}
	right := "not listening"
	if sn.listening {
		right = "listening " + sn.mode
	}
	return imContent{icon: aiSpark(), name: "AI", sub: "agents & connections", right: right, rightTok: theme.TextMuted,
		overview: ov,
		pills: []imPill{{verb: "new", label: "New profile", key: "n"}, {verb: "connect", label: "Connect a service", key: "c"},
			{verb: "start-claude", label: "Start Claude Code", key: "s"}},
		more:       "",
		hint:       "1–4 open a group · Tab pick what needs you · Enter open it · ? keys",
		hintNarrow: "Esc back · 1–4 a group · Enter open · ? keys"}
}

// overviewTiles are the four groups in a few words each. A tile with
// something that needs the user says so in amber.
func overviewTiles(sn itermocSnap) []imTile {
	ok := func(s string) string { g, _ := imGlyph("ready"); return g + " " + s }
	idle := func(s string) string { g, _ := imGlyph("idle"); return g + " " + s }
	need := func(s string) string { return "! " + s }

	prof := imTile{n: 1, icon: imSecIcon(imSecProfiles), title: "PROFILES", value: "no active profile", tok: theme.AccentGreen}
	var untrusted []string
	for _, p := range sn.profiles {
		if p.trust == aiTrustAsk {
			untrusted = append(untrusted, p.p.Name)
		}
		if !p.active {
			continue
		}
		prof.value = p.p.Name
		var names []string
		for _, st := range p.p.Steps {
			t := st.Title
			if t == "" {
				t = st.ID
			}
			names = append(names, t)
		}
		prof.line = ok("active")
		if len(names) > 0 {
			prof.line += " · " + strings.Join(names, " "+imArrow(1)+" ")
		}
	}
	switch len(untrusted) {
	case 0:
	case 1:
		prof.line, prof.tok, prof.warn = need(untrusted[0]+" not trusted yet"), theme.AccentAmber, true
	default:
		prof.line, prof.tok, prof.warn = need(strconv.Itoa(len(untrusted))+" not trusted yet"), theme.AccentAmber, true
	}

	models := imTile{n: 2, icon: imSecIcon(imSecModels), title: "AI PROVIDERS", value: strconv.Itoa(len(sn.providers)) + " AIs",
		line: ok("all ready"), tok: theme.AccentGreen}
	switch len(sn.providers) {
	case 0:
		models.value, models.line, models.tok = "none yet", "+ add one (a)", theme.TextDim
	case 1:
		models.value = "1 AI"
	}
	var waits []string
	for _, n := range sn.providerNeeds() {
		w := n.name + " not connected"
		if n.row.kind == imRowProvider {
			w = n.name + " signed out"
		}
		waits = append(waits, w)
	}
	for _, s := range sn.userSlots() {
		if s.key == imKeyMissing || s.key == imKeyUnset {
			waits = append(waits, s.name+" has no key")
		}
	}
	switch len(waits) {
	case 0:
	case 1:
		models.line, models.tok, models.warn = need(waits[0]), theme.AccentAmber, true
	default:
		models.line, models.tok, models.warn = need(strconv.Itoa(len(waits))+" need you"), theme.AccentAmber, true
	}

	svc := imTile{n: 3, icon: imSecIcon(imSecServices), title: "SERVICES", value: "none yet",
		line: "+ connect one (c)", tok: theme.TextDim}
	if n := len(sn.services); n > 0 {
		ready := 0
		var waits []mcp.Status
		for _, s := range sn.services {
			if s.State == mcp.StateRunning {
				ready++
			}
			if itermocServiceNeeds(s) {
				waits = append(waits, s)
			}
		}
		svc.value = strconv.Itoa(ready) + " of " + strconv.Itoa(n) + " ready"
		svc.line, svc.tok = ok("all ready"), theme.AccentGreen
		if ready < n {
			svc.line, svc.tok = idle(strconv.Itoa(n-ready)+" off"), theme.TextMuted
		}
		switch {
		case len(waits) == 1 && (waits[0].State == mcp.StateFailed || waits[0].State == mcp.StateInvalid):
			svc.line, svc.tok, svc.warn = need(waits[0].Name+" does not start"), theme.AccentAmber, true
		case len(waits) == 1:
			svc.line, svc.tok, svc.warn = need(waits[0].Name+" needs sign-in"), theme.AccentAmber, true
		case len(waits) > 1:
			svc.line, svc.tok, svc.warn = need(strconv.Itoa(len(waits))+" need you"), theme.AccentAmber, true
		}
	}

	ag := imTile{n: 4, icon: imSecIcon(imSecAgents), title: "OUTSIDE AGENTS", value: strconv.Itoa(len(sn.agents)) + " connected",
		line: idle("not listening"), tok: theme.TextMuted}
	switch {
	case len(sn.agents) > 0:
		a := sn.agents[0]
		ag.line, ag.tok = ok(a.name+" · "+callsText(a.calls)), theme.AccentGreen
	case sn.listening:
		ag.line = idle("listening · none yet")
	}
	return []imTile{prof, models, svc, ag}
}

// itermocTileRow is the row a tile (1–4) goes to: the active profile, the
// first model or service that needs the user (else the first one), the
// first outside agent (else the listening row). ok=false: nothing there.
func (sn itermocSnap) tileRow(n int) (imRow, bool) {
	switch n {
	case 1:
		for _, p := range sn.profiles {
			if p.active {
				return imRow{kind: imRowProfile, sec: imSecProfiles, name: p.p.Name}, true
			}
		}
		if len(sn.profiles) > 0 {
			return imRow{kind: imRowProfile, sec: imSecProfiles, name: sn.profiles[0].p.Name}, true
		}
	case 2:
		for _, n := range sn.providerNeeds() {
			if n.row.kind == imRowProvider {
				return n.row, true
			}
		}
		user := sn.userSlots()
		for _, s := range user {
			if s.key == imKeyMissing || s.key == imKeyUnset {
				return imRow{kind: imRowSlot, sec: imSecModels, name: s.name}, true
			}
		}
		if len(sn.providers) > 0 {
			return imRow{kind: imRowProvider, sec: imSecModels, name: itermocProvName(sn.providers[0].kind)}, true
		}
		if len(user) > 0 {
			return imRow{kind: imRowSlot, sec: imSecModels, name: user[0].name}, true
		}
	case 3:
		for _, s := range sn.services {
			if itermocServiceNeeds(s) {
				return imRow{kind: imRowService, sec: imSecServices, name: s.Name}, true
			}
		}
		if len(sn.services) > 0 {
			return imRow{kind: imRowService, sec: imSecServices, name: sn.services[0].Name}, true
		}
	case 4:
		if len(sn.agents) > 0 {
			return imRow{kind: imRowAgent, sec: imSecAgents, name: agentKey(sn.agents[0].id)}, true
		}
		return imRow{kind: imRowAgentStatus, sec: imSecAgents, name: "status"}, true
	}
	return imRow{}, false
}

// itermocJumpTo opens the page of row from the overview: the list's cursor
// moves onto it (its group unfolds) and the page takes the keys.
func (m *Model) itermocJumpTo(row imRow) tea.Cmd {
	st := m.itermocS()
	st.filter, st.typing = "", false
	st.collapse[row.sec] = false
	st.cursorTo(row)
	if row.kind == imRowProblem {
		return m.itermocRun("fixfile", row) // no page: the fix is the file
	}
	return m.itermocShowPage(row, true)
}

// itermocOverviewVerb runs a target of the overview page. ok=false: not one.
func (m *Model) itermocOverviewVerb(verb string) (tea.Cmd, bool) {
	sn := m.itermocS().snap
	switch {
	case strings.HasPrefix(verb, "tile:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(verb, "tile:"))
		if row, ok := sn.tileRow(n); ok {
			return m.itermocJumpTo(row), true
		}
		switch n {
		case 2:
			m.itermocS().focusSection(imSecModels)
		case 3:
			m.itermocS().focusSection(imSecServices)
		}
		return nil, true
	case strings.HasPrefix(verb, "jump:"), strings.HasPrefix(verb, "fix:"):
		kind, idx, _ := strings.Cut(verb, ":")
		i, err := strconv.Atoi(idx)
		needs := sn.needs()
		if err != nil || i < 0 || i >= len(needs) {
			return nil, true
		}
		if m.itermocPage != nil {
			m.itermocPage.sel = i
		}
		if kind == "jump" {
			return m.itermocJumpTo(needs[i].row), true
		}
		return m.itermocRun(needs[i].fix.verb, needs[i].row), true
	case verb == "connect":
		return m.itermocButton(imSecServices, "connect"), true
	case strings.HasPrefix(verb, "step:"):
		// A step box of the active flow: that profile's step form.
		for _, p := range sn.profiles {
			if p.active {
				return m.itermocPageVerb(verb, imRow{kind: imRowProfile, sec: imSecProfiles, name: p.p.Name}), true
			}
		}
		return nil, true
	}
	return nil, false
}

// itermocOverviewKey runs a key on the overview page. ok=false: not ours.
func (m *Model) itermocOverviewKey(key string) (tea.Cmd, bool) {
	p := m.itermocPage
	n := len(m.itermocS().snap.needs())
	switch key {
	case "1", "2", "3", "4":
		return m.itermocOverviewVerb("tile:" + key)
	case "tab":
		if n > 0 {
			p.sel = (min(max(p.sel, 0), n-1) + 1) % n
		}
		return nil, true
	case "shift+tab":
		if n > 0 {
			p.sel = (min(max(p.sel, 0), n-1) + n - 1) % n
		}
		return nil, true
	case "enter":
		if n > 0 {
			return m.itermocOverviewVerb("jump:" + strconv.Itoa(min(max(p.sel, 0), n-1)))
		}
		return nil, true
	}
	return nil, false
}

// ── Drawing ──────────────────────────────────────────────────────────────

// drawOverviewBody draws the tiles, NEEDS YOU and ACTIVE FLOW from row y.
func (d *imDraw) drawOverviewBody(y int, ov *imOverview, h int) int {
	y = d.tiles(y, ov.tiles, h) + d.gap()
	y = d.needsList(y, ov) + d.gap()
	if !ov.haveFlow {
		return y
	}
	cv := d.cv
	x := cv.text(d.mx, y, "ACTIVE FLOW", imTokMuted().B())
	flow := aiSpark() + " " + ov.flowName
	if ov.flowTitle != "" {
		flow += " · " + ov.flowTitle
	}
	cv.text(x+2, y, imTrunc(theme.ASCIIText(flow), max(d.w-d.mx-x-2, 4)), imTokAccent())
	y++
	if d.roomy {
		y++
	}
	return d.pipeline(y, ov.steps, h)
}

// tiles draws the four tiles: one row when they fit, else two by two; a
// narrow or short pane (h, like the pipeline's chips) gets one line per
// group instead of boxes. Returns the row after them.
func (d *imDraw) tiles(y int, tiles []imTile, h int) int {
	contentW := d.w - 2*d.mx
	per, gapX := 4, 2
	switch {
	case contentW >= 4*24+3*gapX:
	case contentW >= 2*22+gapX:
		per = 2
	default:
		per = 1
	}
	if per == 1 || h < 26 {
		return d.tileLines(y, tiles)
	}
	tw := min((contentW-(per-1)*gapX)/per, 44)
	const th = 4
	rowGap := boolInt(d.roomy)
	rows := (len(tiles) + per - 1) / per
	for i, t := range tiles {
		x := d.mx + (i%per)*(tw+gapX)
		ty := y + (i/per)*(th+rowGap)
		border := imFg(theme.BorderDefault)
		if t.warn {
			border = imTokWarn()
		}
		d.cv.box(x, ty, tw, th, border)
		num := " " + strconv.Itoa(t.n) + " "
		head := imTrunc(t.title, max(tw-6-len(num)-3, 3))
		hx := d.cv.text(x+2, ty, " ", border)
		hx = d.cv.text(hx, ty, t.icon+" ", imTokAccent())
		d.cv.text(hx, ty, head+" ", imTokMuted().B())
		d.cv.textR(x+tw-2, ty, num, imTokDim())
		d.cv.text(x+2, ty+1, imTrunc(theme.ASCIIText(t.value), tw-4), imTokPrimary().B())
		d.cv.text(x+2, ty+2, imTrunc(theme.ASCIIText(t.line), tw-4), imFg(t.tok))
		d.hit("tile:"+strconv.Itoa(t.n), x, ty, tw, th)
	}
	return y + rows*th + (rows-1)*rowGap
}

// tileLines is the tiles as one line each: "1 ✦ PROFILES  review · ● active".
func (d *imDraw) tileLines(y int, tiles []imTile) int {
	cv, mx := d.cv, d.mx
	titleW, valueW := 0, 0
	for _, t := range tiles {
		titleW = max(titleW, lenR(t.title))
		valueW = max(valueW, min(lenR(t.value), 16))
	}
	for _, t := range tiles {
		x := cv.text(mx, y, strconv.Itoa(t.n), imTokDim())
		x = cv.text(x+1, y, t.icon, imTokAccent())
		cv.text(x+1, y, t.title, imTokMuted().B())
		x += 1 + titleW + 2
		if room := d.w - mx - x; room >= 4 {
			cv.text(x, y, imTrunc(theme.ASCIIText(t.value), min(room, valueW)), imTokPrimary().B())
		}
		x += valueW + 2
		if room := d.w - mx - x; room >= 6 {
			cv.text(x, y, imTrunc(theme.ASCIIText(t.line), room), imFg(t.tok))
		}
		d.hit("tile:"+strconv.Itoa(t.n), mx, y, d.w-2*mx, 1)
		y++
	}
	return y
}

// needsList draws NEEDS YOU: one line per item (glyph, name, what it
// costs) with its fix pill in one column. Returns the row after it.
func (d *imDraw) needsList(y int, ov *imOverview) int {
	cv, mx := d.cv, d.mx
	contentW := d.w - 2*mx
	if len(ov.needs) == 0 {
		x := cv.text(mx, y, "NEEDS YOU", imTokMuted().B())
		cv.text(x+2, y, "nothing right now", imTokDim())
		return y + 1
	}
	x := cv.text(mx, y, "NEEDS YOU", imTokWarn().B())
	things := strconv.Itoa(len(ov.needs)) + " things"
	if len(ov.needs) == 1 {
		things = "1 thing"
	}
	cv.text(x+2, y, things, imTokDim())
	y++
	if d.roomy {
		y++
	}
	nameW, whatW, pillW := 6, 0, 0
	for _, n := range ov.needs {
		nameW = max(nameW, min(lenR(n.name), 18))
		whatW = max(whatW, lenR(theme.ASCIIText(n.what)))
		pillW = max(pillW, imPillW(n.fix.text()))
	}
	whatX := mx + 2 + nameW + 2
	pillX := whatX + whatW + 3
	if pillX+pillW > mx+contentW {
		pillX = mx + contentW - pillW
	}
	whatRoom := pillX - 3 - whatX
	for i, n := range ov.needs {
		sel := i == ov.sel
		if sel && mx >= 2 {
			cv.text(mx-2, y, imText("▸"), imTokAccent())
		}
		cv.text(mx, y, n.glyph, imFg(n.tok))
		nameTok := imTokPrimary().B()
		if sel {
			nameTok = imTokAccent().B()
		}
		cv.text(mx+2, y, imTrunc(n.name, nameW), nameTok)
		if whatRoom >= 6 {
			cv.text(whatX, y, imTrunc(theme.ASCIIText(n.what), whatRoom), imTokMuted())
		}
		jumpW := pillX - mx
		if pillX >= mx+2+nameW+1 {
			cv.pill(pillX, y, n.fix.text(), theme.BgSectionHdr, theme.AccentAmber, sel)
			d.hit("fix:"+strconv.Itoa(i), pillX, y, imPillW(n.fix.text()), 1)
		} else {
			jumpW = contentW
		}
		d.hit("jump:"+strconv.Itoa(i), mx, y, max(jumpW-1, 1), 1)
		y++
	}
	return y
}

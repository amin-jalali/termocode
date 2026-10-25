package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc: drawing the detail page ─────────────────────────────────────
//
//	row 0   ✦ review  Review then fix                  ● active in the chat
//	row 1   ─────────────────────────────────────────────────────────────
//	        the pipeline: boxes with arrows (chips when the pane is narrow)
//	        STEPS · SETTINGS · last-run line (a profile) or the facts (others)
//	h-4     ─────────────────────────────────────────────────────────────
//	h-3     Use (u)  Edit (e)  Runs (r)   ⋯ more (Enter): Duplicate · …
//	h-1     the key hints
//
// The body is drawn with roomy spacing first; when it does not fit it is
// drawn again tight, and what still does not fit scrolls (j, the wheel).

// imPill is one button of the bar at the bottom.
type imPill struct {
	verb, label, key string
	primary, warn    bool
}

func (p imPill) text() string { return " " + imText(p.label) + " (" + p.key + ") " }

// imPageHit is a click target in pane-local cells.
type imPageHit struct {
	id         string
	x, y, w, h int
}

// imCard is the amber "needs you" box at the top of a page.
type imCard struct {
	title string
	lines []string
}

// imFactSec is a titled block of label / value rows (or plain lines).
type imFactSec struct {
	title string
	rows  [][2]string
	toks  []theme.Color256 // value colours (nil = secondary)
	lines []string
}

// imContent is everything one page shows.
type imContent struct {
	icon, name, sub string
	right           string
	rightTok        theme.Color256
	card            *imCard
	facts           []imFactSec
	profile         *imProfileView // set for a profile page
	pills           []imPill
	more            string // "Duplicate · Edit JSON…": what the menu holds
	hint            string
	hintNarrow      string // for a pane under 70 cells
}

// imPageLayout is a drawn page: one string per pane row and the targets.
type imPageLayout struct {
	rows []string
	hits []imPageHit
}

// imDraw is the state of one drawing pass.
type imDraw struct {
	cv    *imCanvas
	w, mx int
	roomy bool
	hits  []imPageHit
}

func (d *imDraw) hit(id string, x, y, w, h int) {
	d.hits = append(d.hits, imPageHit{id: "im:pg:" + id, x: x, y: y, w: w, h: h})
}

// The text colours are read when used: a theme change must reach the page.
func imTokPrimary() imSt   { return imFg(theme.TextPrimary) }
func imTokSecondary() imSt { return imFg(theme.TextSecondary) }
func imTokMuted() imSt     { return imFg(theme.TextMuted) }
func imTokDim() imSt       { return imFg(theme.TextDim) }
func imTokAccent() imSt    { return imFg(theme.AccentLavender) }
func imTokOK() imSt        { return imFg(theme.AccentGreen) }
func imTokWarn() imSt      { return imFg(theme.AccentAmber) }
func imTokErr() imSt       { return imFg(theme.AccentRedCoral) }

func imQuote(s string) string {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return "\"" + s + "\""
	}
	return "“" + s + "”"
}

func imArrow(n int) string {
	head := "▶"
	line := "─"
	if theme.CurrentIconMode == theme.IconModeASCII {
		head, line = ">", "-"
	}
	return strings.Repeat(line, max(n-1, 0)) + head
}

// head draws a section title and returns the next row.
func (d *imDraw) head(y int, title, right string) int {
	d.cv.text(d.mx, y, title, imTokMuted().B())
	if right != "" {
		d.cv.textR(d.w-d.mx, y, theme.ASCIIText(right), imTokDim())
	}
	if d.roomy {
		return y + 2
	}
	return y + 1
}

// kv draws one label / value row.
func (d *imDraw) kv(y int, label, value string, tok imSt) {
	d.cv.text(d.mx, y, label, imTokMuted())
	d.cv.text(d.mx+14, y, imTrunc(theme.ASCIIText(value), max(d.w-d.mx-14-d.mx, 4)), tok)
}

// gap is the blank rows between two sections.
func (d *imDraw) gap() int {
	if d.roomy {
		return 2
	}
	return 1
}

// facts draws the generic blocks of a page and returns the next row.
func (d *imDraw) facts(y int, secs []imFactSec) int {
	for i, s := range secs {
		if i > 0 {
			y += d.gap()
		}
		y = d.head(y, s.title, "")
		for j, r := range s.rows {
			tok := imTokSecondary()
			if j < len(s.toks) && s.toks[j] != 0 {
				tok = imFg(s.toks[j])
			}
			d.kv(y, r[0], r[1], tok)
			y++
		}
		for _, l := range s.lines {
			d.cv.text(d.mx, y, imTrunc(theme.ASCIIText(l), max(d.w-2*d.mx, 4)), imTokSecondary())
			y++
		}
	}
	return y
}

// card draws the amber box; returns the next row.
func (d *imDraw) card(y int, c *imCard) int {
	cw := min(d.w-2*d.mx, 76)
	h := len(c.lines) + 3
	d.cv.box(d.mx, y, cw, h, imTokWarn())
	d.cv.text(d.mx+2, y, " "+imText(c.title)+" ", imTokWarn().B())
	for i, l := range c.lines {
		d.cv.text(d.mx+3, y+1+i, imTrunc(theme.ASCIIText(l), cw-6), imTokPrimary())
	}
	return y + h + d.gap() - 1
}

// ── The layout ───────────────────────────────────────────────────────────

func (m Model) itermocPageLayout(w, h int) imPageLayout {
	if w < 8 || h < 6 {
		return imPageLayout{rows: newImCanvas(max(w, 1), max(h, 1), theme.BgEditor).rows(0, max(h, 1))}
	}
	ct := m.itermocPageContent()
	pg := m.itermocPage
	mx := 4
	if w < 100 {
		mx = 2
	}
	if w < 40 {
		mx = 1
	}
	barH := 4 // hairline, pills, blank, hint
	bodyTop, bodyBot := 2, h-barH
	if h < 14 {
		bodyBot = h - 2
	}
	avail := bodyBot - bodyTop

	// Draw the body roomy; when it is too tall draw it tight.
	draw := func(roomy bool) (*imDraw, int) {
		d := &imDraw{cv: newImCanvas(w, 260, theme.BgEditor), w: w, mx: mx, roomy: roomy}
		y := bodyTop + 2
		if !roomy {
			y = bodyTop + 1
		}
		if ct.card != nil {
			y = d.card(y, ct.card)
		}
		if ct.profile != nil {
			y = m.drawProfileBody(d, y, ct.profile, h)
		} else {
			y = d.facts(y, ct.facts)
		}
		return d, y
	}
	d, end := draw(true)
	if end-bodyTop > avail {
		d, end = draw(false)
	}
	scroll := 0
	if pg != nil {
		pg.maxScroll = max(end-bodyTop-avail, 0)
		pg.scroll = min(max(pg.scroll, 0), pg.maxScroll)
		scroll = pg.scroll
	}

	out := newImCanvas(w, h, theme.BgEditor)
	for y := 0; y < avail; y++ {
		if src := bodyTop + y + scroll; src < d.cv.h {
			copy(out.cells[bodyTop+y], d.cv.cells[src])
		}
	}
	var hits []imPageHit
	for _, ht := range d.hits {
		ht.y -= scroll
		if ht.y >= bodyTop && ht.y+ht.h <= bodyBot {
			hits = append(hits, ht)
		}
	}
	// A hint at the edges when more is above or below.
	if scroll > 0 {
		out.textR(w-mx, bodyTop, imText("▴ more"), imTokDim())
	}
	if scroll < pagemax(pg) {
		out.textR(w-mx, bodyBot-1, imText("▾ more (j)"), imTokDim())
	}

	// Header.
	x := out.text(mx-1, 0, ct.icon+" "+ct.name, imTokAccent().B())
	if ct.sub != "" {
		out.text(x+2, 0, imTrunc(theme.ASCIIText(ct.sub), max(w-x-2-mx-lenR(ct.right)-3, 6)), imTokSecondary())
	}
	if ct.right != "" {
		out.textR(w-mx+1, 0, theme.ASCIIText(ct.right), imFg(ct.rightTok))
	}
	out.hline(mx-1, 1, w-2*mx+2, imHair(), imFg(theme.BorderSubtle))

	// The pill bar and the hints.
	if h >= 14 {
		by := h - 3
		out.hline(mx-1, by-1, w-2*mx+2, imHair(), imFg(theme.BorderSubtle))
		px := mx
		for _, p := range ct.pills {
			chip, fg := theme.BgSectionHdr, theme.TextPrimary
			switch {
			case p.warn:
				chip, fg = theme.AccentAmber, theme.BgEditor
			case p.primary:
				chip, fg = theme.AccentLavender, theme.BgEditor
			}
			pw := imPillW(p.text())
			if px+pw > w-mx {
				break
			}
			out.pill(px, by, p.text(), chip, fg, p.primary || p.warn)
			hits = append(hits, imPageHit{id: "im:pg:" + p.verb, x: px, y: by, w: pw, h: 1})
			px += pw + 1
		}
		if ct.more != "" && w-px-mx > 6 {
			mt := imText("⋯") + " more (Enter)"
			if ct.more != "-" {
				mt += ": " + ct.more
			}
			mt = imTrunc(theme.ASCIIText(mt), w-px-mx-2)
			out.text(px+2, by, mt, imTokMuted())
			hits = append(hits, imPageHit{id: "im:pg:more", x: px + 2, y: by, w: runewidth.StringWidth(mt), h: 1})
		}
		hint := ct.hint
		if w < 70 {
			hint = ct.hintNarrow
			if hint == "" {
				hint = "Esc back to list · ? keys"
			}
		}
		hint = imTrunc(theme.ASCIIText(hint), w-2*mx)
		out.text(mx-1, h-1, hint, imTokMuted())
	}
	return imPageLayout{rows: out.rows(0, h), hits: hits}
}

func pagemax(p *itermocPageState) int {
	if p == nil {
		return 0
	}
	return p.maxScroll
}

func lenR(s string) int { return runewidth.StringWidth(s) }

func imHair() rune {
	if theme.CurrentIconMode == theme.IconModeASCII {
		return '-'
	}
	return '─'
}

// renderItermocPage is the editor area while the page is shown.
func (m Model) renderItermocPage(w, h int) string {
	return strings.Join(m.itermocPageLayout(w, h).rows, "\n")
}

// itermocPageTargets are the page's click targets in screen cells.
func (m Model) itermocPageTargets(pane editorPane) []hitTarget {
	var out []hitTarget
	for _, h := range m.itermocPageLayout(pane.W, pane.H).hits {
		out = append(out, hitTarget{ID: h.id, X: pane.X + h.x, Y: pane.Top + h.y, W: h.w, H: h.h})
	}
	return out
}

// ── What each page says ──────────────────────────────────────────────────

func (m Model) itermocPageContent() imContent {
	row, have := m.itermocPageRow()
	sn := m.itermocSnapOf()
	icon := aiSpark()
	if !have {
		return imContent{icon: icon, name: "Itermoc", sub: "nothing selected",
			facts: []imFactSec{{title: "NOTHING HERE YET", lines: []string{
				"Pick a profile, model, service or outside agent in the list.",
				"The page of the row you pick shows here; it is the same tab.",
			}}},
			pills: []imPill{{verb: "new", label: "New profile", key: "n", primary: true}},
			more:  "-", hint: "↑↓ pick in the list · n new profile · ? keys"}
	}
	switch row.kind {
	case imRowProfile:
		return m.profilePageContent(sn, row)
	case imRowSlot:
		return slotPageContent(sn, row)
	case imRowService:
		return servicePageContent(sn, row)
	case imRowAgent, imRowAgentStatus:
		return agentPageContent(sn, row)
	}
	return overviewPageContent(sn)
}

const imHintList = "↑↓ pick in the list · Enter more actions · ? keys"

func overviewPageContent(sn itermocSnap) imContent {
	c := imContent{icon: aiSpark(), name: "AI", sub: "profiles, models, services and outside agents",
		more: "-", hint: "↑↓ pick in the list · Space fold · ? keys"}
	prof := imFactSec{title: "SUMMARY"}
	active := ""
	for _, p := range sn.profiles {
		if p.active {
			active = p.p.Name
		}
	}
	prof.rows = [][2]string{
		{"profiles", strconv.Itoa(len(sn.profiles)) + " · active: " + active},
		{"models", strconv.Itoa(len(sn.slots)) + " slots"},
		{"services", strconv.Itoa(len(sn.services))},
		{"outside agents", strconv.Itoa(len(sn.agents)) + " connected"},
	}
	c.facts = []imFactSec{prof}
	var needs []string
	for _, p := range sn.profiles {
		if p.trust == aiTrustAsk {
			needs = append(needs, p.p.Name+" asks for your trust (t)")
		}
	}
	for _, s := range sn.slots {
		if s.key == imKeyMissing || s.key == imKeyUnset {
			needs = append(needs, "model "+s.name+" has no key (k)")
		}
	}
	for _, s := range sn.services {
		if itermocServiceNeeds(s) {
			needs = append(needs, s.Name+": "+itermocServiceWordText(s)+" (i)")
		}
	}
	if len(needs) > 0 {
		c.facts = append(c.facts, imFactSec{title: "NEEDS YOU", lines: needs})
	}
	return c
}

func itermocServiceWordText(s mcp.Status) string {
	w, _ := itermocServiceWord(s)
	return w
}

// slotPageContent is the model slot page: provider, model, key state (never
// the key), and the profiles that use it.
func slotPageContent(sn itermocSnap, row imRow) imContent {
	s, _ := sn.slot(row.name)
	c := imContent{icon: imIconModels.String(), name: s.name, hint: imHintList}
	c.sub = "model slot"
	word, wtok := "● ready", theme.AccentGreen
	switch {
	case s.provider == "":
		word, wtok = "! not set", theme.AccentAmber
	case s.key == imKeyMissing || s.key == imKeyUnset:
		word, wtok = "! needs a key", theme.AccentAmber
	}
	c.right, c.rightTok = imGlyphWord(word), wtok
	if s.provider == "" || s.key == imKeyMissing || s.key == imKeyUnset {
		c.card = &imCard{title: "! " + imKeyNeed(s), lines: []string{
			"This slot cannot answer yet.",
			"Set the key; it goes to the secret store and is never shown.",
		}}
	}
	prov := s.provider
	if prov == "" {
		prov = "not set"
	}
	model := s.model
	if model == "" {
		model = "not set"
	}
	keyTok := theme.AccentGreen
	if s.key == imKeyMissing || s.key == imKeyUnset {
		keyTok = theme.AccentAmber
	}
	users := sn.slotUsers(s.name)
	usedBy := "no profile"
	if len(users) > 0 {
		usedBy = strings.Join(users, ", ")
	}
	where := "built in"
	if s.user {
		where = "config.json (ai_models)"
	}
	c.facts = []imFactSec{{title: "SETTINGS",
		rows: [][2]string{{"provider", prov}, {"model", model}, {"key", imKeyText(s.key)}, {"used by", usedBy}, {"source", where}},
		toks: []theme.Color256{0, 0, keyTok, 0, 0}}}
	if s.key == imKeyMissing || s.key == imKeyUnset {
		c.pills = append(c.pills, imPill{verb: "setkey", label: "Set key", key: "k", warn: true})
	}
	c.pills = append(c.pills, imPill{verb: "edit", label: "Edit…", key: "e", primary: s.key != imKeyMissing && s.key != imKeyUnset})
	c.more = "Edit JSON" + map[bool]string{true: " · Remove…", false: ""}[s.user]
	return c
}

func imKeyNeed(s imSlot) string {
	if s.provider == "" {
		return "This slot has no provider"
	}
	return "This slot has no key"
}

func imGlyphWord(s string) string { return strings.Replace(s, "●", imGlyphReady(), 1) }

func imGlyphReady() string { g, _ := imGlyph("ready"); return g }

// servicePageContent is the service page: what state it is in and what
// that costs; the sign-in card when it waits for the user.
func servicePageContent(sn itermocSnap, row imRow) imContent {
	s, _ := sn.service(row.name)
	c := imContent{icon: imIconServices.String(), name: s.Name, hint: imHintList}
	if s.Type == mcp.TypeStdio {
		c.sub = "local service · MCP"
	} else {
		c.sub = "remote service · MCP"
	}
	word, tok := itermocServiceWord(s)
	g, _ := imGlyph(itermocServiceGlyph(s))
	c.right = g + " " + word
	c.rightTok = tok
	if itermocServiceNeeds(s) {
		c.rightTok = theme.AccentAmber
	} else if s.State == mcp.StateRunning {
		c.rightTok = theme.AccentGreen
	} else {
		c.rightTok = theme.TextMuted
	}
	if s.Expired || s.State == mcp.StateSignIn {
		lines := []string{s.Name + " waits for you to sign in."}
		if s.Tools > 0 {
			lines = append(lines, "The chat lost "+strconv.Itoa(s.Tools)+" tools. Sign in again to get them back.")
		} else {
			lines = append(lines, "Until then the chat cannot use it.")
		}
		c.card = &imCard{title: "! Sign-in needed", lines: lines}
	}
	trusted := "yes"
	if !s.Trusted {
		trusted = "not yet"
	}
	kind := "remote"
	if s.Type == mcp.TypeStdio {
		kind = "local"
	}
	rows := [][2]string{{"state", s.State}, {"where", kind}, {"tools", strconv.Itoa(s.Tools)}, {"trusted", trusted}}
	if s.SignedIn {
		rows = append(rows, [2]string{"sign-in", "stored"})
	}
	if s.Problem != "" {
		rows = append(rows, [2]string{"problem", s.Problem})
	}
	c.facts = []imFactSec{{title: "STATUS", rows: rows}}
	if s.Expired || s.State == mcp.StateSignIn {
		c.pills = append(c.pills, imPill{verb: "signin", label: "Sign in", key: "i", warn: true})
	}
	c.pills = append(c.pills, imPill{verb: "log", label: "Log", key: "l", primary: !(s.Expired || s.State == mcp.StateSignIn)})
	c.more = "tools · writes · disable · remove"
	return c
}

// agentPageContent is the outside-agents page (the listening status, or one
// connected agent).
func agentPageContent(sn itermocSnap, row imRow) imContent {
	c := imContent{icon: imIconAgents.String(), name: "Outside agents", sub: "agents that use termocode over MCP", hint: imHintList}
	mark, _ := imGlyph("ready")
	if sn.listening {
		c.right, c.rightTok = mark+" listening · "+sn.mode, theme.AccentGreen
	} else {
		g, _ := imGlyph("idle")
		c.right, c.rightTok = g+" not listening", theme.TextMuted
	}
	if row.kind == imRowAgent {
		if a, ok := sn.agent(row.name); ok {
			c.name = a.name
			c.sub = "outside agent"
			c.facts = []imFactSec{{title: "CONNECTION",
				rows: [][2]string{{"since", a.since}, {"calls", strconv.Itoa(a.calls)}, {"asked you", strconv.Itoa(a.asked)}}}}
		}
	} else {
		rows := [][2]string{{"mode", sn.mode}}
		if sn.listening {
			rows = append(rows, [2]string{"agents", strconv.Itoa(len(sn.agents)) + " connected"})
		} else {
			rows = append(rows, [2]string{"why", sn.whySentence})
		}
		c.facts = []imFactSec{{title: "STATUS", rows: rows}}
	}
	if len(sn.calls) > 0 {
		var lines []string
		for _, l := range sn.calls {
			lines = append(lines, string(l))
		}
		c.facts = append(c.facts, imFactSec{title: "LAST CALLS", lines: lines})
	}
	c.pills = []imPill{
		{verb: "start-claude", label: "Start Claude Code", key: "s", primary: true},
		{verb: "copy", label: "Copy config", key: "y"},
		{verb: "status", label: "Status", key: "T"},
	}
	c.more = "Start Codex · Allowed tools · Disconnect…"
	return c
}

// ── The profile page ─────────────────────────────────────────────────────

// imProfileView is what the profile page draws.
type imProfileView struct {
	p       imProfile
	steps   []aiPipeStep
	waits   []string // "waits for 1" per step ("" = none)
	note    string
	cats    string
	budget  string
	source  string
	lastRun string // "Last run 12 min ago" or "Not run yet"
	hasRun  bool
}

func (m Model) profilePageContent(sn itermocSnap, row imRow) imContent {
	p, _ := sn.profile(row.name)
	c := imContent{icon: aiSpark(), name: p.p.Name, sub: p.p.Title, hint: "↑↓ pick in the list · u use · e edit · Enter more actions · ? keys", hintNarrow: "Esc back · u use · e edit · ? keys"}
	g, _ := imGlyph("ready")
	switch {
	case p.trust == aiTrustAsk:
		ug, _ := imGlyph("untrusted")
		c.right, c.rightTok = ug+" asks before it is used", theme.AccentAmber
	case p.active:
		c.right, c.rightTok = g+" active in the chat", theme.AccentGreen
	default:
		ig, _ := imGlyph("idle")
		c.right, c.rightTok = ig+" not the active profile", theme.TextMuted
	}
	if m.ai == nil {
		c.facts = []imFactSec{{title: "STEPS", lines: []string{"AI is off."}}}
		return c
	}
	pv := &imProfileView{p: p, steps: m.ai.aiProfileSteps(p.p)}
	pv.waits = make([]string, len(pv.steps))
	graph := aiProfileIsGraph(p.p)
	ids := map[string]int{}
	for i, st := range pv.steps {
		ids[st.step.ID] = i + 1
	}
	for i, st := range pv.steps {
		switch {
		case graph && len(st.step.Deps) > 0:
			var ns []string
			for _, d := range st.step.Deps {
				if n, ok := ids[d]; ok {
					ns = append(ns, strconv.Itoa(n))
				}
			}
			if len(ns) > 0 {
				pv.waits[i] = "waits for " + strings.Join(ns, ", ")
			}
		case !graph && i > 0:
			pv.waits[i] = "waits for " + strconv.Itoa(i)
		}
	}
	pv.note = m.profileNote(sn, p.p, pv, graph)
	cs := p.p.CategorySlots()
	pv.cats = agentdag.CategoryQuick + " " + imArrow(1) + " " + cs[agentdag.CategoryQuick] + "     " +
		agentdag.CategoryDeep + " " + imArrow(1) + " " + cs[agentdag.CategoryDeep]
	pv.budget = "none"
	if p.p.BudgetUSD > 0 {
		pv.budget = aiBudgetText(p.p.BudgetUSD) + " per run"
	}
	switch p.p.Source {
	case ai.ProfileSourceUser:
		pv.source = "profiles.json · yours"
	case ai.ProfileSourceProject:
		pv.source = ".termocode/ai-profiles.json · this project"
	default:
		pv.source = "built in"
	}
	pv.lastRun, pv.hasRun = m.profileLastRun(p.p.Name)
	c.profile = pv

	if p.trust == aiTrustAsk {
		c.pills = append(c.pills, imPill{verb: "trust", label: "Trust…", key: "t", warn: true})
	}
	c.pills = append(c.pills,
		imPill{verb: "use", label: "Use", key: "u", primary: p.trust != aiTrustAsk},
		imPill{verb: "edit", label: "Edit", key: "e"},
		imPill{verb: "runs", label: "Runs", key: "r"})
	var more []string
	_, acts := m.itermocActionsForPage(row)
	for _, a := range acts {
		switch a.id {
		case "use", "runs", "trust":
			continue
		}
		more = append(more, imText(a.title))
	}
	c.more = strings.Join(more, " · ")
	return c
}

// itermocActionsForPage is the verbs of the actions menu without opening it.
func (m Model) itermocActionsForPage(row imRow) (string, []itermocAction) {
	mm := m
	return mm.itermocActionsFor(row)
}

// profileNote is the one line under the pipeline.
func (m Model) profileNote(sn itermocSnap, p ai.Profile, pv *imProfileView, graph bool) string {
	var parts []string
	switch {
	case p.Planned():
		parts = append(parts, "the planner decides the other steps")
	case len(pv.steps) <= 1:
		parts = append(parts, "one step")
	case graph:
		parts = append(parts, "steps with no dependency run side by side")
	case len(pv.steps) == 2:
		parts = append(parts, "step 2 waits for step 1")
	default:
		parts = append(parts, "each step waits for the one before it")
	}
	var svc []string
	for _, s := range sn.services {
		if s.State == mcp.StateRunning && s.Trusted {
			svc = append(svc, s.Name)
		}
	}
	if len(svc) > 0 {
		if len(svc) > 3 {
			svc = append(svc[:3], "…")
		}
		parts = append(parts, "the chat can reach "+strings.Join(svc, ", ")+" while it runs")
	}
	return strings.Join(parts, " · ")
}

// profileLastRun is the one quiet line about the newest run of the profile
// in the chat: "Last run 12 min ago", or "Not run yet". Runs and their cost
// belong to AGENTS and AI: Usage, not to this setup page.
func (m Model) profileLastRun(name string) (line string, ok bool) {
	if m.ai == nil {
		return "Not run yet", false
	}
	msgs := m.ai.conv.Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		r := msgs[i].Run
		if r == nil || r.Profile != name || msgs[i].Role != ai.StoredAssistant {
			continue
		}
		if r.StartedAtMs <= 0 {
			return "Last run earlier", true
		}
		return "Last run " + imAgo(clock.Now().Sub(time.UnixMilli(r.StartedAtMs))), true
	}
	return "Not run yet", false
}

// imAgo is "just now", "12 min ago", "3 h ago", "2 days ago".
func imAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + " min ago"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " h ago"
	}
	return strconv.Itoa(int(d.Hours()/24)) + " days ago"
}

// pipeItem is one box of the pipeline.
type pipeItem struct {
	kind  int // 0 you, 1 step, 2 answer
	n     int
	step  aiPipeStep
	chip  string
	tools string
}

// stepTools is what a step may use, in a few words.
func stepTools(st aiPipeStep) string {
	switch st.step.Tools.Preset {
	case agentdag.PresetReadOnly:
		return "read-only"
	case agentdag.PresetNone:
		return "no tools"
	}
	if t := aiToolsText(st.step.Tools); t != "" {
		return t
	}
	return "all tools"
}

func stepChip(st aiPipeStep) string {
	model := st.model
	if model == "" {
		return theme.ASCIIText(" " + st.slot + ": not set ")
	}
	return theme.ASCIIText(" " + st.slot + " · " + model + " ")
}

// drawProfileBody draws the pipeline, STEPS, SETTINGS and the last-run line from
// row y; it returns the row after the last one. h is the pane height
// (small windows get the chips).
func (m Model) drawProfileBody(d *imDraw, y int, pv *imProfileView, h int) int {
	cv, mx := d.cv, d.mx
	items := []pipeItem{{kind: 0}}
	for i, st := range pv.steps {
		items = append(items, pipeItem{kind: 1, n: i + 1, step: st, chip: stepChip(st), tools: stepTools(st)})
	}
	items = append(items, pipeItem{kind: 2})
	contentW := d.w - 2*mx

	// Boxes when they fit, wrapped boxes when there is room, else chips.
	chips := contentW < 60 || h < 26
	capW := 0
	if !chips {
		for _, c := range []int{36, 30, 26, 22} {
			if pipeWidth(items, c) <= contentW {
				capW = c
				break
			}
		}
		if capW == 0 {
			capW = 26
		}
	}
	if chips {
		y = d.pipeChips(y, items) + 1
	} else {
		y = d.pipeBoxes(y, items, capW, contentW) + 1
	}
	if pv.note != "" {
		cv.text(mx, y, imTrunc(theme.ASCIIText(pv.note), contentW), imTokDim())
		y++
	}
	y++

	// STEPS.
	hint := ""
	if contentW >= 90 {
		hint = "edit a step with e · add with a"
	}
	y = d.head(y, "STEPS", hint)
	nameCol := 8
	for _, st := range pv.steps {
		nameCol = max(nameCol, min(runewidth.StringWidth(st.name), 14))
	}
	for i, st := range pv.steps {
		cv.text(mx, y, strconv.Itoa(i+1), imTokAccent().B())
		cv.text(mx+3, y, imTrunc(st.name, nameCol), imTokPrimary().B())
		x := mx + 3 + nameCol + 1
		chip := stepChip(st)
		if x+imPillW(chip) <= d.w-mx {
			x = cv.pill(x, y, chip, theme.BgSectionHdr, theme.TextSecondary, false) + 2
			if room := d.w - mx - x; room >= 6 {
				x = cv.text(x, y, imTrunc(stepTools(st), room), imTokMuted())
				if w := theme.ASCIIText("· " + pv.waits[i]); pv.waits[i] != "" && x+2+runewidth.StringWidth(w) <= d.w-mx {
					cv.text(x+2, y, w, imTokMuted())
				}
			}
		} else if x < d.w-mx {
			cv.text(x, y, imTrunc(st.modelText(true)+" · "+stepTools(st), d.w-mx-x), imTokMuted())
		}
		y++
		if head := aiProfilePromptHead(st.step.System); head != "" {
			cv.text(mx+3, y, imTrunc(imQuote(head), max(d.w-mx-(mx+3), 4)), imTokDim().I())
			y++
		}
		if d.roomy {
			y++
		}
	}
	if len(pv.steps) == 0 {
		cv.text(mx+3, y, "No steps yet", imTokDim())
		y++
	}

	// SETTINGS.
	y += d.gap() - boolInt(d.roomy)
	y = d.head(y, "SETTINGS", "")
	kvs := [][2]string{{"categories", pv.cats}, {"budget", pv.budget}, {"source", pv.source}}
	for _, kv := range kvs {
		d.kv(y, kv[0], kv[1], imTokSecondary())
		y++
	}

	// One quiet line: when it last ran, and the way to the runs.
	y += d.gap()
	if pv.hasRun {
		link := "See runs →"
		cv.text(mx, y, pv.lastRun+" · ", imTokMuted())
		lx := mx + runewidth.StringWidth(pv.lastRun+" · ")
		cv.text(lx, y, imTrunc(theme.ASCIIText(link), max(d.w-mx-lx, 4)), imTokAccent())
		d.hit("runs", mx, y, min(lx-mx+runewidth.StringWidth(link), d.w-2*mx), 1)
	} else {
		cv.text(mx, y, pv.lastRun, imTokDim())
	}
	return y + 1
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// boxW is the width of the box of item it, with natural widths capped at c.
func boxW(it pipeItem, c int) int {
	switch it.kind {
	case 0:
		return 10
	case 2:
		return 14
	}
	w := runewidth.StringWidth(it.chip) + 6
	w = max(w, runewidth.StringWidth(strconv.Itoa(it.n)+" "+it.step.name)+6)
	w = max(w, runewidth.StringWidth(it.tools)+6)
	return max(18, min(w, c))
}

// pipeWidth is the width of the whole pipeline in one row.
func pipeWidth(items []pipeItem, c int) int {
	w := 0
	uniform := 0
	for _, it := range items {
		if it.kind == 1 {
			uniform = max(uniform, boxW(it, c))
		}
	}
	for i, it := range items {
		bw := boxW(it, c)
		if it.kind == 1 {
			bw = uniform
		}
		w += bw
		if i > 0 {
			w += 1 + 5 + 1
		}
	}
	return w
}

// pipeBoxes draws the pipeline as boxes with arrows, wrapping onto more
// rows when it does not fit; returns the row after the last box.
func (d *imDraw) pipeBoxes(y int, items []pipeItem, capW, contentW int) int {
	uniform := 0
	for _, it := range items {
		if it.kind == 1 {
			uniform = max(uniform, boxW(it, capW))
		}
	}
	cv, mx := d.cv, d.mx
	x := mx
	rowTop := y
	for i, it := range items {
		bw := boxW(it, capW)
		if it.kind == 1 {
			bw = uniform
		}
		need := bw
		if i > 0 && x > mx {
			need += 7
		}
		if x > mx && x+need > mx+contentW {
			rowTop += 6
			x = mx
		}
		if x == mx && i > 0 {
			// a wrapped row starts with the arrow that leads into it
			x = cv.text(x, rowTop+2, imArrow(3), imTokDim()) + 1
		} else if i > 0 {
			x = cv.text(x+1, rowTop+2, imArrow(5), imTokDim()) + 1
		}
		d.drawBox(x, rowTop, bw, it)
		x += bw
	}
	return rowTop + 5
}

func (d *imDraw) drawBox(x, y, bw int, it pipeItem) {
	cv := d.cv
	switch it.kind {
	case 0:
		cv.box(x, y, bw, 5, imFg(theme.BorderDefault))
		cv.textC(x, y+1, bw, "you", imTokPrimary().B())
		cv.textC(x, y+2, bw, "chat", imTokMuted())
		cv.textC(x, y+3, bw, "message", imTokDim())
	case 2:
		cv.box(x, y, bw, 5, imFg(theme.BorderDefault))
		cv.textC(x, y+1, bw, "answer", imTokPrimary().B())
		cv.textC(x, y+2, bw, "+ diff", imTokMuted())
		cv.textC(x, y+3, bw, "in the chat", imTokDim())
	default:
		cv.box(x, y, bw, 5, imFg(theme.BorderDefault))
		cv.text(x+2, y+1, strconv.Itoa(it.n), imTokAccent().B())
		cv.text(x+4, y+1, imTrunc(it.step.name, bw-6), imTokPrimary().B())
		chip := imTrunc(it.chip, bw-6)
		cv.pill(x+2, y+2, chip, theme.BgSectionHdr, theme.TextSecondary, false)
		cv.text(x+3, y+3, imTrunc(it.tools, bw-5), imTokMuted())
		d.hit("step:"+strconv.Itoa(it.n), x, y, bw, 5)
	}
}

// pipeChips draws the pipeline as one line of chips, wrapped to the next
// line when the pane is narrow; returns the row after the last chip line.
func (d *imDraw) pipeChips(y int, items []pipeItem) int {
	cv, mx := d.cv, d.mx
	x := mx
	lineH := 1
	if d.roomy {
		lineH = 2
	}
	for i, it := range items {
		label := " you "
		chipFg := theme.TextPrimary
		switch it.kind {
		case 1:
			label = theme.ASCIIText(" " + strconv.Itoa(it.n) + " " + it.step.name + " · " + it.step.slot + " ")
			chipFg = theme.TextSecondary
		case 2:
			label = " answer "
		}
		w := imPillW(label)
		arrow := 0
		if i > 0 {
			arrow = 4 + 1
		}
		if x+arrow+w > d.w-mx && x > mx {
			y += lineH
			x = mx
			arrow = 0
		}
		if i > 0 && arrow > 0 {
			x = cv.text(x, y, imArrow(4), imTokDim()) + 1
		}
		cv.pill(x, y, label, theme.BgSectionHdr, chipFg, it.kind != 1)
		if it.kind == 1 {
			d.hit("step:"+strconv.Itoa(it.n), x, y, w, 1)
		}
		x += w
		if i < len(items)-1 {
			x++
		}
	}
	return y + 1
}

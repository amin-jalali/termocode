package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc: the outside-agent page (B4) ─────────────────────────────────
//
//	row 0   ↔ claude-code  outside agent · uses termocode over MCP  ● connected since 09:41
//	        ╭──────────────╮      14 calls      ╭───────────────╮
//	        │  termocode   │ ═══════ ↔ ═══════ │  claude-code  │
//	        │ listening ·… │    2 asked you     │   connected   │
//	        ╰──────────────╯                    ╰───────────────╯
//	        ALWAYS ALLOWED   chips (x revokes them, asks first)
//	        RECENT CALLS     this agent's last calls
//	h-3     Revoke… (x)  Copy config (y)  Status (T)   ⋯ more (Enter): …
//
// The listening row ("Outside agents") has the same picture with the
// agents that are not running yet beside it, and the Start pills. Every
// verb is a flow that exists without this page: Start opens a terminal
// tab, Copy config copies the server config, Allowed tools… and
// Disconnect… are today's screens. Revoke… forgets this agent's "Always
// allow" list after a card (Keep first).

// imAgentView is what the agent picture draws.
type imAgentView struct {
	listening bool
	mode      string
	agent     *imAgent // nil: the listening row's page
	first     *imAgent // the listening page: the first connected agent
	others    []string // "Codex · not running"
}

// agentPageContent is the outside-agents page: the listening row, or one
// connected agent.
func agentPageContent(sn itermocSnap, row imRow) imContent {
	c := imContent{icon: imIconAgents.String(), name: "Outside agents", sub: "agents that use termocode over MCP"}
	av := &imAgentView{listening: sn.listening, mode: sn.mode}
	mark, _ := imGlyph("ready")
	idle, _ := imGlyph("idle")
	if sn.listening {
		c.right, c.rightTok = mark+" listening · "+sn.mode, theme.AccentGreen
	} else {
		c.right, c.rightTok = idle+" not listening", theme.TextMuted
	}
	if row.kind == imRowAgent {
		a, ok := sn.agent(row.name)
		if !ok {
			return c
		}
		av.agent = &a
		c.name, c.sub = a.name, "outside agent · uses termocode over MCP"
		c.right, c.rightTok = mark+" connected since "+a.since, theme.AccentGreen
		allow := sn.allow[a.name]
		sec := imFactSec{title: "ALWAYS ALLOWED"}
		if len(allow) == 0 {
			sec.lines = []string{"none · every call that changes something asks you first"}
		} else {
			sec.right = strconv.Itoa(len(allow)) + " run without asking · x revokes them"
			for _, k := range allow {
				sec.chips = append(sec.chips, mcpServeAllowRowName(k))
			}
		}
		c.facts = []imFactSec{sec}
		if calls := sn.callsOf(a.name); len(calls) > 0 {
			c.facts = append(c.facts, imFactSec{title: "RECENT CALLS", lines: calls})
		}
		c.pills = []imPill{
			{verb: "revoke", label: "Revoke…", key: "x", primary: len(allow) > 0},
			{verb: "copy", label: "Copy config", key: "y", primary: len(allow) == 0},
			{verb: "status", label: "Status", key: "T"},
		}
		c.more = "Allowed tools… · Disconnect…"
		c.hint = "x revoke · y copy config · T status · Enter more · Esc list · ? keys"
		c.hintNarrow = "Enter more · Esc list · ? keys"
		c.agent = av
		return c
	}

	// The listening row: the server, who is connected, who is not yet.
	if len(sn.agents) > 0 {
		av.first = &sn.agents[0]
	}
	for _, r := range itermocAgentRecipes() {
		on := false
		for _, a := range sn.agents {
			on = on || strings.Contains(strings.ToLower(a.name), r.ID)
		}
		if !on {
			av.others = append(av.others, r.Title+" · not running")
		}
	}
	rows := [][2]string{{"mode", imServeMode(sn.mode)}}
	if sn.listening {
		rows = append(rows, [2]string{"agents", strconv.Itoa(len(sn.agents)) + " connected"})
	} else if sn.whySentence != "" {
		rows = append(rows, [2]string{"why", sn.whySentence})
	}
	c.facts = []imFactSec{{title: "STATUS", rows: rows}}
	if len(sn.agents) > 1 {
		var lines []string
		for _, a := range sn.agents {
			lines = append(lines, a.name+" · since "+a.since+" · "+strconv.Itoa(a.calls)+" calls")
		}
		c.facts = append(c.facts, imFactSec{title: "CONNECTED", lines: lines})
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
		{verb: "start-codex", label: "Start Codex", key: "S"},
		{verb: "copy", label: "Copy config", key: "y"},
	}
	c.more = "Status… · Allowed tools…"
	switch {
	case !sn.listening && sn.mode == "off":
		c.more = "Turn on · " + c.more
	case !sn.listening:
		c.more = "Start listening now · " + c.more
	default:
		c.more += " · Stop listening…"
	}
	c.hint = "s start Claude Code · S start Codex · y copy config · Enter more · Esc list · ? keys"
	c.hintNarrow = "s start · y copy · Enter more · Esc list"
	c.agent = av
	return c
}

// imServeMode is the mcp_server mode in words.
func imServeMode(mode string) string {
	switch mode {
	case "auto":
		return "auto · listens when you start an agent tab"
	case "on":
		return "on · listens from the start"
	case "off":
		return "off · no outside agent can connect"
	}
	return mode
}

// callsOf is the recent call rows of one agent ("09:42 claude-code · …").
func (sn itermocSnap) callsOf(name string) []string {
	var out []string
	for _, l := range sn.calls {
		if _, rest, ok := strings.Cut(string(l), " "); ok && strings.HasPrefix(rest, name+" · ") {
			out = append(out, string(l))
		}
	}
	return out
}

// drawAgentBody draws the connection picture, then the facts.
func (d *imDraw) drawAgentBody(y int, av *imAgentView, facts []imFactSec) int {
	if d.w-2*d.mx >= 64 {
		y = d.agentPicture(y, av)
	} else {
		y = d.agentLine(y, av)
	}
	y += d.gap()
	return d.facts(y, facts)
}

// agentPicture is termocode ═══ ↔ ═══ agent, as two boxes.
func (d *imDraw) agentPicture(y int, av *imAgentView) int {
	cv := d.cv
	x := d.mx
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	bw := 20
	cv.box(x, y, bw, 5, imFg(theme.BorderDefault))
	cv.textC(x, y+1, bw, "termocode", imTokPrimary().B())
	state := "not listening"
	if av.listening {
		state = "listening · " + av.mode
	}
	cv.textC(x, y+2, bw, imTrunc(theme.ASCIIText(state), bw-2), imTokMuted())
	cv.textC(x, y+3, bw, "this window", imTokDim())
	x += bw

	a := av.agent
	if a == nil {
		a = av.first
	}
	lw := 22
	line, mid := "═", " ↔ "
	if ascii {
		line, mid = "=", " <> "
	}
	half := (lw - imRuneW(mid)) / 2
	wire := strings.Repeat(line, half) + mid + strings.Repeat(line, lw-half-imRuneW(mid))
	wireTok := imTokDim()
	if a != nil {
		wireTok = imTokOK()
		cv.textC(x, y+1, lw, strconv.Itoa(a.calls)+" calls", imTokMuted())
		cv.textC(x, y+3, lw, strconv.Itoa(a.asked)+" asked you", imTokMuted())
	} else {
		cv.textC(x, y+1, lw, "nobody yet", imTokDim())
	}
	cv.text(x, y+2, wire, wireTok)
	x += lw
	aw := 22
	if a != nil {
		cv.box(x, y, aw, 5, imTokAccent())
		cv.textC(x, y+1, aw, imTrunc(a.name, aw-2), imTokPrimary().B())
		cv.textC(x, y+2, aw, "connected", imTokOK())
		cv.textC(x, y+3, aw, "since "+a.since, imTokDim())
	} else {
		cv.box(x, y, aw, 5, imFg(theme.BorderSubtle))
		cv.textC(x, y+1, aw, "no agent yet", imTokMuted())
		cv.textC(x, y+3, aw, "s starts Claude Code", imTokDim())
	}
	x += aw
	if av.agent == nil {
		idle, _ := imGlyph("idle")
		for i, o := range av.others {
			if i > 2 || x+4 >= d.w-d.mx {
				break
			}
			cv.text(x+4, y+1+i, imTrunc(theme.ASCIIText(idle+" "+o), d.w-d.mx-x-4), imTokDim())
		}
	}
	return y + 5
}

// agentLine is the picture in one line for a narrow pane.
func (d *imDraw) agentLine(y int, av *imAgentView) int {
	a := av.agent
	if a == nil {
		a = av.first
	}
	arrow := " ↔ "
	if theme.CurrentIconMode == theme.IconModeASCII {
		arrow = " <> "
	}
	s := "termocode" + arrow
	tok := imTokDim()
	if a != nil {
		s += a.name + " · " + strconv.Itoa(a.calls) + " calls"
		tok = imTokOK()
	} else {
		s += "nobody yet"
	}
	d.cv.text(d.mx, y, imTrunc(s, d.w-2*d.mx), tok)
	return y + 1
}

func imRuneW(s string) int { return lenR(s) }

// ── Revoke… ──────────────────────────────────────────────────────────────

// confirmKindItermocAgentRevoke is the "Revoke always allow?" card.
const confirmKindItermocAgentRevoke confirmKindEnum = 2320

// openItermocAgentRevoke asks before an agent's "Always allow" list is
// forgotten. Keep is first and focused.
func (m *Model) openItermocAgentRevoke(client string) tea.Cmd {
	keys := mcpServeAllowLoad()[client]
	if len(keys) == 0 {
		return m.aiInfo(client + ": nothing is always allowed; every call that changes something asks first")
	}
	var names []string
	for _, k := range keys {
		names = append(names, mcpServeAllowRowName(k))
	}
	what := strconv.Itoa(len(keys)) + " tools run"
	if len(keys) == 1 {
		what = "1 tool runs"
	}
	msg := what + " without asking: " + strings.Join(names, ", ") + ".\nAfter Revoke each call that changes something asks you first again. The agent stays connected."
	m.itermocS().revokeClient = client
	m.confirm = confirm.New("Revoke "+client+"'s always allow?", msg, []confirm.Button{
		{ID: "keep", Title: "Keep"},
		{ID: "revoke", Title: "Revoke", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindItermocAgentRevoke
	return nil
}

// answerItermocAgentRevoke runs the card's button (Esc is Keep).
func (m *Model) answerItermocAgentRevoke(id string) tea.Cmd {
	client := m.itermocS().revokeClient
	m.itermocS().revokeClient = ""
	if id != "revoke" || client == "" {
		return nil
	}
	list := mcpServeAllowLoad()
	delete(list, client)
	err := mcpServeAllowSave(list)
	m.mcpServeAllowSync()
	m.itermocRefreshNow()
	if err != nil {
		return m.aiToastErr("Settings error", err)
	}
	return m.aiInfo(client + ": every call that changes something asks first again")
}

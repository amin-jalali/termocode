package app

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// ── Itermoc: one sidebar view for the AI's profiles, models, services and
// outside agents ───────────────────────────────────────────────────────────
//
// The view reads and routes: every row comes from state that exists today
// (the profile set, the model slots, the MCP manager, the MCP server) and
// every action opens today's picker or card. It holds a snapshot of that
// state, refreshed on open, on R and about once a second while it is
// drawn, so a render never touches disk on every frame. The snapshot has
// no secrets: a model slot carries only whether a key exists.

// The four sections, in order.
const (
	imSecProfiles = "profiles"
	imSecModels   = "models"
	imSecServices = "services"
	imSecAgents   = "agents"
)

var itermocSectionOrder = []string{imSecProfiles, imSecModels, imSecServices, imSecAgents}

// itermocSectionTitle is the header text of a section.
func itermocSectionTitle(sec string) string {
	switch sec {
	case imSecProfiles:
		return "PROFILES"
	case imSecModels:
		return "MODELS"
	case imSecServices:
		return "SERVICES"
	case imSecAgents:
		return "OUTSIDE AGENTS"
	}
	return strings.ToUpper(sec)
}

// itermocSectionByName maps the words of the palette commands and of the
// jump-ins to a section ("" = none).
func itermocSectionByName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "profiles", "profile":
		return imSecProfiles
	case "models", "model", "slots":
		return imSecModels
	case "services", "service":
		return imSecServices
	case "agents", "outside-agents", "outside agents":
		return imSecAgents
	}
	return ""
}

// What a model slot says about its key. Never the key itself.
type imKeyState int

const (
	imKeyOK      imKeyState = iota // a key exists
	imKeyMissing                   // the provider needs a key and none is set
	imKeyNone                      // a local provider: no key needed
	imKeyUnset                     // an OpenAI-compatible server without a key
)

// imProfile is one profile as the view shows it.
type imProfile struct {
	p      ai.Profile
	trust  string // aiTrustUse / aiTrustAsk / aiTrustRefused
	active bool
}

// imSlot is one model slot: names and a key state, nothing secret.
type imSlot struct {
	name, provider, model string
	key                   imKeyState
}

// imAgent is one connected outside agent.
type imAgent struct {
	id           uint64
	name, since  string
	calls, asked int
}

// imCall is one finished call of an outside agent, as a row of text.
type imCall string

// itermocSnap is everything the view draws.
type itermocSnap struct {
	profiles []imProfile
	slots    []imSlot
	services []mcp.Status

	mode       string // mcp_server: auto | on | off
	listening  bool
	whyTitle   string
	whySentence string
	agents     []imAgent
	calls      []imCall
}

// itermocState is the view's state. It lives behind a pointer on the Model,
// so a render (a value receiver) can refresh the snapshot.
type itermocState struct {
	snap     itermocSnap
	loaded   bool
	snapAt   time.Time
	cursor   int
	scroll   int
	collapse map[string]bool
	// collapseSet: the first-open default for the folds was applied.
	collapseSet bool
	filter      string
	menuRow     imRow // the row the open actions menu is about
	typing      bool
	// w is the width the last render used (the buttons rows wrap to it).
	w int
	// seq grows with every change a render must notice.
	seq int
}

func newItermocState() *itermocState {
	return &itermocState{collapse: map[string]bool{}}
}

// width is the width the buttons rows are laid out for.
func (st *itermocState) width() int {
	if st.w <= 0 {
		return 40
	}
	return st.w
}

// touch marks a change.
func (st *itermocState) touch() { st.seq++ }

// ── The snapshot ─────────────────────────────────────────────────────────

// itermocSlotOrder puts the built-in slots first, then the rest by name.
func itermocSlotOrder(names []string) []string {
	rank := map[string]int{ai.SlotMain: 0, ai.SlotQuick: 1, ai.SlotLocal: 2}
	out := append([]string(nil), names...)
	sort.Slice(out, func(i, j int) bool {
		ri, iok := rank[out[i]]
		rj, jok := rank[out[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		return out[i] < out[j]
	})
	return out
}

// itermocKeyOf says whether a slot's provider has what it needs. It looks
// at the config only to answer yes or no: the key text is never copied.
func itermocKeyOf(c ai.Config) imKeyState {
	switch c.Kind {
	case ai.KindNone:
		return imKeyMissing
	case ai.KindOllama, ai.KindLMStudio:
		return imKeyNone
	case ai.KindClaudeOAuth:
		if c.Token != nil {
			return imKeyOK
		}
		return imKeyMissing
	case ai.KindCompatible:
		if c.APIKey != "" {
			return imKeyOK
		}
		return imKeyUnset
	}
	if c.APIKey != "" {
		return imKeyOK
	}
	return imKeyMissing
}

// itermocBuild reads the state the view shows. reload reads the profile
// files and the MCP config again (open, R); otherwise only what is in
// memory and the small status reads are used.
func itermocBuild(m Model, reload bool) itermocSnap {
	var sn itermocSnap
	s := m.ai
	if s == nil {
		for _, p := range ai.BuiltinProfiles() {
			sn.profiles = append(sn.profiles, imProfile{p: p, trust: aiTrustUse, active: p.Name == ai.ProfileDefault})
		}
		return sn
	}
	if reload {
		s.reloadProfiles()
	}
	active := s.activeProfile().Name
	for _, n := range s.profiles.Names() {
		p, ok := s.profiles.Get(n)
		if !ok {
			continue
		}
		sn.profiles = append(sn.profiles, imProfile{p: p, trust: m.aiProfileTrust(p), active: n == active})
	}

	names := map[string]bool{}
	if s.slots != nil {
		for _, n := range s.slots.Names() {
			names[n] = true
		}
	}
	for n := range s.slotFake {
		names[n] = true
	}
	if !names[ai.SlotMain] {
		names[ai.SlotMain] = true // the built-in slot is shown even while AI is off
	}
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, n)
	}
	for _, n := range itermocSlotOrder(list) {
		c, ok := s.aiSlotConfig(n)
		if !ok {
			sn.slots = append(sn.slots, imSlot{name: n, key: imKeyMissing})
			continue
		}
		sn.slots = append(sn.slots, imSlot{name: n, provider: c.Kind, model: c.Model, key: itermocKeyOf(c)})
	}

	if s.mcp != nil || reload {
		mgr := s.mcp
		if mgr == nil {
			mgr = m.aiMCP()
		}
		if reload {
			mgr.Reload()
		}
		sn.services = mgr.Servers()
	}

	sn.mode = mcpServeMode()
	sn.listening = m.mcpServe != nil
	if !sn.listening {
		sn.whyTitle, sn.whySentence = m.mcpServeNotListeningWhy()
	} else {
		for _, a := range m.mcpServe.clients {
			name := a.name
			if name == "" {
				name = "(no name yet)"
			}
			sn.agents = append(sn.agents, imAgent{id: a.id, name: name, since: a.since.Format("15:04"), calls: a.calls, asked: a.asked})
		}
	}
	for _, l := range m.mcpServeAuditLines(3) {
		if row, ok := mcpServeCallRow(l); ok {
			sn.calls = append(sn.calls, imCall(row))
		}
	}
	return sn
}

// refresh rebuilds the snapshot and keeps the cursor on the same row.
func (st *itermocState) refresh(m Model, reload bool) {
	key := st.cursorKey()
	st.snap = itermocBuild(m, reload)
	st.snapAt = clock.Now()
	st.loaded = true
	if !st.collapseSet {
		st.collapseSet = true
		// A short window opens one section at a time (the profiles).
		if m.h > 0 && m.h < 30 {
			st.collapse[imSecModels], st.collapse[imSecAgents] = true, true
		}
	}
	st.restoreCursor(key)
	st.touch()
}

// stale reports whether the snapshot is old enough to read again.
func (st *itermocState) stale() bool {
	return !st.loaded || clock.Now().Sub(st.snapAt) > time.Second
}

// ── Rows ─────────────────────────────────────────────────────────────────

type imRowKind int

const (
	imRowSection imRowKind = iota
	imRowProfile
	imRowSlot
	imRowService
	imRowAgentStatus // "listening · 1 agent" / "not listening" (the outside agents' summary)
	imRowAgent
	imRowCall
	imRowNote    // an empty or error state line
	imRowButtons // the section's buttons: [Label (key)] …
)

// imRow is one line of the view.
type imRow struct {
	kind imRowKind
	sec  string
	name string // the profile, slot, service or agent id this row stands for
	text string // note and call rows
	// note rows: title is drawn strong
	strong bool
}

func (r imRow) selectable() bool {
	switch r.kind {
	case imRowNote, imRowCall, imRowButtons:
		return false
	}
	return true
}

// itermocMatch reports whether a row with these words passes the filter.
func (st *itermocState) matches(words ...string) bool {
	f := strings.ToLower(strings.TrimSpace(st.filter))
	if f == "" {
		return true
	}
	for _, w := range words {
		if strings.Contains(strings.ToLower(w), f) {
			return true
		}
	}
	return false
}

// rows lists the lines of the view for the current snapshot, folds and
// filter. Rows are typed: nothing is parsed back from display text.
func (st *itermocState) rows() []imRow {
	var out []imRow
	filtering := strings.TrimSpace(st.filter) != ""
	for _, sec := range itermocSectionOrder {
		items := st.sectionItems(sec)
		if filtering && len(items) == 0 {
			continue
		}
		out = append(out, imRow{kind: imRowSection, sec: sec})
		if st.collapse[sec] && !filtering {
			continue
		}
		out = append(out, items...)
		if !filtering {
			for i := range itermocButtonLines(sec, st.width()) {
				out = append(out, imRow{kind: imRowButtons, sec: sec, name: strconv.Itoa(i)})
			}
		}
	}
	return out
}

func (st *itermocState) sectionItems(sec string) []imRow {
	sn := st.snap
	var out []imRow
	switch sec {
	case imSecProfiles:
		for _, p := range sn.profiles {
			if st.matches(p.p.Name, p.p.Title, p.p.Source) {
				out = append(out, imRow{kind: imRowProfile, sec: sec, name: p.p.Name})
			}
		}
		if len(sn.profiles) == 0 && st.filter == "" {
			out = append(out, imRow{kind: imRowNote, sec: sec, text: "No profiles", strong: true},
				imRow{kind: imRowNote, sec: sec, text: "Save one from the current profile."})
		}
	case imSecModels:
		for _, s := range sn.slots {
			if st.matches(s.name, s.provider, s.model) {
				out = append(out, imRow{kind: imRowSlot, sec: sec, name: s.name})
			}
		}
	case imSecServices:
		for _, s := range sn.services {
			if st.matches(s.Name, s.State, s.Source) {
				out = append(out, imRow{kind: imRowService, sec: sec, name: s.Name})
			}
		}
		if len(sn.services) == 0 && st.filter == "" {
			out = append(out, imRow{kind: imRowNote, sec: sec, text: "No services yet", strong: true},
				imRow{kind: imRowNote, sec: sec, text: "Connect GitHub, Linear or Slack for the chat."})
		}
	case imSecAgents:
		if st.matches("listening", "not listening", "mcp server", "outside agents", sn.mode) || len(sn.agents) > 0 && st.filter == "" {
			out = append(out, imRow{kind: imRowAgentStatus, sec: sec, name: "status"})
		}
		for _, a := range sn.agents {
			if st.matches(a.name) {
				out = append(out, imRow{kind: imRowAgent, sec: sec, name: agentKey(a.id)})
			}
		}
		if st.filter == "" || st.matches("calls") {
			for _, c := range sn.calls {
				out = append(out, imRow{kind: imRowCall, sec: sec, text: string(c)})
			}
		}
	}
	return out
}

func agentKey(id uint64) string { return "agent:" + strconv.FormatUint(id, 10) }

// ── Cursor ───────────────────────────────────────────────────────────────

func imRowKey(r imRow) string { return string(rune('a'+int(r.kind))) + "/" + r.sec + "/" + r.name }

// cursorKey names the row under the cursor ("" = none).
func (st *itermocState) cursorKey() string {
	rows := st.rows()
	if st.cursor < 0 || st.cursor >= len(rows) {
		return ""
	}
	return imRowKey(rows[st.cursor])
}

// restoreCursor puts the cursor back on the row named key, else clamps it.
func (st *itermocState) restoreCursor(key string) {
	rows := st.rows()
	if key != "" {
		for i, r := range rows {
			if r.selectable() && imRowKey(r) == key {
				st.cursor = i
				return
			}
		}
	}
	st.clamp()
}

// clamp keeps the cursor on a selectable row.
func (st *itermocState) clamp() {
	rows := st.rows()
	if len(rows) == 0 {
		st.cursor = 0
		return
	}
	st.cursor = max(0, min(st.cursor, len(rows)-1))
	if rows[st.cursor].selectable() {
		return
	}
	for d := 1; d < len(rows); d++ {
		for _, i := range []int{st.cursor + d, st.cursor - d} {
			if i >= 0 && i < len(rows) && rows[i].selectable() {
				st.cursor = i
				return
			}
		}
	}
}

// move moves the cursor by d selectable rows.
func (st *itermocState) move(d int) {
	rows := st.rows()
	if len(rows) == 0 {
		return
	}
	i := st.cursor
	for step := 0; step < len(rows); step++ {
		n := i + d
		if n < 0 || n >= len(rows) {
			break
		}
		i = n
		if rows[i].selectable() {
			st.cursor = i
			st.touch()
			return
		}
	}
}

// current is the row under the cursor.
func (st *itermocState) current() (imRow, bool) {
	rows := st.rows()
	if st.cursor < 0 || st.cursor >= len(rows) {
		return imRow{}, false
	}
	return rows[st.cursor], true
}

// jumpSection moves the cursor to the next (d=1) or previous section header.
func (st *itermocState) jumpSection(d int) {
	rows := st.rows()
	var heads []int
	for i, r := range rows {
		if r.kind == imRowSection {
			heads = append(heads, i)
		}
	}
	if len(heads) == 0 {
		return
	}
	at := 0
	for i, h := range heads {
		if h <= st.cursor {
			at = i
		}
	}
	if d > 0 {
		at = (at + 1) % len(heads)
	} else if heads[at] < st.cursor {
		// from inside a section the first step back is its own header
	} else {
		at = (at + len(heads) - 1) % len(heads)
	}
	st.cursor = heads[at]
	st.touch()
}

// focusSection opens a section (unfolds it) and puts the cursor on its header.
func (st *itermocState) focusSection(sec string) {
	st.filter, st.typing = "", false
	st.collapse[sec] = false
	for i, r := range st.rows() {
		if r.kind == imRowSection && r.sec == sec {
			st.cursor = i
			break
		}
	}
	st.touch()
}

// ── Lookups ──────────────────────────────────────────────────────────────

func (sn itermocSnap) profile(name string) (imProfile, bool) {
	for _, p := range sn.profiles {
		if p.p.Name == name {
			return p, true
		}
	}
	return imProfile{}, false
}

func (sn itermocSnap) slot(name string) (imSlot, bool) {
	for _, s := range sn.slots {
		if s.name == name {
			return s, true
		}
	}
	return imSlot{}, false
}

func (sn itermocSnap) service(name string) (mcp.Status, bool) {
	for _, s := range sn.services {
		if s.Name == name {
			return s, true
		}
	}
	return mcp.Status{}, false
}

func (sn itermocSnap) agent(key string) (imAgent, bool) {
	for _, a := range sn.agents {
		if agentKey(a.id) == key {
			return a, true
		}
	}
	return imAgent{}, false
}

// itermocAgentRecipes are the outside agents the view can start.
func itermocAgentRecipes() []agents.Agent {
	var out []agents.Agent
	for _, id := range []string{"claude", "codex"} {
		if a, ok := agents.ByID(id); ok {
			out = append(out, a)
		}
	}
	return out
}

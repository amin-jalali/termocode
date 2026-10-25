package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── Itermoc: opening it, keys, mouse, the actions menu ───────────────────
//
// Nothing here edits anything by itself. Every verb opens today's flow:
// the profile picker's switch, AI: Show Profile, the Connect wizard, the
// MCP service actions, the MCP server's status screen, the trust and
// remove cards. Risky verbs keep their Deny-first cards.

const itermocGuidePage = "guides/itermoc.md"

// itermocNoOpen is the AI policy reason of the open commands.
const itermocNoOpen = "a screen for the user (secrets and access live there); the AI opens it with the typed ai_view_open tool"

// pickerKindItermocActions is the small actions menu of one row.
const pickerKindItermocActions pickerKindEnum = 2300

// itermocVisible reports whether the view is shown in the sidebar.
func (m Model) itermocVisible() bool {
	return m.showExp && !m.zenMode && m.activity.Active() == activity.ViewItermoc
}

// itermocState returns the view's state, created on first use.
func (m *Model) itermocS() *itermocState {
	if m.itermoc == nil {
		m.itermoc = newItermocState()
		m.itermoc.refresh(*m, true)
	}
	return m.itermoc
}

// revealItermocView shows the view; sec ("" = keep) puts the cursor on a
// section header and opens it.
func (m *Model) revealItermocView(focus bool, sec string) tea.Cmd {
	m.activity.SetActive(activity.ViewItermoc)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	if focus {
		m.focus = FocusExplorer
	}
	m.aiS()
	if m.itermoc == nil {
		m.itermoc = newItermocState()
	}
	m.itermoc.refresh(*m, true)
	if sec != "" {
		m.itermoc.focusSection(sec)
	}
	return nil
}

// itermocRefreshNow reads the state again if the view has been opened.
func (m *Model) itermocRefreshNow() {
	if m.itermoc != nil {
		m.itermoc.refresh(*m, false)
	}
}

// ── Palette ──────────────────────────────────────────────────────────────

func itermocPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "itermoc-open", Title: "Itermoc: Open"},
		{ID: "itermoc-profiles", Title: "Itermoc: Profiles"},
		{ID: "itermoc-models", Title: "Itermoc: Models"},
		{ID: "itermoc-services", Title: "Itermoc: Services"},
		{ID: "itermoc-agents", Title: "Itermoc: Outside Agents"},
		{ID: "itermoc-open-ai", Title: "AI: Agents & Connections"}, // the same screen under the name the AI menu uses
		{ID: "ai-model-slot-add", Title: "AI: Add Model Slot…"},    // the slot form (itermoc_slots.go)
	}
}

func (m *Model) dispatchItermocPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "itermoc-open", "itermoc-open-ai":
		return m.revealItermocView(true, ""), true
	case "itermoc-profiles":
		return m.revealItermocView(true, imSecProfiles), true
	case "itermoc-models":
		return m.revealItermocView(true, imSecModels), true
	case "itermoc-services":
		return m.revealItermocView(true, imSecServices), true
	case "itermoc-agents":
		return m.revealItermocView(true, imSecAgents), true
	case "ai-model-slot-add":
		return m.openItermocSlotAdd(), true
	}
	return nil, false
}

// ── Keys ─────────────────────────────────────────────────────────────────

func itermocKeyRows() []confirm.KeyRow {
	return keyRows(
		[]string{"Move"},
		[]string{"j / k", "move"},
		[]string{"Enter", "actions of the row (a section: fold it)"},
		[]string{"Space", "fold or open the section"},
		[]string{"Tab", "next section"},
		[]string{"/", "filter by name (Esc clears)"},
		[]string{"R", "read everything again"},
		[]string{"Esc", "back to the editor"},
		[]string{"Profiles"},
		[]string{"u", "use the profile"},
		[]string{"d", "details of the profile"},
		[]string{"D", "duplicate the profile…"},
		[]string{"t", "trust a project profile…"},
		[]string{"n", "new profile from the active one…"},
		[]string{"J", "edit profiles.json"},
		[]string{"Models"},
		[]string{"a", "add a model slot…"},
		[]string{"e", "edit the slot…"},
		[]string{"k", "set the key of the slot's provider"},
		[]string{"D", "remove the slot… (asks first)"},
		[]string{"E", "edit ai_models in config.json"},
		[]string{"Services"},
		[]string{"i", "sign in"},
		[]string{"c", "connect a service"},
		[]string{"l", "show the log"},
		[]string{"Outside agents"},
		[]string{"s / S", "start Claude Code / Codex"},
		[]string{"y", "copy the termocode server config"},
		[]string{"T", "show the status screen"},
	)
}

func (m *Model) itermocKeysHelp() tea.Cmd {
	return m.openKeyCard("Itermoc keys", itermocKeyRows(), help.Loc{Page: itermocGuidePage, Anchor: "keys"}, nil)
}

// itermocFilterKey runs a key through the "/" filter; true when it took it.
func (m *Model) itermocFilterKey(k tea.KeyMsg) bool {
	st := m.itermoc
	text, typing, handled := inlineFilterStep(st.filter, st.typing, k)
	st.typing = typing
	if !handled {
		return false
	}
	if text != st.filter {
		cur := st.cursorKey()
		st.filter = text
		st.scroll = 0
		st.restoreCursor(cur)
		if _, ok := st.current(); ok && st.cursorKey() != cur {
			st.cursor = 0
			st.clamp()
		}
	}
	st.touch()
	return true
}

func (m Model) handleItermocSidebarKey(k tea.KeyMsg) (Model, tea.Cmd) {
	st := m.itermocS()
	st.w = m.explorerWidth - 1
	if m.itermocFilterKey(k) {
		return m, nil
	}
	st.clamp()
	row, have := st.current()
	switch k.String() {
	case "up", "k":
		st.move(-1)
	case "down", "j":
		st.move(1)
	case "home", "g":
		st.cursor = 0
		st.clamp()
		st.touch()
	case "end", "G":
		st.cursor = max(len(st.rows())-1, 0)
		st.clamp()
		st.touch()
	case "tab":
		st.jumpSection(1)
	case "shift+tab":
		st.jumpSection(-1)
	case " ":
		if have {
			st.toggleSection(row.sec)
		}
	case "left", "h":
		if have {
			st.collapse[row.sec] = true
			st.focusHeader(row.sec)
			st.touch()
		}
	case "right", "l":
		if have && row.kind == imRowSection {
			st.collapse[row.sec] = false
			st.touch()
		} else if k.String() == "l" {
			return m, m.itermocRun("log", row)
		}
	case "enter":
		if have {
			return m, m.itermocEnter(row)
		}
	case "R":
		st.refresh(m, true)
	case "?":
		return m, m.itermocKeysHelp()
	case "esc":
		m.focus = FocusEditor
	default:
		if have {
			if cmd, ok := m.itermocRowKey(k.String(), row); ok {
				m.itermocRefreshNow()
				return m, cmd
			}
		}
		if cmd, ok := m.itermocSectionKey(k.String()); ok {
			m.itermocRefreshNow()
			return m, cmd
		}
	}
	return m, nil
}

// toggleSection folds or opens a section and keeps the cursor on its header.
func (st *itermocState) toggleSection(sec string) {
	st.collapse[sec] = !st.collapse[sec]
	st.focusHeader(sec)
	st.touch()
}

// focusHeader puts the cursor on the header of sec.
func (st *itermocState) focusHeader(sec string) {
	for i, r := range st.rows() {
		if r.kind == imRowSection && r.sec == sec {
			st.cursor = i
			return
		}
	}
}

// itermocRowKey runs a key that acts on the row under the cursor.
func (m *Model) itermocRowKey(key string, row imRow) (tea.Cmd, bool) {
	switch row.kind {
	case imRowProfile:
		switch key {
		case "u":
			return m.itermocRun("use", row), true
		case "d":
			return m.itermocRun("details", row), true
		case "D":
			return m.itermocRun("duplicate", row), true
		case "t":
			return m.itermocRun("trust", row), true
		}
	case imRowSlot:
		switch key {
		case "k":
			return m.itermocRun("setkey", row), true
		case "e":
			return m.itermocRun("edit", row), true
		case "D":
			return m.itermocRun("remove", row), true
		}
	case imRowService:
		if key == "i" {
			return m.itermocRun("signin", row), true
		}
	}
	return nil, false
}

// itermocSectionKey runs a key of a section's buttons (they work from any row).
func (m *Model) itermocSectionKey(key string) (tea.Cmd, bool) {
	for _, sec := range itermocSectionOrder {
		for _, b := range itermocButtonDefs(sec) {
			if b.key == key {
				return m.itermocButton(sec, b.id), true
			}
		}
	}
	return nil, false
}

// itermocButton runs a [Label (key)] button.
func (m *Model) itermocButton(sec, id string) tea.Cmd {
	switch sec + "/" + id {
	case imSecProfiles + "/new":
		return m.openAIProfileNewPrompt()
	case imSecProfiles + "/json":
		return m.editAIProfiles()
	case imSecModels + "/add":
		return m.openItermocSlotAdd()
	case imSecModels + "/slots":
		return m.editAIModelSlots()
	case imSecServices + "/connect":
		return m.openMCPConnect()
	case imSecServices + "/log":
		return m.showMCPLog()
	case imSecAgents + "/start-claude":
		return m.itermocStartAgent("claude")
	case imSecAgents + "/start-codex":
		return m.itermocStartAgent("codex")
	case imSecAgents + "/copy":
		return m.copyMCPServeConfig()
	case imSecAgents + "/status":
		return m.openMCPServeStatus()
	}
	return nil
}

func (m *Model) itermocStartAgent(id string) tea.Cmd {
	a, ok := agents.ByID(id)
	if !ok {
		return nil
	}
	return m.startAgent(a)
}

// ── Enter: the actions menu of a row ─────────────────────────────────────

// itermocEnter is Enter on a row.
func (m *Model) itermocEnter(row imRow) tea.Cmd {
	st := m.itermocS()
	switch row.kind {
	case imRowSection:
		st.toggleSection(row.sec)
		return nil
	case imRowService:
		return m.openMCPServerActions(row.name)
	}
	return m.openItermocActions(row)
}

// itermocAction is one verb of the menu.
type itermocAction struct{ id, title, hint string }

// itermocActionsFor lists the verbs of a row: safe ones first, risky ones
// last and ending in "…".
func (m *Model) itermocActionsFor(row imRow) (title string, acts []itermocAction) {
	sn := m.itermocS().snap
	switch row.kind {
	case imRowProfile:
		p, ok := sn.profile(row.name)
		if !ok {
			return "", nil
		}
		title = aiSpark() + " " + p.p.Name
		if p.p.Title != "" {
			title += " · " + p.p.Title
		}
		acts = []itermocAction{
			{"use", "Use", "switch the chat to it"},
			{"details", "Details", "steps, models, prompts"},
			{"duplicate", "Duplicate…", "save a copy under a name"},
			{"runs", "Runs", "open AGENTS"},
		}
		if p.p.Source == ai.ProfileSourceProject {
			acts = append(acts, itermocAction{"trust", "Trust…", "this project's prompts steer the AI"})
		}
		acts = append(acts, itermocAction{"json", "Edit JSON", "profiles.json"})
	case imRowSlot:
		s, ok := sn.slot(row.name)
		if !ok {
			return "", nil
		}
		title = "Model slot " + s.name
		if s.key == imKeyMissing || s.key == imKeyUnset {
			acts = append(acts, itermocAction{"setkey", "Set key", "stored in the secret store"})
		}
		switch {
		case s.name == ai.SlotMain:
			acts = append(acts, itermocAction{"edit", "Edit…", "AI: Configure Provider…"},
				itermocAction{"pickmodel", "Pick model…", "the main model"})
		case s.user:
			acts = append(acts, itermocAction{"edit", "Edit…", "provider, model, base URL"})
		case s.name == ai.SlotQuick || s.name == ai.SlotLocal:
			acts = append(acts, itermocAction{"edit", "Edit…", "save your own " + s.name + " slot"})
		}
		acts = append(acts, itermocAction{"slots", "Edit JSON", "config.json ai_models"})
		if s.user {
			acts = append(acts, itermocAction{"remove", "Remove…", "asks first"})
		}
	case imRowAgentStatus:
		title = "Outside agents"
		if m.mcpServe != nil {
			acts = append(acts, itermocAction{"status", "Show status…", "agents and their last calls"})
		} else if sn.mode == "off" {
			acts = append(acts, itermocAction{"serve-on", "Turn on", "let outside agents connect"})
		} else {
			acts = append(acts, itermocAction{"serve-start", "Start listening now", "for this session"})
		}
		for _, a := range itermocAgentRecipes() {
			acts = append(acts, itermocAction{"start-" + a.ID, "Start " + a.Title, "in a terminal tab"})
		}
		acts = append(acts, itermocAction{"copy", "Copy config", "JSON for agent CLIs"})
		if m.mcpServe != nil {
			acts = append(acts, itermocAction{"serve-stop", "Stop listening…", "for this session"})
		}
	case imRowAgent:
		a, ok := sn.agent(row.name)
		if !ok {
			return "", nil
		}
		title = "Outside agent " + a.name
		acts = []itermocAction{
			{"status", "Show status…", "agents and their last calls"},
			{"allowed", "Allowed tools…", "see and revoke Always allow"},
			{"disconnect", "Disconnect…", "it can connect again by itself"},
		}
	}
	return title, acts
}

func (m *Model) openItermocActions(row imRow) tea.Cmd {
	title, acts := m.itermocActionsFor(row)
	if len(acts) == 0 {
		return nil
	}
	items := make([]picker.Item, 0, len(acts))
	for _, a := range acts {
		items = append(items, picker.Item{ID: a.id, Title: a.title, Hint: a.hint})
	}
	m.itermocS().menuRow = row
	m.picker = picker.NewItems(" "+title+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindItermocActions
	return nil
}

// handleItermocPickerSelect routes the actions menu. ok=false: not ours.
func (m *Model) handleItermocPickerSelect(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindItermocActions {
		return nil, false
	}
	row := m.itermocS().menuRow
	cmd := m.itermocRun(id, row)
	m.itermocRefreshNow()
	return cmd, true
}

// itermocRun runs one verb on a row. Every verb is a call into a flow
// that exists without this view.
func (m *Model) itermocRun(verb string, row imRow) tea.Cmd {
	switch verb {
	case "use":
		return m.selectAIProfile(row.name)
	case "details":
		return m.showAIProfileNamed(row.name)
	case "duplicate":
		return m.openAIProfileCopyPrompt(row.name)
	case "runs":
		return m.showAgents("")
	case "trust":
		if p, ok := m.aiS().profiles.Get(row.name); ok && p.Source == ai.ProfileSourceProject {
			return m.askAIProfileTrust(p, aiTrustAfterNone, "")
		}
		return nil
	case "json":
		return m.editAIProfiles()
	case "slots":
		return m.editAIModelSlots()
	case "pickmodel":
		return m.openAIModelPicker()
	case "edit":
		if row.kind == imRowSlot {
			return m.openItermocSlotEdit(row.name)
		}
		return nil
	case "remove":
		if row.kind == imRowSlot {
			return m.openItermocSlotRemove(row.name)
		}
		return nil
	case "setkey":
		return m.itermocSetKey(row.name)
	case "signin":
		if st, ok := m.itermocS().snap.service(row.name); ok && (st.Expired || st.State == mcp.StateSignIn || st.Type == mcp.TypeHTTP) {
			m.aiS().mcpSel = row.name
			return m.onMCPServerAction("signin")
		}
		return nil
	case "log":
		return m.showMCPLog()
	case "status":
		return m.openMCPServeStatus()
	case "serve-on", "serve-start", "serve-stop":
		return m.onMCPServeStatusPicked(verb)
	case "allowed":
		return m.openAIAutoApprovePicker()
	case "disconnect":
		return m.onMCPServeStatusPicked(row.name)
	case "copy":
		return m.copyMCPServeConfig()
	case "start-claude", "start-codex":
		return m.itermocStartAgent(strings.TrimPrefix(verb, "start-"))
	}
	return nil
}

// itermocSetKey opens the key prompt of a slot's provider (the key goes
// to the secret store; the view never shows it). A slot without a provider
// opens AI: Configure Provider….
func (m *Model) itermocSetKey(slot string) tea.Cmd {
	sl, ok := m.itermocS().snap.slot(slot)
	if !ok || sl.provider == "" || sl.provider == ai.KindNone {
		return m.openAIConfigure()
	}
	switch sl.provider {
	case ai.KindClaudeOAuth:
		return m.startClaudeSignIn()
	case ai.KindOllama, ai.KindLMStudio:
		return m.aiInfo("A local provider needs no key")
	}
	cmd := m.openAIKeyPrompt(sl.provider)
	m.aiS().keyOnly = true
	return cmd
}

// itermocTooltip is the hover label of a hit target of the view (ids start
// with "im:"): the name, the keys and the guide section. ok=false: not ours.
func itermocTooltip(id string) (name, key, help string, ok bool) {
	rest, isIM := strings.CutPrefix(id, "im:")
	if !isIM {
		return "", "", "", false
	}
	help = itermocGuidePage + "#the-view"
	kind, rest, _ := strings.Cut(rest, ":")
	switch kind {
	case "sec":
		return "Fold section", "Space", help, true
	case "btn":
		sec, bid, _ := strings.Cut(rest, ":")
		for _, b := range itermocButtonDefs(sec) {
			if b.id == bid {
				return b.label, b.key, help, true
			}
		}
	case "fix":
		at := strings.LastIndex(rest, ":")
		if at < 0 {
			return "", "", "", false
		}
		switch rest[at+1:] {
		case "trust":
			return "Trust this project's profile…", "t", help, true
		case "setkey":
			return "Set the key of this model's provider", "k", help, true
		case "signin":
			return "Sign in to this service", "i", help, true
		}
	case "row":
		sec, _, _ := strings.Cut(rest, "/")
		switch sec {
		case imSecProfiles:
			return "Profile actions: use, details, duplicate, runs, trust, edit JSON", "Enter · u · d · D", help, true
		case imSecModels:
			return "Model slot: edit, set a key, remove", "Enter · e · k · D", help, true
		case imSecServices:
			return "Service actions: sign in, tools, writes, disable, remove", "Enter · i", help, true
		case imSecAgents:
			return "Outside agents: status, allowed tools, disconnect", "Enter", help, true
		}
	}
	return "", "", "", false
}

// ── Mouse ────────────────────────────────────────────────────────────────

func (m Model) handleItermocSidebarMouse(x, y int, t tea.MouseEventType) (Model, tea.Cmd) {
	st := m.itermocS()
	w := m.explorerWidth - 1
	st.w = w
	switch t {
	case tea.MouseWheelUp:
		for i := 0; i < 3; i++ {
			st.move(-1)
		}
		return m, nil
	case tea.MouseWheelDown:
		for i := 0; i < 3; i++ {
			st.move(1)
		}
		return m, nil
	case tea.MouseLeft, tea.MouseRight:
	default:
		return m, nil
	}
	m.focus = FocusExplorer
	var hit *hitTarget
	for _, h := range m.itermocHits(w) {
		if x >= h.X && x < h.X+h.W && y >= h.Y && y < h.Y+h.H {
			h := h
			// A button inside a row wins over the row.
			if hit == nil || strings.HasPrefix(h.ID, "im:btn:") || strings.HasPrefix(h.ID, "im:fix:") {
				hit = &h
			}
		}
	}
	if hit == nil {
		return m, nil
	}
	if m.probed(hit.ID) {
		return m, nil
	}
	id := hit.ID
	switch {
	case strings.HasPrefix(id, "im:sec:"):
		st.toggleSection(strings.TrimPrefix(id, "im:sec:"))
		return m, nil
	case strings.HasPrefix(id, "im:btn:"):
		parts := strings.SplitN(strings.TrimPrefix(id, "im:btn:"), ":", 2)
		cmd := m.itermocButton(parts[0], parts[1])
		m.itermocRefreshNow()
		return m, cmd
	case strings.HasPrefix(id, "im:fix:"):
		rest := strings.TrimPrefix(id, "im:fix:")
		at := strings.LastIndex(rest, ":")
		key, verb := rest[:at], rest[at+1:]
		sec, name, _ := strings.Cut(key, "/")
		row := imRow{sec: sec, name: name}
		for i, r := range st.rows() {
			if r.sec == sec && r.name == name && r.selectable() {
				st.cursor, row = i, r
			}
		}
		cmd := m.itermocRun(verb, row)
		m.itermocRefreshNow()
		return m, cmd
	case strings.HasPrefix(id, "im:row:"):
		key := strings.TrimPrefix(id, "im:row:")
		sec, name, _ := strings.Cut(key, "/")
		for i, r := range st.rows() {
			if r.sec == sec && r.name == name && r.selectable() {
				st.cursor = i
				st.touch()
				// A click selects the row and opens its actions.
				return m, m.itermocEnter(r)
			}
		}
	}
	return m, nil
}

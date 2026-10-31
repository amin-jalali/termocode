package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── Runs: the state, the ways in and the verbs ───────────────────────────
//
// The view lives in the sidebar (activity bar: the green pulse icon) and
// owns one page in the editor area: the overview ("◉ Runs") or one run
// ("◉ fix the toast flicker"). The page is a scratch nvim buffer like the
// Find Results page; while it is the shown buffer the editor area is drawn
// by runs_page.go instead of by nvim. Every verb calls the flow that
// already exists: Stop is the chat's stop, Resume the chat card's resume
// (OM-6) or the budget card's Continue, Pause is agentrun's Pause.

// runsGuidePage is the guide of the view.
const runsGuidePage = "guides/runs.md"

// runsNarrowW: a window narrower than this shows Runs as one page (no
// sidebar list): the compact one-line rows and the compact run page.
const runsNarrowW = 120

// runsOlderKey is the "older…" row's key in the list.
const runsOlderKey = "~older"

// runsState is the Runs view's state (Model.runs; nil until it opens).
type runsState struct {
	// chats are the stored chats (not the open one), read when the view
	// opens and after a run ends; fake: the demo set them (never read).
	chats []ai.Conversation
	fake  bool
	// loadedFor is the open chat when chats were read: another open chat
	// means the one before it is a stored chat now (read them again).
	loadedFor string
	// sel is the selected row's key (a run's, or runsOlderKey).
	sel string
	// filter is the typed filter (task, profile or chat words); profile
	// keeps one profile's runs (openRuns from a profile's "Runs" verb).
	filter, profile string
	typing          bool
	older           bool // the older… rows are open
	scroll          int  // sidebar rows scrolled
	// page: the editor-area page; detail is the run it shows ("" = the
	// overview).
	pageOn            bool
	detail            string
	pageScroll, pageM int // the page's scroll and its last max
	cache             runsDrawCache
	chatFiles         ai.ChatListCache // the parsed chat files (runsLoadChats)
	// others are the other windows' agent roll-ups (OMX-35), read with
	// the chats.
	others []agentroll.Rollup
}

// runsDrawCache keeps the drawn sidebar and page between frames.
type runsDrawCache struct {
	mu              sync.Mutex
	sideKey, pageKy string
	side, page      []string
	pageHits        []runsHit
	sideHits        []runsHit
	// While one frame is drawn (drawing), runsCollect keeps its result
	// for that frame's time: a frame asks for the runs several times
	// (list, spend strip, budget …) and nothing changes inside a frame.
	drawing bool
	drawAt  time.Time
	drawAll []runsItem
}

// runsBeginDraw starts a frame drawn at now (the cache lock is held);
// the returned func ends it.
func (st *runsState) runsBeginDraw(now time.Time) func() {
	st.cache.drawing, st.cache.drawAt, st.cache.drawAll = true, now, nil
	return func() { st.cache.drawing, st.cache.drawAll = false, nil }
}

// runsHit is a click target of the view or the page: cells [x0,x1) of
// row y (canvas coordinates); id is "run:<key>", "older", or a verb.
type runsHit struct {
	x0, x1, y int
	id        string
	label     string
}

func newRunsState() *runsState { return &runsState{} }

// runsS returns the state, making it on first use.
func (m *Model) runsS() *runsState {
	if m.runs == nil {
		m.runs = newRunsState()
	}
	return m.runs
}

// runsVisible reports whether the view or its page is on screen (the
// 1 s tick runs while a run is live and Runs is shown).
func (m Model) runsVisible() bool {
	return (m.showExp && !m.zenMode && m.activity.Active() == activity.ViewRuns) || m.runsPageShown()
}

// runsLoadChats reads the stored chats (the demo's stay).
func (m *Model) runsLoadChats() {
	st := m.runsS()
	if st.fake || m.ai == nil {
		return
	}
	st.chats, st.loadedFor = nil, m.ai.conv.ID
	st.others = agentRollupOthers()
	// Only the chat files that changed since the last load are read; the
	// view only reads the chats (marking a run interrupted is the same
	// change every time).
	for _, c := range m.ai.store.ListCached(&st.chatFiles) {
		aiMarkInterrupted(&c) // only the open chat's run can be live
		st.chats = append(st.chats, c)
	}
}

// revealRuns shows the Runs view (the activity bar item, the palette).
// A narrow window shows the page instead of the sidebar list.
func (m *Model) revealRuns(focus bool) tea.Cmd {
	m.aiS()
	m.runsLoadChats()
	m.activity.SetActive(activity.ViewRuns)
	if m.w > 0 && m.w < runsNarrowW {
		if m.showExp {
			m.showExp = false
			m.applyLayout()
		}
		return tea.Batch(m.runsShowPage("", true), m.aiActivityTick())
	}
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	if focus {
		m.focus = FocusExplorer
	}
	m.runsEnsureSel()
	return m.aiActivityTick()
}

// openRuns opens Runs filtered to one profile's runs ("" = every run).
// Itermoc's "Runs" verb on a profile calls it.
func (m *Model) openRuns(filterProfile string) tea.Cmd {
	st := m.runsS()
	st.profile, st.filter, st.typing, st.sel = strings.TrimSpace(filterProfile), "", false, ""
	return m.revealRuns(true)
}

// runsItems are the runs the list shows now (the filter applied).
func (m *Model) runsItems(now time.Time) []runsItem {
	all := m.runsCollect(now)
	st := m.runsS()
	if st.filter == "" && st.profile == "" {
		return all
	}
	q := strings.ToLower(st.filter)
	out := all[:0:0]
	for _, it := range all {
		if st.profile != "" && it.run.Profile != st.profile {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.task+" "+it.run.Profile+" "+it.chatTitle+" "+aiRunByChip(&it.run)), q) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// runsKeys are the selectable rows in list order: runs (older ones only
// while open) and the older… row.
func (m *Model) runsKeys(items []runsItem) []string {
	var out []string
	older := false
	for _, it := range items {
		if it.group == runsGroupOlder {
			older = true
			continue
		}
		out = append(out, it.key)
	}
	if older {
		out = append(out, runsOlderKey)
		if m.runsS().older {
			for _, it := range items {
				if it.group == runsGroupOlder {
					out = append(out, it.key)
				}
			}
		}
	}
	return out
}

// runsEnsureSel keeps the selection on a row that exists (first: the
// newest live run).
func (m *Model) runsEnsureSel() {
	st := m.runsS()
	keys := m.runsKeys(m.runsItems(runsNow()))
	for _, k := range keys {
		if k == st.sel {
			return
		}
	}
	st.sel = ""
	if len(keys) > 0 {
		st.sel = keys[0]
	}
}

// runsMove moves the selection by dir rows.
func (m *Model) runsMove(dir int) {
	st := m.runsS()
	keys := m.runsKeys(m.runsItems(runsNow()))
	if len(keys) == 0 {
		return
	}
	i := 0
	for k, key := range keys {
		if key == st.sel {
			i = k
		}
	}
	i = max(min(i+dir, len(keys)-1), 0)
	st.sel = keys[i]
	if st.pageOn && st.detail != "" && st.sel != runsOlderKey {
		st.detail, st.pageScroll = st.sel, 0 // the open page follows the list
	}
}

// runsFind is the item with key ("" = the selected one).
func (m *Model) runsFind(key string) (runsItem, bool) {
	picked := key == ""
	if picked {
		m.runsEnsureSel() // a run that started after the view opened
		key = m.runsS().sel
	}
	for _, it := range m.runsCollect(runsNow()) {
		if it.key == key {
			return it, true
		}
	}
	return runsItem{}, false
}

// runsTarget is the run a verb acts on: the page's run, else the
// selected one.
func (m *Model) runsTarget() (runsItem, bool) {
	st := m.runsS()
	if m.runsPageShown() && st.detail != "" {
		return m.runsFind(st.detail)
	}
	return m.runsFind("")
}

// ── The page tab ─────────────────────────────────────────────────────────

func runsPageTmp() string { return filepath.Join(os.TempDir(), "termocode-runs-page") }

// isRunsPagePath says whether a buffer name is the Runs page.
func isRunsPagePath(p string) bool {
	return p == "termocode-runs-page" || strings.HasSuffix(p, "/termocode-runs-page")
}

// runsPageShown reports whether the editor area shows the page now.
func (m Model) runsPageShown() bool {
	return m.runs != nil && m.runs.pageOn && isRunsPagePath(m.editor.Path())
}

// runsPageTitle is the tab name: "◉ Runs" or "◉ <task>".
func (m Model) runsPageTitle() string {
	st := m.runs
	name := "Runs"
	if st != nil && st.detail != "" {
		if it, ok := m.runsFind(st.detail); ok {
			name = rcCut(it.task, 28)
		}
	}
	return runsGlyphLive().String() + " " + name
}

// runsShowPage shows the overview (key "") or run key in the page tab,
// opening the tab when it is not there.
func (m *Model) runsShowPage(key string, focus bool) tea.Cmd {
	st := m.runsS()
	if st.detail != key {
		st.pageScroll = 0
	}
	st.pageOn, st.detail = true, key
	if key != "" {
		st.sel = key
	} else {
		m.runsEnsureSel()
	}
	if focus {
		m.focus = FocusEditor
	}
	if m.nvim == nil {
		return nil
	}
	if !m.runsPageShown() {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`local name = ...
local b = vim.g.termocode_runs_buf
if not (b and vim.api.nvim_buf_is_valid(b)) then
  b = vim.api.nvim_create_buf(true, false)
  vim.bo[b].buftype = 'nofile'
  vim.bo[b].swapfile = false
  vim.bo[b].bufhidden = 'hide'
  pcall(vim.api.nvim_buf_set_name, b, name)
  vim.bo[b].modifiable = false
  vim.g.termocode_runs_buf = b
end
if vim.api.nvim_get_current_buf() ~= b then vim.cmd('keepalt buffer ' .. b) end
vim.cmd('stopinsert')`, runsPageTmp())
	}
	return tea.Batch(m.fetchStateCmd(), m.aiActivityTick())
}

// ── Verbs ────────────────────────────────────────────────────────────────

// runsInfo is a short toast.
func (m *Model) runsInfo(text string) tea.Cmd { return m.aiInfo(text) }

// runsStop asks before it stops the live run (safe answer first).
func (m *Model) runsStop(it runsItem) tea.Cmd {
	if !it.kind.live() {
		return m.runsInfo("This run is not going; nothing to stop")
	}
	body := "\"" + rcCut(it.task, 60) + "\" (" + it.run.Profile + ")\n" +
		"The step that works now ends without its answer. Steps that finished keep their work; " +
		"Resume (r) runs the rest later."
	m.confirm = confirm.Risky("Stop this run?", body,
		confirm.Button{ID: "keep", Title: "Keep running"},
		confirm.Button{ID: "stop", Title: "Stop", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindRunsStop
	m.runsS().cache.sideKey = "" // the run it is about stays selected
	return nil
}

// answerRunsStop is the Stop card's answer.
func (m *Model) answerRunsStop(id string) tea.Cmd {
	if id != "stop" {
		return nil
	}
	s := m.aiS()
	if !s.streaming || s.chatCancel == nil {
		return nil
	}
	return m.aiCancelAll()
}

// runsPause asks the live run to pause before its next step.
func (m *Model) runsPause(it runsItem) tea.Cmd {
	s := m.aiS()
	if it.kind != runsRunning && it.kind != runsStalled {
		if it.kind == runsPaused || it.kind == runsPausing {
			return m.runsInfo("This run is paused already; Resume (r) goes on")
		}
		return m.runsInfo("Only a run that is going can pause")
	}
	if s.run == nil {
		return nil
	}
	s.run.Pause()
	s.runPausing = true
	s.runGen++
	return m.runsInfo("The run pauses after the step that works now")
}

// runsResume goes on with a paused live run (Continue) or runs a stopped
// run's steps that are left (the chat card's Resume, OM-6).
func (m *Model) runsResume(it runsItem) tea.Cmd {
	s := m.aiS()
	switch {
	case it.kind == runsPaused || it.kind == runsPausing:
		if s.run == nil {
			return nil
		}
		if rm := s.aiRunMessage(); rm != nil && rm.Run.State == ai.RunStatePaused {
			rm.Run.State = ai.RunStateRunning
		}
		s.runPausing = false
		s.runGen++
		s.run.Continue()
		return nil
	case it.kind.live():
		return m.runsInfo("This run is going")
	case it.resume == 0:
		return m.runsInfo("This run cannot resume: " + runsNoResumeWhy(it))
	case s.streaming:
		return m.runsInfo("The AI is busy in the open chat; stop it or wait, then resume")
	}
	if !it.open {
		if cmd := m.onAIHistoryPicked(it.chat); m.aiS().conv.ID != it.chat {
			return cmd // the chat did not load (its toast says why)
		}
	}
	cmd := m.aiRetryRun(it.idx)
	m.runsS().sel = it.key
	return tea.Batch(cmd, m.aiActivityTick())
}

// runsNoResumeWhy says why a stopped run has no Resume.
func runsNoResumeWhy(it runsItem) string {
	switch it.kind {
	case runsDone:
		return "it is done"
	}
	return "it is not the last turn of its chat, or its profile changed"
}

// runsOpenChat shows the run's chat (the open one, or loads a stored one
// when the AI is not busy).
func (m *Model) runsOpenChat(it runsItem) tea.Cmd {
	s := m.aiS()
	if !it.open {
		if s.streaming {
			return m.runsInfo("The AI is busy in the open chat; open this chat when it is done")
		}
		return m.onAIHistoryPicked(it.chat)
	}
	s.agents.on = false
	if n := len(it.steps); n > 0 {
		s.cardSel = aiCardID(it.run.ID, it.steps[max(it.cur, 0)].ID)
	}
	return m.openAIPanel(true)
}

// runsShowAgents shows the run in the AGENTS view (the open chat's runs).
func (m *Model) runsShowAgents(it runsItem) tea.Cmd {
	if !it.open {
		return m.runsInfo("AGENTS shows the open chat's runs; Open in chat (c) first")
	}
	return m.showAgents(it.run.ID)
}

// runsUsage is AI: Usage on the run (open chat) or for the session.
func (m *Model) runsUsage(it runsItem, ok bool) tea.Cmd {
	if ok && it.open {
		return m.showAIUsage(it.run.ID)
	}
	return m.showAIUsage("")
}

// runsMore is the "⋯ more" list of a run.
func (m *Model) runsMore(it runsItem) tea.Cmd {
	items := []picker.Item{
		{ID: "profile", Title: "Profile in Itermoc: " + it.run.Profile, Hint: "i"},
		{ID: "copy", Title: "Copy run id", Hint: "y"},
		{ID: "agents", Title: "Show in AGENTS", Hint: "a"},
		{ID: "usage", Title: "Usage by model", Hint: "u"},
		{ID: "chat", Title: "Open in chat", Hint: "c"},
	}
	if it.kind == runsRunning || it.kind == runsStalled {
		items = append([]picker.Item{{ID: "pause", Title: "Pause after this step", Hint: "p"}}, items...)
	}
	m.picker = picker.NewItems(" "+rcCut(it.task, 40)+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindRunsMore
	m.runsS().sel = it.key
	return nil
}

// onRunsMorePicked runs a "more" entry on the selected run.
func (m *Model) onRunsMorePicked(id string) tea.Cmd {
	it, ok := m.runsFind("")
	if !ok {
		return nil
	}
	return m.runsVerb(id, it)
}

// runsVerb runs one verb on run it.
func (m *Model) runsVerb(verb string, it runsItem) tea.Cmd {
	switch verb {
	case "open":
		return m.runsShowPage(it.key, true)
	case "stop":
		return m.runsStop(it)
	case "pause":
		return m.runsPause(it)
	case "resume":
		return m.runsResume(it)
	case "chat":
		return m.runsOpenChat(it)
	case "agents":
		return m.runsShowAgents(it)
	case "usage":
		return m.runsUsage(it, true)
	case "profile":
		return m.openItermocProfile(it.run.Profile)
	case "copy":
		return m.copyWithToast(it.run.ID, "Copied the run id")
	case "more":
		return m.runsMore(it)
	}
	return nil
}

// runsKeyCard is "?": the keys of the view.
func (m *Model) runsKeyCard() tea.Cmd {
	return m.openKeyCard("Runs keys", keyRows(
		[]string{"The list and the page"},
		[]string{"↑ ↓", "pick a run"},
		[]string{"Enter", "open the run (on its page: more)"},
		[]string{"x", "stop the run (asks first)"},
		[]string{"p", "pause after the step that works now"},
		[]string{"r", "resume: go on, or run the steps that are left"},
		[]string{"c", "open the run's chat"},
		[]string{"a", "show the run in AGENTS"},
		[]string{"u", "usage by model"},
		[]string{"i", "the run's profile in Itermoc (no run: the profiles)"},
		[]string{"y", "copy the run id"},
		[]string{"/", "filter the runs"},
		[]string{"Esc", "back: the page to the list, the filter off"},
	), help.Loc{Page: runsGuidePage, Anchor: "keys"}, nil)
}

// runsCommonKey runs a verb key of the list and the page on run it.
func (m *Model) runsCommonKey(key string, it runsItem, have bool) (tea.Cmd, bool) {
	verbs := map[string]string{"x": "stop", "p": "pause", "r": "resume", "c": "chat", "a": "agents",
		"i": "profile", "y": "copy"}
	if v, ok := verbs[key]; ok {
		if !have {
			switch key {
			case "c": // the empty page's "Open the chat (c)"
				return m.openAIPanel(true), true
			case "i": // the empty page's "Profiles in Itermoc (i)"
				return m.revealItermocView(true, imSecProfiles), true
			}
			return m.runsInfo("No run is picked"), true
		}
		return m.runsVerb(v, it), true
	}
	switch key {
	case "u":
		return m.runsUsage(it, have), true
	case "?":
		return m.runsKeyCard(), true
	case "/":
		st := m.runsS()
		st.typing = true
		if st.profile != "" && st.filter == "" {
			st.profile = ""
		}
		return nil, true
	}
	return nil, false
}

// runsFilterKey edits the filter while it is typed. ok=false: not taken.
func (m *Model) runsFilterKey(k tea.KeyMsg) bool {
	st := m.runsS()
	if !st.typing {
		return false
	}
	switch k.Type {
	case tea.KeyEsc:
		st.typing, st.filter = false, ""
	case tea.KeyEnter:
		st.typing = false
	case tea.KeyBackspace:
		if r := []rune(st.filter); len(r) > 0 {
			st.filter = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		if !k.Alt {
			st.filter += string(k.Runes)
			if k.Type == tea.KeySpace {
				st.filter += " "
			}
		}
	default:
		return false
	}
	m.runsEnsureSel()
	return true
}

// handleRunsSidebarKey runs a key of the focused list.
func (m *Model) handleRunsSidebarKey(k tea.KeyMsg) (Model, tea.Cmd) {
	if m.runsFilterKey(k) {
		return *m, nil
	}
	st := m.runsS()
	key := k.String()
	it, have := m.runsFind("")
	switch key {
	case "up", "k":
		m.runsMove(-1)
		return *m, nil
	case "down", "j":
		m.runsMove(1)
		return *m, nil
	case "enter", " ":
		if st.sel == runsOlderKey {
			st.older = !st.older
			return *m, nil
		}
		if have {
			return *m, m.runsShowPage(it.key, true)
		}
		return *m, m.runsShowPage("", true)
	case "esc":
		if st.filter != "" || st.profile != "" {
			st.filter, st.profile = "", ""
			m.runsEnsureSel()
		}
		return *m, nil
	}
	if cmd, ok := m.runsCommonKey(key, it, have); ok {
		return *m, cmd
	}
	return *m, nil
}

// handleRunsPageKey runs a key pressed on the page (the editor area has
// the keys). ok=false lets it go on.
func (m *Model) handleRunsPageKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.runsFilterKey(k) {
		return nil, true
	}
	st := m.runsS()
	key := k.String()
	it, have := m.runsTarget()
	switch key {
	case "esc":
		if st.detail != "" {
			st.detail, st.pageScroll = "", 0
			return nil, true
		}
		if st.filter != "" || st.profile != "" {
			st.filter, st.profile = "", ""
			m.runsEnsureSel()
			return nil, true
		}
		if m.showExp && m.activity.Active() == activity.ViewRuns {
			m.focus = FocusExplorer
		}
		return nil, true
	case "enter":
		switch {
		case st.detail != "" && have:
			return m.runsMore(it), true
		case st.sel == runsOlderKey:
			st.older = !st.older
			return nil, true
		case have:
			return m.runsShowPage(it.key, true), true
		}
		return nil, true
	case "up", "k":
		if st.detail != "" {
			st.pageScroll = max(st.pageScroll-1, 0)
		} else {
			m.runsMove(-1)
		}
		return nil, true
	case "down", "j":
		if st.detail != "" {
			st.pageScroll = min(st.pageScroll+1, max(st.pageM, 0))
		} else {
			m.runsMove(1)
		}
		return nil, true
	case "pgup":
		st.pageScroll = max(st.pageScroll-5, 0)
		return nil, true
	case "pgdown":
		st.pageScroll = min(st.pageScroll+5, max(st.pageM, 0))
		return nil, true
	}
	if cmd, ok := m.runsCommonKey(key, it, have); ok {
		return cmd, true
	}
	if k.Type == tea.KeyRunes && !k.Alt && !k.Paste {
		return nil, true // nothing to type into
	}
	return nil, false
}

// routeToRunsPage gives the page its keys (while the editor has the
// focus) and its clicks. ok is false when the message goes on.
func (m Model) routeToRunsPage(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.focus != FocusEditor {
			return m, nil, false
		}
		cmd, ok := m.handleRunsPageKey(msg)
		return m, cmd, ok
	case tea.MouseMsg:
		return m.runsPageMouse(msg)
	}
	return m, nil, false
}

// dispatchRunsPalette runs the Runs palette entries.
func (m *Model) dispatchRunsPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "ai-runs":
		return m.revealRuns(true), true
	}
	return nil, false
}

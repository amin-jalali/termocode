package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
)

// ── Itermoc: the detail page ─────────────────────────────────────────────
//
// The list (itermoc_view.go) is the master; the page is the detail. It is
// ONE tab in the editor area ("✦ review"): selecting another row changes
// what that tab shows, it never opens a second one. The tab is a scratch
// nvim buffer (like Find Results), so it closes, switches and orders like
// any tab; while it is the shown buffer the editor area is drawn by
// itermoc_page_view.go instead of by nvim.
//
// The page has three pills at the bottom (the verbs most used); Enter opens
// today's actions menu for the rest. Every verb is a call into the flow
// that already exists (itermocRun); the page never edits anything itself.

// itermocPageState is which object the page shows.
type itermocPageState struct {
	sec, name string // sec "overview": the groups' summary
	scroll    int    // body rows scrolled from the top
	maxScroll int    // set by the last draw
}

// itermocPageTmp is the buffer name of the page (a temp path, so sessions
// and recents skip it like Find Results).
func itermocPageTmp() string { return filepath.Join(os.TempDir(), "termocode-itermoc-page") }

// isItermocPagePath says whether a buffer name is the detail page.
func isItermocPagePath(p string) bool {
	return p == "termocode-itermoc-page" || strings.HasSuffix(p, "/termocode-itermoc-page")
}

// itermocPageShown reports whether the editor area shows the page now.
func (m Model) itermocPageShown() bool {
	return m.itermocPage != nil && isItermocPagePath(m.editor.Path())
}

// itermocPageObjectOf says which page a list row stands for.
func itermocPageObjectOf(row imRow) (sec, name string, ok bool) {
	switch row.kind {
	case imRowSection:
		return "overview", "", true
	case imRowProfile, imRowSlot, imRowService:
		return row.sec, row.name, true
	case imRowAgentStatus, imRowAgent:
		return row.sec, row.name, true
	}
	return "", "", false
}

// itermocPageRow is the list row of the object the page shows.
func (m Model) itermocPageRow() (imRow, bool) {
	p := m.itermocPage
	if p == nil {
		return imRow{}, false
	}
	switch p.sec {
	case "overview":
		return imRow{kind: imRowSection, sec: imSecProfiles}, true
	case imSecProfiles:
		if _, ok := m.itermocSnapOf().profile(p.name); ok {
			return imRow{kind: imRowProfile, sec: p.sec, name: p.name}, true
		}
	case imSecModels:
		if _, ok := m.itermocSnapOf().slot(p.name); ok {
			return imRow{kind: imRowSlot, sec: p.sec, name: p.name}, true
		}
	case imSecServices:
		if _, ok := m.itermocSnapOf().service(p.name); ok {
			return imRow{kind: imRowService, sec: p.sec, name: p.name}, true
		}
	case imSecAgents:
		if p.name == "status" {
			return imRow{kind: imRowAgentStatus, sec: p.sec, name: p.name}, true
		}
		if _, ok := m.itermocSnapOf().agent(p.name); ok {
			return imRow{kind: imRowAgent, sec: p.sec, name: p.name}, true
		}
	}
	return imRow{}, false
}

// itermocSnapOf is the view's snapshot (read again when it is old).
func (m Model) itermocSnapOf() itermocSnap {
	st := m.itermoc
	if st == nil {
		st = newItermocState()
		st.refresh(m, false)
	} else if st.stale() {
		st.refresh(m, false)
	}
	return st.snap
}

// itermocPageTitle is the tab name: "✦ review".
func (m Model) itermocPageTitle() string {
	p := m.itermocPage
	name := "AI"
	switch {
	case p == nil:
	case p.sec == "overview":
	case p.name == "status":
		name = "Outside agents"
	case strings.HasPrefix(p.name, "agent:"):
		if a, ok := m.itermocSnapOf().agent(p.name); ok {
			name = a.name
		}
	case p.name != "":
		name = p.name
	}
	return aiSpark() + " " + name
}

// itermocShowPage shows the page of row in the page tab, opening the tab
// when it is not there. focus gives the page the keys; else the list
// keeps them (the preview of a file tree).
func (m *Model) itermocShowPage(row imRow, focus bool) tea.Cmd {
	sec, name, ok := itermocPageObjectOf(row)
	if !ok {
		return nil
	}
	p := m.itermocPage
	if p == nil {
		p = &itermocPageState{}
		m.itermocPage = p
	}
	if p.sec != sec || p.name != name {
		p.scroll = 0
	}
	p.sec, p.name = sec, name
	if focus {
		m.focus = FocusEditor
	}
	if m.nvim == nil {
		return nil
	}
	if !m.itermocPageShown() {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`local name = ...
local b = vim.g.termocode_itermoc_buf
if not (b and vim.api.nvim_buf_is_valid(b)) then
  b = vim.api.nvim_create_buf(true, false)
  vim.bo[b].buftype = 'nofile'
  vim.bo[b].swapfile = false
  vim.bo[b].bufhidden = 'hide'
  pcall(vim.api.nvim_buf_set_name, b, name)
  vim.bo[b].modifiable = false
  vim.g.termocode_itermoc_buf = b
end
if vim.api.nvim_get_current_buf() ~= b then vim.cmd('keepalt buffer ' .. b) end
vim.cmd('stopinsert')`, itermocPageTmp())
	}
	return m.fetchStateCmd()
}

// itermocSyncPage keeps a shown page on the row under the list's cursor
// (↑↓ in the list change the page at once). A page that is not shown, or
// a cursor on a row without a page, changes nothing.
func (m *Model) itermocSyncPage() tea.Cmd {
	if !m.itermocPageShown() || m.itermoc == nil {
		return nil
	}
	row, ok := m.itermoc.current()
	if !ok {
		return nil
	}
	sec, name, ok := itermocPageObjectOf(row)
	if !ok || (sec == m.itermocPage.sec && name == m.itermocPage.name) {
		return nil
	}
	return m.itermocShowPage(row, false)
}

// ── Keys and clicks ──────────────────────────────────────────────────────

// routeToItermocPage gives the page its keys (while the editor has the
// focus) and its clicks. ok is false when the message goes on.
func (m Model) routeToItermocPage(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.focus != FocusEditor {
			return m, nil, false
		}
		cmd, ok := m.itermocPageKey(msg)
		return m, cmd, ok
	case tea.MouseMsg:
		return m.itermocPageMouse(msg)
	}
	return m, nil, false
}

// itermocPageKey runs a key pressed on the page. Plain letters never reach
// the (empty) buffer behind it.
func (m *Model) itermocPageKey(k tea.KeyMsg) (tea.Cmd, bool) {
	key := k.String()
	row, have := m.itermocPageRow()
	if have && row.kind != imRowSection {
		if cmd, ok := m.itermocRowKey(key, row); ok {
			m.itermocRefreshNow()
			return cmd, true
		}
	}
	if cmd, ok := m.itermocSectionKey(key); ok {
		m.itermocRefreshNow()
		return cmd, true
	}
	p := m.itermocPage
	switch key {
	case "enter":
		if have {
			return m.itermocPageMore(row), true
		}
		return nil, true
	case "esc":
		if m.showExp && m.activity.Active() == activity.ViewItermoc {
			m.focus = FocusExplorer
		}
		return nil, true
	case "?":
		return m.itermocKeysHelp(), true
	case "j", "down":
		p.scroll = min(p.scroll+1, max(p.maxScroll, 0))
		return nil, true
	case "up", "pgup":
		p.scroll = max(p.scroll-1, 0)
		return nil, true
	case "pgdown":
		p.scroll = min(p.scroll+5, max(p.maxScroll, 0))
		return nil, true
	}
	if k.Type == tea.KeyRunes && !k.Alt && !k.Paste {
		return nil, true // a letter, digit or symbol: nothing to type into
	}
	return nil, false
}

// itermocPageMore is Enter on the page: today's actions menu of the object.
func (m *Model) itermocPageMore(row imRow) tea.Cmd {
	if row.kind == imRowSection {
		return nil
	}
	return m.itermocEnter(row)
}

// itermocPageVerb runs a pill or a step box.
func (m *Model) itermocPageVerb(verb string, row imRow) tea.Cmd {
	defer m.itermocRefreshNow()
	switch {
	case verb == "new":
		return m.itermocButton(imSecProfiles, "new")
	case verb == "more":
		return m.itermocPageMore(row)
	case strings.HasPrefix(verb, "step:"):
		// TODO(#12, feat/profile-form): open the step form of this step. Until
		// that branch is merged the step box opens the profile file.
		return m.itermocRun("edit", row)
	case verb == "jump":
		// The overview's "needs you" lines: go to the object.
		return nil
	}
	return m.itermocRun(verb, row)
}

func (m Model) itermocPageMouse(msg tea.MouseMsg) (Model, tea.Cmd, bool) {
	pane := m.editorPaneLayout()
	if msg.X < pane.X || msg.X >= pane.X+pane.W || msg.Y < pane.Top || msg.Y >= pane.Top+pane.H {
		return m, nil, false
	}
	switch {
	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		p := m.itermocPage
		p.scroll = min(p.scroll+3, max(p.maxScroll, 0))
		return m, nil, true
	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		p := m.itermocPage
		p.scroll = max(p.scroll-3, 0)
		return m, nil, true
	case msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress:
		return m, nil, false
	}
	m.focus = FocusEditor
	row, have := m.itermocPageRow()
	for _, h := range m.itermocPageTargets(pane) {
		if msg.X >= h.X && msg.X < h.X+h.W && msg.Y >= h.Y && msg.Y < h.Y+h.H {
			if m.probed(h.ID) {
				return m, nil, true
			}
			verb := strings.TrimPrefix(h.ID, "im:pg:")
			if !have && verb != "new" {
				return m, nil, true
			}
			return m, m.itermocPageVerb(verb, row), true
		}
	}
	if m.mouseProbe != nil {
		m.probed("im:pg")
	}
	return m, nil, true
}

// itermocPageTip is the hover label of a pill or box of the page.
func itermocPageTip(verb string) (name, key string, ok bool) {
	if strings.HasPrefix(verb, "step:") {
		return "Edit this step", "e", true
	}
	switch verb {
	case "use":
		return "Use this profile in the chat", "u", true
	case "edit":
		return "Edit", "e", true
	case "runs":
		return "Show the runs in AGENTS", "r", true
	case "trust":
		return "Trust this project's profile…", "t", true
	case "more":
		return "More actions", "Enter", true
	case "setkey":
		return "Set the key of this model's provider", "k", true
	case "signin":
		return "Sign in to this service", "i", true
	case "log":
		return "Show the log", "l", true
	case "new":
		return "New profile", "n", true
	case "start-claude":
		return "Start Claude Code in a terminal tab", "s", true
	case "copy":
		return "Copy the termocode server config", "y", true
	case "status":
		return "Show the status screen", "T", true
	}
	return "", "", false
}

// slotUsers are the names of the profiles with a step on slot.
func (sn itermocSnap) slotUsers(slot string) []string {
	var out []string
	for _, p := range sn.profiles {
		for _, st := range p.p.Steps {
			if p.p.SlotFor(st) == slot {
				out = append(out, p.p.Name)
				break
			}
		}
	}
	return out
}

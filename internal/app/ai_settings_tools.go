package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// AI tools for settings, layout and the whole IDE state (Group K2):
//
//   - settings_list (auto): the settings the AI may change, with value,
//     type, choices and help. Never settings (aiSettingPolicies) are left
//     out.
//   - settings_set (confirm, class settings): validates the value, then
//     applies and saves it through applySetting, the same code the
//     Settings UI runs. Never keys and anything that looks like a secret
//     are refused before the dialog.
//   - layout_set (auto): sidebar / bottom panel / zen / AI panel, through
//     the same code as the mouse drags and toggles; sizes are clamped like
//     a drag and saved like one. It never hides the AI panel: the chat
//     that called it lives there.
//   - get_ide_state (auto): focus, views, panels, layout, theme, git, LSP,
//     problems, debug and runs as JSON.

// aiSecretWords mark keys that hold secrets. No such key is a setting
// today; the check keeps a future one out of reach.
var aiSecretWords = []string{"key", "token", "secret", "credential", "password", "auth"}

// aiSettingEntry is a setting the AI may change (gate is not never and it
// has a Settings row).
func aiSettingEntry(key string) (SettingEntry, bool) {
	p, ok := aiSettingPolicies[key]
	if !ok || p.Gate == aiGateNever {
		return SettingEntry{}, false
	}
	for _, e := range SettingsCatalog() {
		if e.Key == key && e.ID != "" {
			return e, true
		}
	}
	return SettingEntry{}, false
}

// aiExposedSettings are the settings_list rows, in Settings order.
func aiExposedSettings() []SettingEntry {
	var out []SettingEntry
	for _, e := range SettingsCatalog() {
		if x, ok := aiSettingEntry(e.Key); ok {
			out = append(out, x)
		}
	}
	return out
}

// aiSettingChoices are the allowed values of e (theme ids for theme).
func aiSettingChoices(e SettingEntry) []string {
	if e.Key == "theme" {
		var ids []string
		for _, t := range theme.AvailableThemes() {
			ids = append(ids, t.ID)
		}
		return ids
	}
	if e.Type == "bool" {
		return []string{"true", "false"}
	}
	return e.Choices
}

// aiSettingsList is the settings_list result (JSON array).
func aiSettingsList() string {
	type row struct {
		Key         string   `json:"key"`
		Type        string   `json:"type"`
		Value       string   `json:"value"`
		Default     string   `json:"default"`
		Choices     []string `json:"choices,omitempty"`
		Description string   `json:"description"`
	}
	cfg := loadSettings()
	var rows []row
	for _, e := range aiExposedSettings() {
		r, _ := findSettingsRow(e.ID)
		typ := e.Type
		if e.Key == "theme" {
			typ = "enum"
		}
		rows = append(rows, row{Key: e.Key, Type: typ, Value: currentSettingValue(r, cfg), Default: e.Default,
			Choices: aiSettingChoices(e), Description: e.Description})
	}
	b, _ := json.Marshal(rows)
	return string(b)
}

// aiSettingRange is the allowed range of an int setting.
func aiSettingRange(key string) (lo, hi int) {
	switch key {
	case "tab_size":
		return 1, 16
	case "search_max_results":
		return 0, 1 << 30
	case "font_delta":
		return -20, 20
	}
	return -1 << 30, 1 << 30
}

// aiValidateSetting checks a settings_set call. It returns the Settings
// row and the value in the form the row stores ("yes" → "true").
func aiValidateSetting(key, value string) (settingsRow, string, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	lk := strings.ToLower(key)
	for _, w := range aiSecretWords {
		if strings.Contains(lk, w) {
			return settingsRow{}, "", fmt.Errorf("setting %q is not for the AI: %s", key, aiNoAIConfig)
		}
	}
	p, ok := aiSettingPolicies[key]
	if !ok {
		var keys []string
		for _, e := range aiExposedSettings() {
			keys = append(keys, e.Key)
		}
		return settingsRow{}, "", fmt.Errorf("unknown setting %q; settings: %s (see settings_list)", key, strings.Join(keys, ", "))
	}
	if p.Gate == aiGateNever {
		return settingsRow{}, "", fmt.Errorf("setting %q is not for the AI: %s", key, p.Reason)
	}
	e, ok := aiSettingEntry(key)
	if !ok {
		return settingsRow{}, "", fmt.Errorf("setting %q can not be set here", key)
	}
	row, _ := findSettingsRow(e.ID)
	switch e.Type {
	case "bool":
		b, err := parseBoolLoose(value)
		if err != nil {
			return row, "", fmt.Errorf("%s: expected true or false, got %q", key, value)
		}
		return row, strconv.FormatBool(b), nil
	case "int":
		n, err := strconv.Atoi(value)
		lo, hi := aiSettingRange(key)
		if err != nil {
			return row, "", fmt.Errorf("%s: expected an integer, got %q", key, value)
		}
		if n < lo || n > hi {
			if hi == 1<<30 {
				return row, "", fmt.Errorf("%s: expected an integer ≥ %d", key, lo)
			}
			return row, "", fmt.Errorf("%s: expected an integer %d..%d", key, lo, hi)
		}
		return row, strconv.Itoa(n), nil
	}
	if choices := aiSettingChoices(e); len(choices) > 0 {
		for _, c := range choices {
			if strings.EqualFold(c, value) {
				return row, c, nil
			}
		}
		return row, "", fmt.Errorf("%s: %q is not allowed; expected one of: %s", key, value, strings.Join(choices, ", "))
	}
	return row, value, nil
}

// aiSetSetting applies a validated setting like the Settings UI and
// reads it back from config.json.
func (m *Model) aiSetSetting(row settingsRow, key, value string) (string, tea.Cmd, error) {
	cmd := m.applySetting(row.ID, value)
	if now := currentSettingValue(row, loadSettings()); now != value {
		return "", cmd, fmt.Errorf("could not save %s (it is %q now)", key, now)
	}
	out := key + " = " + value + " (applied and saved)"
	switch key {
	case "font_delta", "persistent_session":
		out = key + " = " + value + " (saved; applies the next time termocode starts)"
	}
	return out, cmd, nil
}

// aiLayoutKeys are the layout_set arguments.
var aiLayoutKeys = []string{"sidebar_width", "panel_rows", "zen", "sidebar", "panel", "ai_panel"}

// aiLayoutSet runs one layout_set call on the Model.
func (m *Model) aiLayoutSet(a map[string]any) (string, tea.Cmd, error) {
	has := func(k string) bool { _, ok := a[k]; return ok && a[k] != nil }
	given := false
	for _, k := range aiLayoutKeys {
		given = given || has(k)
	}
	if !given {
		return "", nil, errors.New("nothing to change; give at least one of " + strings.Join(aiLayoutKeys, ", "))
	}
	if has("ai_panel") && !ai.ArgBool(a, "ai_panel") {
		return "", nil, errors.New("the AI panel shows this chat, so the AI can not hide it; the user closes it with Alt+A")
	}
	var notes []string
	var cmds []tea.Cmd
	if has("zen") {
		if on := ai.ArgBool(a, "zen"); on != m.zenMode {
			cmds = append(cmds, m.toggleZenMode())
		}
		if m.zenMode {
			notes = append(notes, "zen on: only the editor shows, this chat too is hidden until the user leaves zen (View: Zen Mode)")
		} else {
			notes = append(notes, "zen off")
		}
	}
	if has("ai_panel") { // true: show it (leaves zen, which hides it)
		if m.zenMode && !has("zen") {
			cmds = append(cmds, m.toggleZenMode())
		}
		if !m.actionsOpen {
			m.toggleActionsPanel()
		} else if !m.actionsPinned {
			m.setActionsPinned(true)
		}
		m.applyLayout()
		notes = append(notes, "AI panel shown")
	}
	if has("sidebar") {
		m.showExp = ai.ArgBool(a, "sidebar")
		m.applyLayout()
		notes = append(notes, map[bool]string{true: "sidebar shown", false: "sidebar hidden"}[m.showExp])
	}
	if has("panel") {
		want := ai.ArgBool(a, "panel")
		if want && !m.termOpen {
			m.toggleTerminalPanel()
		} else if !want && m.termOpen {
			m.hideTerminalPanel()
		}
		m.applyLayout()
		m.resizeTerminalSplit()
		switch {
		case m.termOpen == want:
			notes = append(notes, map[bool]string{true: "panel open", false: "panel closed"}[want])
		default:
			notes = append(notes, "panel could not be changed (the editor is not running)")
		}
	}
	if has("sidebar_width") {
		n := ai.ArgInt(a, "sidebar_width")
		w := n
		// Same limits as dragging the sidebar edge (mouse.go).
		w = max(w, explorerMinWidth)
		w = min(w, explorerMaxWidth)
		if room := m.w - activity.Width - editorScrollbarWidth - 10; room > 0 && w > room {
			w = room
		}
		m.explorerWidth = w
		m.applyLayout()
		m.persistExplorerWidth()
		note := fmt.Sprintf("sidebar width %d", m.explorerWidth)
		if m.explorerWidth != n {
			note += fmt.Sprintf(" (%d clamped to %d..%d and the window)", n, explorerMinWidth, explorerMaxWidth)
		}
		if !m.showExp || m.zenMode {
			note += "; the sidebar is hidden, the width applies when it shows"
		}
		notes = append(notes, note)
	}
	if has("panel_rows") {
		n := ai.ArgInt(a, "panel_rows")
		// Same limits as dragging the panel's tab bar (mouse.go).
		r := max(n, terminalRowsMin)
		if hi := m.h - 8; hi >= terminalRowsMin && r > hi {
			r = hi
		}
		m.terminalMinimized = false
		m.terminalRows = r
		m.applyLayout()
		m.resizeTerminalSplit()
		m.persistTerminalRows()
		note := fmt.Sprintf("panel rows %d", r)
		if r != n {
			note += fmt.Sprintf(" (%d clamped to %d..%d)", n, terminalRowsMin, max(m.h-8, terminalRowsMin))
		}
		if !m.termOpen {
			note += "; the panel is closed, the height applies when it opens"
		}
		notes = append(notes, note)
	}
	return strings.Join(notes, "; "), tea.Batch(cmds...), nil
}

// aiViewName names a sidebar view.
func aiViewName(v activity.View) string {
	switch v {
	case activity.ViewFiles:
		return "explorer"
	case activity.ViewSearch:
		return "search"
	case activity.ViewGit:
		return "git"
	case activity.ViewRun:
		return "run"
	case activity.ViewSettings:
		return "settings"
	case activity.ViewTests:
		return "tests"
	}
	return "unknown"
}

// aiIDEStateLua reads the LSP clients of the editor window's buffer and
// the diagnostic counts of all buffers.
const aiIDEStateLua = `
local function editorwin(w)
  local b = vim.api.nvim_win_get_buf(w)
  return vim.bo[b].buftype == '' and not vim.w[w].termocode_panel
end
local win = vim.api.nvim_get_current_win()
if not editorwin(win) then
  for _, w in ipairs(vim.api.nvim_tabpage_list_wins(0)) do
    if editorwin(w) then win = w break end
  end
end
local buf = vim.api.nvim_win_get_buf(win)
local get = vim.lsp.get_clients or vim.lsp.get_active_clients
local names = {}
for _, c in ipairs(get({ bufnr = buf })) do names[#names + 1] = c.name end
local n = { 0, 0, 0, 0 }
for _, d in ipairs(vim.diagnostic.get()) do n[d.severity] = (n[d.severity] or 0) + 1 end
return vim.json.encode({ lsp = table.concat(names, '\n'), e = n[1], w = n[2], i = n[3] + n[4] })
`

// aiIDEState is the get_ide_state result (JSON).
func (m *Model) aiIDEState(rel func(string) string) (string, error) {
	type counts struct {
		Errors   int `json:"errors"`
		Warnings int `json:"warnings"`
		Info     int `json:"info"`
	}
	var probs counts
	probs.Errors, probs.Warnings, probs.Info = m.problems.counts()
	lsp := []string{}
	if m.nvim != nil {
		var raw string
		if err := m.nvim.ExecLuaResult(aiIDEStateLua, &raw); err == nil {
			var r struct {
				LSP     string `json:"lsp"`
				E, W, I int
			}
			if json.Unmarshal([]byte(raw), &r) == nil {
				if r.LSP != "" {
					lsp = strings.Split(r.LSP, "\n")
				}
				probs = counts{r.E, r.W, r.I}
			}
		}
	}
	var tabs []string
	activeTab := ""
	for _, e := range m.panelBarEntries() {
		title := panelKindTitle(e.Kind)
		if e.Kind == panelKindTerminal {
			title = "Terminal: " + m.terminalTabs[e.Index].Name
		}
		tabs = append(tabs, title)
		if m.panelEntryActive(e) {
			activeTab = title
		}
	}
	if !m.termOpen {
		activeTab = ""
	}
	termTabs, hiddenTabs := []string{}, []string{}
	for _, t := range m.terminalTabs {
		termTabs = append(termTabs, t.Name)
	}
	for _, t := range m.hiddenTerminalTabs {
		hiddenTabs = append(hiddenTabs, t.Name)
	}
	running := []string{}
	if m.tasks != nil {
		for _, buf := range m.tasks.order {
			if r := m.tasks.runs[buf]; r != nil && r.Running {
				running = append(running, r.Task.Label)
			}
		}
	}
	sort.Strings(running)
	debug := map[string]any{"active": m.dapSessionActive}
	if m.debug != nil && m.debug.status != "" {
		debug["status"] = m.debug.status
	}
	file := ""
	if p := m.editor.Path(); p != "" {
		file = rel(p)
	}
	themeID := m.themeID
	if themeID == "" {
		themeID = currentSettingValue(settingsRow{ID: "setting-theme", Default: "vscode-dark-plus"}, loadSettings())
	}
	st := map[string]any{
		"focus":       m.aiFocusName(),
		"window":      map[string]int{"cols": m.w, "rows": m.h},
		"active_file": file,
		"sidebar": map[string]any{"visible": m.showExp && !m.zenMode, "view": aiViewName(m.activity.Active()),
			"width": m.explorerWidth},
		"panel": map[string]any{"open": m.termOpen, "active_tab": activeTab, "tabs": tabs,
			"rows": m.terminalRows, "minimized": m.terminalMinimized},
		"terminal_tabs":        termTabs,
		"terminal_tabs_hidden": hiddenTabs,
		"ai_panel":             map[string]any{"visible": m.actionsPanelVisible(), "width": m.actionsColumnWidth()},
		"zen":                  m.zenMode,
		"theme":                themeID,
		"icons":                iconModeName(theme.CurrentIconMode),
		"git": map[string]any{"repo": m.gitIsRepo, "branch": m.gitBranch.Name, "ahead": m.gitBranch.Ahead,
			"behind": m.gitBranch.Behind, "changed_files": len(m.gitFiles)},
		"lsp_clients":   lsp,
		"problems":      probs,
		"debug":         debug,
		"tests_running": m.testView != nil && m.testView.running,
		"tasks_running": running,
	}
	b, err := json.Marshal(st)
	return string(b), err
}

// aiSettingsTools are settings_list, settings_set, layout_set and
// get_ide_state; they act on the Model through call.
func aiSettingsTools(root string, call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	boolean := func(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
	rel := aiRel(root)
	return []ai.Tool{
		{
			Name: "settings_list",
			Description: "List the IDE settings you may change with settings_set: key, type, current value, " +
				"allowed values (all theme ids for theme) and help.",
			Describe: func(map[string]any) string { return "List settings" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				return call(ctx, func(*Model) (string, tea.Cmd, error) { return aiSettingsList(), nil, nil })
			},
		},
		{
			Name: "settings_set",
			Description: "Change one IDE setting (see settings_list) and save it to config.json; it applies at once " +
				"(theme repaints, tab_size sets the editor's indent). The user confirms first.",
			Params:   map[string]any{"key": str("setting key, e.g. theme, tab_size, word_wrap, icons"), "value": str("new value")},
			Required: []string{"key", "value"},
			Mutating: true, Class: ai.ClassSettings,
			Describe: func(a map[string]any) string {
				return "Set " + ai.ArgString(a, "key") + " = " + ai.ArgString(a, "value")
			},
			Validate: func(a map[string]any) error {
				_, _, err := aiValidateSetting(ai.ArgString(a, "key"), ai.ArgString(a, "value"))
				return err
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				key := strings.TrimSpace(ai.ArgString(a, "key"))
				row, value, err := aiValidateSetting(key, ai.ArgString(a, "value"))
				if err != nil {
					return "", err
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiSetSetting(row, key, value) })
			},
		},
		{
			Name: "layout_set",
			Description: "Change the IDE layout: sidebar width (columns) and shown / hidden, bottom panel height (rows) " +
				"and open / closed, zen mode, show the AI panel. Give only what should change; sizes are clamped. " +
				"The AI panel can not be hidden (this chat lives in it).",
			Params: map[string]any{
				"sidebar_width": num(fmt.Sprintf("sidebar width in columns (%d..%d)", explorerMinWidth, explorerMaxWidth)),
				"panel_rows":    num(fmt.Sprintf("bottom panel height in rows (at least %d)", terminalRowsMin)),
				"zen":           boolean("zen mode: only the editor shows (hides this chat too)"),
				"sidebar":       boolean("show (true) or hide (false) the sidebar"),
				"panel":         boolean("open (true) or close (false) the bottom panel; shells keep running"),
				"ai_panel":      boolean("true shows the AI chat panel; false is refused"),
			},
			Describe: func(a map[string]any) string {
				var parts []string
				for _, k := range aiLayoutKeys {
					if v, ok := a[k]; ok && v != nil {
						parts = append(parts, fmt.Sprintf("%s=%v", k, v))
					}
				}
				return "Layout " + strings.Join(parts, " ")
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiLayoutSet(a) })
			},
		},
		{
			Name: "get_ide_state",
			Description: "Read the whole IDE state as JSON: focus, sidebar view, bottom panel tabs, terminal tabs, " +
				"layout sizes, zen, theme, icons, git branch and changed files, LSP clients of the active file, " +
				"problem counts, debug session, running tests and tasks.",
			Describe: func(map[string]any) string { return "Read IDE state" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					out, err := m.aiIDEState(rel)
					return out, nil, err
				})
			},
		},
	}
}

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"termocode/extensions"
	"termocode/internal/activity"
	"termocode/internal/ext"
	"termocode/internal/keymap"
	"termocode/internal/nvim"
	"termocode/internal/picker"
	"termocode/internal/preview"
	"termocode/internal/prompt"
	"termocode/internal/theme"
	"termocode/internal/toast"
)

// ── Extensions (Group I) ─────────────────────────────────────────────────
//
// The extension host is nvim's own Lua runtime (ADR 0006). This file is
// the Go half:
//
//   - load: after the built-in Lua chunks (nvim.ReadyMsg), extLoadCmd
//     installs extLua, loads every ~/.config/termocode/extensions/<name>/
//     init.lua and reads back the registry (commands, panels, status
//     items) as JSON.
//   - palette: "Ext: <Title>" rows (id "ext-<command id>") plus the
//     "Extensions: …" management commands.
//   - panels: one activity-bar item per registered panel; the sidebar
//     shows the panel's lines through the generic markup renderer.
//   - status items: rendered in the status-bar center cluster; a click
//     runs the item's command.
//   - keymap.json: "ext:<command id>" values bind keys to commands.
//   - heartbeat: every extHeartbeat the visible panel and the status items
//     are re-rendered (one Lua call, off the UI goroutine), and at once
//     when Lua calls termocode.refresh() (termocode_ext_panel_dirty).
//
// Every Lua call runs inside a Cmd (never in Update/View) with a Go-side
// timeout, and Lua guards each extension call with pcall + a time budget,
// so a broken or slow extension can never block startup or the UI.

const (
	extHeartbeat    = time.Second
	extLoadTimeout  = 10 * time.Second
	extCallTimeout  = 3 * time.Second
	extPaletteIDPfx = "ext-"
)

// extState lives behind a pointer so every copy of the value-type Model
// shares it.
type extState struct {
	reg     ext.Registry
	exts    []ext.Extension
	loaded  bool
	loading bool
	gen     int // heartbeat generation; a reload starts a new one
	polling bool

	lines  map[string][]string // panel id → last rendered lines
	status map[string]string   // status item id → last text
	cursor map[string]int      // panel id → selected row (0-based)
	scroll map[string]int      // panel id → first visible row

	keys map[string]string // key string → command id (keymap.json "ext:")

	pendingPrompt int // Lua request id of the open prompt (0 = none)
	pendingPick   int // Lua request id of the open picker (0 = none)
}

func newExtState() *extState {
	return &extState{
		lines:  map[string][]string{},
		status: map[string]string{},
		cursor: map[string]int{},
		scroll: map[string]int{},
	}
}

// ── messages ─────────────────────────────────────────────────────────────

type extLoadedMsg struct {
	reg     ext.Registry
	exts    []ext.Extension
	skipped []string
	err     error
}

type extPollMsg struct {
	poll ext.Poll
	err  error
}

type extTickMsg struct{ gen int }

// extCallDoneMsg follows a run / select / resolve call; it triggers a
// poll so the UI reflects whatever the extension changed.
type extCallDoneMsg struct {
	what string
	err  error
}

// updateExt handles the extension messages. Called first from updateInner
// so no shared switch needs a case per message.
func (m *Model) updateExt(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case extLoadedMsg:
		return m.onExtLoaded(msg), true
	case extPollMsg:
		if m.ext != nil {
			m.ext.polling = false
			m.applyExtPoll(msg)
		}
		return nil, true
	case extTickMsg:
		if m.ext == nil || msg.gen != m.ext.gen || !m.extNeedsHeartbeat() {
			return nil, true
		}
		return tea.Batch(m.extPollCmd(), extTick(msg.gen)), true
	case extCallDoneMsg:
		if msg.err != nil {
			recordError(fmt.Sprintf("[ext] %s: %v", msg.what, msg.err))
		}
		return m.extPollCmd(), true
	}
	return nil, false
}

func extTick(gen int) tea.Cmd {
	return tea.Tick(extHeartbeat, func(time.Time) tea.Msg { return extTickMsg{gen: gen} })
}

func (m *Model) extNeedsHeartbeat() bool {
	return m.ext != nil && (len(m.ext.reg.Panels) > 0 || len(m.ext.reg.Status) > 0)
}

// ── Lua plumbing ─────────────────────────────────────────────────────────

// luaQuote renders s as a Lua single-quoted string literal. Every byte
// that could end the literal or confuse the parser is escaped (\ddd for
// control bytes), so any Go string round-trips.
func luaQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' || c == '\'':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			b.WriteString(fmt.Sprintf("\\%03d", c))
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// errExtTimeout is returned when nvim does not answer in time (usually an
// extension blocked in a C call such as vim.fn.system).
var errExtTimeout = errors.New("nvim did not answer in time (an extension may be blocked)")

// extEval runs code with a Go-side timeout. The RPC itself cannot be
// cancelled; on timeout the result is dropped when it finally arrives.
func extEval(c *nvim.Client, code string, timeout time.Duration) (string, error) {
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := c.EvalLuaString(code)
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-time.After(timeout):
		return "", errExtTimeout
	}
}

// extCall runs a fire-and-forget call into _termocode_ext off the UI
// goroutine.
func (m *Model) extCall(what, code string) tea.Cmd {
	if m.nvim == nil || m.ext == nil || !m.ext.loaded {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		_, err := extEval(c, "local H = _G._termocode_ext; if not H then return '' end; "+code+"; return ''", extCallTimeout)
		return extCallDoneMsg{what: what, err: err}
	}
}

// ── load / reload ────────────────────────────────────────────────────────

// extLoadCmd discovers and loads every extension. reload first drops what
// the previous load registered (commands, panels, autocmds, modules).
func (m *Model) extLoadCmd(reload bool) tea.Cmd {
	if m.nvim == nil || m.ext == nil || m.ext.loading {
		return nil
	}
	m.ext.loading = true
	c := m.nvim
	return func() tea.Msg {
		dir, err := ext.Dir()
		if err != nil {
			return extLoadedMsg{err: err}
		}
		exts, skipped := ext.Discover(dir)
		type wire struct {
			Name    string `json:"name"`
			Dir     string `json:"dir"`
			Init    string `json:"init"`
			Version string `json:"version"`
		}
		list := make([]wire, 0, len(exts))
		for _, x := range exts {
			list = append(list, wire{x.Name, x.Dir, x.Init, x.Version})
		}
		b, _ := json.Marshal(list)
		reset := ""
		if reload {
			reset = "_G._termocode_ext.reset()\n"
		}
		// Installing the API is cheap and idempotent; doing it here (not
		// in attachCmd) keeps the whole feature in this file.
		if err := c.ExecLua(extLua); err != nil {
			return extLoadedMsg{exts: exts, skipped: skipped, err: fmt.Errorf("install API: %w", err)}
		}
		code := reset + "local r = _G._termocode_ext.load(" + luaQuote(string(b)) + ")\n" +
			"_G._termocode_ext.emit('ready')\nreturn r"
		out, err := extEval(c, code, extLoadTimeout)
		if err != nil {
			return extLoadedMsg{exts: exts, skipped: skipped, err: err}
		}
		reg, err := ext.ParseRegistry(out)
		return extLoadedMsg{reg: reg, exts: exts, skipped: skipped, err: err}
	}
}

func (m *Model) onExtLoaded(msg extLoadedMsg) tea.Cmd {
	if m.ext == nil {
		return nil
	}
	s := m.ext
	s.loading = false
	s.exts = msg.exts
	var cmds []tea.Cmd
	for _, sk := range msg.skipped {
		recordError("[ext] skipped " + sk)
		var c tea.Cmd
		m.toast, c = m.toast.PushKeyed("ext-skip", toast.Warn, "Extension skipped", sk)
		cmds = append(cmds, c)
	}
	for _, x := range msg.exts {
		if x.Problem != "" {
			recordError("[ext] " + x.Name + ": " + x.Problem)
		}
	}
	if msg.err != nil {
		recordError("[ext] load: " + msg.err.Error())
		var c tea.Cmd
		m.toast, c = m.toast.PushKeyed("ext-load", toast.Errr, "Extensions failed to load", msg.err.Error())
		return tea.Batch(append(cmds, c)...)
	}
	s.reg = msg.reg
	s.loaded = true
	s.keys = keymap.LoadExtBindings("")
	// Activity bar: one item per panel. If the active view pointed at a
	// panel that is gone after a reload, fall back to the explorer.
	items := make([]activity.ExtraItem, 0, len(s.reg.Panels))
	for _, p := range s.reg.Panels {
		items = append(items, activity.ExtraItem{Label: p.Title, Glyph: p.Icon})
	}
	activity.SetExtraItems(items)
	if v := m.activity.Active(); v >= activity.ViewExtBase {
		if _, ok := activity.ExtIndex(v); !ok {
			m.activity.SetActive(activity.ViewFiles)
		}
	}
	s.gen++
	cmds = append(cmds, m.extPollCmd())
	if m.extNeedsHeartbeat() {
		cmds = append(cmds, extTick(s.gen))
	}
	return tea.Batch(cmds...)
}

// extReload re-reads the extensions folder and reloads everything.
func (m *Model) extReload() tea.Cmd {
	if m.ext == nil {
		return nil
	}
	m.ext.loaded = false
	m.ext.lines = map[string][]string{}
	m.ext.status = map[string]string{}
	m.cancelExtRequests()
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Reloading extensions…")
	return tea.Batch(c, m.extLoadCmd(true))
}

// ── heartbeat poll ───────────────────────────────────────────────────────

// extActivePanel returns the panel shown in the sidebar, if any.
func (m Model) extActivePanel() (ext.Panel, bool) {
	if m.ext == nil || !m.showExp {
		return ext.Panel{}, false
	}
	i, ok := activity.ExtIndex(m.activity.Active())
	if !ok || i >= len(m.ext.reg.Panels) {
		return ext.Panel{}, false
	}
	return m.ext.reg.Panels[i], true
}

// extPanelSize is the content area of an extension panel (below its
// 1-row header).
func (m Model) extPanelSize() (w, h int) {
	w = m.explorerWidth - 1
	h = m.h - 1 - 1 // status bar + header
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

func (m *Model) extPollCmd() tea.Cmd {
	if m.nvim == nil || m.ext == nil || !m.ext.loaded || m.ext.polling {
		return nil
	}
	if !m.extNeedsHeartbeat() {
		return nil
	}
	panelID := ""
	if p, ok := m.extActivePanel(); ok {
		panelID = p.ID
	}
	w, h := m.extPanelSize()
	m.ext.polling = true
	c := m.nvim
	code := fmt.Sprintf("local H = _G._termocode_ext; if not H then return '' end; return H.poll(%s, %d, %d)",
		luaQuote(panelID), w, h)
	return func() tea.Msg {
		out, err := extEval(c, code, extCallTimeout)
		if err != nil {
			return extPollMsg{err: err}
		}
		p, err := ext.ParsePoll(out)
		return extPollMsg{poll: p, err: err}
	}
}

func (m *Model) applyExtPoll(msg extPollMsg) {
	if msg.err != nil {
		recordError("[ext] poll: " + msg.err.Error())
		return
	}
	s := m.ext
	for _, st := range msg.poll.Status {
		s.status[st.ID] = st.Text
	}
	if msg.poll.Panel != "" {
		s.lines[msg.poll.Panel] = msg.poll.Lines
		m.clampExtCursor(msg.poll.Panel)
	}
}

// ── nvim → Go notifications ──────────────────────────────────────────────

func init() {
	registerNotifyHandler("termocode_ext_error", func(m *Model, args []any) tea.Cmd {
		name, what, text := notifyArgString(args, 0), notifyArgString(args, 1), notifyArgString(args, 2)
		recordError(fmt.Sprintf("[ext] %s: %s: %s", name, what, text))
		var c tea.Cmd
		m.toast, c = m.toast.PushKeyed("ext-err-"+name, toast.Errr, "Extension "+name+": "+what, text)
		return c
	})
	registerNotifyHandler("termocode_ext_notify", func(m *Model, args []any) tea.Cmd {
		sev := toast.Info
		switch notifyArgString(args, 0) {
		case "warn", "warning":
			sev = toast.Warn
		case "error":
			sev = toast.Errr
		}
		title, detail := notifyArgString(args, 1), notifyArgString(args, 2)
		var c tea.Cmd
		if detail != "" {
			m.toast, c = m.toast.PushDetail(sev, title, detail)
		} else {
			m.toast, c = m.toast.Push(sev, title)
		}
		return c
	})
	registerNotifyHandler("termocode_ext_panel_dirty", func(m *Model, args []any) tea.Cmd {
		return m.extPollCmd()
	})
	registerNotifyHandler("termocode_ext_preview", func(m *Model, args []any) tea.Cmd {
		title := notifyArgString(args, 0)
		body := sanitizeExtText(notifyArgString(args, 1))
		m.preview = preview.New(" "+title+" ", body)
		m.preview.SetSize(m.w, m.h)
		m.previewOpen = true
		return nil
	})
	registerNotifyHandler("termocode_ext_open", func(m *Model, args []any) tea.Cmd {
		return m.extOpenFile(notifyArgString(args, 0), notifyArgInt(args, 1), notifyArgInt(args, 2))
	})
	registerNotifyHandler("termocode_ext_prompt", func(m *Model, args []any) tea.Cmd {
		if m.ext == nil {
			return nil
		}
		cancel := m.cancelExtRequests()
		m.prompt = prompt.New(notifyArgString(args, 1), notifyArgString(args, 2), notifyArgString(args, 3))
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindExt
		m.ext.pendingPrompt = notifyArgInt(args, 0)
		return cancel
	})
	registerNotifyHandler("termocode_ext_pick", func(m *Model, args []any) tea.Cmd {
		if m.ext == nil {
			return nil
		}
		cancel := m.cancelExtRequests()
		var items []picker.Item
		if list, ok := argList(args, 2); ok {
			for i, raw := range list {
				mp, _ := raw.(map[string]any)
				items = append(items, picker.Item{
					ID:    strconv.Itoa(i + 1),
					Title: notifyArgString([]any{mp["title"]}, 0),
					Hint:  notifyArgString([]any{mp["hint"]}, 0),
				})
			}
		}
		m.picker = picker.NewItems(" "+notifyArgString(args, 1)+" ", items)
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindExt
		m.ext.pendingPick = notifyArgInt(args, 0)
		return tea.Batch(cancel, m.picker.Init())
	})
}

func argList(args []any, i int) ([]any, bool) {
	if i < 0 || i >= len(args) {
		return nil, false
	}
	l, ok := args[i].([]any)
	return l, ok
}

// sanitizeExtText drops ESC and other control bytes (except newline and
// tab) so an extension cannot inject terminal escapes into the preview.
func sanitizeExtText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// extOpenFile opens path in the editor window and moves the cursor.
func (m *Model) extOpenFile(path string, line, col int) tea.Cmd {
	if m.nvim == nil || path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			path = filepath.Join(wd, path)
		}
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`
		local path, line, col = ...
		pcall(vim.cmd, 'edit ' .. vim.fn.fnameescape(path))
		if line > 0 then
			pcall(vim.api.nvim_win_set_cursor, 0, { line, math.max(col - 1, 0) })
			pcall(vim.cmd, 'normal! zz')
		end
	`, path, line, col)
	m.explorer.RevealPath(path)
	m.focus = FocusEditor
	return nil
}

// ── prompt / pick round trip ─────────────────────────────────────────────

// resolveExt answers Lua request req with value (nil = cancelled).
func (m *Model) resolveExt(req int, value any) tea.Cmd {
	if req <= 0 {
		return nil
	}
	arg := "nil"
	switch v := value.(type) {
	case string:
		arg = luaQuote(v)
	case int:
		arg = strconv.Itoa(v)
	}
	return m.extCall("callback", fmt.Sprintf("H.resolve(%d, %s)", req, arg))
}

// cancelExtRequests answers any open prompt / pick request with nil.
func (m *Model) cancelExtRequests() tea.Cmd {
	if m.ext == nil {
		return nil
	}
	var cmds []tea.Cmd
	if m.ext.pendingPrompt > 0 {
		cmds = append(cmds, m.resolveExt(m.ext.pendingPrompt, nil))
		m.ext.pendingPrompt = 0
	}
	if m.ext.pendingPick > 0 {
		cmds = append(cmds, m.resolveExt(m.ext.pendingPick, nil))
		m.ext.pendingPick = 0
	}
	return tea.Batch(cmds...)
}

// extPromptSubmit runs from handlePromptSubmit for promptKindExt.
func (m *Model) extPromptSubmit(value string) tea.Cmd {
	if m.ext == nil {
		return nil
	}
	req := m.ext.pendingPrompt
	m.ext.pendingPrompt = 0
	return m.resolveExt(req, value)
}

// extPickSelect runs from handlePickerSelect for pickerKindExt.
func (m *Model) extPickSelect(id string) tea.Cmd {
	if m.ext == nil {
		return nil
	}
	req := m.ext.pendingPick
	m.ext.pendingPick = 0
	idx, err := strconv.Atoi(id)
	if err != nil {
		return m.resolveExt(req, nil)
	}
	return m.resolveExt(req, idx)
}

// extOverlayClosed runs on prompt.CloseMsg / picker.CloseMsg: a cancelled
// extension prompt or picker answers its callback with nil.
func (m *Model) extOverlayClosed() tea.Cmd {
	if m.ext == nil {
		return nil
	}
	if m.ext.pendingPrompt > 0 && m.promptKind == promptKindExt {
		req := m.ext.pendingPrompt
		m.ext.pendingPrompt = 0
		return m.resolveExt(req, nil)
	}
	if m.ext.pendingPick > 0 && m.pickerKind == pickerKindExt {
		req := m.ext.pendingPick
		m.ext.pendingPick = 0
		return m.resolveExt(req, nil)
	}
	return nil
}

// ── commands, palette, keys ──────────────────────────────────────────────

// runExtCommand runs a registered command by id.
func (m *Model) runExtCommand(id string) tea.Cmd {
	if m.ext == nil || !m.ext.loaded {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Extensions are not loaded", id)
		return c
	}
	for _, c := range m.ext.reg.Commands {
		if c.ID == id {
			return m.extCall("command "+id, "H.run("+luaQuote(id)+")")
		}
	}
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Warn, "Unknown extension command", id)
	return c
}

// extKeyCommand returns the command bound to key in keymap.json.
func (m Model) extKeyCommand(key string) (string, bool) {
	if m.ext == nil || !m.ext.loaded || len(m.ext.keys) == 0 {
		return "", false
	}
	id, ok := m.ext.keys[key]
	return id, ok
}

// extPaletteItems returns the "Ext: <Title>" rows.
func (m Model) extPaletteItems() []picker.Item {
	if m.ext == nil {
		return nil
	}
	keyFor := map[string]string{}
	for k, id := range m.ext.keys {
		if prev, ok := keyFor[id]; !ok || k < prev {
			keyFor[id] = k
		}
	}
	items := make([]picker.Item, 0, len(m.ext.reg.Commands))
	for _, c := range m.ext.reg.Commands {
		hint := c.Hint
		if k, ok := keyFor[c.ID]; ok {
			hint = k
		}
		if hint == "" {
			hint = c.Ext
		}
		items = append(items, picker.Item{ID: extPaletteIDPfx + c.ID, Title: "Ext: " + c.Title, Hint: hint})
	}
	return items
}

// extManagePaletteItems are the static "Extensions: …" rows.
func extManagePaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "ext-manage-reload", Title: "Extensions: Reload"},
		{ID: "ext-manage-open-folder", Title: "Extensions: Open Folder"},
		{ID: "ext-manage-log", Title: "Extensions: Show Log"},
		{ID: "ext-manage-new", Title: "Extensions: New Extension..."},
		{ID: "ext-manage-install-samples", Title: "Extensions: Install Sample Extensions"},
	}
}

// dispatchExtPalette handles palette ids owned by this file.
func (m *Model) dispatchExtPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "ext-manage-reload":
		return m.extReload(), true
	case "ext-manage-open-folder":
		return m.extOpenFolder(), true
	case "ext-manage-log":
		return m.extShowLog(), true
	case "ext-manage-new":
		m.prompt = prompt.New("New Extension", "Name (letters, digits, _ . -):", "")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindExtNew
		return nil, true
	case "ext-manage-install-samples":
		return m.extInstallSamples(), true
	}
	if strings.HasPrefix(id, extPaletteIDPfx) {
		return m.runExtCommand(strings.TrimPrefix(id, extPaletteIDPfx)), true
	}
	return nil, false
}

func (m *Model) extOpenFolder() tea.Cmd {
	dir, err := ext.Dir()
	if err == nil {
		err = os.MkdirAll(dir, 0o755)
	}
	if err == nil {
		err = osOpen(dir)
	}
	var c tea.Cmd
	if err != nil {
		// Headless / SSH: no file manager. The path is still useful.
		m.toast, c = m.toast.PushDetail(toast.Warn, "Could not open the folder", dir)
		return c
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Extensions folder", dir)
	return c
}

// extShowLog shows load results plus the Lua-side extension log.
func (m *Model) extShowLog() tea.Cmd {
	var b strings.Builder
	dir, _ := ext.Dir()
	fmt.Fprintf(&b, "Extensions folder: %s\n\n", dir)
	if m.ext != nil {
		if len(m.ext.reg.Extensions) == 0 {
			b.WriteString("No extensions loaded.\n")
		}
		for _, x := range m.ext.reg.Extensions {
			state := "✓ loaded"
			if !x.OK {
				state = "✘ failed: " + x.Error
			}
			ver := ""
			if x.Version != "" {
				ver = " v" + x.Version
			}
			fmt.Fprintf(&b, "  %s%s  %s\n", x.Name, ver, state)
		}
		for _, x := range m.ext.exts {
			if x.Problem != "" {
				fmt.Fprintf(&b, "  %s  ⚠ %s\n", x.Name, x.Problem)
			}
		}
		fmt.Fprintf(&b, "\n%d commands · %d panels · %d status items\n",
			len(m.ext.reg.Commands), len(m.ext.reg.Panels), len(m.ext.reg.Status))
	}
	if m.nvim != nil {
		if out, err := extEval(m.nvim, "local H = _G._termocode_ext; return H and H.log_text() or ''", extCallTimeout); err == nil && out != "" {
			b.WriteString("\n── log ──\n")
			b.WriteString(out)
		}
	}
	m.preview = preview.New(" Extensions: Log ", sanitizeExtText(b.String()))
	m.preview.SetSize(m.w, m.h)
	m.previewOpen = true
	return nil
}

// extNewSubmit scaffolds a new extension, opens its init.lua and reloads.
func (m *Model) extNewSubmit(name string) tea.Cmd {
	dir, err := ext.Dir()
	var initPath string
	if err == nil {
		initPath, err = ext.Scaffold(dir, name)
	}
	if err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Errr, "New extension failed", err.Error())
		return c
	}
	open := m.extOpenFile(initPath, 1, 1)
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "Extension created", initPath)
	return tea.Batch(open, c, m.extReload())
}

// extInstallSamples copies the bundled sample extensions (repo folder
// extensions/, embedded in the binary) into the extensions folder.
func (m *Model) extInstallSamples() tea.Cmd {
	dir, err := ext.Dir()
	var names []string
	if err == nil {
		names, err = ext.InstallBundled(extensions.FS, dir)
	}
	var c tea.Cmd
	switch {
	case err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, "Install failed", err.Error())
		return c
	case len(names) == 0:
		m.toast, c = m.toast.PushDetail(toast.Info, "Samples already installed", dir)
		return c
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Installed "+strings.Join(names, ", "), dir)
	return tea.Batch(c, m.extReload())
}

// ── sidebar panel ────────────────────────────────────────────────────────

// isExtSidebar reports whether the sidebar shows an extension panel.
func (m Model) isExtSidebar() bool {
	_, ok := m.extActivePanel()
	return ok
}

// renderExtSidebar draws the active extension panel: a header row and the
// panel's markup lines on the sidebar background.
func (m Model) renderExtSidebar(w, h int) string {
	p, ok := m.extActivePanel()
	if !ok {
		return ""
	}
	bg := theme.LG(theme.BgSidebar)
	fill := theme.Bg(theme.BgSidebar)
	rows := []string{sidebarHeader(w, p.Title, "")}
	lines := m.ext.lines[p.ID]
	cur := m.ext.cursor[p.ID]
	top := m.ext.scroll[p.ID]
	focused := m.focus == FocusExplorer
	body := h - 1
	if len(lines) == 0 && body > 0 {
		msg := "Loading…"
		if !m.ext.loaded {
			msg = "Extensions are not loaded."
		}
		rows = append(rows, extPadRow(ext.Render("  {{muted}}"+msg+"{{/}}", theme.TextPrimary, bg), w, fill))
		body--
	}
	for i := 0; i < body; i++ {
		idx := top + i
		if idx >= len(lines) {
			rows = append(rows, fill.Render(strings.Repeat(" ", w)))
			continue
		}
		line := " " + lines[idx]
		if focused && idx == cur {
			sel := theme.LG(theme.BgSelection)
			rows = append(rows, extPadRow(ext.Render(line, theme.TextWhite, sel), w, theme.Bg(theme.BgSelection)))
			continue
		}
		rows = append(rows, extPadRow(ext.Render(line, theme.TextPrimary, bg), w, fill))
	}
	return strings.Join(rows, "\n")
}

// extPadRow pads / clips a styled row to exactly w cells.
func extPadRow(s string, w int, fill lipgloss.Style) string {
	return padTabBarToWidth(s, w, fill)
}

func (m *Model) clampExtCursor(panelID string) {
	n := len(m.ext.lines[panelID])
	cur := m.ext.cursor[panelID]
	if cur >= n {
		cur = n - 1
	}
	if cur < 0 {
		cur = 0
	}
	m.ext.cursor[panelID] = cur
	_, h := m.extPanelSize()
	top := m.ext.scroll[panelID]
	if cur < top {
		top = cur
	}
	if cur >= top+h {
		top = cur - h + 1
	}
	if top > n-h {
		top = n - h
	}
	if top < 0 {
		top = 0
	}
	m.ext.scroll[panelID] = top
}

// extSelectRow selects row (0-based) of the active panel and calls its
// on_select.
func (m *Model) extSelectRow(row int) tea.Cmd {
	p, ok := m.extActivePanel()
	if !ok {
		return nil
	}
	lines := m.ext.lines[p.ID]
	if row < 0 || row >= len(lines) {
		return nil
	}
	m.ext.cursor[p.ID] = row
	m.clampExtCursor(p.ID)
	return m.extCall("select "+p.ID, fmt.Sprintf("H.select(%s, %d, %s)",
		luaQuote(p.ID), row+1, luaQuote(ext.PlainText(lines[row]))))
}

// handleExtSidebarKey drives the panel from the keyboard.
func (m *Model) handleExtSidebarKey(k tea.KeyMsg) tea.Cmd {
	p, ok := m.extActivePanel()
	if !ok {
		return nil
	}
	_, h := m.extPanelSize()
	move := func(d int) {
		m.ext.cursor[p.ID] += d
		m.clampExtCursor(p.ID)
	}
	switch k.String() {
	case "up", "k":
		move(-1)
	case "down", "j":
		move(1)
	case "pgup":
		move(-h)
	case "pgdown":
		move(h)
	case "home", "g":
		m.ext.cursor[p.ID] = 0
		m.clampExtCursor(p.ID)
	case "end", "G":
		m.ext.cursor[p.ID] = len(m.ext.lines[p.ID]) - 1
		m.clampExtCursor(p.ID)
	case "enter", " ":
		return m.extSelectRow(m.ext.cursor[p.ID])
	case "r":
		return m.extPollCmd()
	case "esc":
		m.focus = FocusEditor
	}
	return nil
}

// handleExtSidebarMouse handles clicks / wheel in the panel. y is the row
// inside the sidebar (0 = header).
func (m *Model) handleExtSidebarMouse(y int, t tea.MouseEventType) tea.Cmd {
	p, ok := m.extActivePanel()
	if !ok {
		return nil
	}
	_, h := m.extPanelSize()
	switch t {
	case tea.MouseWheelUp, tea.MouseWheelDown:
		d := 3
		if t == tea.MouseWheelUp {
			d = -3
		}
		top := m.ext.scroll[p.ID] + d
		if max := len(m.ext.lines[p.ID]) - h; top > max {
			top = max
		}
		if top < 0 {
			top = 0
		}
		m.ext.scroll[p.ID] = top
		cur := m.ext.cursor[p.ID]
		if cur < top {
			m.ext.cursor[p.ID] = top
		} else if cur >= top+h {
			m.ext.cursor[p.ID] = top + h - 1
		}
	case tea.MouseLeft:
		if y < 1 {
			return m.extPollCmd() // header click refreshes
		}
		return m.extSelectRow(m.ext.scroll[p.ID] + y - 1)
	}
	return nil
}

// ── status bar ───────────────────────────────────────────────────────────

// extStatusTexts returns the status item texts in registration order.
func (m Model) extStatusTexts() []string {
	if m.ext == nil || !m.ext.loaded || len(m.ext.reg.Status) == 0 {
		return nil
	}
	out := make([]string, len(m.ext.reg.Status))
	for i, s := range m.ext.reg.Status {
		out[i] = m.ext.status[s.ID]
	}
	return out
}

// hitExtStatusItem runs the command of the status item under (x, y).
func (m *Model) hitExtStatusItem(x, y int) (tea.Cmd, bool) {
	if m.ext == nil || m.zenMode || y != m.h-1 {
		return nil, false
	}
	st := m.statusState()
	if len(st.Ext) == 0 {
		return nil, false
	}
	bx, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	for _, sp := range sb.ExtItemSpans(st) {
		if x >= bx+sp.X0 && x < bx+sp.X1 {
			item := m.ext.reg.Status[sp.Index]
			if item.Command == "" {
				return nil, true
			}
			return m.runExtCommand(item.Command), true
		}
	}
	return nil, false
}

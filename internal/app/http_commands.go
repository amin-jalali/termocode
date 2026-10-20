package app

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// HTTP client commands (plan 2.8): palette entries, Ctrl+Alt+R, the
// pickers (send, history, environment, saved), the one-line prompts
// (quick request, import curl, save names) and the env files.

const (
	pickerKindHTTPSend pickerKindEnum = 1100 + iota
	pickerKindHTTPHistory
	pickerKindHTTPEnv
	pickerKindHTTPSaved
	pickerKindDBSave
	pickerKindDBType
	pickerKindDBTestResult
)

const (
	promptKindHTTPQuick promptKindEnum = 1100 + iota
	promptKindHTTPImport
	promptKindHTTPSaveName
	promptKindHTTPSaveBody
	promptKindHTTPNewFile
	promptKindDBAdd
	promptKindDBFilter
	promptKindDBPassword
	promptKindDBHost
	promptKindDBPort
	promptKindDBUser
	promptKindDBName
	promptKindDBSQLitePath
)

const (
	confirmKindHTTPUnresolved confirmKindEnum = 1100 + iota
	confirmKindHTTPCopySecrets
	confirmKindHTTPGitignore
	confirmKindHTTPClearHistory
)

// httpPaletteItems are the palette's "HTTP: …" commands.
func httpPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "http-send", Title: "HTTP: Send Request at Cursor", Hint: "Ctrl+Alt+R"},
		{ID: "http-send-pick", Title: "HTTP: Send Request…"},
		{ID: "http-quick", Title: "HTTP: Quick Request…"},
		{ID: "http-cancel", Title: "HTTP: Cancel Request"},
		{ID: "http-last-response", Title: "HTTP: Show Last Response"},
		{ID: "http-history", Title: "HTTP: History…"},
		{ID: "http-env-select", Title: "HTTP: Select Environment…"},
		{ID: "http-env-open", Title: "HTTP: Open Environment File"},
		{ID: "http-env-open-private", Title: "HTTP: Open Private Environment File"},
		{ID: "http-saved", Title: "HTTP: Saved Requests…"},
		{ID: "http-save", Title: "HTTP: Save Request to Saved…"},
		{ID: "http-copy-curl", Title: "HTTP: Copy as curl"},
		{ID: "http-import-curl", Title: "HTTP: Import from curl…"},
		{ID: "http-new-file", Title: "HTTP: New .http File…"},
		{ID: "http-save-body", Title: "HTTP: Save Response Body…"},
	}
}

// dispatchHTTPPalette runs an "http-…" palette id. ok=false: not ours.
func (m *Model) dispatchHTTPPalette(id string) (tea.Cmd, bool) {
	if !strings.HasPrefix(id, "http-") {
		return nil, false
	}
	switch id {
	case "http-send":
		return m.httpSendAction(), true
	case "http-send-pick":
		return m.openHTTPSendPicker(), true
	case "http-quick":
		return m.openHTTPPrompt(promptKindHTTPQuick, "HTTP: Quick Request", "Request:", ""), true
	case "http-cancel":
		return m.httpCancel(), true
	case "http-last-response":
		return m.httpShowLast(), true
	case "http-history":
		return m.openHTTPHistory(), true
	case "http-env-select":
		return m.openHTTPEnvPicker(), true
	case "http-env-open":
		return m.httpOpenEnvFile(false), true
	case "http-env-open-private":
		return m.httpOpenEnvFile(true), true
	case "http-saved":
		return m.openHTTPSaved(), true
	case "http-save":
		return m.httpSaveRequestPrompt(), true
	case "http-copy-curl":
		return m.httpCopyCurlAction(), true
	case "http-import-curl":
		m.httpS().importInto = m.httpCurrentFile()
		return m.openHTTPPrompt(promptKindHTTPImport, "HTTP: Import from curl", "curl:", ""), true
	case "http-new-file":
		return m.openHTTPPrompt(promptKindHTTPNewFile, "HTTP: New .http File", "File:", "requests.http"), true
	case "http-save-body":
		return m.httpSaveBodyPrompt(), true
	}
	return nil, false
}

func (m *Model) openHTTPPrompt(kind promptKindEnum, title, label, initial string) tea.Cmd {
	m.prompt = prompt.New(title, label, initial)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
	return nil
}

// httpCurrentFile is the active buffer when it is an .http file.
func (m *Model) httpCurrentFile() string {
	if p := m.editor.Path(); isHTTPPath(p) {
		return p
	}
	return ""
}

// sendKeyBuffer reports whether the editor shows a buffer the send key
// works in (.http, .sql, or one of their result pages). Ctrl+Enter, the
// second send key, only acts there.
func (m *Model) sendKeyBuffer() bool {
	if m.focus != FocusEditor {
		return false
	}
	p := m.editor.Path()
	base := filepath.Base(p)
	return isHTTPPath(p) || strings.EqualFold(filepath.Ext(p), ".sql") || base == "SQL Result" || base == "HTTP Response"
}

// httpSendAction is the send key (Ctrl+Alt+R, Ctrl+Enter), dispatched by
// file type: the request at the cursor in an .http buffer, the statement
// at the cursor (or the selection) in a .sql buffer, a rerun on the SQL
// Result page, a resend on the response page, else the last request.
func (m *Model) httpSendAction() tea.Cmd {
	p := m.editor.Path()
	switch {
	case strings.EqualFold(filepath.Ext(p), ".sql"):
		return m.databaseSend(false)
	case filepath.Base(p) == "SQL Result":
		return m.databaseRerun()
	case isHTTPPath(p):
		return m.httpSendBufferLine(0, 0)
	case m.httpS().last != nil || m.httpS().shown != nil:
		return m.httpResend()
	}
	return m.pushToastDetail(toast.Info, "No request yet", "Open an .http file, or run HTTP: Quick Request…")
}

// httpSendRequest resolves req and sends it as the user's request. With
// unknown {{names}} it asks "Send anyway?" first.
func (m *Model) httpSendRequest(file string, vars []httpclient.Var, req httpclient.Request, env string) tea.Cmd {
	s := m.httpS()
	if s.running != nil {
		return m.pushToastDetail(toast.Warn, "A request is still running", "Esc on the response page, or HTTP: Cancel Request")
	}
	rv := httpResolver(file, vars, env)
	rr, un := rv.Resolve(req)
	label := req.Label()
	run := &httpRun{rr: rr, file: file, line: req.Line, label: label, env: rv.Env}
	if len(un) > 0 {
		s.pending = run
		var names []string
		seen := map[string]bool{}
		for _, u := range un {
			if !seen[u.Name] {
				seen[u.Name] = true
				names = append(names, "{{"+u.Name+"}}")
			}
		}
		m.confirm = confirm.New("Send anyway?",
			"Unknown variables stay as written: "+strings.Join(names, ", ")+". Define them in http-client.env.json or with @name = value.",
			[]confirm.Button{{ID: "send", Title: "Send anyway", Style: confirm.StylePrimary}, {ID: "cancel", Title: "Cancel"}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindHTTPUnresolved
		return nil
	}
	return m.startHTTPRun(run, httpclient.Hooks{})
}

// httpResend sends the shown (or last) request again, read again from
// its .http file so edits and a rotated token are picked up.
func (m *Model) httpResend() tea.Cmd {
	s := m.httpS()
	r := s.shown
	if r == nil {
		r = s.last
	}
	if r == nil {
		return m.pushToastDetail(toast.Info, "No request yet", "")
	}
	if r.file != "" {
		if f, ok := m.httpReadFile(r.file); ok {
			for i := range f.Requests {
				if f.Requests[i].Label() == r.label {
					return m.httpSendRequest(r.file, f.Vars, f.Requests[i], "")
				}
			}
			return m.httpSendRequest(r.file, f.Vars, r.rr.Template, "")
		}
	}
	return m.httpSendRequest(r.file, nil, r.rr.Template, "")
}

// httpReadFile parses an .http file: the open buffer when there is one
// (unsaved edits count), else the file on disk.
func (m *Model) httpReadFile(file string) (httpclient.File, bool) {
	if m.nvim != nil {
		var raw string
		if err := m.nvim.ExecLuaResult(`local name = ...
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.api.nvim_buf_get_name(b) == name then
    return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n') .. '\n'
  end
end
return ''`, &raw, file); err == nil && raw != "" {
			return httpclient.Parse(raw), true
		}
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return httpclient.File{}, false
	}
	return httpclient.Parse(string(b)), true
}

// httpShowLast opens the page on the last answer without sending.
func (m *Model) httpShowLast() tea.Cmd {
	s := m.httpS()
	r := s.shown
	if r == nil {
		r = s.last
	}
	if r == nil || r.resp == nil {
		return m.pushToastDetail(toast.Info, "No response yet", "Send a request first")
	}
	return m.httpShowRun(r, false)
}

// ── pickers ─────────────────────────────────────────────────────────────

func (m *Model) openHTTPPicker(kind pickerKindEnum, title string, items []picker.Item, note []string) tea.Cmd {
	m.picker = picker.NewItems(title, items)
	if len(note) > 0 {
		m.picker.SetNote(note)
	}
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = kind
	return m.picker.Init()
}

// httpWorkspaceFiles lists the .http / .rest files in the workspace
// (max 40, sorted).
func httpWorkspaceFiles(root string, max int) []string {
	var out []string
	skip := map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "target": true}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || len(out) >= max {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if p != root && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if isHTTPPath(p) {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// openHTTPSendPicker lists the requests of the open .http file (or of
// every .http file in the workspace).
func (m *Model) openHTTPSendPicker() tea.Cmd {
	s := m.httpS()
	files := []string{}
	if f := m.httpCurrentFile(); f != "" {
		files = append(files, f)
	} else {
		files = httpWorkspaceFiles(aiWorkspaceRoot(), 40)
	}
	s.pickReqs = nil
	var items []picker.Item
	for _, file := range files {
		f, ok := m.httpReadFile(file)
		if !ok {
			continue
		}
		rel := file
		if r, err := filepath.Rel(aiWorkspaceRoot(), file); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
		for _, r := range f.Requests {
			id := fmt.Sprint(len(s.pickReqs))
			s.pickReqs = append(s.pickReqs, httpPickReq{file: file, line: r.Line})
			items = append(items, picker.Item{ID: id, Title: r.Method + " " + r.Label(), Hint: fmt.Sprintf("%s:%d", rel, r.Line)})
		}
	}
	if len(items) == 0 {
		return m.pushToastDetail(toast.Info, "No .http requests found", "HTTP: New .http File… makes one")
	}
	return m.openHTTPPicker(pickerKindHTTPSend, " Send Request ", items, nil)
}

// openHTTPHistory lists history.jsonl, newest first.
func (m *Model) openHTTPHistory() tea.Cmd {
	list, err := httpHistoryStore().List()
	if err != nil {
		return m.pushToastDetail(toast.Errr, "History failed", err.Error())
	}
	if len(list) == 0 {
		return m.pushToastDetail(toast.Info, "No HTTP history yet", "")
	}
	now := time.Now()
	items := make([]picker.Item, 0, len(list)+1)
	for _, e := range list {
		hint := e.Env
		if e.By == "ai" {
			hint = strings.TrimSpace(hint + " by AI")
		}
		items = append(items, picker.Item{ID: e.ID, Title: e.Line(now), Hint: hint})
	}
	items = append(items, picker.Item{ID: "__clear", Title: "Clear history…"})
	return m.openHTTPPicker(pickerKindHTTPHistory, " HTTP History ", items,
		[]string{"Enter sends again · Ctrl+O opens the stored response · Ctrl+D deletes"})
}

// openHTTPEnvPicker lists the environments of the env files found.
func (m *Model) openHTTPEnvPicker() tea.Cmd {
	dir := aiWorkspaceRoot()
	if f := m.httpCurrentFile(); f != "" {
		dir = filepath.Dir(f)
	} else if s := m.httpS(); s.shown != nil && s.shown.file != "" {
		dir = filepath.Dir(s.shown.file)
	}
	root := httpRootFor(dir)
	envs := httpclient.LoadEnvs(dir, root)
	cur := httpSelectedEnv(root)
	m.httpS().envRoot = root
	items := []picker.Item{{ID: "-", Title: "(none)"}}
	for _, n := range envs.Names() {
		hint := ""
		if n == cur {
			hint = "active"
		}
		items = append(items, picker.Item{ID: n, Title: n, Hint: hint})
	}
	var note []string
	if len(envs.Names()) == 0 {
		note = []string{"No environments: HTTP: Open Environment File makes http-client.env.json"}
	}
	cmd := m.openHTTPPicker(pickerKindHTTPEnv, " HTTP Environment ", items, note)
	if cur != "" {
		m.picker.SetCursorID(cur)
	}
	return cmd
}

// openHTTPSaved lists saved.http.
func (m *Model) openHTTPSaved() tea.Cmd {
	b, _ := os.ReadFile(httpSavedPath())
	f := httpclient.Parse(string(b))
	m.httpS().saved = f.Requests
	var items []picker.Item
	for i, r := range f.Requests {
		items = append(items, picker.Item{ID: fmt.Sprint(i), Title: r.Label(), Hint: r.Method + " " + r.URL})
	}
	items = append(items, picker.Item{ID: "__edit", Title: "Open saved.http in the editor"})
	return m.openHTTPPicker(pickerKindHTTPSaved, " Saved Requests ", items, nil)
}

// handleHTTPPickerSelect routes the HTTP pickers. ok=false: not ours.
func (m *Model) handleHTTPPickerSelect(id string) (tea.Cmd, bool) {
	if cmd, ok := m.databaseConsolePicker(id); ok {
		return cmd, true
	}
	s := m.httpS()
	switch m.pickerKind {
	case pickerKindDBSave:
		return m.databaseSavePending(id == "db-save-project"), true
	case pickerKindDBType:
		return m.databaseStartAdd(id), true
	case pickerKindDBTestResult:
		switch id {
		case "db-test-password":
			return m.databasePromptPasswordForPending(), true
		case "db-test-retry":
			return m.databaseRetestPending(), true
		case "db-test-cancel":
			if m.databases != nil {
				m.databases.pendingConn = dbclient.Conn{}
				m.databases.pendingPassword = ""
			}
			return nil, true
		}
		return nil, true
	case pickerKindHTTPSend:
		var i int
		if _, err := fmt.Sscan(id, &i); err != nil || i < 0 || i >= len(s.pickReqs) {
			return nil, true
		}
		pr := s.pickReqs[i]
		f, ok := m.httpReadFile(pr.file)
		if !ok {
			return nil, true
		}
		if r := f.AtLine(pr.line); r != nil {
			return m.httpSendRequest(pr.file, f.Vars, *r, ""), true
		}
		return nil, true
	case pickerKindHTTPHistory:
		if id == "__clear" {
			m.confirm = confirm.New("Clear HTTP history?", "Every entry in http/history.jsonl is deleted.",
				[]confirm.Button{{ID: "clear", Title: "Clear", Style: confirm.StyleDestructive}, {ID: "cancel", Title: "Cancel"}})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindHTTPClearHistory
			return nil, true
		}
		e, ok := httpHistoryStore().Get(id)
		if !ok {
			return nil, true
		}
		return m.httpResendEntry(e), true
	case pickerKindHTTPEnv:
		root := s.envRoot
		if root == "" {
			root = aiWorkspaceRoot()
		}
		env := id
		if env == "-" {
			env = ""
		}
		if err := httpSetSelectedEnv(root, env); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not save the environment", err.Error()), true
		}
		m.httpRefreshLens(m.httpCurrentFile())
		if env == "" {
			env = "(none)"
		}
		return m.pushToastDetail(toast.Info, "HTTP environment: "+env, ""), true
	case pickerKindHTTPSaved:
		if id == "__edit" {
			return m.httpOpenInEditor(httpSavedPath()), true
		}
		var i int
		if _, err := fmt.Sscan(id, &i); err != nil || i < 0 || i >= len(s.saved) {
			return nil, true
		}
		return m.httpSendRequest(httpSavedPath(), nil, s.saved[i], ""), true
	}
	return nil, false
}

// httpPickerKey handles Ctrl+O / Ctrl+D in the history picker and
// Ctrl+E in the saved picker. ok=false: not ours.
func (m *Model) httpPickerKey(k tea.KeyMsg) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindHTTPHistory:
		it, ok := m.picker.Current()
		if !ok || it.ID == "__clear" {
			return nil, false
		}
		switch k.String() {
		case "ctrl+o":
			e, ok := httpHistoryStore().Get(it.ID)
			if !ok {
				return nil, true
			}
			m.pickerOpen = false
			return m.httpShowEntry(e), true
		case "ctrl+d":
			if err := httpHistoryStore().Delete(it.ID); err != nil {
				return m.pushToastDetail(toast.Errr, "Delete failed", err.Error()), true
			}
			m.pickerOpen = false
			return m.openHTTPHistory(), true
		}
	case pickerKindHTTPSaved:
		if k.String() == "ctrl+e" {
			m.pickerOpen = false
			return m.httpOpenInEditor(httpSavedPath()), true
		}
	}
	return nil, false
}

// httpEntryFile is the absolute .http file of a history entry ("" for a
// quick request).
func httpEntryFile(e httpclient.Entry) string {
	if e.File == "" {
		return ""
	}
	if filepath.IsAbs(e.File) {
		return e.File
	}
	return filepath.Join(aiWorkspaceRoot(), filepath.FromSlash(e.File))
}

// httpResendEntry sends a history entry again, re-resolved with the
// current environment (a rotated token is picked up).
func (m *Model) httpResendEntry(e httpclient.Entry) tea.Cmd {
	tmpl := e.Request()
	file := httpEntryFile(e)
	if file != "" {
		if f, ok := m.httpReadFile(file); ok {
			for i := range f.Requests {
				if f.Requests[i].Label() == e.Name {
					return m.httpSendRequest(file, f.Vars, f.Requests[i], "")
				}
			}
			return m.httpSendRequest(file, f.Vars, tmpl, "")
		}
	}
	return m.httpSendRequest(file, nil, tmpl, "")
}

// httpShowEntry opens a stored response on the page without sending.
func (m *Model) httpShowEntry(e httpclient.Entry) tea.Cmd {
	tmpl := e.Request()
	run := &httpRun{rr: httpclient.Resolved{Request: tmpl, Template: tmpl, Env: e.Env}, resp: e.Response(),
		file: httpEntryFile(e), label: tmpl.Label(), env: e.Env, by: e.By, at: e.Time}
	return m.httpShowRun(run, false)
}

// ── prompts ─────────────────────────────────────────────────────────────

// handleHTTPPromptSubmit routes the HTTP prompts. ok=false: not ours.
func (m *Model) handleHTTPPromptSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind == promptKindDBResultFilter {
		s := m.dbConsole()
		s.filter = value
		s.page = 0
		return m.databaseShowPage(), true
	}
	switch m.promptKind {
	case promptKindHTTPQuick:
		req, err := httpclient.ParseQuick(value)
		if err != nil {
			return m.pushToastDetail(toast.Warn, "Quick request: "+err.Error(), "Example: POST https://host/path {\"a\":1}"), true
		}
		return m.httpSendRequest("", nil, req, ""), true
	case promptKindHTTPImport:
		req, err := httpclient.FromCurl(value)
		if err != nil {
			return m.pushToastDetail(toast.Warn, "Import from curl: "+err.Error(), ""), true
		}
		return m.httpInsertRequest(m.httpS().importInto, req), true
	case promptKindHTTPSaveName:
		return m.httpSaveRequest(strings.TrimSpace(value)), true
	case promptKindHTTPSaveBody:
		return m.httpSaveBody(strings.TrimSpace(value)), true
	case promptKindHTTPNewFile:
		return m.httpNewFile(strings.TrimSpace(value)), true
	case promptKindDBAdd, promptKindDBFilter, promptKindDBHost, promptKindDBPort, promptKindDBUser, promptKindDBName, promptKindDBSQLitePath:
		return m.onDBPrompt(m.promptKind, value), true
	case promptKindDBPassword:
		return m.onDBPrompt(m.promptKind, value), true
	}
	return nil, false
}

// httpInsertRequest appends req as a new ### block at the end of file
// (or scratch.http in the workspace root) and puts the cursor on it.
func (m *Model) httpInsertRequest(file string, req httpclient.Request) tea.Cmd {
	if file == "" {
		file = filepath.Join(aiWorkspaceRoot(), "scratch.http")
	}
	block := httpclient.Serialize([]httpclient.Request{req})
	if m.nvim != nil {
		var line int
		var raw string
		err := m.nvim.ExecLuaResult(`local file, block = ...
local b
for _, x in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(x) and vim.api.nvim_buf_get_name(x) == file then b = x end
end
if not b then return '' end
local n = vim.api.nvim_buf_line_count(b)
local last = vim.api.nvim_buf_get_lines(b, n - 1, n, false)[1] or ''
local add = vim.split(block, '\n', { plain = true })
if add[#add] == '' then table.remove(add) end
local head = {}
if n > 1 or last ~= '' then
  if last ~= '' then head[#head + 1] = '' end
  head[#head + 1] = '###'
end
local at = n
if n == 1 and last == '' then at = 0 end
local k = #head
vim.api.nvim_buf_set_lines(b, at, at, false, vim.list_extend(head, add))
return tostring(at + k + 1)`, &raw, file, block)
		if err == nil && raw != "" {
			fmt.Sscan(raw, &line)
			m.ensureEditorWindowCurrent()
			_ = m.nvim.ExecLuaArgs(`local f, l = ...
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_get_name(b) == f then vim.api.nvim_set_current_buf(b) end
end
pcall(vim.api.nvim_win_set_cursor, 0, { l, 0 })`, file, line)
			m.focus = FocusEditor
			return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Imported "+req.Method+" "+req.URL, filepath.Base(file)))
		}
	}
	old, _ := os.ReadFile(file)
	text := string(old)
	if strings.TrimSpace(text) != "" {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n###\n"
	}
	line := strings.Count(text, "\n") + 1
	text += block
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		return m.pushToastDetail(toast.Errr, "Import failed", err.Error())
	}
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`local f, l = ...
vim.cmd('edit ' .. vim.fn.fnameescape(f))
pcall(vim.api.nvim_win_set_cursor, 0, { l, 0 })`, file, line)
		m.focus = FocusEditor
	}
	m.explorer.Reload()
	return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Imported "+req.Method+" "+req.URL, filepath.Base(file)))
}

// httpSaveRequestPrompt asks for the name of the shown request.
func (m *Model) httpSaveRequestPrompt() tea.Cmd {
	s := m.httpS()
	r := s.shown
	if r == nil {
		r = s.last
	}
	if r == nil {
		if f := m.httpCurrentFile(); f != "" {
			if _, _, req, err := m.httpRequestFromBuffer(0, 0); err == nil {
				s.copyRun = &httpRun{rr: httpclient.Resolved{Request: *req, Template: *req}, label: req.Label()}
				return m.openHTTPPrompt(promptKindHTTPSaveName, "HTTP: Save Request", "Name:", req.Label())
			}
		}
		return m.pushToastDetail(toast.Info, "No request to save", "")
	}
	s.copyRun = r
	name := r.rr.Template.Name
	if name == "" {
		name = httpclient.DeriveName(r.rr.Template.Method, r.rr.Template.URL)
	}
	return m.openHTTPPrompt(promptKindHTTPSaveName, "HTTP: Save Request", "Name:", name)
}

// httpSaveRequest appends the request (as written, templates kept) to
// saved.http.
func (m *Model) httpSaveRequest(name string) tea.Cmd {
	r := m.httpS().copyRun
	if r == nil {
		return nil
	}
	req := r.rr.Template
	req.Name = name
	req.Headers = httpclient.RedactTemplateHeaders(req.Headers)
	req.Directives = nil
	p := httpSavedPath()
	b, _ := os.ReadFile(p)
	f := httpclient.Parse(string(b))
	f.Requests = append(f.Requests, req)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return m.pushToastDetail(toast.Errr, "Save failed", err.Error())
	}
	if err := os.WriteFile(p, []byte(httpclient.Serialize(f.Requests)), 0o600); err != nil {
		return m.pushToastDetail(toast.Errr, "Save failed", err.Error())
	}
	return m.pushToastDetail(toast.Info, "Saved request "+name, "HTTP: Saved Requests… lists it")
}

// httpSaveBodyPrompt asks where to save the shown body.
func (m *Model) httpSaveBodyPrompt() tea.Cmd {
	s := m.httpS()
	if s.shown == nil || s.shown.resp == nil || len(s.shown.resp.Body) == 0 {
		return m.pushToastDetail(toast.Info, "No response body to save", "")
	}
	s.saveBody = s.shown.resp.Body
	return m.openHTTPPrompt(promptKindHTTPSaveBody, "HTTP: Save Response Body", "File:", httpBodyFileName(s.shown))
}

// httpBodyFileName is the default name: the URL's last path part plus
// an extension from the content type ("users.json").
func httpBodyFileName(r *httpRun) string {
	base := "response"
	raw := r.rr.URL
	if r.resp.URL != "" {
		raw = r.resp.URL // after redirects
	}
	if u, err := url.Parse(raw); err == nil {
		if b := path.Base(u.Path); b != "" && b != "/" && b != "." {
			base = b
		}
	}
	ext := map[string]string{"application/json": ".json", "text/html": ".html", "application/xml": ".xml", "text/xml": ".xml",
		"text/plain": ".txt", "image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
		"application/pdf": ".pdf", "text/csv": ".csv"}[r.resp.ContentType]
	if ext == "" && r.resp.IsJSON {
		ext = ".json"
	}
	if ext != "" && !strings.HasSuffix(base, ext) {
		base += ext
	}
	return base
}

func (m *Model) httpSaveBody(name string) tea.Cmd {
	s := m.httpS()
	if name == "" || s.saveBody == nil {
		return nil
	}
	p := name
	if !filepath.IsAbs(p) {
		p = filepath.Join(aiWorkspaceRoot(), p)
	}
	if _, err := os.Stat(p); err == nil {
		return m.pushToastDetail(toast.Warn, name+" already exists", "Pick another name")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return m.pushToastDetail(toast.Errr, "Save failed", err.Error())
	}
	if err := os.WriteFile(p, s.saveBody, 0o644); err != nil {
		return m.pushToastDetail(toast.Errr, "Save failed", err.Error())
	}
	m.explorer.Reload()
	return m.pushToastDetail(toast.Info, "Saved "+name, httpclient.FormatSize(int64(len(s.saveBody))))
}

const httpNewFileTemplate = `### Example
# @name example
GET {{host}}/health
Accept: application/json

###
# @name create
POST {{host}}/items
Content-Type: application/json

{
  "name": "example"
}
`

func (m *Model) httpNewFile(name string) tea.Cmd {
	if name == "" {
		return nil
	}
	if !isHTTPPath(name) {
		name += ".http"
	}
	p := name
	if !filepath.IsAbs(p) {
		p = filepath.Join(aiWorkspaceRoot(), p)
	}
	if _, err := os.Stat(p); err != nil {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not create "+name, err.Error())
		}
		if err := os.WriteFile(p, []byte(httpNewFileTemplate), 0o644); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not create "+name, err.Error())
		}
		m.explorer.Reload()
	}
	return m.httpOpenInEditor(p)
}

func (m *Model) httpOpenInEditor(p string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`local f = ...
vim.cmd('edit ' .. vim.fn.fnameescape(f))`, p)
	m.focus = FocusEditor
	return m.fetchStateCmd()
}

// ── env files ───────────────────────────────────────────────────────────

const httpEnvTemplate = `{
  "$shared": {
    "version": "v1"
  },
  "dev": {
    "host": "http://localhost:8080"
  },
  "staging": {
    "host": "https://staging.example.com"
  }
}
`

const httpPrivateEnvTemplate = `{
  "dev": {
    "token": ""
  },
  "staging": {
    "token": ""
  }
}
`

// httpOpenEnvFile opens (and makes, with a template) the env file of
// the workspace; the private one is offered for .gitignore.
func (m *Model) httpOpenEnvFile(private bool) tea.Cmd {
	dir := aiWorkspaceRoot()
	if f := m.httpCurrentFile(); f != "" {
		dir = filepath.Dir(f)
	}
	root := httpRootFor(dir)
	name, tmpl, mode := httpclient.EnvFile, httpEnvTemplate, os.FileMode(0o644)
	if private {
		name, tmpl, mode = httpclient.PrivateEnvFile, httpPrivateEnvTemplate, 0o600
	}
	// An existing file nearer the .http file wins.
	p := filepath.Join(root, name)
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, name)); err == nil {
			p = filepath.Join(d, name)
			break
		}
		if d == root || filepath.Dir(d) == d {
			break
		}
	}
	if _, err := os.Stat(p); err != nil {
		if err := os.WriteFile(p, []byte(tmpl), mode); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not create "+name, err.Error())
		}
		m.explorer.Reload()
	}
	cmd := m.httpOpenInEditor(p)
	if private {
		rel, _ := filepath.Rel(root, p)
		if httpGitState(root, rel) == httpGitUnignored {
			m.httpS().envRoot = root
			m.confirm = confirm.New("Keep secrets out of git?",
				filepath.ToSlash(rel)+" holds tokens but git does not ignore it. Add it to .gitignore?",
				[]confirm.Button{{ID: "add", Title: "Add to .gitignore", Style: confirm.StylePrimary}, {ID: "cancel", Title: "Not now"}})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindHTTPGitignore
		}
	}
	return cmd
}

type httpGitStatus int

const (
	httpGitNone      httpGitStatus = iota // not a repo / git missing
	httpGitIgnored                        // ignored and not tracked
	httpGitUnignored                      // would be committed
	httpGitTracked                        // already in git
)

// httpGitState says what git does with rel (a path under root).
func httpGitState(root, rel string) httpGitStatus {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return httpGitNone
	}
	tracked := spawn.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", rel)
	if tracked.Run() == nil {
		return httpGitTracked
	}
	ig := spawn.Command("git", "-C", root, "check-ignore", "-q", "--", rel)
	err := ig.Run()
	if err == nil {
		return httpGitIgnored
	}
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return httpGitUnignored
	}
	return httpGitNone
}

// httpAddToGitignore appends the private env file name to .gitignore.
func httpAddToGitignore(root string) error {
	p := filepath.Join(root, ".gitignore")
	old, _ := os.ReadFile(p)
	text := string(old)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += httpclient.PrivateEnvFile + "\n"
	return os.WriteFile(p, []byte(text), 0o644)
}

// httpPrivateEnvCheckMsg carries the once-per-workspace warning.
type httpPrivateEnvCheckMsg struct {
	root, rel string
	state     httpGitStatus
}

// httpCheckPrivateEnv looks once per workspace (off the UI goroutine)
// whether a private env file would reach git.
func (m *Model) httpCheckPrivateEnv(file string) tea.Cmd {
	root := httpRootFor(filepath.Dir(file))
	s := m.httpS()
	if s.gitWarned[root] {
		return nil
	}
	s.gitWarned[root] = true
	dir := filepath.Dir(file)
	return func() tea.Msg {
		for d := dir; ; d = filepath.Dir(d) {
			p := filepath.Join(d, httpclient.PrivateEnvFile)
			if _, err := os.Stat(p); err == nil {
				rel, _ := filepath.Rel(root, p)
				if st := httpGitState(root, rel); st == httpGitUnignored || st == httpGitTracked {
					return httpPrivateEnvCheckMsg{root: root, rel: rel, state: st}
				}
			}
			if d == root || filepath.Dir(d) == d {
				return nil
			}
		}
	}
}

// handleHTTPConfirm routes the HTTP confirm cards. ok=false: not ours.
func (m *Model) handleHTTPConfirm(id string) (tea.Cmd, bool) {
	s := m.httpS()
	switch m.confirmKind {
	case confirmKindHTTPUnresolved:
		run := s.pending
		s.pending = nil
		if id != "send" || run == nil {
			return nil, true
		}
		return m.startHTTPRun(run, httpclient.Hooks{}), true
	case confirmKindHTTPCopySecrets:
		r := s.copyRun
		if r == nil {
			return nil, true
		}
		switch id {
		case "secrets":
			return m.copyWithToast(httpclient.ToCurl(r.rr, true), "Copied curl (with secrets)"), true
		case "template":
			return m.copyWithToast(httpclient.ToCurl(r.rr, false), "Copied curl"), true
		}
		return nil, true
	case confirmKindHTTPGitignore:
		if id != "add" {
			return nil, true
		}
		if err := httpAddToGitignore(s.envRoot); err != nil {
			return m.pushToastDetail(toast.Errr, ".gitignore not changed", err.Error()), true
		}
		return m.pushToastDetail(toast.Info, "Added "+httpclient.PrivateEnvFile+" to .gitignore", ""), true
	case confirmKindHTTPClearHistory:
		if id != "clear" {
			return nil, true
		}
		if err := httpHistoryStore().Clear(); err != nil {
			return m.pushToastDetail(toast.Errr, "Clear failed", err.Error()), true
		}
		return m.pushToastDetail(toast.Info, "HTTP history cleared", ""), true
	}
	return nil, false
}

// ── copy as curl ────────────────────────────────────────────────────────

// httpCopyCurlAction copies the request at the cursor (in an .http
// buffer) or the shown one.
func (m *Model) httpCopyCurlAction() tea.Cmd {
	if f := m.httpCurrentFile(); f != "" {
		file, pf, req, err := m.httpRequestFromBuffer(0, 0)
		if err != nil {
			return m.pushToastDetail(toast.Warn, "HTTP: "+err.Error(), "")
		}
		rv := httpResolver(file, pf.Vars, "")
		rr, _ := rv.Resolve(*req)
		return m.httpCopyCurl(&httpRun{rr: rr, file: file, label: req.Label()})
	}
	return m.httpCopyCurl(m.httpS().shown)
}

// httpCopyCurl copies a one-line curl. Secret values are copied as their
// {{template}}; with a secret in the request the user chooses.
func (m *Model) httpCopyCurl(r *httpRun) tea.Cmd {
	if r == nil {
		return m.pushToastDetail(toast.Info, "No request to copy", "")
	}
	m.httpS().copyRun = r
	plain, full := httpclient.ToCurl(r.rr, false), httpclient.ToCurl(r.rr, true)
	if plain == full {
		return m.copyWithToast(plain, "Copied curl")
	}
	m.confirm = confirm.New("Copy as curl",
		"The request holds secret values. Copy it with {{templates}} in their place, or with the secrets?",
		[]confirm.Button{{ID: "template", Title: "Copy with templates", Style: confirm.StylePrimary},
			{ID: "secrets", Title: "Copy with secrets", Style: confirm.StyleDestructive}, {ID: "cancel", Title: "Cancel"}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindHTTPCopySecrets
	return nil
}

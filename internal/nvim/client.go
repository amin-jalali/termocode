package nvim

import (
	"errors"
	"fmt"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	gonvim "github.com/neovim/go-client/nvim"
)

// RedrawMsg carries one batch of nvim redraw events. Each element is an event
// array of the form [name, instance1, instance2, ...] where name is a string
// and each instance is the args for one occurrence of that event.
type RedrawMsg struct {
	Events [][]any
}

// ReadyMsg is delivered after AttachUI succeeds.
type ReadyMsg struct{}

// ErrMsg wraps a fatal nvim error.
type ErrMsg struct{ Err error }

// Client wraps a spawned `nvim --embed` subprocess and exposes its UI events
// as a stream of tea.Msg values.
type Client struct {
	nv        *gonvim.Nvim
	events    chan tea.Msg
	fetchBusy atomic.Bool

	// mouseQueue serialises mouse events on a dedicated dispatcher
	// goroutine so MouseAsync can fire-and-forget without breaking
	// press → drag → release ordering. Buffered so a brief stall never
	// blocks the Bubble Tea Update loop; if it ever fills, the oldest
	// drag is dropped (selection still ends correctly because we never
	// drop press / release — see MouseAsync).
	mouseQueue chan mouseEvent
}

type mouseEvent struct {
	button, action, modifier string
	row, col                 int
}

// errFetchBusy is returned when FetchState is called while a previous call is
// still in flight. The caller should drop the request.
var errFetchBusy = errors.New("nvim: fetch state busy")

// New spawns nvim --embed and registers the redraw notification handler.
// The caller must call Attach before any redraw events will be produced.
func New() (*Client, error) {
	c := &Client{
		events:     make(chan tea.Msg, 1024),
		mouseQueue: make(chan mouseEvent, 256),
	}
	go c.mouseDispatcher()

	nv, err := gonvim.NewChildProcess(
		gonvim.ChildProcessCommand("nvim"),
		// -i NONE disables ShaDa (shared data) so termocode never reads or
		// writes the user's nvim history / marks / registers. Avoids E576
		// corruption errors and keeps each termocode session isolated.
		// -n disables swap files (we own the buffers via nvim_input).
		gonvim.ChildProcessArgs("--embed", "-i", "NONE", "-n"),
	)
	if err != nil {
		return nil, fmt.Errorf("spawn nvim: %w", err)
	}
	c.nv = nv

	if err := nv.RegisterHandler("redraw", func(events ...[]interface{}) {
		cp := make([][]any, len(events))
		for i, e := range events {
			cp[i] = e
		}
		select {
		case c.events <- RedrawMsg{Events: cp}:
		default:
			// Drop if the consumer is slow. nvim will resync via grid_clear+grid_line.
		}
	}); err != nil {
		_ = nv.Close()
		return nil, fmt.Errorf("register redraw handler: %w", err)
	}

	return c, nil
}

// Attach attaches as a UI client with ext_linegrid and rgb enabled.
func (c *Client) Attach(width, height int) error {
	return c.nv.AttachUI(width, height, map[string]interface{}{
		"ext_linegrid": true,
		"rgb":          true,
	})
}

// Input forwards a string of nvim keycodes (e.g. "<C-s>", "<CR>", "abc").
func (c *Client) Input(keys string) error {
	_, err := c.nv.Input(keys)
	return err
}

// Command runs an Ex command (e.g. ":edit foo.go", ":w").
func (c *Client) Command(cmd string) error {
	return c.nv.Command(cmd)
}

// BufferInfo describes one open nvim buffer.
type BufferInfo struct {
	ID       int
	Path     string
	Modified bool
	Filetype string
}

// Buffers returns the list of currently loaded, listed buffers and the id
// of the active one.
func (c *Client) Buffers() ([]BufferInfo, int, error) {
	var raw []interface{}
	err := c.nv.ExecLua(`
		local bufs = {}
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buflisted then
				table.insert(bufs, {b, vim.api.nvim_buf_get_name(b), vim.bo[b].modified, vim.bo[b].filetype})
			end
		end
		return { bufs, vim.api.nvim_get_current_buf() }
	`, &raw)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) < 2 {
		return nil, 0, nil
	}
	bufList, _ := raw[0].([]interface{})
	active := toInt(raw[1])

	var bufs []BufferInfo
	for _, b := range bufList {
		bArr, _ := b.([]interface{})
		if len(bArr) < 4 {
			continue
		}
		bufs = append(bufs, BufferInfo{
			ID:       toInt(bArr[0]),
			Path:     toString(bArr[1]),
			Modified: toBool(bArr[2]),
			Filetype: toString(bArr[3]),
		})
	}
	return bufs, active, nil
}

func toInt(v interface{}) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case uint32:
		return int(x)
	case uint64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return ""
}

func toBool(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	if i, ok := v.(int64); ok {
		return i != 0
	}
	return false
}

// State is a snapshot of nvim state used to drive the UI: the active buffer's
// metadata plus the full list of listed buffers and LSP diagnostic counts.
type State struct {
	Path       string
	Dirty      bool
	Lang       string
	Buffers    []BufferInfo
	Active     int
	Errors     int
	Warnings   int
	CursorLine int // 1-based; 0 when unknown
	// Cursors maps each listed buffer's absolute path to its last-known
	// (line, col), 1-based. The active buffer's entry reflects the live
	// cursor; inactive buffers report the `"` mark (position on last
	// buffer leave). Empty path keys are skipped.
	Cursors map[string][2]int
	// LastReloadPath holds the absolute path of the buffer most recently
	// reloaded from disk via the autoread/FileChangedShellPost path. The
	// counter is bumped on every reload so the consumer can detect a fresh
	// event by tracking the previous value (the path alone repeats on
	// successive reloads of the same file).
	LastReloadPath string
	LastReloadSeq  int
	// CurrentWin is the nvim window-id currently holding focus. Used by
	// the app's keymap dispatcher to detect "user is typing into the
	// integrated terminal" so terminal-meaningful shortcuts (Ctrl+C,
	// Tab, Ctrl+D, …) bypass the global keymap and reach the shell PTY.
	CurrentWin int

	// Mode is the first character of vim.api.nvim_get_mode().mode — 'n'
	// normal, 'i' insert, 't' terminal-insert, 'v' visual, etc. The same
	// terminal-passthrough check uses Mode == "t" to make sure the user is
	// actively typing into the PTY, not just hovering the terminal window
	// in Terminal-Normal mode (`nt`) where keys should still hit termocode.
	Mode string
}

// FetchState returns the current State in a single Lua call. If a previous
// call is still in flight, returns errFetchBusy and the caller should drop.
func (c *Client) FetchState() (State, error) {
	if !c.fetchBusy.CompareAndSwap(false, true) {
		return State{}, errFetchBusy
	}
	defer c.fetchBusy.Store(false)
	var raw []interface{}
	err := c.nv.ExecLua(`
		local bufs = {}
		local cursors = {}
		local cur_buf = vim.api.nvim_get_current_buf()
		-- Live cursor for the active window: the '"' mark only refreshes
		-- on BufLeave, so the active buffer needs to be queried directly.
		local ok_live, live_pos = pcall(vim.api.nvim_win_get_cursor, 0)
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buflisted then
				local name = vim.api.nvim_buf_get_name(b)
				table.insert(bufs, {b, name, vim.bo[b].modified, vim.bo[b].filetype})
				if name ~= '' then
					if b == cur_buf and ok_live and live_pos then
						-- nvim_win_get_cursor returns {line(1-based), col(0-based)};
						-- bump col to 1-based for parity with vim.fn.cursor().
						cursors[name] = { live_pos[1], live_pos[2] + 1 }
					else
						-- The '"' mark holds the cursor position from the last
						-- BufLeave on this buffer: exactly the "where I was"
						-- semantics we want for restoration.
						local ok_m, m = pcall(vim.api.nvim_buf_get_mark, b, '"')
						if ok_m and m and m[1] > 0 then
							cursors[name] = { m[1], m[2] + 1 }
						end
					end
				end
			end
		end
		local errors, warnings = 0, 0
		if vim.diagnostic and vim.diagnostic.get then
			for _, d in ipairs(vim.diagnostic.get(0)) do
				if d.severity == 1 then errors = errors + 1
				elseif d.severity == 2 then warnings = warnings + 1 end
			end
		end
		local cur_line = 0
		if ok_live and live_pos then cur_line = live_pos[1] end
		-- Current window + mode. The app's keymap dispatcher uses these
		-- to detect "user is typing into the integrated terminal" so
		-- terminal-meaningful shortcuts bypass the global keymap. Mode
		-- is the first byte of nvim_get_mode().mode: 't' = terminal-
		-- insert (PTY is consuming keys), 'n'/'i'/'v'/etc. = nvim is
		-- consuming keys.
		local cur_win = vim.api.nvim_get_current_win()
		local mode_full = vim.api.nvim_get_mode().mode or ''
		local mode_first = mode_full:sub(1, 1)
		return {
			vim.fn.expand('%:p'),
			vim.bo.modified,
			vim.bo.filetype,
			bufs,
			cur_buf,
			errors,
			warnings,
			cur_line,
			cursors,
			vim.g.termocode_last_reload or '',
			vim.g.termocode_reload_seq or 0,
			cur_win,
			mode_first
		}
	`, &raw)
	if err != nil {
		return State{}, err
	}
	var s State
	if len(raw) > 0 {
		s.Path = toString(raw[0])
	}
	if len(raw) > 1 {
		s.Dirty = toBool(raw[1])
	}
	if len(raw) > 2 {
		s.Lang = toString(raw[2])
	}
	if len(raw) > 3 {
		if list, ok := raw[3].([]interface{}); ok {
			for _, b := range list {
				bArr, ok := b.([]interface{})
				if !ok || len(bArr) < 4 {
					continue
				}
				s.Buffers = append(s.Buffers, BufferInfo{
					ID:       toInt(bArr[0]),
					Path:     toString(bArr[1]),
					Modified: toBool(bArr[2]),
					Filetype: toString(bArr[3]),
				})
			}
		}
	}
	if len(raw) > 4 {
		s.Active = toInt(raw[4])
	}
	if len(raw) > 5 {
		s.Errors = toInt(raw[5])
	}
	if len(raw) > 6 {
		s.Warnings = toInt(raw[6])
	}
	if len(raw) > 7 {
		s.CursorLine = toInt(raw[7])
	}
	if len(raw) > 8 {
		// Lua tables come across as map[string]interface{} when keyed by
		// strings (our buffer paths). Each value is a 2-element list of ints.
		if m, ok := raw[8].(map[string]interface{}); ok {
			s.Cursors = make(map[string][2]int, len(m))
			for path, v := range m {
				arr, ok := v.([]interface{})
				if !ok || len(arr) < 2 {
					continue
				}
				s.Cursors[path] = [2]int{toInt(arr[0]), toInt(arr[1])}
			}
		}
	}
	if len(raw) > 9 {
		s.LastReloadPath = toString(raw[9])
	}
	if len(raw) > 10 {
		s.LastReloadSeq = toInt(raw[10])
	}
	if len(raw) > 11 {
		s.CurrentWin = toInt(raw[11])
	}
	if len(raw) > 12 {
		s.Mode = toString(raw[12])
	}
	return s, nil
}

// Resize tells nvim the UI grid has been resized.
func (c *Client) Resize(width, height int) error {
	return c.nv.TryResizeUI(width, height)
}

// DiagnosticMark is a single LSP diagnostic location used by the scrollbar
// overview ruler. Severity is the LSP code (1=error, 2=warning, 3=info, 4=hint).
type DiagnosticMark struct {
	Line     int
	Severity int
}

// BufferDiagnosticsAndLines returns the diagnostic positions for the current
// buffer plus its total line count, in a single Lua call. Returns an empty
// list when there's no LSP attached or no diagnostics.
func (c *Client) BufferDiagnosticsAndLines() ([]DiagnosticMark, int, error) {
	var raw []interface{}
	err := c.nv.ExecLua(`
		local diags = {}
		if vim.diagnostic and vim.diagnostic.get then
			for _, d in ipairs(vim.diagnostic.get(0)) do
				table.insert(diags, { d.lnum + 1, d.severity })
			end
		end
		return { diags, vim.api.nvim_buf_line_count(0) }
	`, &raw)
	if err != nil || len(raw) < 2 {
		return nil, 0, err
	}
	total := toInt(raw[1])
	list, _ := raw[0].([]interface{})
	out := make([]DiagnosticMark, 0, len(list))
	for _, v := range list {
		row, ok := v.([]interface{})
		if !ok || len(row) < 2 {
			continue
		}
		out = append(out, DiagnosticMark{
			Line:     toInt(row[0]),
			Severity: toInt(row[1]),
		})
	}
	return out, total, nil
}

// Mouse forwards a mouse event to nvim. button is "left"/"right"/"middle"/"wheel";
// action is "press"/"release"/"drag"/"up"/"down". row and col are 0-based.
//
// This is the SYNCHRONOUS variant; the caller blocks for the msgpack
// round-trip. For interactive paths (drag-select) prefer MouseAsync.
func (c *Client) Mouse(button, action, modifier string, row, col int) error {
	return c.nv.InputMouse(button, action, modifier, 0, row, col)
}

// MouseAsync enqueues a mouse event onto the dispatcher goroutine and
// returns immediately. Events are delivered to nvim in the order they
// were enqueued, so press → drag → release semantics are preserved.
//
// Drop policy: when the queue is full (256 events backed up) we drop
// the new event ONLY when it's a "drag" — press / release / wheel
// always block until there's room, because losing them produces
// visibly broken state (a release that never reached nvim leaves the
// visual selection stuck). In practice the queue never fills under
// normal interactive use; the buffer exists to absorb brief stalls.
func (c *Client) MouseAsync(button, action, modifier string, row, col int) {
	ev := mouseEvent{button: button, action: action, modifier: modifier, row: row, col: col}
	if action == "drag" {
		select {
		case c.mouseQueue <- ev:
		default:
			// Queue full — drop this drag; the next one carries the
			// updated position so the selection still tracks the
			// cursor correctly.
		}
		return
	}
	c.mouseQueue <- ev
}

// mouseDispatcher runs for the lifetime of the Client and forwards
// queued mouse events to nvim sequentially.
func (c *Client) mouseDispatcher() {
	for ev := range c.mouseQueue {
		_ = c.nv.InputMouse(ev.button, ev.action, ev.modifier, 0, ev.row, ev.col)
	}
}

// Diagnostic is a single LSP diagnostic with full source information so the
// Problems pane can render it and jump to its location.
type Diagnostic struct {
	Path     string // absolute path (empty for [No Name] buffers)
	Line     int    // 1-based
	Col      int    // 1-based
	Severity int    // 1=error, 2=warning, 3=info, 4=hint
	Source   string // optional ("gopls", "rust-analyzer", …)
	Message  string
}

// WorkspaceSymbol is one entry returned by an LSP workspace/symbol query.
// File is an absolute path (LSP returns a URI; we resolve it on the Lua
// side).
type WorkspaceSymbol struct {
	Name      string
	Container string // optional containerName (class / module owning the symbol)
	Kind      int
	File      string
	Line      int // 1-based
	Col       int // 1-based
}

// WorkspaceSymbols asks every LSP attached to the current buffer for symbols
// matching `query`. Returns a flat list aggregated across servers. Empty
// query is fine — most servers interpret it as "give me the first N
// symbols", which is useful for the initial picker open.
func (c *Client) WorkspaceSymbols(query string) ([]WorkspaceSymbol, error) {
	var raw []interface{}
	err := c.nv.ExecLua(fmt.Sprintf(`
		local out = {}
		if not (vim.lsp and vim.lsp.buf_request_sync) then
			return out
		end
		local buf = vim.api.nvim_get_current_buf()
		local clients = (vim.lsp.get_clients or vim.lsp.get_active_clients)({ bufnr = buf })
		if not clients or #clients == 0 then
			return out
		end
		local params = { query = %q }
		for _, client in ipairs(clients) do
			local results, _ = client.request_sync('workspace/symbol', params, 1500, buf)
			if results and results.result then
				for _, s in ipairs(results.result) do
					local loc = s.location or {}
					local uri = loc.uri or ''
					local file = vim.uri_to_fname and vim.uri_to_fname(uri) or uri
					local r = (loc.range and loc.range.start) or { line = 0, character = 0 }
					table.insert(out, {
						s.name or '',
						s.containerName or '',
						s.kind or 0,
						file,
						(r.line or 0) + 1,
						(r.character or 0) + 1,
					})
				end
			end
		end
		return out
	`, query), &raw)
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceSymbol, 0, len(raw))
	for _, v := range raw {
		row, ok := v.([]interface{})
		if !ok || len(row) < 6 {
			continue
		}
		out = append(out, WorkspaceSymbol{
			Name:      toString(row[0]),
			Container: toString(row[1]),
			Kind:      toInt(row[2]),
			File:      toString(row[3]),
			Line:      toInt(row[4]),
			Col:       toInt(row[5]),
		})
	}
	return out, nil
}

// GotoTypeDefinition asks every attached LSP for the type-definition
// location at the cursor and jumps to the first non-empty answer. Returns
// true if any server answered with a usable location.
func (c *Client) GotoTypeDefinition() (bool, error) {
	return c.lspGotoMethod("textDocument/typeDefinition")
}

// GotoImplementation does the same for textDocument/implementation.
func (c *Client) GotoImplementation() (bool, error) {
	return c.lspGotoMethod("textDocument/implementation")
}

// lspGotoMethod is the shared logic behind GotoTypeDefinition / Goto-
// Implementation: send the request to every attached server, take the first
// location that comes back, and `:edit` + `cursor()` over to it.
func (c *Client) lspGotoMethod(method string) (bool, error) {
	var raw interface{}
	err := c.nv.ExecLua(fmt.Sprintf(`
		if not (vim.lsp and vim.lsp.buf_request_sync) then return false end
		local bufnr = vim.api.nvim_get_current_buf()
		local clients = (vim.lsp.get_clients or vim.lsp.get_active_clients)({ bufnr = bufnr })
		if not clients or #clients == 0 then return false end
		local params = vim.lsp.util.make_position_params(0, clients[1].offset_encoding or 'utf-16')
		local results, _ = vim.lsp.buf_request_sync(bufnr, %q, params, 1500)
		if not results then return false end
		for _, r in pairs(results) do
			local res = r.result
			if res then
				local loc = res
				if type(res) == 'table' and res[1] then loc = res[1] end
				if loc and loc.uri and loc.range then
					local file = vim.uri_to_fname and vim.uri_to_fname(loc.uri) or loc.uri
					local line = (loc.range.start.line or 0) + 1
					local col  = (loc.range.start.character or 0) + 1
					vim.cmd('edit +' .. line .. ' ' .. vim.fn.fnameescape(file))
					vim.fn.cursor(line, col)
					return true
				end
			end
		end
		return false
	`, method), &raw)
	if err != nil {
		return false, err
	}
	return toBool(raw), nil
}

// Hover queries the LSP for hover documentation at the cursor's current
// position and returns the markdown body. Empty string when no LSP is
// attached or the server has nothing to say at the cursor.
func (c *Client) Hover() (string, error) {
	var raw interface{}
	err := c.nv.ExecLua(`
		if not (vim.lsp and vim.lsp.buf_request_sync) then return '' end
		local bufnr = vim.api.nvim_get_current_buf()
		local clients = (vim.lsp.get_clients or vim.lsp.get_active_clients)({ bufnr = bufnr })
		if not clients or #clients == 0 then return '' end
		local params = vim.lsp.util.make_position_params(0, clients[1].offset_encoding or 'utf-16')
		local results, _ = vim.lsp.buf_request_sync(bufnr, 'textDocument/hover', params, 800)
		if not results then return '' end
		for _, r in pairs(results) do
			local result = r.result
			if result and result.contents then
				local c = result.contents
				if type(c) == 'string' then return c end
				if c.value then return c.value end
				if type(c) == 'table' and c[1] then
					-- Array of MarkedString values; concat plain strings.
					local parts = {}
					for _, item in ipairs(c) do
						if type(item) == 'string' then table.insert(parts, item)
						elseif item.value then table.insert(parts, item.value) end
					end
					return table.concat(parts, '\n\n')
				end
			end
		end
		return ''
	`, &raw)
	if err != nil {
		return "", err
	}
	return toString(raw), nil
}

// LocationItem is one entry returned by References / Definition / etc.
type LocationItem struct {
	File    string
	Line    int    // 1-based
	Col     int    // 1-based
	Preview string // a short snippet of the line (best-effort)
}

// References calls textDocument/references at the cursor and returns every
// location reported by the LSP server.
func (c *Client) References() ([]LocationItem, error) {
	var raw []interface{}
	err := c.nv.ExecLua(`
		local out = {}
		if not (vim.lsp and vim.lsp.buf_request_sync) then return out end
		local bufnr = vim.api.nvim_get_current_buf()
		local clients = (vim.lsp.get_clients or vim.lsp.get_active_clients)({ bufnr = bufnr })
		if not clients or #clients == 0 then return out end
		local params = vim.lsp.util.make_position_params(0, clients[1].offset_encoding or 'utf-16')
		params.context = { includeDeclaration = true }
		local results, _ = vim.lsp.buf_request_sync(bufnr, 'textDocument/references', params, 1500)
		if not results then return out end
		for _, r in pairs(results) do
			if r.result then
				for _, loc in ipairs(r.result) do
					local file = vim.uri_to_fname and vim.uri_to_fname(loc.uri) or loc.uri
					local r0 = (loc.range and loc.range.start) or { line = 0, character = 0 }
					-- Try to load the line for a preview snippet.
					local preview = ''
					local ok, lines = pcall(vim.fn.readfile, file, '', (r0.line or 0) + 1)
					if ok and #lines > 0 then
						preview = lines[#lines]
					end
					table.insert(out, { file, (r0.line or 0) + 1, (r0.character or 0) + 1, preview })
				end
			end
		end
		return out
	`, &raw)
	if err != nil {
		return nil, err
	}
	out := make([]LocationItem, 0, len(raw))
	for _, v := range raw {
		row, ok := v.([]interface{})
		if !ok || len(row) < 4 {
			continue
		}
		out = append(out, LocationItem{
			File:    toString(row[0]),
			Line:    toInt(row[1]),
			Col:     toInt(row[2]),
			Preview: toString(row[3]),
		})
	}
	return out, nil
}

// BookmarkEntry is one set global mark (A-Z) returned by Bookmarks().
type BookmarkEntry struct {
	Letter string // "A".."Z"
	File   string // absolute path; empty for marks in unnamed buffers
	Line   int    // 1-based
	Col    int    // 1-based
}

// Bookmarks returns every currently-set global mark A-Z. Unset marks are
// skipped. The list is in alphabetical order (A first), matching the order
// they were assigned by ToggleBookmark's "lowest available letter" rule.
func (c *Client) Bookmarks() ([]BookmarkEntry, error) {
	var raw []interface{}
	err := c.nv.ExecLua(`
		local out = {}
		for code = 65, 90 do
			local letter = string.char(code)
			local pos = vim.api.nvim_get_mark(letter, {})
			if pos[1] > 0 then
				table.insert(out, { letter, pos[4] or '', pos[1], pos[2] + 1 })
			end
		end
		return out
	`, &raw)
	if err != nil {
		return nil, err
	}
	out := make([]BookmarkEntry, 0, len(raw))
	for _, v := range raw {
		row, ok := v.([]interface{})
		if !ok || len(row) < 4 {
			continue
		}
		out = append(out, BookmarkEntry{
			Letter: toString(row[0]),
			File:   toString(row[1]),
			Line:   toInt(row[2]),
			Col:    toInt(row[3]),
		})
	}
	return out, nil
}

// CodeAction is one entry in the picker's "Quick Fix" overlay. Title is what
// the LSP reported (e.g. "Organize Imports"). Index is the position in the
// fetched action list — we round-trip it back to nvim when the user selects
// so the apply step can find the corresponding action without us having to
// serialise the whole CodeAction object.
type CodeAction struct {
	Index       int    // 0-based position in the underlying nvim-side action list
	Title       string // human-readable label
	Kind        string // LSP code-action kind (quickfix, refactor, source, …)
	Client      string // LSP client name (gopls, ts_ls, …) for disambiguation
	IsPreferred bool   // LSP `isPreferred` flag — usually the canonical Quick Fix
}

// FetchCodeActions asks each LSP attached to the current buffer for the code
// actions available at the cursor's current line. Returns a flat list, one
// per (client, action). The actions themselves stay parked inside nvim under
// `_termocode_code_actions` so ApplyCodeAction can resolve and apply them
// back without us shuttling LSP JSON across the RPC boundary.
//
// If no LSP is attached or no actions are available, the slice is empty and
// the caller should surface a "no actions" toast rather than opening an
// empty picker.
func (c *Client) FetchCodeActions() ([]CodeAction, error) {
	var raw []interface{}
	err := c.nv.ExecLua(`
		local out = {}
		_G._termocode_code_actions = {}
		if not (vim.lsp and vim.lsp.buf_request_sync) then
			return out
		end
		local buf = vim.api.nvim_get_current_buf()
		local clients = vim.lsp.get_clients and vim.lsp.get_clients({ bufnr = buf })
			or vim.lsp.get_active_clients({ bufnr = buf })
		if not clients or #clients == 0 then
			return out
		end
		local row = vim.api.nvim_win_get_cursor(0)[1] - 1
		-- Build the LSP request: actions at the cursor line, fetching every
		-- diagnostic on that line as context so the server can return both
		-- diagnostic-driven (Quick Fix) and free-standing (refactors) actions.
		local ctx = { diagnostics = {} }
		for _, d in ipairs(vim.diagnostic.get(buf, { lnum = row })) do
			table.insert(ctx.diagnostics, {
				range = {
					start = { line = d.lnum, character = d.col },
					['end'] = { line = d.end_lnum or d.lnum, character = d.end_col or d.col },
				},
				severity = d.severity,
				code = d.code,
				source = d.source,
				message = d.message,
			})
		end
		local params = {
			textDocument = vim.lsp.util.make_text_document_params(buf),
			range = {
				start = { line = row, character = 0 },
				['end'] = { line = row, character = 0 },
			},
			context = ctx,
		}
		local idx = 0
		for _, client in ipairs(clients) do
			local results, _ = client.request_sync('textDocument/codeAction', params, 800, buf)
			if results and results.result then
				for _, action in ipairs(results.result) do
					idx = idx + 1
					_G._termocode_code_actions[idx] = { client = client, action = action }
					table.insert(out, {
						idx,
						action.title or '',
						action.kind or '',
						client.name or '',
						action.isPreferred and 1 or 0,
					})
				end
			end
		end
		return out
	`, &raw)
	if err != nil {
		return nil, err
	}
	out := make([]CodeAction, 0, len(raw))
	for _, v := range raw {
		row, ok := v.([]interface{})
		if !ok || len(row) < 4 {
			continue
		}
		ca := CodeAction{
			Index:  toInt(row[0]),
			Title:  toString(row[1]),
			Kind:   toString(row[2]),
			Client: toString(row[3]),
		}
		if len(row) >= 5 {
			ca.IsPreferred = toInt(row[4]) != 0
		}
		out = append(out, ca)
	}
	return out, nil
}

// ApplyCodeAction resolves and applies the action at the given 1-based index
// from the most recent FetchCodeActions call. The action object itself lives
// inside nvim's Lua state (`_termocode_code_actions[index]`), so this just
// runs the standard "resolve → apply edit / execute command" dance there.
func (c *Client) ApplyCodeAction(index int) error {
	var dummy interface{}
	return c.nv.ExecLua(fmt.Sprintf(`
		local entry = _G._termocode_code_actions and _G._termocode_code_actions[%d]
		if not entry then return false end
		local client = entry.client
		local action = entry.action
		local function exec(a)
			if a.edit then
				vim.lsp.util.apply_workspace_edit(a.edit, client.offset_encoding or 'utf-16')
			end
			if a.command then
				if type(a.command) == 'table' then
					client.request('workspace/executeCommand', a.command, nil, vim.api.nvim_get_current_buf())
				else
					client.request('workspace/executeCommand', { command = a.command, arguments = a.arguments or {} }, nil, vim.api.nvim_get_current_buf())
				end
			end
		end
		-- Some servers send "data" in the action and require codeAction/resolve
		-- before edit/command are available. Run the resolve first when needed.
		local needs_resolve = action.edit == nil and action.command == nil and action.data ~= nil
		if needs_resolve and client.supports_method and client.supports_method('codeAction/resolve') then
			local resolved, _ = client.request_sync('codeAction/resolve', action, 800, vim.api.nvim_get_current_buf())
			if resolved and resolved.result then
				exec(resolved.result)
				return true
			end
		end
		exec(action)
		return true
	`, index), &dummy)
}

// AllDiagnostics returns every diagnostic across every loaded, listed buffer.
// Sorted by severity (errors first) and then by file then line, so the
// Problems pane shows the most-actionable items at the top.
func (c *Client) AllDiagnostics() ([]Diagnostic, error) {
	var raw []interface{}
	err := c.nv.ExecLua(`
		local out = {}
		if not (vim.diagnostic and vim.diagnostic.get) then
			return out
		end
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buflisted then
				local name = vim.api.nvim_buf_get_name(b)
				for _, d in ipairs(vim.diagnostic.get(b)) do
					table.insert(out, {
						name,
						d.lnum + 1,
						(d.col or 0) + 1,
						d.severity or 1,
						d.source or '',
						d.message or '',
					})
				end
			end
		end
		return out
	`, &raw)
	if err != nil {
		return nil, err
	}
	out := make([]Diagnostic, 0, len(raw))
	for _, v := range raw {
		row, ok := v.([]interface{})
		if !ok || len(row) < 6 {
			continue
		}
		out = append(out, Diagnostic{
			Path:     toString(row[0]),
			Line:     toInt(row[1]),
			Col:      toInt(row[2]),
			Severity: toInt(row[3]),
			Source:   toString(row[4]),
			Message:  toString(row[5]),
		})
	}
	return out, nil
}

// ExecLua runs a Lua chunk in nvim. Discards the return value.
func (c *Client) ExecLua(code string) error {
	var result interface{}
	return c.nv.ExecLua(code, &result)
}

// EvalLuaString runs a Lua chunk and returns its (string) return value.
// Used for one-off "what's the line at cursor" style queries that don't
// fit the redraw stream model. Empty string is returned both for an empty
// Lua return and for a non-string return type — callers should treat it
// as "no value".
func (c *Client) EvalLuaString(code string) (string, error) {
	var s string
	if err := c.nv.ExecLua(code, &s); err != nil {
		return "", err
	}
	return s, nil
}

// DocSymbol mirrors a single LSP DocumentSymbol entry, flattened into the
// fields termocode actually renders. SelectionRange (the symbol *name*) is
// preferred as the jump target; we fall back to Range when missing.
type DocSymbol struct {
	Name     string
	Detail   string
	Kind     int // LSP SymbolKind (1=File, 5=Class, 6=Method, 12=Function, ...)
	Line     int // 1-based line of the selection range start
	Col      int // 1-based column of the selection range start
	Children []DocSymbol
}

// DocumentSymbols asks the active buffer's LSP servers for the document
// symbol tree via textDocument/documentSymbol. Returns an empty slice when no
// server is attached or no symbols are reported. The Lua side enforces a
// 1s timeout so a slow LSP can't stall the UI.
//
// Both response shapes (DocumentSymbol[] hierarchical, SymbolInformation[]
// flat) are normalized into a tree.
func (c *Client) DocumentSymbols() ([]DocSymbol, error) {
	var raw interface{}
	err := c.nv.ExecLua(`
		local bufnr = vim.api.nvim_get_current_buf()
		local clients = (vim.lsp.get_clients or vim.lsp.get_active_clients)({ bufnr = bufnr })
		if not clients or #clients == 0 then
			return { kind = 'no_lsp' }
		end
		local params = { textDocument = vim.lsp.util.make_text_document_params(bufnr) }
		local ok, results = pcall(vim.lsp.buf_request_sync, bufnr, 'textDocument/documentSymbol', params, 1000)
		if not ok or not results then
			return { kind = 'empty', symbols = {} }
		end
		local symbols = {}
		for _, r in pairs(results) do
			if r and r.result then
				for _, s in ipairs(r.result) do
					table.insert(symbols, s)
				end
			end
		end
		-- Normalize: DocumentSymbol has .range/.selectionRange/.children;
		-- SymbolInformation has .location.range and a flat list with
		-- .containerName. We pass both shapes back as-is and let Go side
		-- handle the field names.
		return { kind = 'ok', symbols = symbols }
	`, &raw)
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	switch toString(m["kind"]) {
	case "no_lsp":
		return nil, errNoLSP
	case "empty":
		return nil, nil
	}
	list, _ := m["symbols"].([]interface{})
	return parseSymbols(list), nil
}

// errNoLSP signals that no LSP client is attached to the active buffer; the
// symbol picker uses this to show its empty state.
var errNoLSP = errors.New("nvim: no LSP attached")

// IsNoLSP reports whether err came from DocumentSymbols meaning no LSP was
// attached (vs a transport error).
func IsNoLSP(err error) bool { return errors.Is(err, errNoLSP) }

func parseSymbols(list []interface{}) []DocSymbol {
	out := make([]DocSymbol, 0, len(list))
	for _, raw := range list {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		s := DocSymbol{
			Name:   toString(m["name"]),
			Detail: toString(m["detail"]),
			Kind:   toInt(m["kind"]),
		}
		// DocumentSymbol shape: range + selectionRange + children.
		if sel := pickRange(m["selectionRange"], m["range"]); sel != nil {
			s.Line, s.Col = rangeStart(sel)
		} else if loc, ok := m["location"].(map[string]interface{}); ok {
			// SymbolInformation shape: location.range.
			if rng, ok := loc["range"].(map[string]interface{}); ok {
				s.Line, s.Col = rangeStart(rng)
			}
		}
		if children, ok := m["children"].([]interface{}); ok {
			s.Children = parseSymbols(children)
		}
		if s.Name == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// pickRange returns the first non-nil range map among the candidates.
func pickRange(candidates ...interface{}) map[string]interface{} {
	for _, c := range candidates {
		if m, ok := c.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}

// rangeStart extracts the 1-based (line, col) from an LSP Range map. LSP
// uses 0-based line and 0-based UTF-16 column; we convert to 1-based for
// :call cursor() compatibility.
func rangeStart(r map[string]interface{}) (line, col int) {
	start, _ := r["start"].(map[string]interface{})
	if start == nil {
		return 1, 1
	}
	line = toInt(start["line"]) + 1
	col = toInt(start["character"]) + 1
	if line < 1 {
		line = 1
	}
	if col < 1 {
		col = 1
	}
	return line, col
}

// BufferContent returns the active buffer's full text.
func (c *Client) BufferContent() (string, error) {
	var s string
	err := c.nv.ExecLua(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &s)
	return s, err
}

// Close terminates the nvim subprocess.
func (c *Client) Close() error {
	if c.mouseQueue != nil {
		close(c.mouseQueue)
	}
	return c.nv.Close()
}

// Next returns a tea.Cmd that blocks for the next event from nvim and
// returns it as a tea.Msg. Re-invoke after each delivery to keep the stream
// flowing.
func (c *Client) Next() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-c.events
		if !ok {
			return nil
		}
		return msg
	}
}


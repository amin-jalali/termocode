package nvim

import (
	"fmt"
)

// NotifyMsg carries one rpcnotify() call from Lua to Go. Lua sends it with
//
//	vim.rpcnotify(vim.g.termocode_channel, method, arg1, arg2, ...)
//
// or the shorter _G.termocode_notify(method, ...) helper that Attach
// installs. Only methods subscribed via RegisterNotify reach the Go side;
// Args holds the decoded msgpack values in call order (string, int64,
// float64, bool, []any, map[string]any, nil).
type NotifyMsg struct {
	Method string
	Args   []any
}

// RegisterNotify subscribes to rpcnotify calls for `method`. Every call is
// delivered as a NotifyMsg through Next(), interleaved with the redraw
// stream.
//
// fn is optional (nil = always forward). When set it runs on the RPC reader
// goroutine BEFORE the message is queued and may return false to drop it —
// a cheap filter for very chatty events. It must not block and must not
// touch Bubble Tea state.
//
// Register before Lua starts sending: the RPC layer discards notifications
// for unknown methods.
func (c *Client) RegisterNotify(method string, fn func(args []any) bool) error {
	if method == "" || method == "redraw" {
		return fmt.Errorf("nvim: invalid notify method %q", method)
	}
	return c.nv.RegisterHandler(method, func(args ...interface{}) {
		cp := make([]any, len(args))
		copy(cp, args)
		if fn != nil && !fn(cp) {
			return
		}
		select {
		case c.events <- NotifyMsg{Method: method, Args: cp}:
		default:
			// Drop if the consumer is slow — same policy as redraw. Senders
			// that need delivery guarantees should re-send on the next event.
		}
	})
}

// ChannelID returns this client's nvim channel id (from nvim_get_api_info).
// Lua passes it as the first argument of vim.rpcnotify to reach us. Cached
// after the first successful call; 0 means the lookup failed.
func (c *Client) ChannelID() int {
	return c.nv.ChannelID()
}

// exportChannel publishes the channel id to Lua as vim.g.termocode_channel
// and installs _G.termocode_notify(method, ...) — a nil-safe wrapper so Lua
// helpers never look the id up themselves.
func (c *Client) exportChannel() error {
	id := c.ChannelID()
	if id <= 0 {
		return fmt.Errorf("nvim: no channel id")
	}
	return c.ExecLua(fmt.Sprintf(`
		vim.g.termocode_channel = %d
		_G.termocode_notify = function(method, ...)
			local ch = vim.g.termocode_channel
			if not ch or ch <= 0 then return end
			pcall(vim.rpcnotify, ch, method, ...)
		end
	`, id))
}

// CursorInfo is a snapshot of the text around the cursor in the current
// window — the input for inline completion, AI prompts and similar
// "what is the user looking at" features.
type CursorInfo struct {
	Path     string // absolute buffer name ("" for unnamed buffers)
	Filetype string
	Line     int // 1-based
	Col      int // 1-based byte column of the insert position
	// Before is the text from the start of the window up to the cursor;
	// After is the text from the cursor to the end of the window. Lines
	// are joined with "\n", so Before+After is the windowed buffer text.
	Before string
	After  string
}

// Default line windows used by CursorContext.
const (
	CursorContextBefore = 200
	CursorContextAfter  = 100
)

// CursorContext returns the cursor context of the current window, with up
// to CursorContextBefore lines above and CursorContextAfter lines below.
func (c *Client) CursorContext() (CursorInfo, error) {
	return c.CursorContextWindow(CursorContextBefore, CursorContextAfter)
}

// CursorContextWindow is CursorContext with explicit line windows. The
// cursor line itself is always included (split between Before and After).
func (c *Client) CursorContextWindow(linesBefore, linesAfter int) (CursorInfo, error) {
	if linesBefore < 0 {
		linesBefore = 0
	}
	if linesAfter < 0 {
		linesAfter = 0
	}
	var raw []interface{}
	err := c.nv.ExecLua(fmt.Sprintf(`
		local nb, na = %d, %d
		local buf = vim.api.nvim_get_current_buf()
		local pos = vim.api.nvim_win_get_cursor(0)
		local row, col = pos[1], pos[2]
		local total = vim.api.nvim_buf_line_count(buf)
		local first = math.max(row - nb, 1)
		local last = math.min(row + na, total)
		local lines = vim.api.nvim_buf_get_lines(buf, first - 1, last, false)
		local cur = lines[row - first + 1] or ''
		local before = {}
		for i = 1, row - first do before[#before + 1] = lines[i] end
		before[#before + 1] = cur:sub(1, col)
		local after = { cur:sub(col + 1) }
		for i = row - first + 2, #lines do after[#after + 1] = lines[i] end
		return {
			vim.api.nvim_buf_get_name(buf), vim.bo[buf].filetype, row, col + 1,
			table.concat(before, '\n'), table.concat(after, '\n'),
		}
	`, linesBefore, linesAfter), &raw)
	if err != nil {
		return CursorInfo{}, err
	}
	return parseCursorInfo(raw), nil
}

// parseCursorInfo decodes the 6-tuple returned by CursorContextWindow's Lua.
func parseCursorInfo(raw []interface{}) CursorInfo {
	var ci CursorInfo
	if len(raw) < 6 {
		return ci
	}
	ci.Path = toString(raw[0])
	ci.Filetype = toString(raw[1])
	ci.Line = toInt(raw[2])
	ci.Col = toInt(raw[3])
	ci.Before = toString(raw[4])
	ci.After = toString(raw[5])
	return ci
}

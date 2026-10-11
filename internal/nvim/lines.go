package nvim

// CurrentBufLines returns every line of the current buffer.
func (c *Client) CurrentBufLines() ([]string, error) {
	var lines []string
	err := c.execLua(`return vim.api.nvim_buf_get_lines(0, 0, -1, false)`, &lines)
	return lines, err
}

// ReplaceCurrentBufLines replaces the 0-based, end-exclusive line range
// [start, end) of the current buffer with lines in a single
// nvim_buf_set_lines call — one undo step — then puts the cursor on the
// first replaced line. Lines travel as RPC arguments, so no Lua quoting of
// user text is needed.
func (c *Client) ReplaceCurrentBufLines(start, end int, lines []string) error {
	if lines == nil {
		lines = []string{}
	}
	var result interface{}
	return c.execLua(`
		local s, e, repl = ...
		vim.api.nvim_buf_set_lines(0, s, e, false, repl)
		local last = math.max(vim.api.nvim_buf_line_count(0), 1)
		pcall(vim.api.nvim_win_set_cursor, 0, { math.min(s + 1, last), 0 })
	`, &result, start, end, lines)
}

// ExecLuaArgs runs a Lua chunk with positional arguments (read in Lua via
// `...`). Arguments are sent over RPC, so paths and user text need no
// escaping.
func (c *Client) ExecLuaArgs(code string, args ...interface{}) error {
	var result interface{}
	return c.execLua(code, &result, args...)
}

// ExecLuaResult runs a Lua chunk with positional arguments and decodes its
// return value into result (Group A: AI proposal apply).
func (c *Client) ExecLuaResult(code string, result interface{}, args ...interface{}) error {
	return c.execLua(code, result, args...)
}

// EvalLuaArgs is ExecLuaArgs that decodes the chunk's return value into
// result (a pointer, e.g. *string, *[]string, *int64).
func (c *Client) EvalLuaArgs(code string, result interface{}, args ...interface{}) error {
	return c.execLua(code, result, args...)
}

// OpenFile runs an Ex command that takes a file name (edit, vsplit, ...)
// on path. The name goes through fnameescape: in a plain "edit " + path,
// Vim reads % and # as the current / alternate file, so "100%.txt"
// opened "100<current file>.txt".
func (c *Client) OpenFile(cmd, path string) error {
	return c.ExecLuaArgs(`local cmd, path = ...
vim.cmd(cmd .. ' ' .. vim.fn.fnameescape(path))`, cmd, path)
}

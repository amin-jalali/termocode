package nvim

// CurrentBufLines returns every line of the current buffer.
func (c *Client) CurrentBufLines() ([]string, error) {
	var lines []string
	err := c.nv.ExecLua(`return vim.api.nvim_buf_get_lines(0, 0, -1, false)`, &lines)
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
	return c.nv.ExecLua(`
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
	return c.nv.ExecLua(code, &result, args...)
}

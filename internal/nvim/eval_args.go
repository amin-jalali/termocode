package nvim

// EvalLuaStringArgs is EvalLuaString with positional arguments (read in Lua
// via `...`). Arguments travel over RPC, so JSON blobs, paths and user text
// need no Lua quoting. A non-string return yields "".
func (c *Client) EvalLuaStringArgs(code string, args ...interface{}) (string, error) {
	var s string
	if err := c.nv.ExecLua(code, &s, args...); err != nil {
		return "", err
	}
	return s, nil
}

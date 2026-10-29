package nvim

import "fmt"

// RegisterRequest answers a synchronous request from Lua:
//
//	vim.rpcrequest(vim.g.termocode_channel, method, arg1, ...)
//
// fn gets the decoded arguments and returns the reply (any msgpack value)
// or an error, which Lua sees as a raised error. fn runs on its own
// goroutine while nvim waits for the answer: it must not call back into
// nvim (that would deadlock) and must not touch Bubble Tea state.
//
// Register before Lua sends the first request.
func (c *Client) RegisterRequest(method string, fn func(args []any) (any, error)) error {
	if method == "" || method == "redraw" || fn == nil {
		return fmt.Errorf("nvim: invalid request method %q", method)
	}
	return c.nv.RegisterHandler(method, func(args ...interface{}) (interface{}, error) {
		cp := make([]any, len(args))
		copy(cp, args)
		return fn(cp)
	})
}

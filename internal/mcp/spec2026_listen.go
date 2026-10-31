package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/extproc"
)

// subscriptions/listen (2026-07-28, MCPX-20): a stateless server sends
// change notifications only on a long request the client keeps open.
// termocode opens one per connection for the tool, prompt and resource
// list changes; its notifications reach the same handlers as before
// (Client.handle). A stream that ends (the server closed it, the
// network broke) is opened again after a pause that grows to
// listenMaxRetry; a server that refuses the method is not asked again.

const (
	listenRetry    = time.Second
	listenMaxRetry = 30 * time.Second
)

// listenFilter is what termocode asks to be told about.
func listenFilter() map[string]any {
	return map[string]any{"notifications": map[string]any{
		"toolsListChanged": true, "promptsListChanged": true, "resourcesListChanged": true}}
}

// startListen keeps the subscription open while the connection lives.
func (c *Client) startListen() {
	ctx, cancel := context.WithCancel(context.Background())
	crash.Go("an MCP subscription", func() {
		<-c.conn.Done()
		cancel()
	})
	crash.Go("an MCP subscription", func() {
		wait := listenRetry
		for {
			start := time.Now()
			_, err := c.call(ctx, "subscriptions/listen", listenFilter())
			if ctx.Err() != nil {
				return
			}
			var re *extproc.Error
			if errors.As(err, &re) {
				c.opts.log("the server has no change stream: %s", re.Message)
				return
			}
			select {
			case <-c.conn.Done():
				return // the connection ended
			default:
			}
			if time.Since(start) > listenMaxRetry {
				wait = listenRetry // it ran a long time: not a fast loop
			}
			if err != nil {
				c.opts.log("the change stream broke (%v); listening again in %s", err, wait)
			} else {
				c.opts.log("the server ended the change stream; listening again in %s", wait)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = min(wait*2, listenMaxRetry)
		}
	})
}

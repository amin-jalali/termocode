package mcp

import (
	"context"
	"fmt"
	"sync"
)

// maxTurnFailures is how many failed calls one server may have in one
// chat turn before its tools stop calling it.
const maxTurnFailures = 2

// TurnCaller wraps call for one chat turn: after two failed calls
// (errors, not tool results with isError) to the same server, its tools
// answer "unavailable, do not retry" without calling it, so the agent
// does not loop on a broken server. A cancelled turn is not counted.
func TurnCaller(call CallFunc) CallFunc {
	var mu sync.Mutex
	failed := map[string]int{}
	return func(ctx context.Context, server, tool string, args map[string]any) (CallResult, error) {
		mu.Lock()
		n := failed[server]
		mu.Unlock()
		if n >= maxTurnFailures {
			return CallResult{}, fmt.Errorf("MCP server %s is unavailable for the rest of this turn (%d failed calls); do not retry its tools now, tell the user", server, n)
		}
		res, err := call(ctx, server, tool, args)
		if err != nil && ctx.Err() == nil {
			mu.Lock()
			failed[server]++
			mu.Unlock()
		}
		return res, err
	}
}

package mcp

import (
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// ttlMs of tools/list (2026-07-28): the server says how long its list
// may be cached. The manager keeps the end in memory and in the disk
// cache; an expired list is listed again on next use. No ttlMs: the list
// stays until the server says it changed (as before). cacheScope is not
// read yet: termocode's cache is per user already.

// ToolsTTL is the ttlMs of the last full tools/list (0: none).
func (c *Client) ToolsTTL() time.Duration {
	return time.Duration(c.toolsTTL.Load()) * time.Millisecond
}

func ttlExpiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return clock.Now().Add(ttl)
}

func expired(t time.Time) bool { return !t.IsZero() && clock.Now().After(t) }

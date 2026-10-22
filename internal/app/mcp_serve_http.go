package app

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// http_send for outside agents (M1 MS-7): the chat's tool with the chat's
// gate, unchanged (httpAIGate: GET / HEAD / OPTIONS to loopback run at
// once; other hosts and methods ask; private hosts always ask). Variables
// such as {{$secret NAME}} and private env values are resolved inside
// termocode, registered as secrets and hidden in the card, the audit line
// and the result. Two additions:
//
//   - the card says when the request carries such secret values, since
//     the card shows them hidden;
//   - "Always allow" is per host (http_send:<host>, or <host>/write for
//     other methods), like the chat's class, never per tool.

// mcpServeHTTPSend is http_send with the card note.
func mcpServeHTTPSend(t ai.Tool, root string) ai.Tool {
	describe := t.Describe
	t.Describe = func(a map[string]any) string {
		text := ""
		if describe != nil {
			text = describe(a)
		}
		if n := len(httpAIGet(root, a).rr.Secrets); n > 0 {
			text += fmt.Sprintf("\nSends %d secret value%s from your env files or secret store (hidden here).", n, plurals(n))
		}
		return text
	}
	return t
}

// mcpServeHTTPAlwaysKey is the "Always allow" key of a gated http_send
// call: per host from its class, "" when the class is "" (private hosts).
func mcpServeHTTPAlwaysKey(class string) string {
	host, ok := strings.CutPrefix(class, "http:")
	if !ok || host == "" {
		return ""
	}
	return "http_send:" + host
}

// mcpServeForgetHTTP drops the request http_send built for args
// (httpAICache is keyed by the map's address: a later map at the same
// address must never get this call's request).
func mcpServeForgetHTTP(args map[string]any) {
	if args != nil {
		httpAICache.Delete(httpAIKey(args))
	}
}

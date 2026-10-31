package ai

import (
	"encoding/json"
	"strings"
)

// Prompt caching (OMX-1). Anthropic bills a cached prompt prefix at a
// fraction of the input price. Every request marks three cache points:
// the last tool, the last system block (tools + system render first, so
// this one covers both) and the top-level automatic point, which the
// API moves to the end of the growing conversation. Each agent step then
// reads the whole earlier turn from the cache.
//
// config.json "ai_prompt_cache" picks the cache life: "5m" (default),
// "1h" (writes cost more, reads last an hour: for long pauses) or "off".

// Values of Config.PromptCache ("" is the 5 minute cache).
const (
	PromptCache1h  = "1h"
	PromptCacheOff = "off"
)

// PromptCacheModes are the values of ai_prompt_cache.
var PromptCacheModes = []string{"5m", PromptCache1h, PromptCacheOff}

// promptCacheMode turns an ai_prompt_cache value into Config.PromptCache;
// an unknown value is the default.
func promptCacheMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case PromptCache1h:
		return PromptCache1h
	case PromptCacheOff:
		return PromptCacheOff
	}
	return ""
}

// anthropicBody is the Messages API body run sends: buildAnthropicBody
// plus the options of cfg (prompt cache).
func anthropicBody(req Request, model string, cfg Config) map[string]any {
	body := buildAnthropicBody(req, model, cfg.Kind == KindClaudeOAuth)
	addPromptCache(body, cfg.PromptCache)
	return body
}

// addPromptCache marks the cache points of body (see the top of the
// file). The tools' shared JSON is not changed: the last entry is copied.
func addPromptCache(body map[string]any, mode string) {
	if mode == PromptCacheOff {
		return
	}
	mark := map[string]any{"type": "ephemeral"}
	raw := `{"type":"ephemeral"}`
	if mode == PromptCache1h {
		mark["ttl"] = "1h"
		raw = `{"ttl":"1h","type":"ephemeral"}`
	}
	if sys, ok := body["system"].([]map[string]any); ok && len(sys) > 0 {
		sys[len(sys)-1]["cache_control"] = mark
	}
	if tools, ok := body["tools"].([]json.RawMessage); ok && len(tools) > 0 {
		last := tools[len(tools)-1]
		if n := len(last); n > 1 && last[n-1] == '}' {
			out := make([]json.RawMessage, len(tools))
			copy(out, tools)
			sep := ","
			if strings.TrimSpace(string(last[:n-1])) == "{" {
				sep = ""
			}
			b := make([]byte, 0, n+len(raw)+20)
			b = append(b, last[:n-1]...)
			b = append(b, sep+`"cache_control":`+raw+`}`...)
			out[len(out)-1] = b
			body["tools"] = out
		}
	}
	body["cache_control"] = mark
}

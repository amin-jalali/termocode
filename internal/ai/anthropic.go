package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
)

// anthropicProvider streams the Anthropic Messages API (raw SSE over
// net/http). Port of mobocode's anthropic_provider.dart, both auth modes:
//
//   - API key: x-api-key header.
//   - OAuth (claude-oauth, opt-in): Authorization: Bearer + the
//     anthropic-beta oauth header, with the Claude Code identity as the
//     first system block (the API requires it for subscription tokens).
//
// Sampling parameters are never sent: current Claude models reject them.
type anthropicProvider struct{ cfg Config }

const (
	anthropicVersion      = "2023-06-01"
	anthropicOAuthBeta    = "oauth-2025-04-20"
	anthropicMaxTokens    = 32000
	claudeCodeIdentity    = "You are Claude Code, Anthropic's official CLI for Claude."
	anthropicRefusalError = errString("the model declined this request (refusal)")
)

func (p *anthropicProvider) Name() string        { return p.cfg.Kind }
func (p *anthropicProvider) Configured() bool    { return p.cfg.Configured() }
func (p *anthropicProvider) SupportsTools() bool { return true }

func (p *anthropicProvider) Stream(ctx context.Context, req Request) <-chan Chunk {
	ch := make(chan Chunk, 16)
	go func() {
		defer close(ch)
		ch <- p.run(ctx, req, ch)
	}()
	return ch
}

// buildAnthropicBody maps a Request onto the Messages API body. System
// messages are lifted into "system"; tool results become user
// tool_result blocks (consecutive results share one user turn); assistant
// turns replay their raw Blocks when present (thinking signatures must be
// echoed unchanged before tool results).
func buildAnthropicBody(req Request, model string, oauth bool) map[string]any {
	var system []map[string]any
	if oauth {
		system = append(system, map[string]any{"type": "text", "text": claudeCodeIdentity})
	}
	var msgs []map[string]any
	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			if m.Content != "" {
				system = append(system, map[string]any{"type": "text", "text": m.Content})
			}
		case RoleUser:
			msgs = append(msgs, map[string]any{"role": "user", "content": m.Content})
		case RoleAssistant:
			if len(m.Blocks) > 0 {
				msgs = append(msgs, map[string]any{"role": "assistant", "content": m.Blocks})
				continue
			}
			if len(m.ToolCalls) == 0 {
				msgs = append(msgs, map[string]any{"role": "assistant", "content": m.Content})
				continue
			}
			var blocks []map[string]any
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Args
				if args == nil {
					args = map[string]any{}
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": args})
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": blocks})
		case RoleTool:
			block := map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content}
			if n := len(msgs); n > 0 && msgs[n-1]["role"] == "user" {
				if prev, ok := msgs[n-1]["content"].([]map[string]any); ok {
					msgs[n-1]["content"] = append(prev, block)
					continue
				}
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": []map[string]any{block}})
		}
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = anthropicMaxTokens
	}
	body := map[string]any{
		"model":      model,
		"messages":   msgs,
		"max_tokens": maxTok,
		"stream":     true,
	}
	if len(system) > 0 {
		body["system"] = system
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Schema,
			})
		}
		body["tools"] = tools
	}
	return body
}

func (p *anthropicProvider) run(ctx context.Context, req Request, ch chan<- Chunk) Chunk {
	if !p.Configured() {
		return Chunk{Done: true, Err: ErrNotConfigured}
	}
	oauth := p.cfg.Kind == KindClaudeOAuth
	var token string
	if oauth {
		t, err := p.cfg.Token(ctx)
		if err != nil || t == "" {
			if err == nil {
				err = errString("not signed in to Claude")
			}
			return Chunk{Done: true, Err: errString(Redact(err.Error()))}
		}
		token = t
	}
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}
	payload, err := json.Marshal(buildAnthropicBody(req, model, oauth))
	if err != nil {
		return Chunk{Done: true, Err: err}
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, trimSlash(p.cfg.BaseURL)+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return Chunk{Done: true, Err: err}
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	hreq.Header.Set("anthropic-version", anthropicVersion)
	if oauth {
		hreq.Header.Set("Authorization", "Bearer "+token)
		hreq.Header.Set("anthropic-beta", anthropicOAuthBeta)
	} else {
		hreq.Header.Set("x-api-key", p.cfg.APIKey)
	}
	resp, err := p.cfg.HTTPClient.Do(hreq)
	if err != nil {
		return Chunk{Done: true, Err: errString(Redact(err.Error()))}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return Chunk{Done: true, Err: httpError(resp.StatusCode, body)}
	}
	return parseAnthropicSSE(ctx, resp.Body, ch)
}

// anthropicBlock accumulates one streamed content block.
type anthropicBlock struct {
	Type      string
	Text      strings.Builder
	Thinking  strings.Builder
	Signature string
	Data      string // redacted_thinking
	ID, Name  string
	JSON      strings.Builder
	Input     map[string]any
}

// parseAnthropicSSE reads the event stream: text deltas go to ch live;
// thinking / tool_use blocks are accumulated and returned (with the raw
// replay Blocks) on the final chunk.
func parseAnthropicSSE(ctx context.Context, r io.Reader, ch chan<- Chunk) Chunk {
	blocks := map[int]*anthropicBlock{}
	stopReason := ""
	finish := func() Chunk {
		out := Chunk{Done: true}
		if stopReason == "refusal" {
			out.Err = anthropicRefusalError
		}
		idx := make([]int, 0, len(blocks))
		for i := range blocks {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		var raw []map[string]any
		for _, i := range idx {
			b := blocks[i]
			switch b.Type {
			case "text":
				if b.Text.Len() > 0 {
					raw = append(raw, map[string]any{"type": "text", "text": b.Text.String()})
				}
			case "thinking":
				raw = append(raw, map[string]any{"type": "thinking", "thinking": b.Thinking.String(), "signature": b.Signature})
			case "redacted_thinking":
				raw = append(raw, map[string]any{"type": "redacted_thinking", "data": b.Data})
			case "tool_use":
				args := b.Input
				if s := strings.TrimSpace(b.JSON.String()); s != "" {
					parsed := map[string]any{}
					if json.Unmarshal([]byte(s), &parsed) == nil {
						args = parsed
					}
				}
				if args == nil {
					args = map[string]any{}
				}
				out.ToolCalls = append(out.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Args: args})
				raw = append(raw, map[string]any{"type": "tool_use", "id": b.ID, "name": b.Name, "input": args})
			}
		}
		if len(raw) > 0 {
			out.Blocks, _ = json.Marshal(raw)
		}
		return out
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if ctx.Err() != nil {
			return Chunk{Done: true, Err: ctx.Err()}
		}
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type  string         `json:"type"`
				ID    string         `json:"id"`
				Name  string         `json:"name"`
				Text  string         `json:"text"`
				Data  string         `json:"data"`
				Input map[string]any `json:"input"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "content_block_start":
			b := &anthropicBlock{Type: ev.ContentBlock.Type, ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name, Data: ev.ContentBlock.Data, Input: ev.ContentBlock.Input}
			if ev.ContentBlock.Text != "" {
				b.Text.WriteString(ev.ContentBlock.Text)
				select {
				case ch <- Chunk{Delta: ev.ContentBlock.Text}:
				case <-ctx.Done():
					return Chunk{Done: true, Err: ctx.Err()}
				}
			}
			blocks[ev.Index] = b
		case "content_block_delta":
			b := blocks[ev.Index]
			if b == nil {
				b = &anthropicBlock{Type: "text"}
				blocks[ev.Index] = b
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text == "" {
					continue
				}
				b.Text.WriteString(ev.Delta.Text)
				select {
				case ch <- Chunk{Delta: ev.Delta.Text}:
				case <-ctx.Done():
					return Chunk{Done: true, Err: ctx.Err()}
				}
			case "thinking_delta":
				b.Thinking.WriteString(ev.Delta.Thinking)
			case "signature_delta":
				b.Signature += ev.Delta.Signature
			case "input_json_delta":
				b.JSON.WriteString(ev.Delta.PartialJSON)
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
		case "message_stop":
			return finish()
		case "error":
			msg := ev.Error.Message
			if msg == "" {
				msg = ev.Error.Type
			}
			return Chunk{Done: true, Err: errString(Redact("Claude error: " + msg))}
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return Chunk{Done: true, Err: ctx.Err()}
		}
		return Chunk{Done: true, Err: errString(Redact(err.Error()))}
	}
	return finish()
}

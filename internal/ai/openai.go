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

// openAIProvider speaks the OpenAI chat-completions protocol with SSE
// streaming to any base URL: OpenAI, OpenRouter, Ollama, LM Studio or a
// self-hosted gateway. Port of mobocode's openai_compatible_provider.dart,
// plus native tool calling.
type openAIProvider struct{ cfg Config }

func (p *openAIProvider) Name() string        { return p.cfg.Kind }
func (p *openAIProvider) Configured() bool    { return p.cfg.Configured() }
func (p *openAIProvider) SupportsTools() bool { return true }

func (p *openAIProvider) Stream(ctx context.Context, req Request) <-chan Chunk {
	ch := make(chan Chunk, 16)
	go func() {
		defer close(ch)
		ch <- p.run(ctx, req, ch)
	}()
	return ch
}

// openAIMessage is the wire shape of one chat message.
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// buildOpenAIBody maps a Request onto the chat-completions JSON body.
func buildOpenAIBody(req Request, model string) map[string]any {
	msgs := make([]openAIMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		content := m.Content
		om := openAIMessage{Role: string(m.Role), Content: &content}
		switch m.Role {
		case RoleTool:
			om.ToolCallID = m.ToolCallID
		case RoleAssistant:
			for _, tc := range m.ToolCalls {
				var w openAIToolCall
				w.ID, w.Type = tc.ID, "function"
				w.Function.Name = tc.Name
				args, _ := json.Marshal(tc.Args)
				w.Function.Arguments = string(args)
				om.ToolCalls = append(om.ToolCalls, w)
			}
			if len(om.ToolCalls) > 0 && content == "" {
				om.Content = nil
			}
		}
		msgs = append(msgs, om)
	}
	body := map[string]any{"messages": msgs, "stream": true}
	if model != "" {
		body["model"] = model
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Schema,
				},
			})
		}
		body["tools"] = tools
	}
	return body
}

// run performs the request, forwards text deltas to ch and returns the
// final Done chunk.
func (p *openAIProvider) run(ctx context.Context, req Request, ch chan<- Chunk) Chunk {
	if !p.Configured() {
		return Chunk{Done: true, Err: ErrNotConfigured}
	}
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}
	payload, err := json.Marshal(buildOpenAIBody(req, model))
	if err != nil {
		return Chunk{Done: true, Err: err}
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, trimSlash(p.cfg.BaseURL)+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Chunk{Done: true, Err: err}
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	if p.cfg.APIKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
	if p.cfg.Kind == KindOpenRouter {
		hreq.Header.Set("X-Title", "termocode")
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
	return parseOpenAISSE(ctx, resp.Body, ch)
}

// parseOpenAISSE reads the SSE stream. Text deltas go to ch as they arrive;
// tool-call fragments (streamed per index) are accumulated and returned on
// the final chunk. Malformed lines are skipped.
func parseOpenAISSE(ctx context.Context, r io.Reader, ch chan<- Chunk) Chunk {
	type partial struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*partial{}
	finish := func() Chunk {
		out := Chunk{Done: true}
		idx := make([]int, 0, len(calls))
		for i := range calls {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		for _, i := range idx {
			c := calls[i]
			args := map[string]any{}
			if s := strings.TrimSpace(c.args.String()); s != "" {
				_ = json.Unmarshal([]byte(s), &args)
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: c.id, Name: c.name, Args: args})
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
		if data == "[DONE]" {
			return finish()
		}
		var ev struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Error any `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.Error != nil {
			b, _ := json.Marshal(ev.Error)
			return Chunk{Done: true, Err: errString(Redact(string(b)))}
		}
		if len(ev.Choices) == 0 {
			continue
		}
		d := ev.Choices[0].Delta
		if d.Content != "" {
			select {
			case ch <- Chunk{Delta: d.Content}:
			case <-ctx.Done():
				return Chunk{Done: true, Err: ctx.Err()}
			}
		}
		for _, tc := range d.ToolCalls {
			c := calls[tc.Index]
			if c == nil {
				c = &partial{}
				calls[tc.Index] = c
			}
			if tc.ID != "" {
				c.id = tc.ID
			}
			if tc.Function.Name != "" {
				c.name = tc.Function.Name
			}
			c.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return Chunk{Done: true, Err: ctx.Err()}
		}
		return Chunk{Done: true, Err: errString(Redact(err.Error()))}
	}
	// Stream ended without [DONE] — still a normal end.
	return finish()
}

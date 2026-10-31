package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Prompts (MCPX-13, PR-15/16): a server's ready-made prompts, got with
// their arguments, and argument completion (completion/complete).

// Prompt is one entry of prompts/list.
type Prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument is one argument of a prompt.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is one message of prompts/get.
type PromptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// PromptResult is the answer of prompts/get.
type PromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// ListPrompts returns every prompt, following nextCursor.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	return listPages[Prompt](ctx, c, "prompts/list", "prompts")
}

// GetPrompt gets one prompt filled with args.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (PromptResult, error) {
	if args == nil {
		args = map[string]string{}
	}
	raw, err := c.call(ctx, "prompts/get", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return PromptResult{}, fmt.Errorf("prompts/get: %w", err)
	}
	var res PromptResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return PromptResult{}, fmt.Errorf("prompts/get: bad answer: %w", err)
	}
	return res, nil
}

// maxCompletions caps the values of one completion answer (the spec's
// limit).
const maxCompletions = 100

// CompletePrompt asks for values of a prompt argument that start like
// value; done are the arguments already given.
func (c *Client) CompletePrompt(ctx context.Context, prompt, arg, value string, done map[string]string) ([]string, error) {
	params := map[string]any{
		"ref":      map[string]any{"type": "ref/prompt", "name": prompt},
		"argument": map[string]any{"name": arg, "value": value},
	}
	if len(done) > 0 {
		params["context"] = map[string]any{"arguments": done}
	}
	raw, err := c.call(ctx, "completion/complete", params)
	if err != nil {
		return nil, fmt.Errorf("completion/complete: %w", err)
	}
	var res struct {
		Completion struct {
			Values []string `json:"values"`
		} `json:"completion"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("completion/complete: bad answer: %w", err)
	}
	v := res.Completion.Values
	if len(v) > maxCompletions {
		v = v[:maxCompletions]
	}
	return v, nil
}

// Caps are a server's features from its last connection or the disk
// cache (it never starts the server).
func (m *Manager) Caps(name string) (ServerCaps, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if !ok {
		return ServerCaps{}, false
	}
	m.loadCacheLocked(st)
	return st.caps, true
}

// Prompts lists a server's prompts (none without the prompts feature).
func (m *Manager) Prompts(ctx context.Context, ask AskTrust, server string) ([]Prompt, error) {
	var out []Prompt
	err := m.withClient(ctx, ask, server, func(ctx context.Context, c *Client) error {
		if !c.Capabilities().Prompts {
			return nil
		}
		var err error
		out, err = c.ListPrompts(ctx)
		return err
	})
	return out, err
}

// GetPrompt gets one prompt of a server.
func (m *Manager) GetPrompt(ctx context.Context, ask AskTrust, server, name string, args map[string]string) (PromptResult, error) {
	var out PromptResult
	err := m.withClient(ctx, ask, server, func(ctx context.Context, c *Client) error {
		var err error
		out, err = c.GetPrompt(ctx, name, args)
		return err
	})
	return out, err
}

// CompletePrompt completes a prompt argument (no values when the server
// has no completions).
func (m *Manager) CompletePrompt(ctx context.Context, ask AskTrust, server, prompt, arg, value string, done map[string]string) ([]string, error) {
	var out []string
	err := m.withClient(ctx, ask, server, func(ctx context.Context, c *Client) error {
		if !c.Capabilities().Completions {
			return nil
		}
		var err error
		out, err = c.CompletePrompt(ctx, prompt, arg, value, done)
		return err
	})
	return out, err
}

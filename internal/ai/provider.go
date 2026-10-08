// Package ai is termocode's provider-agnostic AI layer: one streaming
// Provider interface with two wire implementations (OpenAI-compatible SSE
// and the Anthropic Messages API), plus config / secrets, chat storage,
// inline-completion helpers and a small tool-calling agent.
//
// Nothing here imports Bubble Tea or the app package — the TUI drives it
// through plain Go calls and channels, and every network path is testable
// with httptest.
package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Role is a chat message role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one chat turn.
//
// Tool calling: an assistant turn that called tools carries ToolCalls; the
// matching results are RoleTool messages with ToolCallID set. Providers
// that need to replay opaque content (Anthropic thinking blocks + their
// signatures) stash them in Blocks; other providers ignore the field.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	ToolName   string
	Blocks     json.RawMessage
}

// ToolSpec describes one callable tool for native tool calling. Schema is
// a JSON-Schema object ({"type":"object","properties":{…}}).
type ToolSpec struct {
	Name        string
	Description string
	Schema      map[string]any
}

// ToolCall is one tool invocation requested by the model.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// Request is a provider-neutral chat request.
type Request struct {
	Messages  []Message
	Model     string // "" → provider default
	MaxTokens int    // 0 → provider default
	// Temperature is only sent by providers that accept it (OpenAI-
	// compatible). Current Claude models reject sampling parameters.
	Temperature *float64
	Tools       []ToolSpec
}

// Chunk is one streamed piece of a reply. The final chunk has Done set; it
// carries Err on failure and, when the model called tools, ToolCalls (and,
// for Anthropic, the raw assistant Blocks to replay next turn).
type Chunk struct {
	Delta     string
	Done      bool
	Err       error
	ToolCalls []ToolCall
	Blocks    json.RawMessage
}

// Provider streams chat completions. Stream never blocks the caller: it
// returns a channel that yields deltas and is closed after exactly one
// Done chunk. Cancelling ctx aborts the request (the Done chunk then
// carries ctx.Err()).
type Provider interface {
	Name() string
	Configured() bool
	SupportsTools() bool
	Stream(ctx context.Context, req Request) <-chan Chunk
}

// Provider kinds accepted in config ("ai_provider").
const (
	KindNone        = ""
	KindOpenAI      = "openai"
	KindAnthropic   = "anthropic"
	KindClaudeOAuth = "claude-oauth"
	KindOpenRouter  = "openrouter"
	KindOllama      = "ollama"
	KindLMStudio    = "lmstudio"
	KindCompatible  = "openai-compatible"
)

// Kinds lists every provider kind in picker order.
var Kinds = []string{KindAnthropic, KindOpenAI, KindOpenRouter, KindOllama, KindLMStudio, KindCompatible, KindClaudeOAuth}

// KindLabel is the human label for a provider kind.
func KindLabel(kind string) string {
	switch kind {
	case KindOpenAI:
		return "OpenAI"
	case KindAnthropic:
		return "Anthropic (API key)"
	case KindClaudeOAuth:
		return "Claude (OAuth sign-in)"
	case KindOpenRouter:
		return "OpenRouter"
	case KindOllama:
		return "Ollama (local)"
	case KindLMStudio:
		return "LM Studio (local)"
	case KindCompatible:
		return "OpenAI-compatible (custom URL)"
	}
	return "None"
}

// DefaultBaseURL is the endpoint used when the user sets none.
func DefaultBaseURL(kind string) string {
	switch kind {
	case KindOpenAI:
		return "https://api.openai.com/v1"
	case KindAnthropic, KindClaudeOAuth:
		return "https://api.anthropic.com"
	case KindOpenRouter:
		return "https://openrouter.ai/api/v1"
	case KindOllama:
		return "http://localhost:11434/v1"
	case KindLMStudio:
		return "http://localhost:1234/v1"
	}
	return ""
}

// Claude model ids suggested in the model picker and used as defaults.
const (
	ModelClaudeOpus   = "claude-opus-5-5"
	ModelClaudeSonnet = "claude-sonnet-5-5"
	ModelClaudeHaiku  = "claude-haiku-4-5"
)

// SuggestedModels returns model ids offered by "AI: Pick Model".
func SuggestedModels(kind string) []string {
	switch kind {
	case KindAnthropic, KindClaudeOAuth:
		return []string{ModelClaudeOpus, ModelClaudeSonnet, ModelClaudeHaiku}
	case KindOpenAI:
		return []string{"gpt-4.1", "gpt-4.1-mini", "gpt-4o-mini"}
	case KindOpenRouter:
		return []string{"anthropic/" + ModelClaudeSonnet, "openai/gpt-4.1-mini", "qwen/qwen-2.5-coder-32b-instruct"}
	case KindOllama:
		return []string{"qwen2.5-coder:7b", "qwen2.5-coder:1.5b", "llama3.1:8b"}
	}
	return nil
}

// DefaultModel is the chat model used when the user picked none.
func DefaultModel(kind string) string {
	switch kind {
	case KindAnthropic, KindClaudeOAuth:
		return ModelClaudeOpus
	case KindOpenAI:
		return "gpt-4.1-mini"
	case KindOpenRouter:
		return "anthropic/" + ModelClaudeSonnet
	case KindOllama:
		return "qwen2.5-coder:7b"
	}
	return ""
}

// DefaultInlineModel is the fast model used for ghost-text completion.
func DefaultInlineModel(kind, chatModel string) string {
	switch kind {
	case KindAnthropic, KindClaudeOAuth:
		return ModelClaudeHaiku
	case KindOpenAI:
		return "gpt-4.1-mini"
	}
	return chatModel
}

// Config is a resolved provider configuration (see Resolve).
type Config struct {
	Kind        string
	BaseURL     string
	APIKey      string
	Model       string
	InlineModel string
	// Token returns a fresh OAuth access token (claude-oauth only).
	Token func(ctx context.Context) (string, error)
	// HTTPClient overrides the client (tests). nil → a sane default.
	HTTPClient *http.Client
}

// Configured reports whether cfg is complete enough to send requests.
func (c Config) Configured() bool {
	switch c.Kind {
	case KindNone:
		return false
	case KindOllama, KindLMStudio:
		return true
	case KindCompatible:
		return c.BaseURL != ""
	case KindClaudeOAuth:
		return c.Token != nil
	}
	return c.APIKey != ""
}

// NewProvider builds the provider for cfg. An unconfigured config yields a
// provider whose Stream fails immediately with ErrNotConfigured.
func NewProvider(cfg Config) Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL(cfg.Kind)
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel(cfg.Kind)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	switch cfg.Kind {
	case KindAnthropic, KindClaudeOAuth:
		return &anthropicProvider{cfg: cfg}
	case KindNone:
		return noneProvider{}
	}
	return &openAIProvider{cfg: cfg}
}

// ErrNotConfigured is returned by every AI entry point without a provider.
var ErrNotConfigured = errString("no AI provider configured")

type errString string

func (e errString) Error() string { return string(e) }

type noneProvider struct{}

func (noneProvider) Name() string        { return "none" }
func (noneProvider) Configured() bool    { return false }
func (noneProvider) SupportsTools() bool { return false }
func (noneProvider) Stream(context.Context, Request) <-chan Chunk {
	ch := make(chan Chunk, 1)
	ch <- Chunk{Done: true, Err: ErrNotConfigured}
	close(ch)
	return ch
}

// Collect drains a stream into its full text, final tool calls and error.
func Collect(ch <-chan Chunk) (string, Chunk) {
	var b strings.Builder
	var last Chunk
	for c := range ch {
		b.WriteString(c.Delta)
		if c.Done {
			last = c
		}
	}
	return b.String(), last
}

// trimSlash drops trailing slashes from a base URL.
func trimSlash(s string) string { return strings.TrimRight(s, "/") }

// httpError formats a non-2xx response body into a short error.
func httpError(code int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var parsed struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != nil {
		switch e := parsed.Error.(type) {
		case string:
			msg = e
		case map[string]any:
			if s, ok := e["message"].(string); ok {
				msg = s
			}
		}
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return errString(Redact("HTTP " + itoa(code) + ": " + msg))
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

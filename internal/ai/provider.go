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
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	// Images are sent with a user message, or with a tool result
	// (AttachImages), to providers with image input (SupportsImages).
	Images []Image
}

// Image is one picture attached to a user message.
type Image struct {
	MediaType string // image/png, image/jpeg, image/gif, image/webp
	Data      []byte
}

// SupportsImages reports whether p sends images to the model: Anthropic
// and the hosted OpenAI-style APIs (OpenAI, OpenRouter) do; local and
// custom servers are not assumed to (their model may be text only).
func SupportsImages(p Provider) bool {
	if v, ok := p.(interface{ SupportsImages() bool }); ok {
		return v.SupportsImages()
	}
	switch p := p.(type) {
	case *anthropicProvider:
		return true
	case *openAIProvider:
		return p.cfg.Kind == KindOpenAI || p.cfg.Kind == KindOpenRouter
	}
	return false
}

// ToolSpec describes one callable tool for native tool calling. Schema is
// a JSON-Schema object ({"type":"object","properties":{…}}).
type ToolSpec struct {
	Name        string
	Description string
	Schema      map[string]any
	// wire keeps the spec's JSON per provider once made: every request
	// of a turn sends the same specs (Agent.Run makes them once per
	// turn). Copies share it; nil (a spec literal) encodes each time.
	wire *toolWire
}

// toolWire is a ToolSpec's encoded JSON, made once per provider format.
type toolWire struct {
	// spec is what the JSON is made from (a deep copy of the schema, see
	// wireFor).
	spec              ToolSpec
	openAI, anthropic onceJSON
}

type onceJSON struct {
	once sync.Once
	b    json.RawMessage
	err  error
}

func (o *onceJSON) get(v func() any) (json.RawMessage, error) {
	o.once.Do(func() { o.b, o.err = json.Marshal(v()) })
	return o.b, o.err
}

// encodedTools is the "tools" list of a body: each spec's JSON (made once
// per turn). A spec that can not be encoded makes a list json.Marshal
// fails on, as before.
func encodedTools(specs []ToolSpec, enc func(ToolSpec) (json.RawMessage, error)) any {
	out := make([]json.RawMessage, 0, len(specs))
	for _, t := range specs {
		b, err := enc(t)
		if err != nil {
			return []any{unencodable{err}}
		}
		out = append(out, b)
	}
	return out
}

// marshalBody is json.Marshal(body), but an encoded "tools" list is put
// in as it is, without encoding json checking each tool again. "tools"
// sorts after every other key of a body, so it is the last one, where
// json.Marshal puts it too.
func marshalBody(body map[string]any) ([]byte, error) {
	tools, ok := body["tools"].([]json.RawMessage)
	if !ok {
		return json.Marshal(body)
	}
	for k := range body {
		if k > "tools" {
			return json.Marshal(body)
		}
	}
	rest := make(map[string]any, len(body)-1)
	for k, v := range body {
		if k != "tools" {
			rest[k] = v
		}
	}
	b, err := json.Marshal(rest)
	if err != nil {
		return nil, err
	}
	n := 0
	for _, t := range tools {
		n += len(t) + 1
	}
	out := make([]byte, 0, len(b)+n+16)
	out = append(out, b[:len(b)-1]...) // without the closing brace
	if len(rest) > 0 {
		out = append(out, ',')
	}
	out = append(out, `"tools":[`...)
	for i, t := range tools {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, t...)
	}
	return append(out, "]}"...), nil
}

// unencodable fails json.Marshal with err (a spec that could not be
// encoded fails the request as before).
type unencodable struct{ err error }

func (u unencodable) MarshalJSON() ([]byte, error) { return nil, u.err }

// openAIJSON is the spec as an OpenAI "tools" entry.
func (t ToolSpec) openAIJSON() (json.RawMessage, error) {
	if w := t.wire; w != nil {
		return w.openAI.get(func() any { return openAIToolEntry(w.spec) })
	}
	return json.Marshal(openAIToolEntry(t))
}

func openAIToolEntry(t ToolSpec) any {
	return map[string]any{"type": "function", "function": map[string]any{
		"name": t.Name, "description": t.Description, "parameters": t.Schema}}
}

// anthropicJSON is the spec as an Anthropic "tools" entry.
func (t ToolSpec) anthropicJSON() (json.RawMessage, error) {
	if w := t.wire; w != nil {
		return w.anthropic.get(func() any { return anthropicToolEntry(w.spec) })
	}
	return json.Marshal(anthropicToolEntry(t))
}

func anthropicToolEntry(t ToolSpec) any {
	return map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Schema}
}

// ToolCall is one tool invocation requested by the model.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
	// BadArgs is set when the provider sent arguments that are not valid
	// JSON (a cut stream, a model slip). The agent does not run the call;
	// it tells the model instead.
	BadArgs bool `json:"-"`
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
	// Reasoning is this request's reasoning level (reasoning.go; "":
	// the config's).
	Reasoning string
}

// Chunk is one streamed piece of a reply. The final chunk has Done set; it
// carries Err on failure and, when the model called tools, ToolCalls (and,
// for Anthropic, the raw assistant Blocks to replay next turn). Usage is
// set on the Done chunk when the server reported token counts (nil when
// it did not).
type Chunk struct {
	Delta     string
	Done      bool
	Err       error
	ToolCalls []ToolCall
	Blocks    json.RawMessage
	Usage     *Usage
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
	// PromptCache is the Anthropic prompt cache life (ai_prompt_cache):
	// "" 5 minutes, PromptCache1h or PromptCacheOff.
	PromptCache string
	// Reasoning is the slot's reasoning level (ai_reasoning or
	// ai_models.<slot>.reasoning; "": the model's default).
	Reasoning string
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
	client := *cfg.HTTPClient
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = firstByteTransport{base: transport, limit: firstByteLimit(cfg.Kind, cfg.BaseURL)}
	cfg.HTTPClient = &client

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
	msg = ansi.Truncate(msg, 301, "…")
	return statusError{code: code, msg: Redact("HTTP " + itoa(code) + ": " + msg)}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// statusError is an HTTP error answer of a provider.
type statusError struct {
	code int
	msg  string
}

func (e statusError) Error() string { return e.msg }

// Retryable reports whether err is a busy server (HTTP 429, 500, 502,
// 503, 504 or Anthropic's 529 "overloaded") worth trying again.
func Retryable(err error) bool {
	var se statusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.code {
	case 429, 500, 502, 503, 504, 529:
		return true
	}
	return false
}

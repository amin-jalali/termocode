package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// SlackSendTool is the send tool of a message via an MCP server when the
// step names none (the Slack MCP server's).
const SlackSendTool = "slack_send_message"

// Message is one message to deliver: Via is the channel kind (os, web,
// ${secret:NAME} or an MCP server's name); Channel and Tool are for a
// server.
type Message struct {
	Via, Channel, Tool, Text string
}

// RedactOutside hides secrets in text that leaves this machine (the same
// redaction as the run's ledger and audit file).
func RedactOutside(text string) string {
	if text == "" {
		return text
	}
	s, _ := services.Redact(ai.Redact(text))
	return s
}

// Messenger delivers a message write's text and a notify target's
// summary (plan §3.2, §3.4): "os" is a desktop note, "web" an attention
// event for the browsers, "${secret:NAME}" a webhook whose URL is that
// secret (https only), posted as JSON {"text": …}, and an MCP server's
// name calls its send tool (Slack: slack_send_message) on a channel.
// Text for a webhook or a server is redacted first. The URL never shows
// in an error.
type Messenger struct {
	OS, Web func(ctx context.Context, text string) error
	Secrets source.SecretGetter
	Post    func(ctx context.Context, url string, body []byte) error
	// Tools calls MCP tools for a message via a server (nil: none
	// connected).
	Tools source.ToolCaller
}

// Send delivers text via one channel (no server channel).
func (ms Messenger) Send(ctx context.Context, via, text string) error {
	return ms.Deliver(ctx, Message{Via: via, Text: text})
}

// Deliver sends one message.
func (ms Messenger) Deliver(ctx context.Context, m Message) error {
	via, text := strings.TrimSpace(m.Via), m.Text
	switch via {
	case "os", "web":
		f := ms.OS
		if via == "web" {
			f = ms.Web
		}
		if f == nil {
			return fmt.Errorf("message via %s is not available here", via)
		}
		return f(ctx, text)
	}
	text = RedactOutside(text)
	if !strings.HasPrefix(via, "${") {
		return ms.server(ctx, via, m, text)
	}
	name, ok := strings.CutPrefix(via, "${secret:")
	name, closed := strings.CutSuffix(name, "}")
	if !ok || !closed || secrets.ValidName(name) != nil {
		return errors.New("message via a webhook: use ${secret:NAME}, where the secret holds the https URL")
	}
	if ms.Secrets == nil {
		return fmt.Errorf("message via ${secret:%s}: no secret store", name)
	}
	url, ok, err := ms.Secrets.Get(name)
	switch {
	case err != nil:
		return fmt.Errorf("message via ${secret:%s}: the secret could not be read", name)
	case !ok || strings.TrimSpace(url) == "":
		return fmt.Errorf("message via ${secret:%s}: the secret is not set", name)
	case !strings.HasPrefix(strings.TrimSpace(url), "https://"):
		return fmt.Errorf("message via ${secret:%s}: the webhook must be an https URL", name)
	case ms.Post == nil:
		return errors.New("message via a webhook is not available here")
	}
	body, _ := json.Marshal(map[string]string{"text": text})
	if err := ms.Post(ctx, strings.TrimSpace(url), body); err != nil {
		return fmt.Errorf("message via ${secret:%s}: %s", name, strings.ReplaceAll(err.Error(), strings.TrimSpace(url), "<webhook>"))
	}
	return nil
}

// server sends text through MCP server via's send tool.
func (ms Messenger) server(ctx context.Context, via string, m Message, text string) error {
	channel := strings.TrimSpace(m.Channel)
	if channel == "" {
		return fmt.Errorf(`message via the MCP server %q: add "channel" (where the message goes)`, via)
	}
	if ms.Tools == nil {
		return fmt.Errorf("message via %s: %w", via, source.ErrNotConnected)
	}
	tool := strings.TrimSpace(m.Tool)
	if tool == "" {
		tool = SlackSendTool
	}
	args := map[string]any{"channel_id": channel, "message": text}
	if _, _, err := ms.Tools.CallTool(source.WithWriteAllowed(ctx), via, tool, args); err != nil {
		return fmt.Errorf("message via %s · %s: %s", via, tool, RedactOutside(err.Error()))
	}
	return nil
}

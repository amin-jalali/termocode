package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// SlackSendTool is the send tool of a message via an MCP server when the
// step names none and the server's tool list has no send-message tool
// (the reference Slack MCP server's, with channel_id and text).
const SlackSendTool = "slack_post_message"

// knownSendTools are send-message tools picked before any other match,
// best first (names compared without case, "_", "-" and ".").
var knownSendTools = []string{"slackpostmessage", "slacksendmessage", "chatpostmessage"}

// sendTool is a server's send tool and the names of its channel and
// text arguments.
type sendTool struct {
	name, channel, text string
	found               bool     // found in the server's tool list
	have                []string // the server's tools (for an error)
}

// SendCache keeps each server's send tool for one run (the zero value
// is ready; safe for concurrent use).
type SendCache struct {
	mu sync.Mutex
	m  map[string]sendTool
}

func (c *SendCache) get(server string) (sendTool, bool) {
	if c == nil {
		return sendTool{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.m[server]
	return t, ok
}

func (c *SendCache) put(server string, t sendTool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]sendTool{}
	}
	c.m[server] = t
}

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
// name calls its send tool on a channel: the step's "tool", or the
// send-message tool found in the server's tool list.
// Text for a webhook or a server is redacted first. The URL never shows
// in an error.
type Messenger struct {
	OS, Web func(ctx context.Context, text string) error
	Secrets source.SecretGetter
	Post    func(ctx context.Context, url string, body []byte) error
	// Tools calls MCP tools for a message via a server (nil: none
	// connected). When it is also a source.ToolLister, the server's send
	// tool is found in its tool list.
	Tools source.ToolCaller
	// Cache keeps each server's send tool for the run (nil: list every
	// time).
	Cache *SendCache
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
	st := ms.sendTool(ctx, via, strings.TrimSpace(m.Tool))
	args := map[string]any{st.channel: channel, st.text: text}
	if _, _, err := ms.Tools.CallTool(source.WithWriteAllowed(ctx), via, st.name, args); err != nil {
		msg := fmt.Sprintf("message via %s · %s: %s", via, st.name, RedactOutside(err.Error()))
		if m.Tool == "" && !st.found {
			msg += "; no send-message tool found"
			if len(st.have) > 0 {
				msg += "; the server has: " + strings.Join(st.have, ", ")
			}
			msg += `; set "tool" on the step`
		}
		return errors.New(msg)
	}
	return nil
}

// sendTool is the tool a message via server calls: own (the step's
// "tool") when set, else the send-message tool in the server's list,
// else SlackSendTool. Only a listed result is cached.
func (ms Messenger) sendTool(ctx context.Context, server, own string) sendTool {
	if own == "" {
		if t, ok := ms.Cache.get(server); ok {
			return t
		}
	}
	lister, _ := ms.Tools.(source.ToolLister)
	var list []source.ToolInfo
	listed := false
	if lister != nil {
		if l, err := lister.ListTools(ctx, server); err == nil {
			list, listed = l, true
		}
	}
	if own != "" {
		// As written; its schema (when listed) names the arguments.
		t := sendTool{name: own, channel: "channel_id", text: "message", found: true}
		for _, ti := range list {
			if ti.Name == own {
				t.channel, t.text = argNames(ti.InputSchema, t.channel, t.text)
			}
		}
		return t
	}
	t := pickSendTool(list)
	if listed {
		ms.Cache.put(server, t)
	}
	return t
}

// pickSendTool finds the send-message tool in list: a known name first,
// then the first name with post or send and message whose schema has a
// channel and a text argument. None: SlackSendTool with channel_id and
// text.
func pickSendTool(list []source.ToolInfo) sendTool {
	const maxNames = 30
	t := sendTool{name: SlackSendTool, channel: "channel_id", text: "text"}
	for i, ti := range list {
		if i == maxNames {
			t.have = append(t.have, fmt.Sprintf("… (%d more)", len(list)-maxNames))
			break
		}
		t.have = append(t.have, ti.Name)
	}
	best, rank := -1, len(knownSendTools)+1
	for i, ti := range list {
		n := normToolName(ti.Name)
		if !strings.Contains(n, "message") || !(strings.Contains(n, "post") || strings.Contains(n, "send")) {
			continue
		}
		if _, _, ok := schemaArgs(ti.InputSchema); !ok {
			continue
		}
		r := len(knownSendTools)
		for k, known := range knownSendTools {
			if n == known {
				r = k
			}
		}
		if r < rank {
			best, rank = i, r
		}
	}
	if best < 0 {
		return t
	}
	t.name, t.found = list[best].Name, true
	t.channel, t.text = argNames(list[best].InputSchema, "channel_id", "text")
	return t
}

func normToolName(name string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(name))
}

// schemaArgs finds the channel (channel_id or channel) and text (text or
// message) arguments in a tool's input schema. A schema without
// properties says nothing: ok, with no names.
func schemaArgs(schema map[string]any) (channel, text string, ok bool) {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return "", "", true
	}
	for _, n := range []string{"channel_id", "channel"} {
		if _, has := props[n]; has {
			channel = n
			break
		}
	}
	for _, n := range []string{"text", "message"} {
		if _, has := props[n]; has {
			text = n
			break
		}
	}
	return channel, text, channel != "" && text != ""
}

// argNames is schemaArgs with defaults for what the schema does not name.
func argNames(schema map[string]any, channel, text string) (string, string) {
	c, x, _ := schemaArgs(schema)
	if c != "" {
		channel = c
	}
	if x != "" {
		text = x
	}
	return channel, text
}

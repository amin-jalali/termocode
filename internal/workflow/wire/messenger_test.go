package wire

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow/source"
)

type fakeSecrets map[string]string

func (f fakeSecrets) Get(name string) (string, bool, error) {
	v, ok := f[name]
	return v, ok, nil
}

// Spec (plan §3.4, WFX-18): a message goes to the OS ("os"), to the
// browsers ("web") or to a webhook whose URL is a secret
// ("${secret:NAME}", https only, posted as JSON {"text": …}). An error
// never shows the webhook's URL. Other channels are refused clearly.
func TestMessengerDelivers(t *testing.T) {
	var osGot, webGot []string
	var posts []string
	hook := "https://hooks.example.test/" + "T0/" + "abc"
	ms := Messenger{
		OS:      func(_ context.Context, text string) error { osGot = append(osGot, text); return nil },
		Web:     func(_ context.Context, text string) error { webGot = append(webGot, text); return nil },
		Secrets: fakeSecrets{"HOOK": hook, "PLAIN": "http://hooks.example.test/x"},
		Post: func(_ context.Context, url string, body []byte) error {
			posts = append(posts, url+" "+string(body))
			return nil
		},
	}
	ctx := context.Background()
	for _, via := range []string{"os", "web", "${secret:HOOK}"} {
		if err := ms.Send(ctx, via, `wf: done · 2 items "ok"`); err != nil {
			t.Fatalf("%s: %v", via, err)
		}
	}
	if len(osGot) != 1 || len(webGot) != 1 || osGot[0] != `wf: done · 2 items "ok"` {
		t.Fatalf("os %q web %q", osGot, webGot)
	}
	want, _ := json.Marshal(map[string]string{"text": `wf: done · 2 items "ok"`})
	if len(posts) != 1 || posts[0] != hook+" "+string(want) {
		t.Fatalf("posts %q", posts)
	}
	for via, why := range map[string]string{
		"${secret:PLAIN}":   "https",
		"${secret:MISSING}": "not set",
		"${env:HOOK}":       "${secret:NAME}",
		"slack":             "MCP",
	} {
		err := ms.Send(ctx, via, "x")
		if err == nil || !strings.Contains(err.Error(), why) || strings.Contains(err.Error(), "hooks.example.test") {
			t.Errorf("%s: %v (want it to say %q, never the URL)", via, err, why)
		}
	}
	if len(posts) != 1 {
		t.Fatalf("a refused channel posted: %q", posts)
	}
	if err := (Messenger{}).Send(ctx, "os", "x"); err == nil {
		t.Error("no OS channel, no error")
	}
}

type msgTools struct {
	calls []sentCall
	write []bool
}

func (m *msgTools) CallTool(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, bool, error) {
	m.calls = append(m.calls, sentCall{server, tool, args})
	m.write = append(m.write, source.WriteAllowed(ctx))
	return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), false, nil
}

// Spec (plan §3.3, WFX-18): "via" an MCP server (Slack) calls that
// server's send tool: slack_send_message with channel_id and message by
// default, or the step's "tool". The call is a write. A server message
// needs a channel; a server that is not connected says so.
func TestMessengerViaMCPServer(t *testing.T) {
	tools := &msgTools{}
	ms := Messenger{Tools: tools}
	ctx := context.Background()
	if err := ms.Deliver(ctx, Message{Via: "slack", Channel: "C0123", Text: "wf: done"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.Deliver(ctx, Message{Via: "chat", Channel: "#team", Tool: "post", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if len(tools.calls) != 2 {
		t.Fatalf("calls %+v", tools.calls)
	}
	c := tools.calls[0]
	if c.server != "slack" || c.tool != "slack_send_message" || c.args["channel_id"] != "C0123" || c.args["message"] != "wf: done" {
		t.Errorf("slack call %+v", c)
	}
	if c := tools.calls[1]; c.server != "chat" || c.tool != "post" || c.args["channel_id"] != "#team" {
		t.Errorf("own tool call %+v", c)
	}
	if !tools.write[0] || !tools.write[1] {
		t.Error("a message call is not marked as a write")
	}
	if err := ms.Deliver(ctx, Message{Via: "slack", Text: "x"}); err == nil || !strings.Contains(err.Error(), "channel") {
		t.Errorf("no channel: %v", err)
	}
	err := (Messenger{}).Deliver(ctx, Message{Via: "slack", Channel: "C1", Text: "x"})
	if !errors.Is(err, source.ErrNotConnected) || !strings.Contains(err.Error(), "slack") {
		t.Errorf("no MCP: %v", err)
	}
	if len(tools.calls) != 2 {
		t.Fatalf("a refused message called a tool: %+v", tools.calls)
	}
}

// Spec (WFX-18, plan §6.2): what goes to an outside system (a webhook, an
// MCP server) passes the redaction first; the OS and the browsers are
// this machine and get the text as it is.
func TestMessengerRedactsOutside(t *testing.T) {
	key := "sk-ant-" + "api03-" + "abcdefghijklmnopqrstuvwxyz0123456789"
	text := "agent said: key " + key
	var posted []string
	var local []string
	tools := &msgTools{}
	ms := Messenger{Tools: tools, Secrets: fakeSecrets{"HOOK": "https://hooks.example.test/x"},
		Post: func(_ context.Context, _ string, body []byte) error {
			posted = append(posted, string(body))
			return nil
		},
		OS: func(_ context.Context, t string) error { local = append(local, t); return nil }}
	ctx := context.Background()
	for _, m := range []Message{{Via: "${secret:HOOK}", Text: text}, {Via: "slack", Channel: "C1", Text: text}, {Via: "os", Text: text}} {
		if err := ms.Deliver(ctx, m); err != nil {
			t.Fatalf("%s: %v", m.Via, err)
		}
	}
	if len(posted) != 1 || strings.Contains(posted[0], key) || !strings.Contains(posted[0], "agent said") {
		t.Errorf("webhook body %q", posted)
	}
	if got, _ := tools.calls[0].args["message"].(string); strings.Contains(got, key) || !strings.Contains(got, "agent said") {
		t.Errorf("slack message %q", got)
	}
	if len(local) != 1 || local[0] != text {
		t.Errorf("os %q", local)
	}
	if RedactOutside(text) == text {
		t.Error("RedactOutside kept the key")
	}
}

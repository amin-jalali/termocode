package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// server's send tool. A server that cannot list its tools gets the
// reference Slack server's slack_post_message with channel_id and text;
// the step's own "tool" is called as written (channel_id and message).
// The call is a write. A server message
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
	if c.server != "slack" || c.tool != "slack_post_message" || c.args["channel_id"] != "C0123" || c.args["text"] != "wf: done" {
		t.Errorf("slack call %+v", c)
	}
	if c := tools.calls[1]; c.server != "chat" || c.tool != "post" || c.args["channel_id"] != "#team" || c.args["message"] != "hi" {
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
	if got, _ := tools.calls[0].args["text"].(string); strings.Contains(got, key) || !strings.Contains(got, "agent said") {
		t.Errorf("slack message %q", got)
	}
	if len(local) != 1 || local[0] != text {
		t.Errorf("os %q", local)
	}
	if RedactOutside(text) == text {
		t.Error("RedactOutside kept the key")
	}
}

// listTools is a fake MCP server set: each server lists its tools
// (tools/list) and a call to a tool it does not have fails.
type listTools struct {
	tools map[string][]source.ToolInfo
	lists map[string]int
	fail  error // ListTools fails
	calls []sentCall
	write []bool
}

func (l *listTools) ListTools(_ context.Context, server string) ([]source.ToolInfo, error) {
	if l.lists == nil {
		l.lists = map[string]int{}
	}
	l.lists[server]++
	if l.fail != nil {
		return nil, l.fail
	}
	ts, ok := l.tools[server]
	if !ok {
		return nil, fmt.Errorf("%s: %w", server, source.ErrNotConnected)
	}
	return ts, nil
}

func (l *listTools) CallTool(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, bool, error) {
	l.calls = append(l.calls, sentCall{server, tool, args})
	l.write = append(l.write, source.WriteAllowed(ctx))
	for _, t := range l.tools[server] {
		if t.Name == tool {
			return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), false, nil
		}
	}
	return nil, false, fmt.Errorf("%s has no tool %q", server, tool)
}

func schemaOf(props ...string) map[string]any {
	p := map[string]any{}
	for _, n := range props {
		p[n] = map[string]any{"type": "string"}
	}
	return map[string]any{"type": "object", "properties": p, "required": props}
}

// Spec (WFX-18 follow-up): with no "tool" on the step, the messenger
// asks the server for its tools (tools/list) and picks the send-message
// tool: a name with post or send and message, a known Slack name first.
// Its input schema names the channel (channel_id or channel) and the
// text (text or message) arguments. The call is a write.
func TestMessengerDetectsSendTool(t *testing.T) {
	for name, tc := range map[string]struct {
		tools               []source.ToolInfo
		tool, channel, text string
	}{
		"reference server": {[]source.ToolInfo{
			{Name: "slack_list_channels", InputSchema: schemaOf("limit")},
			{Name: "slack_post_message", InputSchema: schemaOf("channel_id", "text")},
			{Name: "slack_reply_to_thread", InputSchema: schemaOf("channel_id", "thread_ts", "text")},
		}, "slack_post_message", "channel_id", "text"},
		"send message, message arg": {[]source.ToolInfo{
			{Name: "slack_get_users"},
			{Name: "slack_send_message", InputSchema: schemaOf("channel_id", "message")},
		}, "slack_send_message", "channel_id", "message"},
		"web api name, channel arg": {[]source.ToolInfo{
			{Name: "conversations_history", InputSchema: schemaOf("channel")},
			{Name: "chat_postMessage", InputSchema: schemaOf("channel", "text")},
		}, "chat_postMessage", "channel", "text"},
		"known name first": {[]source.ToolInfo{
			{Name: "team_send_message", InputSchema: schemaOf("channel", "message")},
			{Name: "slack_post_message", InputSchema: schemaOf("channel_id", "text")},
		}, "slack_post_message", "channel_id", "text"},
		"other server's name": {[]source.ToolInfo{
			{Name: "search_messages", InputSchema: schemaOf("query")},
			{Name: "post-message", InputSchema: schemaOf("channel", "message")},
		}, "post-message", "channel", "message"},
	} {
		t.Run(name, func(t *testing.T) {
			tools := &listTools{tools: map[string][]source.ToolInfo{"slack": tc.tools}}
			ms := Messenger{Tools: tools, Cache: &SendCache{}}
			if err := ms.Deliver(context.Background(), Message{Via: "slack", Channel: "C0123", Text: "wf: done"}); err != nil {
				t.Fatal(err)
			}
			if len(tools.calls) != 1 {
				t.Fatalf("calls %+v", tools.calls)
			}
			c := tools.calls[0]
			if c.tool != tc.tool || c.args[tc.channel] != "C0123" || c.args[tc.text] != "wf: done" || len(c.args) != 2 {
				t.Errorf("call %+v, want %s(%s, %s)", c, tc.tool, tc.channel, tc.text)
			}
			if !tools.write[0] {
				t.Error("a message call is not marked as a write")
			}
		})
	}
}

// Spec: the found tool is kept per server for the run: two messages to
// the same server list its tools once; another server is listed on its
// own.
func TestMessengerCachesSendToolPerServer(t *testing.T) {
	tools := &listTools{tools: map[string][]source.ToolInfo{
		"slack": {{Name: "slack_post_message", InputSchema: schemaOf("channel_id", "text")}},
		"chat":  {{Name: "chat_postMessage", InputSchema: schemaOf("channel", "text")}},
	}}
	ms := Messenger{Tools: tools, Cache: &SendCache{}}
	ctx := context.Background()
	for _, m := range []Message{
		{Via: "slack", Channel: "C1", Text: "a"},
		{Via: "slack", Channel: "C2", Text: "b"},
		{Via: "chat", Channel: "#x", Text: "c"},
	} {
		if err := ms.Deliver(ctx, m); err != nil {
			t.Fatalf("%s: %v", m.Via, err)
		}
	}
	if tools.lists["slack"] != 1 || tools.lists["chat"] != 1 {
		t.Errorf("lists %v, want each server once", tools.lists)
	}
	if len(tools.calls) != 3 || tools.calls[1].tool != "slack_post_message" || tools.calls[2].tool != "chat_postMessage" {
		t.Errorf("calls %+v", tools.calls)
	}
}

// Spec: a server with no send-message tool gets the fallback
// slack_post_message(channel_id, text); when that fails, the error says
// which tools the server has, so the user can set "tool". A list that
// fails also falls back.
func TestMessengerSendToolFallback(t *testing.T) {
	tools := &listTools{tools: map[string][]source.ToolInfo{"slack": {
		{Name: "list_channels", InputSchema: schemaOf("limit")},
		{Name: "get_history", InputSchema: schemaOf("channel")},
	}}}
	ms := Messenger{Tools: tools, Cache: &SendCache{}}
	ctx := context.Background()
	err := ms.Deliver(ctx, Message{Via: "slack", Channel: "C1", Text: "x"})
	if err == nil {
		t.Fatal("a server without a send tool sent")
	}
	for _, want := range []string{"slack_post_message", "list_channels", "get_history", `"tool"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	if len(tools.calls) != 1 || tools.calls[0].tool != "slack_post_message" || tools.calls[0].args["channel_id"] != "C1" || tools.calls[0].args["text"] != "x" {
		t.Errorf("fallback call %+v", tools.calls)
	}

	broken := &listTools{fail: errors.New("tools/list timed out")}
	err = Messenger{Tools: broken}.Deliver(ctx, Message{Via: "slack", Channel: "C1", Text: "x"})
	if len(broken.calls) != 1 || broken.calls[0].tool != "slack_post_message" {
		t.Errorf("list failed, call %+v", broken.calls)
	}
	if err == nil || !strings.Contains(err.Error(), "slack_post_message") {
		t.Errorf("list failed: %v", err)
	}
}

// Spec: the step's own "tool" is used as written, never a detected one;
// its schema (when the server lists it) names the arguments.
func TestMessengerOwnToolWins(t *testing.T) {
	tools := &listTools{tools: map[string][]source.ToolInfo{"slack": {
		{Name: "slack_post_message", InputSchema: schemaOf("channel_id", "text")},
		{Name: "notify_team", InputSchema: schemaOf("channel", "text")},
	}}}
	ms := Messenger{Tools: tools, Cache: &SendCache{}}
	if err := ms.Deliver(context.Background(), Message{Via: "slack", Channel: "#dev", Tool: "notify_team", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("calls %+v", tools.calls)
	}
	if c := tools.calls[0]; c.tool != "notify_team" || c.args["channel"] != "#dev" || c.args["text"] != "hi" {
		t.Errorf("own tool call %+v", c)
	}
}

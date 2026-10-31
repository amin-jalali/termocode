package app

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mcp"
)

func elicitForm() mcp.ElicitRequest {
	return mcp.ElicitRequest{Server: "fake", Mode: mcp.ElicitForm, Message: "Who are you?", Fields: []mcp.ElicitField{
		{Name: "name", Title: "Your name", Type: "string", Required: true},
		{Name: "age", Type: "integer"},
		{Name: "color", Type: "string", Enum: []string{"red", "blue"}},
		{Name: "api_key", Type: "string", Default: "do-not-show"},
	}}
}

func openElicit(t *testing.T, m *Model, r mcp.ElicitRequest) chan mcp.ElicitResult {
	t.Helper()
	reply := make(chan mcp.ElicitResult, 1)
	m.onAIMCPElicit(aiMCPElicitMsg{gen: m.aiS().chatGen, req: r, reply: reply, ch: make(chan tea.Msg, 1)})
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	return reply
}

// MCPX-14 form mode: a Deny-first card names the fields and warns about
// a key; each field is asked (checked, the key masked and not filled);
// a last Cancel-first card shows what goes out; Send answers accept.
func TestAIMCPElicitFormFlow(t *testing.T) {
	m := covMCPModel(t)
	reply := openElicit(t, m, elicitForm())
	view := ansi.Strip(m.confirm.View())
	if m.confirmKind != confirmKindMCPElicit || m.confirm.SafeID() != "deny" || !strings.Contains(view, "Your name (text, required)") || !strings.Contains(view, "password or key") {
		t.Fatalf("card %d / %q:\n%s", m.confirmKind, m.confirm.SafeID(), view)
	}
	m.answerMCPElicit(confirmKindMCPElicit, "fill")
	for _, step := range []struct{ in, wantErr string }{{"", "required"}, {"Bob", ""}, {"x", "whole number"}, {"7", ""}, {"green", "one of"}, {"", ""}} {
		if !m.promptOpen || m.promptKind != promptKindMCPElicit {
			t.Fatalf("no field prompt before %q", step.in)
		}
		m.promptOpen = false
		m.onMCPElicitField(step.in)
		if got := m.prompt.Error(); (step.wantErr == "") != (got == "") || !strings.Contains(got, step.wantErr) {
			t.Fatalf("value %q: error %q, want %q", step.in, got, step.wantErr)
		}
	}
	if !m.prompt.Masked() || m.prompt.Value() != "" {
		t.Fatalf("the key field is not masked or was filled: %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.onMCPElicitField("sk-" + "test-key")
	view = ansi.Strip(m.confirm.View())
	if m.confirmKind != confirmKindMCPElicitSend || m.confirm.SafeID() != "cancel" || !strings.Contains(view, "Your name: Bob") || strings.Contains(view, "test-key") || !strings.Contains(view, "••••••") {
		t.Fatalf("review card:\n%s", view)
	}
	m.answerMCPElicit(confirmKindMCPElicitSend, "send")
	r := <-reply
	if r.Action != mcp.ElicitAccept || r.Content["name"] != "Bob" || r.Content["age"] != 7 || r.Content["api_key"] != "sk-"+"test-key" {
		t.Fatalf("answer %+v", r)
	}
	if _, has := r.Content["color"]; has {
		t.Error("a skipped field was sent")
	}
}

// Deny declines; Esc on the card or a field prompt cancels; a question
// of an old turn is cancelled at once.
func TestAIMCPElicitDenyEscStale(t *testing.T) {
	m := covMCPModel(t)
	reply := openElicit(t, m, elicitForm())
	m.answerMCPElicit(confirmKindMCPElicit, "deny")
	if r := <-reply; r.Action != mcp.ElicitDecline {
		t.Fatalf("deny = %+v", r)
	}
	reply = openElicit(t, m, elicitForm())
	m.confirmKind = confirmKindMCPElicit
	m.onAIConfirmClosed()
	if r := <-reply; r.Action != mcp.ElicitCancel {
		t.Fatalf("esc = %+v", r)
	}
	reply = openElicit(t, m, elicitForm())
	m.answerMCPElicit(confirmKindMCPElicit, "fill")
	m.aiMCPElicitPromptClosed()
	if r := <-reply; r.Action != mcp.ElicitCancel || m.aiS().mcpx.elicit != nil {
		t.Fatalf("esc on a field = %+v", r)
	}
	stale := make(chan mcp.ElicitResult, 1)
	m.confirmOpen = false
	m.onAIMCPElicit(aiMCPElicitMsg{gen: m.aiS().chatGen + 5, req: elicitForm(), reply: stale, ch: make(chan tea.Msg, 1)})
	if r := <-stale; r.Action != mcp.ElicitCancel || m.confirmOpen {
		t.Fatalf("stale = %+v", r)
	}
}

// URL mode: the card shows the whole URL and its site (Deny first);
// Open in browser opens it and answers accept.
func TestAIMCPElicitURL(t *testing.T) {
	m := covMCPModel(t)
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { osOpen = old })
	reply := openElicit(t, m, mcp.ElicitRequest{Server: "fake", Mode: mcp.ElicitURL, Message: "Connect.", URL: "http://example.com/a?b=1"})
	view := ansi.Strip(m.confirm.View())
	if m.confirmKind != confirmKindMCPElicitURL || m.confirm.SafeID() != "deny" || !strings.Contains(view, "http://example.com/a?b=1") || !strings.Contains(view, "Site: example.com") || !strings.Contains(view, "https") {
		t.Fatalf("card:\n%s", view)
	}
	m.answerMCPElicit(confirmKindMCPElicitURL, "open")
	if r := <-reply; r.Action != mcp.ElicitAccept || len(opened) != 1 || opened[0] != "http://example.com/a?b=1" {
		t.Fatalf("open: %+v %v", r, opened)
	}
}

// The turn's tool calls carry the elicitor: a question reaches the
// chat channel with the server's name.
func TestMCPElicitCallSetsServer(t *testing.T) {
	ch := make(chan tea.Msg, 1)
	call := mcpElicitCall(func(ctx context.Context, server, tool string, args map[string]any) (mcp.CallResult, error) {
		r := mcp.Elicit(ctx, mcp.ElicitRequest{Mode: mcp.ElicitForm})
		return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: r.Action}}}, nil
	}, 3, ch)
	go func() {
		msg := (<-ch).(aiMCPElicitMsg)
		if msg.req.Server != "docs" || msg.gen != 3 {
			msg.reply <- mcp.ElicitResult{Action: "wrong"}
			return
		}
		msg.reply <- mcp.ElicitResult{Action: mcp.ElicitDecline}
	}()
	res, _ := call(context.Background(), "docs", "x", nil)
	if res.Text() != mcp.ElicitDecline {
		t.Fatalf("result %q", res.Text())
	}
}

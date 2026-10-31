package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// MCPX-14: a server's elicitation/create during a tool call reaches the
// call's Elicitor (fields in schema order) and the answer goes back. A
// call without one (no UI) is declined.
func TestClientElicitationForm(t *testing.T) {
	p := startFake(t, nil, Options{})
	var got ElicitRequest
	ctx := WithElicitor(context.Background(), func(_ context.Context, r ElicitRequest) ElicitResult {
		got = r
		return ElicitResult{Action: ElicitAccept, Content: map[string]any{"name": "Bob", "age": 7}}
	})
	res, err := p.Client.CallTool(ctx, "ask", nil)
	if err != nil || res.Text() != `accept {"age":7,"name":"Bob"}` {
		t.Fatalf("result %q %v", res.Text(), err)
	}
	if got.Mode != ElicitForm || got.Message != "Who are you?" || len(got.Fields) != 3 {
		t.Fatalf("request %+v", got)
	}
	f := got.Fields
	if f[0].Name != "name" || !f[0].Required || f[1].Name != "age" || f[1].Type != "integer" || f[2].Name != "color" || strings.Join(f[2].Enum, ",") != "red,blue" {
		t.Fatalf("fields %+v", f)
	}
	res, _ = p.Client.CallTool(context.Background(), "ask", nil)
	if res.Text() != "decline" {
		t.Fatalf("no elicitor: %q", res.Text())
	}
}

// URL mode: the request carries the URL and its id.
func TestClientElicitationURL(t *testing.T) {
	p := startFake(t, nil, Options{})
	var got ElicitRequest
	ctx := WithElicitor(context.Background(), func(_ context.Context, r ElicitRequest) ElicitResult {
		got = r
		return ElicitResult{Action: ElicitDecline}
	})
	res, err := p.Client.CallTool(ctx, "ask_url", nil)
	if err != nil || res.Text() != "decline" || got.Mode != ElicitURL || got.URL != "https://example.com/connect?x=1" || got.ElicitationID != "e1" {
		t.Fatalf("%q %v %+v", res.Text(), err, got)
	}
}

// Secret-looking fields are marked (the UI masks them and warns).
func TestElicitFieldSecret(t *testing.T) {
	for name, want := range map[string]bool{"password": true, "api_key": true, "Token": true, "name": false, "keyboard": false} {
		if got := (ElicitField{Name: name}).Secret(); got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
	if !(ElicitField{Name: "x", Format: "password"}).Secret() {
		t.Error("format password not secret")
	}
}

// While the user answers, the call's no-answer timeout waits.
func TestElicitationPausesCallTimeout(t *testing.T) {
	p := startFake(t, nil, Options{})
	ctx := WithElicitor(context.Background(), func(ctx context.Context, r ElicitRequest) ElicitResult {
		select {
		case <-time.After(600 * time.Millisecond):
		case <-ctx.Done():
			return ElicitResult{Action: ElicitCancel}
		}
		return ElicitResult{Action: ElicitDecline}
	})
	res, err := p.Client.CallToolTimeout(ctx, "ask", nil, 200*time.Millisecond)
	if err != nil || res.Text() != "decline" {
		t.Fatalf("%q %v", res.Text(), err)
	}
}

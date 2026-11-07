package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// viewOf is the View of the EventToolResult of tool name ("" = none).
func viewOf(evs []Event, name string) (*blocks.Block, bool) {
	for _, e := range evs {
		if e.Kind == EventToolResult && e.Tool == name {
			return e.View, true
		}
	}
	return nil, false
}

func listView(args map[string]any, result string) *blocks.Block {
	return &blocks.Block{Kind: blocks.KindList, Title: "Matches", Items: []blocks.BlockItem{{ID: "1", Title: result}}}
}

// MOB-10: a tool's View hook turns a finished call into a block on the
// result event; the agent fills the id and the source, and the model
// still gets the plain text.
func TestAgentToolViewBuildsBlock(t *testing.T) {
	var gotArgs map[string]any
	tools := []Tool{{Name: "search", Display: "Search",
		Run: func(context.Context, map[string]any) (string, error) { return "a.go:1", nil },
		View: func(args map[string]any, result string) *blocks.Block {
			gotArgs = args
			return listView(args, result)
		}}}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "call-7", Name: "search", Args: map[string]any{"q": "x"}}}}}}
	a := &Agent{Provider: p, Native: true, Tools: tools}
	evs, added := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	if added[1].Content != "a.go:1" {
		t.Fatalf("model result = %q", added[1].Content)
	}
	b, ok := viewOf(evs, "search")
	if !ok || b == nil {
		t.Fatalf("no block on the result event: %v", kinds(evs))
	}
	if b.ID != "call-7" || b.Source != "Search" || b.Items[0].Title != "a.go:1" {
		t.Errorf("block = %+v", b)
	}
	if gotArgs["q"] != "x" {
		t.Errorf("View args = %v", gotArgs)
	}
}

// A tool without a View, a failed call, a declined call and a refused
// call carry no block.
func TestAgentToolViewOnlyOnSuccess(t *testing.T) {
	called := 0
	view := func(args map[string]any, result string) *blocks.Block { called++; return listView(args, result) }
	tools := []Tool{
		{Name: "plain", Run: func(context.Context, map[string]any) (string, error) { return "ok", nil }},
		{Name: "fail", View: view, Run: func(context.Context, map[string]any) (string, error) { return "", errors.New("no") }},
		{Name: "ask", View: view, Mutating: true, Run: func(context.Context, map[string]any) (string, error) { return "ok", nil }},
		{Name: "bad", View: view, Validate: func(map[string]any) error { return errors.New("bad arg") },
			Run: func(context.Context, map[string]any) (string, error) { return "ok", nil }},
	}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "1", Name: "plain"}, {ID: "2", Name: "fail"}, {ID: "3", Name: "ask"}, {ID: "4", Name: "bad"}}}}}
	a := &Agent{Provider: p, Native: true, Tools: tools, Confirm: func(context.Context, Tool, string) bool { return false }}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	for _, name := range []string{"plain", "fail", "ask", "bad"} {
		if b, ok := viewOf(evs, name); !ok || b != nil {
			t.Errorf("%s: block = %+v (seen %v)", name, b, ok)
		}
	}
	if called != 0 {
		t.Errorf("View ran %d times", called)
	}
}

// A View that panics or builds a block over the caps loses the block,
// never the call's result.
func TestAgentToolViewPanicOrInvalidDropsBlock(t *testing.T) {
	tools := []Tool{
		{Name: "boom", Run: func(context.Context, map[string]any) (string, error) { return "kept", nil },
			View: func(map[string]any, string) *blocks.Block { panic("view bug") }},
		{Name: "huge", Run: func(context.Context, map[string]any) (string, error) { return "kept too", nil },
			View: func(map[string]any, string) *blocks.Block {
				return &blocks.Block{Kind: blocks.KindText, Title: strings.Repeat("t", blocks.MaxTitle+1)}
			}},
		{Name: "odd", Run: func(context.Context, map[string]any) (string, error) { return "kept 3", nil },
			View: func(map[string]any, string) *blocks.Block { return &blocks.Block{Kind: "hologram", Title: "x"} }},
	}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "1", Name: "boom"}, {ID: "2", Name: "huge"}, {ID: "3", Name: "odd"}}}}}
	a := &Agent{Provider: p, Native: true, Tools: tools}
	evs, added := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	if added[1].Content != "kept" || added[2].Content != "kept too" || added[3].Content != "kept 3" {
		t.Errorf("results = %q / %q / %q", added[1].Content, added[2].Content, added[3].Content)
	}
	for _, name := range []string{"boom", "huge", "odd"} {
		if b, ok := viewOf(evs, name); !ok || b != nil {
			t.Errorf("%s: block = %+v", name, b)
		}
	}
}

// The block's text is redacted on the laptop: a registered secret in
// the result never reaches the phone through the block.
func TestAgentToolViewRedactsSecrets(t *testing.T) {
	secret := "view-secret-value-4242"
	RegisterSecret(secret)
	tools := []Tool{{Name: "env", Run: func(context.Context, map[string]any) (string, error) { return "TOKEN=" + secret, nil },
		View: func(_ map[string]any, result string) *blocks.Block {
			return &blocks.Block{Kind: blocks.KindDetail, Title: "Env " + secret, Fields: []blocks.BlockField{{Label: "TOKEN", Value: result}}}
		}}}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "1", Name: "env"}}}}}
	a := &Agent{Provider: p, Native: true, Tools: tools}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	b, _ := viewOf(evs, "env")
	if b == nil {
		t.Fatal("no block")
	}
	if strings.Contains(b.Title+b.Fields[0].Value, secret) {
		t.Errorf("secret in block: %+v", b)
	}
}

// A View's own id and source are kept.
func TestAgentToolViewKeepsOwnIDAndSource(t *testing.T) {
	tools := []Tool{{Name: "s", Run: func(context.Context, map[string]any) (string, error) { return "r", nil },
		View: func(a map[string]any, r string) *blocks.Block {
			b := listView(a, r)
			b.ID, b.Source = "mine", "git.status"
			return b
		}}}
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "9", Name: "s"}}}}}
	evs, _ := collectEvents(&Agent{Provider: p, Native: true, Tools: tools}, []Message{{Role: RoleUser, Content: "go"}})
	if b, _ := viewOf(evs, "s"); b == nil || b.ID != "mine" || b.Source != "git.status" {
		t.Errorf("block = %+v", b)
	}
}

// StoredMessage.Block survives a save and load of the chat and never
// goes to the model.
func TestStoredMessageBlockRoundTrip(t *testing.T) {
	b := listView(nil, "row")
	b.ID = "c1"
	sm := StoredMessage{Role: StoredTool, Text: "⚙ Search → 1 match", Block: b}
	got := roundTripStored(t, sm)
	if got.Block == nil || got.Block.ID != "c1" || got.Block.Items[0].Title != "row" {
		t.Errorf("block after load = %+v", got.Block)
	}
	if got := (StoredMessage{Role: StoredTool, Text: "x"}); got.Block != nil {
		t.Error("zero message has a block")
	}
}

// roundTripStored saves a chat with sm and loads it again.
func roundTripStored(t *testing.T, sm StoredMessage) StoredMessage {
	t.Helper()
	s := ChatStore{Dir: t.TempDir()}
	c := NewConversation()
	c.Messages = []StoredMessage{{Role: StoredUser, Text: "go"}, sm}
	if err := s.Save(&c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got.Messages[1]
}

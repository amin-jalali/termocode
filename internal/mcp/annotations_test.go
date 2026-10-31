package mcp

import (
	"encoding/json"
	"testing"
)

// Spec (MCPX-38, content annotations): an item for the user only
// (audience ["user"]) is not sent to the model but kept for the user;
// items come to the model by priority, highest first (no priority: 0.5),
// equal ones in their order.
func TestContentAnnotations(t *testing.T) {
	var r CallResult
	raw := `{"content":[
		{"type":"text","text":"low","annotations":{"priority":0.2}},
		{"type":"text","text":"high","annotations":{"priority":0.9,"audience":["assistant","user"]}},
		{"type":"text","text":"for you","annotations":{"audience":["user"]}},
		{"type":"text","text":"plain"},
		{"type":"text","text":"model only","annotations":{"audience":["assistant"]}}]}`
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if got, want := r.Text(), "high\nplain\nmodel only\nlow"; got != want {
		t.Fatalf("model text %q, want %q", got, want)
	}
	if got := r.UserText(); got != "for you" {
		t.Fatalf("user text %q", got)
	}
	plain := CallResult{Content: []Content{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}}
	if plain.Text() != "a\nb" || plain.UserText() != "" {
		t.Fatalf("no annotations: %q %q", plain.Text(), plain.UserText())
	}
}

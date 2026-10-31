package mcptest

import (
	"context"
	"encoding/json"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// Elicit sends one elicitation/create (form for "ask", URL for
// "ask_url") and returns "<action> <content JSON>".
func Elicit(ctx context.Context, c Caller, name string) (any, *extproc.Error) {
	params := map[string]any{"mode": "form", "message": "Who are you?",
		"requestedSchema": json.RawMessage(`{"type":"object","properties":{` +
			`"name":{"type":"string","title":"Your name"},` +
			`"age":{"type":"integer","minimum":0},` +
			`"color":{"type":"string","enum":["red","blue"]}},"required":["name"]}`)}
	if name == "ask_url" {
		params = map[string]any{"mode": "url", "message": "Connect your account.", "url": "https://example.com/connect?x=1", "elicitationId": "e1"}
	}
	raw, err := c.Call(ctx, "elicitation/create", params)
	if err != nil {
		return text("elicit error: " + err.Error()), nil
	}
	var res struct {
		Action  string          `json:"action"`
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(raw, &res)
	out := res.Action
	if len(res.Content) > 0 {
		out += " " + string(res.Content)
	}
	return text(out), nil
}

package ai

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateBodies = flag.Bool("update-bodies", false, "rewrite testdata/request_body_*.json")

// goldenRequest has every kind of message and two tools.
func goldenRequest() Request {
	temp := 0.3
	tool := Tool{Name: "read_file", Description: "Read a file <with> & \"quotes\".",
		Params:   map[string]any{"path": map[string]any{"type": "string", "description": "the path"}, "n": map[string]any{"type": "integer"}},
		Required: []string{"path"}}
	raw := Tool{Name: "mcp__gh__x", Description: "From MCP.", Schema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}
	return Request{
		Model: "m", MaxTokens: 1000, Temperature: &temp,
		Messages: []Message{
			{Role: RoleSystem, Content: "be brief"},
			{Role: RoleSystem, Content: ""},
			{Role: RoleUser, Content: "hi <b>&\n\"q\" ✦"},
			{Role: RoleAssistant, Content: "plain answer"},
			{Role: RoleUser, Content: "look", Images: []Image{{MediaType: "image/png", Data: []byte("PNG")}}},
			{Role: RoleUser, Content: "", Images: []Image{{MediaType: "image/png", Data: []byte("P2")}}},
			{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{{ID: "a", Name: "read_file", Args: map[string]any{"path": "x.go"}}, {ID: "b", Name: "mcp__gh__x"}}},
			{Role: RoleTool, ToolCallID: "a", ToolName: "read_file", Content: "package x"},
			{Role: RoleTool, ToolCallID: "b", ToolName: "mcp__gh__x", Content: "ok"},
			{Role: RoleAssistant, Content: "with text", ToolCalls: []ToolCall{{ID: "c", Name: "read_file", Args: map[string]any{"path": "y.go", "n": 2}}}},
			{Role: RoleTool, ToolCallID: "c", ToolName: "read_file", Content: "package y"},
			{Role: RoleAssistant, Content: "ignored", Blocks: json.RawMessage(`[{"type":"thinking","thinking":"t","signature":"s"},{"type":"text","text":"x"}]`)},
			{Role: RoleUser, Content: "last"},
		},
		Tools: []ToolSpec{tool.Spec(), raw.Spec()},
	}
}

// The request bodies sent to the providers (with the default options:
// the Anthropic ones carry the prompt cache points) stay byte for byte the same
// (testdata/request_body_*.json; -update-bodies rewrites them).
func TestRequestBodiesGolden(t *testing.T) {
	req := goldenRequest()
	bodies := map[string]any{
		"openai":          buildOpenAIBody(req, "m"),
		"anthropic":       anthropicBody(req, "m", Config{Kind: KindAnthropic}),
		"anthropic_oauth": anthropicBody(req, "m", Config{Kind: KindClaudeOAuth}),
	}
	for name, body := range bodies {
		for round := 0; round < 2; round++ { // a second request of the turn reuses the specs
			got, err := marshalBody(body.(map[string]any))
			if err != nil {
				t.Fatal(err)
			}
			if plain, _ := json.Marshal(body); string(plain) != string(got) {
				t.Errorf("%s: marshalBody differs from json.Marshal:\n%s\n%s", name, got, plain)
			}
			p := filepath.Join("testdata", "request_body_"+name+".json")
			if *updateBodies {
				_ = os.MkdirAll("testdata", 0o755)
				if err := os.WriteFile(p, got, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("%s body (round %d) changed:\ngot  %s\nwant %s", name, round, got, want)
			}
			switch name {
			case "openai":
				body = buildOpenAIBody(req, "m")
			case "anthropic":
				body = anthropicBody(req, "m", Config{Kind: KindAnthropic})
			default:
				body = anthropicBody(req, "m", Config{Kind: KindClaudeOAuth})
			}
		}
	}
}

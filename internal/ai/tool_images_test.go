package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// seeing is a scripted provider whose model takes images.
type seeing struct{ scripted }

func (*seeing) SupportsImages() bool { return true }

func imageTool(saw *bool) Tool {
	return Tool{Name: "shot", Run: func(ctx context.Context, _ map[string]any) (string, error) {
		*saw = AttachImages(ctx, Image{MediaType: "image/png", Data: []byte("PNG")})
		return "a screenshot", nil
	}}
}

// A tool's images go with its result to a native model that takes
// images; AttachImages reports true.
func TestAgentToolImagesReachVisionModel(t *testing.T) {
	p := &seeing{scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "c1", Name: "shot"}}}, {Delta: "ok"}}}}
	var saw bool
	a := &Agent{Provider: p, Native: true, Tools: []Tool{imageTool(&saw)}}
	_, added := collectEvents(a, []Message{{Role: RoleUser, Content: "look"}})
	if !saw {
		t.Fatal("AttachImages = false for a vision model")
	}
	var tool *Message
	for i := range added {
		if added[i].Role == RoleTool {
			tool = &added[i]
		}
	}
	if tool == nil || len(tool.Images) != 1 || string(tool.Images[0].Data) != "PNG" {
		t.Fatalf("tool message images: %+v", tool)
	}
}

// A text-only model (or the text protocol) gets no images: AttachImages
// says false so the tool writes a note instead.
func TestAgentToolImagesTextModel(t *testing.T) {
	p := &scripted{tools: true, replies: []Chunk{{ToolCalls: []ToolCall{{ID: "c1", Name: "shot"}}}, {Delta: "ok"}}}
	saw := true
	a := &Agent{Provider: p, Native: true, Tools: []Tool{imageTool(&saw)}}
	_, added := collectEvents(a, []Message{{Role: RoleUser, Content: "look"}})
	if saw {
		t.Fatal("AttachImages = true for a text-only model")
	}
	for _, m := range added {
		if len(m.Images) > 0 {
			t.Fatalf("images sent to a text model: %+v", m)
		}
	}
	if AttachImages(context.Background(), Image{}) {
		t.Fatal("AttachImages outside a call = true")
	}
}

// Anthropic: a tool result with images is a tool_result whose content
// is a text block and image blocks.
func TestAnthropicBodyToolResultImage(t *testing.T) {
	body := buildAnthropicBody(Request{Messages: []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "shot"}}},
		{Role: RoleTool, ToolCallID: "c1", Content: "a screenshot", Images: []Image{{MediaType: "image/png", Data: []byte("PNG")}}},
	}}, "m", false)
	b, _ := json.Marshal(body)
	s := string(b)
	for _, want := range []string{`"type":"tool_result"`, `"tool_use_id":"c1"`, `"text":"a screenshot"`, `"type":"image"`, `"data":"UE5H"`} {
		if !strings.Contains(s, want) {
			t.Errorf("anthropic body misses %s:\n%s", want, s)
		}
	}
}

// OpenAI: tool messages are text only, so the images follow all the
// step's tool messages in one user message.
func TestOpenAIBodyToolResultImage(t *testing.T) {
	body := buildOpenAIBody(Request{Messages: []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "shot"}, {ID: "c2", Name: "shot"}}},
		{Role: RoleTool, ToolCallID: "c1", Content: "one", Images: []Image{{MediaType: "image/png", Data: []byte("PNG")}}},
		{Role: RoleTool, ToolCallID: "c2", Content: "two"},
	}}, "m")
	msgs := body["messages"].([]openAIMessage)
	if len(msgs) != 4 || msgs[1].Role != "tool" || msgs[2].Role != "tool" || msgs[3].Role != "user" {
		t.Fatalf("roles: %+v", msgs)
	}
	if s, ok := msgs[1].Content.(string); !ok || s != "one" {
		t.Fatalf("tool content: %#v", msgs[1].Content)
	}
	b, _ := json.Marshal(msgs[3])
	if !strings.Contains(string(b), `data:image/png;base64,UE5H`) || !strings.Contains(string(b), "c1") {
		t.Fatalf("image message: %s", b)
	}
}

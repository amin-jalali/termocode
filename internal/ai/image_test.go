package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// A user message with an image is sent as an image block + a text block
// on the Anthropic Messages API (base64 source).
func TestAnthropicBodyImage(t *testing.T) {
	body := buildAnthropicBody(Request{Messages: []Message{
		{Role: RoleUser, Content: "what is this?", Images: []Image{{MediaType: "image/png", Data: []byte("PNGDATA")}}},
	}}, "m", false)
	b, _ := json.Marshal(body)
	s := string(b)
	for _, want := range []string{`"type":"image"`, `"media_type":"image/png"`, `"data":"UE5HREFUQQ=="`, `"type":"base64"`, `"text":"what is this?"`} {
		if !strings.Contains(s, want) {
			t.Errorf("anthropic body misses %s:\n%s", want, s)
		}
	}
}

// OpenAI-compatible: content becomes an array of text + image_url parts
// with a data: URL. A message without images keeps plain string content.
func TestOpenAIBodyImage(t *testing.T) {
	body := buildOpenAIBody(Request{Messages: []Message{
		{Role: RoleUser, Content: "plain"},
		{Role: RoleUser, Content: "look", Images: []Image{{MediaType: "image/jpeg", Data: []byte("JPG")}}},
	}}, "m")
	b, _ := json.Marshal(body)
	s := string(b)
	for _, want := range []string{`"content":"plain"`, `"type":"image_url"`, `"url":"data:image/jpeg;base64,SlBH"`, `"text":"look","type":"text"`} {
		if !strings.Contains(s, want) {
			t.Errorf("openai body misses %s:\n%s", want, s)
		}
	}
}

func TestSupportsImages(t *testing.T) {
	cases := map[string]bool{
		KindAnthropic: true, KindClaudeOAuth: true, KindOpenAI: true, KindOpenRouter: true,
		KindOllama: false, KindLMStudio: false, KindCompatible: false, KindNone: false,
	}
	for kind, want := range cases {
		p := NewProvider(Config{Kind: kind, APIKey: "k-12345678", BaseURL: "http://x"})
		if got := SupportsImages(p); got != want {
			t.Errorf("SupportsImages(%s) = %v, want %v", kind, got, want)
		}
	}
}

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (AU-28): before an MCP call is sent, its arguments are scanned
// for stored secrets and token-like values. A hit makes the call ask
// (Deny first, no "Always allow"), the card names the argument, never
// the value, and the shown arguments are redacted.
func TestOutgoingArgsSecretScan(t *testing.T) {
	stored := "stored-secret-" + "value-123"
	ai.RegisterSecret(stored)
	gh := "ghp_" + "abcdefghijklmnop1234"
	tl := AITools(Server{Name: "fake"}, []Tool{{Name: "echo", Annotations: ann(bp(true), nil)}},
		bigCall("ok"), AdapterOptions{TrustAnnotations: true})[0]
	if tl.Gate == nil {
		t.Fatal("no per-call gate")
	}
	if mut, class := tl.Gate(map[string]any{"text": "hello"}); mut || class != "" {
		t.Errorf("clean args: %v %q", mut, class)
	}
	args := map[string]any{"text": "x " + stored, "opts": map[string]any{"list": []any{"a", gh}}}
	mut, class := tl.Gate(args)
	if !mut || class != "" {
		t.Errorf("secret args: %v %q", mut, class)
	}
	h := tl.Human(args)
	if strings.Contains(h, stored) || strings.Contains(h, gh) {
		t.Errorf("card shows a secret: %q", h)
	}
	for _, want := range []string{"text", "opts.list[1]", "secret"} {
		if !strings.Contains(h, want) {
			t.Errorf("card %q misses %q", h, want)
		}
	}
	if got := ScanSecrets(map[string]any{"n": 5, "s": "plain"}); len(got) != 0 {
		t.Errorf("false hit %v", got)
	}
	// A mutating tool keeps asking, but with no "Always allow".
	w := AITools(Server{Name: "fake"}, []Tool{{Name: "write_note"}}, bigCall("ok"), AdapterOptions{})[0]
	if mut, class := w.Gate(map[string]any{"body": stored}); !mut || class != "" {
		t.Errorf("mutating with secret: %v %q", mut, class)
	}
	if _, class := w.Gate(map[string]any{"body": "x"}); class != ClassPrefix+"fake" {
		t.Errorf("mutating clean class %q", class)
	}
	_, _ = tl.Run(context.Background(), map[string]any{"text": "a"})
}

// A long token is redacted before the shown value is cut.
func TestShortArgsRedactsBeforeCut(t *testing.T) {
	long := "ghp_" + strings.Repeat("a", 60)
	if got := shortArgs(map[string]any{"k": strings.Repeat("x", 30) + " " + long}); strings.Contains(got, "ghp_") {
		t.Errorf("shown %q", got)
	}
}

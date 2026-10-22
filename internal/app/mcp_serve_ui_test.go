package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// mcpServeCard is the open MCP card's text.
func mcpServeCard(m *Model) string { return m.confirm.View() }

// Spec (MS-3): the card names the client that asks and the tool, and shows
// the key arguments of the call.
func TestMCPServeCardTitleNamesClient(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "claude-code", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
	view := mcpServeCard(m)
	for _, want := range []string{"claude-code wants to run debug_breakpoint", "op=clear"} {
		if !strings.Contains(view, want) {
			t.Errorf("card has no %q:\n%s", want, view)
		}
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, call)
}

// Spec (MS-3): a client that sends no name gets the generic title.
func TestMCPServeCardTitleFallback(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "An outside agent") })
	if view := mcpServeCard(m); !strings.Contains(view, "An outside agent wants to run debug_breakpoint") {
		t.Errorf("card:\n%s", view)
	}
	mcpServeAnswer(m, "deny")
	mcpServeAwait(t, m, hub, call)
}

// Spec (MS-3): a secret in the arguments (a key pattern, or a value the IDE
// knows as a secret) never reaches the card, in the text or the arguments.
func TestMCPServeCardHidesSecrets(t *testing.T) {
	known := "ms3-known-secret-value-7781"
	ai.RegisterSecret(known)
	pattern := "sk-ant-api03-" + strings.Repeat("a1B2c3", 6)
	for _, secret := range []string{known, pattern} {
		m, src, hub := mcpServeTestModel(t)
		call := mcpServeStartCall(src, "claude-code", "debug_evaluate", map[string]any{"expr": "use(\"" + secret + "\")"})
		mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "claude-code") })
		if view := mcpServeCard(m); strings.Contains(view, secret) || strings.Contains(view, secret[:14]) {
			t.Errorf("the card shows a secret:\n%s", view)
		}
		mcpServeAnswer(m, "deny")
		mcpServeAwait(t, m, hub, call)
	}
}

//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (MCP-10): in ASCII icon mode the MCP screens show no ✦ and no ⇄:
// the card of an outside agent, the status screen and the OpenCode card
// use the theme's glyphs (ASCII there), and the Unicode mode keeps ✦.
func TestMCPServeScreensFollowIconMode(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)

	screens := func(t *testing.T) map[string]string {
		m := mcpServeStatusModel(t)
		m.w, m.h = 150, 42
		out := map[string]string{}
		out["card title"] = mcpServeCardTitle("claude-code", "tasks_run", "Run task: x")
		m.openMCPServeStatus()
		out["status"] = ansi.Strip(m.picker.View())
		dir := agentCardWorkspace(t)
		if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"theme":"x"}`), 0o640); err != nil {
			t.Fatal(err)
		}
		card, _ := m.agentMCPCardFor(agentByID(t, "opencode"))
		if card == nil {
			t.Fatal("no OpenCode card")
		}
		m.openAgentMCPCard(card)
		out["opencode card"] = ansi.Strip(m.confirm.View())
		return out
	}

	theme.CurrentIconMode = theme.IconModeASCII
	for name, text := range screens(t) {
		if strings.ContainsAny(text, "✦⇄") {
			t.Errorf("ASCII mode: %s shows a Unicode glyph:\n%s", name, text)
		}
	}
	theme.CurrentIconMode = theme.IconModeUnicode
	got := screens(t)
	if !strings.Contains(got["card title"], "✦") || !strings.Contains(got["opencode card"], "✦") {
		t.Errorf("Unicode mode lost its glyphs: %q", got)
	}
}

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/debounce"
	"github.com/amin-jalali/termocode/internal/ext"
)

// TestNextEditToggleSavesAndSyncs: the palette command flips ai_next_edit,
// saves it and turns the Lua half on and off.
func TestNextEditToggleSavesAndSyncs(t *testing.T) {
	c, _ := nesNvim(t, "x\n")
	s := newAIState()
	s.provider = &nesProvider{}
	m := &Model{w: 100, h: 30, nvim: c, debounce: debounce.New(), ai: s}
	m.syncAIInline()
	waitLua(t, c, `tostring(_G._termocode_ai.nes_enabled)`, "true")

	found := false
	for _, e := range aiPaletteItems() {
		if e.id == "ai-toggle-next-edit" && e.title == "AI: Toggle Next Edit Suggestions" {
			found = true
		}
	}
	if !found {
		t.Fatal("palette entry missing")
	}
	if _, ok := m.dispatchAIPalette("ai-toggle-next-edit"); !ok {
		t.Fatal("palette id not handled")
	}
	path, _ := ai.ConfigPath()
	if st := ai.LoadSettings(path); st.NextEdit == nil || *st.NextEdit {
		t.Fatalf("ai_next_edit not saved off: %+v", st.NextEdit)
	}
	if m.aiNESEnabled() {
		t.Fatal("still on")
	}
	waitLua(t, c, `tostring(_G._termocode_ai.nes_enabled)`, "false")
	m.dispatchAIPalette("ai-toggle-next-edit")
	waitLua(t, c, `tostring(_G._termocode_ai.nes_enabled)`, "true")
}

// TestNextEditSettingRow: the Settings row reads and writes ai_next_edit.
func TestNextEditSettingRow(t *testing.T) {
	withConfigDir(t)
	m := &Model{w: 100, h: 30, debounce: debounce.New(), ai: newAIState()}
	if v, ok := aiSettingValue("setting-ai-next-edit"); !ok || v != "true" {
		t.Fatalf("value %q ok=%v", v, ok)
	}
	if _, ok := m.applyAISetting("setting-ai-next-edit", "false"); !ok {
		t.Fatal("row not applied")
	}
	if v, _ := aiSettingValue("setting-ai-next-edit"); v != "false" {
		t.Fatalf("after apply %q", v)
	}
	row := false
	for _, r := range settingsRows {
		if r.ID == "setting-ai-next-edit" && r.Label == "ai_next_edit" && r.Kind == "bool" {
			row = true
		}
	}
	if !row {
		t.Fatal("settings row missing")
	}
}

// TestNextEditStatusHint: the status bar tells what Tab does.
func TestNextEditStatusHint(t *testing.T) {
	m := &Model{w: 120, h: 30, ai: newAIState()}
	plain := func() string {
		var out []string
		for _, s := range m.aiNESStatusTexts() {
			out = append(out, ext.PlainText(s))
		}
		return strings.Join(out, "|")
	}
	if got := plain(); got != "" {
		t.Fatalf("hint with nothing shown: %q", got)
	}
	m.ai.nes.shown = "shown"
	if got := plain(); !strings.Contains(got, "Tab: go to next edit") {
		t.Fatalf("shown hint %q", got)
	}
	m.ai.nes.shown = "jumped"
	if got := plain(); !strings.Contains(got, "Tab: apply") || !strings.Contains(got, "Esc: dismiss") {
		t.Fatalf("jumped hint %q", got)
	}
	if st := m.statusState(); !strings.Contains(strings.Join(st.Ext, "|"), "Tab: apply") {
		t.Fatalf("status bar misses the hint: %q", st.Ext)
	}
}

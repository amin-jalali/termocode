package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// covKeyModel is a sized model with a private config dir.
func covKeyModel(t *testing.T) (Model, string) {
	t.Helper()
	dir := withConfigDir(t)
	m := covNew(t)
	m.w, m.h = 120, 40
	return m, dir
}

func covKeyFor(m Model, a keymap.Action) string {
	for k, b := range m.keys.Bindings() {
		if b == a {
			return k
		}
	}
	return ""
}

// The whole Customize Keybindings flow: palette → picker row → prompt
// (pre-filled with the current key) → saved, applied and toasted.
func TestCovCoreKeybindingRebind(t *testing.T) {
	m, dir := covKeyModel(t)
	cmd := m.dispatchPaletteAction("customize-keybindings")
	_ = cmd
	if !m.pickerOpen || m.pickerKind != pickerKindKeybinding {
		t.Fatalf("picker not open: open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	name := keymap.ActionName(keymap.ActionToggleTerminal)
	m, _ = m.update(picker.SelectMsg{ID: "kb-" + name})
	if !m.promptOpen || m.promptKind != promptKindKeybinding || m.keybindingPromptAction != name {
		t.Fatalf("prompt: open=%v kind=%v action=%q", m.promptOpen, m.promptKind, m.keybindingPromptAction)
	}
	if cur := m.prompt.Value(); m.keys.Bindings()[cur] != keymap.ActionToggleTerminal {
		t.Errorf("prompt value %q is not a current key of the action", cur)
	}
	m, _ = m.update(prompt.SubmitMsg{Value: "  ctrl+alt+y "})
	if got := m.keys.Bindings()["ctrl+alt+y"]; got != keymap.ActionToggleTerminal {
		t.Errorf("ctrl+alt+y → %v, want the new binding live", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, "termocode", "keymap.json"))
	if err != nil || !strings.Contains(string(b), `"ctrl+alt+y"`) {
		t.Errorf("keymap.json = %q, %v", b, err)
	}
	if !strings.Contains(toastText(m), "ctrl+alt+y") {
		t.Errorf("no toast for the new key:\n%s", toastText(m))
	}
	if m.keybindingPromptAction != "" {
		t.Error("the prompt action was kept after the save")
	}
}

func TestCovCoreKeybindingRejects(t *testing.T) {
	m, dir := covKeyModel(t)
	// Rows that are not keybinding rows are ignored.
	if m.onKeybindingRowSelected("kb-") != nil || m.onKeybindingRowSelected("x-toggle") != nil || m.promptOpen {
		t.Error("a bad row opened the prompt")
	}
	// No action asked: nothing happens.
	if m.applyKeybinding("ctrl+x") != nil {
		t.Error("apply without an action did something")
	}
	// A malformed key.
	m.keybindingPromptAction = keymap.ActionName(keymap.ActionToggleTerminal)
	_ = m.applyKeybinding("ctrl+")
	if !strings.Contains(toastText(m), "Invalid key") {
		t.Errorf("no invalid-key toast:\n%s", toastText(m))
	}
	// An action that does not exist.
	m.keybindingPromptAction = "no-such-action"
	_ = m.applyKeybinding("ctrl+alt+u")
	if !strings.Contains(toastText(m), "Unknown action") {
		t.Errorf("no unknown-action toast:\n%s", toastText(m))
	}
	if _, err := os.Stat(filepath.Join(dir, "termocode", "keymap.json")); err == nil {
		t.Error("a rejected key was saved")
	}
}

// A key another action uses asks first; Replace moves it, Cancel keeps
// everything.
func TestCovCoreKeybindingConflict(t *testing.T) {
	for _, choice := range []string{"cancel", "replace"} {
		t.Run(choice, func(t *testing.T) {
			m, _ := covKeyModel(t)
			taken := covKeyFor(m, keymap.ActionQuickOpen)
			if taken == "" {
				t.Skip("quick open has no default key")
			}
			_ = m.onKeybindingRowSelected("kb-" + keymap.ActionName(keymap.ActionToggleTerminal))
			m, _ = m.update(prompt.SubmitMsg{Value: taken})
			if !m.confirmOpen || m.confirmKind != confirmKindKeybindingConflict {
				t.Fatalf("no conflict question: open=%v kind=%v", m.confirmOpen, m.confirmKind)
			}
			// Safe first: Cancel is focused.
			if v := m.confirm.View(); !strings.Contains(v, "Key already in use") {
				t.Errorf("confirm view:\n%s", v)
			}
			m, _ = m.update(confirm.SelectMsg{ID: choice})
			got := m.keys.Bindings()[taken]
			if choice == "replace" && got != keymap.ActionToggleTerminal {
				t.Errorf("replace: %s → %v", taken, got)
			}
			if choice == "cancel" && got != keymap.ActionQuickOpen {
				t.Errorf("cancel: %s → %v", taken, got)
			}
			if m.keybindingPromptAction != "" || m.extras.kbPendingKey != "" {
				t.Error("pending rebind kept after the answer")
			}
		})
	}
}

func TestCovCoreKeybindingSaveFails(t *testing.T) {
	m, dir := covKeyModel(t)
	// A folder where keymap.json must go: the save fails.
	if err := os.MkdirAll(filepath.Join(dir, "termocode", "keymap.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := m.keys.Bindings()["ctrl+alt+y"]
	m.keybindingPromptAction = keymap.ActionName(keymap.ActionToggleTerminal)
	_ = m.applyKeybinding("ctrl+alt+y")
	if !strings.Contains(toastText(m), "Keymap error") {
		t.Errorf("no keymap error toast:\n%s", toastText(m))
	}
	if m.keys.Bindings()["ctrl+alt+y"] != before {
		t.Error("the key went live although the save failed")
	}
}

func TestCovCoreKeymapProblemsToast(t *testing.T) {
	m, dir := covKeyModel(t)
	if m.keymapProblemsToast() != nil {
		t.Error("a toast with no problems")
	}
	for _, tc := range []struct {
		probs []string
		title string
	}{
		{[]string{`"ctrl+" is not a key`}, "keymap.json: 1 entry ignored"},
		{[]string{"a", "b", "c"}, "keymap.json: 3 entries ignored"},
	} {
		m.keymapProblems = tc.probs
		_ = m.keymapProblemsToast()
		if !strings.Contains(toastText(m), tc.title) {
			t.Errorf("toast misses %q:\n%s", tc.title, toastText(m))
		}
		if m.keymapProblems != nil {
			t.Error("problems kept after the toast")
		}
	}
	log, _ := os.ReadFile(filepath.Join(dir, "termocode", "errors.log"))
	if !strings.Contains(string(log), `keymap.json: "ctrl+" is not a key`) {
		t.Errorf("errors.log:\n%s", log)
	}
}

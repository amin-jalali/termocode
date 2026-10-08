package keymap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOverrides(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "keymap.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidateKey(t *testing.T) {
	good := []string{
		"ctrl+k", "alt+h", "f4", "f24", "shift+f4", "ctrl+shift+t", "alt+T",
		"ctrl+_", "ctrl+\\", "ctrl+`", "ctrl++", "+", "a", "/", "ctrl+pgdown",
		"alt+enter", "shift+tab", "space", "ctrl+alt+i", "alt+é",
	}
	for _, k := range good {
		if err := ValidateKey(k); err != nil {
			t.Errorf("ValidateKey(%q) = %v, want ok", k, err)
		}
	}
	bad := []string{
		"", " ctrl+k", "ctrl+", "ctrl+monkey", "ctrl+ctrl+s", "F5", "f0", "f25",
		"meta+k", "ctrl+ ", "pageup",
	}
	for _, k := range bad {
		if err := ValidateKey(k); err == nil {
			t.Errorf("ValidateKey(%q) = ok, want an error", k)
		}
	}
	// Every default binding must pass, or the validator is too strict.
	for k := range Default().bindings {
		if err := ValidateKey(k); err != nil {
			t.Errorf("default key: %v", err)
		}
	}
}

// keymap.json may bind keys no default uses (ctrl+k); typos are dropped
// and reported, ext: entries are left to LoadExtBindings.
func TestLoadOverridesReport(t *testing.T) {
	p := writeOverrides(t, `{
		"ctrl+k": "QuickOpen",
		"alt+s": "Save",
		"ctrl+monkey": "Save",
		"alt+j": "Sav",
		"alt+h": "ext:hello.say"
	}`)
	got, probs := LoadOverridesReport(p)
	if got["ctrl+k"] != ActionQuickOpen || got["alt+s"] != ActionSave || len(got) != 2 {
		t.Errorf("overrides = %v", got)
	}
	if len(probs) != 2 {
		t.Fatalf("problems = %q, want 2", probs)
	}
	joined := strings.Join(probs, "\n")
	for _, want := range []string{"ctrl+monkey", `unknown action "Sav"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems %q do not mention %q", joined, want)
		}
	}
	if _, probs := LoadOverridesReport(writeOverrides(t, "{broken")); len(probs) != 1 {
		t.Errorf("broken JSON: problems = %q", probs)
	}
	if got, probs := LoadOverridesReport(filepath.Join(t.TempDir(), "none.json")); got != nil || probs != nil {
		t.Errorf("missing file: %v %q", got, probs)
	}
	if !IsKnownKey("ctrl+k") {
		t.Error("IsKnownKey(ctrl+k) = false")
	}
}

// The retired "MdLivePreview" name still loads and runs the Markdown
// preview, so old keymap.json files keep working.
func TestLegacyMdLivePreviewName(t *testing.T) {
	got := LoadOverrides(writeOverrides(t, `{"alt+p": "MdLivePreview"}`))
	if got["alt+p"] != ActionMarkdownPreview {
		t.Errorf("MdLivePreview: got %q, want MarkdownPreview", ActionName(got["alt+p"]))
	}
	for _, a := range AllActions() {
		if ActionName(a) == "MdLivePreview" {
			t.Error("MdLivePreview must not be listed as an action")
		}
	}
}

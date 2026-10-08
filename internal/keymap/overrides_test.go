package keymap

import (
	"os"
	"path/filepath"
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

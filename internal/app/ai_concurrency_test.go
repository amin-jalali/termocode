package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-26: ai_concurrency is applied when the AI settings load; a bad
// entry is named with the profile problems.
func TestAIConcurrencyLoaded(t *testing.T) {
	m := aiTestModel(t)
	t.Cleanup(func() { ai.SetConcurrencyLimits(nil) })
	path, err := ai.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(`{"ai_concurrency": {"anthropic": 2, "nope": 1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ai.reload()
	if got := ai.ConcurrencyLimit("anthropic"); got != 2 {
		t.Errorf("limit %d, want 2", got)
	}
	if !strings.Contains(strings.Join(m.ai.profiles.Errors, "\n"), `ai_concurrency: "nope"`) {
		t.Errorf("problems: %v", m.ai.profiles.Errors)
	}
}

package shotkit

import (
	"path/filepath"
	"testing"
)

// Run must remove its private config dir (termocode-shots-*) when it
// returns, so repeated runs do not fill TMPDIR.
func TestRunLeavesNoTempDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if _, err := Run(Toast(), 80, 24, Options{Quiet: true}); err != nil {
		t.Skipf("scene cannot run here: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(tmp, "termocode-*"))
	if len(left) != 0 {
		t.Fatalf("temp dirs left behind: %v", left)
	}
}

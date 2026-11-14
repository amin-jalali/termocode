//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// Spec (bench finding, PR #535): `termocode --no-session <file>` opens
// that file in the editor, with the file's folder as the workspace.
// Before, the path was dropped without a word and the Welcome page of
// the current folder opened.
func TestOpenFileFromTheCommandLine(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, "argdir-sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(proj, "arg-file.txt")
	if err := os.WriteFile(file, []byte("opened from the command line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Args: []string{"--no-session", file}}) // the cwd is another repo
	tm.WaitText("opened from the command line", uiTimeout)
	tm.WaitText("argdir-sub", uiTimeout) // the explorer lists the file's folder
	tm.Quit()
}

package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Temp folders (test runs, screenshot demos, extracted archives) never
// reach the recent workspaces on the start page.
func TestPushWorkspaceSkipsTempFolders(t *testing.T) {
	withConfigDir(t) // a real config, outside the temp folder below
	t.Setenv("TMPDIR", t.TempDir())
	pushWorkspace(filepath.Join(os.TempDir(), "demo"))
	pushWorkspace("/home/someone/proj")
	got := loadWorkspaces()
	if len(got) != 1 || got[0].Path != "/home/someone/proj" {
		t.Errorf("workspaces = %+v, want only /home/someone/proj", got)
	}
}

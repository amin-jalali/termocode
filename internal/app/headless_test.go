package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/toast"
)

// Headless (tests): Reload Window and Open Workspace must not re-exec the
// process (that would replace the test binary), and nothing is opened
// with xdg-open.
func TestHeadlessHostActionsAreSkipped(t *testing.T) {
	if !headless() {
		t.Fatal("TestMain should make tests headless")
	}
	m := Model{toast: toast.New()}
	_ = m.reloadWindow()
	_ = m.openWorkspace(t.TempDir())
	if err := defaultOpen(t.TempDir()); err != errHeadless {
		t.Errorf("defaultOpen = %v, want errHeadless", err)
	}
}

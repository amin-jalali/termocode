package git

import (
	"os"
	"testing"
)

// TestMain runs the rebase helper when git started this binary as one.
// It also cuts the tests off the developer's git setup: no global or
// system config (no signing, editor, hooks path, autosquash …) and a
// fixed identity, so every commit and rebase here behaves the same on any
// machine.
func TestMain(m *testing.M) {
	RunRebaseHelperIfAsked() // git re-runs this binary as its rebase editor
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "Test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "Test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		_ = os.Setenv(k, v)
	}
	os.Exit(m.Run())
}

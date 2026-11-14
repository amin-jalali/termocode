package app

import (
	"os"
	"path/filepath"
)

// EnvStartFile names a file the command line asked for (`termocode
// <file>`): cmd/termocode sets it, the UI opens the file once the editor
// is ready. It is an environment variable so a UI that a new session
// server starts gets it too. The UI clears it, so the shells it starts
// do not inherit it.
const EnvStartFile = "TERMOCODE_START_FILE"

// openStartFile opens the command line's file (EnvStartFile), after the
// session restore, so it is the file in front.
func (m *Model) openStartFile() {
	p := os.Getenv(EnvStartFile)
	if p == "" {
		return
	}
	_ = os.Unsetenv(EnvStartFile)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return
	}
	m.openFileAtLine(p, 1)
	m.explorer.RevealPath(p)
}

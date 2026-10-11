package app

import (
	"errors"
	"os"

	"github.com/charmbracelet/x/term"
)

// headless reports whether the app runs without a real terminal: stdout is
// not a TTY, or TERMOCODE_HEADLESS=1 (internal/testkit and the app tests
// set it). Then the app does nothing outside its own window: no escapes
// to the host terminal (OSC 52, fullscreen), no host key presses, no
// xdg-open, and no re-exec of the process (Reload Window, Open Workspace),
// which would replace a test binary.
func headless() bool {
	return os.Getenv("TERMOCODE_HEADLESS") == "1" || !term.IsTerminal(os.Stdout.Fd())
}

// errHeadless is returned by host actions skipped in headless mode.
var errHeadless = errors.New("not available without a terminal")

// writeTerminal writes an out-of-band escape sequence to the host
// terminal, unless headless.
func writeTerminal(seq string) {
	if !headless() {
		_, _ = os.Stdout.WriteString(seq)
	}
}

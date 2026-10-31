package ai

import (
	"path/filepath"
	"strings"
	"time"
)

// EnvInfo is what the env block of the system prompt says (OMX-10).
type EnvInfo struct {
	Now          time.Time // local time; only the date is used
	GOOS, GOARCH string
	Shell        string // $SHELL (a path) or "" when unknown
}

// EnvBlock is the system prompt's environment lines: today's date, the
// OS and the shell, and how hidden commands run. Only the date changes,
// once a day, so the prompt cache still works.
func EnvBlock(e EnvInfo) string {
	osName := e.GOOS
	switch e.GOOS {
	case "darwin":
		osName = "macOS"
	case "windows":
		osName = "Windows"
	}
	var b strings.Builder
	b.WriteString("Today is " + e.Now.Format("2006-01-02") + " (" + e.Now.Weekday().String() + "). OS: " + osName)
	if e.GOARCH != "" {
		b.WriteString(" (" + e.GOARCH + ")")
	}
	if sh := filepath.Base(strings.TrimSpace(e.Shell)); sh != "" && sh != "." && sh != "/" {
		b.WriteString(", shell: " + sh)
	}
	b.WriteString(".\nrun_command has no terminal and no stdin; it runs with PAGER=cat, GIT_TERMINAL_PROMPT=0 and CI=1. " +
		"Use non-interactive flags (for example --yes, --no-edit) and never start a command that waits for input.")
	return b.String()
}

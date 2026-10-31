package mcp

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runtime check before a start (DI-18): a local server whose program is
// not installed fails at once with what to install, not after the fact.

// MissingRuntime is a server program that is not on PATH.
type MissingRuntime struct {
	Command string
	Hint    string // what to install, with its page
}

func (e *MissingRuntime) Error() string {
	return fmt.Sprintf("%s not found. %s", e.Command, e.Hint)
}

// runtimeHints maps common runtimes to what to install.
var runtimeHints = map[string]string{
	"npx":     "Install Node.js (it brings npx): https://nodejs.org",
	"node":    "Install Node.js: https://nodejs.org",
	"npm":     "Install Node.js (it brings npm): https://nodejs.org",
	"uvx":     "Install uv (it brings uvx): https://docs.astral.sh/uv/",
	"uv":      "Install uv: https://docs.astral.sh/uv/",
	"docker":  "Install Docker and start it: https://docs.docker.com/get-docker/",
	"podman":  "Install Podman: https://podman.io",
	"python":  "Install Python 3: https://www.python.org/downloads/",
	"python3": "Install Python 3: https://www.python.org/downloads/",
	"pipx":    "Install pipx: https://pipx.pypa.io",
	"bunx":    "Install Bun (it brings bunx): https://bun.sh",
	"bun":     "Install Bun: https://bun.sh",
	"deno":    "Install Deno: https://deno.com",
}

// RuntimeHint is what to install for a missing command.
func RuntimeHint(command string) string {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(command)), ".exe")
	if h, ok := runtimeHints[base]; ok {
		return h
	}
	return "Install it, or set command to a full path."
}

// CheckRuntime fails with a *MissingRuntime when command can not run.
func CheckRuntime(command string) error {
	if command == "" {
		return nil
	}
	if _, err := exec.LookPath(command); err != nil {
		return &MissingRuntime{Command: command, Hint: RuntimeHint(command)}
	}
	return nil
}

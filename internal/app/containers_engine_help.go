package app

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// Engine start help (CTX-42): when the engine is not running, the
// Containers view shows the command that starts it (shown, never run;
// y copies it), and Containers: Open Engine Config opens daemon.json
// (Docker) or containers.conf (Podman) in the editor.

// engineGOOS is runtime.GOOS (tests change it).
var engineGOOS = runtime.GOOS

// engineStartCommand is the command that starts the engine, or a hint
// when there is no command (Docker Desktop is an app).
func engineStartCommand(kind, goos string) (cmd, hint string) {
	if kind == "podman" {
		if goos == "linux" {
			return "systemctl --user start podman.socket", ""
		}
		return "podman machine start", ""
	}
	if goos == "linux" {
		return "sudo systemctl start docker", ""
	}
	return "", "Open Docker Desktop"
}

// engineConfigPath is the engine's config file: Docker's daemon.json
// (the system one on Linux, the Desktop one elsewhere) or Podman's
// containers.conf.
func engineConfigPath(kind, goos, home string) string {
	if kind == "podman" {
		return filepath.Join(home, ".config", "containers", "containers.conf")
	}
	if goos == "linux" {
		return "/etc/docker/daemon.json"
	}
	return filepath.Join(home, ".docker", "daemon.json")
}

// engineStartRow is the start command row (code) or its hint row.
func engineStartRow(kind, section string) svcRow {
	cmd, hint := engineStartCommand(kind, engineGOOS)
	if cmd != "" {
		return svcRow{kind: svcRowCode, section: section, text: cmd}
	}
	return svcRow{kind: svcRowHint, section: section, text: hint}
}

// engineStartCopy is the start command y copies while the engine is not
// running ("" = none to copy).
func (st *servicesState) engineStartCopy() string {
	kind := ""
	switch {
	case st.problem.Kind == services.ProblemDaemonDown:
		kind = st.composeProblem().Engine
	case !st.containerEngine.Reachable && st.containerEngine.Problem.Kind == services.ProblemDaemonDown:
		kind = st.containerEngine.Kind
	default:
		return ""
	}
	cmd, _ := engineStartCommand(kind, engineGOOS)
	return cmd
}

// openEngineConfig opens the engine's config file (a new buffer when it
// does not exist yet).
func (m *Model) openEngineConfig() tea.Cmd {
	home, err := os.UserHomeDir()
	if err != nil {
		return m.servicesToast(toast.Warn, "No home folder", err.Error())
	}
	kind := "docker"
	if m.services != nil && m.services.containerEngine.Kind == "podman" {
		kind = "podman"
	}
	p := engineConfigPath(kind, engineGOOS, home)
	m.openFileAtLine(p, 1)
	if _, err := os.Stat(p); err != nil {
		return m.servicesToast(toast.Info, "New file: "+filepath.Base(p), "Restart the engine after you save it.")
	}
	return m.servicesToast(toast.Info, filepath.Base(p), "Restart the engine after you change it.")
}

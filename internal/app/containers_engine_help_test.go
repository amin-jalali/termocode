package app

import (
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

func TestEngineStartCommand(t *testing.T) {
	for _, tc := range []struct{ kind, goos, cmd, hint string }{
		{"docker", "linux", "sudo systemctl start docker", ""},
		{"", "linux", "sudo systemctl start docker", ""},
		{"podman", "linux", "systemctl --user start podman.socket", ""},
		{"podman", "darwin", "podman machine start", ""},
		{"podman", "windows", "podman machine start", ""},
		{"docker", "darwin", "", "Open Docker Desktop"},
		{"docker", "windows", "", "Open Docker Desktop"},
	} {
		cmd, hint := engineStartCommand(tc.kind, tc.goos)
		if cmd != tc.cmd || hint != tc.hint {
			t.Errorf("%s/%s = %q %q, want %q %q", tc.kind, tc.goos, cmd, hint, tc.cmd, tc.hint)
		}
	}
}

func TestEngineConfigPath(t *testing.T) {
	home := "/home/u"
	for _, tc := range []struct{ kind, goos, want string }{
		{"docker", "linux", "/etc/docker/daemon.json"},
		{"docker", "darwin", filepath.Join(home, ".docker", "daemon.json")},
		{"docker", "windows", filepath.Join(home, ".docker", "daemon.json")},
		{"podman", "linux", filepath.Join(home, ".config", "containers", "containers.conf")},
		{"podman", "darwin", filepath.Join(home, ".config", "containers", "containers.conf")},
	} {
		if got := engineConfigPath(tc.kind, tc.goos, home); got != tc.want {
			t.Errorf("%s/%s = %q, want %q", tc.kind, tc.goos, got, tc.want)
		}
	}
}

// The engine-down rows show the start command to copy (y), then Retry.
func TestEngineDownRowsShowTheStartCommand(t *testing.T) {
	old := engineGOOS
	engineGOOS = "darwin"
	t.Cleanup(func() { engineGOOS = old })
	st := svcHostState()
	st.containerEngine.Kind = "podman"
	st.problem = services.Problem{Kind: services.ProblemDaemonDown}
	rows := st.problemRows()
	if len(rows) != 4 || rows[0].text != "Podman is not running" || rows[1].kind != svcRowCode || rows[1].text != "podman machine start" ||
		rows[2].kind != svcRowHint || rows[3].kind != svcRowRetry {
		t.Fatalf("rows %+v", rows)
	}
	st = svcHostState()
	st.containerEngine.Reachable = false
	st.containerEngine.Kind = "docker"
	st.containerEngine.Problem = services.Problem{Kind: services.ProblemDaemonDown}
	body := st.hostSectionBody("containers")
	if len(body) != 3 || body[1].kind != svcRowHint || body[1].text != "Open Docker Desktop" || body[2].kind != svcRowRetry {
		t.Fatalf("containers rows %+v", body)
	}
}

// y on an engine that is not running copies its start command.
func TestEngineStartCopyText(t *testing.T) {
	old := engineGOOS
	engineGOOS = "linux"
	t.Cleanup(func() { engineGOOS = old })
	st := svcHostState()
	if got := st.engineStartCopy(); got != "" {
		t.Fatalf("engine up: %q", got)
	}
	st.problem = services.Problem{Kind: services.ProblemDaemonDown}
	if got := st.engineStartCopy(); got != "sudo systemctl start docker" {
		t.Fatalf("compose down: %q", got)
	}
	st = svcHostState()
	st.containerEngine.Reachable = false
	st.containerEngine.Kind = "podman"
	st.containerEngine.Problem = services.Problem{Kind: services.ProblemDaemonDown}
	if got := st.engineStartCopy(); got != "systemctl --user start podman.socket" {
		t.Fatalf("engine down: %q", got)
	}
	engineGOOS = "darwin"
	st.containerEngine.Kind = "docker"
	if got := st.engineStartCopy(); got != "" {
		t.Fatalf("Docker Desktop has no command: %q", got)
	}
}

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/zalando/go-keyring"
)

// TestMain points HOME and every XDG dir at a temp folder, so no test in
// this package reads or writes the developer's real ~/.config/termocode
// (errors.log, recents, session …) or clones plugins into ~/.local/share.
// It also cuts the system clipboard and the network off, and puts
// fail-closed docker / podman / kubectl stand-ins first on PATH: a test
// that forgets its fake (servicestest) fails instead of reaching the
// developer's real Docker or cluster.
func TestMain(m *testing.M) {
	mcp.RunShareIfHelper() // the shared-server broker re-runs this binary
	mcptest.RunIfHelper()  // the fake MCP server re-runs this binary
	dir, err := os.MkdirTemp("", "termocode-app-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	home := filepath.Join(dir, "home")
	for _, d := range []string{"config", "data", "state", "cache",
		"plugins/vim-visual-multi/.git", "plugins/nvim-dap/.git",
		".local/share/termocode/plugins/rainbow-delimiters.nvim"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	for k, v := range map[string]string{
		"HOME":                      home,
		"XDG_CONFIG_HOME":           filepath.Join(home, "config"),
		"XDG_DATA_HOME":             filepath.Join(home, "data"),
		"XDG_STATE_HOME":            filepath.Join(home, "state"),
		"XDG_CACHE_HOME":            filepath.Join(home, "cache"),
		"TERMOCODE_PLUGINS_DIR":     filepath.Join(home, "plugins"),
		"TERMOCODE_HEADLESS":        "1",
		"TERMOCODE_NO_DB_DISCOVERY": "1",
		"DISPLAY":                   "",
		"WAYLAND_DISPLAY":           "",
		"HTTP_PROXY":                "http://127.0.0.1:9",
		"HTTPS_PROXY":               "http://127.0.0.1:9",
		"http_proxy":                "http://127.0.0.1:9",
		"https_proxy":               "http://127.0.0.1:9",
		// No D-Bus session: the OS keyring is never probed (secrets_ui.go).
		"DBUS_SESSION_BUS_ADDRESS": "",
	} {
		_ = os.Setenv(k, v)
	}
	guard, err := servicestest.InstallGuard(filepath.Join(dir, "cli-guard"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = os.Setenv("PATH", guard.PathEnv())
	cliGuard = guard
	setupCardOff = true // see setup_card_ui.go
	// The secret store: an in-memory keyring, never the real one, and the
	// plain backend these tests were written for (they share one config
	// dir; secrets_ui_test.go opts in to auto / age / keyring).
	keyring.MockInit()
	secretStoreSettingOverride = "plain"
	secretKeyringProbe = func() bool { return false }
	// Never the real claude / codex (no subscription use): a CLI check
	// says nothing and a CLI step fails unless a test fakes it.
	aiCLICheck = func(context.Context, string, []string) clistep.Status { return clistep.StatusUnknown }
	aiCLIRun = func(context.Context, clistep.Spec, string, func(ai.Event)) (clistep.Result, error) {
		return clistep.Result{}, errors.New("a test ran a CLI step without faking aiCLIRun")
	}
	nvim.TrackClients()
	code := m.Run()
	if msg := guard.Report(""); msg != "" {
		fmt.Fprintln(os.Stderr, "FAIL:", msg)
	if code == 0 && nvimLeaked() {
		code = 1
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// cliGuard holds the stand-ins TestMain put first on PATH.
var cliGuard *servicestest.Guard

// Guard for TestMain: with no fake installed, every container / cluster
// tool on PATH is the guard's stand-in, never the real one.
func TestRealContainerCLIsAreBlocked(t *testing.T) {
	for _, name := range servicestest.GuardedCLIs {
		p, err := exec.LookPath(name)
		if err != nil {
			continue // not installed at all: nothing to reach
		}
		if cliGuard == nil || filepath.Dir(p) != cliGuard.Bin {
			t.Errorf("a test would run the real %s at %s", name, p)
		}
	}
// nvimLeaked reports (on stderr) the tests whose nvim still runs after
// all tests ended: every test must close its nvim and wait for it.
func nvimLeaked() bool {
	r := nvim.LeakReport(15 * time.Second)
	if r != "" {
		fmt.Fprint(os.Stderr, "FAIL: "+r)
	}
	return r != ""
}

// Guard for TestMain: recordError must land in the temp config dir.
func TestErrorLogIsSandboxed(t *testing.T) {
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, os.TempDir()) {
		t.Fatalf("config dir %q is outside the temp dir", dir)
	}
	recordError("sandbox check")
	if _, err := os.Stat(filepath.Join(dir, "errors.log")); err != nil {
		t.Fatalf("errors.log not in sandbox: %v", err)
	}
}

// newTestModel is New for tests: the nvim it starts is closed, and waited
// for, when the test ends.
func newTestModel(tb testing.TB) Model {
	tb.Helper()
	m := New()
	if c := m.nvim; c != nil {
		tb.Cleanup(func() { _ = c.Close() })
	}
	return m
}

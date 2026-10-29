//go:build unix

package clilogin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/lspinstall"
)

func status(s clistep.Status) func(context.Context, string, []string) clistep.Status {
	return func(context.Context, string, []string) clistep.Status { return s }
}

func installOK(context.Context, lspinstall.Tool, lspinstall.Progress) (string, error) { return "", nil }

func TestFlowUnknownCLI(t *testing.T) {
	end := last(t, drain(t, Run(context.Background(), fast(Config{CLI: "aider"})), nil))
	if end.Phase != PhaseFailed || end.Err.Msg != "Unknown sign-in." || end.Err.Error() != "Unknown sign-in." {
		t.Fatalf("end %+v", end)
	}
}

// The installer says it worked, but the CLI is still not there.
func TestFlowInstalledButStillMissing(t *testing.T) {
	cfg := fast(Config{CLI: ai.CLICodex, Check: status(clistep.StatusMissing), Install: installOK})
	end := last(t, drain(t, Run(context.Background(), cfg), nil))
	if end.Err == nil || end.Err.Kind != FailInstall || !strings.Contains(end.Err.Detail, "not found in the tools folder") {
		t.Fatalf("end %+v", end)
	}
}

func TestFlowUnsupportedSystem(t *testing.T) {
	cfg := fast(Config{CLI: ai.CLIClaude, Check: status(clistep.StatusMissing)})
	cfg.Install = func(context.Context, lspinstall.Tool, lspinstall.Progress) (string, error) {
		return "", &lspinstall.UnsupportedError{Tool: "claude-code", Platform: "plan9"}
	}
	end := last(t, drain(t, Run(context.Background(), cfg), nil))
	if end.Err == nil || end.Err.Msg != "Could not install Claude Code on this system. Use an API key instead." || !strings.Contains(end.Err.Detail, "plan9") {
		t.Fatalf("end %+v", end)
	}
}

// The error detail keeps only the installer's last 8 lines.
func TestFlowInstallDetailKeepsLastLines(t *testing.T) {
	var out []string
	for i := 1; i <= 12; i++ {
		out = append(out, "line"+strings.Repeat("x", i))
	}
	cfg := fast(Config{CLI: ai.CLICodex, Check: status(clistep.StatusMissing)})
	cfg.Install = func(context.Context, lspinstall.Tool, lspinstall.Progress) (string, error) {
		return strings.Join(out, "\n"), errors.New("exit status 1")
	}
	end := last(t, drain(t, Run(context.Background(), cfg), nil))
	d := end.Err.Detail
	if strings.Contains(d, out[3]+"\n") || !strings.HasPrefix(d, out[4]) || !strings.HasSuffix(d, "exit status 1") {
		t.Fatalf("detail %q", d)
	}
}

// A stop before the first check ends the flow with no state at all.
func TestFlowCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sts := drain(t, Run(ctx, fast(Config{CLI: ai.CLICodex, Check: status(clistep.StatusMissing)})), nil); len(sts) != 0 {
		t.Fatalf("states %+v", sts)
	}
}

// The status says logged out, but the CLI cannot start (gone meanwhile).
func TestFlowLoginCannotStart(t *testing.T) {
	cfg := fast(Config{CLI: ai.CLICodex, Env: []string{"PATH=" + t.TempDir()}, Check: status(clistep.StatusLoggedOut)})
	end := last(t, drain(t, Run(context.Background(), cfg), nil))
	if end.Err == nil || end.Err.Msg != "Could not start the Codex sign-in. Try again, or use an API key." {
		t.Fatalf("end %+v", end)
	}
}

// Cancel while the browser sign-in waits: no Failed state, the CLI is
// stopped.
func TestFlowCancelWhileWaitingForBrowser(t *testing.T) {
	env, home := fakeEnv(t, "FAKE_LOGIN=hang")
	f := Run(context.Background(), fast(Config{CLI: ai.CLICodex, Env: env}))
	if err := f.Write("x"); err != nil && err.Error() != "no sign-in is running" {
		t.Errorf("write before the sign-in: %v", err)
	}
	sts := drain(t, f, func(st State) {
		if st.Phase == PhaseWaitingForBrowser && st.URL != "" {
			f.Cancel()
		}
	})
	for _, st := range sts {
		if st.Phase == PhaseFailed {
			t.Fatalf("cancel must not fail: %+v", st)
		}
	}
	gone(t, pidFile(t, home, "login.pid"))
	gone(t, pidFile(t, home, "child.pid"))
	if err := f.Write("x"); err == nil || err.Error() != "no sign-in is running" {
		t.Errorf("write after the end: %v", err)
	}
}

func TestPhaseNames(t *testing.T) {
	for p, want := range map[Phase]string{
		PhaseInstalling: "installing", PhaseWaitingForBrowser: "waiting for browser", PhaseLoadingModels: "loading models",
		PhaseReady: "ready", PhaseFailed: "failed", Phase(99): "unknown",
	} {
		if p.String() != want {
			t.Errorf("%d = %q", p, p.String())
		}
	}
	if ToolName("aider") != "" {
		t.Error("unknown cli has no tool")
	}
}

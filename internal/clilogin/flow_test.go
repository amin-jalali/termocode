//go:build unix

package clilogin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lspinstall"
)

// fast are the test timings: quick ticks and polls, a short grace.
func fast(cfg Config) Config {
	cfg.Tick, cfg.PollEvery, cfg.ExitGrace = 30*time.Millisecond, 50*time.Millisecond, 100*time.Millisecond
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return cfg
}

// drain reads every state until the flow closes its channel. onState
// may act on a state (e.g. paste a code).
func drain(t *testing.T, f *Flow, onState func(State)) []State {
	t.Helper()
	var out []State
	timeout := time.After(15 * time.Second)
	for {
		select {
		case st, ok := <-f.Updates():
			if !ok {
				return out
			}
			out = append(out, st)
			if onState != nil {
				onState(st)
			}
		case <-timeout:
			t.Fatalf("flow did not end; states %+v", out)
		}
	}
}

func phases(sts []State) []Phase {
	var out []Phase
	for _, st := range sts {
		if len(out) == 0 || out[len(out)-1] != st.Phase {
			out = append(out, st.Phase)
		}
	}
	return out
}

func last(t *testing.T, sts []State) State {
	t.Helper()
	if len(sts) == 0 {
		t.Fatal("no states")
	}
	return sts[len(sts)-1]
}

// From nothing to Ready: (a fake) npm installs the (fake) CLI into the
// tools folder, the login prints its link, the status turns ready.
func TestFlowInstallThenSignIn(t *testing.T) {
	tools, fake, home := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(lspinstall.ToolsDirEnv, tools)
	t.Setenv("PATH", fake+":/usr/bin:/bin")
	t.Setenv("HOME", home)
	t.Setenv("FAKE_LOGIN", "ok")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-not-for-the-cli")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	cli := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(cli, []byte(fakeCodex), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"node": "exit 0\n",
		"npm": `prefix=""
while [ $# -gt 0 ]; do [ "$1" = "--prefix" ] && prefix="$2"; shift; done
sleep 0.3
mkdir -p "$prefix/node_modules/.bin"
cp "` + cli + `" "$prefix/node_modules/.bin/codex"
`,
	} {
		if err := os.WriteFile(filepath.Join(fake, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sts := drain(t, Run(context.Background(), fast(Config{CLI: ai.CLICodex})), nil)
	want := []Phase{PhaseInstalling, PhaseWaitingForBrowser, PhaseLoadingModels, PhaseReady}
	if got := phases(sts); !reflect.DeepEqual(got, want) {
		t.Fatalf("phases %v, want %v (states %+v)", got, want, sts)
	}
	var ticks int
	var maxElapsed time.Duration
	var url string
	for _, st := range sts {
		if st.Phase == PhaseInstalling {
			ticks++
			maxElapsed = max(maxElapsed, st.Elapsed)
		}
		if st.URL != "" {
			url = st.URL
		}
	}
	if ticks < 3 || maxElapsed < 100*time.Millisecond {
		t.Fatalf("install timer: %d states, max %v", ticks, maxElapsed)
	}
	if !strings.HasPrefix(url, "https://auth.openai.com/oauth/authorize") {
		t.Fatalf("url %q", url)
	}
	end := last(t, sts)
	if !reflect.DeepEqual(end.Models, ai.CLIModels(ai.CLICodex)) {
		t.Fatalf("models %v", end.Models)
	}
	if _, err := os.Stat(filepath.Join(tools, "bin", "codex")); err != nil {
		t.Fatal("no shim in tools/bin")
	}
	if _, err := os.Stat(filepath.Join(home, "leak")); err == nil {
		t.Fatal("the login saw an API key")
	}
}

func TestFlowNoNpm(t *testing.T) {
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	sts := drain(t, Run(context.Background(), fast(Config{CLI: ai.CLICodex})), nil)
	end := last(t, sts)
	if end.Phase != PhaseFailed || end.Err == nil || end.Err.Kind != FailNoNode {
		t.Fatalf("end %+v", end)
	}
	if want := "Codex needs Node.js. Install Node.js (nodejs.org or your package manager), then press Try again. Or use an API key instead."; end.Err.Msg != want {
		t.Fatalf("msg %q", end.Err.Msg)
	}
}

func TestFlowOfflineInstall(t *testing.T) {
	cfg := fast(Config{CLI: ai.CLIClaude, Env: []string{"PATH=" + t.TempDir()}})
	cfg.Install = func(context.Context, lspinstall.Tool, lspinstall.Progress) (string, error) {
		return "npm error code EAI_AGAIN", &lspinstall.OfflineError{Tool: "claude-code"}
	}
	end := last(t, drain(t, Run(context.Background(), cfg), nil))
	if end.Err == nil || end.Err.Kind != FailOffline || end.Err.Msg != "No connection. Try again when you are online." {
		t.Fatalf("end %+v", end)
	}
}

func TestFlowInstallFailedKeepsDetailRedacted(t *testing.T) {
	ai.RegisterSecret("sk-ant-very-secret-123456")
	cfg := fast(Config{CLI: ai.CLIClaude, Env: []string{"PATH=" + t.TempDir()}})
	cfg.Install = func(context.Context, lspinstall.Tool, lspinstall.Progress) (string, error) {
		return "npm error EACCES\nkey sk-ant-very-secret-123456\n", os.ErrPermission
	}
	end := last(t, drain(t, Run(context.Background(), cfg), nil))
	if end.Err == nil || end.Err.Kind != FailInstall || !strings.HasPrefix(end.Err.Msg, "Could not install Claude Code.") {
		t.Fatalf("end %+v", end)
	}
	if strings.Contains(end.Err.Detail, "very-secret") || !strings.Contains(end.Err.Detail, "EACCES") {
		t.Fatalf("detail %q", end.Err.Detail)
	}
}

func TestFlowAlreadySignedIn(t *testing.T) {
	env, home := fakeEnv(t)
	_ = os.WriteFile(filepath.Join(home, "logged-in"), nil, 0o644)
	sts := drain(t, Run(context.Background(), fast(Config{CLI: ai.CLIClaude, Env: env})), nil)
	if got, want := phases(sts), []Phase{PhaseLoadingModels, PhaseReady}; !reflect.DeepEqual(got, want) {
		t.Fatalf("phases %v", got)
	}
}

// The status poll ends the flow while the CLI still runs; the CLI is
// stopped after the grace time.
func TestFlowPollReadyThenStopsCLI(t *testing.T) {
	env, home := fakeEnv(t, "FAKE_LOGIN=okstay")
	sts := drain(t, Run(context.Background(), fast(Config{CLI: ai.CLICodex, Env: env})), nil)
	if end := last(t, sts); end.Phase != PhaseReady {
		t.Fatalf("end %+v", end)
	}
	gone(t, pidFile(t, home, "login.pid"))
}

func TestFlowTimeout(t *testing.T) {
	env, home := fakeEnv(t, "FAKE_LOGIN=hang")
	cfg := fast(Config{CLI: ai.CLICodex, Env: env, Timeout: 400 * time.Millisecond})
	end := last(t, drain(t, Run(context.Background(), cfg), nil))
	if end.Err == nil || end.Err.Kind != FailTimeout || end.Err.Msg != "Sign-in took too long. Try again." {
		t.Fatalf("end %+v", end)
	}
	gone(t, pidFile(t, home, "login.pid"))
	gone(t, pidFile(t, home, "child.pid"))
}

func TestFlowNotFinished(t *testing.T) {
	env, _ := fakeEnv(t, "FAKE_LOGIN=fail")
	end := last(t, drain(t, Run(context.Background(), fast(Config{CLI: ai.CLICodex, Env: env})), nil))
	if end.Err == nil || end.Err.Kind != FailNotFinished || end.Err.Msg != "Sign-in did not finish. Try again, or use an API key." {
		t.Fatalf("end %+v", end)
	}
	if strings.Contains(end.Err.Msg+end.Err.Detail, "https://") {
		t.Fatal("the sign-in link must not be in an error")
	}
}

func TestFlowNodeMissingAtLogin(t *testing.T) {
	env, _ := fakeEnv(t, "FAKE_LOGIN=nonode")
	end := last(t, drain(t, Run(context.Background(), fast(Config{CLI: ai.CLICodex, Env: env})), nil))
	if end.Err == nil || end.Err.Kind != FailNoNode {
		t.Fatalf("end %+v", end)
	}
}

// A pasted code goes to the CLI; the flow then ends ready.
func TestFlowPasteCode(t *testing.T) {
	env, _ := fakeEnv(t)
	f := Run(context.Background(), fast(Config{CLI: ai.CLIClaude, Env: env}))
	asked := false
	sts := drain(t, f, func(st State) {
		if st.NeedsCode && !asked {
			asked = true
			if err := f.Write("GOOD-CODE#state"); err != nil {
				t.Error(err)
			}
		}
	})
	if !asked || last(t, sts).Phase != PhaseReady {
		t.Fatalf("asked %v, states %+v", asked, sts)
	}
}

func TestFlowDeviceAuthOverSSH(t *testing.T) {
	env, _ := fakeEnv(t, "FAKE_LOGIN=ok", "SSH_CONNECTION=10.0.0.1 5 10.0.0.2 22")
	sts := drain(t, Run(context.Background(), fast(Config{CLI: ai.CLICodex, Env: env})), nil)
	var code, url string
	for _, st := range sts {
		if st.Code != "" {
			code, url = st.Code, st.URL
		}
	}
	if code != "WXYZ-98765" || url != "https://auth.openai.com/codex/device" {
		t.Fatalf("code %q url %q", code, url)
	}
	if last(t, sts).Phase != PhaseReady {
		t.Fatal("not ready")
	}
}

// Cancel during the install cancels the installer's context and closes
// the flow without a Failed state.
func TestFlowCancelMidInstall(t *testing.T) {
	started, sawCancel := make(chan struct{}), make(chan bool, 1)
	cfg := fast(Config{CLI: ai.CLICodex, Env: []string{"PATH=" + t.TempDir()}})
	cfg.Install = func(ctx context.Context, _ lspinstall.Tool, p lspinstall.Progress) (string, error) {
		p("[1/1] npm install @openai/codex@latest")
		close(started)
		<-ctx.Done()
		sawCancel <- true
		return "", ctx.Err()
	}
	f := Run(context.Background(), cfg)
	go func() { <-started; f.Cancel() }()
	sts := drain(t, f, nil)
	select {
	case <-sawCancel:
	case <-time.After(5 * time.Second):
		t.Fatal("installer context not cancelled")
	}
	for _, st := range sts {
		if st.Phase == PhaseFailed {
			t.Fatalf("cancel must not fail: %+v", st)
		}
		if st.Phase == PhaseInstalling && st.Line != "" && st.Line != "[1/1] npm install @openai/codex@latest" {
			t.Fatalf("line %q", st.Line)
		}
	}
}

// A phase change is never lost to a context that ended meanwhile (the
// signed-in CLI's exit grace) while the card still has room for it:
// Ready must reach the card.
func TestSendDeliversAfterContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 200; i++ {
		f := &Flow{ch: make(chan State, 1)}
		f.send(ctx, State{Phase: PhaseReady})
		select {
		case st := <-f.ch:
			if st.Phase != PhaseReady {
				t.Fatalf("got %v", st.Phase)
			}
		default:
			t.Fatalf("run %d: Ready was dropped", i)
		}
	}
}

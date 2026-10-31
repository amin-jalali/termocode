package ai

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// OMX-10: run_command runs without a terminal: pagers print, git never
// asks for a password, tools see CI and do not wait for input.
func TestRunCommandNonInteractiveEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	t.Setenv("PAGER", "less")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	w := Workspace{Root: t.TempDir()}
	out, err := runTool(t, w, "run_command", map[string]any{"command": `echo "P=$PAGER G=$GIT_PAGER T=$GIT_TERMINAL_PROMPT CI=$CI"; read x || echo "no stdin"`})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"P=cat", "G=cat", "T=0", "CI=1", "no stdin"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in %q", want, out)
		}
	}
}

func TestEnvBlock(t *testing.T) {
	now := time.Date(2026, 10, 31, 23, 59, 0, 0, time.UTC)
	got := EnvBlock(EnvInfo{Now: now, GOOS: "linux", GOARCH: "amd64", Shell: "/usr/bin/zsh"})
	for _, want := range []string{"Today is 2026-10-31 (Saturday)", "OS: linux (amd64)", "shell: zsh", "PAGER=cat", "no stdin"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in %q", want, got)
		}
	}
	if strings.Contains(got, "23:59") {
		t.Errorf("the time changes every request and breaks the prompt cache: %q", got)
	}
	if got := EnvBlock(EnvInfo{Now: now, GOOS: "darwin", GOARCH: "arm64"}); !strings.Contains(got, "OS: macOS (arm64)") || strings.Contains(got, "shell:") {
		t.Errorf("darwin, no shell: %q", got)
	}
}

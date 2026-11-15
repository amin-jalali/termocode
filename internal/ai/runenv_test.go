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

// A hidden command never opens an editor, a prompt or an auto-update:
// git commit/merge/rebase keep their default message, package managers
// answer yes, credential helpers never ask.
func TestRunCommandNoEditorNoPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	t.Setenv("EDITOR", "vim")
	t.Setenv("VISUAL", "nvim")
	t.Setenv("GIT_EDITOR", "vim")
	w := Workspace{Root: t.TempDir()}
	out, err := runTool(t, w, "run_command", map[string]any{"command": `for v in GIT_EDITOR EDITOR VISUAL GIT_SEQUENCE_EDITOR GIT_MERGE_AUTOEDIT DEBIAN_FRONTEND npm_config_yes PIP_NO_INPUT HOMEBREW_NO_AUTO_UPDATE GCM_INTERACTIVE; do eval "echo $v=[\$$v]"; done`})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GIT_EDITOR=[:]", "\nEDITOR=[:]", "VISUAL=[]", "GIT_SEQUENCE_EDITOR=[:]", "GIT_MERGE_AUTOEDIT=[no]",
		"DEBIAN_FRONTEND=[noninteractive]", "npm_config_yes=[true]", "PIP_NO_INPUT=[1]", "HOMEBREW_NO_AUTO_UPDATE=[1]", "GCM_INTERACTIVE=[never]"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in %q", want, out)
		}
	}
	if strings.Contains(out, "vim") {
		t.Errorf("the user's editor leaked in: %q", out)
	}
}

// git commit --amend without -m in a hidden command keeps its message
// and ends at once: no editor waits until the time limit.
func TestRunCommandGitAmendNoEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	dir := gitRepo(t)
	t.Setenv("EDITOR", "sleep 6;")
	t.Setenv("GIT_EDITOR", "sleep 6;")
	w := Workspace{Root: dir, RunTimeout: 10 * time.Second}
	start := time.Now()
	out, err := runTool(t, w, "run_command", map[string]any{"command": "git commit -q --allow-empty --amend && git log -1 --format=msg=%s"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "timed out") || time.Since(start) > 5*time.Second || !strings.Contains(out, "msg=") {
		t.Fatalf("git waited for an editor (%s): %q", time.Since(start), out)
	}
}

// The run_command description tells the model that full-screen programs
// (editors, pagers, top) cannot work there.
func TestRunCommandDescribesNoFullScreen(t *testing.T) {
	tl := wsTool(t, Workspace{Root: t.TempDir()}, "run_command")
	for _, want := range []string{"vim", "less", "top"} {
		if !strings.Contains(tl.Description, want) {
			t.Errorf("run_command description does not name %q: %q", want, tl.Description)
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

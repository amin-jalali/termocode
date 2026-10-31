package ai

import (
	"runtime"
	"strings"
	"testing"
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

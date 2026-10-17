//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (T1 + T2, real PTY): with bash as $SHELL and an AI provider set
// up (an unreachable local one: nothing is sent unless clicked), `false`
// in the terminal shows the "Fix?" chip on its tab; a good command (`cd`
// into a folder) removes it and the tab is named after the folder, from
// the shell's own OSC 7.
func TestShellIntegrationBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	ws := NewRepo(t)
	if err := os.Mkdir(filepath.Join(ws, "e2ecwd"), 0o755); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Cols: 120, Rows: 36, Workspace: ws, Env: []string{
		"SHELL=" + bash, "PS1=$ ",
		"TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake",
		"OLLAMA_HOST=http://127.0.0.1:9", "TERMOCODE_AI_BASE_URL=http://127.0.0.1:9/v1",
	}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.OpenTerminal()
	tm.step("bash open")

	tm.Type("false\r")
	tm.WaitText("Fix?", uiTimeout)
	tm.step("fix chip after false")

	tm.Type("cd e2ecwd\r")
	tm.WaitGone("Fix?", uiTimeout)
	tm.WaitFor("tab named after the folder", uiTimeout, func(ls []string) bool {
		for _, l := range ls {
			if strings.Contains(l, "$ e2ecwd ") {
				return true
			}
		}
		return false
	})
	tm.step("tab follows cd")
	tm.Quit()
}

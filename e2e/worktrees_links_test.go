//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// W3 on the real binary (WTX-9..12): an external worktree says no meta;
// i links PR #12 through a fake gh and the card shows its state; an agent
// in a terminal tab leaves a note and a status with `termocode worktree`
// and both show on the card; F forgets a worktree whose folder is gone.
func TestWorktreeLinkNoteForget(t *testing.T) {
	ws := NewRepo(t)
	ext, gone := ws+"-ext", ws+"-gone"
	t.Cleanup(func() { _ = os.RemoveAll(ext) })
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("worktree", "add", "-q", "-b", "ext", ext)
	git("worktree", "add", "-q", "-b", "gone", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	gh := "#!/bin/sh\nif [ \"$1 $2 $3\" = \"pr view 12\" ]; then\n" +
		`  echo '{"number":12,"title":"Fix the race","state":"OPEN","url":"https://github.com/o/r/pull/12","statusCheckRollup":[{"__typename":"CheckRun","name":"CI","status":"COMPLETED","conclusion":"SUCCESS"}]}'` +
		"\n  exit 0\nfi\necho 'unknown' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := filepath.Abs(binPath)
	if err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Workspace: ws, Cols: 150, Rows: 42, Env: []string{"PATH=" + bin + ":" + os.Getenv("PATH")}})
	tm.WaitText("README.md", uiTimeout)
	tm.Palette("Worktree: Show")
	tm.WaitText("EXTERNAL", uiTimeout)
	tm.WaitText("no meta", uiTimeout)

	// Link PR #12 to the external worktree.
	tm.Send("/")
	tm.Type("ext")
	tm.Send(KeyEnter)
	tm.settle()
	tm.Send("i")
	tm.WaitText("Link an issue or PR", uiTimeout)
	tm.Type("12")
	tm.Send(KeyEnter)
	tm.WaitText("PR #12 open", 20*time.Second)

	// An agent in a terminal tab leaves a note and a status.
	tm.OpenTerminal()
	tm.Type("cd '" + ext + "' && '" + self + "' worktree note 'from the cli' && '" + self + "' worktree status blocked && echo NOTED\r")
	tm.WaitText("NOTED", 20*time.Second)
	tm.WaitText("from the cli", 20*time.Second)
	tm.WaitText("blocked", uiTimeout)

	// Forget the gone one.
	tm.Send(KeyCtrlT) // the panel closes; the keys go back to the view
	tm.WaitGone("NOTED", uiTimeout)
	tm.Palette("Worktree: Show")
	tm.Send(KeyEsc) // clears the name filter
	tm.settle()
	tm.Send("/")
	tm.Type("gone")
	tm.Send(KeyEnter)
	tm.settle()
	tm.Send("F")
	tm.WaitText("Forgot", uiTimeout)
	if out := git("worktree", "list", "--porcelain"); strings.Contains(out, gone) {
		t.Errorf("git still lists the gone worktree:\n%s", out)
	}
	tm.Quit()
}

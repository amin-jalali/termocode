//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// backupFiles lists the hot-exit backup files under home.
func backupFiles(home string) []string {
	var out []string
	_ = filepath.Walk(filepath.Join(home, ".local/state/termocode/backup"), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// TestHotExitRestore: without a session, the terminal hangs up (SIGHUP to
// the whole process group) while a file has unsaved
// text. The next start offers to restore it, and Restore brings the text
// back as unsaved changes. A clean quit leaves no backup.
func TestHotExitRestore(t *testing.T) {
	home := t.TempDir()
	ws := NewRepo(t)
	start := func() *Term {
		tm := Start(t, StartOpts{Home: home, Workspace: ws, Args: []string{"--no-session"}})
		tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
			return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
		})
		return tm
	}
	tm := start()
	tm.settle()
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Type("ohot-exit-marker-9")
	tm.WaitText("hot-exit-marker-9", uiTimeout)

	// The live backup is written a moment after the last change.
	deadline := time.Now().Add(5 * time.Second)
	for len(backupFiles(home)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no hot-exit backup written")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := syscall.Kill(-tm.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("termocode did not exit on SIGHUP")
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "notes.txt")); strings.Contains(string(b), "hot-exit-marker-9") {
		t.Fatalf("the unsaved text was written to the file itself")
	}

	tm2 := start()
	tm2.WaitText("Restore unsaved changes?", bootTimeout)
	tm2.WaitText("notes.txt", uiTimeout)
	tm2.settle()
	tm2.ClickText("Restore")
	tm2.WaitText("hot-exit-marker-9", uiTimeout)
	tm2.WaitFor("notes.txt tab, modified", uiTimeout, func(ls []string) bool {
		return strings.Contains(ls[0], "notes.txt ●")
	})
	tm2.step("restored")

	tm2.quitDiscard()
	if left := backupFiles(home); len(left) > 0 {
		t.Errorf("backup left after a clean quit: %v", left)
	}
}

// TestHotExitHangupFlushesLatest: the terminal hangs up right after a
// keystroke, before the debounced backup runs. SIGHUP goes to the whole
// process group, as on a dropped SSH connection. The backup must still
// hold the latest text: termocode flushes it before nvim is gone.
func TestHotExitHangupFlushesLatest(t *testing.T) {
	home := t.TempDir()
	ws := NewRepo(t)
	tm := Start(t, StartOpts{Home: home, Workspace: ws, Args: []string{"--no-session"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send("ohangup-marker-7")
	tm.WaitText("hangup-marker-7", uiTimeout)

	// No wait for the debounced backup: hang up at once.
	if err := syscall.Kill(-tm.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("termocode did not exit on SIGHUP")
	}
	var got string
	for _, f := range backupFiles(home) {
		b, _ := os.ReadFile(f)
		got += string(b)
	}
	if !strings.Contains(got, "hangup-marker-7") {
		t.Fatalf("backup misses the last text; backup files %v hold %q", backupFiles(home), got)
	}
}

// TestNvimEndsWithTermocode: nvim runs in its own session (a terminal
// hangup does not reach it), so nothing kills it with termocode's
// process group. When termocode dies hard (SIGKILL, no cleanup), nvim
// must still exit (EOF on its embed pipe) and end its jobs.
func TestNvimEndsWithTermocode(t *testing.T) {
	tm := Start(t, StartOpts{Args: []string{"--no-session"},
		NvimInit: "vim.fn.jobstart({ 'sleep', '300' })\n"})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	pgrep := func(args ...string) []string {
		out, _ := exec.Command("pgrep", args...).Output()
		return strings.Fields(string(out))
	}
	ppid := strconv.Itoa(tm.cmd.Process.Pid)
	var nvims, jobs []string
	deadline := time.Now().Add(5 * time.Second)
	for len(nvims) == 0 || len(jobs) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("nvim %v or its job %v not found", nvims, jobs)
		}
		time.Sleep(50 * time.Millisecond)
		nvims = pgrep("-P", ppid, "-x", "nvim")
		if len(nvims) == 1 {
			jobs = pgrep("-P", nvims[0], "-x", "sleep")
		}
	}

	if err := tm.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-tm.exited
	deadline = time.Now().Add(5 * time.Second)
	for {
		var left []string
		for _, p := range append(nvims, jobs...) {
			if n, _ := strconv.Atoi(p); syscall.Kill(n, 0) == nil {
				left = append(left, p)
			}
		}
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("still running after termocode was killed: %v", left)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestHotExitDiscard: Discard drops the backup; the file stays as saved.
func TestHotExitDiscard(t *testing.T) {
	home := t.TempDir()
	ws := NewRepo(t)
	// A backup left by a process that is gone.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	pid := strconv.Itoa(dead.ProcessState.Pid())
	writeFile(t, filepath.Join(home, ".local/state/termocode/backup", hashKey(ws), pid, "x.bak"),
		filepath.Join(ws, "notes.txt")+"\nfrom a crash")

	tm := Start(t, StartOpts{Home: home, Workspace: ws, Args: []string{"--no-session"}})
	tm.WaitText("Restore unsaved changes?", bootTimeout)
	tm.settle()
	tm.ClickText("Discard ") // not the test name in the workspace path
	tm.WaitGone("Restore unsaved changes?", uiTimeout)
	if left := backupFiles(home); len(left) > 0 {
		t.Errorf("backup left after Discard: %v", left)
	}
	if tm.Has("from a crash") {
		t.Errorf("discarded text shown")
	}
	tm.Quit()
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

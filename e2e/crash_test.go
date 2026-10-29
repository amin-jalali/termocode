//go:build e2e

package e2e

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// keyF12 is F12; with TERMOCODE_TEST_CRASH set it makes termocode panic
// (cmd/termocode/crash.go).
const keyF12 = "\x1b[24~"

// crashReports lists the crash reports under home.
func crashReports(home string) []string {
	m, _ := filepath.Glob(filepath.Join(home, ".local/state/termocode/crash/crash-*.txt"))
	return m
}

// TestCrashKeepsWorkAndRestoresTerminal: a panic in the UI loop ends
// termocode, but first the unsaved text goes to the hot-exit backup, a
// report without secrets is written, and the terminal is given back
// (alt screen, mouse, paste, kitty keyboard off, cursor on). The next
// start brings the text back at once and says what happened.
func TestCrashKeepsWorkAndRestoresTerminal(t *testing.T) {
	home := t.TempDir()
	ws := NewRepo(t)
	canary := "sk-" + "canary" + "0123456789abcdef"
	tm := Start(t, StartOpts{Home: home, Workspace: ws, Args: []string{"--no-session"}, KittyKeys: true,
		Env: []string{"TERMOCODE_TEST_CRASH=update", "OPENAI_API_KEY=" + canary}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send("ocrash-marker-5 " + canary)
	tm.WaitText("crash-marker-5", uiTimeout)
	tm.Send(KeyEsc)

	tm.Send(keyF12) // no wait for the debounced backup: the crash flushes it
	select {
	case <-tm.exited:
	case <-time.After(15 * time.Second):
		t.Fatalf("termocode did not end after the crash\n%s", tm.Screen())
	}
	select {
	case <-tm.readDone:
	case <-time.After(3 * time.Second):
	}
	var ee *exec.ExitError
	if !errors.As(tm.exitErr, &ee) || ee.ExitCode() == 0 {
		t.Errorf("exit = %v, want a non-zero status", tm.exitErr)
	}

	tm.mu.Lock()
	if tm.em.IsAltScreen() {
		t.Errorf("still on the alt screen after the crash")
	}
	if !tm.cursorVis {
		t.Errorf("cursor hidden after the crash")
	}
	for _, m := range []string{"?1000", "?1002", "?1003", "?1006", "?2004", "?1004"} {
		if tm.modes[m] {
			t.Errorf("mode %s still on after the crash", m)
		}
	}
	raw := tm.raw.String()
	tm.mu.Unlock()
	if !strings.Contains(raw, "\x1b[<u") {
		t.Errorf("kitty keyboard flags not popped after the crash")
	}
	if !strings.Contains(raw, "stopped unexpectedly") {
		t.Errorf("no message about the crash on the terminal")
	}

	reports := crashReports(home)
	if len(reports) != 1 {
		t.Fatalf("crash reports %v, want one", reports)
	}
	b, _ := os.ReadFile(reports[0])
	if info, _ := os.Stat(reports[0]); info == nil || info.Mode().Perm() != 0o600 {
		t.Errorf("report mode %v, want 0600", info.Mode().Perm())
	}
	for _, bad := range []string{canary, "crash-marker-5", home} {
		if strings.Contains(string(b), bad) {
			t.Errorf("report holds %q:\n%s", bad, b)
		}
	}
	if !strings.Contains(string(b), "test crash in update") {
		t.Errorf("report misses the panic:\n%s", b)
	}
	var backup string
	for _, f := range backupFiles(home) {
		bb, _ := os.ReadFile(f)
		backup += string(bb)
	}
	if !strings.Contains(backup, "crash-marker-5") {
		t.Fatalf("the hot-exit backup misses the unsaved text: %q", backup)
	}

	tm2 := Start(t, StartOpts{Home: home, Workspace: ws, Args: []string{"--no-session"}})
	tm2.WaitText("stopped unexpectedly", bootTimeout)
	tm2.WaitText("work was restored", uiTimeout)
	if tm2.Has("Restore unsaved changes?") {
		t.Errorf("asked to restore after a crash")
	}
	tm2.WaitText("crash-marker-5", uiTimeout)
	tm2.Send("\r") // Close (the focused button)
	tm2.WaitGone("stopped unexpectedly", uiTimeout)
	tm2.WaitFor("notes.txt tab, modified", uiTimeout, func(ls []string) bool {
		return strings.Contains(ls[0], "notes.txt ●")
	})
	tm2.quitDiscard()
}

// TestGoroutinePanicShowsToast: a panic in a background goroutine is
// recovered: termocode shows a toast and goes on (a clean quit after).
func TestGoroutinePanicShowsToast(t *testing.T) {
	home := t.TempDir()
	tm := Start(t, StartOpts{Home: home, Args: []string{"--no-session"},
		Env: []string{"TERMOCODE_TEST_CRASH=go"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.Send(keyF12)
	tm.WaitText("Something went wrong in the test worker", uiTimeout)
	tm.WaitText("Report saved", uiTimeout)
	if len(crashReports(home)) != 1 {
		t.Errorf("crash reports %v, want one", crashReports(home))
	}
	tm.Quit()
}

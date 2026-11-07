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

// SCN-20: tasks, the test runner and the debugger on the real binary:
// runs end when the user stops them, quits, or quits after a detach, and
// a debug session against a fake adapter stops, shows its frame and ends.

// procsWith lists the processes of home / ws whose command line holds sub.
func procsWith(home, ws, sub string) []string {
	var out []string
	for _, p := range procsOf(home, []string{home, ws}) {
		if strings.Contains(p, sub) {
			out = append(out, p)
		}
	}
	return out
}

// waitProcsGone waits until no process of home / ws has sub in its
// command line; it fails with those left after d.
func waitProcsGone(t *testing.T, home, ws, sub string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		left := procsWith(home, ws, sub)
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("still running %v later (%q):\n%s", d, sub, strings.Join(left, "\n"))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// scnRunsWorkspace writes the gated task and the gated test of SCN-3.
func scnRunsWorkspace(t *testing.T, ws string) {
	t.Helper()
	writeFile(t, filepath.Join(ws, "go.mod"), "module scn\n\ngo 1.21\n")
	writeFile(t, filepath.Join(ws, "gate_test.go"), scnGateTest)
	writeFile(t, filepath.Join(ws, ".termocode", "tasks.json"), scnTaskJSON)
}

// startTask runs slow-task from the palette and waits until it runs.
func startTask(t *testing.T, tm *Term, ws string) {
	t.Helper()
	tm.Palette("Tasks: Run Task...")
	tm.Type("slow-task")
	tm.settle()
	tm.Send(KeyEnter)
	waitFileHas(t, filepath.Join(ws, "task-runs.log"), "start", 20*time.Second)
	tm.WaitText("task-started-7", uiTimeout)
}

// Spec (SCN-20): Tasks: Terminate Task and Testing: Stop Test Run end the
// running task and test run: their processes go, and opening the gates
// afterwards finishes nothing (no "end" line). termocode keeps running.
func TestSCN20StopTaskAndTestRun(t *testing.T) {
	needProcFS(t)
	ws := NewRepo(t)
	scnRunsWorkspace(t, ws)
	tm := Start(t, StartOpts{Workspace: ws, Env: goTestEnv(t), Args: []string{"--no-session"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	taskLog := filepath.Join(ws, "task-runs.log")
	testLog := filepath.Join(ws, "test-runs.log")

	startTask(t, tm, ws)
	tm.Palette("Testing: Run All Tests")
	waitFileHas(t, testLog, "start", 90*time.Second)

	tm.Palette("Testing: Stop Test Run")
	waitProcsGone(t, tm.home, ws, "scn.test", 5*time.Second)
	tm.Palette("Tasks: Terminate Task")
	waitProcsGone(t, tm.home, ws, "task.gate", 5*time.Second)

	writeFile(t, filepath.Join(ws, "task.gate"), "")
	writeFile(t, filepath.Join(ws, "test.gate"), "")
	time.Sleep(time.Second) // a run that was not stopped would see its gate in 0.1 s
	if n := countLines(taskLog, "end"); n != 0 {
		t.Errorf("the task finished after Terminate Task (%d end lines)", n)
	}
	if n := countLines(testLog, "end"); n != 0 {
		t.Errorf("the test run finished after Stop Test Run (%d end lines)", n)
	}
	tm.settle()
	tm.CheckScreen("after stopping both", mainFrame)
	tm.Quit()
}

// Spec (SCN-20): quitting while a task runs in a terminal ends the task:
// nothing of it outlives termocode.
func TestSCN20TaskEndsOnQuit(t *testing.T) {
	needProcFS(t)
	ws := NewRepo(t)
	scnRunsWorkspace(t, ws)
	tm := Start(t, StartOpts{Workspace: ws, Args: []string{"--no-session"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	startTask(t, tm, ws)
	tm.Quit()
	if left := waitNoProcs(tm.home, []string{tm.home, ws}, 5*time.Second); len(left) > 0 {
		t.Fatalf("still running 5s after quit:\n%s", strings.Join(left, "\n"))
	}
	if n := countLines(filepath.Join(ws, "task-runs.log"), "end"); n != 0 {
		t.Errorf("the task finished by itself (%d end lines): the gate was never opened", n)
	}
}

// Spec (SCN-20): a detach keeps the task and the test run going (the
// session server owns them); a client that attaches again and quits ends
// the session, and with it both runs: no process is left.
func TestSCN20DetachThenQuitEndsRuns(t *testing.T) {
	needProcFS(t)
	r := newSessionRig(t)
	r.env = append(r.env, goTestEnv(t)...)
	scnRunsWorkspace(t, r.ws)

	tm := r.start()
	r.waitSocket(true)
	startTask(t, tm, r.ws)
	tm.Palette("Testing: Run All Tests")
	waitFileHas(t, filepath.Join(r.ws, "test-runs.log"), "start", 90*time.Second)

	tm.Palette("Session: Detach")
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		t.Fatalf("client still running after Session: Detach\n%s", tm.Screen())
	}
	time.Sleep(500 * time.Millisecond)
	if len(procsWith(r.home, r.ws, "task.gate")) == 0 {
		t.Fatal("the task ended on detach: the session should keep it running")
	}
	if len(procsWith(r.home, r.ws, "scn.test")) == 0 {
		t.Fatal("the test run ended on detach: the session should keep it running")
	}

	tm2 := r.start()
	tm2.WaitText("task-started-7", uiTimeout)
	tm2.Quit()
	r.waitSocket(false)
	if left := waitNoProcs(r.home, []string{r.home, r.ws}, 5*time.Second); len(left) > 0 {
		t.Fatalf("still running 5s after quit:\n%s", strings.Join(left, "\n"))
	}
	if n := countLines(filepath.Join(r.ws, "task-runs.log"), "start"); n != 1 {
		t.Errorf("the task started %d times, want 1", n)
	}
	if n := countLines(filepath.Join(r.ws, "test-runs.log"), "start"); n != 1 {
		t.Errorf("the tests ran %d times, want 1", n)
	}
}

// hostNvimDap is the nvim-dap checkout downloaded on this machine ("" when
// there is none), looked up in the real HOME before a test moves it.
func hostNvimDap() string {
	var roots []string
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		roots = append(roots, filepath.Join(v, "termocode", "plugins"))
	}
	if h, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(h, ".local", "share", "termocode", "plugins"))
	}
	for _, r := range roots {
		p := filepath.Join(r, "nvim-dap")
		if _, err := os.Stat(filepath.Join(p, "lua", "dap.lua")); err == nil {
			return p
		}
	}
	return ""
}

// buildFakeDlv builds testdata/fakedlv as dir/dlv.
func buildFakeDlv(t *testing.T, dir string) {
	t.Helper()
	out, err := exec.Command("go", "build", "-o", filepath.Join(dir, "dlv"), "./testdata/fakedlv").CombinedOutput()
	if err != nil {
		t.Fatalf("build fake dlv: %v\n%s", err, out)
	}
}

const scnDebugMain = `package main

func main() {
	answer := 42
	_ = answer
}
`

// Spec (SCN-20): Debug: Start / Continue runs the launch.json
// configuration through nvim-dap against a fake adapter (a fake `dlv dap`
// first on PATH). The session stops: the call stack shows the adapter's
// frame; Continue ends it with the program's output in the Debug Console;
// the adapter is gone; quitting leaves nothing.
func TestSCN20DebugFakeAdapter(t *testing.T) {
	needProcFS(t)
	dap := hostNvimDap()
	if dap == "" {
		t.Skip("nvim-dap is not downloaded on this machine (~/.local/share/termocode/plugins/nvim-dap)")
	}
	home := t.TempDir()
	plugins := filepath.Join(home, ".local", "share", "termocode", "plugins")
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-R", dap, filepath.Join(plugins, "nvim-dap")).CombinedOutput(); err != nil {
		t.Fatalf("copy nvim-dap: %v\n%s", err, out)
	}
	bin := t.TempDir()
	buildFakeDlv(t, bin)

	ws := NewRepo(t)
	writeFile(t, filepath.Join(ws, "go.mod"), "module scn\n\ngo 1.21\n")
	writeFile(t, filepath.Join(ws, "main.go"), scnDebugMain)
	writeFile(t, filepath.Join(ws, ".termocode", "launch.json"),
		`{"configurations": [{"name": "Fake Debug 20", "type": "go", "request": "launch", "program": "${workspaceFolder}"}]}`)
	dlvLog := filepath.Join(t.TempDir(), "dlv.log")
	env := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "FAKEDLV_LOG=" + dlvLog}

	tm := Start(t, StartOpts{Home: home, Workspace: ws, Env: env, Args: []string{"--no-session"}, Cols: 140, Rows: 40})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()

	tm.Palette("Debug: Start / Continue")
	waitFileHas(t, dlvLog, "configurationDone", 20*time.Second)
	waitFileHas(t, dlvLog, "stackTrace", 10*time.Second)
	tm.Palette("Debug: Show Call Stack")
	tm.WaitText("main.fakeFrame", uiTimeout)
	if len(procsWith(home, ws, "dlv dap")) == 0 {
		t.Fatal("no fake dlv running while the session is paused")
	}

	tm.Palette("Debug: Continue")
	waitFileHas(t, dlvLog, "continue", 10*time.Second)
	tm.WaitText("fake-program-done-20", uiTimeout)
	tm.WaitGone("main.fakeFrame", uiTimeout)
	waitProcsGone(t, home, ws, "dlv dap", 10*time.Second)
	tm.settle()
	tm.CheckScreen("after the session ended", mainFrame)
	tm.Quit()
	if left := waitNoProcs(home, []string{home, ws}, 5*time.Second); len(left) > 0 {
		t.Fatalf("still running 5s after quit:\n%s", strings.Join(left, "\n"))
	}
}

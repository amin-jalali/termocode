//go:build unix

package nvim

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startInSystem makes nvim run a blocking vim.fn.system() (as the git
// signs did) and returns the pids of nvim and of the system() child once
// it runs.
func startInSystem(t *testing.T, c *Client) (nvimPid, childPid int) {
	t.Helper()
	s, err := c.EvalLuaString(`return tostring(vim.fn.getpid())`)
	if err != nil {
		t.Fatalf("pid: %v", err)
	}
	nvimPid, _ = strconv.Atoi(s)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	lua := fmt.Sprintf(`vim.defer_fn(function()
  vim.fn.system({ 'sh', '-c', 'echo $$ > %s.tmp && mv %s.tmp %s && exec sleep 30' })
end, 1)`, pidFile, pidFile, pidFile)
	if err := c.ExecLua(lua); err != nil {
		t.Fatalf("start system(): %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, err := os.ReadFile(pidFile); err == nil {
			childPid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			return nvimPid, childPid
		}
		if time.Now().After(deadline) {
			t.Fatal("the system() child did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitGone waits until none of pids runs. A zombie counts as gone: its
// reaper (init) may be slow.
func waitGone(pids []int, timeout time.Duration) []int {
	deadline := time.Now().Add(timeout)
	for {
		var left []int
		for _, p := range pids {
			if running(p) {
				left = append(left, p)
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			return left
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func running(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}

// EOF on the embed pipe while nvim waits in system() sends nvim into an
// exit that never ends (nvim 0.12: it keeps running its loop with
// v:exiting set and ignores SIGTERM). Close then waited for its 10 s
// kill, which froze Quit; the watchdog ends such an exit quickly, and
// with it the system() child.
func TestCloseDuringSystemCall(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	nvimPid, child := startInSystem(t, c)
	start := time.Now()
	_ = c.Close()
	if d := time.Since(start); d > 7*time.Second {
		t.Errorf("Close took %v while nvim was in system()", d.Round(time.Millisecond))
	}
	if left := waitGone([]int{nvimPid, child}, 3*time.Second); len(left) > 0 {
		t.Errorf("still running after Close: %v (nvim %d, system() child %d)", left, nvimPid, child)
		for _, p := range left {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
	}
}

// Close of an idle nvim stays quick: the watchdog timer must not hold
// nvim's exit.
func TestCloseIdleIsQuick(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	start := time.Now()
	_ = c.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Close of an idle nvim took %v", d.Round(time.Millisecond))
	}
}

// helperEnv makes the test binary act as termocode: start nvim, put it in
// system(), print the pids and wait to be killed.
const helperEnv = "TERMOCODE_NVIM_TEST_HELPER"

func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("helper process only")
	}
	c, err := New()
	if err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
	if err := c.Attach(80, 24); err != nil {
		fmt.Println("error", err)
		os.Exit(1)
	}
	nvimPid, child := startInSystem(t, c)
	fmt.Printf("pids %d %d\n", nvimPid, child)
	select {}
}

// nvim (and what it runs) must never outlive termocode, even when
// termocode dies hard and nvim cannot act on the EOF of its pipe (it is
// in system(), see TestCloseDuringSystemCall).
func TestNvimDiesWithParent(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var nvimPid, child int
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if _, err := fmt.Sscanf(sc.Text(), "pids %d %d", &nvimPid, &child); err == nil {
			break
		}
	}
	if nvimPid == 0 {
		t.Fatal("the helper printed no pids")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if left := waitGone([]int{nvimPid, child}, 6*time.Second); len(left) > 0 {
		t.Errorf("still running 6s after termocode was killed: %v (nvim %d, system() child %d)", left, nvimPid, child)
		for _, p := range left {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
	}
}

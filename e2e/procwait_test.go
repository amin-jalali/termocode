//go:build e2e

package e2e

import (
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// descendants lists every process under pid (children, their children
// …), read from ps, so it works on macOS too.
func descendants(pid int) []int {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	kids := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		c, err1 := strconv.Atoi(f[0])
		p, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			kids[p] = append(kids[p], c)
		}
	}
	var all []int
	for todo := kids[pid]; len(todo) > 0; {
		c := todo[0]
		todo = todo[1:]
		all = append(all, c)
		todo = append(todo, kids[c]...)
	}
	return all
}

// waitGone waits until none of pids runs and returns those still running
// after timeout.
func waitGone(pids []int, timeout time.Duration) []int {
	deadline := time.Now().Add(timeout)
	for {
		var left []int
		for _, p := range pids {
			if err := syscall.Kill(p, 0); !errors.Is(err, syscall.ESRCH) {
				left = append(left, p)
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			return left
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A child that outlives its killed parent is found before the kill and
// waited for: waitGone names it while it runs and nothing once it ended.
func TestDescendantsOutliveTheirParent(t *testing.T) {
	sh := exec.Command("sh", "-c", "sleep 30 & wait")
	if err := sh.Start(); err != nil {
		t.Fatal(err)
	}
	var kids []int
	deadline := time.Now().Add(10 * time.Second)
	for len(kids) == 0 {
		if kids = descendants(sh.Process.Pid); len(kids) == 0 && time.Now().After(deadline) {
			t.Fatal("the sleep under sh was not found")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = sh.Process.Kill()
	_ = sh.Wait()
	left := waitGone(kids, 200*time.Millisecond)
	if !slices.Equal(left, kids) {
		t.Fatalf("still running %v, want the orphan %v", left, kids)
	}
	for _, p := range left {
		_ = syscall.Kill(p, syscall.SIGKILL)
	}
	if left := waitGone(kids, 10*time.Second); len(left) > 0 {
		t.Fatalf("still running after the kill: %v", left)
	}
}

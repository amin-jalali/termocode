//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// leakWait is how long processes of a finished test may take to go.
const leakWait = 5 * time.Second

var leakWatched sync.Map // home → true

// watchLeaks fails the test if, once it is over, a process still runs
// that belongs to it: its HOME is home, or its working directory lies in
// home or one of dirs (termocode, its session server, nvim and all they
// start inherit both). Leftovers are killed, so one leak does not spill
// into later tests. Call it before the cleanups that end the test's
// processes: cleanups run last-in first-out, so this check runs after
// them. Once per home is enough.
func watchLeaks(t *testing.T, home string, dirs ...string) {
	t.Helper()
	if _, loaded := leakWatched.LoadOrStore(home, true); loaded {
		return
	}
	dirs = append([]string{home}, dirs...)
	t.Cleanup(func() {
		left := waitNoProcs(home, dirs, leakWait)
		if len(left) == 0 {
			return
		}
		t.Errorf("processes of this test still running %v after it ended:\n%s", leakWait, strings.Join(left, "\n"))
		for _, p := range left {
			if pid, err := strconv.Atoi(strings.Fields(p)[0]); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

// waitNoProcs waits until no process of home / dirs runs and returns
// those still running after timeout ("pid args" each).
func waitNoProcs(home string, dirs []string, timeout time.Duration) []string {
	deadline := time.Now().Add(timeout)
	for {
		left := procsOf(home, dirs)
		if len(left) == 0 || time.Now().After(deadline) {
			return left
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// procsOf lists the live processes (not zombies) whose HOME is home or
// whose working directory is in one of dirs. It reads /proc, so it finds
// nothing outside Linux (the e2e CI job runs on Linux).
func procsOf(home string, dirs []string) []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var out []string
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		base := filepath.Join("/proc", e.Name())
		if st, err := os.ReadFile(filepath.Join(base, "stat")); err != nil || zombie(st) {
			continue
		}
		if !ownsHome(base, home) && !cwdIn(base, dirs) {
			continue
		}
		args, _ := os.ReadFile(filepath.Join(base, "cmdline"))
		args = bytes.TrimRight(bytes.ReplaceAll(args, []byte{0}, []byte{' '}), " ")
		out = append(out, fmt.Sprintf("%d %s", pid, args))
	}
	return out
}

// zombie reads the state field of /proc/<pid>/stat ("pid (comm) S ...").
func zombie(stat []byte) bool {
	i := bytes.LastIndexByte(stat, ')')
	return i >= 0 && i+2 < len(stat) && stat[i+2] == 'Z'
}

func ownsHome(base, home string) bool {
	env, err := os.ReadFile(filepath.Join(base, "environ"))
	if err != nil {
		return false
	}
	for _, kv := range bytes.Split(env, []byte{0}) {
		if string(kv) == "HOME="+home {
			return true
		}
	}
	return false
}

func cwdIn(base string, dirs []string) bool {
	cwd, err := os.Readlink(filepath.Join(base, "cwd"))
	if err != nil {
		return false
	}
	cwd = strings.TrimSuffix(cwd, " (deleted)")
	for _, d := range dirs {
		if cwd == d || strings.HasPrefix(cwd, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

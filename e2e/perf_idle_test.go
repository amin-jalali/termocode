//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// procCPU is the CPU time pid has used so far: the sum over its threads
// of /proc/<pid>/task/<tid>/schedstat (nanoseconds on a CPU; /proc/stat
// counts 10 ms ticks, too coarse for an idle process), plus the CPU of
// its children that already ended.
func procCPU(pid int) time.Duration {
	tasks, _ := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	var d time.Duration
	for _, t := range tasks {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/schedstat", pid, t.Name()))
		if err != nil {
			continue
		}
		if f := strings.Fields(string(b)); len(f) > 0 {
			ns, _ := strconv.ParseInt(f[0], 10, 64)
			d += time.Duration(ns)
		}
	}
	return d + procChildCPU(pid)
}

// procChildCPU is the CPU time of pid's children that ended and were
// waited for (cutime + cstime in /proc/<pid>/stat, 10 ms ticks): the
// short git, gh, … runs a poll starts.
func procChildCPU(pid int) time.Duration {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(f) < 15 {
		return 0
	}
	// After "(comm)": state(0) ppid … utime(11) stime(12) cutime(13) cstime(14).
	cu, _ := strconv.Atoi(f[13])
	cs, _ := strconv.Atoi(f[14])
	return time.Duration(cu+cs) * 10 * time.Millisecond
}

// procWakeups is how often the threads of pid were switched in so far
// (voluntary + involuntary context switches): one per timer, poll or
// read that woke them.
func procWakeups(pid int) int {
	tasks, _ := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	n := 0
	for _, t := range tasks {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/status", pid, t.Name()))
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "voluntary_ctxt_switches:") || strings.HasPrefix(l, "nonvoluntary_ctxt_switches:") {
				if f := strings.Fields(l); len(f) == 2 {
					k, _ := strconv.Atoi(f[1])
					n += k
				}
			}
		}
	}
	return n
}

// sessionServerPID is the pid of the session server of the rig's
// workspace.
func sessionServerPID(t *testing.T, r *sessionRig) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.runtime, "termocode", hashKey(r.ws)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(b, &info); err != nil || info.PID == 0 {
		t.Fatalf("session info %q: %v", b, err)
	}
	return info.PID
}

// idleWindow is how long the idle guards watch (TERMOCODE_PERF_IDLE_SECS,
// default 60 s).
func idleWindow() time.Duration {
	if n, err := strconv.Atoi(os.Getenv("TERMOCODE_PERF_IDLE_SECS")); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 60 * time.Second
}

// Idle CPU of one session: the client, the session server and all under
// it (UI, nvim, shell) in a git workspace with a text file open, while
// nobody types for a minute. Reports the CPU time used per minute and
// the wakeups per second (timers and polls that run while nothing
// changes).
func TestPerfSessionIdleCPU(t *testing.T) {
	perfRequire(t)
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	r := newSessionRig(t)
	tm := r.start()
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.settle()
	server := sessionServerPID(t, r)
	// Let start-up work (language checks, first git status, …) finish.
	tm.WaitStable(2*time.Second, uiTimeout)
	pids := func() []int { return append([]int{tm.cmd.Process.Pid}, procTree(server)...) }
	type use struct {
		cpu  time.Duration
		wake int
	}
	sample := func() (time.Duration, int, map[string]use) {
		var cpu time.Duration
		wake := 0
		per := map[string]use{}
		for _, pid := range pids() {
			c, w := procCPU(pid), procWakeups(pid)
			cpu += c
			wake += w
			name := fmt.Sprintf("%s[%d]", procName(pid), pid)
			per[name] = use{c, w}
		}
		return cpu, wake, per
	}
	win := idleWindow()
	cpu0, wake0, per0 := sample()
	t0 := time.Now()
	time.Sleep(win) // idle on purpose: nothing may be sent for the whole window
	cpu1, wake1, per1 := sample()
	dt := time.Since(t0)
	var parts []string
	for name, u := range per1 {
		parts = append(parts, fmt.Sprintf("%s %v/%d", name, (u.cpu-per0[name].cpu).Round(time.Millisecond), u.wake-per0[name].wake))
	}
	perMin := float64((cpu1 - cpu0).Milliseconds()) * float64(time.Minute) / float64(dt)
	wakePerSec := float64(wake1-wake0) / dt.Seconds()
	t.Logf("idle %v: CPU %v, %d wakeups (per process cpu/wakeups: %s)", dt.Round(time.Second), (cpu1 - cpu0).Round(time.Millisecond), wake1-wake0, strings.Join(parts, ", "))
	perfCheck(t, "session_idle_cpu", float64(int(perMin+0.5)))
	perfCheck(t, "session_idle_wakeups", float64(int(wakePerSec*10+0.5))/10)
	tm.Quit()
}

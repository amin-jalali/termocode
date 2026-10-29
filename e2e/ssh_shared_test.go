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

// Spec (Group S5): on a server shared by many users, what a session
// leaves on disk is the user's only: the socket folder is 0700 and every
// file in it (socket, info, log, caps) and in termocode's config folder
// is 0600. The idle memory of one session is logged (go test -v).
func TestSSHSessionPrivateAndRSS(t *testing.T) {
	r := newSessionRig(t)
	tm := r.start()
	tm.OpenFile("main.go")
	tm.WaitText("func main()", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	// Something in the state files: a recent file, an error-free log.
	time.Sleep(time.Second) // the session writer runs on a timer

	sockDir := filepath.Join(r.runtime, "termocode")
	fi, err := os.Stat(sockDir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("%s has mode %o, want 700", sockDir, fi.Mode().Perm())
	}
	for _, dir := range []string{sockDir, filepath.Join(r.home, ".config", "termocode")} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			info, err := e.Info()
			if err != nil || e.IsDir() {
				continue
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s has mode %o, want 600", filepath.Join(dir, e.Name()), info.Mode().Perm())
			}
		}
	}

	// Idle memory: the client, the session server and everything under
	// the server (the UI, nvim, language servers, the shell).
	b, err := os.ReadFile(filepath.Join(sockDir, hashKey(r.ws)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(b, &info); err != nil || info.PID == 0 {
		t.Fatalf("session info %q: %v", b, err)
	}
	if runtime.GOOS != "linux" {
		t.Log("idle memory not logged: it reads /proc (Linux only)")
		tm.Quit()
		return
	}
	time.Sleep(2 * time.Second) // idle
	var parts []string
	total := 0
	for _, pid := range append([]int{tm.cmd.Process.Pid}, procTree(info.PID)...) {
		kb := rssKB(pid)
		total += kb
		parts = append(parts, fmt.Sprintf("%s %d MB", procName(pid), kb/1024))
	}
	t.Logf("idle RSS of one session: %d MB (%s)", total/1024, strings.Join(parts, ", "))
	tm.Quit()
}

// procTree returns pid and all its descendants (Linux /proc).
func procTree(pid int) []int {
	children := map[int][]int{}
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		p, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// pid (comm) state ppid ...
		s := string(b)
		i := strings.LastIndexByte(s, ')')
		f := strings.Fields(s[i+1:])
		if len(f) < 2 {
			continue
		}
		pp, _ := strconv.Atoi(f[1])
		children[pp] = append(children[pp], p)
	}
	out := []int{pid}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i]]...)
	}
	return out
}

// rssKB is the resident memory of pid in KiB (0 when gone).
func rssKB(pid int) int {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				n, _ := strconv.Atoi(f[1])
				return n
			}
		}
	}
	return 0
}

func procName(pid int) string {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	return strings.TrimSpace(string(b))
}

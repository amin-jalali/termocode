package git

import (
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// silentListener accepts connections and never sends a byte (a network
// that swallows the answer). Closed at the end of the test.
func silentListener(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return l.Addr().String()
}

// Spec (SCN-13): a fetch from a server that accepts the connection and
// then says nothing gives up with a reason instead of waiting forever
// (the Source Control view allows one remote operation at a time, so a
// hung one blocked every later push and fetch). The test shortens the
// time through git's own variable; termocode adds the speed limit.
func TestFetchGivesUpOnSilentRemote(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("remote", "add", "origin", "http://"+silentListener(t)+"/repo.git")
	t.Setenv("GIT_HTTP_LOW_SPEED_TIME", "2")
	t.Setenv("GIT_HTTP_LOW_SPEED_LIMIT", "")

	done := make(chan error, 1)
	go func() { done <- Fetch(dir) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "too slow") {
			t.Fatalf("want a too-slow error, got %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("git fetch still waits for a silent server after 30 s")
	}
}

// Spec: remote commands stop a transfer that stays below 1 byte/s for a
// minute; a value the user set wins.
func TestRemoteEnvLowSpeed(t *testing.T) {
	env := strings.Join(cloneEnv([]string{"PATH=/bin"}, "", "", ""), "\n")
	for _, kv := range []string{"GIT_HTTP_LOW_SPEED_LIMIT=1", "GIT_HTTP_LOW_SPEED_TIME=60"} {
		if !strings.Contains(env, kv) {
			t.Errorf("missing %s in\n%s", kv, env)
		}
	}
	env = strings.Join(cloneEnv([]string{"GIT_HTTP_LOW_SPEED_TIME=300", "GIT_HTTP_LOW_SPEED_LIMIT=1000"}, "", "", ""), "\n")
	if strings.Contains(env, "GIT_HTTP_LOW_SPEED_TIME=60") || strings.Contains(env, "GIT_HTTP_LOW_SPEED_LIMIT=1\n") ||
		!strings.Contains(env, "GIT_HTTP_LOW_SPEED_TIME=300") || !strings.Contains(env, "GIT_HTTP_LOW_SPEED_LIMIT=1000") {
		t.Errorf("the user's values must win:\n%s", env)
	}
}

// Spec (SCN-13): the default ssh command gives up on a host that accepts
// the connection and never answers (ssh waits for its greeting forever
// without a connect timeout) and on a link that dies during a transfer.
// A GIT_SSH_COMMAND of the user is kept as it is.
func TestRemoteEnvSSHTimeouts(t *testing.T) {
	env := strings.Join(cloneEnv([]string{"PATH=/bin"}, "", "", ""), "\n")
	want := "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=30 -o ServerAliveInterval=15 -o ServerAliveCountMax=4"
	if !strings.Contains(env, want) {
		t.Errorf("want %q in\n%s", want, env)
	}
	env = strings.Join(cloneEnv([]string{"GIT_SSH_COMMAND=ssh -i key"}, "", "", ""), "\n")
	if !strings.Contains(env, "GIT_SSH_COMMAND=ssh -i key") || strings.Contains(env, "ConnectTimeout") {
		t.Errorf("the user's ssh command must be kept:\n%s", env)
	}
}

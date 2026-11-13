package bench

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// RemoteCfg is the SSH host the remote measures use. The host is given by
// the person running the bench (BENCH_SSH_HOST=user@host, BENCH_SSH_PORT);
// auth is by key or agent only, never a password in argv.
type RemoteCfg struct {
	Host string
	User string
	Port int
}

// DetectRemote reads the config, or says why remote measures are skipped.
func DetectRemote() (*RemoteCfg, string) {
	h := os.Getenv("BENCH_SSH_HOST")
	if h == "" {
		return nil, "remote measures skipped: set BENCH_SSH_HOST=user@host (key auth) to run them"
	}
	if _, err := lookPath("ssh"); err != nil {
		return nil, "remote measures skipped: no ssh client installed"
	}
	c := &RemoteCfg{Host: h, Port: 22}
	if u, host, ok := strings.Cut(h, "@"); ok {
		c.User, c.Host = u, host
	}
	if p, err := strconv.Atoi(os.Getenv("BENCH_SSH_PORT")); err == nil && p > 0 {
		c.Port = p
	}
	return c, ""
}

// SSHArgs is the ssh command line that runs remoteCmd in a terminal on the
// remote host (key auth only; fails fast instead of asking for a password).
func SSHArgs(c RemoteCfg, remoteCmd ...string) []string {
	target := c.Host
	if c.User != "" {
		target = c.User + "@" + c.Host
	}
	a := []string{"-tt", "-o", "BatchMode=yes", "-o", "ServerAliveInterval=2", "-p", strconv.Itoa(c.Port), target}
	return append(a, remoteCmd...)
}

// BaselineCmd wraps a command in a multiplexer that keeps it alive over a
// drop, the "keep alive" baseline the editors are compared with.
func BaselineCmd(mux, session string, cmd ...string) []string {
	switch mux {
	case "tmux":
		return append([]string{"tmux", "new-session", "-A", "-s", session}, cmd...)
	case "zellij":
		return append([]string{"zellij", "attach", "--create", session}, cmd...)
	}
	return nil
}

// BytesPerKey sends one key and returns the bytes the remote sent back for
// it (the whole screen update) and the time until want shows.
func BytesPerKey(s *Session, key, want string, timeout time.Duration) (int, time.Duration, error) {
	mark := s.Mark()
	d, _, err := s.TimeKey([]byte(key), want, timeout)
	if err != nil {
		return 0, d, err
	}
	return s.Settle(mark, 100*time.Millisecond, 2*time.Second), d, nil
}

// Reconnect cuts the link with drop and times until want shows again. If the
// program survives and reconnects by itself, want must show in the same
// session. If it exits, relaunch starts it again (nil relaunch: error).
// The returned session is the one that shows want.
func Reconnect(s *Session, drop func(), relaunch func() (*Session, error), want string, timeout time.Duration) (time.Duration, *Session, error) {
	mark := s.Mark()
	start := time.Now()
	drop()
	cur, curMark, relaunched := s, mark, false
	for time.Since(start) < timeout {
		select {
		case <-cur.exitc:
			if relaunch == nil {
				return time.Since(start), cur, errors.New("program exited after the drop and no relaunch is set")
			}
			if relaunched {
				return time.Since(start), cur, errors.New("program exited again after the relaunch")
			}
			ns, err := relaunch()
			if err != nil {
				return time.Since(start), cur, err
			}
			cur, curMark, relaunched = ns, 0, true
		default:
		}
		cur.mu.Lock()
		var tail string
		if curMark <= len(cur.buf) {
			tail = string(cur.buf[curMark:])
		}
		cur.mu.Unlock()
		if strings.Contains(tail, want) {
			return time.Since(start), cur, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return time.Since(start), cur, errors.New("did not come back before the timeout")
}

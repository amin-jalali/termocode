package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// initTimeout caps the handshake of a fresh server (npx downloads can be
// slow, but a hung server must not hang the chat turn).
const initTimeout = 15 * time.Second

// Process is a connected server and its Client: a child process (stdio)
// or a remote session (HTTP, cmd nil).
type Process struct {
	Client *Client

	cmd       *exec.Cmd
	done      chan struct{} // closed when the process exited / the session ended
	closeOnce sync.Once
	closeFn   func() // HTTP: end the session; shared: leave the broker

	// shared: a server the share broker runs for many sessions (cmd nil);
	// pid and sessions come from the broker.
	shared    bool
	sharedPid atomic.Int64
	sessions  atomic.Int64
}

// Pid is the server's process id (0 for a remote server).
func (p *Process) Pid() int {
	if p.shared {
		return int(p.sharedPid.Load())
	}
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Done is closed when the process has exited.
func (p *Process) Done() <-chan struct{} { return p.done }

// StartStdio starts s (already expanded) as a child process in its own
// process group with a minimal environment, and runs the handshake.
func StartStdio(ctx context.Context, s Server, opts Options) (*Process, error) {
	if s.Command == "" {
		return nil, errors.New("no command")
	}
	path, err := exec.LookPath(s.Command)
	if err != nil {
		return nil, fmt.Errorf("%s not found (install it, or set command to a full path)", s.Command)
	}
	cmd := spawn.Command(path, s.Args...)
	cmd.Dir = s.Cwd
	cmd.Env = childEnv(os.Environ(), s.Env)
	setProcGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", s.Command, err)
	}
	opts.log("started %s (pid %d)", s.Command, cmd.Process.Pid)
	p := &Process{cmd: cmd, done: make(chan struct{})}
	crash.Go("an MCP server log", func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			opts.log("stderr: %s", sc.Text())
		}
	})
	crash.Go("an MCP server", func() {
		err := cmd.Wait()
		killGroup(cmd.Process.Pid) // children it left behind
		if err != nil {
			opts.log("exited: %v", err)
		} else {
			opts.log("exited")
		}
		close(p.done)
	})
	p.Client = NewClient(stdout, stdin, opts)

	ictx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	if err := p.Client.Initialize(ictx); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// Close ends the server: stdin is closed (the polite stop), then the
// process group gets SIGTERM after 1 s and SIGKILL after 2 s.
func (p *Process) Close() {
	p.closeOnce.Do(func() {
		if p.cmd == nil {
			if p.closeFn != nil {
				p.closeFn()
			}
			<-p.done
			return
		}
		p.Client.Close()
		pid := p.Pid()
		select {
		case <-p.done:
			return
		case <-time.After(time.Second):
		}
		termGroup(pid)
		select {
		case <-p.done:
			return
		case <-time.After(time.Second):
		}
		killGroup(pid)
		<-p.done
	})
}

// childEnv is the environment of a server: a few basics from ours plus
// the entry's env — never our whole environment, which can hold API keys
// (ANTHROPIC_API_KEY, TERMOCODE_AI_API_KEY …).
func childEnv(environ []string, extra map[string]string) []string {
	keep := func(k string) bool {
		switch k {
		case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "TERM", "TMPDIR", "TZ":
			return true
		}
		return strings.HasPrefix(k, "LC_") || strings.HasPrefix(k, "XDG_")
	}
	var out []string
	for _, kv := range environ {
		k, _, ok := strings.Cut(kv, "=")
		if ok && keep(k) && extra[k] == "" {
			out = append(out, kv)
		}
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

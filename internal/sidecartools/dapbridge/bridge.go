// Package dapbridge is tc-dap-bridge: it puts a debug adapter that only
// listens on TCP (dlv dap -l 127.0.0.1:0) on stdin/stdout, so nvim-dap
// can start it as an "executable" adapter through `docker exec -i` in
// the tools sidecar (plan RC, phase 6). Nothing is listening outside the
// sidecar's (the target's) loopback.
package dapbridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// addrLine is the adapter's "listening at" line (dlv: "DAP server
// listening at: 127.0.0.1:41234").
var addrLine = regexp.MustCompile(`listening at:?\s+(\S+:\d+)`)

// startTimeout bounds the wait for the address line.
var startTimeout = 30 * time.Second

// Run starts argv, relays in → adapter and adapter → out, and returns
// when either side ends. The adapter's stdout and stderr go to errw.
func Run(ctx context.Context, argv []string, in io.Reader, out, errw io.Writer) error {
	if len(argv) == 0 {
		return errors.New("tc-dap-bridge: give the adapter command after --")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errw = &lockedWriter{w: errw}
	cmd := spawn.CommandContext(ctx, argv[0], argv[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = errw
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("tc-dap-bridge: cannot start %s: %w", argv[0], err)
	}
	waited := make(chan error, 1)
	addrc := make(chan string, 1)
	crash.Go("dapbridge-adapter-output", func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		sent := false
		for sc.Scan() {
			line := sc.Text()
			if !sent {
				if m := addrLine.FindStringSubmatch(line); m != nil {
					addrc <- m[1]
					sent = true
					continue
				}
			}
			fmt.Fprintln(errw, line)
		}
		if !sent {
			close(addrc)
		}
		waited <- cmd.Wait()
	})
	// Every return waits for the adapter, so no output comes after Run.
	defer func() {
		cancel()
		select {
		case <-waited:
		case <-time.After(5 * time.Second):
		}
	}()
	var addr string
	select {
	case a, ok := <-addrc:
		if !ok {
			return fmt.Errorf("tc-dap-bridge: %s ended without printing its address", argv[0])
		}
		addr = a
	case <-time.After(startTimeout):
		return fmt.Errorf("tc-dap-bridge: %s printed no address within %s", argv[0], startTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("tc-dap-bridge: cannot reach the adapter at %s: %w", addr, err)
	}
	defer conn.Close()
	done := make(chan struct{}, 2)
	crash.Go("dapbridge-to-adapter", func() {
		_, _ = io.Copy(conn, in)
		done <- struct{}{}
	})
	crash.Go("dapbridge-from-adapter", func() {
		_, _ = io.Copy(out, conn)
		done <- struct{}{}
	})
	<-done // the client left or the adapter closed: end both
	cancel()
	_ = conn.Close()
	return nil
}

// lockedWriter lets the adapter's stdout and stderr share one writer.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

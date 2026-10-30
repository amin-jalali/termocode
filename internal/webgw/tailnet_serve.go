//go:build unix

package webgw

import (
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// serveStopWait is how long Serve may take to end after SIGINT before it
// is killed.
const serveStopWait = 3 * time.Second

// StartTailscaleServe runs `tailscale serve --https=443 <target>` in the
// foreground, in its own process group (terminal signals stay with the
// UI). Without --bg its config is removed when it ends, so the panel's
// https name exists only while the gateway runs.
func StartTailscaleServe(target string) (ServeProc, error) {
	bin, err := tailscaleBin()
	if err != nil {
		return nil, err
	}
	cmd := spawn.Command(bin, "serve", "--https="+strconv.Itoa(TailnetPort), target)
	out := &tailBuf{max: 4 << 10}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &serveProc{done: make(chan struct{}), out: out}
	p.signal = func(s syscall.Signal) { _ = cmd.Process.Signal(s) }
	crash.Go("web gateway: tailscale serve", func() {
		_ = cmd.Wait()
		close(p.done)
	})
	return p, nil
}

type serveProc struct {
	signal func(syscall.Signal)
	done   chan struct{}
	out    *tailBuf
	once   sync.Once
}

func (p *serveProc) Stop() {
	p.once.Do(func() {
		p.signal(syscall.SIGINT)
		select {
		case <-p.done:
		case <-time.After(serveStopWait):
			p.signal(syscall.SIGKILL)
			<-p.done
		}
	})
}

func (p *serveProc) Done() <-chan struct{} { return p.done }

func (p *serveProc) Reason() string { return lastLine(p.out.String(), errServeEnded) }

// tailBuf keeps the last max bytes written to it.
type tailBuf struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (w *tailBuf) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.b = append(w.b, p...)
	if over := len(w.b) - w.max; over > 0 {
		w.b = append(w.b[:0], w.b[over:]...)
	}
	return len(p), nil
}

func (w *tailBuf) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.b)
}

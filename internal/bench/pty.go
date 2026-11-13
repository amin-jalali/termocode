// Package bench measures termocode against other editors: a PTY runner for
// terminal programs, fixtures, per-editor scripts, a process collector and a
// report generator. It is a dev tool (cmd/bench), never part of the product.
package bench

import (
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
)

// Spec says what to start in the PTY.
type Spec struct {
	Bin        string
	Args       []string
	Dir        string
	Env        []string // added to the environment (KEY=VALUE)
	Rows, Cols uint16   // default 40x120
}

// Session is a program running in a PTY. All output is kept with its size
// so tests can ask "how long until this text" and "how many bytes".
type Session struct {
	mu    sync.Mutex
	buf   []byte
	cond  chan struct{} // closed and replaced on every new output
	ptmx  *os.File
	cmd   interface{ Wait() error }
	pid   int
	wg    sync.WaitGroup
	once  sync.Once
	exitc chan struct{}
}

// Start runs spec in a new PTY session.
func Start(spec Spec) (*Session, error) {
	cmd := spawn.Command(spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Env = append(cmd.Env, spec.Env...)
	rows, cols := spec.Rows, spec.Cols
	if rows == 0 {
		rows = 40
	}
	if cols == 0 {
		cols = 120
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}
	s := &Session{ptmx: ptmx, cmd: cmd, pid: cmd.Process.Pid, cond: make(chan struct{}), exitc: make(chan struct{})}
	s.wg.Go(func() {
		tmp := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(tmp)
			if n > 0 {
				s.mu.Lock()
				s.buf = append(s.buf, tmp[:n]...)
				old := s.cond
				s.cond = make(chan struct{})
				s.mu.Unlock()
				close(old)
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					_ = err // EIO when the child exits: normal
				}
				close(s.exitc)
				return
			}
		}
	})
	return s, nil
}

// Pid is the child's process id (the root of its process tree).
func (s *Session) Pid() int { return s.pid }

// Send writes keys to the program.
func (s *Session) Send(b []byte) error {
	_, err := s.ptmx.Write(b)
	return err
}

// BytesOut is the total number of bytes the program has written so far.
func (s *Session) BytesOut() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}

// Text is everything written so far, without escape sequences.
func (s *Session) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ansi.Strip(string(s.buf))
}

// Mark is the current output offset; pass it to WaitAfter.
func (s *Session) Mark() int { return s.BytesOut() }

// WaitAfter waits until match is true for the text written after mark. It
// returns the time since the call.
func (s *Session) WaitAfter(mark int, match func(text string) bool, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	deadline := time.After(timeout)
	for {
		s.mu.Lock()
		var tail string
		if mark <= len(s.buf) {
			tail = ansi.Strip(string(s.buf[mark:]))
		}
		c := s.cond
		s.mu.Unlock()
		if match(tail) {
			return time.Since(start), nil
		}
		select {
		case <-c:
		case <-s.exitc:
			// last look at the final output
			s.mu.Lock()
			tail = ansi.Strip(string(s.buf[min(mark, len(s.buf)):]))
			s.mu.Unlock()
			if match(tail) {
				return time.Since(start), nil
			}
			return time.Since(start), errors.New("program exited before the screen matched")
		case <-deadline:
			return time.Since(start), errors.New("timeout waiting for the screen")
		}
	}
}

// WaitForText waits for text anywhere in the output so far or later.
func (s *Session) WaitForText(text string, timeout time.Duration) (time.Duration, error) {
	return s.WaitAfter(0, func(t string) bool { return strings.Contains(t, text) }, timeout)
}

// TimeKey sends key and returns the time until want shows in the output
// after it, plus the bytes the program wrote in the meantime (a frame size).
func (s *Session) TimeKey(key []byte, want string, timeout time.Duration) (time.Duration, int, error) {
	mark := s.Mark()
	start := time.Now()
	if err := s.Send(key); err != nil {
		return 0, 0, err
	}
	_, err := s.WaitAfter(mark, func(t string) bool { return strings.Contains(t, want) }, timeout)
	d := time.Since(start)
	return d, s.BytesOut() - mark, err
}

// Settle waits until the program has written nothing for quiet (or timeout)
// and returns the bytes written since mark: the cost of one screen update.
func (s *Session) Settle(mark int, quiet, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	last := s.BytesOut()
	lastChange := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(quiet / 4)
		if n := s.BytesOut(); n != last {
			last, lastChange = n, time.Now()
		} else if time.Since(lastChange) >= quiet {
			break
		}
	}
	return last - mark
}

// Close kills the program and its process group, and frees the PTY.
func (s *Session) Close() {
	s.once.Do(func() {
		_ = syscall.Kill(-s.pid, syscall.SIGKILL)
		_ = syscall.Kill(s.pid, syscall.SIGKILL)
		_ = s.ptmx.Close()
		_ = s.cmd.Wait()
		s.wg.Wait()
	})
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// Stats is a summary of repeated timings.
type Stats struct {
	N             int
	Min, Max      time.Duration
	P50, P95, Avg time.Duration
}

// Summarize gives min, median, p95, max and mean (nearest rank).
func Summarize(ds []time.Duration) Stats {
	if len(ds) == 0 {
		return Stats{}
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	var sum time.Duration
	for _, d := range s {
		sum += d
	}
	rank := func(p float64) time.Duration {
		i := int(p*float64(len(s))+0.5) - 1
		return s[max(0, min(i, len(s)-1))]
	}
	return Stats{N: len(s), Min: s[0], Max: s[len(s)-1], P50: rank(0.5), P95: rank(0.95), Avg: sum / time.Duration(len(s))}
}

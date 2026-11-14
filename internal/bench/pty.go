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
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
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
// so tests can ask "how long until this text" and "how many bytes"; text is
// matched on the screen the output draws (scr), as a user sees it.
type Session struct {
	mu    sync.Mutex
	buf   []byte
	scr   *screen
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
	var replies []byte // answers to terminal queries, sent after the lock
	s.scr = newScreen(int(rows), int(cols), func(b []byte) { replies = append(replies, b...) })
	s.wg.Go(func() {
		tmp := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(tmp)
			if n > 0 {
				s.mu.Lock()
				s.buf = append(s.buf, tmp[:n]...)
				s.scr.Write(tmp[:n])
				out := replies
				replies = nil
				old := s.cond
				s.cond = make(chan struct{})
				s.mu.Unlock()
				close(old)
				if len(out) > 0 {
					_, _ = ptmx.Write(out) // the program waits for it; a closed PTY means it is gone
				}
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

// Text is the screen the output drew so far, one line per row.
func (s *Session) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scr.Text()
}

// Mark is the current output offset; pass it to WaitAfter.
func (s *Session) Mark() int { return s.BytesOut() }

// WaitAfter waits until the program wrote something after mark (any time
// when mark is 0) and match is true for the screen. It returns the time
// since the call.
func (s *Session) WaitAfter(mark int, match func(screen string) bool, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	deadline := time.After(timeout)
	look := func() (bool, chan struct{}) {
		s.mu.Lock()
		defer s.mu.Unlock()
		ok := (mark == 0 || len(s.buf) > mark) && match(s.scr.Text())
		return ok, s.cond
	}
	for {
		ok, c := look()
		if ok {
			return time.Since(start), nil
		}
		select {
		case <-c:
		case <-s.exitc:
			if ok, _ := look(); ok { // last look at the final screen
				return time.Since(start), nil
			}
			return time.Since(start), errors.New("program exited before the screen matched")
		case <-deadline:
			return time.Since(start), errors.New("timeout waiting for the screen")
		}
	}
}

// WaitForText waits for text on the screen now or later.
func (s *Session) WaitForText(text string, timeout time.Duration) (time.Duration, error) {
	return s.WaitAfter(0, func(t string) bool { return strings.Contains(t, text) }, timeout)
}

// TimeKey sends key and returns the time until want shows on the screen at
// a place it was not before the key (a "q" already on screen does not
// count), plus the bytes the program wrote in the meantime (a frame size).
func (s *Session) TimeKey(key []byte, want string, timeout time.Duration) (time.Duration, int, error) {
	s.mu.Lock()
	mark := len(s.buf)
	before := matchSpots(s.scr.Text(), want)
	s.mu.Unlock()
	start := time.Now()
	if err := s.Send(key); err != nil {
		return 0, 0, err
	}
	_, err := s.WaitAfter(mark, func(t string) bool {
		for spot := range matchSpots(t, want) {
			if !before[spot] {
				return true
			}
		}
		return false
	}, timeout)
	d := time.Since(start)
	return d, s.BytesOut() - mark, err
}

// matchSpots is where want shows on the screen: row and byte offset of
// every match.
func matchSpots(scr, want string) map[[2]int]bool {
	spots := map[[2]int]bool{}
	if want == "" {
		return spots
	}
	for row, line := range strings.Split(scr, "\n") {
		for off := 0; ; {
			i := strings.Index(line[off:], want)
			if i < 0 {
				break
			}
			spots[[2]int{row, off + i}] = true
			off += i + 1
		}
	}
	return spots
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
		killGroup(s.pid)
		_ = s.ptmx.Close()
		_ = s.cmd.Wait()
		s.wg.Wait()
	})
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

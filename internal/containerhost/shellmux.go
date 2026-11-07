package containerhost

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// shellmux (plan RC, phase 8): one long `exec -i <c> sh` per host runs
// the many short sh one-liners of the file operations and the short
// commands of Exec.Capture. Over an ssh:// engine context every engine
// call opens a new ssh session, which costs hundreds of milliseconds; the
// mux pays that once. Nothing is copied into the container: the mux is
// the image's own sh, reading commands on stdin.
//
// Each request is
//
//	( set -- '<arg1>' '<arg2>'…; eval '<script>' ) </dev/null
//	printf '\n<nonce> %d\n' "$?"; printf '\n<nonce>\n' >&2
//
// Arguments and the script are single-quoted (' becomes '\''), so no
// argument is ever read as shell code; inside the script they are "$1"…
// as in a one-shot `sh -c script sh args…`. The nonce is 128 random bits,
// new for each request, so the output of a command cannot fake the end
// marker. The subshell keeps cd, variables and exit inside the request;
// </dev/null keeps the command from reading the next request.
//
// Any transport problem (the mux died, a time limit, a broken marker)
// kills the mux and the call runs again as a one-shot engine call, so the
// mux can only make calls faster, never change their answer.

// Piper starts a long engine process with pipes: the shellmux.
type Piper interface {
	Pipe(ctx context.Context, argv ...string) (stdin io.WriteCloser, stdout, stderr io.ReadCloser, stop func(), err error)
}

// SpawnPipe is the real Piper (spawn: the Android trampoline).
type SpawnPipe struct{}

// Pipe starts argv; stop kills it and waits for it.
func (SpawnPipe) Pipe(ctx context.Context, argv ...string) (io.WriteCloser, io.ReadCloser, io.ReadCloser, func(), error) {
	if len(argv) == 0 {
		return nil, nil, nil, nil, errors.New("no command")
	}
	cmd := spawn.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = 2 * time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	errR, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, nil, err
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = in.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		})
	}
	return in, out, errR, stop, nil
}

// muxMaxRestarts is how often a dead mux is started again before the
// host stays on one-shot calls.
const muxMaxRestarts = 3

// errMux is a transport problem of the mux (the caller falls back).
var errMux = errors.New("shellmux")

// shellMux is one running mux.
type shellMux struct {
	in   io.WriteCloser
	out  *bufio.Reader
	errR *bufio.Reader
	stop func()
}

// muxState is the host's mux: started on first use.
type muxState struct {
	mu       sync.Mutex // one request at a time
	m        *shellMux
	restarts int
	off      bool
}

// shQuote single-quotes s for sh.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "tcmux" + hex.EncodeToString(b[:]), nil
}

// muxRequest is the text of one request (see the package comment).
func muxRequest(nonce, script string, args []string) string {
	var b strings.Builder
	b.WriteString("( set --")
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(shQuote(a))
	}
	b.WriteString("; eval ")
	b.WriteString(shQuote(script))
	b.WriteString(" ) </dev/null; printf '\\n%s %d\\n' " + nonce + ` "$?"; printf '\n%s\n' ` + nonce + " >&2\n")
	return b.String()
}

// readUntil reads r up to the marker line "<nonce>[ <code>]" that follows
// a newline. withCode says the marker carries the exit code. The output
// is capped at services.MaxCapture (the rest is read and dropped).
func readUntil(r *bufio.Reader, nonce string, withCode bool) (string, int, error) {
	var buf []byte
	atLineStart := true
	for {
		chunk, err := r.ReadSlice('\n')
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return "", -1, errMux
		}
		full := err == nil
		if full && atLineStart && strings.HasPrefix(string(chunk), nonce) {
			rest := strings.TrimSuffix(string(chunk[len(nonce):]), "\n")
			code := 0
			ok := false
			if withCode {
				if s, cut := strings.CutPrefix(rest, " "); cut {
					if n, perr := strconv.Atoi(s); perr == nil {
						code, ok = n, true
					}
				}
			} else {
				ok = rest == ""
			}
			if ok {
				// The request printed one newline before the marker.
				out := strings.TrimSuffix(string(buf), "\n")
				return out, code, nil
			}
		}
		if room := services.MaxCapture + 1 - len(buf); room > 0 {
			if len(chunk) > room {
				buf = append(buf, chunk[:room]...)
			} else {
				buf = append(buf, chunk...)
			}
		}
		atLineStart = full
	}
}

// run sends one request and reads its answer.
func (m *shellMux) run(ctx context.Context, script string, args []string) (string, string, int, error) {
	nonce, err := newNonce()
	if err != nil {
		return "", "", -1, errMux
	}
	type answer struct {
		out, errs string
		code      int
		err       error
	}
	done := make(chan answer, 1)
	crash.Go("containerhost.run", func() {
		if _, err := io.WriteString(m.in, muxRequest(nonce, script, args)); err != nil {
			done <- answer{err: errMux}
			return
		}
		errc := make(chan answer, 1)
		crash.Go("containerhost.run", func() {
			s, _, err := readUntil(m.errR, nonce, false)
			errc <- answer{errs: s, err: err}
		})
		out, code, err := readUntil(m.out, nonce, true)
		e := <-errc
		if err == nil {
			err = e.err
		}
		done <- answer{out: out, errs: e.errs, code: code, err: err}
	})
	select {
	case a := <-done:
		return a.out, a.errs, a.code, a.err
	case <-ctx.Done():
		m.stop() // unblocks the readers
		return "", "", -1, ctx.Err()
	}
}

// muxStart starts the mux and checks it answers.
func (h *Host) muxStart(ctx context.Context) (*shellMux, error) {
	in, out, errR, stop, err := h.cfg.Pipe.Pipe(context.Background(), h.argv(h.execArgs("-i", "/", []string{"sh"})...)...)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	m := &shellMux{in: in, out: bufio.NewReaderSize(out, 64<<10), errR: bufio.NewReaderSize(errR, 64<<10),
		stop: func() { once.Do(stop) }}
	pctx, cancel := context.WithTimeout(ctx, h.cfg.Timeout)
	defer cancel()
	if _, _, code, err := m.run(pctx, ":", nil); err != nil || code != 0 {
		stop()
		return nil, errMux
	}
	return m, nil
}

// viaMux runs script with args through the mux. ok is false when the mux
// is off or failed: the caller then runs a one-shot engine call.
func (h *Host) viaMux(ctx context.Context, script string, args []string) (out, stderr string, code int, ok bool) {
	if !h.cfg.Mux || h.cfg.Pipe == nil || !h.Caps().HasShell {
		return "", "", 0, false
	}
	st := &h.mux
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.off {
		return "", "", 0, false
	}
	if st.m == nil {
		m, err := h.muxStart(ctx)
		if err != nil {
			st.restarts++
			if st.restarts > muxMaxRestarts {
				st.off = true
			}
			return "", "", 0, false
		}
		st.m = m
	}
	out, stderr, code, err := st.m.run(ctx, script, args)
	if err != nil {
		st.m.stop()
		st.m = nil
		st.restarts++
		if st.restarts > muxMaxRestarts {
			st.off = true
		}
		return "", "", 0, false
	}
	return out, stderr, code, true
}

// muxExecScript runs "$2"… in folder $1, like `exec -w $1 argv…`.
const muxExecScript = `cd -- "$1" || exit 126
shift
exec "$@"`

// Close stops the shellmux, if one runs. The host still works after it
// (with one-shot engine calls, or a new mux on the next call).
func (h *Host) Close() error {
	h.mux.mu.Lock()
	defer h.mux.mu.Unlock()
	if h.mux.m != nil {
		h.mux.m.stop()
		h.mux.m = nil
	}
	return nil
}

// MuxRunning reports whether a shellmux runs now (status menu, tests).
func (h *Host) MuxRunning() bool {
	h.mux.mu.Lock()
	defer h.mux.mu.Unlock()
	return h.mux.m != nil
}

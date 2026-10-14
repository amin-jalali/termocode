package transfer

import (
	"bytes"
	"os"
	"sync"
	"sync/atomic"
)

// Input sits in front of Bubble Tea's input (tea.WithInput): while a
// kitty transfer listens, it takes the terminal's OSC 5113 answers out of
// the key stream and hands them to the transfer. Otherwise it passes
// every byte through unchanged. It keeps the file's descriptor, so
// Bubble Tea still sets raw mode and waits on it.
type Input struct {
	f *os.File

	mu      sync.Mutex
	ch      chan string // nil: nobody listens
	held    []byte      // a tail that may start an answer
	pending []byte      // filtered bytes that did not fit the caller's buffer
}

var kittyPrefix = []byte("\x1b]5113;")

// maxAnswer bounds one held answer; a longer "answer" is passed through.
const maxAnswer = 1 << 20

// NewInput wraps f (normally os.Stdin).
func NewInput(f *os.File) *Input { return &Input{f: f} }

var current atomic.Pointer[Input]

// Use makes in the Input the app listens on (nil: none, e.g. headless).
func Use(in *Input) { current.Store(in) }

// Current is the Input set by Use, or nil.
func Current() *Input { return current.Load() }

// Listen starts taking answers out of the input; they arrive on the
// channel. stop ends it (one listener at a time; a new Listen replaces
// the old one).
func (in *Input) Listen() (<-chan string, func()) {
	ch := make(chan string, 8192)
	in.mu.Lock()
	in.ch = ch
	in.mu.Unlock()
	return ch, func() {
		in.mu.Lock()
		if in.ch == ch {
			in.ch = nil
		}
		in.mu.Unlock()
	}
}

// Inject hands an answer to the listener as if the terminal had sent it.
// For tests (a fake terminal without a TTY).
func (in *Input) Inject(payload string) {
	in.mu.Lock()
	ch := in.ch
	in.mu.Unlock()
	if ch != nil {
		select {
		case ch <- payload:
		default:
		}
	}
}

// Read reads from the file and returns the bytes that are not answers.
// It may return 0 bytes (all were answers); Bubble Tea reads again.
func (in *Input) Read(p []byte) (int, error) {
	in.mu.Lock()
	if len(in.pending) > 0 {
		n := copy(p, in.pending)
		in.pending = in.pending[n:]
		in.mu.Unlock()
		return n, nil
	}
	in.mu.Unlock()
	n, err := in.f.Read(p)
	if n == 0 {
		return 0, err
	}
	out := in.filter(p[:n])
	c := copy(p, out)
	if c < len(out) {
		in.mu.Lock()
		in.pending = append(in.pending, out[c:]...)
		in.mu.Unlock()
	}
	return c, err
}

// Feed filters bytes read elsewhere (an upload reads the terminal
// itself): answers go to the listener, the other bytes are returned.
func (in *Input) Feed(b []byte) []byte { return in.filter(b) }

// filter returns b without answers (with the held tail of the last call
// in front).
func (in *Input) filter(b []byte) []byte {
	in.mu.Lock()
	defer in.mu.Unlock()
	data := append(in.held, b...)
	in.held = nil
	if in.ch == nil {
		return data
	}
	var out []byte
	for len(data) > 0 {
		i := bytes.Index(data, kittyPrefix)
		if i < 0 {
			k := partialPrefix(data)
			out = append(out, data[:len(data)-k]...)
			if k > 0 {
				in.held = append([]byte(nil), data[len(data)-k:]...)
			}
			break
		}
		out = append(out, data[:i]...)
		rest := data[i+len(kittyPrefix):]
		end, tl := oscEnd(rest)
		if end < 0 {
			if len(data)-i > maxAnswer {
				out = append(out, data[i:]...)
			} else {
				in.held = append([]byte(nil), data[i:]...)
			}
			break
		}
		select {
		case in.ch <- string(rest[:end]):
		default: // the transfer stopped reading; drop
		}
		data = rest[end+tl:]
	}
	return out
}

// partialPrefix is the length of the longest tail of b that is a proper
// start of kittyPrefix.
func partialPrefix(b []byte) int {
	for k := min(len(kittyPrefix)-1, len(b)); k > 0; k-- {
		if bytes.Equal(b[len(b)-k:], kittyPrefix[:k]) {
			return k
		}
	}
	return 0
}

// Write writes to the file (term.File).
func (in *Input) Write(p []byte) (int, error) { return in.f.Write(p) }

// Close closes the file.
func (in *Input) Close() error { return in.f.Close() }

// Fd is the file's descriptor (raw mode, epoll).
func (in *Input) Fd() uintptr { return in.f.Fd() }

// Name is the file's name.
func (in *Input) Name() string { return in.f.Name() }

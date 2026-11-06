package agentlog

import (
	"io"
)

// TailWindow is how much of a long file the first Poll reads: the
// newest part, which is what a chat view shows (the session is marked
// Truncated and the view says the earlier turns are not shown).
const TailWindow = 2 << 20

// Tail follows a session file while the CLI writes it: each Poll reads
// what was added since the last one (at most MaxRead per poll; the first
// poll of a longer file starts at its newest TailWindow bytes). A file that
// got shorter is read again from the start. A Tail is not safe for use
// by more than one goroutine at a time.
type Tail struct {
	cli  CLI
	path string
	off  int64
	p    *Parser
}

// NewTail follows path; nothing is opened before the first Poll.
func NewTail(cli CLI, path string) *Tail {
	return &Tail{cli: cli, path: path, p: NewParser(cli)}
}

// Path is the followed file.
func (t *Tail) Path() string { return t.path }

// Poll reads the new bytes. changed reports new turns or usage; err is a
// read error or ErrUnknownFormat.
func (t *Tail) Poll() (changed bool, err error) {
	f, err := openFile(t.path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	size := st.Size()
	if size < t.off { // rewritten: start again
		t.off, t.p = 0, NewParser(t.cli)
		changed = true
	}
	if size == t.off {
		return changed, t.p.Err()
	}
	if t.off == 0 && size > TailWindow {
		t.off = size - TailWindow
		t.p.dropFirst = true
		t.p.s.Truncated = true
	}
	if _, err := f.Seek(t.off, io.SeekStart); err != nil {
		return changed, err
	}
	before := len(t.p.s.Turns) + t.p.s.Skipped
	beforeUsage := t.p.s.Usage
	n, err := io.Copy(t.p, io.LimitReader(f, maxRead))
	t.off += n
	if err != nil {
		return changed, err
	}
	after := len(t.p.s.Turns) + t.p.s.Skipped
	return changed || after != before || t.p.s.Usage != beforeUsage, t.p.Err()
}

// Session is what was read so far (its own copy of the turns).
func (t *Tail) Session() Session {
	s := t.p.Session()
	s.Path = t.path
	s.Turns = append([]Turn(nil), s.Turns...)
	return s
}

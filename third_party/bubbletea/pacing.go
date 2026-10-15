package tea

// termocode patch: frame pacing for slow links (Group S2).
//
// Over SSH the terminal is far away and the line may be slow. Frames are
// written to the local pty at once and queue up in the network (the TCP
// send buffer), so after a held key is let go the user watches the
// queue drain: the cursor keeps moving for seconds. With pacing, every
// frame ends with a status query (DSR 5, CSI 5 n); the terminal answers
// it (CSI 0 n) once it has read the frame. While more than the window of
// frame bytes is unanswered, the renderer holds back and keeps only the
// latest frame, so at most about one window waits in the network.
//
// A terminal that never answered is not paced. An answer that does not
// come in time (lost while detached, taken by a child process) is
// forgiven after pacingTimeout.

import (
	"sync"
	"time"
)

// pacingTimeout forgives unanswered frames, so a lost answer stalls the
// screen for at most this long.
const pacingTimeout = time.Second

// pacingDrain is how long the program waits for the last answers before
// it gives the terminal away (exit, exec), so they do not end up as junk
// in the shell or the child.
const pacingDrain = 800 * time.Millisecond

// maxUnanswered bounds the record of frames a silent terminal never
// answers.
const maxUnanswered = 256

// statusQuery asks the terminal for its status (DSR 5).
const statusQuery = "\x1b[5n"

type sentFrame struct {
	n  int
	at time.Time
}

// framePacer counts the frame bytes the terminal has not answered yet.
type framePacer struct {
	mu       sync.Mutex
	window   int
	now      func() time.Time
	sent     []sentFrame
	bytes    int
	answered bool // the terminal answered at least once
}

func newFramePacer(window int) *framePacer {
	return &framePacer{window: window, now: time.Now}
}

// ready reports whether the next frame may go out.
func (f *framePacer) ready() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.answered || f.bytes < f.window || len(f.sent) == 0 {
		return true
	}
	if f.now().Sub(f.sent[0].at) >= pacingTimeout {
		f.sent, f.bytes = nil, 0
		return true
	}
	return false
}

// frameSent records a frame of n bytes that ends with statusQuery.
func (f *framePacer) frameSent(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentFrame{n: n, at: f.now()})
	f.bytes += n
	if len(f.sent) > maxUnanswered {
		f.bytes -= f.sent[0].n
		f.sent = f.sent[1:]
	}
}

// answer records the terminal's answer to the oldest query.
func (f *framePacer) answer() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answered = true
	if len(f.sent) > 0 {
		f.bytes -= f.sent[0].n
		f.sent = f.sent[1:]
	}
}

// pending is the number of queries the terminal has not answered, when
// it answers at all.
func (f *framePacer) pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.answered {
		return 0
	}
	return len(f.sent)
}

// isStatusAnswer reports whether msg is the terminal's answer to
// statusQuery: CSI 0 n ("ready"), CSI 3 n ("malfunction"), or CSI ? 0 n
// as some emulators send it.
func isStatusAnswer(msg Msg) bool {
	u, ok := msg.(unknownCSISequenceMsg)
	if !ok {
		return false
	}
	switch string(u) {
	case "\x1b[0n", "\x1b[3n", "\x1b[?0n":
		return true
	}
	return false
}

// WithFramePacing paces frames to what the terminal has shown: at most
// window bytes of frames wait unanswered (see pacing.go). For slow links
// such as SSH. termocode patch.
func WithFramePacing(window int) ProgramOption {
	return func(p *Program) {
		if window > 0 {
			p.pacer = newFramePacer(window)
		}
	}
}

// drainAnswers waits up to pacingDrain for the answers still on their
// way and drops other input meanwhile. It runs on the event loop before
// the program gives the terminal away.
func (p *Program) drainAnswers() {
	if p.pacer == nil || p.pacer.pending() == 0 {
		return
	}
	deadline := time.NewTimer(pacingDrain)
	defer deadline.Stop()
	for p.pacer.pending() > 0 {
		select {
		case msg := <-p.msgs:
			if isStatusAnswer(msg) {
				p.pacer.answer()
			}
		case <-deadline.C:
			return
		case <-p.ctx.Done():
			return
		}
	}
}

package tea

// termocode patch (terminal-compat R6): the input reader.
//
// Upstream only waits for more bytes when a read filled its 256-byte
// buffer. With all-motion mouse tracking (1003) a short read often ends
// in the middle of an SGR report ("\x1b[<35;70;" … "19M"); upstream then
// emits the fragments as typed keys, which leak into text inputs. Here an
// incomplete escape sequence at the end of a read is held until the next
// read completes it, or for at most holdWait (then it is parsed as it
// is: a lone Esc, Alt+[ …).
//
// Also, a burst of plain pointer motion (no button) within one read is
// delivered as its last event only: hover needs the latest position, and
// every motion event costs an update and maybe a frame.

import (
	"context"
	"fmt"
	"io"
	"time"
)

// holdWait is how long an incomplete sequence at the end of a read waits
// for its rest. A lone Esc key waits this long too.
const holdWait = 50 * time.Millisecond

// maxHold bounds a held tail; a longer "incomplete" string is parsed as
// it is.
const maxHold = 4096

type readResult struct {
	n   int
	err error
}

// readAnsiInputs reads keypress and mouse inputs from a TTY and produces
// messages containing information about the key or mouse events
// accordingly.
func readAnsiInputs(ctx context.Context, msgs chan<- Msg, input io.Reader) error {
	var buf [256]byte

	// One read at a time, only when asked: nothing is read ahead, so a
	// released terminal (exec) loses no input.
	want := make(chan struct{})
	got := make(chan readResult)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-want:
			case <-stop:
				return
			}
			n, err := input.Read(buf[:])
			select {
			case got <- readResult{n, err}:
			case <-stop:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	ctxErr := func() error {
		err := ctx.Err()
		if err != nil {
			err = fmt.Errorf("found context error while reading input: %w", err)
		}
		return err
	}
	send := func(msg Msg) error {
		select {
		case msgs <- msg:
			return nil
		case <-ctx.Done():
			return ctxErr()
		}
	}

	var held []byte
	reading := false
	for {
		if !reading {
			select {
			case want <- struct{}{}:
				reading = true
			case <-ctx.Done():
				return ctxErr()
			}
		}
		var timeout <-chan time.Time
		var timer *time.Timer
		if len(held) > 0 {
			timer = time.NewTimer(holdWait)
			timeout = timer.C
		}
		var b []byte
		canHaveMoreData, final := false, false
		select {
		case r := <-got:
			reading = false
			if timer != nil {
				timer.Stop()
			}
			if r.err != nil {
				// The held tail is all there will be.
				if err := flushHeld(held, send); err != nil {
					return err
				}
				return fmt.Errorf("error reading input: %w", r.err)
			}
			if startsNewSequence(held, buf[:r.n]) {
				// The held tail was complete after all (a lone Esc
				// before the next sequence): never join the two.
				if err := flushHeld(held, send); err != nil {
					return err
				}
				held = nil
			}
			b = append(held, buf[:r.n]...)
			// A full buffer may continue in the OS buffer (upstream rule).
			canHaveMoreData = r.n == len(buf)
		case <-timeout:
			// The rest never came: parse the tail as it is.
			b, final = held, true
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return ctxErr()
		}
		held = nil

		var motion Msg // a plain pointer motion not sent yet
		var i, w int
		for i = 0; i < len(b); i += w {
			if !final && !canHaveMoreData && len(b)-i <= maxHold && incompleteTail(b[i:]) {
				held = append([]byte(nil), b[i:]...)
				break
			}
			var msg Msg
			w, msg = detectOneMsg(b[i:], canHaveMoreData)
			if w == 0 {
				// Expecting more bytes beyond the current buffer.
				held = append([]byte(nil), b[i:]...)
				break
			}
			if isPlainMotion(msg) {
				motion = msg
				continue
			}
			if motion != nil {
				if err := send(motion); err != nil {
					return err
				}
				motion = nil
			}
			if err := send(msg); err != nil {
				return err
			}
		}
		if motion != nil {
			if err := send(motion); err != nil {
				return err
			}
		}
	}
}

// flushHeld parses a held tail as it is (its rest will not come).
func flushHeld(held []byte, send func(Msg) error) error {
	for i, w := 0, 0; i < len(held); i += w {
		var msg Msg
		w, msg = detectOneMsg(held[i:], false)
		if err := send(msg); err != nil {
			return err
		}
	}
	return nil
}

// startsNewSequence reports whether next (a new read) starts a new escape
// sequence, so the held tail cannot continue into it. A CSI or SS3
// sequence never holds an ESC; only an escape string's terminator (ESC \)
// may come after a held string. Joining a held lone Esc with the next
// answer ("\x1b" + "\x1b[?0n") made Alt+Esc and the typed text "[?0n".
func startsNewSequence(held, next []byte) bool {
	if len(held) == 0 || len(next) == 0 || next[0] != '\x1b' {
		return false
	}
	if len(held) >= 2 {
		switch held[1] {
		case '_', 'P', ']', '^', 'X':
			return false
		}
	}
	return true
}

// isPlainMotion: pointer motion with no button held (all-motion hover).
func isPlainMotion(msg Msg) bool {
	m, ok := msg.(MouseMsg)
	return ok && m.Action == MouseActionMotion && m.Button == MouseButtonNone
}

// incompleteTail reports whether b (the rest of a read) is the start of
// an escape sequence that has not ended yet: a lone ESC, ESC [ or ESC O
// with only parameter bytes after it, or an escape string (APC, DCS,
// OSC, PM, SOS) without its terminator.
func incompleteTail(b []byte) bool {
	if len(b) == 0 || b[0] != '\x1b' {
		return false
	}
	if len(b) == 1 {
		return true
	}
	switch b[1] {
	case '[':
		for _, c := range b[2:] {
			if c < 0x20 || c > 0x3f {
				return false // a final byte (or junk): complete
			}
		}
		return true
	case 'O':
		return len(b) == 2
	case '_', 'P', ']', '^', 'X':
		for i := 2; i < len(b); i++ {
			if b[i] == '\x1b' {
				// ESC \ ends it; an ESC at the very end may be its start.
				return i == len(b)-1
			}
			if b[i] == '\a' && b[1] == ']' {
				return false
			}
		}
		return true
	}
	return false
}

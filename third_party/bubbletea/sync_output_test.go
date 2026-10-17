package tea

import (
	"strings"
	"testing"
)

const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// termocode patch (terminal-compat R10): with synchronized output every
// frame is one begin … end pair, so the terminal shows it at once (no
// torn rows on a slow link, no half-drawn sixel image). The pacing query
// goes after the end mark.
func TestFlushSynchronizedOutput(t *testing.T) {
	out := &syncBuf{}
	r := newRenderer(out, false, 60).(*standardRenderer)
	r.syncOutput = true
	r.pacer = newFramePacer(1 << 20)
	r.buf.WriteString("frame one\nline two")
	r.flushFrame(true)
	got := out.String()
	if !strings.HasPrefix(got, syncBegin) {
		t.Errorf("frame does not start with %q: %q", syncBegin, got)
	}
	if !strings.HasSuffix(got, syncEnd+statusQuery) {
		t.Errorf("frame does not end with end mark + pacing query: %q", got)
	}
	if strings.Count(got, syncBegin) != 1 || strings.Count(got, syncEnd) != 1 {
		t.Errorf("not one pair: %q", got)
	}
	// Nothing new to draw: nothing written, not even an empty pair.
	n := len(out.String())
	r.buf.WriteString("frame one\nline two")
	r.flushFrame(false)
	if len(out.String()) != n {
		t.Errorf("unchanged frame wrote %q", out.String()[n:])
	}
}

func TestFlushWithoutSynchronizedOutput(t *testing.T) {
	out := &syncBuf{}
	r := newRenderer(out, false, 60).(*standardRenderer)
	r.buf.WriteString("frame")
	r.flushFrame(false)
	if strings.Contains(out.String(), "2026") {
		t.Errorf("mode 2026 sent without the option: %q", out.String())
	}
}

func TestWithSynchronizedOutputOption(t *testing.T) {
	p := NewProgram(nil, WithSynchronizedOutput())
	if !p.syncOutput {
		t.Error("option not set")
	}
}

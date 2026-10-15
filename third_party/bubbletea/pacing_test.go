package tea

import (
	"strings"
	"testing"
	"time"
)

// termocode patch (Group S2): every paced frame ends with the status
// query; once the terminal has answered at all, frames wait while more
// than the window is unanswered, and the latest frame goes out after the
// answer.
func TestRendererPacesFrames(t *testing.T) {
	out := &syncBuf{}
	r := newRenderer(out, false, 60).(*standardRenderer)
	r.pacer = newFramePacer(10)
	r.start()
	defer r.kill()
	waitFor := func(what string, f func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for !f() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s; output %q", what, out.String())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	r.write("frame one")
	waitFor("frame one", func() bool { return strings.Contains(out.String(), "frame one") })
	if !strings.HasSuffix(out.String(), statusQuery) {
		t.Fatalf("frame does not end with the status query: %q", out.String())
	}
	// Not answered yet: a terminal that never answers is not paced.
	r.write("frame two")
	waitFor("frame two", func() bool { return strings.Contains(out.String(), "frame two") })

	// The terminal answers frame one; frame two (over the window) is
	// still unanswered, so the next frames wait.
	r.pacer.answer()
	r.write("frame three")
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(out.String(), "frame three") {
		t.Fatalf("frame drawn while the window was full: %q", out.String())
	}
	r.write("frame four") // only the latest state goes out
	r.pacer.answer()
	waitFor("frame four", func() bool { return strings.Contains(out.String(), "frame four") })
	if strings.Contains(out.String(), "frame three") {
		t.Errorf("a held-back frame was drawn: %q", out.String())
	}
}

// A lost answer stalls the screen for at most pacingTimeout.
func TestPacerForgivesLostAnswers(t *testing.T) {
	now := time.Unix(0, 0)
	f := newFramePacer(10)
	f.now = func() time.Time { return now }
	f.frameSent(5)
	f.answer() // the terminal answers
	f.frameSent(50)
	if f.ready() {
		t.Fatal("ready with 50 unanswered bytes over a window of 10")
	}
	now = now.Add(pacingTimeout)
	if !f.ready() {
		t.Fatal("still waiting after the timeout")
	}
	if f.pending() != 0 {
		t.Errorf("pending = %d after the timeout", f.pending())
	}
}

// The answers parse as their own messages, so they never reach the model
// as keys.
func TestStatusAnswerParses(t *testing.T) {
	for _, in := range []string{"\x1b[0n", "\x1b[?0n", "\x1b[3n"} {
		msgs := ParseInput([]byte(in + "a"))
		if len(msgs) != 2 || !isStatusAnswer(msgs[0]) {
			t.Errorf("%q: got %#v", in, msgs)
		}
	}
	if isStatusAnswer(KeyMsg{Type: KeyRunes, Runes: []rune("n")}) {
		t.Error("a key counted as an answer")
	}
}

package tea

import (
	"io"
	"testing"
	"time"
)

// chunkReader returns one chunk per Read, gap apart, then io.EOF.
type chunkReader struct {
	chunks [][]byte
	gap    time.Duration
	n      int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.n >= len(r.chunks) {
		return 0, io.EOF
	}
	if r.n > 0 {
		time.Sleep(r.gap)
	}
	c := r.chunks[r.n]
	r.n++
	return copy(p, c), nil
}

func countMsgs(msgs []Msg) (mouse, keys int, names []string) {
	for _, m := range msgs {
		switch m := m.(type) {
		case MouseMsg:
			mouse++
		case KeyMsg:
			keys++
			names = append(names, m.String())
		}
	}
	return
}

// Terminal-compat R6: an SGR mouse report split over two reads (a short
// read in the middle, as all-motion tracking makes likely) is one mouse
// event, never typed text such as "[<35;70;".
func TestSplitMouseReportIsOneEvent(t *testing.T) {
	seq := []byte("\x1b[<35;70;19M")
	for k := 1; k < len(seq); k++ {
		r := &chunkReader{chunks: [][]byte{seq[:k], seq[k:]}, gap: 5 * time.Millisecond}
		mouse, keys, names := countMsgs(testReadInputs(t, r))
		if mouse != 1 || keys != 0 {
			t.Errorf("split at %d: %d mouse, %d keys %v; want 1 mouse, 0 keys", k, mouse, keys, names)
		}
	}
}

// A lone Esc (or Alt+[) whose rest never comes is still a key, after a
// short wait.
func TestHeldTailFlushedAfterWait(t *testing.T) {
	r := &chunkReader{chunks: [][]byte{[]byte("\x1b"), []byte("a")}, gap: 150 * time.Millisecond}
	_, _, names := countMsgs(testReadInputs(t, r))
	if len(names) != 2 || names[0] != "esc" || names[1] != "a" {
		t.Errorf("keys = %v, want [esc a]", names)
	}
	r = &chunkReader{chunks: [][]byte{[]byte("\x1b["), []byte("x")}, gap: 150 * time.Millisecond}
	_, _, names = countMsgs(testReadInputs(t, r))
	if len(names) != 2 || names[0] != "alt+[" || names[1] != "x" {
		t.Errorf("keys = %v, want [alt+[ x]", names)
	}
}

// A held lone Esc must not swallow the next sequence: the Esc key, then
// the terminal's answer to the frame pacing query, read 5 ms apart, are
// Esc + the answer. (Joined they read as Alt+Esc and the typed text
// "[?0n": the "n" ran a search in the editor, and the pacer lost an
// answer and stalled the screen — TestSlowLink run-on.)
func TestHeldEscNotJoinedWithNextSequence(t *testing.T) {
	for _, next := range []string{"\x1b[?0n", "\x1b[0n", "\x1b[B", "\x1b[<35;1;1M"} {
		r := &chunkReader{chunks: [][]byte{[]byte("\x1b"), []byte(next)}, gap: 5 * time.Millisecond}
		msgs := testReadInputs(t, r)
		if len(msgs) != 2 {
			t.Errorf("esc + %q: %d msgs %v, want 2", next, len(msgs), msgs)
			continue
		}
		if k, ok := msgs[0].(KeyMsg); !ok || k.Type != KeyEscape || k.Alt {
			t.Errorf("esc + %q: first msg %v, want esc", next, msgs[0])
		}
		if k, ok := msgs[1].(KeyMsg); ok && k.Type == KeyRunes {
			t.Errorf("esc + %q: typed %q", next, string(k.Runes))
		}
	}
	// A string terminator split after its ESC still joins.
	r := &chunkReader{chunks: [][]byte{[]byte("\x1b_Gi=31;OK\x1b"), []byte("\\")}, gap: 5 * time.Millisecond}
	for _, m := range testReadInputs(t, r) {
		if k, ok := m.(KeyMsg); ok {
			t.Errorf("split APC terminator: key %q", k.String())
		}
	}
}

// Terminal-compat R6: a burst of plain pointer motion in one read is one
// event (the last position); clicks and drags are never merged.
func TestMotionBurstCoalesced(t *testing.T) {
	burst := "\x1b[<35;1;1M\x1b[<35;2;1M\x1b[<35;3;1M\x1b[<35;4;2M" +
		"\x1b[<0;4;2M" + "\x1b[<32;5;2M\x1b[<32;6;2M" + "\x1b[<35;7;3M\x1b[<35;8;3M"
	msgs := testReadInputs(t, &chunkReader{chunks: [][]byte{[]byte(burst)}})
	var got []MouseMsg
	for _, m := range msgs {
		if mm, ok := m.(MouseMsg); ok {
			got = append(got, mm)
		}
	}
	want := []struct {
		x, y   int
		action MouseAction
	}{
		{3, 1, MouseActionMotion}, {3, 1, MouseActionPress},
		{4, 1, MouseActionMotion}, {5, 1, MouseActionMotion}, // drag: kept
		{7, 2, MouseActionMotion},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d mouse events %+v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].X != w.x || got[i].Y != w.y || got[i].Action != w.action {
			t.Errorf("event %d = %+v, want %+v", i, got[i], w)
		}
	}
}

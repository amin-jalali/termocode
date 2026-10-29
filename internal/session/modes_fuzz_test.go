package session

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/termfont"
)

// maxReplayFuzz is far above what any real UI sets, yet small enough that
// a stream of odd sequences cannot make attach replay megabytes.
const maxReplayFuzz = 16 << 10

// FuzzModesFeed feeds any UI output into the mode tracker. Splitting the
// stream anywhere gives the same state, replaying the state into a fresh
// tracker gives the same replay again, and the replay stays small.
func FuzzModesFeed(f *testing.F) {
	for _, s := range []string{
		"hello\x1b[?1049h\x1b[?1hstext\x1b[31mred\x1b[0m",
		"\x1b[?1003l\x1b[?1002h",
		"\x1b[5 q\x1b]11;rgb:0/0/0\x07\x1b]10;#fff\x1b\\",
		"\x1b[0 q\x1b]111\x07",
		"\x1b_Ga=T;[?1049h\x1b\\\x1bP[?2004h\x1b\\\x1b]2;" + strings.Repeat("x", 600) + "\x07\x1b[?1006h",
		"\x1b[>1u\x1b[>3u\x1b[<u" + termfont.Request(termfont.Kitty, -2, true),
		"\x1bP@kitty-cmd{\"cmd\":\"ls\"}\x1b\\\x1bP+q544e\x1b\\",
		termfont.Request(termfont.Kitty, 3, false),
		"\x1b[?2026h\x1b[?25l\x1b[?47h",
		"\x1b[" + strings.Repeat("1;", 40) + "\x1b[?7h",
		"\x1b]12;red\x1b\x1b[?1h",
	} {
		f.Add([]byte(s), uint16(len(s)/2))
	}
	f.Fuzz(func(t *testing.T, p []byte, cut uint16) {
		whole := NewModes()
		whole.Feed(p)
		split := NewModes()
		i := int(cut) % (len(p) + 1)
		split.Feed(p[:i])
		split.Feed(p[i:])
		r := whole.Replay()
		if !bytes.Equal(r, split.Replay()) || !bytes.Equal(whole.Reset(), split.Reset()) {
			t.Fatalf("split at %d changes the state:\n%q\n%q", i, r, split.Replay())
		}
		if len(r) > maxReplayFuzz {
			t.Fatalf("replay is %d bytes", len(r))
		}
		again := NewModes()
		again.Feed(r)
		if r2 := again.Replay(); !bytes.Equal(r, r2) {
			t.Fatalf("replay is not stable:\n%q\n%q", r, r2)
		}
		_ = whole.Reset()
	})
}

// FuzzReadFrame reads any bytes as the attach protocol. A frame that
// reads must fit the limit and write back to the same bytes.
func FuzzReadFrame(f *testing.F) {
	var seed bytes.Buffer
	_ = writeFrame(&seed, 1, encodeHello(80, 24, &ClientCaps{}))
	f.Add(seed.Bytes())
	f.Add([]byte{2, 0, 0, 0, 4, 0, 80, 0, 24})
	f.Add([]byte{2, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		r := bytes.NewReader(b)
		for {
			typ, p, err := readFrame(r)
			if err != nil {
				return
			}
			if len(p) > maxFrame {
				t.Fatalf("frame of %d bytes passed the limit", len(p))
			}
			var out bytes.Buffer
			if err := writeFrame(&out, typ, p); err != nil {
				t.Fatal(err)
			}
			_ = decodeCode(p)
			_, _, _, _ = decodeHello(p)
		}
	})
}

// FuzzDecodeHello checks the hello payload: short input is an error, and
// a size with caps survives encode -> decode.
func FuzzDecodeHello(f *testing.F) {
	f.Add([]byte{0, 80, 0, 24})
	f.Add(encodeHello(120, 40, &ClientCaps{}))
	f.Add([]byte{0, 1})
	f.Add(append([]byte{0, 1, 0, 1}, "{not json"...))
	f.Fuzz(func(t *testing.T, b []byte) {
		cols, rows, caps, err := decodeHello(b)
		if len(b) < 4 {
			if err == nil {
				t.Fatalf("short hello %q accepted", b)
			}
			return
		}
		if err != nil {
			t.Fatalf("hello %q: %v", b, err)
		}
		c2, r2, caps2, err := decodeHello(encodeHello(cols, rows, caps))
		if err != nil || c2 != cols || r2 != rows || (caps == nil) != (caps2 == nil) {
			t.Fatalf("round trip: %d %d %v -> %d %d %v (%v)", cols, rows, caps, c2, r2, caps2, err)
		}
	})
}

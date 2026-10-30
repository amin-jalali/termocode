package session

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

var testHandoff = WebHandoff{Port: 41237, Sock: "/run/user/1000/termocode/0123456789abcdef.webhttp.sock",
	Secret: "AbC-dEf_0123456789abcdefghijklmnopqrstuvwxy"}

// feed runs a filter over chunks and joins what it lets through.
func feed(chunks ...[]byte) ([]byte, []WebHandoff) {
	f := newWebFilter()
	var out []byte
	var found []WebHandoff
	for _, c := range chunks {
		o, h := f.Filter(c)
		out = append(out, o...)
		found = append(found, h...)
	}
	return out, found
}

// The hand-off string from the far termocode is removed from the stream
// wherever the reads split it, and read back intact; the bytes around it
// pass through in order. This filter reads the far UI's output, not
// keys, so it may hold even a lone Esc at the end of a read: a split right
// after it must not let the secret through.
func TestAPCWebFilterSplitReads(t *testing.T) {
	apc := WebHandoffAPC(testHandoff)
	if !strings.HasPrefix(apc, "\x1b_termocode:web;1;") || !strings.HasSuffix(apc, "\x1b\\") {
		t.Fatalf("APC %q", apc)
	}
	if strings.Contains(apc, testHandoff.Secret) || strings.Contains(apc, testHandoff.Sock) {
		t.Fatalf("the APC carries the values in plain text: %q", apc)
	}
	stream := []byte("before\x1b[1mbold" + apc + "after\x1b[0m")
	for i := 0; i <= len(stream); i++ {
		for j := i; j <= len(stream); j++ {
			out, found := feed(stream[:i], stream[i:j], stream[j:])
			if string(out) != "before\x1b[1mboldafter\x1b[0m" {
				t.Fatalf("split %d,%d: out %q", i, j, out)
			}
			if len(found) != 1 || found[0] != testHandoff {
				t.Fatalf("split %d,%d: found %+v", i, j, found)
			}
		}
	}
	// An error from the far side comes through the same way.
	out, found := feed([]byte("x" + WebHandoffAPC(WebHandoff{Err: "The web panel is off there."}) + "y"))
	if string(out) != "xy" || len(found) != 1 || found[0].Err != "The web panel is off there." {
		t.Errorf("error hand-off: %q %+v", out, found)
	}
	// A lone Esc at the end waits for the next read, then goes on as it was.
	f := newWebFilter()
	if out, _ := f.Filter([]byte("a\x1b")); string(out) != "a" {
		t.Errorf("a trailing Esc was not held: %q", out)
	}
	if out, _ := f.Filter([]byte("[0m")); string(out) != "\x1b[0m" {
		t.Errorf("the held Esc did not come back: %q", out)
	}
}

// A bad hand-off (wrong port, a relative socket, a secret with odd
// characters, an unknown version, too long) is removed but never used;
// a sequence that never ends is let through once it is too long to be
// ours, so the filter holds at most a few hundred bytes.
func TestAPCWebFilterRefusesBadHandoffs(t *testing.T) {
	for _, h := range []WebHandoff{
		{Port: 80, Sock: testHandoff.Sock, Secret: testHandoff.Secret},
		{Port: 70000, Sock: testHandoff.Sock, Secret: testHandoff.Secret},
		{Port: 41237, Sock: "relative.sock", Secret: testHandoff.Secret},
		{Port: 41237, Sock: testHandoff.Sock, Secret: "a b"},
		{Port: 41237, Sock: testHandoff.Sock, Secret: ""},
		{Port: 41237, Sock: "/" + strings.Repeat("s", 200), Secret: testHandoff.Secret},
	} {
		out, found := feed([]byte("a" + WebHandoffAPC(h) + "b"))
		if string(out) != "ab" || len(found) != 0 {
			t.Errorf("%+v: out %q found %+v", h, out, found)
		}
	}
	for _, bad := range []string{"\x1b_termocode:web;2;x\x1b\\", "\x1b_termocode:web;1;41237\x1b\\", "\x1b_termocode:web;0;!!\x1b\\"} {
		if out, found := feed([]byte("a" + bad + "b")); string(out) != "ab" || len(found) != 0 {
			t.Errorf("%q: out %q found %+v", bad, out, found)
		}
	}
	long := "\x1b_termocode:web;1;" + strings.Repeat("A", 2*maxWebAPC)
	f := newWebFilter()
	out, found := f.Filter([]byte(long))
	if len(found) != 0 || !bytes.Equal(out, []byte(long)) || len(f.held) != 0 {
		t.Errorf("an endless sequence: out %d bytes of %d, held %d", len(out), len(long), len(f.held))
	}
}

// FuzzAPCWebFilter: the filter never panics, holds a bounded tail, gives
// the same result however the input is split, lets input without the
// prefix through untouched, and reads back any hand-off it was given.
func FuzzAPCWebFilter(f *testing.F) {
	f.Add([]byte("plain"), 3, 7)
	f.Add([]byte("a"+WebHandoffAPC(testHandoff)+"b"), 5, 40)
	f.Add([]byte("\x1b_termocode:web;0;"+strings.Repeat("x", 600)), 1, 2)
	f.Add([]byte("\x1b_termocode:link;1\x1b\\\x1b_termocode:web;1;1;2;3\x1b\\"), 0, 0)
	f.Fuzz(func(t *testing.T, data []byte, a, b int) {
		whole, wfound := feed(data)
		i, j := clampSplit(a, len(data)), clampSplit(b, len(data))
		if i > j {
			i, j = j, i
		}
		f := newWebFilter()
		var out []byte
		var found []WebHandoff
		for _, c := range [][]byte{data[:i], data[i:j], data[j:]} {
			o, h := f.Filter(c)
			out = append(out, o...)
			found = append(found, h...)
			if len(f.held) > maxWebAPC+len(webAPCPrefix) {
				t.Fatalf("holds %d bytes", len(f.held))
			}
		}
		out = append(out, f.held...) // what is still held would come with the next read
		whole = append(whole, newWebFilterHeld(data)...)
		if !bytes.Equal(out, whole) || !reflect.DeepEqual(found, wfound) {
			t.Fatalf("split %d,%d differs:\n%q %+v\n%q %+v", i, j, out, found, whole, wfound)
		}
		if !bytes.Contains(data, []byte("\x1b_termocode:web;")) {
			if !bytes.Equal(out, data) {
				t.Fatalf("plain input changed: %q -> %q", data, out)
			}
		}
		for _, h := range found {
			_, back := feed([]byte(WebHandoffAPC(h)))
			if len(back) != 1 || back[0] != h {
				t.Fatalf("round trip of %+v gave %+v", h, back)
			}
		}
	})
}

func clampSplit(v, n int) int {
	if v < 0 {
		v = -v
	}
	if n == 0 {
		return 0
	}
	return v % (n + 1)
}

// newWebFilterHeld is what a filter fed data in one read still holds.
func newWebFilterHeld(data []byte) []byte {
	f := newWebFilter()
	f.Filter(data)
	return f.held
}

package session

import (
	"reflect"
	"testing"
)

func TestSeqFilter(t *testing.T) {
	f := newSeqFilter(linkPing, linkPong)
	out, found := f.Filter([]byte("a" + linkPing + "b" + linkPong + linkPing + "c"))
	if string(out) != "abc" || !reflect.DeepEqual(found, []int{0, 1, 0}) {
		t.Errorf("got %q %v", out, found)
	}
	// Split across reads: held back, then found.
	out, found = f.Filter([]byte("x" + linkPing[:5]))
	if string(out) != "x" || found != nil {
		t.Errorf("first half: %q %v", out, found)
	}
	out, found = f.Filter([]byte(linkPing[5:] + "y"))
	if string(out) != "y" || !reflect.DeepEqual(found, []int{0}) {
		t.Errorf("second half: %q %v", out, found)
	}
	// A prefix that turns out to be something else passes through whole.
	out, _ = f.Filter([]byte("\x1b_termo"))
	if string(out) != "" {
		t.Errorf("held prefix leaked: %q", out)
	}
	out, found = f.Filter([]byte("cat\x1b\\ \x1b[A"))
	if string(out) != "\x1b_termocat\x1b\\ \x1b[A" || found != nil {
		t.Errorf("not ours: %q %v", out, found)
	}
	// A lone Esc key or Alt+_ never waits for the next key.
	for _, k := range []string{"q\x1b", "\x1b_"} {
		if out, _ := f.Filter([]byte(k)); string(out) != k {
			t.Errorf("%q held back: %q", k, out)
		}
	}
}

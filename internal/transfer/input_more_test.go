package transfer

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestUseAndCurrent(t *testing.T) {
	old := Current()
	t.Cleanup(func() { Use(old) })
	in := &Input{}
	Use(in)
	if Current() != in {
		t.Fatal("Current is not the Input given to Use")
	}
	Use(nil)
	if Current() != nil {
		t.Fatal("Use(nil) did not clear it")
	}
}

func TestInjectAndFeed(t *testing.T) {
	in := &Input{}
	in.Inject("ac=status") // nobody listens: dropped, no block
	ch, stop := in.Listen()
	in.Inject("ac=status;id=x")
	if got := <-ch; got != "ac=status;id=x" {
		t.Fatalf("got %q", got)
	}
	if got := string(in.Feed([]byte("a\x1b]5113;ac=data\x07b"))); got != "ab" {
		t.Fatalf("Feed kept %q", got)
	}
	if got := <-ch; got != "ac=data" {
		t.Fatalf("got %q", got)
	}
	// A full channel (the transfer stopped reading) drops answers
	// instead of blocking the keyboard.
	for i := 0; i < cap(ch); i++ {
		in.Inject("fill")
	}
	in.Inject("one too many")
	if got := string(in.Feed([]byte("k\x1b]5113;dropped\x07"))); got != "k" {
		t.Fatalf("Feed with a full channel kept %q", got)
	}
	stop()
	// A stop from an older Listen does not end a newer one.
	ch2, stop2 := in.Listen()
	stop()
	in.Inject("still here")
	if got := <-ch2; got != "still here" {
		t.Fatalf("got %q", got)
	}
	stop2()
}

// A huge paste that starts like an answer but never ends is typed text:
// it passes through once it is longer than any answer.
func TestInputHugeUnterminatedPastePassesThrough(t *testing.T) {
	in := &Input{}
	_, stop := in.Listen()
	defer stop()
	paste := "\x1b]5113;" + strings.Repeat("p", maxAnswer+10)
	if got := in.Feed([]byte(paste)); len(got) != len(paste) {
		t.Fatalf("passed %d of %d bytes", len(got), len(paste))
	}
}

func TestInputReadSmallBufferAndEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	in := NewInput(r)
	_, stop := in.Listen()
	defer stop()
	if _, err := w.WriteString("abcdef"); err != nil {
		t.Fatal(err)
	}
	// Bytes that do not fit the caller's buffer come on the next Read.
	in.held = []byte("\x1b]51") // a held tail that is not an answer after all
	buf := make([]byte, 3)
	var got []byte
	for len(got) < 10 {
		n, err := in.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if string(got) != "\x1b]51abcdef" {
		t.Fatalf("got %q", got)
	}
	if n, err := in.Write([]byte("x")); n != 0 || err == nil {
		t.Errorf("Write on the read end: %d %v, want an error", n, err)
	}
	_ = w.Close()
	if n, err := in.Read(buf); n != 0 || err != io.EOF {
		t.Fatalf("after close: %d %v, want EOF", n, err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
}

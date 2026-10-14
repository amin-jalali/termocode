package transfer

import (
	"os"
	"testing"
	"time"
)

// While a transfer listens, kitty's OSC 5113 replies are taken out of the
// key stream (even when split across reads); everything else, and all
// input when nobody listens, passes through unchanged.
func TestInputFilter(t *testing.T) {
	in := &Input{}
	// Not listening: bytes pass as they are.
	reply := "\x1b]5113;ac=status;id=a;st=T0s=\x1b\\"
	if got := string(in.filter([]byte("x" + reply))); got != "x"+reply {
		t.Fatalf("not listening: %q", got)
	}
	ch, stop := in.Listen()
	defer stop()
	parts := []string{"ab\x1b", "]51", "13;ac=status;id=a;st=T0s=\x1b", "\\cd\x1b]5113;ac=data;d=AA==\x07", "\x1b[A"}
	var keys string
	for _, p := range parts {
		keys += string(in.filter([]byte(p)))
	}
	if keys != "abcd\x1b[A" {
		t.Errorf("keys %q, want %q", keys, "abcd\x1b[A")
	}
	for _, want := range []string{"ac=status;id=a;st=T0s=", "ac=data;d=AA=="} {
		select {
		case got := <-ch:
			if got != want {
				t.Errorf("reply %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("reply %q not delivered", want)
		}
	}
	// Alt+] (ESC ]) followed by other keys is not a reply: it passes.
	if got := string(in.filter([]byte("\x1b]x"))); got != "\x1b]x" {
		t.Errorf("alt+]: %q", got)
	}
}

// Input works as Bubble Tea's input: Read returns the filtered bytes
// from the file and it keeps the file's descriptor (raw mode, epoll).
func TestInputReadsFile(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	in := NewInput(r)
	if in.Fd() != r.Fd() || in.Name() != r.Name() {
		t.Error("Fd / Name not the file's")
	}
	_, stop := in.Listen()
	_, _ = w.WriteString("k\x1b]5113;ac=status\x1b\\")
	buf := make([]byte, 64)
	n, err := in.Read(buf)
	if err != nil || string(buf[:n]) != "k" {
		t.Fatalf("read %q %v", buf[:n], err)
	}
	stop()
}

package session

import "testing"

func TestDetachFilter(t *testing.T) {
	var f detachFilter
	out, detach := f.Filter([]byte("a\x1b[1mb" + DetachSeq + "c"))
	if !detach || string(out) != "a\x1b[1mbc" {
		t.Fatalf("got %q %v", out, detach)
	}
	// Split across reads, byte by byte: nothing of it is forwarded.
	var got []byte
	detach = false
	for _, c := range []byte("x" + DetachSeq + "y") {
		o, d := f.Filter([]byte{c})
		got = append(got, o...)
		detach = detach || d
	}
	if !detach || string(got) != "xy" {
		t.Fatalf("split request: got %q %v", got, detach)
	}
	// Other APC strings (kitty images) pass through untouched.
	in := "\x1b_Ga=T;AAAA\x1b\\\x1b_termocode:other\x1b\\"
	out, detach = f.Filter([]byte(in))
	if detach || string(out) != in {
		t.Fatalf("other APC: got %q %v", out, detach)
	}
	// A held tail that turns out not to be a request is sent later.
	out, _ = f.Filter([]byte("z\x1b_term"))
	if string(out) != "z" {
		t.Fatalf("tail not held: %q", out)
	}
	out, detach = f.Filter([]byte("inal\x1b\\"))
	if detach || string(out) != "\x1b_terminal\x1b\\" {
		t.Fatalf("held tail lost: %q %v", out, detach)
	}
}

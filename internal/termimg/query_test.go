//go:build unix

package termimg

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// readUntil stops as soon as done is true, and at the deadline otherwise.
func TestReadUntil(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	_, _ = w.WriteString("abcEND")
	got := readUntil(r, time.Now().Add(2*time.Second), func(b []byte) bool { return bytes.HasSuffix(b, []byte("END")) })
	if string(got) != "abcEND" {
		t.Errorf("got %q", got)
	}
	start := time.Now()
	got = readUntil(r, time.Now().Add(50*time.Millisecond), func([]byte) bool { return false })
	if len(got) != 0 || time.Since(start) > time.Second {
		t.Errorf("silent input: got %q after %v", got, time.Since(start))
	}
}

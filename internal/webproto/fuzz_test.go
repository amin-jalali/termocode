package webproto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// FuzzDecodeClientMsg feeds the strict decoder random bytes. Invariants:
// it never panics; an accepted message encodes; when the encoding is not
// longer than the input it decodes again to the same bytes (the decoder
// and the encoder agree).
func FuzzDecodeClientMsg(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "golden", "client", "*.json"))
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(bytes.TrimSuffix(b, []byte("\n")))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		m, err := DecodeClient(in)
		if err != nil {
			return
		}
		if len(in) > MaxFrame {
			t.Fatalf("accepted %d bytes", len(in))
		}
		out, err := Encode(m)
		if err != nil {
			t.Fatalf("accepted message does not encode: %v", err)
		}
		if len(out) > len(in) {
			return // escapes can grow a string past a limit
		}
		m2, err := DecodeClient(out)
		if err != nil {
			t.Fatalf("own encoding refused: %v\n in %q\nout %q", err, in, out)
		}
		out2, err := Encode(m2)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("not stable: %q vs %q (%v)", out, out2, err)
		}
	})
}

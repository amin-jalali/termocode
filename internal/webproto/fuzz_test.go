package webproto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FuzzOpsArgs feeds every op's args decoder random bytes. Invariants: no
// panic; accepted args fit the row's size cap; they encode, and their
// encoding (when it still fits) is accepted again and is stable.
func FuzzOpsArgs(f *testing.F) {
	ops := Ops()
	for i, p := range ops {
		b, err := os.ReadFile(filepath.Join("testdata", "golden", "ops", p.Op+".json"))
		if err != nil {
			f.Fatal(err)
		}
		m, err := DecodeClient(bytes.TrimSuffix(b, []byte("\n")))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(uint8(i), []byte(m.(*Req).Args))
	}
	f.Fuzz(func(t *testing.T, i uint8, raw []byte) {
		p := ops[int(i)%len(ops)]
		a, err := DecodeArgs(p.Op, raw)
		if err != nil {
			return
		}
		if len(raw) > p.MaxBytes {
			t.Fatalf("%s: accepted %d bytes", p.Op, len(raw))
		}
		out, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("%s: accepted args do not encode: %v", p.Op, err)
		}
		if len(out) > p.MaxBytes {
			return
		}
		a2, err := DecodeArgs(p.Op, out)
		if err != nil {
			t.Fatalf("%s: own encoding refused: %v\n in %q\nout %q", p.Op, err, raw, out)
		}
		out2, _ := json.Marshal(a2)
		if !bytes.Equal(out, out2) {
			t.Fatalf("%s: not stable: %q vs %q", p.Op, out, out2)
		}
	})
}

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

package webproto

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Golden fixtures live in testdata/golden/<dir>/<name>.json, one compact
// JSON line each. client/, server/ and gateway/ hold one file per message
// kind of that direction.

type goldenDir struct {
	dir    string
	names  func() []string
	decode func([]byte) (Msg, error)
}

func goldenDirs() []goldenDir {
	return []goldenDir{
		{"client", ClientKinds, DecodeClient},
		{"server", ServerKinds, DecodeServer},
		{"gateway", GatewayKinds, DecodeGatewayLine},
	}
}

func readGoldens(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "golden", dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = bytes.TrimSuffix(b, []byte("\n"))
	}
	return out
}

// TestGoldensRoundTrip decodes every fixture with its direction's decoder,
// encodes it again and wants the same bytes.
func TestGoldensRoundTrip(t *testing.T) {
	for _, g := range goldenDirs() {
		for name, b := range readGoldens(t, g.dir) {
			m, err := g.decode(b)
			if err != nil {
				t.Errorf("%s/%s: decode: %v", g.dir, name, err)
				continue
			}
			if m.Type() != name {
				t.Errorf("%s/%s: decoded as %q", g.dir, name, m.Type())
			}
			out, err := Encode(m)
			if err != nil {
				t.Errorf("%s/%s: encode: %v", g.dir, name, err)
				continue
			}
			if !bytes.Equal(out, b) {
				t.Errorf("%s/%s: round trip differs\n got %s\nwant %s", g.dir, name, out, b)
			}
		}
	}
}

// TestGoldensCoverEveryTypeAndOp fails when a message kind has no fixture,
// or a fixture names no kind.
func TestGoldensCoverEveryTypeAndOp(t *testing.T) {
	for _, g := range goldenDirs() {
		have := readGoldens(t, g.dir)
		want := map[string]bool{}
		for _, k := range g.names() {
			want[k] = true
			if _, ok := have[k]; !ok {
				t.Errorf("%s: no fixture for %q", g.dir, k)
			}
		}
		var extra []string
		for k := range have {
			if !want[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			t.Errorf("%s: fixtures without a kind: %v", g.dir, extra)
		}
	}
}

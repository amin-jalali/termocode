package webproto

import (
	"bytes"
	"encoding/json"
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

// TestOpGoldensRoundTrip decodes every ops/<op>.json request, decodes its
// args into the op's type, and encodes both again to the same bytes.
func TestOpGoldensRoundTrip(t *testing.T) {
	for name, b := range readGoldens(t, "ops") {
		m, err := DecodeClient(b)
		if err != nil {
			t.Errorf("ops/%s: decode: %v", name, err)
			continue
		}
		req, ok := m.(*Req)
		if !ok || req.Op != name {
			t.Errorf("ops/%s: not a req for its op: %#v", name, m)
			continue
		}
		args, err := DecodeArgs(req.Op, req.Args)
		if err != nil {
			t.Errorf("ops/%s: args: %v", name, err)
			continue
		}
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		req.Args = raw
		out, err := Encode(req)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, b) {
			t.Errorf("ops/%s: round trip differs\n got %s\nwant %s", name, out, b)
		}
	}
}

// TestGoldensCoverEveryTypeAndOp fails when a message kind or an op has no
// fixture, or a fixture names no kind or op.
func TestGoldensCoverEveryTypeAndOp(t *testing.T) {
	dirs := goldenDirs()
	dirs = append(dirs, goldenDir{dir: "ops", names: func() []string {
		var out []string
		for _, p := range Ops() {
			out = append(out, p.Op)
		}
		return out
	}})
	for _, g := range dirs {
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

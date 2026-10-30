package webproto

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestProtoTSInSync fails when the committed proto.d.ts or schema.json is
// not what the generator writes now. Fix: go generate ./internal/webproto
func TestProtoTSInSync(t *testing.T) {
	for _, f := range []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"proto.d.ts", GenerateTS},
		{"schema.json", GenerateSchema},
	} {
		want, err := f.gen()
		if err != nil {
			t.Fatal(err)
		}
		have, err := os.ReadFile(f.name)
		if err != nil {
			t.Fatalf("%s: %v (run go generate ./internal/webproto)", f.name, err)
		}
		if !bytes.Equal(have, want) {
			t.Errorf("%s is stale: run go generate ./internal/webproto", f.name)
		}
	}
}

func TestGeneratedTSCoversProtocol(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`export interface Hello {`,
		`  t: "hello";`,
		`  device_name?: string;`,
		`  resume?: Resume;`,
		`  topics: Record<string, number>;`,
		`  role: Role;`,
		`  data: unknown;`,
		`  code: CloseCode;`,
		`export type Role = "viewer" | "controller";`,
		`export type ClientMsg = Bye | Hello | Ping | Req | Sub | Unsub;`,
		`  "buf.set_text": SetTextArgs;`,
		`  lines: string[];`,
		`export type OpName = keyof OpArgs;`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
	for _, never := range []string{`"web.open"`, `"web.stop"`, "WebOpenArgs"} {
		if strings.Contains(ts, never) {
			t.Errorf("proto.d.ts offers the control op %s to the browser", never)
		}
	}
}

func TestGeneratedSchemaStrictForClient(t *testing.T) {
	b, err := GenerateSchema()
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Ops  []struct{ Op, Args, Role string }
		Defs map[string]struct {
			AdditionalProperties bool `json:"additionalProperties"`
			Required             []string
		}
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Ops) != len(Ops()) {
		t.Fatalf("schema has %d ops, table has %d", len(s.Ops), len(Ops()))
	}
	if d := s.Defs["Hello"]; d.AdditionalProperties {
		t.Error("Hello allows unknown fields")
	}
	if d := s.Defs["SetTextArgs"]; d.AdditionalProperties {
		t.Error("SetTextArgs allows unknown fields")
	}
	if d := s.Defs["Meta"]; !d.AdditionalProperties {
		t.Error("Meta (server → browser) must allow new fields")
	}
}

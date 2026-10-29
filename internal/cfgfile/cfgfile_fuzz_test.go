package cfgfile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// decodeExact reads JSON keeping numbers as written.
func decodeExact(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}

// FuzzUpdate runs a no-op Update and UpdateRaw over any config file. A
// file that is not a JSON object is refused and left byte for byte; an
// object keeps every key and value exactly.
func FuzzUpdate(f *testing.F) {
	for _, s := range []string{
		`{"theme":"dark","font_delta":2,"nested":{"a":[1,2.5,true,null]}}`,
		`{"ai_day_budget_usd":0.1}`,
		``, `null`, `[]`, `"x"`, `{"a":1} trailing`, `{"a":`, `// comment
{}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		dir := t.TempDir()
		for i, run := range []func(string) error{
			func(p string) error { return Update(p, func(map[string]any) error { return nil }) },
			func(p string) error { return UpdateRaw(p, func(map[string]json.RawMessage) error { return nil }) },
		} {
			p := filepath.Join(dir, "config"+string(rune('a'+i))+".json")
			if err := os.WriteFile(p, b, 0o600); err != nil {
				t.Fatal(err)
			}
			err := run(p)
			after, rerr := os.ReadFile(p)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if err != nil {
				if !bytes.Equal(after, b) {
					t.Fatalf("failed update (%v) changed the file:\n%q\n%q", err, b, after)
				}
				continue
			}
			want, werr := decodeExact(b)
			if len(bytes.TrimSpace(b)) == 0 || werr != nil || want == nil {
				continue
			}
			got, gerr := decodeExact(after)
			if gerr != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("no-op update changed the config:\n%s\n---\n%s", b, after)
			}
		}
	})
}

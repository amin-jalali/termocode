package mcp

import (
	"encoding/json"
	"testing"
)

var viewLayouts = map[string]bool{ViewTable: true, ViewList: true, ViewCard: true, ViewDetail: true, ViewText: true, ViewMarkdown: true, ViewJSON: true, ViewLine: true}

var viewKinds = map[string]bool{KindString: true, KindNumber: true, KindInteger: true, KindBool: true, KindTime: true, KindJSON: true, KindEnum: true}

func checkShape(t *testing.T, s ViewShape, ok bool) {
	t.Helper()
	if ok && !viewLayouts[s.Layout] {
		t.Fatalf("layout %q", s.Layout)
	}
	if len(s.Fields) > MaxViewFields {
		t.Fatalf("%d fields", len(s.Fields))
	}
	seen := map[string]bool{}
	for _, f := range s.Fields {
		if f.Name == "" || seen[f.Name] || !viewKinds[f.Kind] {
			t.Fatalf("field %+v in %+v", f, s.Fields)
		}
		seen[f.Name] = true
	}
}

// Fuzz (MOB-72): any schema gives a known layout, at most MaxViewFields
// unique named fields of known kinds, params of known kinds; no panic.
func FuzzViewSchemas(f *testing.F) {
	for _, s := range []string{`{}`, `null`, `{"type":"array","items":{"type":"object","properties":{"a":{"type":"string"}}}}`,
		`{"type":"object","properties":{"x":{"type":"array","items":{"properties":{"":{}}}}}}`, `{"type":["string","null"]}`,
		`{"properties":{"q":{"anyOf":[{"type":"string"},{"type":"null"}]},"e":{"enum":[1,"a",null]}},"required":"q"}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		var schema map[string]any
		if json.Unmarshal([]byte(raw), &schema) != nil {
			return
		}
		s, ok := ViewShapeFromSchema(schema)
		checkShape(t, s, ok)
		params, _ := ViewParamsFromSchema(schema)
		for _, p := range params {
			if p.Name == "" || !viewKinds[p.Kind] {
				t.Fatalf("param %+v", p)
			}
		}
	})
}

// Fuzz (MOB-72): any result teaches a valid shape or nothing; no panic.
func FuzzViewResults(f *testing.F) {
	for _, s := range []string{`[{"a":1}]`, `{"items":[{"b":"x"}]}`, `"text"`, `[1,2]`, `[]`, `{"":1}`, `[[{"a":1}]]`} {
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, raw string, asText bool) {
		res := CallResult{StructuredContent: json.RawMessage(raw)}
		if asText || !json.Valid([]byte(raw)) {
			res = CallResult{Content: []Content{{Type: "text", Text: raw}}}
		}
		s, ok := LearnViewShape(res)
		checkShape(t, s, ok)
		ResourceViewLayout(ResourceContents{URI: "x://a", MimeType: "application/json", Text: raw})
		TemplateVars(raw)
	})
}

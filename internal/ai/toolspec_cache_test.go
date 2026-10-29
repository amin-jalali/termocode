package ai

import (
	"strings"
	"testing"
)

func specJSON(t *testing.T, tl Tool) string {
	t.Helper()
	b, err := tl.Spec().openAIJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A tool built again with the same fields (the next turn) reuses its
// encoded JSON; a changed description, a changed schema value, a schema
// map changed in place or an added parameter gives new JSON at once.
func TestToolSpecWireCache(t *testing.T) {
	mk := func() Tool {
		return Tool{Name: "cache_probe", Description: "first",
			Params:   map[string]any{"op": map[string]any{"type": "string", "enum": []string{"a", "b"}}},
			Required: []string{"op"}}
	}
	if a, b := mk().Spec(), mk().Spec(); a.wire == nil || a.wire != b.wire {
		t.Fatal("the same tool built twice does not share its JSON")
	}
	if got := specJSON(t, mk()); !strings.Contains(got, `"enum":["a","b"]`) || !strings.Contains(got, `"first"`) {
		t.Fatalf("json %s", got)
	}

	d := mk()
	d.Description = "second"
	if got := specJSON(t, d); !strings.Contains(got, `"second"`) {
		t.Errorf("new description not sent: %s", got)
	}

	e := mk()
	e.Params["op"].(map[string]any)["enum"] = []string{"a", "c"}
	if got := specJSON(t, e); !strings.Contains(got, `"enum":["a","c"]`) {
		t.Errorf("new enum not sent: %s", got)
	}

	f := mk()
	spec := f.Spec()
	f.Params["op"].(map[string]any)["type"] = "integer" // changed in place after the spec was made
	if got := specJSON(t, f); !strings.Contains(got, `"type":"integer"`) {
		t.Errorf("schema changed in place not sent: %s", got)
	}
	if b, _ := spec.openAIJSON(); !strings.Contains(string(b), `"type":"string"`) {
		t.Errorf("the earlier spec's JSON changed: %s", b)
	}

	g := mk()
	g.Params["n"] = map[string]any{"type": "integer"}
	if got := specJSON(t, g); !strings.Contains(got, `"n":{"type":"integer"}`) {
		t.Errorf("added parameter not sent: %s", got)
	}
	if got := specJSON(t, mk()); !strings.Contains(got, `"first"`) || strings.Contains(got, `"n"`) {
		t.Errorf("back to the first tool: %s", got)
	}
}

// Values the cache can not copy (not JSON-like) are encoded each time.
func TestToolSpecWireOddValues(t *testing.T) {
	type odd struct{ X int }
	tl := Tool{Name: "odd_probe", Schema: map[string]any{"type": "object", "x": odd{1}}}
	if tl.Spec().wire != nil {
		t.Error("a schema with a struct value is cached")
	}
	if got := specJSON(t, tl); !strings.Contains(got, `"x":{"X":1}`) {
		t.Errorf("json %s", got)
	}
	if !jsonEqual(map[string]any{"a": []any{1.0, "x", nil, true}}, map[string]any{"a": []any{1.0, "x", nil, true}}) ||
		jsonEqual(map[string]any{"a": 1}, map[string]any{"a": 1.0}) || jsonEqual([]string{}, []string(nil)) {
		t.Error("jsonEqual")
	}
}

func BenchmarkToolSpecs150(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		for _, s := range perfSpecs(150) {
			if _, err := s.openAIJSON(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

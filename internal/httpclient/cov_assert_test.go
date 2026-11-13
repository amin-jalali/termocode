package httpclient

import (
	"strings"
	"testing"
	"time"
)

func runAssert(t *testing.T, line string, resp *Response) AssertResult {
	t.Helper()
	res := RunAsserts([]string{line}, resp)
	if len(res) != 1 {
		t.Fatalf("results = %+v", res)
	}
	return res[0]
}

func TestParseAssertRefusals(t *testing.T) {
	for line, want := range map[string]string{
		"status exists 200":    "takes no value",
		"status > many":        "needs a number",
		"time < soon":          "",
		"header X schema {}":   "schema checks the body",
		"body schema nonsense": "not JSON",
		"body schema [1]":      "must be a JSON object",
		"body matches (":       "bad pattern",
		"body matches " + strings.Repeat("a", maxAssertPattern+1): "longer than",
		"body $. exists":     "",
		"body $[x] exists":   "bad index",
		"body $[-1] exists":  "bad index",
		"body $x exists":     "bad path",
		"body nopath exists": "path must start",
	} {
		_, err := ParseAssert(line)
		if err == nil {
			t.Errorf("ParseAssert(%q) = nil error", line)
			continue
		}
		if want != "" && !strings.Contains(err.Error(), want) {
			t.Errorf("ParseAssert(%q) = %v, want %q", line, err, want)
		}
	}
}

func TestAssertTimeAndNumberOperators(t *testing.T) {
	resp := &Response{Status: 200, Elapsed: 250 * time.Millisecond, Body: []byte(`{"n":5,"s":"abc","list":[1,"two"],"q":{"ab":1}}`)}
	pass := map[string]bool{
		"time == 250ms": true, "time != 1s": true, "time < 1s": true, "time <= 250ms": true,
		"time > 100ms": true, "time >= 250ms": true,
		"body $.n <= 5": true, "body $.n > 4": true, "body $.n >= 5": true, "body $.n < 6": true,
		"body $.list contains two": true, "body $.list contains 1": true,
		`body $.q["ab"] == 1`: true, "body $.q['ab'] exists": true,
		"body $.zzz !exists": true,
	}
	for line, want := range pass {
		if got := runAssert(t, line, resp); got.Pass != want {
			t.Errorf("%s: pass %v (%s)", line, got.Pass, got.Message)
		}
	}
	for line, why := range map[string]string{
		"time > 1s":              "time was",
		"time contains 1s":        "time does not support",
		"body $.list contains 9": "has no item",
		"body $.s > 1":           "not both numbers",
		"body $.n exists":        "",
		"body $.n !exists":       "is there",
		"body $.zzz exists":      "is missing",
	} {
		got := runAssert(t, line, resp)
		if line == "body $.n exists" {
			if !got.Pass {
				t.Errorf("%s should pass", line)
			}
			continue
		}
		if got.Pass || !strings.Contains(got.Message, why) {
			t.Errorf("%s = pass %v message %q, want %q", line, got.Pass, got.Message, why)
		}
	}
}

func TestAssertSchemaChecks(t *testing.T) {
	resp := &Response{Status: 200, Body: []byte(`{"name":"ab","age":3,"tags":["x"],"extra":1,"mode":"x"}`)}
	for _, c := range []struct {
		schema, why string
	}{
		{`{"type":["string","null"]}`, "want"},
		{`{"type":"object","properties":{"name":{"minLength":3}}}`, "length 2, want at least 3"},
		{`{"type":"object","properties":{"name":{"maxLength":1}}}`, "length 2, want at most 1"},
		{`{"type":"object","properties":{"name":{"pattern":"^z"}}}`, "does not match"},
		{`{"type":"object","properties":{"name":{"pattern":"("}}}`, "bad pattern"},
		{`{"type":"object","properties":{"age":{"maximum":2}}}`, "is above 2"},
		{`{"type":"object","properties":{"age":{"minimum":9}}}`, "below"},
		{`{"type":"object","properties":{"tags":{"minItems":2}}}`, "at least 2"},
		{`{"type":"object","properties":{"tags":{"maxItems":0}}}`, "at most 0"},
		{`{"type":"object","properties":{"mode":{"const":"y"}}}`, "want"},
		{`{"type":"object","additionalProperties":false,"properties":{"name":{}}}`, ""},
		{`{"type":"object","additionalProperties":{"type":"string"},"properties":{"name":{}}}`, ""},
	} {
		got := runAssert(t, "body schema "+c.schema, resp)
		if got.Pass {
			t.Errorf("schema %s should fail", c.schema)
		} else if c.why != "" && !strings.Contains(got.Message, c.why) {
			t.Errorf("schema %s: %q, want %q", c.schema, got.Message, c.why)
		}
	}
	if got := runAssert(t, `body schema {"type":"object","properties":{"name":{"pattern":"^a","minLength":2}}}`, resp); !got.Pass {
		t.Errorf("a matching schema passes: %s", got.Message)
	}
	// The kinds of JSON value.
	for _, c := range []struct {
		in   any
		want string
	}{{nil, "null"}, {true, "boolean"}, {1.5, "number"}, {"s", "string"}, {[]any{}, "array"}, {map[string]any{}, "object"}, {3, "unknown"}} {
		if got := schemaTypeOf(c.in); got != c.want {
			t.Errorf("schemaTypeOf(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	// A schema nested too deep stops with a message.
	deep := map[string]any{"type": "object"}
	node := deep
	val := map[string]any{}
	cur := val
	for i := 0; i < maxSchemaDepth+2; i++ {
		child := map[string]any{"type": "object"}
		node["properties"] = map[string]any{"a": child}
		node = child
		next := map[string]any{}
		cur["a"] = next
		cur = next
	}
	if errs := ValidateSchema(deep, val); len(errs) == 0 || !strings.Contains(strings.Join(errs, ";"), "too deep") {
		t.Errorf("deep schema = %v", errs)
	}
}

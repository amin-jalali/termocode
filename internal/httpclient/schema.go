package httpclient

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A small JSON Schema subset for `# @assert body [$.path] schema {…}`
// (API-8): type (one or a list), enum, const, required, properties,
// additionalProperties (false or a schema), items, minItems, maxItems,
// minLength, maxLength, pattern, minimum, maximum. Other keywords are
// ignored. Nothing is fetched: $ref is not followed.

// maxSchemaDepth caps how deep a schema (and the value) is walked.
const maxSchemaDepth = 32

// maxSchemaErrors caps the reasons one check collects.
const maxSchemaErrors = 5

// parseSchema reads a schema's JSON text.
func parseSchema(s string) (map[string]any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("schema is not JSON: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema must be a JSON object")
	}
	return m, nil
}

// ValidateSchema checks v against schema and returns the reasons it
// does not match (nil = it matches).
func ValidateSchema(schema map[string]any, v any) []string {
	var errs []string
	validateSchema(schema, v, "$", 0, &errs)
	return errs
}

func validateSchema(s map[string]any, v any, at string, depth int, errs *[]string) {
	if len(*errs) >= maxSchemaErrors {
		return
	}
	if depth > maxSchemaDepth {
		*errs = append(*errs, at+": too deep")
		return
	}
	fail := func(f string, a ...any) {
		if len(*errs) < maxSchemaErrors {
			*errs = append(*errs, at+": "+fmt.Sprintf(f, a...))
		}
	}
	if t, ok := s["type"]; ok {
		var types []string
		switch x := t.(type) {
		case string:
			types = []string{x}
		case []any:
			for _, e := range x {
				if str, ok := e.(string); ok {
					types = append(types, str)
				}
			}
		}
		if len(types) > 0 && !slices.ContainsFunc(types, func(t string) bool { return schemaTypeIs(t, v) }) {
			fail("want %s, got %s", strings.Join(types, " or "), schemaTypeOf(v))
			return
		}
	}
	if e, ok := s["enum"].([]any); ok {
		if !slices.ContainsFunc(e, func(x any) bool { return jsonText(x) == jsonText(v) && schemaTypeOf(x) == schemaTypeOf(v) }) {
			fail("%s is not one of the allowed values", clip(jsonText(v), 40))
		}
	}
	if c, ok := s["const"]; ok && jsonText(c) != jsonText(v) {
		fail("want %s, got %s", clip(jsonText(c), 40), clip(jsonText(v), 40))
	}
	switch x := v.(type) {
	case map[string]any:
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if name, ok := r.(string); ok {
					if _, has := x[name]; !has {
						fail("missing %q", name)
					}
				}
			}
		}
		props, _ := s["properties"].(map[string]any)
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if ps, ok := props[k].(map[string]any); ok {
				validateSchema(ps, x[k], at+"."+k, depth+1, errs)
				continue
			}
			switch ap := s["additionalProperties"].(type) {
			case bool:
				if !ap {
					fail("%q is not allowed", k)
				}
			case map[string]any:
				validateSchema(ap, x[k], at+"."+k, depth+1, errs)
			}
		}
	case []any:
		if n, ok := schemaNum(s, "minItems"); ok && float64(len(x)) < n {
			fail("%d items, want at least %s", len(x), fmtNum(n))
		}
		if n, ok := schemaNum(s, "maxItems"); ok && float64(len(x)) > n {
			fail("%d items, want at most %s", len(x), fmtNum(n))
		}
		if is, ok := s["items"].(map[string]any); ok {
			for i, e := range x {
				validateSchema(is, e, at+"["+strconv.Itoa(i)+"]", depth+1, errs)
			}
		}
	case string:
		n := float64(len([]rune(x)))
		if m, ok := schemaNum(s, "minLength"); ok && n < m {
			fail("length %s, want at least %s", fmtNum(n), fmtNum(m))
		}
		if m, ok := schemaNum(s, "maxLength"); ok && n > m {
			fail("length %s, want at most %s", fmtNum(n), fmtNum(m))
		}
		if p, ok := s["pattern"].(string); ok {
			if len(p) > maxAssertPattern {
				fail("pattern too long")
			} else if re, err := regexp.Compile(p); err != nil {
				fail("bad pattern: %v", err)
			} else if !re.MatchString(x) {
				fail("%q does not match %s", clip(x, 40), p)
			}
		}
	case float64:
		if m, ok := schemaNum(s, "minimum"); ok && x < m {
			fail("%s is below %s", fmtNum(x), fmtNum(m))
		}
		if m, ok := schemaNum(s, "maximum"); ok && x > m {
			fail("%s is above %s", fmtNum(x), fmtNum(m))
		}
	}
}

func schemaNum(s map[string]any, k string) (float64, bool) {
	n, ok := s[k].(float64)
	return n, ok
}

func fmtNum(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

func schemaTypeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if x == math.Trunc(x) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func schemaTypeIs(t string, v any) bool {
	got := schemaTypeOf(v)
	return got == t || (t == "number" && got == "integer")
}

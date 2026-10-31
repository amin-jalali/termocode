package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// checkSchema checks a JSON value against the common part of JSON
// Schema (TS-30): type, required, properties, items, enum. Other
// keywords are not checked (a value they would refuse passes).
func checkSchema(schema map[string]any, raw json.RawMessage) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return fmt.Errorf("not JSON: %v", err)
	}
	return checkValue(schema, v, "")
}

func checkValue(schema map[string]any, v any, path string) error {
	at := path
	if at == "" {
		at = "the result"
	}
	if t, ok := schema["type"]; ok && !typeMatches(t, v) {
		return fmt.Errorf("%s is %s, want %v", at, jsonType(v), t)
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s is not one of the allowed values", at)
		}
	}
	switch x := v.(type) {
	case map[string]any:
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if k, _ := r.(string); k != "" {
					if _, has := x[k]; !has {
						return fmt.Errorf("%s has no %q", at, k)
					}
				}
			}
		}
		if props, ok := schema["properties"].(map[string]any); ok {
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				ps, _ := props[k].(map[string]any)
				if e, has := x[k]; has && ps != nil {
					if err := checkValue(ps, e, joinPath(path, k)); err != nil {
						return err
					}
				}
			}
		}
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range x {
				if err := checkValue(items, e, fmt.Sprintf("%s[%d]", strings.TrimSpace(path), i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func joinPath(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

func typeMatches(t any, v any) bool {
	switch tt := t.(type) {
	case string:
		return oneType(tt, v)
	case []any:
		for _, x := range tt {
			if s, ok := x.(string); ok && oneType(s, v) {
				return true
			}
		}
		return false
	}
	return true // an odd "type": not checked
}

func oneType(t string, v any) bool {
	switch t {
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		f, err := n.Float64()
		return err == nil && f == math.Trunc(f)
	case "number":
		_, ok := v.(json.Number)
		return ok
	}
	return jsonType(v) == t
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
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

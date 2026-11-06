package expr

import (
	"encoding/json"
	"math"
	"strconv"
)

// Kind is the JSON type of a Value.
type Kind int

const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindList
	KindMap
)

func (k Kind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindList:
		return "list"
	case KindMap:
		return "map"
	}
	return "null"
}

// Value is the result of an expression. It always holds a JSON-shaped
// Go value: nil, bool, float64, string, []any or map[string]any.
// Nested values of a list or map come from the scope as they are.
type Value struct{ v any }

// Any returns the plain Go value (nil, bool, float64, string, []any,
// map[string]any).
func (v Value) Any() any { return v.v }

// Kind returns the JSON type.
func (v Value) Kind() Kind { return kindOf(v.v) }

// Truthy reports whether the value counts as true in `when`: null,
// false, 0, "" and empty lists and maps are false.
func (v Value) Truthy() bool { return truthy(v.v) }

// Text is the value as template text: strings as they are, null as "",
// numbers in short form, lists and maps as JSON.
func (v Value) Text() string { return text(v.v) }

func kindOf(x any) Kind {
	switch x.(type) {
	case bool:
		return KindBool
	case float64:
		return KindNumber
	case string:
		return KindString
	case []any:
		return KindList
	case map[string]any:
		return KindMap
	}
	return KindNull
}

func truthy(x any) bool {
	switch t := x.(type) {
	case bool:
		return t
	case float64:
		return t != 0 && !math.IsNaN(t)
	case string:
		return t != ""
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return false
}

func text(x any) string {
	switch t := x.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return formatNumber(t)
	}
	b, err := json.Marshal(x)
	if err != nil {
		return ""
	}
	return string(b)
}

func formatNumber(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// norm turns a scope value into a JSON-shaped value. Common Go types are
// mapped directly; anything else goes through encoding/json once.
func norm(x any) any {
	switch t := x.(type) {
	case nil, bool, float64, string, []any, map[string]any:
		return x
	case int:
		return float64(t)
	case int8:
		return float64(t)
	case int16:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case uint:
		return float64(t)
	case uint8:
		return float64(t)
	case uint16:
		return float64(t)
	case uint32:
		return float64(t)
	case uint64:
		return float64(t)
	case float32:
		return float64(t)
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return t.String()
		}
		return f
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(t))
		for k, s := range t {
			out[k] = s
		}
		return out
	case json.RawMessage:
		var v any
		if json.Unmarshal(t, &v) != nil {
			return nil
		}
		return v
	}
	b, err := json.Marshal(x)
	if err != nil {
		return nil
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return v
}

package ai

import "sync"

// The chat's tool list is built again for every turn (the tools hold the
// turn's callbacks), but nearly every tool's name, description and schema
// are the same as last turn. specWires keeps each tool's encoded JSON
// across turns: a spec reuses it only when its description and schema
// equal the kept copy (a deep copy, so a schema map changed in place
// later is never mistaken for the kept one).

const maxSpecWires = 1024

var specWires = struct {
	sync.Mutex
	m map[string]*specWire
}{m: map[string]*specWire{}}

type specWire struct {
	desc   string
	schema map[string]any
	wire   *toolWire
}

// wireFor is the shared toolWire for a spec with these fields.
func wireFor(name, desc string, schema map[string]any) *toolWire {
	specWires.Lock()
	defer specWires.Unlock()
	if e, ok := specWires.m[name]; ok && e.desc == desc && jsonEqual(e.schema, schema) {
		return e.wire
	}
	cp, ok := jsonCopy(schema)
	if !ok { // a value the copy does not know: encoded for each spec
		return nil
	}
	if len(specWires.m) >= maxSpecWires {
		specWires.m = map[string]*specWire{}
	}
	w := &toolWire{spec: ToolSpec{Name: name, Description: desc, Schema: cp.(map[string]any)}}
	specWires.m[name] = &specWire{desc: desc, schema: cp.(map[string]any), wire: w}
	return w
}

// jsonCopy deep-copies a JSON-like value (maps, lists, strings, numbers,
// bools, nil). ok is false for any other type.
func jsonCopy(v any) (any, bool) {
	switch x := v.(type) {
	case nil, string, bool, float64, float32, int, int64, int32, uint, uint64, uint32:
		return x, true
	case map[string]any:
		if x == nil {
			return x, true
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			c, ok := jsonCopy(e)
			if !ok {
				return nil, false
			}
			out[k] = c
		}
		return out, true
	case []any:
		if x == nil {
			return x, true
		}
		out := make([]any, len(x))
		for i, e := range x {
			c, ok := jsonCopy(e)
			if !ok {
				return nil, false
			}
			out[i] = c
		}
		return out, true
	case []string:
		if x == nil {
			return x, true
		}
		return append([]string{}, x...), true
	}
	return nil, false
}

// jsonEqual compares two JSON-like values of the kinds jsonCopy knows
// (same types, same contents; nil and empty differ like in JSON).
func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) || (x == nil) != (y == nil) {
			return false
		}
		for k, e := range x {
			f, ok := y[k]
			if !ok || !jsonEqual(e, f) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) || (x == nil) != (y == nil) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case []string:
		y, ok := b.([]string)
		if !ok || len(x) != len(y) || (x == nil) != (y == nil) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	case nil, string, bool, float64, float32, int, int64, int32, uint, uint64, uint32:
		return a == b // same dynamic type and value
	}
	return false
}

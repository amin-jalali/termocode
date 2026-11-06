package source

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// seg is one step of an items path: a field name, an index, or [*].
type seg struct {
	name  string
	index int // -1: none
	all   bool
}

const maxPathLen = 256

// parsePath parses "$", "$.a.b", "$.a[*].b", "$.a[2]". "" is "$".
func parsePath(p string) ([]seg, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "$" {
		return nil, nil
	}
	if len(p) > maxPathLen {
		return nil, fmt.Errorf("path is longer than %d characters", maxPathLen)
	}
	if !strings.HasPrefix(p, "$") {
		return nil, fmt.Errorf("path %q must start with $", p)
	}
	rest := p[1:]
	var segs []seg
	for rest != "" {
		switch rest[0] {
		case '.':
			rest = rest[1:]
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				end = len(rest)
			}
			name := rest[:end]
			if name == "" {
				return nil, fmt.Errorf("path %q has an empty name", p)
			}
			segs = append(segs, seg{name: name, index: -1})
			rest = rest[end:]
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, fmt.Errorf("path %q is missing ]", p)
			}
			in := rest[1:end]
			rest = rest[end+1:]
			if in == "*" {
				segs = append(segs, seg{index: -1, all: true})
				continue
			}
			n, err := strconv.Atoi(in)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("path %q: [%s] must be [*] or a number", p, in)
			}
			segs = append(segs, seg{index: n})
		default:
			return nil, fmt.Errorf("path %q: want . or [ after %q", p, p[:len(p)-len(rest)])
		}
	}
	return segs, nil
}

// Select returns the values at path in doc (see Shape.Items).
func Select(doc []byte, path string) ([]json.RawMessage, error) {
	segs, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	if !json.Valid(doc) {
		return nil, fmt.Errorf("the answer is not valid JSON")
	}
	vals, err := selectSegs(doc, segs)
	if err != nil {
		return nil, err
	}
	fan := false
	for _, s := range segs {
		fan = fan || s.all
	}
	if !fan && len(vals) == 1 {
		if arr, ok := asArray(vals[0]); ok {
			return arr, nil
		}
	}
	return vals, nil
}

// selectSegs walks segs; a missing field or index drops that branch.
func selectSegs(doc []byte, segs []seg) ([]json.RawMessage, error) {
	cur := []json.RawMessage{json.RawMessage(bytes.TrimSpace(doc))}
	for _, s := range segs {
		var next []json.RawMessage
		for _, v := range cur {
			switch {
			case s.name != "":
				var obj map[string]json.RawMessage
				if json.Unmarshal(v, &obj) == nil {
					if f, ok := obj[s.name]; ok {
						next = append(next, f)
					}
				}
			case s.all:
				if arr, ok := asArray(v); ok {
					next = append(next, arr...)
				}
			default:
				if arr, ok := asArray(v); ok && s.index < len(arr) {
					next = append(next, arr[s.index])
				}
			}
		}
		cur = next
	}
	out := cur[:0]
	for _, v := range cur {
		if !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			out = append(out, v)
		}
	}
	return out, nil
}

func asArray(v json.RawMessage) ([]json.RawMessage, bool) {
	v = bytes.TrimSpace(v)
	if len(v) == 0 || v[0] != '[' {
		return nil, false
	}
	var arr []json.RawMessage
	if json.Unmarshal(v, &arr) != nil {
		return nil, false
	}
	return arr, true
}

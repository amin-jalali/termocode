package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// A node is one JSON value with the offset where it starts, so every
// problem can name its line. Objects keep their key order.

type nodeKind byte

const (
	kObject nodeKind = iota + 1
	kArray
	kString
	kNumber
	kBool
	kNull
)

func (k nodeKind) String() string {
	switch k {
	case kObject:
		return "an object"
	case kArray:
		return "a list"
	case kString:
		return "a string"
	case kNumber:
		return "a number"
	case kBool:
		return "true or false"
	}
	return "null"
}

type node struct {
	kind nodeKind
	off  int
	str  string      // kString
	num  json.Number // kNumber
	b    bool        // kBool
	keys []string    // kObject, in file order
	koff []int       // kObject: offset of each key
	vals []*node     // kObject values / kArray elements
}

// maxDepth caps nesting so a hostile file cannot grow the stack.
const maxDepth = 64

type syntaxErr struct {
	off int
	msg string
}

func (e *syntaxErr) Error() string { return e.msg }

// parseTree reads one JSON value from src (comments already blanked).
func parseTree(src []byte) (*node, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()
	t := &treeReader{src: src, dec: dec}
	n, err := t.value(0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, &syntaxErr{off: t.skipSpace(int(dec.InputOffset())), msg: "more text after the end of the file's { … }"}
	}
	return n, nil
}

type treeReader struct {
	src []byte
	dec *json.Decoder
}

// skipSpace moves off past whitespace, ':' and ',' to the next token.
func (t *treeReader) skipSpace(off int) int {
	for off < len(t.src) {
		switch t.src[off] {
		case ' ', '\t', '\n', '\r', ':', ',':
			off++
		default:
			return off
		}
	}
	return off
}

func (t *treeReader) token() (json.Token, int, error) {
	off := t.skipSpace(int(t.dec.InputOffset()))
	tok, err := t.dec.Token()
	if err != nil {
		var se *json.SyntaxError
		switch {
		case errors.As(err, &se):
			return nil, int(se.Offset), &syntaxErr{off: int(se.Offset), msg: "not valid JSON: " + se.Error()}
		case err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF):
			return nil, len(t.src), &syntaxErr{off: len(t.src), msg: "the file ends too early (a } or ] is missing)"}
		}
		return nil, off, &syntaxErr{off: off, msg: "not valid JSON: " + err.Error()}
	}
	return tok, off, nil
}

func (t *treeReader) value(depth int) (*node, error) {
	tok, off, err := t.token()
	if err != nil {
		return nil, err
	}
	return t.fromToken(tok, off, depth)
}

func (t *treeReader) fromToken(tok json.Token, off, depth int) (*node, error) {
	if depth > maxDepth {
		return nil, &syntaxErr{off: off, msg: fmt.Sprintf("nested more than %d levels deep", maxDepth)}
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			n := &node{kind: kObject, off: off}
			seen := map[string]bool{}
			for t.dec.More() {
				ktok, koff, err := t.token()
				if err != nil {
					return nil, err
				}
				k, ok := ktok.(string)
				if !ok {
					return nil, &syntaxErr{off: koff, msg: "not valid JSON: a key must be a string"}
				}
				if seen[k] {
					return nil, &syntaxErr{off: koff, msg: fmt.Sprintf("the key %s appears twice in one object", quote(k))}
				}
				seen[k] = true
				val, err := t.value(depth + 1)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, k)
				n.koff = append(n.koff, koff)
				n.vals = append(n.vals, val)
			}
			if _, _, err := t.token(); err != nil { // '}'
				return nil, err
			}
			return n, nil
		case '[':
			n := &node{kind: kArray, off: off}
			for t.dec.More() {
				val, err := t.value(depth + 1)
				if err != nil {
					return nil, err
				}
				n.vals = append(n.vals, val)
			}
			if _, _, err := t.token(); err != nil { // ']'
				return nil, err
			}
			return n, nil
		}
		return nil, &syntaxErr{off: off, msg: "not valid JSON: unexpected " + string(v)}
	case string:
		return &node{kind: kString, off: off, str: v}, nil
	case json.Number:
		return &node{kind: kNumber, off: off, num: v}, nil
	case bool:
		return &node{kind: kBool, off: off, b: v}, nil
	case nil:
		return &node{kind: kNull, off: off}, nil
	}
	return nil, &syntaxErr{off: off, msg: "not valid JSON"}
}

// plain turns a node into a Go value: map[string]any, []any, string,
// float64, bool or nil.
func (n *node) plain() any {
	switch n.kind {
	case kObject:
		m := make(map[string]any, len(n.keys))
		for i, k := range n.keys {
			m[k] = n.vals[i].plain()
		}
		return m
	case kArray:
		l := make([]any, len(n.vals))
		for i, v := range n.vals {
			l[i] = v.plain()
		}
		return l
	case kString:
		return n.str
	case kNumber:
		f, _ := n.num.Float64()
		return f
	case kBool:
		return n.b
	}
	return nil
}

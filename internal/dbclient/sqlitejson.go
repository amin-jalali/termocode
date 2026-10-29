package dbclient

import (
	"encoding/json"
	"strings"
)

// parseSQLiteJSON reads the rows sqlite3 prints in -json mode. A fast scan
// handles what sqlite3 prints (an array of flat objects of null, number
// and string values) with one string for the whole output and no decoder
// per cell; anything else goes through parseSQLiteJSONDecoder, which
// gives the same rows (or the same error) for every input.
func parseSQLiteJSON(lines []string) ([][]Cell, []Column, error) {
	n := 0
	for _, line := range lines {
		n += len(line) + 1
	}
	var b strings.Builder
	b.Grow(n)
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "changes:") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	text := b.String()
	if rows, cols, ok := scanSQLiteJSON(text); ok {
		return rows, cols, nil
	}
	return parseSQLiteJSONDecoder(lines)
}

// scanSQLiteJSON is the fast path of parseSQLiteJSON. ok is false when the
// text holds anything it does not handle exactly as encoding/json would.
// Cells share the memory of text.
func scanSQLiteJSON(text string) (rows [][]Cell, cols []Column, ok bool) {
	p := jsonScan{s: text}
	p.ws()
	if p.i == len(p.s) {
		return nil, nil, true // no output: no rows
	}
	if !p.byte('[') {
		return nil, nil, false
	}
	width := 0
	var cells []Cell // one backing array for many rows
	for {
		p.ws()
		if p.peek() == ']' {
			return rows, cols, true
		}
		if len(rows) > 0 && !p.byte(',') {
			return nil, nil, false
		}
		p.ws()
		if !p.byte('{') {
			return nil, nil, false
		}
		if cap(cells)-len(cells) < max(width, 1) {
			cells = make([]Cell, 0, max(width, 1)*256)
		}
		start := len(cells)
		for first := true; ; first = false {
			p.ws()
			if p.peek() == '}' {
				p.i++
				break
			}
			if !first && !p.byte(',') {
				return nil, nil, false
			}
			p.ws()
			name, okKey := p.str()
			if !okKey {
				return nil, nil, false
			}
			p.ws()
			if !p.byte(':') {
				return nil, nil, false
			}
			p.ws()
			cell, okVal := p.value()
			if !okVal {
				return nil, nil, false
			}
			cells = append(cells, cell)
			if len(rows) == 0 {
				cols = append(cols, Column{Name: name})
			}
		}
		row := cells[start:len(cells):len(cells)]
		if len(row) == 0 {
			row = nil // as the decoder path: a row with no cells is nil
		}
		rows = append(rows, row)
		if len(rows) == 1 {
			width = len(row)
		}
	}
}

type jsonScan struct {
	s string
	i int
}

func (p *jsonScan) ws() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonScan) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *jsonScan) byte(c byte) bool {
	if p.peek() != c {
		return false
	}
	p.i++
	return true
}

// str reads a JSON string. Plain ASCII without escapes is a slice of the
// text; anything else is decoded by encoding/json (escapes, invalid UTF-8).
func (p *jsonScan) str() (string, bool) {
	if !p.byte('"') {
		return "", false
	}
	start := p.i
	plain := true
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			if plain {
				return p.s[start : p.i-1], true
			}
			var v string
			if json.Unmarshal([]byte(p.s[start-1:p.i]), &v) != nil {
				return "", false
			}
			return v, true
		case c == '\\':
			plain = false
			p.i += 2
			continue
		case c < 0x20:
			return "", false // invalid JSON: the decoder reports it
		case c >= 0x80:
			plain = false
		}
		p.i++
	}
	return "", false
}

// value reads one cell value: null, a number, a string, true or false.
func (p *jsonScan) value() (Cell, bool) {
	switch c := p.peek(); {
	case c == '"':
		s, ok := p.str()
		return Cell{Text: s}, ok
	case c == 'n':
		if strings.HasPrefix(p.s[p.i:], "null") {
			p.i += 4
			return Cell{Null: true}, true
		}
	case c == 't':
		if strings.HasPrefix(p.s[p.i:], "true") {
			p.i += 4
			return Cell{Text: "true"}, true
		}
	case c == 'f':
		if strings.HasPrefix(p.s[p.i:], "false") {
			p.i += 5
			return Cell{Text: "false"}, true
		}
	case c == '-' || c >= '0' && c <= '9':
		start := p.i
		if !p.number() {
			return Cell{}, false
		}
		return Cell{Text: p.s[start:p.i]}, true
	}
	return Cell{}, false
}

// number reads a JSON number literal (the grammar of RFC 8259).
func (p *jsonScan) number() bool {
	p.byte('-')
	switch c := p.peek(); {
	case c == '0':
		p.i++
	case c >= '1' && c <= '9':
		p.digits()
	default:
		return false
	}
	if p.byte('.') {
		if !p.digits() {
			return false
		}
	}
	if c := p.peek(); c == 'e' || c == 'E' {
		p.i++
		if c := p.peek(); c == '+' || c == '-' {
			p.i++
		}
		if !p.digits() {
			return false
		}
	}
	// A literal runs into the next token: "12x" is not a number.
	if p.i == len(p.s) {
		return true
	}
	switch p.s[p.i] {
	case ',', '}', ']', ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

func (p *jsonScan) digits() bool {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	return p.i > start
}

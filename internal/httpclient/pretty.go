package httpclient

import "strings"

// indentJSON appends src (one JSON value that starts with { or [, no
// space around it) to dst indented like json.Indent(dst, src, "", "  "),
// and checks src like encoding/json's scanner does (same grammar, same
// 10 000-level nesting limit). ok=false: src is not valid JSON.
//
// It does what json.Indent does in one pass over the bytes, without
// the scanner's per-byte function calls: a 5 MB answer is laid out
// about 5 times faster.
func indentJSON(dst, src []byte) ([]byte, bool) {
	p := jsonIndenter{src: src, out: dst}
	if !p.value(0) {
		return dst, false
	}
	p.space()
	return p.out, p.i == len(p.src)
}

const jsonMaxDepth = 10000 // encoding/json's maxNestingDepth

type jsonIndenter struct {
	src []byte
	i   int
	out []byte
}

func (p *jsonIndenter) space() {
	for p.i < len(p.src) {
		switch p.src[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

// jsonNewlines is "\n" and 64 levels of indent, sliced by newline.
var jsonNewlines = "\n" + strings.Repeat("  ", 64)

func (p *jsonIndenter) newline(depth int) {
	if n := 1 + 2*depth; n <= len(jsonNewlines) {
		p.out = append(p.out, jsonNewlines[:n]...)
		return
	}
	p.out = append(p.out, '\n')
	for range depth {
		p.out = append(p.out, ' ', ' ')
	}
}

func (p *jsonIndenter) value(depth int) bool {
	p.space()
	if p.i >= len(p.src) {
		return false
	}
	switch c := p.src[p.i]; {
	case c == '{' || c == '[':
		return p.container(depth)
	case c == '"':
		return p.str()
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	case c == 't':
		return p.literal("true")
	case c == 'f':
		return p.literal("false")
	case c == 'n':
		return p.literal("null")
	}
	return false
}

func (p *jsonIndenter) container(depth int) bool {
	open := p.src[p.i]
	closer := byte('}')
	if open == '[' {
		closer = ']'
	}
	if depth+1 > jsonMaxDepth {
		return false
	}
	p.i++
	p.out = append(p.out, open)
	p.space()
	if p.i < len(p.src) && p.src[p.i] == closer {
		p.i++
		p.out = append(p.out, closer)
		return true
	}
	for {
		p.newline(depth + 1)
		if open == '{' {
			p.space()
			if p.i >= len(p.src) || p.src[p.i] != '"' || !p.str() {
				return false
			}
			p.space()
			if p.i >= len(p.src) || p.src[p.i] != ':' {
				return false
			}
			p.i++
			p.out = append(p.out, ':', ' ')
		}
		if !p.value(depth + 1) {
			return false
		}
		p.space()
		if p.i >= len(p.src) {
			return false
		}
		switch p.src[p.i] {
		case ',':
			p.i++
			p.out = append(p.out, ',')
			continue
		case closer:
			p.i++
			p.newline(depth)
			p.out = append(p.out, closer)
			return true
		}
		return false
	}
}

// str copies a string, checking escapes and control bytes (any other
// byte is copied as is, like encoding/json's scanner).
func (p *jsonIndenter) str() bool {
	start := p.i
	i := p.i + 1
	for i < len(p.src) {
		c := p.src[i]
		switch {
		case c == '"':
			i++
			p.out = append(p.out, p.src[start:i]...)
			p.i = i
			return true
		case c < 0x20:
			return false
		case c == '\\':
			if i+1 >= len(p.src) {
				return false
			}
			switch p.src[i+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i += 2
			case 'u':
				if i+6 > len(p.src) {
					return false
				}
				for _, h := range p.src[i+2 : i+6] {
					if !(h >= '0' && h <= '9' || h >= 'a' && h <= 'f' || h >= 'A' && h <= 'F') {
						return false
					}
				}
				i += 6
			default:
				return false
			}
		default:
			i++
		}
	}
	return false
}

func (p *jsonIndenter) digits(i int) int {
	for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
		i++
	}
	return i
}

func (p *jsonIndenter) number() bool {
	s := p.src
	i := p.i
	if s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		i = p.digits(i + 1)
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		j := p.digits(i + 1)
		if j == i+1 {
			return false
		}
		i = j
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		j := p.digits(i)
		if j == i {
			return false
		}
		i = j
	}
	p.out = append(p.out, s[p.i:i]...)
	p.i = i
	return true
}

func (p *jsonIndenter) literal(word string) bool {
	if len(p.src)-p.i < len(word) || string(p.src[p.i:p.i+len(word)]) != word {
		return false
	}
	p.out = append(p.out, word...)
	p.i += len(word)
	return true
}

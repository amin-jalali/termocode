package jsonc

// Strip removes // and /* */ comments (outside strings) and trailing
// commas before } or ], so VSCode-style launch.json files parse.
func Strip(src []byte) []byte {
	var out []byte
	inStr, esc := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		out = append(out, c)
	}
	// Trailing commas: ",<ws>}" / ",<ws>]" → drop the comma.
	res := make([]byte, 0, len(out))
	inStr, esc = false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inStr {
			res = append(res, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		}
		if c == ',' {
			j := i + 1
			for j < len(out) && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				continue
			}
		}
		res = append(res, c)
	}
	return res
}

// Blank is Strip that keeps every offset: comments and trailing commas
// become spaces (newlines in a block comment stay), so the result has the
// length of src and the same lines. A JSON decoder's error offset in the
// result is the offset in src.
func Blank(src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)
	inStr, esc := false, false
	blank := func(i int) {
		if out[i] != '\n' {
			out[i] = ' '
		}
	}
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for i < len(out) && out[i] != '\n' {
				blank(i)
				i++
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			blank(i)
			blank(i + 1)
			i += 2
			for i < len(out) && !(out[i] == '*' && i+1 < len(out) && out[i+1] == '/') {
				blank(i)
				i++
			}
			if i < len(out) {
				blank(i)
				blank(i + 1)
				i++
			}
		}
	}
	// Trailing commas: ",<ws>}" / ",<ws>]" → a space. Comments are spaces
	// now, so the whitespace run may span them.
	inStr, esc = false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(out) && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				out[i] = ' '
			}
		}
	}
	return out
}

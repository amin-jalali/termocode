package httpclient

// IndentPrefix pretty-prints JSON like json.Indent(dst, src, "", "  ")
// but stops once the output reaches max bytes, so a 5 MB body costs only
// the part that is shown. It returns the output (at most max bytes, cut
// at a byte boundary of the indented text), whether src was read to the
// end, and ok=false when src does not look like JSON (then the caller
// shows the raw text). It does not validate fully; for a complete,
// valid document the output equals json.Indent's.
func IndentPrefix(src []byte, max int) (out []byte, complete, ok bool) {
	i := 0
	for i < len(src) && isSpace(src[i]) {
		i++
	}
	if i == len(src) || (src[i] != '{' && src[i] != '[') {
		return nil, false, false
	}
	buf := make([]byte, 0, min(max, len(src)*2)+16)
	depth := 0
	newline := func() {
		buf = append(buf, '\n')
		for d := 0; d < depth; d++ {
			buf = append(buf, ' ', ' ')
		}
	}
	// emptyAfter reports whether the next non-space byte closes the
	// current container ({} and [] stay on one line, like json.Indent).
	emptyAfter := func(j int, closer byte) bool {
		for j < len(src) && isSpace(src[j]) {
			j++
		}
		return j < len(src) && src[j] == closer
	}
	for ; i < len(src); i++ {
		if len(buf) >= max {
			return buf[:max], false, true
		}
		c := src[i]
		switch {
		case isSpace(c):
		case c == '"':
			j := i + 1
			for j < len(src) {
				if src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == '"' {
					break
				}
				j++
			}
			if j >= len(src) {
				buf = append(buf, src[i:]...)
				i = len(src)
				break
			}
			buf = append(buf, src[i:j+1]...)
			i = j
		case c == '{' || c == '[':
			closer := byte('}')
			if c == '[' {
				closer = ']'
			}
			buf = append(buf, c)
			if emptyAfter(i+1, closer) {
				for i+1 < len(src) && src[i+1] != closer {
					i++
				}
				i++
				buf = append(buf, closer)
				break
			}
			depth++
			newline()
		case c == '}' || c == ']':
			depth--
			if depth < 0 {
				return nil, false, false
			}
			newline()
			buf = append(buf, c)
		case c == ',':
			buf = append(buf, ',')
			newline()
		case c == ':':
			buf = append(buf, ':', ' ')
		default:
			buf = append(buf, c)
		}
	}
	if len(buf) > max {
		return buf[:max], false, true
	}
	return buf, depth == 0, true
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

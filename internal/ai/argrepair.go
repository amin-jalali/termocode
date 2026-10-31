package ai

import (
	"encoding/json"
	"strings"
)

// RepairToolArgs reads a tool call's arguments that are not valid JSON
// because of a small slip (OMX-25): a ```json fence around them, the
// object sent as a JSON string, a trailing comma, a raw newline or tab
// inside a string. Arguments that were cut (an open string, a missing
// brace) are never "repaired": the call would run with half its input.
// ok=false when s is not a repairable object.
func RepairToolArgs(s string) (map[string]any, bool) {
	return repairArgs(s, true)
}

func repairArgs(s string, unquote bool) (map[string]any, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "\ufeff"))
	if rest, ok := strings.CutPrefix(s, "```"); ok {
		nl := strings.IndexByte(rest, '\n')
		body, closed := strings.CutSuffix(strings.TrimSpace(rest), "```")
		if nl < 0 || !closed {
			return nil, false
		}
		s = strings.TrimSpace(body[strings.IndexByte(body, '\n')+1:])
	}
	if unquote && strings.HasPrefix(s, `"`) {
		var inner string
		if json.Unmarshal([]byte(s), &inner) != nil {
			return nil, false
		}
		return repairArgs(inner, false)
	}
	if !strings.HasPrefix(s, "{") {
		return nil, false
	}
	fixed, ok := fixJSONSlips(s)
	if !ok {
		return nil, false
	}
	out := map[string]any{}
	if json.Unmarshal([]byte(fixed), &out) != nil {
		return nil, false
	}
	return out, true
}

// fixJSONSlips drops trailing commas and escapes raw control characters
// in strings. ok=false when s ends inside a string or with open
// brackets (a cut stream).
func fixJSONSlips(s string) (string, bool) {
	var b strings.Builder
	b.Grow(len(s) + 8)
	inStr, esc, depth := false, false, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			case c == '\n':
				b.WriteString(`\n`)
				continue
			case c == '\r':
				b.WriteString(`\r`)
				continue
			case c == '\t':
				b.WriteString(`\t`)
				continue
			case c < 0x20:
				continue // other control bytes carry nothing
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ',':
			j := i + 1
			for j < len(s) && strings.IndexByte(" \t\r\n", s[j]) >= 0 {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // a trailing comma
			}
		}
		b.WriteByte(c)
	}
	if inStr || depth != 0 {
		return "", false
	}
	return b.String(), true
}

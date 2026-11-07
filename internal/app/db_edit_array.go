package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Array cells (DBX-55): e on a PostgreSQL array opens it in a buffer with
// one element per line; :w writes the lines back as an array literal.
// A line \N is a NULL element; a line in double quotes keeps the text
// inside as it is (an empty text, spaces, the word NULL). Empty lines are
// left out. Arrays in arrays keep the one-line editor.

// dbArrayBase is the element type of an array column type ("int4[]",
// "_int4", "integer ARRAY"); ok=false: not an array.
func dbArrayBase(typ string) (string, bool) {
	t := strings.ToLower(strings.TrimSpace(dbTypeArgs.ReplaceAllString(typ, "")))
	switch {
	case strings.HasSuffix(t, "[]"):
		return strings.TrimSpace(strings.TrimRight(t, "[] ")), true
	case strings.HasSuffix(t, " array"):
		return strings.TrimSpace(strings.TrimSuffix(t, " array")), true
	case strings.HasPrefix(t, "_") && len(t) > 1:
		return t[1:], true
	}
	return "", false
}

// dbArrayParse splits a one-level array literal {a,"b c",NULL} into its
// elements (nil: a NULL element). false: not one, or an array in an array.
func dbArrayParse(text string) ([]*string, bool) {
	s := strings.TrimSpace(text)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, false
	}
	s = s[1 : len(s)-1]
	var out []*string
	if strings.TrimSpace(s) == "" {
		return out, true
	}
	for i := 0; ; {
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i < len(s) && s[i] == '{' {
			return nil, false // nested
		}
		var b strings.Builder
		quoted := i < len(s) && s[i] == '"'
		if quoted {
			i++
			closed := false
			for i < len(s) {
				c := s[i]
				if c == '\\' && i+1 < len(s) {
					b.WriteByte(s[i+1])
					i += 2
					continue
				}
				if c == '"' {
					i++
					closed = true
					break
				}
				b.WriteByte(c)
				i++
			}
			if !closed {
				return nil, false
			}
			for i < len(s) && s[i] == ' ' {
				i++
			}
		} else {
			for i < len(s) && s[i] != ',' {
				if s[i] == '"' || s[i] == '{' || s[i] == '}' {
					return nil, false
				}
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
				i++
			}
		}
		v := b.String()
		if !quoted {
			v = strings.TrimSpace(v)
		}
		if !quoted && strings.EqualFold(v, "NULL") {
			out = append(out, nil)
		} else {
			out = append(out, &v)
		}
		if i >= len(s) {
			return out, true
		}
		if s[i] != ',' {
			return nil, false
		}
		i++
	}
}

// dbArrayLines are the buffer lines for an array cell; false: the cell is
// not a one-level array of the column's type (a NULL or new cell, too).
func dbArrayLines(typ string, cur dbclient.Cell, has bool) ([]string, bool) {
	if _, ok := dbArrayBase(typ); !ok || !has || cur.Null {
		return nil, false
	}
	elems, ok := dbArrayParse(cur.Text)
	if !ok {
		return nil, false
	}
	lines := []string{}
	for _, e := range elems {
		if e == nil {
			lines = append(lines, `\N`)
			continue
		}
		v := *e
		if strings.ContainsAny(v, "\n\r") {
			return nil, false // one element per line cannot hold it
		}
		if v == "" || v == `\N` || strings.EqualFold(v, "NULL") || strings.TrimSpace(v) != v || len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = `"` + v + `"`
		}
		lines = append(lines, v)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines, true
}

// dbArrayLiteral writes the buffer lines back as an array literal.
func dbArrayLiteral(lines []string) string {
	var parts []string
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.TrimSpace(l) == "":
			continue
		case l == `\N`:
			parts = append(parts, "NULL")
			continue
		case len(l) >= 2 && l[0] == '"' && l[len(l)-1] == '"':
			l = l[1 : len(l)-1]
		}
		if l == "" || strings.EqualFold(l, "NULL") || strings.ContainsAny(l, "{},\"\\ \t") {
			l = `"` + strings.ReplaceAll(strings.ReplaceAll(l, `\`, `\\`), `"`, `\"`) + `"`
		}
		parts = append(parts, l)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// dbCheckArrayType checks each element of an array value against the
// element type; "" when it fits (or is not checked).
func dbCheckArrayType(base, value string) string {
	elems, ok := dbArrayParse(value)
	if !ok {
		if strings.HasPrefix(strings.TrimSpace(value), "{") {
			return "" // an array in an array: the server checks it
		}
		return "an array, such as {1,2,3}"
	}
	for _, e := range elems {
		if e == nil {
			continue
		}
		if err := dbCheckCellType(base, *e); err != nil {
			return "an array whose elements are " + err.Error()
		}
	}
	return ""
}

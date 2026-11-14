package dbclient

import (
	"strings"
)

// ToMongoDocs writes a MongoDB result as shell documents, one per row, one
// field per line (DBX-68): the document view of the JSON tab. Numbers,
// booleans and shell literals (ObjectId) are bare, nested values nested,
// times ISODate, binary BinData; a NULL cell (a field the document lacks)
// is left out.
func ToMongoDocs(r Result) string {
	var b strings.Builder
	for ri, row := range r.Rows {
		if ri > 0 {
			b.WriteByte('\n')
		}
		b.WriteByte('{')
		first := true
		for i, c := range r.Columns {
			if i >= len(row) || row[i].Null {
				continue
			}
			if !first {
				b.WriteByte(',')
			}
			first = false
			b.WriteString("\n  ")
			b.WriteString(mongoQuote(c.Name))
			b.WriteString(": ")
			b.WriteString(mongoIndent(mongoDocValue(row[i]), "  "))
		}
		if !first {
			b.WriteByte('\n')
		}
		b.WriteByte('}')
	}
	return b.String()
}

// mongoDocValue is one cell as shell syntax.
func mongoDocValue(c Cell) string {
	switch c.Kind {
	case "json", "literal", "number", "bool":
		return c.Text
	case "time":
		return `ISODate("` + c.Text + `")`
	case "binary":
		return `BinData(0, "` + strings.TrimPrefix(c.Text, "0x") + `")`
	}
	return mongoQuote(c.Text)
}

// mongoIndent lays compact shell JSON out one value per line, each level
// two spaces deeper than base. Strings are copied whole.
func mongoIndent(s, base string) string {
	if !strings.ContainsAny(s, "{[") {
		return s
	}
	var b strings.Builder
	depth := 0
	line := func() {
		b.WriteByte('\n')
		b.WriteString(base)
		b.WriteString(strings.Repeat("  ", depth))
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '"':
			j := i + 1
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				j = len(s) - 1
			}
			b.WriteString(s[i : j+1])
			i = j
		case '{', '[':
			closer := byte('}')
			if ch == '[' {
				closer = ']'
			}
			if i+1 < len(s) && s[i+1] == closer {
				b.WriteByte(ch)
				b.WriteByte(closer)
				i++
				continue
			}
			b.WriteByte(ch)
			depth++
			line()
		case '}', ']':
			depth--
			line()
			b.WriteByte(ch)
		case ',':
			b.WriteByte(',')
			line()
			for i+1 < len(s) && s[i+1] == ' ' {
				i++
			}
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

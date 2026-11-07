package dbclient

import (
	"errors"
	"fmt"
	"strings"
)

// ER diagram export (DBX-47): a schema read by ReadCanonSchema written as
// Mermaid, PlantUML or Graphviz DOT text. Every table lists its columns
// (PK and FK marked); every foreign key to a table of the same schema is
// one line from the child to the parent. Names come from the server, so
// line breaks, braces and quotes are neutralized for each format.

// ERFormats are the formats ERDiagram writes, in menu order.
var ERFormats = []string{"mermaid", "plantuml", "dot"}

// erClean drops line breaks and control characters and turns braces into
// parentheses (they open and close blocks in every format).
func erClean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return ' '
		case r == '{':
			return '('
		case r == '}':
			return ')'
		}
		return r
	}, s)
}

// erIdent is a Mermaid word: letters, digits and _ only.
func erIdent(s string) string {
	b := []rune(s)
	for i, r := range b {
		if !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			b[i] = '_'
		}
	}
	out := strings.Trim(string(b), "_")
	if out == "" {
		out = "_"
	}
	return out
}

func erQuote(s string) string { return strings.ReplaceAll(erClean(s), `"`, "'") }

func erDot(s string) string {
	s = strings.ReplaceAll(erClean(s), `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

type erColumn struct {
	name, typ string
	pk, fk    bool
}

type erEdge struct {
	child, parent int
	name          string
}

// erModel is the part of a schema a diagram shows.
func erModel(s CanonSchema) ([][]erColumn, []erEdge) {
	index := map[string]int{}
	for i, t := range s.Tables {
		index[t.Name] = i
	}
	cols := make([][]erColumn, len(s.Tables))
	var edges []erEdge
	for i, t := range s.Tables {
		pk, fk := map[string]bool{}, map[string]bool{}
		for _, c := range t.Constraints {
			switch c.Kind {
			case "PRIMARY KEY":
				for _, n := range c.Columns {
					pk[n] = true
				}
			case "FOREIGN KEY":
				for _, n := range c.Columns {
					fk[n] = true
				}
				if p, ok := index[c.RefTable]; ok {
					edges = append(edges, erEdge{child: i, parent: p, name: c.Name})
				}
			}
		}
		for _, c := range t.Columns {
			cols[i] = append(cols[i], erColumn{name: c.Name, typ: c.Type, pk: pk[c.Name], fk: fk[c.Name]})
		}
	}
	return cols, edges
}

// ERDiagram writes the schema's ER diagram in format (see ERFormats).
func ERDiagram(s CanonSchema, format string) (string, error) {
	cols, edges := erModel(s)
	var b strings.Builder
	switch format {
	case "mermaid":
		b.WriteString("erDiagram\n")
		for i, t := range s.Tables {
			fmt.Fprintf(&b, "  %s {\n", erIdent(t.Name))
			for _, c := range cols[i] {
				fmt.Fprintf(&b, "    %s %s", erIdent(c.typ), erIdent(c.name))
				switch {
				case c.pk && c.fk:
					b.WriteString(" PK, FK")
				case c.pk:
					b.WriteString(" PK")
				case c.fk:
					b.WriteString(" FK")
				}
				b.WriteString("\n")
			}
			b.WriteString("  }\n")
		}
		for _, e := range edges {
			fmt.Fprintf(&b, "  %s ||--o{ %s : \"%s\"\n", erIdent(s.Tables[e.parent].Name), erIdent(s.Tables[e.child].Name), erQuote(e.name))
		}
	case "plantuml":
		b.WriteString("@startuml\n")
		for i, t := range s.Tables {
			fmt.Fprintf(&b, "entity \"%s\" as t%d {\n", erQuote(t.Name), i)
			for _, c := range cols[i] {
				b.WriteString("  ")
				if c.pk {
					b.WriteString("* ")
				}
				fmt.Fprintf(&b, "%s : %s", erQuote(c.name), erQuote(c.typ))
				if c.pk {
					b.WriteString(" <<PK>>")
				}
				if c.fk {
					b.WriteString(" <<FK>>")
				}
				b.WriteString("\n")
			}
			b.WriteString("}\n")
		}
		for _, e := range edges {
			fmt.Fprintf(&b, "t%d ||--o{ t%d : %s\n", e.parent, e.child, erQuote(e.name))
		}
		b.WriteString("@enduml\n")
	case "dot":
		b.WriteString("digraph er {\n  rankdir=LR;\n")
		for i, t := range s.Tables {
			label := erDot(t.Name) + `\l\l`
			for _, c := range cols[i] {
				label += erDot(c.name) + " : " + erDot(c.typ)
				if c.pk {
					label += " PK"
				}
				if c.fk {
					label += " FK"
				}
				label += `\l`
			}
			fmt.Fprintf(&b, "  \"%s\" [shape=box, label=\"%s\"];\n", erDot(t.Name), label)
		}
		for _, e := range edges {
			fmt.Fprintf(&b, "  \"%s\" -> \"%s\" [label=\"%s\"];\n", erDot(s.Tables[e.child].Name), erDot(s.Tables[e.parent].Name), erDot(e.name))
		}
		b.WriteString("}\n")
	default:
		return "", errors.New("unknown diagram format " + format + " (mermaid, plantuml or dot)")
	}
	return b.String(), nil
}

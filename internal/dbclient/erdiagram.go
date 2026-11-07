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
	virtual       bool // a virtual FK (DBX-31): a dashed line
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
					edges = append(edges, erEdge{child: i, parent: p, name: c.Name, virtual: c.Virtual})
				}
			}
		}
		for _, c := range t.Columns {
			cols[i] = append(cols[i], erColumn{name: c.Name, typ: c.Type, pk: pk[c.Name], fk: fk[c.Name]})
		}
	}
	return cols, edges
}

// erLine is the Mermaid / PlantUML line of an edge: solid, or dotted for a
// virtual key.
func erLine(e erEdge) string {
	if e.virtual {
		return ".."
	}
	return "--"
}

// ERStyle changes what a diagram shows. Colors gives every table a fill
// colour by its name prefix (the part before the first "_"), so related
// tables stand out; Highlight tables get the highlight colour; Only, when
// set, keeps just those tables and the keys between them.
type ERStyle struct {
	Colors    bool
	Highlight []string
	Only      []string
}

// erColors are the prefix fills, light enough for dark text.
var erColors = []string{"#cfe8ff", "#d8f5d0", "#ffe7c2", "#f3d6ff", "#ffd6d6", "#d6fff6", "#fff6c2", "#e2e2e2"}

const erHighlight = "#ffcc66"

// erPrefix is the colour group of a table name.
func erPrefix(name string) string {
	if i := strings.Index(name, "_"); i > 0 {
		return name[:i]
	}
	return name
}

// erFills is the fill of each table ("" none).
func erFills(s CanonSchema, st ERStyle) []string {
	hi := map[string]bool{}
	for _, n := range st.Highlight {
		hi[n] = true
	}
	group := map[string]int{}
	fills := make([]string, len(s.Tables))
	for i, t := range s.Tables {
		if st.Colors {
			g, ok := group[erPrefix(t.Name)]
			if !ok {
				g = len(group)
				group[erPrefix(t.Name)] = g
			}
			fills[i] = erColors[g%len(erColors)]
		}
		if hi[t.Name] {
			fills[i] = erHighlight
		}
	}
	return fills
}

// erOnly is s with only the named tables (all when names is empty).
func erOnly(s CanonSchema, names []string) CanonSchema {
	if len(names) == 0 {
		return s
	}
	keep := map[string]bool{}
	for _, n := range names {
		keep[n] = true
	}
	out := s
	out.Tables = nil
	for _, t := range s.Tables {
		if keep[t.Name] {
			out.Tables = append(out.Tables, t)
		}
	}
	return out
}

// ERPath is the shortest foreign-key path from table from to table to,
// following keys in either direction; ok is false when there is none.
func ERPath(s CanonSchema, from, to string) ([]string, bool) {
	index := map[string]int{}
	for i, t := range s.Tables {
		index[t.Name] = i
	}
	a, okA := index[from]
	z, okZ := index[to]
	if !okA || !okZ {
		return nil, false
	}
	_, edges := erModel(s)
	next := make([][]int, len(s.Tables))
	for _, e := range edges {
		if e.child != e.parent {
			next[e.child] = append(next[e.child], e.parent)
			next[e.parent] = append(next[e.parent], e.child)
		}
	}
	prev := make([]int, len(s.Tables))
	for i := range prev {
		prev[i] = -1
	}
	prev[a] = a
	queue := []int{a}
	for len(queue) > 0 && prev[z] < 0 {
		n := queue[0]
		queue = queue[1:]
		for _, x := range next[n] {
			if prev[x] < 0 {
				prev[x] = n
				queue = append(queue, x)
			}
		}
	}
	if prev[z] < 0 {
		return nil, false
	}
	var path []string
	for n := z; ; n = prev[n] {
		path = append([]string{s.Tables[n].Name}, path...)
		if n == a {
			break
		}
	}
	return path, true
}

// ERDiagram writes the schema's ER diagram in format (see ERFormats).
func ERDiagram(s CanonSchema, format string) (string, error) {
	return ERDiagramStyled(s, format, ERStyle{})
}

// ERDiagramStyled is ERDiagram with colours and a table filter.
func ERDiagramStyled(s CanonSchema, format string, st ERStyle) (string, error) {
	s = erOnly(s, st.Only)
	cols, edges := erModel(s)
	fills := erFills(s, st)
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
			fmt.Fprintf(&b, "  %s ||%so{ %s : \"%s\"\n", erIdent(s.Tables[e.parent].Name), erLine(e), erIdent(s.Tables[e.child].Name), erQuote(e.name))
		}
		for i, t := range s.Tables {
			if fills[i] != "" {
				fmt.Fprintf(&b, "  style %s fill:%s\n", erIdent(t.Name), fills[i])
			}
		}
	case "plantuml":
		b.WriteString("@startuml\n")
		for i, t := range s.Tables {
			fill := ""
			if fills[i] != "" {
				fill = " " + fills[i]
			}
			fmt.Fprintf(&b, "entity \"%s\" as t%d%s {\n", erQuote(t.Name), i, fill)
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
			fmt.Fprintf(&b, "t%d ||%so{ t%d : %s\n", e.parent, erLine(e), e.child, erQuote(e.name))
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
			fill := ""
			if fills[i] != "" {
				fill = `style=filled, fillcolor="` + fills[i] + `", `
			}
			fmt.Fprintf(&b, "  \"%s\" [shape=box, %slabel=\"%s\"];\n", erDot(t.Name), fill, label)
		}
		for _, e := range edges {
			dash := ""
			if e.virtual {
				dash = ", style=dashed"
			}
			fmt.Fprintf(&b, "  \"%s\" -> \"%s\" [label=\"%s\"%s];\n", erDot(s.Tables[e.child].Name), erDot(s.Tables[e.parent].Name), erDot(e.name), dash)
		}
		b.WriteString("}\n")
	default:
		return "", errors.New("unknown diagram format " + format + " (mermaid, plantuml or dot)")
	}
	return b.String(), nil
}

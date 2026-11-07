package dbclient

import (
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/cellwidth"
)

// Dependencies (DBX-16): what a table, view or function uses and what
// uses it, read from one CanonSchema (the schema-compare read). Foreign
// keys link tables; view definitions and routine bodies link by name
// (names inside comments and string literals do not count). Nothing
// here talks to the database.

// DepRef is one object on the other side of a dependency.
type DepRef struct {
	Kind string // table, view, materialized view, function, procedure
	Name string
	Via  string // "FK name (a) → (b)", "definition" or "body"
}

// Deps is the answer for one object.
type Deps struct {
	Kind    string
	Name    string
	Uses    []DepRef
	UsedBy  []DepRef
	Missing bool // no object with that name in the schema
}

type depObj struct {
	kind, name string
	idents     map[string]bool // lower-cased names used in its SQL text
	via        string
	table      *CanonTable
}

func depObjects(s CanonSchema) []depObj {
	var out []depObj
	for i := range s.Tables {
		out = append(out, depObj{kind: "table", name: s.Tables[i].Name, table: &s.Tables[i]})
	}
	for _, v := range s.Views {
		k := "view"
		if v.Materialized {
			k = "materialized view"
		}
		out = append(out, depObj{kind: k, name: v.Name, idents: sqlIdents(v.Definition), via: "definition"})
	}
	for _, r := range s.Routines {
		k := strings.ToLower(r.Kind)
		if k == "" {
			k = "function"
		}
		out = append(out, depObj{kind: k, name: r.Name, idents: sqlIdents(r.Body), via: "body"})
	}
	return out
}

// Dependencies lists what name uses and what uses it (name matches any
// case). Both lists are sorted: tables, then views, then routines, by name.
func Dependencies(s CanonSchema, name string) Deps {
	objs := depObjects(s)
	self := -1
	for i, o := range objs {
		if strings.EqualFold(o.name, name) {
			self = i
			break
		}
	}
	if self < 0 {
		return Deps{Name: name, Missing: true}
	}
	me := objs[self]
	d := Deps{Kind: me.kind, Name: me.name}
	key := strings.ToLower(me.name)
	seenUse, seenBy := map[string]bool{}, map[string]bool{}
	addUse := func(r DepRef) {
		if k := strings.ToLower(r.Name); !seenUse[k] {
			seenUse[k] = true
			d.Uses = append(d.Uses, r)
		}
	}
	addBy := func(r DepRef) {
		if k := strings.ToLower(r.Name); !seenBy[k] {
			seenBy[k] = true
			d.UsedBy = append(d.UsedBy, r)
		}
	}
	if me.table != nil {
		for _, c := range me.table.Constraints {
			if c.Kind != "FOREIGN KEY" || c.RefTable == "" {
				continue
			}
			addUse(DepRef{Kind: "table", Name: refObjName(objs, c.RefTable), Via: fkVia(c)})
		}
	}
	for i, o := range objs {
		if i == self {
			continue
		}
		if me.idents[strings.ToLower(o.name)] {
			addUse(DepRef{Kind: o.kind, Name: o.name, Via: me.via})
		}
		if o.table != nil {
			for _, c := range o.table.Constraints {
				if c.Kind == "FOREIGN KEY" && strings.EqualFold(c.RefTable, key) {
					addBy(DepRef{Kind: "table", Name: o.name, Via: fkVia(c)})
					break
				}
			}
		}
		if o.idents[key] {
			addBy(DepRef{Kind: o.kind, Name: o.name, Via: o.via})
		}
	}
	sortDeps(d.Uses)
	sortDeps(d.UsedBy)
	return d
}

func refObjName(objs []depObj, name string) string {
	for _, o := range objs {
		if strings.EqualFold(o.name, name) {
			return o.name
		}
	}
	return name
}

func fkVia(c CanonConstraint) string {
	s := "FK "
	if c.Name != "" {
		s += c.Name + " "
	}
	return s + "(" + strings.Join(c.Columns, ", ") + ") → (" + strings.Join(c.RefColumns, ", ") + ")"
}

func depRank(kind string) int {
	switch kind {
	case "table":
		return 0
	case "view", "materialized view":
		return 1
	}
	return 2
}

func sortDeps(rs []DepRef) {
	sort.SliceStable(rs, func(i, j int) bool {
		if a, b := depRank(rs[i].Kind), depRank(rs[j].Kind); a != b {
			return a < b
		}
		return strings.ToLower(rs[i].Name) < strings.ToLower(rs[j].Name)
	})
}

// sqlIdents returns the lower-cased words and quoted names in SQL text,
// skipping string literals and comments.
func sqlIdents(sql string) map[string]bool {
	out := map[string]bool{}
	n := len(sql)
	for i := 0; i < n; {
		c := sql[i]
		switch {
		case c == '-' && i+1 < n && sql[i+1] == '-':
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				return out
			}
			i += end + 4
		case c == '\'':
			i++
			for i < n {
				if sql[i] == '\'' {
					if i+1 < n && sql[i+1] == '\'' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			i++
		case c == '"' || c == '`' || c == '[':
			closer := c
			if c == '[' {
				closer = ']'
			}
			end := strings.IndexByte(sql[i+1:], closer)
			if end < 0 {
				return out
			}
			out[strings.ToLower(sql[i+1:i+1+end])] = true
			i += end + 2
		case identStart(c):
			j := i + 1
			for j < n && (identStart(sql[j]) || (sql[j] >= '0' && sql[j] <= '9') || sql[j] == '$') {
				j++
			}
			out[strings.ToLower(sql[i:j])] = true
			i = j
		default:
			i++
		}
	}
	return out
}

func identStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

// ERDiagram draws the tables as text boxes (PK and FK columns marked,
// FK targets shown) and then one line per foreign key. With a focus
// table only the tables within depth FK hops (either way) are drawn;
// depth < 1 counts as 1.
func ERDiagram(s CanonSchema, focus string, depth int) string {
	tables := erTables(s, focus, depth)
	var b strings.Builder
	var edges []string
	for _, t := range tables {
		pk := map[string]bool{}
		fk := map[string]string{}
		for _, c := range t.Constraints {
			switch c.Kind {
			case "PRIMARY KEY":
				for _, col := range c.Columns {
					pk[col] = true
				}
			case "FOREIGN KEY":
				for i, col := range c.Columns {
					ref := c.RefTable
					if i < len(c.RefColumns) {
						ref += "." + c.RefColumns[i]
					}
					fk[col] = ref
					edges = append(edges, t.Name+"."+col+" ──▶ "+ref)
				}
			}
		}
		rows := make([]string, 0, len(t.Columns))
		for _, col := range t.Columns {
			mark := "  "
			if pk[col.Name] {
				mark = "PK"
			} else if fk[col.Name] != "" {
				mark = "FK"
			}
			row := mark + " " + col.Name + " " + col.Type
			if ref := fk[col.Name]; ref != "" {
				row += " → " + ref
			}
			rows = append(rows, row)
		}
		w := cellwidth.StringWidth(t.Name) + 2
		for _, r := range rows {
			w = max(w, cellwidth.StringWidth(r))
		}
		// Every line is w+4 cells: "│ " + row + " │".
		top := "┌─ " + t.Name + " "
		top += strings.Repeat("─", max(1, w-cellwidth.StringWidth(t.Name)-1)) + "┐"
		b.WriteString(top + "\n")
		for _, r := range rows {
			b.WriteString("│ " + cellwidth.FillRight(r, w) + " │\n")
		}
		b.WriteString("└" + strings.Repeat("─", w+2) + "┘\n\n")
	}
	if len(edges) > 0 {
		b.WriteString("Relations\n")
		for _, e := range edges {
			b.WriteString("  " + e + "\n")
		}
	}
	return b.String()
}

func erTables(s CanonSchema, focus string, depth int) []CanonTable {
	if focus == "" {
		return s.Tables
	}
	depth = max(depth, 1)
	near := map[string]bool{}
	for _, t := range s.Tables {
		if strings.EqualFold(t.Name, focus) {
			near[strings.ToLower(t.Name)] = true
		}
	}
	for range depth {
		next := map[string]bool{}
		for _, t := range s.Tables {
			me := strings.ToLower(t.Name)
			for _, c := range t.Constraints {
				if c.Kind != "FOREIGN KEY" {
					continue
				}
				ref := strings.ToLower(c.RefTable)
				if near[me] {
					next[ref] = true
				}
				if near[ref] {
					next[me] = true
				}
			}
		}
		for k := range next {
			near[k] = true
		}
	}
	var out []CanonTable
	for _, t := range s.Tables {
		if near[strings.ToLower(t.Name)] {
			out = append(out, t)
		}
	}
	return out
}

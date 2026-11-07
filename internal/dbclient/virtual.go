package dbclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// VirtualViewKind is the tree kind of a saved SELECT that lives only in the
// user's config, never in the database.
const VirtualViewKind = "virtual view"

// VirtualObjects are user-defined objects layered over a real schema:
// foreign keys the database does not declare, computed columns and saved
// read-only SELECTs. Nothing here is ever written to the database.
type VirtualObjects struct {
	ForeignKeys []VirtualFK       `json:"foreignKeys"`
	NameRules   []VirtualNameRule `json:"nameRules"`
	Columns     []VirtualColumn   `json:"columns"`
	Views       []VirtualView     `json:"views"`
	dialect     string
}

type VirtualFK struct {
	Table     string `json:"table"`
	Column    string `json:"column"`
	RefTable  string `json:"refTable"`
	RefColumn string `json:"refColumn"`
}

// VirtualNameRule turns a column named <table><Suffix> into a virtual FK to
// <table> (or <table>s) on RefColumn ("id" when empty).
type VirtualNameRule struct {
	Suffix    string `json:"suffix"`
	RefColumn string `json:"refColumn"`
}

type VirtualColumn struct {
	Table string `json:"table"`
	Name  string `json:"name"`
	Expr  string `json:"expr"`
}

type VirtualView struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

// ParseVirtualObjects reads a JSONC virtual-objects file. Broken or unsafe
// entries (a view or column expression that is not one read-only statement)
// are dropped and reported as problems; only bad JSON is an error.
func ParseVirtualObjects(data []byte, dialect string) (VirtualObjects, []string, error) {
	var in VirtualObjects
	if err := json.Unmarshal(jsonc.Strip(data), &in); err != nil {
		return VirtualObjects{}, nil, err
	}
	out := VirtualObjects{dialect: dialect}
	var problems []string
	for i, fk := range in.ForeignKeys {
		if blank(fk.Table, fk.Column, fk.RefTable, fk.RefColumn) {
			problems = append(problems, fmt.Sprintf("foreignKeys[%d]: table, column, refTable and refColumn are required", i))
			continue
		}
		out.ForeignKeys = append(out.ForeignKeys, fk)
	}
	for i, r := range in.NameRules {
		if strings.TrimSpace(r.Suffix) == "" {
			problems = append(problems, fmt.Sprintf("nameRules[%d]: suffix is required", i))
			continue
		}
		if strings.TrimSpace(r.RefColumn) == "" {
			r.RefColumn = "id"
		}
		out.NameRules = append(out.NameRules, r)
	}
	for i, c := range in.Columns {
		if blank(c.Table, c.Name, c.Expr) {
			problems = append(problems, fmt.Sprintf("columns[%d]: table, name and expr are required", i))
			continue
		}
		if !oneRead("SELECT ("+c.Expr+") AS x", dialect) {
			problems = append(problems, fmt.Sprintf("columns[%d] %s: expr must be a read-only expression", i, c.Name))
			continue
		}
		out.Columns = append(out.Columns, c)
	}
	seen := map[string]bool{}
	for i, v := range in.Views {
		key := strings.ToLower(strings.TrimSpace(v.Name))
		switch {
		case blank(v.Name, v.SQL):
			problems = append(problems, fmt.Sprintf("views[%d]: name and sql are required", i))
		case seen[key]:
			problems = append(problems, fmt.Sprintf("views[%d] %s: duplicate view name", i, v.Name))
		case !oneRead(v.SQL, dialect):
			problems = append(problems, fmt.Sprintf("views[%d] %s: sql must be one read-only SELECT", i, v.Name))
		default:
			seen[key] = true
			out.Views = append(out.Views, v)
		}
	}
	return out, problems, nil
}

func blank(ss ...string) bool {
	for _, s := range ss {
		if strings.TrimSpace(s) == "" {
			return true
		}
	}
	return false
}

func oneRead(sql, dialect string) bool {
	kind, sts := Classify(sql, dialect)
	return kind == KindRead && len(sts) == 1
}

// ForeignKeysFor returns the virtual FKs of table: explicit ones first, then
// name-rule matches whose target is in tables. A column that already has a
// real or explicit FK gets no rule match. Results are marked Virtual and
// each is its own key (named "virtual:<column>"), so following one column
// never pulls in the table's other virtual keys.
func (v VirtualObjects) ForeignKeysFor(table string, cols []Column, real []SchemaForeignKey, tables []string) []SchemaForeignKey {
	var out []SchemaForeignKey
	taken := map[string]bool{}
	for _, fk := range real {
		taken[strings.ToLower(fk.Column)] = true
	}
	for _, fk := range v.ForeignKeys {
		if !strings.EqualFold(fk.Table, table) || taken[strings.ToLower(fk.Column)] {
			continue
		}
		taken[strings.ToLower(fk.Column)] = true
		out = append(out, SchemaForeignKey{Name: "virtual:" + fk.Column, Column: fk.Column, RefTable: fk.RefTable, RefColumn: fk.RefColumn, Virtual: true})
	}
	byName := map[string]string{}
	for _, t := range tables {
		byName[strings.ToLower(t)] = t
	}
	for _, c := range cols {
		if taken[strings.ToLower(c.Name)] {
			continue
		}
		for _, r := range v.NameRules {
			low, suf := strings.ToLower(c.Name), strings.ToLower(r.Suffix)
			if !strings.HasSuffix(low, suf) || len(low) == len(suf) {
				continue
			}
			base := strings.TrimSuffix(low, suf)
			ref, ok := byName[base]
			if !ok {
				ref, ok = byName[base+"s"]
			}
			if !ok {
				continue
			}
			taken[low] = true
			out = append(out, SchemaForeignKey{Name: "virtual:" + c.Name, Column: c.Name, RefTable: ref, RefColumn: r.RefColumn, Virtual: true})
			break
		}
	}
	return out
}

// WithForeignKeys is cs with every table's virtual FKs added as FOREIGN
// KEY constraints marked Virtual (one per column), for the ER diagram and
// the "rows that point here" list. A reference names the table as cs
// spells it. cs itself is not changed.
func (v VirtualObjects) WithForeignKeys(cs CanonSchema) CanonSchema {
	if len(v.ForeignKeys) == 0 && len(v.NameRules) == 0 {
		return cs
	}
	names := make([]string, len(cs.Tables))
	byName := map[string]string{}
	for i, t := range cs.Tables {
		names[i] = t.Name
		byName[strings.ToLower(t.Name)] = t.Name
	}
	out := cs
	out.Tables = make([]CanonTable, len(cs.Tables))
	for i, t := range cs.Tables {
		cols := make([]Column, len(t.Columns))
		for j, c := range t.Columns {
			cols[j] = Column{Name: c.Name}
		}
		var real []SchemaForeignKey
		for _, c := range t.Constraints {
			if c.Kind == "FOREIGN KEY" {
				for _, n := range c.Columns {
					real = append(real, SchemaForeignKey{Column: n})
				}
			}
		}
		add := v.ForeignKeysFor(t.Name, cols, real, names)
		if len(add) > 0 {
			t.Constraints = append([]CanonConstraint(nil), t.Constraints...)
			for _, fk := range add {
				ref := fk.RefTable
				if n, ok := byName[strings.ToLower(ref)]; ok {
					ref = n
				}
				t.Constraints = append(t.Constraints, CanonConstraint{Kind: "FOREIGN KEY", Name: fk.Name,
					Columns: []string{fk.Column}, RefTable: ref, RefColumns: []string{fk.RefColumn}, Virtual: true})
			}
		}
		out.Tables[i] = t
	}
	return out
}

// ColumnsFor returns the virtual columns of table.
func (v VirtualObjects) ColumnsFor(table string) []VirtualColumn {
	var out []VirtualColumn
	for _, c := range v.Columns {
		if strings.EqualFold(c.Table, table) {
			out = append(out, c)
		}
	}
	return out
}

// SelectWithVirtualColumns is the table's SELECT * plus its virtual columns,
// or "" when the table has none.
func (v VirtualObjects) SelectWithVirtualColumns(table string) string {
	cols := v.ColumnsFor(table)
	if len(cols) == 0 {
		return ""
	}
	d := DialectFor(v.dialect)
	if d == nil {
		d = PostgresDialect{}
	}
	var b strings.Builder
	b.WriteString("SELECT *")
	for _, c := range cols {
		fmt.Fprintf(&b, ", (%s) AS %s", c.Expr, d.QuoteIdent(c.Name))
	}
	b.WriteString(" FROM " + d.QuoteIdent(table))
	return b.String()
}

// ViewObjects lists the virtual views as tree objects.
func (v VirtualObjects) ViewObjects() []SchemaObject {
	var out []SchemaObject
	for _, vv := range v.Views {
		out = append(out, SchemaObject{Name: vv.Name, Kind: VirtualViewKind})
	}
	return out
}

// ViewSQL returns the saved SELECT of a virtual view.
func (v VirtualObjects) ViewSQL(name string) (string, bool) {
	for _, vv := range v.Views {
		if strings.EqualFold(vv.Name, name) {
			return vv.SQL, true
		}
	}
	return "", false
}

package dbclient

import (
	"errors"
	"fmt"
	"strings"
)

// ParseDesignSpec reads the designer form (DBX-15): one line per part,
// keywords in any case, "--" and "#" lines are comments.
//
// A new table starts with "table <name>", then:
//
//	column <name> <type> [not null] [default <expr>] [pk]
//	primary key (<cols>)
//	index <name> (<cols>) [unique]
//	unique <name> (<cols>)
//	fk <name> (<cols>) references <table> (<cols>) [on delete <action>] [on update <action>]
//	check <name> <expr>
//
// A change to a table is a list of:
//
//	add column … | add index … | add unique … | add fk … | add check …
//	drop column <name> | drop index <name>
//	drop constraint <name> [primary key|unique|foreign key|check]
//	rename column <old> to <new>
//
// create reports which of the two the text is; the result is checked
// again by DesignCreateDDL / DesignAlterDDL.
func ParseDesignSpec(text string) (d TableDesign, ops []AlterOp, create bool, err error) {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "--") || strings.HasPrefix(l, "#") {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		return d, nil, false, errors.New("the design is empty")
	}
	f := strings.Fields(lines[0])
	create = strings.EqualFold(f[0], "table")
	if create {
		if len(f) != 2 {
			return d, nil, true, errors.New("line 1: write table <name>")
		}
		d.Name = f[1]
		for i, l := range lines[1:] {
			if err := parseCreateLine(&d, l); err != nil {
				return d, nil, true, fmt.Errorf("line %d: %w", i+2, err)
			}
		}
		return d, nil, true, nil
	}
	for i, l := range lines {
		op, err := parseAlterLine(l)
		if err != nil {
			return d, nil, false, fmt.Errorf("line %d: %w", i+1, err)
		}
		ops = append(ops, op)
	}
	return d, ops, false, nil
}

// cutWord splits off the first word (lower case) and the rest.
func cutWord(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return strings.ToLower(s), ""
	}
	return strings.ToLower(s[:i]), strings.TrimSpace(s[i+1:])
}

func parseCreateLine(d *TableDesign, l string) error {
	w, rest := cutWord(l)
	switch w {
	case "column":
		c, pk, err := parseColumnSpec(rest)
		if err != nil {
			return err
		}
		d.Columns = append(d.Columns, c)
		if pk {
			d.PrimaryKey = append(d.PrimaryKey, c.Name)
		}
	case "primary":
		k, rest := cutWord(rest)
		if k != "key" {
			return errors.New("write primary key (<columns>)")
		}
		cols, tail, err := parseColList(rest)
		if err != nil || tail != "" {
			return errors.New("write primary key (<columns>)")
		}
		d.PrimaryKey = cols
	case "index":
		ix, err := parseIndexSpec(rest)
		if err != nil {
			return err
		}
		d.Indexes = append(d.Indexes, ix)
	case "unique":
		u, err := parseUniqueSpec(rest)
		if err != nil {
			return err
		}
		d.Uniques = append(d.Uniques, u)
	case "fk":
		fk, err := parseFKSpec(rest)
		if err != nil {
			return err
		}
		d.FKs = append(d.FKs, fk)
	case "check":
		c, err := parseCheckSpec(rest)
		if err != nil {
			return err
		}
		d.Checks = append(d.Checks, c)
	default:
		return fmt.Errorf("unknown line %q: use column, primary key, index, unique, fk or check", w)
	}
	return nil
}

func parseAlterLine(l string) (AlterOp, error) {
	verb, rest := cutWord(l)
	what, rest := cutWord(rest)
	switch verb + " " + what {
	case "add column":
		c, pk, err := parseColumnSpec(rest)
		if err != nil {
			return AlterOp{}, err
		}
		if pk {
			return AlterOp{}, errors.New("add column cannot make a primary key; add the column, then a unique or a key")
		}
		return AlterOp{Kind: AlterAddColumn, Column: c}, nil
	case "add index":
		ix, err := parseIndexSpec(rest)
		return AlterOp{Kind: AlterAddIndex, Index: ix}, err
	case "add unique":
		u, err := parseUniqueSpec(rest)
		return AlterOp{Kind: AlterAddUnique, Unique: u}, err
	case "add fk":
		fk, err := parseFKSpec(rest)
		return AlterOp{Kind: AlterAddFK, FK: fk}, err
	case "add check":
		c, err := parseCheckSpec(rest)
		return AlterOp{Kind: AlterAddCheck, Check: c}, err
	case "drop column", "drop index":
		if len(strings.Fields(rest)) != 1 {
			return AlterOp{}, fmt.Errorf("write %s %s <name>", verb, what)
		}
		k := AlterDropColumn
		if what == "index" {
			k = AlterDropIndex
		}
		return AlterOp{Kind: k, Name: rest}, nil
	case "drop constraint":
		n, k := cutWordKeep(rest)
		if n == "" {
			return AlterOp{}, errors.New("write drop constraint <name> [primary key|unique|foreign key|check]")
		}
		kind := strings.ToUpper(strings.Join(strings.Fields(k), " "))
		switch kind {
		case "", "PRIMARY KEY", "UNIQUE", "FOREIGN KEY", "CHECK":
		default:
			return AlterOp{}, fmt.Errorf("unknown constraint kind %q", kind)
		}
		return AlterOp{Kind: AlterDropConstraint, Name: n, ConstraintKind: kind}, nil
	case "rename column":
		f := strings.Fields(rest)
		if len(f) != 3 || !strings.EqualFold(f[1], "to") {
			return AlterOp{}, errors.New("write rename column <old> to <new>")
		}
		return AlterOp{Kind: AlterRenameColumn, Name: f[0], NewName: f[2]}, nil
	}
	return AlterOp{}, fmt.Errorf("unknown change %q: use add, drop or rename (or start with table <name> for a new table)", strings.TrimSpace(verb+" "+what))
}

// cutWordKeep is cutWord without lowering the first word.
func cutWordKeep(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i+1:])
}

// parseColumnSpec reads <name> <type> [not null] [default <expr>] [pk].
func parseColumnSpec(s string) (CanonColumn, bool, error) {
	name, rest := cutWordKeep(s)
	if name == "" || rest == "" {
		return CanonColumn{}, false, errors.New("write column <name> <type>")
	}
	c := CanonColumn{Name: name}
	pk := false
	low := strings.ToLower(rest)
	for _, suf := range []string{" primary key", " pk"} {
		if strings.HasSuffix(low, suf) {
			pk = true
			rest, low = strings.TrimSpace(rest[:len(rest)-len(suf)]), strings.TrimSpace(low[:len(low)-len(suf)])
			break
		}
	}
	if strings.HasSuffix(low, " default") {
		return CanonColumn{}, false, fmt.Errorf("column %s: default has no value", name)
	}
	if i := strings.Index(low, " default "); i >= 0 {
		c.Default = strings.TrimSpace(rest[i+len(" default "):])
		rest, low = strings.TrimSpace(rest[:i]), strings.TrimSpace(low[:i])
		if c.Default == "" {
			return CanonColumn{}, false, fmt.Errorf("column %s: default has no value", name)
		}
	}
	if strings.HasSuffix(low, " not null") {
		c.NotNull = true
		rest = strings.TrimSpace(rest[:len(rest)-len(" not null")])
	}
	c.Type = rest
	if c.Type == "" {
		return CanonColumn{}, false, fmt.Errorf("column %s has no type", name)
	}
	return c, pk, nil
}

// parseColList reads "(a, b)" and returns the columns and the rest.
func parseColList(s string) ([]string, string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") {
		return nil, s, errors.New("a column list starts with (")
	}
	end := strings.IndexByte(s, ')')
	if end < 0 {
		return nil, s, errors.New("a column list ends with )")
	}
	var cols []string
	for _, c := range strings.Split(s[1:end], ",") {
		if c = strings.TrimSpace(c); c != "" {
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		return nil, s, errors.New("the column list is empty")
	}
	return cols, strings.TrimSpace(s[end+1:]), nil
}

func parseIndexSpec(s string) (DesignIndex, error) {
	name, rest := cutWordKeep(s)
	cols, tail, err := parseColList(rest)
	if name == "" || err != nil {
		return DesignIndex{}, errors.New("write index <name> (<columns>) [unique]")
	}
	ix := DesignIndex{Name: name, Columns: cols}
	switch strings.ToLower(tail) {
	case "":
	case "unique":
		ix.Unique = true
	default:
		return DesignIndex{}, fmt.Errorf("index %s: unknown %q after the columns", name, tail)
	}
	return ix, nil
}

func parseUniqueSpec(s string) (DesignUnique, error) {
	name, rest := cutWordKeep(s)
	cols, tail, err := parseColList(rest)
	if name == "" || err != nil || tail != "" {
		return DesignUnique{}, errors.New("write unique <name> (<columns>)")
	}
	return DesignUnique{Name: name, Columns: cols}, nil
}

func parseFKSpec(s string) (DesignFK, error) {
	const usage = "write fk <name> (<columns>) references <table> (<columns>) [on delete <action>] [on update <action>]"
	name, rest := cutWordKeep(s)
	cols, rest, err := parseColList(rest)
	if name == "" || err != nil {
		return DesignFK{}, errors.New(usage)
	}
	w, rest := cutWord(rest)
	if w != "references" {
		return DesignFK{}, errors.New(usage)
	}
	i := strings.IndexByte(rest, '(')
	if i <= 0 {
		return DesignFK{}, errors.New(usage)
	}
	ref := strings.TrimSpace(rest[:i])
	refCols, tail, err := parseColList(rest[i:])
	if err != nil || ref == "" {
		return DesignFK{}, errors.New(usage)
	}
	fk := DesignFK{Name: name, Columns: cols, RefTable: ref, RefColumns: refCols}
	for tail != "" {
		on, r := cutWord(tail)
		ev, r := cutWord(r)
		if on != "on" || (ev != "delete" && ev != "update") {
			return DesignFK{}, errors.New(usage)
		}
		act := strings.ToUpper(r)
		next := ""
		if j := strings.Index(act, " ON "); j >= 0 {
			act, next = act[:j], strings.TrimSpace(r[j+1:])
		}
		act = strings.Join(strings.Fields(act), " ")
		if !fkActions[act] || act == "" {
			return DesignFK{}, fmt.Errorf("fk %s: unknown action %q", name, act)
		}
		if ev == "delete" {
			fk.OnDelete = act
		} else {
			fk.OnUpdate = act
		}
		tail = next
	}
	return fk, nil
}

func parseCheckSpec(s string) (DesignCheck, error) {
	name, expr := cutWordKeep(s)
	if name == "" || expr == "" {
		return DesignCheck{}, errors.New("write check <name> <expression>")
	}
	return DesignCheck{Name: name, Expr: expr}, nil
}

package dbclient

import (
	"errors"
	"fmt"
	"strings"
)

// The table designer (DBX-15) turns a form into DDL text: CREATE TABLE
// for a new table, ALTER statements for changes. It only builds text;
// the page shows it in a preview card (Cancel first) before anything
// runs. Names are quoted by the dialect; types, defaults and check
// expressions are SQL text the person typed, so a ";" in them is
// refused (one design = only the statements shown).

// DesignUnique is a UNIQUE constraint.
type DesignUnique struct {
	Name    string
	Columns []string
}

// DesignFK is a foreign key.
type DesignFK struct {
	Name               string
	Columns            []string
	RefTable           string
	RefColumns         []string
	OnUpdate, OnDelete string // "", NO ACTION, RESTRICT, CASCADE, SET NULL, SET DEFAULT
}

// DesignCheck is a CHECK constraint; Expr is the text inside CHECK (…).
type DesignCheck struct {
	Name string
	Expr string
}

// DesignIndex is a plain index on columns.
type DesignIndex struct {
	Name    string
	Columns []string
	Unique  bool
}

// TableDesign is the designer form of one table.
type TableDesign struct {
	Name       string
	Columns    []CanonColumn
	PrimaryKey []string
	Uniques    []DesignUnique
	FKs        []DesignFK
	Checks     []DesignCheck
	Indexes    []DesignIndex
}

var fkActions = map[string]bool{"": true, "NO ACTION": true, "RESTRICT": true, "CASCADE": true, "SET NULL": true, "SET DEFAULT": true}

func noSemicolon(what, s string) error {
	if strings.Contains(s, ";") {
		return fmt.Errorf("%s may not contain ;", what)
	}
	return nil
}

func designColumn(c CanonColumn) error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("a column has no name")
	}
	if strings.TrimSpace(c.Type) == "" {
		return fmt.Errorf("column %s has no type", c.Name)
	}
	for what, s := range map[string]string{"the type of " + c.Name: c.Type, "the default of " + c.Name: c.Default, "the extra of " + c.Name: c.Extra} {
		if err := noSemicolon(what, s); err != nil {
			return err
		}
	}
	return nil
}

// checkCols reports a column list that is empty or names an unknown
// column (known == nil: any name is fine, for ALTER).
func checkCols(what string, cols []string, known map[string]bool) error {
	if len(cols) == 0 {
		return fmt.Errorf("%s has no columns", what)
	}
	for _, c := range cols {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("%s has an empty column", what)
		}
		if known != nil && !known[strings.ToLower(c)] {
			return fmt.Errorf("%s uses unknown column %s", what, c)
		}
	}
	return nil
}

func designFK(fk DesignFK, known map[string]bool) error {
	if err := checkCols("foreign key "+fk.Name, fk.Columns, known); err != nil {
		return err
	}
	if strings.TrimSpace(fk.RefTable) == "" {
		return fmt.Errorf("foreign key %s has no referenced table", fk.Name)
	}
	if len(fk.RefColumns) != len(fk.Columns) {
		return fmt.Errorf("foreign key %s: %d columns but %d referenced columns", fk.Name, len(fk.Columns), len(fk.RefColumns))
	}
	if !fkActions[strings.ToUpper(fk.OnUpdate)] || !fkActions[strings.ToUpper(fk.OnDelete)] {
		return fmt.Errorf("foreign key %s: unknown ON UPDATE / ON DELETE action", fk.Name)
	}
	return nil
}

func designCheck(c DesignCheck) error {
	if strings.TrimSpace(c.Expr) == "" {
		return fmt.Errorf("check %s is empty", c.Name)
	}
	return noSemicolon("check "+c.Name, c.Expr)
}

func fkConstraint(fk DesignFK) CanonConstraint {
	return CanonConstraint{Kind: "FOREIGN KEY", Name: fk.Name, Columns: fk.Columns, RefTable: fk.RefTable, RefColumns: fk.RefColumns, OnUpdate: strings.ToUpper(fk.OnUpdate), OnDelete: strings.ToUpper(fk.OnDelete)}
}

func checkConstraint(c DesignCheck) CanonConstraint {
	return CanonConstraint{Kind: "CHECK", Name: c.Name, Check: "CHECK (" + strings.TrimSpace(c.Expr) + ")"}
}

func (w canonWriter) designIndex(table string, ix DesignIndex) string {
	exprs := make([]string, len(ix.Columns))
	for i, c := range ix.Columns {
		exprs[i] = w.q(c)
	}
	return w.index(CanonIndex{Table: table, Name: ix.Name, Exprs: exprs, Unique: ix.Unique})
}

// DesignCreateDDL is the CREATE TABLE script (plus its indexes) of d.
func DesignCreateDDL(driver, schema string, d TableDesign) (string, error) {
	if strings.TrimSpace(d.Name) == "" {
		return "", errors.New("the table has no name")
	}
	if len(d.Columns) == 0 {
		return "", errors.New("the table has no columns")
	}
	known := map[string]bool{}
	for _, c := range d.Columns {
		if err := designColumn(c); err != nil {
			return "", err
		}
		k := strings.ToLower(c.Name)
		if known[k] {
			return "", fmt.Errorf("column %s is there twice", c.Name)
		}
		known[k] = true
	}
	w := newCanonWriter(driver, schema)
	t := CanonTable{Name: d.Name, Columns: d.Columns}
	if len(d.PrimaryKey) > 0 {
		if err := checkCols("the primary key", d.PrimaryKey, known); err != nil {
			return "", err
		}
		name := d.Name + "_pkey"
		if w.driver == "mysql" {
			name = "" // MySQL ignores it: the key is always PRIMARY
		}
		t.Constraints = append(t.Constraints, CanonConstraint{Kind: "PRIMARY KEY", Name: name, Columns: d.PrimaryKey})
	}
	for _, u := range d.Uniques {
		if err := checkCols("unique "+u.Name, u.Columns, known); err != nil {
			return "", err
		}
		t.Constraints = append(t.Constraints, CanonConstraint{Kind: "UNIQUE", Name: u.Name, Columns: u.Columns})
	}
	for _, fk := range d.FKs {
		if err := designFK(fk, known); err != nil {
			return "", err
		}
		t.Constraints = append(t.Constraints, fkConstraint(fk))
	}
	for _, c := range d.Checks {
		if err := designCheck(c); err != nil {
			return "", err
		}
		t.Constraints = append(t.Constraints, checkConstraint(c))
	}
	var b strings.Builder
	b.WriteString(w.table(t, true) + "\n")
	for _, ix := range d.Indexes {
		if strings.TrimSpace(ix.Name) == "" {
			return "", errors.New("an index has no name")
		}
		if err := checkCols("index "+ix.Name, ix.Columns, known); err != nil {
			return "", err
		}
		b.WriteString(w.designIndex(d.Name, ix) + "\n")
	}
	return b.String(), nil
}

// AlterKind is one kind of table change.
type AlterKind int

// Table changes.
const (
	AlterAddColumn AlterKind = iota + 1
	AlterDropColumn
	AlterRenameColumn
	AlterAddIndex
	AlterDropIndex
	AlterAddFK
	AlterAddCheck
	AlterAddUnique
	AlterDropConstraint
)

// AlterOp is one change. Name is the column, index or constraint it
// drops or renames; ConstraintKind (PRIMARY KEY, UNIQUE, FOREIGN KEY,
// CHECK) picks the MySQL DROP form.
type AlterOp struct {
	Kind           AlterKind
	Column         CanonColumn
	Name, NewName  string
	ConstraintKind string
	Index          DesignIndex
	FK             DesignFK
	Check          DesignCheck
	Unique         DesignUnique
}

// AlterPlan is the script of a list of changes and how many of its
// statements drop something (the preview card warns about them).
type AlterPlan struct {
	Script      string
	Destructive int
}

// DesignAlterDDL is the ALTER script of ops on schema.table.
func DesignAlterDDL(driver, schema, table string, ops []AlterOp) (AlterPlan, error) {
	if strings.TrimSpace(table) == "" {
		return AlterPlan{}, errors.New("no table to change")
	}
	if len(ops) == 0 {
		return AlterPlan{}, errors.New("no changes")
	}
	w := newCanonWriter(driver, schema)
	head := "ALTER TABLE " + w.qual(table) + " "
	var b strings.Builder
	plan := AlterPlan{}
	line := func(s string) { b.WriteString(s + ";\n") }
	addConstraint := func(c CanonConstraint) error {
		if w.sqlite() {
			return errors.New("SQLite cannot add a constraint to a table; create the table again")
		}
		line(head + "ADD " + w.constraint(c, w.refName))
		return nil
	}
	for _, op := range ops {
		switch op.Kind {
		case AlterAddColumn:
			if err := designColumn(op.Column); err != nil {
				return AlterPlan{}, err
			}
			line(head + "ADD COLUMN " + w.column(op.Column))
		case AlterDropColumn:
			if strings.TrimSpace(op.Name) == "" {
				return AlterPlan{}, errors.New("drop column: no column")
			}
			line(head + "DROP COLUMN " + w.q(op.Name))
			plan.Destructive++
		case AlterRenameColumn:
			if strings.TrimSpace(op.Name) == "" || strings.TrimSpace(op.NewName) == "" {
				return AlterPlan{}, errors.New("rename column needs the old and the new name")
			}
			line(head + "RENAME COLUMN " + w.q(op.Name) + " TO " + w.q(op.NewName))
		case AlterAddIndex:
			if strings.TrimSpace(op.Index.Name) == "" {
				return AlterPlan{}, errors.New("an index has no name")
			}
			if err := checkCols("index "+op.Index.Name, op.Index.Columns, nil); err != nil {
				return AlterPlan{}, err
			}
			b.WriteString(w.designIndex(table, op.Index) + "\n")
		case AlterDropIndex:
			if strings.TrimSpace(op.Name) == "" {
				return AlterPlan{}, errors.New("drop index: no index")
			}
			if w.driver == "mysql" {
				line("DROP INDEX " + w.q(op.Name) + " ON " + w.qual(table))
			} else {
				line("DROP INDEX " + w.qual(op.Name))
			}
			plan.Destructive++
		case AlterAddFK:
			if err := designFK(op.FK, nil); err != nil {
				return AlterPlan{}, err
			}
			if err := addConstraint(fkConstraint(op.FK)); err != nil {
				return AlterPlan{}, err
			}
		case AlterAddCheck:
			if err := designCheck(op.Check); err != nil {
				return AlterPlan{}, err
			}
			if err := addConstraint(checkConstraint(op.Check)); err != nil {
				return AlterPlan{}, err
			}
		case AlterAddUnique:
			if err := checkCols("unique "+op.Unique.Name, op.Unique.Columns, nil); err != nil {
				return AlterPlan{}, err
			}
			if err := addConstraint(CanonConstraint{Kind: "UNIQUE", Name: op.Unique.Name, Columns: op.Unique.Columns}); err != nil {
				return AlterPlan{}, err
			}
		case AlterDropConstraint:
			if w.sqlite() {
				return AlterPlan{}, errors.New("SQLite cannot drop a constraint from a table; create the table again")
			}
			kind := strings.ToUpper(op.ConstraintKind)
			if w.driver == "mysql" {
				switch kind {
				case "PRIMARY KEY":
					line(head + "DROP PRIMARY KEY")
				case "FOREIGN KEY":
					line(head + "DROP FOREIGN KEY " + w.q(op.Name))
				case "UNIQUE":
					line(head + "DROP INDEX " + w.q(op.Name))
				case "CHECK":
					line(head + "DROP CHECK " + w.q(op.Name))
				default:
					return AlterPlan{}, fmt.Errorf("drop constraint %s: unknown kind %q", op.Name, op.ConstraintKind)
				}
			} else {
				if strings.TrimSpace(op.Name) == "" {
					return AlterPlan{}, errors.New("drop constraint: no name")
				}
				line(head + "DROP CONSTRAINT " + w.q(op.Name))
			}
			plan.Destructive++
		default:
			return AlterPlan{}, fmt.Errorf("unknown change %d", op.Kind)
		}
	}
	plan.Script = b.String()
	return plan, nil
}

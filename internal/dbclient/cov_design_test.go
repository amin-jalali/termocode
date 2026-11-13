package dbclient

import (
	"strings"
	"testing"
)

func TestParseDesignSpecEveryRefusal(t *testing.T) {
	for spec, why := range map[string]string{
		"":                                 "empty",
		"# only a comment\n-- another":     "empty",
		"table":                            "line 1: write table",
		"table a b":                        "line 1: write table",
		"table t\nbogus x":                 "unknown line",
		"table t\ncolumn":                  "column",
		"table t\ncolumn c":                "write column",
		"table t\nprimary":                 "primary key (<columns>)",
		"table t\nprimary key":             "primary key (<columns>)",
		"table t\nprimary key (a) extra":   "primary key (<columns>)",
		"table t\nprimary key a":           "primary key (<columns>)",
		"table t\nindex":                   "write index",
		"table t\nindex i":                 "write index",
		"table t\nindex i (a) weird":       "unknown",
		"table t\nunique":                  "write unique",
		"table t\nunique u (a) x":          "write unique",
		"table t\nfk":                      "fk",
		"table t\nfk f (a) refs x (b)":     "fk",
		"table t\nfk f (a) references x":   "fk",
		"table t\nfk f (a) references (b)": "fk",
		"table t\nfk f (a) references x (b) when delete cascade": "fk",
		"table t\nfk f (a) references x (b) on drop cascade":     "fk",
		"table t\nfk f (a) references x (b) on delete explode":   "unknown action",
		"table t\nfk f (a) references x (b) on delete":           "unknown action",
		"table t\ncheck":               "write check",
		"table t\ncheck only_name":     "write check",
		"table t\nindex i (":           "write index",
		"table t\nindex i ()":          "write index",
		"add column":                   "column",
		"add column c":                 "write column",
		"add column id int pk":         "cannot make a primary key",
		"add index":                    "write index",
		"add unique":                   "write unique",
		"add fk":                       "fk",
		"add check":                    "write check",
		"drop column":                  "write drop column",
		"drop column a b":              "write drop column",
		"drop index":                   "write drop index",
		"drop constraint":              "write drop constraint",
		"drop constraint c weird kind": "unknown constraint kind",
		"rename column a":              "write rename column",
		"rename column a to":           "write rename column",
		"rename column a into b":       "write rename column",
		"swap tables":                  "unknown change",
	} {
		_, _, _, err := ParseDesignSpec(spec)
		if err == nil {
			t.Errorf("%q: want an error", spec)
			continue
		}
		if !strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v, want %q", spec, err, why)
		}
	}
}

func TestParseDesignSpecAlterAndMoreCreateForms(t *testing.T) {
	_, ops, create, err := ParseDesignSpec(`
add column age int not null default 0
add index ix_age (age)
add unique uq_age (age)
add fk fk_u (age) references users (id) on delete set null on update cascade
add check ck_age age > 0
drop column old
drop index ix_old
drop constraint c1
drop constraint c2 foreign key
rename column a to b
`)
	if err != nil || create || len(ops) != 10 {
		t.Fatalf("ops = %d, create %v, err %v", len(ops), create, err)
	}
	kinds := []AlterKind{AlterAddColumn, AlterAddIndex, AlterAddUnique, AlterAddFK, AlterAddCheck, AlterDropColumn, AlterDropIndex, AlterDropConstraint, AlterDropConstraint, AlterRenameColumn}
	for i, k := range kinds {
		if ops[i].Kind != k {
			t.Errorf("op %d kind = %d, want %d", i, ops[i].Kind, k)
		}
	}
	if ops[3].FK.OnDelete != "SET NULL" || ops[3].FK.OnUpdate != "CASCADE" {
		t.Errorf("fk actions = %+v", ops[3].FK)
	}
	if ops[8].ConstraintKind != "FOREIGN KEY" {
		t.Errorf("constraint kind = %q", ops[8].ConstraintKind)
	}
	d, _, _, err := ParseDesignSpec("table t\ncolumn a int\ncolumn b int\nprimary key (a, b)\nindex i (a) unique")
	if err != nil || len(d.PrimaryKey) != 2 || !d.Indexes[0].Unique {
		t.Errorf("primary key and unique index = %+v, %v", d, err)
	}
}

func TestDesignAlterDDLPerEngine(t *testing.T) {
	ops := []AlterOp{
		{Kind: AlterAddColumn, Column: CanonColumn{Name: "age", Type: "int"}},
		{Kind: AlterRenameColumn, Name: "a", NewName: "b"},
		{Kind: AlterAddIndex, Index: DesignIndex{Name: "ix", Columns: []string{"age"}}},
		{Kind: AlterDropIndex, Name: "ix_old"},
		{Kind: AlterDropColumn, Name: "gone"},
	}
	for _, driver := range []string{"postgres", "mysql", "sqlite"} {
		plan, err := DesignAlterDDL(driver, "", "t", ops)
		if err != nil {
			t.Errorf("%s: %v", driver, err)
			continue
		}
		if plan.Destructive != 2 || !strings.Contains(plan.Script, "RENAME COLUMN") || !strings.Contains(plan.Script, "DROP INDEX") {
			t.Errorf("%s plan = %+v", driver, plan)
		}
	}
	my, _ := DesignAlterDDL("mysql", "", "t", ops)
	if !strings.Contains(my.Script, "DROP INDEX `ix_old` ON `t`") {
		t.Errorf("mysql drops an index ON the table: %s", my.Script)
	}
	cons := []AlterOp{
		{Kind: AlterAddFK, FK: DesignFK{Name: "f", Columns: []string{"a"}, RefTable: "u", RefColumns: []string{"id"}}},
		{Kind: AlterAddCheck, Check: DesignCheck{Name: "c", Expr: "a > 0"}},
		{Kind: AlterAddUnique, Unique: DesignUnique{Name: "u1", Columns: []string{"a"}}},
		{Kind: AlterDropConstraint, Name: "c", ConstraintKind: "CHECK"},
	}
	if _, err := DesignAlterDDL("sqlite", "", "t", cons[:1]); err == nil || !strings.Contains(err.Error(), "SQLite") {
		t.Errorf("sqlite add constraint = %v", err)
	}
	if _, err := DesignAlterDDL("sqlite", "", "t", cons[3:]); err == nil || !strings.Contains(err.Error(), "SQLite") {
		t.Errorf("sqlite drop constraint = %v", err)
	}
	for _, kind := range []string{"PRIMARY KEY", "FOREIGN KEY", "UNIQUE", "CHECK"} {
		plan, err := DesignAlterDDL("mysql", "", "t", []AlterOp{{Kind: AlterDropConstraint, Name: "n", ConstraintKind: kind}})
		if err != nil || plan.Destructive != 1 {
			t.Errorf("mysql drop %s = %+v, %v", kind, plan, err)
		}
	}
	if _, err := DesignAlterDDL("mysql", "", "t", []AlterOp{{Kind: AlterDropConstraint, Name: "n", ConstraintKind: "WHAT"}}); err == nil {
		t.Error("mysql: an unknown constraint kind")
	}
	pg, err := DesignAlterDDL("postgres", "public", "t", cons)
	if err != nil || !strings.Contains(pg.Script, "DROP CONSTRAINT") || !strings.Contains(pg.Script, "FOREIGN KEY") {
		t.Errorf("postgres constraints = %+v, %v", pg, err)
	}
	for name, bad := range map[string][]AlterOp{
		"no ops":          nil,
		"column no name":  {{Kind: AlterAddColumn, Column: CanonColumn{Type: "int"}}},
		"drop no name":    {{Kind: AlterDropColumn}},
		"rename no new":   {{Kind: AlterRenameColumn, Name: "a"}},
		"index no name":   {{Kind: AlterAddIndex, Index: DesignIndex{Columns: []string{"a"}}}},
		"index no cols":   {{Kind: AlterAddIndex, Index: DesignIndex{Name: "i"}}},
		"drop index none": {{Kind: AlterDropIndex}},
		"fk no cols":      {{Kind: AlterAddFK, FK: DesignFK{Name: "f", RefTable: "u"}}},
		"check no expr":   {{Kind: AlterAddCheck, Check: DesignCheck{Name: "c"}}},
		"unique no cols":  {{Kind: AlterAddUnique, Unique: DesignUnique{Name: "u"}}},
		"drop cons none":  {{Kind: AlterDropConstraint}},
		"unknown kind":    {{Kind: 99}},
	} {
		if _, err := DesignAlterDDL("postgres", "", "t", bad); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := DesignAlterDDL("postgres", "", " ", ops); err == nil {
		t.Error("no table: want error")
	}
}

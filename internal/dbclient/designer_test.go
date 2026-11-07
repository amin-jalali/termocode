package dbclient

import (
	"strings"
	"testing"
)

func designUsers() TableDesign {
	return TableDesign{
		Name: "users",
		Columns: []CanonColumn{
			{Name: "id", Type: "integer", NotNull: true},
			{Name: "org_id", Type: "integer"},
			{Name: "email", Type: "text", NotNull: true, Default: "''"},
		},
		PrimaryKey: []string{"id"},
		Uniques:    []DesignUnique{{Name: "users_email_key", Columns: []string{"email"}}},
		FKs:        []DesignFK{{Name: "users_org_fk", Columns: []string{"org_id"}, RefTable: "orgs", RefColumns: []string{"id"}, OnDelete: "CASCADE"}},
		Checks:     []DesignCheck{{Name: "users_email_ck", Expr: "email <> ''"}},
		Indexes:    []DesignIndex{{Name: "users_org_ix", Columns: []string{"org_id"}}},
	}
}

func TestDesignCreateDDLPostgres(t *testing.T) {
	got, err := DesignCreateDDL("postgres", "public", designUsers())
	if err != nil {
		t.Fatal(err)
	}
	want := `CREATE TABLE "public"."users" (
  "id" integer NOT NULL,
  "org_id" integer,
  "email" text NOT NULL DEFAULT '',
  CONSTRAINT "users_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "users_email_key" UNIQUE ("email"),
  CONSTRAINT "users_org_fk" FOREIGN KEY ("org_id") REFERENCES "public"."orgs" ("id") ON DELETE CASCADE,
  CONSTRAINT "users_email_ck" CHECK (email <> '')
);
CREATE INDEX "users_org_ix" ON "public"."users" ("org_id");
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDesignCreateDDLMySQLAndSQLite(t *testing.T) {
	my, err := DesignCreateDDL("mysql", "", designUsers())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(my, "CREATE TABLE `users` (") || !strings.Contains(my, "REFERENCES `orgs` (`id`)") {
		t.Fatalf("mysql:\n%s", my)
	}
	lite, err := DesignCreateDDL("sqlite", "main", designUsers())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(lite, `CREATE TABLE "users" (`) || !strings.Contains(lite, `CREATE INDEX "users_org_ix" ON "users" ("org_id");`) {
		t.Fatalf("sqlite:\n%s", lite)
	}
}

func TestDesignCreateDDLRefusesBadDesigns(t *testing.T) {
	cases := map[string]func(*TableDesign){
		"no name":          func(d *TableDesign) { d.Name = " " },
		"no columns":       func(d *TableDesign) { d.Columns = nil },
		"column no name":   func(d *TableDesign) { d.Columns[0].Name = "" },
		"column no type":   func(d *TableDesign) { d.Columns[1].Type = "" },
		"twice":            func(d *TableDesign) { d.Columns[1].Name = "ID" },
		"pk unknown col":   func(d *TableDesign) { d.PrimaryKey = []string{"nope"} },
		"fk unknown col":   func(d *TableDesign) { d.FKs[0].Columns = []string{"nope"} },
		"fk count":         func(d *TableDesign) { d.FKs[0].RefColumns = []string{"a", "b"} },
		"fk no table":      func(d *TableDesign) { d.FKs[0].RefTable = "" },
		"bad on delete":    func(d *TableDesign) { d.FKs[0].OnDelete = "DROP TABLE x" },
		"index no columns": func(d *TableDesign) { d.Indexes[0].Columns = nil },
		"semicolon type":   func(d *TableDesign) { d.Columns[0].Type = "int; DROP TABLE x" },
		"semicolon check":  func(d *TableDesign) { d.Checks[0].Expr = "1=1); DROP TABLE x; --" },
		"semicolon def":    func(d *TableDesign) { d.Columns[2].Default = "'';DROP TABLE x" },
		"empty check":      func(d *TableDesign) { d.Checks[0].Expr = " " },
	}
	for name, mod := range cases {
		d := designUsers()
		mod(&d)
		if _, err := DesignCreateDDL("postgres", "", d); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestDesignAlterDDL(t *testing.T) {
	ops := []AlterOp{
		{Kind: AlterAddColumn, Column: CanonColumn{Name: "age", Type: "int", Default: "0"}},
		{Kind: AlterRenameColumn, Name: "email", NewName: "mail"},
		{Kind: AlterDropColumn, Name: "old"},
		{Kind: AlterAddIndex, Index: DesignIndex{Name: "age_ix", Columns: []string{"age"}, Unique: true}},
		{Kind: AlterDropIndex, Name: "old_ix"},
		{Kind: AlterAddFK, FK: DesignFK{Name: "u_org", Columns: []string{"org_id"}, RefTable: "orgs", RefColumns: []string{"id"}}},
		{Kind: AlterAddCheck, Check: DesignCheck{Name: "age_ck", Expr: "age >= 0"}},
		{Kind: AlterDropConstraint, Name: "u_org", ConstraintKind: "FOREIGN KEY"},
	}
	plan, err := DesignAlterDDL("postgres", "public", "users", ops)
	if err != nil {
		t.Fatal(err)
	}
	want := `ALTER TABLE "public"."users" ADD COLUMN "age" int DEFAULT 0;
ALTER TABLE "public"."users" RENAME COLUMN "email" TO "mail";
ALTER TABLE "public"."users" DROP COLUMN "old";
CREATE UNIQUE INDEX "age_ix" ON "public"."users" ("age");
DROP INDEX "public"."old_ix";
ALTER TABLE "public"."users" ADD CONSTRAINT "u_org" FOREIGN KEY ("org_id") REFERENCES "public"."orgs" ("id");
ALTER TABLE "public"."users" ADD CONSTRAINT "age_ck" CHECK (age >= 0);
ALTER TABLE "public"."users" DROP CONSTRAINT "u_org";
`
	if plan.Script != want {
		t.Fatalf("got:\n%s\nwant:\n%s", plan.Script, want)
	}
	if plan.Destructive != 3 {
		t.Fatalf("destructive = %d, want 3 (drop column, drop index, drop constraint)", plan.Destructive)
	}
}

func TestDesignAlterDDLMySQLDropsByKind(t *testing.T) {
	plan, err := DesignAlterDDL("mysql", "", "users", []AlterOp{
		{Kind: AlterDropConstraint, Name: "u_org", ConstraintKind: "FOREIGN KEY"},
		{Kind: AlterDropConstraint, Name: "u_mail", ConstraintKind: "UNIQUE"},
		{Kind: AlterDropConstraint, Name: "ck", ConstraintKind: "CHECK"},
		{Kind: AlterDropIndex, Name: "ix"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "ALTER TABLE `users` DROP FOREIGN KEY `u_org`;\nALTER TABLE `users` DROP INDEX `u_mail`;\nALTER TABLE `users` DROP CHECK `ck`;\nDROP INDEX `ix` ON `users`;\n"
	if plan.Script != want {
		t.Fatalf("got:\n%s\nwant:\n%s", plan.Script, want)
	}
}

func TestDesignAlterDDLSQLiteCannotAddConstraints(t *testing.T) {
	if _, err := DesignAlterDDL("sqlite", "", "users", []AlterOp{{Kind: AlterAddCheck, Check: DesignCheck{Expr: "1"}}}); err == nil {
		t.Fatalf("SQLite has no ADD CONSTRAINT: want an error")
	}
	if _, err := DesignAlterDDL("postgres", "", "users", nil); err == nil {
		t.Fatalf("no changes: want an error")
	}
	if _, err := DesignAlterDDL("postgres", "", "users", []AlterOp{{Kind: AlterRenameColumn, Name: "a"}}); err == nil {
		t.Fatalf("rename without a new name: want an error")
	}
}

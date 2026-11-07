package dbclient

import (
	"strings"
	"testing"
)

func TestParseDesignSpecCreate(t *testing.T) {
	spec := `-- a comment
table orders
column id bigint not null pk
column user_id bigint not null
column total numeric(10,2) default 0
index orders_user (user_id)
unique orders_uq (id, user_id)
fk orders_user_fk (user_id) references users (id) on delete cascade
check orders_total_ck total >= 0
`
	d, ops, create, err := ParseDesignSpec(spec)
	if err != nil || !create || ops != nil {
		t.Fatalf("create=%v ops=%v err=%v", create, ops, err)
	}
	if d.Name != "orders" || len(d.Columns) != 3 || !d.Columns[0].NotNull || d.Columns[2].Type != "numeric(10,2)" || d.Columns[2].Default != "0" {
		t.Fatalf("design = %+v", d)
	}
	if len(d.PrimaryKey) != 1 || d.PrimaryKey[0] != "id" {
		t.Fatalf("pk = %v", d.PrimaryKey)
	}
	if len(d.FKs) != 1 || d.FKs[0].RefTable != "users" || d.FKs[0].OnDelete != "CASCADE" {
		t.Fatalf("fk = %+v", d.FKs)
	}
	if len(d.Checks) != 1 || d.Checks[0].Expr != "total >= 0" {
		t.Fatalf("check = %+v", d.Checks)
	}
	ddl, err := DesignCreateDDL("postgres", "public", d)
	if err != nil || !strings.Contains(ddl, "CREATE TABLE") {
		t.Fatalf("ddl = %q, %v", ddl, err)
	}
}

func TestParseDesignSpecAlter(t *testing.T) {
	spec := `add column email text not null default ''
drop column legacy
rename column nm to name
add index users_email (email) unique
drop index users_old
drop constraint users_ck check`
	_, ops, create, err := ParseDesignSpec(spec)
	if err != nil || create {
		t.Fatalf("create=%v err=%v", create, err)
	}
	if len(ops) != 6 || ops[0].Kind != AlterAddColumn || ops[0].Column.Default != "''" || ops[2].NewName != "name" || !ops[3].Index.Unique || ops[5].ConstraintKind != "CHECK" {
		t.Fatalf("ops = %+v", ops)
	}
	plan, err := DesignAlterDDL("postgres", "public", "users", ops)
	if err != nil || plan.Destructive != 3 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

func TestParseDesignSpecErrors(t *testing.T) {
	for _, bad := range []string{"", "-- only a comment", "table t\nfrobnicate x", "add column", "table t\nadd column x int", "rename column a b"} {
		if _, _, _, err := ParseDesignSpec(bad); err == nil {
			t.Fatalf("%q: want an error", bad)
		}
	}
}

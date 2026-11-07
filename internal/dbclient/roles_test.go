package dbclient

import (
	"strings"
	"testing"
)

// DBX-18: the queries behind the users/roles list and grants view.
func TestRoleQueries(t *testing.T) {
	for _, drv := range []string{"postgres", "mysql"} {
		q, ok := RoleQueriesFor(drv)
		if !ok || q.Roles == "" || q.Grants == "" {
			t.Fatalf("%s: %+v %v", drv, q, ok)
		}
	}
	if q, _ := RoleQueriesFor("postgres"); !strings.Contains(q.Roles, "pg_roles") || !strings.Contains(q.Grants, "$1") {
		t.Fatalf("pg: %+v", q)
	}
	if q, _ := RoleQueriesFor("mysql"); !strings.Contains(q.Roles, "mysql.user") || !strings.Contains(q.Grants, "?") {
		t.Fatalf("mysql: %+v", q)
	}
	if _, ok := RoleQueriesFor("sqlite"); ok {
		t.Fatal("sqlite has no roles")
	}
	if MySQLGrantee("o'k", "") != `'o''k'@'%'` {
		t.Fatal(MySQLGrantee("o'k", ""))
	}
}

func TestRoleDDL(t *testing.T) {
	cases := []struct {
		drv   string
		spec  RoleSpec
		alter bool
		want  string
	}{
		{"postgres", RoleSpec{Name: "app", Login: true, CreateDB: true, ConnLimit: 5}, false,
			`CREATE ROLE "app" WITH LOGIN NOSUPERUSER CREATEDB NOCREATEROLE CONNECTION LIMIT 5;`},
		{"postgres", RoleSpec{Name: `we"ird`, ConnLimit: -1}, true,
			`ALTER ROLE "we""ird" WITH NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE CONNECTION LIMIT -1;`},
		{"mysql", RoleSpec{Name: "app", Login: true, ConnLimit: -1}, false, `CREATE USER 'app'@'%';`},
		{"mysql", RoleSpec{Name: "app", Host: "localhost", ConnLimit: 3}, false,
			`CREATE USER 'app'@'localhost' WITH MAX_USER_CONNECTIONS 3 ACCOUNT LOCK;`},
		{"mysql", RoleSpec{Name: "app", Login: true, ConnLimit: -1}, true, `ALTER USER 'app'@'%' ACCOUNT UNLOCK;`},
	}
	for _, c := range cases {
		got, err := RoleDDL(c.drv, c.spec, c.alter)
		if err != nil || got != c.want {
			t.Errorf("%s %+v alter=%v:\n got %q (%v)\nwant %q", c.drv, c.spec, c.alter, got, err, c.want)
		}
	}
	for _, c := range []struct {
		drv  string
		spec RoleSpec
	}{
		{"postgres", RoleSpec{Name: ""}},
		{"postgres", RoleSpec{Name: "a\x00b"}},
		{"mysql", RoleSpec{Name: "app", Superuser: true}},
		{"sqlite", RoleSpec{Name: "app"}},
		{"postgres", RoleSpec{Name: "app", ConnLimit: -2}},
	} {
		if got, err := RoleDDL(c.drv, c.spec, false); err == nil {
			t.Errorf("%s %+v: want error, got %q", c.drv, c.spec, got)
		}
	}
}

func TestGrantDDL(t *testing.T) {
	cases := []struct {
		drv  string
		spec GrantSpec
		want string
	}{
		{"postgres", GrantSpec{Role: "app", Privileges: []string{"select", "INSERT"}, Schema: "public", Table: "users"},
			`GRANT SELECT, INSERT ON TABLE "public"."users" TO "app";`},
		{"postgres", GrantSpec{Role: "app", Privileges: []string{"SELECT"}, Schema: "public"},
			`GRANT SELECT ON ALL TABLES IN SCHEMA "public" TO "app";`},
		{"postgres", GrantSpec{Role: "app", Privileges: []string{"all"}, Schema: "public", Table: "t", WithGrant: true},
			`GRANT ALL PRIVILEGES ON TABLE "public"."t" TO "app" WITH GRANT OPTION;`},
		{"postgres", GrantSpec{Role: "app", Privileges: []string{"DELETE"}, Schema: "s", Table: "t", Revoke: true},
			`REVOKE DELETE ON TABLE "s"."t" FROM "app";`},
		{"mysql", GrantSpec{Role: "app", Privileges: []string{"SELECT", "UPDATE"}, Schema: "shop", Table: "orders"},
			"GRANT SELECT, UPDATE ON `shop`.`orders` TO 'app'@'%';"},
		{"mysql", GrantSpec{Role: "app", Host: "10.0.0.1", Privileges: []string{"ALL"}, Schema: "shop", Revoke: true},
			"REVOKE ALL PRIVILEGES ON `shop`.* FROM 'app'@'10.0.0.1';"},
	}
	for _, c := range cases {
		got, err := GrantDDL(c.drv, c.spec)
		if err != nil || got != c.want {
			t.Errorf("%s %+v:\n got %q (%v)\nwant %q", c.drv, c.spec, got, err, c.want)
		}
	}
	for _, spec := range []GrantSpec{
		{Role: "app", Privileges: []string{"SELECT; DROP TABLE x"}, Schema: "s"},
		{Role: "app", Schema: "s"},
		{Role: "", Privileges: []string{"SELECT"}, Schema: "s"},
		{Role: "app", Privileges: []string{"SELECT"}},
	} {
		if got, err := GrantDDL("postgres", spec); err == nil {
			t.Errorf("%+v: want error, got %q", spec, got)
		}
	}
	if _, err := GrantDDL("mysql", GrantSpec{Role: "app", Privileges: []string{"TRUNCATE"}, Schema: "s"}); err == nil {
		t.Error("TRUNCATE is not a MySQL privilege")
	}
}

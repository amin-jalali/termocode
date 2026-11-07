package dbclient

import (
	"strings"
	"testing"
)

// DBX-18 UI helpers: the grants query with the role inlined, account and
// grant-target parsing, and the role presets the create card offers.

func TestGrantsSQLInlinesRole(t *testing.T) {
	pg, err := GrantsSQL("postgres", "o'neil", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pg, "$1") || !strings.Contains(pg, "grantee = 'o''neil'") {
		t.Errorf("postgres grants = %q", pg)
	}
	my, err := GrantsSQL("mysql", "app", "10.%")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(my, "?") || strings.Count(my, `'''app''@''10.%'''`) != 2 {
		t.Errorf("mysql grants = %q", my)
	}
	if _, err := GrantsSQL("sqlite", "x", ""); err == nil {
		t.Error("sqlite grants: no error")
	}
	if _, err := GrantsSQL("postgres", "a\nb", ""); err == nil {
		t.Error("newline in role: no error")
	}
}

func TestParseAccount(t *testing.T) {
	for _, c := range []struct{ drv, in, name, host string }{
		{"postgres", " app@x ", "app@x", ""},
		{"mysql", "app@10.0.0.%", "app", "10.0.0.%"},
		{"mysql", "app", "app", ""},
		{"mariadb", "a@b@c", "a@b", "c"},
	} {
		n, h := ParseAccount(c.drv, c.in)
		if n != c.name || h != c.host {
			t.Errorf("ParseAccount(%s,%q) = %q,%q want %q,%q", c.drv, c.in, n, h, c.name, c.host)
		}
	}
}

func TestParseGrantTarget(t *testing.T) {
	privs, schema, table, err := ParseGrantTarget("select, Insert ON public.users")
	if err != nil || strings.Join(privs, ",") != "SELECT,INSERT" || schema != "public" || table != "users" {
		t.Errorf("got %v %q %q %v", privs, schema, table, err)
	}
	_, schema, table, err = ParseGrantTarget("ALL on app")
	if err != nil || schema != "app" || table != "" {
		t.Errorf("schema only: %q %q %v", schema, table, err)
	}
	for _, bad := range []string{"", "SELECT", "on public", "SELECT on "} {
		if _, _, _, err := ParseGrantTarget(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestRolePresets(t *testing.T) {
	pg := RolePresets("postgres")
	if len(pg) < 3 {
		t.Fatalf("postgres presets = %d", len(pg))
	}
	for _, drv := range []string{"postgres", "mysql"} {
		for _, p := range RolePresets(drv) {
			spec := p.Spec
			spec.Name = "r"
			if _, err := RoleDDL(drv, spec, false); err != nil {
				t.Errorf("%s preset %s: %v", drv, p.ID, err)
			}
		}
	}
	if len(RolePresets("sqlite")) != 0 {
		t.Error("sqlite has presets")
	}
}

package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-18: users and roles from the palette. Reads run in the console;
// create / alter / grant / revoke write the DDL there and go through the
// run card (Cancel first), so the user sees the statement before it runs.

func dbRolesModel(t *testing.T, driver string) (*Model, *[]string) {
	t.Helper()
	m := hoverModel(t)
	m.databases = newDBState()
	m.databases.conns = []dbclient.Conn{{ID: "c", Name: "c", Driver: driver}}
	m.dbConsole().shown = &dbRun{conn: "c"}
	var ran []string
	old := dbRolesRun
	dbRolesRun = func(_ *Model, conn, stmt string) tea.Cmd {
		ran = append(ran, conn+"|"+stmt)
		return nil
	}
	t.Cleanup(func() { dbRolesRun = old })
	return &m, &ran
}

func TestDBRolesPaletteEntries(t *testing.T) {
	titles := map[string]string{}
	for _, it := range databasesPaletteItems() {
		titles[it.ID] = it.Title
	}
	for _, id := range []string{"db-roles", "db-role-grants", "db-role-create", "db-role-alter", "db-grant", "db-revoke"} {
		if titles[id] == "" {
			t.Errorf("%s not in the palette", id)
		}
		p, ok := aiActionPolicies[id]
		if !ok {
			t.Errorf("%s has no AI policy", id)
		}
		if p.Gate == aiGateAuto {
			t.Errorf("%s AI policy is auto", id)
		}
	}
}

func TestDBRolesShowRunsCatalogQuery(t *testing.T) {
	m, ran := dbRolesModel(t, "postgres")
	if _, ok := m.dispatchDatabasesPalette("db-roles"); !ok {
		t.Fatal("db-roles not dispatched")
	}
	if len(*ran) != 1 || !strings.Contains((*ran)[0], "pg_roles") {
		t.Fatalf("ran = %v", *ran)
	}

	// SQLite: a hint, nothing runs.
	m, ran = dbRolesModel(t, "sqlite")
	m.dispatchDatabasesPalette("db-roles")
	if len(*ran) != 0 {
		t.Fatalf("sqlite ran %v", *ran)
	}
}

func TestDBRolesGrantsPromptInlinesRole(t *testing.T) {
	m, ran := dbRolesModel(t, "mysql")
	m.dispatchDatabasesPalette("db-role-grants")
	if !m.promptOpen || m.promptKind != promptKindDBRoleGrants {
		t.Fatalf("prompt=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	if _, ok := m.handleHTTPPromptSubmit("app@%"); !ok {
		t.Fatal("prompt not routed")
	}
	if len(*ran) != 1 || !strings.Contains((*ran)[0], `'''app''@''%'''`) {
		t.Fatalf("ran = %v", *ran)
	}
}

func TestDBRolesCreateGoesThroughPresetThenRunCard(t *testing.T) {
	m, ran := dbRolesModel(t, "postgres")
	m.dispatchDatabasesPalette("db-role-create")
	if !m.promptOpen || m.promptKind != promptKindDBRoleName {
		t.Fatalf("prompt=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	m.handleHTTPPromptSubmit("reporting")
	if !m.pickerOpen || m.pickerKind != pickerKindDBRolePreset {
		t.Fatalf("picker=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	m.pickerOpen = false
	if _, ok := m.databaseConsolePicker("login"); !ok {
		t.Fatal("preset pick not routed")
	}
	if len(*ran) != 1 || !strings.Contains((*ran)[0], `CREATE ROLE "reporting" WITH LOGIN`) {
		t.Fatalf("ran = %v", *ran)
	}
}

func TestDBRolesAlterWritesAlter(t *testing.T) {
	m, ran := dbRolesModel(t, "postgres")
	m.dispatchDatabasesPalette("db-role-alter")
	m.promptOpen = false
	m.handleHTTPPromptSubmit("app")
	m.pickerOpen = false
	m.databaseConsolePicker("group")
	if len(*ran) != 1 || !strings.Contains((*ran)[0], `ALTER ROLE "app" WITH NOLOGIN`) {
		t.Fatalf("ran = %v", *ran)
	}
}

func TestDBRolesGrantAndRevoke(t *testing.T) {
	m, ran := dbRolesModel(t, "postgres")
	m.dispatchDatabasesPalette("db-grant")
	if m.promptKind != promptKindDBGrantRole {
		t.Fatalf("kind=%v", m.promptKind)
	}
	m.promptOpen = false
	m.handleHTTPPromptSubmit("app")
	if !m.promptOpen || m.promptKind != promptKindDBGrantTarget {
		t.Fatalf("target prompt=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	m.handleHTTPPromptSubmit("SELECT, UPDATE on public.users")
	if len(*ran) != 1 || !strings.Contains((*ran)[0], `GRANT SELECT, UPDATE ON TABLE "public"."users" TO "app";`) {
		t.Fatalf("ran = %v", *ran)
	}

	m, ran = dbRolesModel(t, "postgres")
	m.dispatchDatabasesPalette("db-revoke")
	m.promptOpen = false
	m.handleHTTPPromptSubmit("app")
	m.promptOpen = false
	m.handleHTTPPromptSubmit("ALL on public")
	if len(*ran) != 1 || !strings.Contains((*ran)[0], `REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA "public" FROM "app";`) {
		t.Fatalf("ran = %v", *ran)
	}

	// A bad target is refused with nothing run.
	m, ran = dbRolesModel(t, "postgres")
	m.dispatchDatabasesPalette("db-grant")
	m.promptOpen = false
	m.handleHTTPPromptSubmit("app")
	m.promptOpen = false
	m.handleHTTPPromptSubmit("FLY on public")
	if len(*ran) != 0 {
		t.Fatalf("bad privilege ran %v", *ran)
	}
}

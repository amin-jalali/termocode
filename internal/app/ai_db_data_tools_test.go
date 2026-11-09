package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// db_data asks where the GUI asks: an import, a dump and a restore show
// the GUI's card every time (no Always allow); an export writes a file,
// so it asks as write_file does, and replacing a file always asks.
func TestAIDBDataGateMatchesTheGUI(t *testing.T) {
	e := newAIDBEnv(t)
	tl := e.tool(t, "db_data")
	for op, class := range map[string]string{"import_csv": "", "dump": "", "restore": "", "export": ai.ClassFileWrite} {
		if ask, got := tl.Gate(map[string]any{"op": op}); !ask || got != class {
			t.Errorf("op %s: ask=%v class=%q, want a card with class %q", op, ask, got, class)
		}
	}
	if err := os.WriteFile(filepath.Join(e.root, "old.csv"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ask, class := tl.Gate(map[string]any{"op": "export", "conn": "local", "sql": "SELECT 1", "path": "old.csv"}); !ask || class != "" {
		t.Errorf("replacing a file: ask=%v class=%q, want an ask with no Always allow", ask, class)
	}
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"op": "export", "conn": "local", "sql": "DELETE FROM users", "path": "a.csv"}, "read"},
		{map[string]any{"op": "export", "conn": "local", "sql": "SELECT 1", "path": "../a.csv"}, "inside the workspace"},
		{map[string]any{"op": "export", "conn": "local", "sql": "SELECT 1", "path": ".env"}, "secret"},
		{map[string]any{"op": "export", "conn": "local", "sql": "SELECT 1", "path": "a.csv", "format": "xls"}, "format"},
		{map[string]any{"op": "import_csv", "conn": "ro", "path": "a.csv", "table": "users"}, "read-only"},
		{map[string]any{"op": "import_csv", "conn": "local", "path": "a.txt", "table": "users"}, ".csv"},
		{map[string]any{"op": "restore", "conn": "ro", "path": "a.sql"}, "read-only"},
		{map[string]any{"op": "restore", "conn": "local", "path": "missing.sql"}, "not a file"},
		{map[string]any{"op": "dump", "conn": "local"}, "path"},
		{map[string]any{"op": "zip", "conn": "local"}, "op"},
	} {
		if _, _, err := e.gate(t, "db_data", tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err %v, want %q", tc.args, err, tc.want)
		}
	}
}

// export runs the read in the AI's read-only session and writes the rows
// to a workspace file in the format asked.
func TestAIDBDataExportWritesTheFile(t *testing.T) {
	e := newAIDBEnv(t)
	out, err := e.run(t, "db_data", map[string]any{"op": "export", "conn": "local", "sql": "SELECT id, email FROM users", "path": "out/users.csv"})
	if err != nil || !strings.Contains(out, "out/users.csv") {
		t.Fatalf("export: %q %v", out, err)
	}
	b, err := os.ReadFile(filepath.Join(e.root, "out", "users.csv"))
	if err != nil || !strings.Contains(string(b), "id,email") || !strings.Contains(string(b), "ada@example.com") {
		t.Fatalf("file: %q %v", b, err)
	}
	if len(e.opened) == 0 || !e.opened[0].readOnly && len(e.opened[0].clones) == 0 {
		t.Fatal("the export did not read in a read-only session")
	}
	if _, err := e.run(t, "db_data", map[string]any{"op": "export", "conn": "local", "sql": "SELECT id FROM users", "path": "out/users.json", "format": "json"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(e.root, "out", "users.json")); !strings.Contains(string(b), `"id"`) {
		t.Fatalf("json: %s", b)
	}
}

// import_csv names the file, the table and the file's columns on the
// card, then inserts in the AI's own write session.
func TestAIDBDataImportCSV(t *testing.T) {
	e := newAIDBEnv(t)
	if err := os.WriteFile(filepath.Join(e.root, "people.csv"), []byte("id,email\n7,bo@example.com\n8,cy@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"op": "import_csv", "conn": "local", "path": "people.csv", "table": "users"}
	card := e.tool(t, "db_data").Human(args)
	for _, want := range []string{"Import 2 rows from people.csv into public.users on local", "id, email", "one transaction"} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	out, err := e.run(t, "db_data", args)
	if err != nil || !strings.Contains(out, "2 rows") {
		t.Fatalf("import: %q %v", out, err)
	}
	inserted := false
	for _, s := range e.opened {
		if !s.readOnly && strings.Contains(strings.Join(s.ran, ";"), "INSERT INTO") {
			inserted = true
		}
	}
	if !inserted {
		t.Fatalf("no INSERT in a write session: %v", e.log)
	}
}

// dump and restore run the server's own tool on a connected connection;
// the card shows the command, never the password; the result hides it.
func TestAIDBDataDumpAndRestore(t *testing.T) {
	e := newAIDBEnv(t)
	oldLook, oldRun := dbToolLookPath, dbToolRun
	t.Cleanup(func() { dbToolLookPath, dbToolRun = oldLook, oldRun })
	dbToolLookPath = func(name string) (string, error) { return "/fake/bin/" + name, nil }
	var ran []dbclient.CLICmd
	dbToolRun = func(_ context.Context, path string, c dbclient.CLICmd) (string, error) {
		ran = append(ran, c)
		return "warning: pw-secret-9 was used", nil
	}
	dump := map[string]any{"op": "dump", "conn": "local", "path": "backup/app.dump"}
	if _, err := e.run(t, "db_data", dump); err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("dump of a closed connection: %v", err)
	}
	e.m.databases.sessions["local"] = &aiDBSession{name: "user"}
	e.m.databases.passwords["local"] = "pw-secret-9"
	card := e.tool(t, "db_data").Human(dump)
	if !strings.Contains(card, "pg_dump") || !strings.Contains(card, "backup/app.dump") || strings.Contains(card, "pw-secret-9") {
		t.Errorf("dump card:\n%s", card)
	}
	out, err := e.run(t, "db_data", dump)
	if err != nil || strings.Contains(out, "pw-secret-9") || !strings.Contains(out, "Dumped") {
		t.Fatalf("dump: %q %v", out, err)
	}
	if len(ran) != 1 || strings.Contains(strings.Join(ran[0].Args, " "), "pw-secret-9") {
		t.Fatalf("dump command: %+v", ran)
	}
	if err := os.WriteFile(filepath.Join(e.root, "backup", "app.sql"), []byte("SELECT 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := map[string]any{"op": "restore", "conn": "local", "path": "backup/app.sql"}
	if card := e.tool(t, "db_data").Human(restore); !strings.Contains(card, "cannot be undone") {
		t.Errorf("restore card:\n%s", card)
	}
	if out, err := e.run(t, "db_data", restore); err != nil || !strings.Contains(out, "Restored") {
		t.Fatalf("restore: %q %v", out, err)
	}
	if len(ran) != 2 {
		t.Fatalf("restore did not run: %+v", ran)
	}
}

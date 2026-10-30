package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestDB is a web hub on a workspace with the AI tests' three Postgres
// connections and a fake session factory: no database is ever reached.
func webTestDB(t *testing.T, result func(sql string) dbclient.Result) (*Model, *webConn, *[]*aiDBSession) {
	t.Helper()
	m := aiTestModel(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(aiDBConns), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ws = workspace.Workspace{Root: root}
	m.output = newOutputStore()
	m.databases = newDBState()
	m.databases.load(root)
	for _, id := range []string{"local", "prod", "ro"} {
		m.databases.passwords[id] = "pw-" + "web-db-" + id
	}
	var mu sync.Mutex
	opened := &[]*aiDBSession{}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(_ context.Context, c dbclient.Conn, pw string) (dbclient.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		s := &aiDBSession{name: "web-" + c.ID, result: result}
		*opened = append(*opened, s)
		return s, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	webTestHub(t, &m)
	return &m, webTestSub(t, &m), opened
}

// Spec (WEB-43, T43): db.conns lists the connections with no host, user,
// URL or password; db.tables lists the default schema's tables; db.rows
// pages a listed table through a read-only session; a cell of a secret
// column and the password itself are masked; a table not listed is
// not_found.
func TestDBBrowseMasksSecrets(t *testing.T) {
	pw := "pw-" + "web-db-local"
	m, c, opened := webTestDB(t, func(sql string) dbclient.Result {
		if strings.Contains(sql, "information_schema") {
			return dbclient.Result{Columns: []dbclient.Column{{Name: "table_name"}, {Name: "table_type"}},
				Rows: [][]dbclient.Cell{{{Text: "users"}, {Text: "BASE TABLE"}}, {{Text: "active"}, {Text: "VIEW"}}}}
		}
		return dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}, {Name: "password_hash"}, {Name: "note"}, {Name: "apiKey"}},
			Rows: [][]dbclient.Cell{
				{{Text: "1"}, {Text: "ada@example.com"}, {Text: "$2b$10$abc"}, {Text: "uses " + pw}, {Text: "k-1"}},
				{{Text: "2"}, {Null: true}, {Text: "$2b$10$def"}, {Text: "fine"}, {Text: "k-2"}},
			}}
	})
	var cs webproto.DBConns
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBConns, `{}`), &cs)
	if len(cs.Conns) != 3 || cs.Conns[0].ID != "local" || cs.Conns[0].Database != "app" || !cs.Conns[0].SQL || cs.Conns[1].Env != "production" || !cs.Conns[2].ReadOnly {
		t.Fatalf("conns %+v", cs)
	}
	if raw, _ := webData(cs); strings.Contains(string(raw), "localhost") || strings.Contains(string(raw), "db.example") || strings.Contains(string(raw), `"app"`) && strings.Contains(string(raw), "user") {
		t.Fatalf("a host or user left the hub: %s", raw)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBRows, `{"conn":"local","table":"users"}`); webTestErr(f) != "not_found" {
		t.Fatalf("rows before the list: %v", f)
	}
	var tb webproto.DBTables
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBTables, `{"conn":"local"}`), &tb)
	if tb.Schema != "public" || len(tb.Tables) != 2 || tb.Tables[0].Name != "users" || !tb.Tables[1].View {
		t.Fatalf("tables %+v", tb)
	}
	var rows webproto.DBRows
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBRows, `{"conn":"local","table":"users"}`), &rows)
	if strings.Join(rows.Columns, ",") != "id,email,password_hash,note,apiKey" || len(rows.Rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	r0 := rows.Rows[0]
	if r0[1].V != "ada@example.com" || !r0[2].M || !r0[3].M || !r0[4].M || rows.Masked != 5 || !rows.Rows[1][1].N {
		t.Errorf("cells %+v masked %d", rows.Rows, rows.Masked)
	}
	if raw, _ := webData(rows); strings.Contains(string(raw), pw) || strings.Contains(string(raw), "$2b$") || strings.Contains(string(raw), "k-1") {
		t.Fatalf("a secret left the hub: %s", raw)
	}
	for _, s := range *opened {
		for _, cl := range s.clones {
			for _, q := range cl.ran {
				if strings.HasPrefix(q, "SELECT * FROM") && q != `SELECT * FROM "public"."users" LIMIT 101 OFFSET 0` {
					t.Errorf("ran %q", q)
				}
			}
		}
		if len(s.ran) != 0 {
			t.Errorf("ran on a writable session: %q", s.ran)
		}
	}
}

// Spec (WEB-43): db.query runs one read in a read-only session; a
// statement the classifier does not call a read is forbidden there and
// goes to db.exec, which waits for the laptop (showing the exact SQL,
// never the host or the password); a read-only connection refuses it;
// two statements, or a transaction statement, are refused.
func TestDBWriteNeedsLaptopCard(t *testing.T) {
	m, c, opened := webTestDB(t, nil)
	var rows webproto.DBRows
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBQuery, `{"conn":"local","sql":"SELECT id, email FROM users"}`), &rows)
	if len(rows.Rows) != 1 || rows.Rows[0][1].V != "ada@example.com" {
		t.Fatalf("query %+v", rows)
	}
	upd := `{"conn":"local","sql":"UPDATE users SET email = 'x' WHERE id = 7"}`
	if f := webTestCallBg(t, m, c, webproto.OpDBQuery, upd); webTestErr(f) != "forbidden" {
		t.Fatalf("a write as a query: %v", f)
	}
	for _, bad := range []string{`{"conn":"local","sql":"SELECT 1; DELETE FROM users"}`, `{"conn":"local","sql":"BEGIN"}`} {
		if f := webTestCallBg(t, m, c, webproto.OpDBQuery, bad); webTestErr(f) != "bad_request" {
			t.Errorf("%s: %v", bad, f)
		}
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBExec, `{"conn":"ro","sql":"DELETE FROM users"}`); webTestErr(f) != "forbidden" {
		t.Fatalf("a write on a read-only connection: %v", f)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBExec, `{"conn":"local","sql":"SELECT 1"}`); webTestErr(f) != "bad_request" {
		t.Fatalf("a read through exec: %v", f)
	}
	n := len(*opened)
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBExec, upd), &ref)
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatal("no laptop card")
	}
	v := m.confirm.View()
	if !strings.Contains(v, "UPDATE users SET email") || strings.Contains(v, "localhost") || strings.Contains(v, "pw-") {
		t.Errorf("card:\n%s", v)
	}
	if len(*opened) != n {
		t.Fatal("a session opened before the laptop said so")
	}
	webTestLaptopSelect(t, m, "confirm")
	last := (*opened)[len(*opened)-1]
	if len(last.ran) != 1 || !strings.HasPrefix(last.ran[0], "UPDATE users") {
		t.Fatalf("after the laptop's yes ran %q", last.ran)
	}
	if !strings.Contains(webTestToasts(*m), "SQL ran on local") {
		t.Errorf("toasts %s", webTestToasts(*m))
	}
	if a := webTestAudit(m); strings.Contains(a, "UPDATE") || !strings.Contains(a, "db.exec  local ") {
		t.Errorf("audit (never the SQL text):\n%s", a)
	}
}

// Spec (WEB-43): the secret column words.
func TestDBSecretColumns(t *testing.T) {
	for _, n := range []string{"password", "password_hash", "api_key", "apiKey", "AccessToken", "client_secret", "private_key", "otp", "auth_token", "key"} {
		if !webDBSecretColumn(n) {
			t.Errorf("%s not masked", n)
		}
	}
	for _, n := range []string{"id", "email", "author", "monkey", "keyboard_layout", "created_at", "name"} {
		if webDBSecretColumn(n) {
			t.Errorf("%s masked", n)
		}
	}
}

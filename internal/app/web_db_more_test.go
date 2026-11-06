package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webDBMoreConns are the AI tests' Postgres connections plus two Redis ones.
const webDBMoreConns = `{"connections":[` +
	`{"id":"local","driver":"postgres","host":"localhost","port":5432,"user":"app","database":"app"},` +
	`{"id":"prod","driver":"postgres","host":"db.example","port":5432,"database":"app","environment":"production"},` +
	`{"id":"ro","driver":"postgres","host":"ro.example","port":5432,"database":"app","readOnly":true},` +
	`{"id":"cache","driver":"redis","host":"cache.internal","port":6379,"password":"${env:TC_WEB_REDIS_PW}"},` +
	`{"id":"ro-cache","driver":"redis","host":"ro.internal","port":6379,"readOnly":true}]}`

// webTestDBMore is webTestDB with the Redis connections and a fake Redis.
func webTestDBMore(t *testing.T, result func(sql string) dbclient.Result) (*Model, *webConn, *[]*aiDBSession, *dbtest.FakeRedis) {
	t.Helper()
	m, c, opened := webTestDB(t, result)
	t.Setenv("TC_WEB_REDIS_PW", "s3cret-"+"web-redis")
	if err := os.WriteFile(filepath.Join(m.ws.Root, ".termocode", "databases.json"), []byte(webDBMoreConns), 0o644); err != nil {
		t.Fatal(err)
	}
	m.databases.load(m.ws.Root)
	f := dbtest.NewFakeRedis()
	f.Password = "s3cret-" + "web-redis"
	f.HSet(0, "session:ab12", "plan", "pro", "token", "tok-"+"value")
	f.Expire(0, "session:ab12", time.Hour)
	f.SetString(0, "counter", "41")
	t.Cleanup(SetRedisDialForTest(f.Dial))
	t.Cleanup(f.Close)
	return m, c, opened, f
}

// webDBRanAny is every statement the fake sessions ran, read-only clones
// marked "ro: ".
func webDBRanAny(opened []*aiDBSession) (rw, ro []string) {
	for _, s := range opened {
		s.mu.Lock()
		rw = append(rw, s.ran...)
		for _, cl := range s.clones {
			cl.mu.Lock()
			ro = append(ro, cl.ran...)
			cl.mu.Unlock()
		}
		s.mu.Unlock()
	}
	return rw, ro
}

// webDBRanClones is what the read-only clones of s (and theirs) ran.
func webDBRanClones(s *aiDBSession) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, cl := range s.clones {
		cl.mu.Lock()
		out = append(out, cl.ran...)
		cl.mu.Unlock()
		out = append(out, webDBRanClones(cl)...)
	}
	return out
}

func webDBRes(t *testing.T, f map[string]any) {
	t.Helper()
	if f["t"] != "res" {
		t.Fatalf("not an answer: %v", f)
	}
}

// Spec (web parity, Databases): the reads. A connection lists the
// laptop session's state (connected, manual mode, open transaction and
// its writes); a table page names its key columns; db.ddl gives a listed
// table's DDL; db.explain gives the plan of a statement, analyze only of
// a read; db.test, db.compare and the rest read through read-only
// sessions and nothing ever runs on a writable one; db.history is
// redacted and newest first; db.csv_files lists the workspace's CSV
// files; db.config names the connections file; redis.keys lists names,
// types and TTLs, never a value or the password.
func TestDBMoreReads(t *testing.T) {
	result := func(sql string) dbclient.Result {
		switch {
		case strings.HasPrefix(sql, "EXPLAIN"):
			return dbclient.Result{Columns: []dbclient.Column{{Name: "QUERY PLAN"}},
				Rows: [][]dbclient.Cell{{{Text: `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"users","Total Cost":1.5,"Plan Rows":1}}]`}}}}
		case strings.Contains(sql, "information_schema.tables") && strings.Contains(sql, "table_type"):
			return dbclient.Result{Columns: []dbclient.Column{{Name: "table_name"}, {Name: "table_type"}},
				Rows: [][]dbclient.Cell{{{Text: "users"}, {Text: "BASE TABLE"}}, {{Text: "active"}, {Text: "VIEW"}}}}
		case strings.HasPrefix(sql, "SELECT * FROM"):
			return dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}}, Rows: [][]dbclient.Cell{{{Text: "1"}, {Text: "ada@example.com"}}}}
		}
		return dbclient.Result{} // the catalogs: an empty schema
	}
	m, c, opened, _ := webTestDBMore(t, result)
	userLocal := &aiDBSession{name: "user-local", result: result}
	m.databases.sessions["local"] = userLocal
	m.databases.manual = map[string]bool{"local": true}
	m.databases.tx("local").open = true
	m.databases.tx("local").writes = 2
	var cs webproto.DBConns
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBConns, `{}`), &cs)
	byID := map[string]webproto.DBConn{}
	for _, x := range cs.Conns {
		byID[x.ID] = x
	}
	if l := byID["local"]; !l.Connected || !l.Manual || !l.Tx || l.Writes != 2 || byID["prod"].Connected || byID["cache"].Driver != "redis" {
		t.Fatalf("conns %+v", cs.Conns)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBDDL, `{"conn":"local","table":"users"}`); webTestErr(f) != "not_found" {
		t.Fatalf("ddl before the list: %v", f)
	}
	webDBRes(t, webTestCallBg(t, m, c, webproto.OpDBTables, `{"conn":"local"}`))
	var rows webproto.DBRows
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBRows, `{"conn":"local","table":"users"}`), &rows)
	if strings.Join(rows.Key, ",") != "id" {
		t.Errorf("the page's key columns %q", rows.Key)
	}
	var ddl webproto.DBText
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBDDL, `{"conn":"local","table":"users"}`), &ddl)
	if !strings.Contains(ddl.Text, "CREATE TABLE") || !strings.Contains(ddl.Text, "users") {
		t.Errorf("ddl %q", ddl.Text)
	}
	var plan webproto.DBPlan
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBExplain, `{"conn":"local","sql":"SELECT * FROM users"}`), &plan)
	if len(plan.Lines) == 0 || !strings.Contains(plan.Lines[0].Text, "Seq Scan") || plan.Analyze {
		t.Errorf("plan %+v", plan)
	}
	// An analyzed read runs read-only (the fake refuses it as the server
	// guard would refuse a write; a real server runs the read).
	webTestCallBg(t, m, c, webproto.OpDBExplain, `{"conn":"local","sql":"SELECT * FROM users","analyze":true}`)
	if f := webTestCallBg(t, m, c, webproto.OpDBExplain, `{"conn":"local","sql":"DELETE FROM users","analyze":true}`); webTestErr(f) != "forbidden" {
		t.Errorf("an analyzed write: %v", f)
	}
	var done webproto.DBDone
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBTest, `{"conn":"prod"}`), &done)
	if !strings.Contains(done.Text, "works") {
		t.Errorf("test %+v", done)
	}
	var cmp webproto.DBCompare
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBCompare, `{"conn":"local","target":"prod"}`), &cmp)
	if cmp.Changes != 0 || cmp.Script == "" {
		t.Errorf("compare %+v", cmp)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBCompare, `{"conn":"local","target":"cache"}`); webTestErr(f) != "bad_request" {
		t.Errorf("postgres with redis: %v", f)
	}
	rw, ro := webDBRanAny(*opened)
	ro = append(ro, webDBRanClones(userLocal)...)
	if len(rw) != 0 {
		t.Errorf("ran on a writable session: %q", rw)
	}
	if !strings.Contains(strings.Join(ro, "\n"), "EXPLAIN (ANALYZE, FORMAT JSON) SELECT * FROM users") {
		t.Errorf("the analyzed read did not run read-only: %q", ro)
	}

	h := &dbclient.History{Path: dbConfigPath("history.jsonl")}
	for i, e := range []dbclient.HistoryEntry{{Conn: "local", SQL: "SELECT 1"}, {Conn: "prod", SQL: "SELECT 2"}, {Conn: "local", SQL: "ALTER USER app PASSWORD 'hunter2-" + "web'"}} {
		e.ID, e.Time = string(rune('a'+i)), time.Unix(int64(1700000000+i), 0)
		if err := h.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	var hist webproto.DBHistory
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBHistory, `{"conn":"local"}`), &hist)
	if len(hist.Entries) != 2 || hist.Entries[1].SQL != "SELECT 1" || hist.Entries[0].At != 1700000002000 {
		t.Errorf("history %+v", hist)
	}
	if raw, _ := json.Marshal(hist); strings.Contains(string(raw), "hunter2") {
		t.Errorf("a password left the hub: %s", raw)
	}

	if err := os.MkdirAll(filepath.Join(m.ws.Root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.ws.Root, "data", "users.csv"), []byte("email\na@x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var files webproto.DBFiles
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBCSVFiles, `{}`), &files)
	if strings.Join(files.Files, ",") != "data/users.csv" {
		t.Errorf("csv files %+v", files)
	}
	var cfg webproto.DBConfigFile
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBConfig, `{}`), &cfg)
	if cfg.Path != ".termocode/databases.json" || cfg.Created {
		t.Errorf("config %+v", cfg)
	}

	var keys webproto.RedisKeys
	webTestData(t, webTestCallBg(t, m, c, webproto.OpRedisKeys, `{"conn":"cache","pattern":"session:*"}`), &keys)
	if len(keys.Keys) != 1 || keys.Keys[0].Key != "session:ab12" || keys.Keys[0].Type != "hash" || keys.Keys[0].TTL <= 0 {
		t.Fatalf("keys %+v", keys)
	}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpRedisKeys, `{"conn":"cache"}`), &keys)
	if len(keys.Keys) != 2 {
		t.Errorf("all keys %+v", keys)
	}
	if raw, _ := json.Marshal(keys); strings.Contains(string(raw), "tok-") || strings.Contains(string(raw), "s3cret") || strings.Contains(string(raw), "pro") {
		t.Errorf("a value or the password left the hub: %s", raw)
	}
	if f := webTestCallBg(t, m, c, webproto.OpRedisKeys, `{"conn":"local"}`); webTestErr(f) != "bad_request" {
		t.Errorf("redis keys of postgres: %v", f)
	}
}

// Spec (web parity): db.cancel stops the page's own reads on a
// connection, not another page's nor another connection's.
func TestDBCancelStopsOwnReads(t *testing.T) {
	var r webDBRuns
	a, stopA := r.start("c1", "local", context.Background())
	b, stopB := r.start("c2", "local", context.Background())
	d, stopD := r.start("c1", "prod", context.Background())
	defer stopA()
	defer stopB()
	defer stopD()
	if n := r.cancel("c1", "local"); n != 1 {
		t.Fatalf("cancelled %d", n)
	}
	if a.Err() == nil || b.Err() != nil || d.Err() != nil {
		t.Errorf("a %v b %v d %v", a.Err(), b.Err(), d.Err())
	}
	stopB()
	if n := r.cancel("c2", "local"); n != 0 {
		t.Errorf("a finished read was cancelled again: %d", n)
	}
}

// webTxSession records the laptop session's commit and rollback.
type webTxSession struct {
	*aiDBSession
	mu   sync.Mutex
	ends []string
}

func (s *webTxSession) Commit(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ends = append(s.ends, "commit")
	return nil
}

func (s *webTxSession) Rollback(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ends = append(s.ends, "rollback")
	return nil
}

// webDBAsk asks op and checks the laptop card: Cancel first, every want
// in its text, no host or password; nothing has run yet.
func webDBAsk(t *testing.T, m *Model, c *webConn, op, args string, want ...string) {
	t.Helper()
	var ref webproto.AskRef
	webTestData(t, webTestCallBg(t, m, c, op, args), &ref)
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
		t.Fatalf("%s: no laptop card", op)
	}
	v := m.confirm.View()
	for _, w := range want {
		if !strings.Contains(v, w) {
			t.Errorf("%s card has no %q:\n%s", op, w, v)
		}
	}
	if strings.Contains(v, "pw-web-db") || strings.Contains(v, "s3cret") {
		t.Errorf("%s card shows a password:\n%s", op, v)
	}
}

// Spec (web parity, Databases): every change waits for a laptop card
// (Cancel first) and runs only after its yes. Row edits name a listed
// table, find rows by the table's real key, never set a secret column,
// show each statement with its values and run in one transaction on a
// fresh session; a CSV import names a workspace file, shows its rows,
// columns and INSERT and runs in one transaction; a Redis key must be
// one redis.keys listed; a read-only connection refuses every change.
func TestDBMoreChangesNeedLaptopCard(t *testing.T) {
	m, c, opened, f := webTestDBMore(t, func(sql string) dbclient.Result {
		if k, _ := dbclient.Classify(sql, "postgres"); k != dbclient.KindRead {
			return dbclient.Result{Affected: 1}
		}
		if strings.Contains(sql, "information_schema") {
			return dbclient.Result{Columns: []dbclient.Column{{Name: "table_name"}, {Name: "table_type"}}, Rows: [][]dbclient.Cell{{{Text: "users"}, {Text: "BASE TABLE"}}}}
		}
		return dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}}, Rows: [][]dbclient.Cell{{{Text: "1"}, {Text: "ada@example.com"}}}}
	})
	edit := `{"conn":"local","table":"users","edits":[` +
		`{"kind":"update","key":[{"col":"id","v":"1"}],"set":[{"col":"email","v":"new@example.com"}],"old":[{"col":"email","v":"ada@example.com"}]},` +
		`{"kind":"delete","key":[{"col":"id","v":"2"}]},{"kind":"insert","set":[{"col":"email","n":true}]}]}`
	if f := webTestCallBg(t, m, c, webproto.OpDBEdit, edit); webTestErr(f) != "not_found" {
		t.Fatalf("edit before the list: %v", f)
	}
	webDBRes(t, webTestCallBg(t, m, c, webproto.OpDBTables, `{"conn":"local"}`))
	webDBRes(t, webTestCallBg(t, m, c, webproto.OpDBTables, `{"conn":"ro"}`))
	for _, bad := range []struct{ args, code string }{
		{`{"conn":"local","table":"users","edits":[{"kind":"update","key":[{"col":"email","v":"x"}],"set":[{"col":"email","v":"y"}]}]}`, "bad_request"},
		{`{"conn":"local","table":"users","edits":[{"kind":"update","key":[{"col":"id","v":"1"}],"set":[{"col":"password_hash","v":"x"}]}]}`, "forbidden"},
		{`{"conn":"local","table":"users","edits":[{"kind":"update","key":[{"col":"id","v":"` + webproto.DBMaskedValue + `"}],"set":[{"col":"email","v":"y"}]}]}`, "bad_request"},
		{`{"conn":"ro","table":"users","edits":[{"kind":"delete","key":[{"col":"id","v":"1"}]}]}`, "forbidden"},
	} {
		if f := webTestCallBg(t, m, c, webproto.OpDBEdit, bad.args); webTestErr(f) != bad.code {
			t.Errorf("%s: %v, want %s", bad.args, f, bad.code)
		}
	}
	n := len(*opened)
	webDBAsk(t, m, c, webproto.OpDBEdit, edit, "3 changes", "users", `DELETE FROM "public"."users" WHERE "id" = '2'`, `'new@example.com'`, `INSERT INTO "public"."users" ("email") VALUES (NULL)`)
	if len(*opened) != n+1 { // the key lookup's read-only one
		t.Fatalf("sessions before the yes: %d", len(*opened)-n)
	}
	webTestLaptopSelect(t, m, "confirm")
	last := (*opened)[len(*opened)-1]
	if got := strings.Join(last.ran, "\n"); !strings.HasPrefix(got, "DELETE") || !strings.Contains(got, "UPDATE") || !strings.Contains(got, "INSERT") {
		t.Fatalf("after the yes ran %q", last.ran)
	}
	if !strings.Contains(webTestToasts(*m), "3 changes saved") {
		t.Errorf("toasts %s", webTestToasts(*m))
	}
	if a := webTestAudit(m); strings.Contains(a, "new@example.com") || !strings.Contains(a, "db.edit") {
		t.Errorf("audit (never the values):\n%s", a)
	}

	if err := os.MkdirAll(filepath.Join(m.ws.Root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.ws.Root, "data", "users.csv"), []byte("email,nick\na@x,Ada\nb@x,Bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBImport, `{"conn":"local","table":"users","path":"data/missing.csv"}`); webTestErr(f) == "" {
		t.Errorf("a missing file: %v", f)
	}
	webDBAsk(t, m, c, webproto.OpDBImport, `{"conn":"local","table":"users","path":"data/users.csv"}`, "2 rows", "data/users.csv", `INSERT INTO "public"."users" ("email")`, "nick")
	webTestLaptopSelect(t, m, "confirm")
	last = (*opened)[len(*opened)-1]
	if got := strings.Join(last.ran, "\n"); !strings.Contains(got, `INSERT INTO "public"."users" ("email") VALUES ($1), ($2)`) {
		t.Errorf("import ran %q", last.ran)
	}
	if !strings.Contains(webTestToasts(*m), "Imported 2 rows") {
		t.Errorf("toasts %s", webTestToasts(*m))
	}

	if f := webTestCallBg(t, m, c, webproto.OpRedisKey, `{"conn":"cache","key":"counter","verb":"delete"}`); webTestErr(f) != "not_found" {
		t.Fatalf("a key not listed: %v", f)
	}
	webDBRes(t, webTestCallBg(t, m, c, webproto.OpRedisKeys, `{"conn":"cache"}`))
	webDBAsk(t, m, c, webproto.OpRedisKey, `{"conn":"cache","key":"counter","verb":"delete"}`, "counter", "Delete")
	if !f.Exists(0, "counter") {
		t.Fatal("deleted before the yes")
	}
	webTestLaptopSelect(t, m, "confirm")
	if f.Exists(0, "counter") {
		t.Error("the key is still there after the yes")
	}
	webDBAsk(t, m, c, webproto.OpRedisKey, `{"conn":"cache","key":"session:ab12","verb":"ttl","ttl":60}`, "session:ab12", "60")
	webTestLaptopSelect(t, m, "confirm")
	if !f.Sent("EXPIRE") {
		t.Errorf("no EXPIRE: %q", f.Commands())
	}
	if f := webTestCallBg(t, m, c, webproto.OpRedisKey, `{"conn":"ro-cache","key":"counter","verb":"delete"}`); webTestErr(f) != "forbidden" {
		t.Errorf("a read-only redis: %v", f)
	}
}

// Spec (web parity, Databases): the saved connections and the laptop's
// session. db.add saves a new project connection with no password after
// the laptop's yes (the laptop asks for it on connect); db.ssh and db.ssl
// change a saved connection; db.connect starts the laptop's own connect;
// db.disconnect and db.tx act on the laptop's session after a card.
func TestDBMoreConnectionsNeedLaptopCard(t *testing.T) {
	m, c, opened, _ := webTestDBMore(t, nil)
	cfg := func() string {
		b, err := os.ReadFile(filepath.Join(m.ws.Root, ".termocode", "databases.json"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBAdd, `{"id":"local","driver":"postgres","host":"h"}`); webTestErr(f) != "conflict" {
		t.Errorf("a used id: %v", f)
	}
	webDBAsk(t, m, c, webproto.OpDBAdd, `{"id":"app2","name":"App 2","driver":"postgres","host":"db2.internal","user":"app","database":"app2","env":"staging"}`,
		"App 2", "db2.internal:5432", "app2", "No password")
	if strings.Contains(cfg(), "app2") {
		t.Fatal("saved before the yes")
	}
	webTestLaptopSelect(t, m, "confirm")
	if s := cfg(); !strings.Contains(s, `"id": "app2"`) || strings.Contains(s[strings.Index(s, `"id": "app2"`):], "password") {
		t.Errorf("config after the yes:\n%s", s)
	}
	if _, ok := m.databases.connection("app2"); !ok {
		t.Error("the laptop's list has no app2")
	}
	// A new sqlite file in the workspace still asks (it need not exist yet).
	webDBAsk(t, m, c, webproto.OpDBAdd, `{"id":"lite","driver":"sqlite","path":"new.db"}`, "new.db")
	webTestLaptopSelect(t, m, "cancel")

	webDBAsk(t, m, c, webproto.OpDBSSH, `{"conn":"local","host":"bastion","user":"me","auth":"agent"}`, "bastion", "me")
	webTestLaptopSelect(t, m, "confirm")
	if s := cfg(); !strings.Contains(s, `"host": "bastion"`) {
		t.Errorf("no tunnel saved:\n%s", s)
	}
	webDBAsk(t, m, c, webproto.OpDBSSL, `{"conn":"local","mode":"verify-full","ca":"certs/ca.pem"}`, "verify-full", "certs/ca.pem")
	webTestLaptopSelect(t, m, "confirm")
	if s := cfg(); !strings.Contains(s, `"mode": "verify-full"`) {
		t.Errorf("no SSL saved:\n%s", s)
	}
	webDBAsk(t, m, c, webproto.OpDBSSH, `{"conn":"local","remove":true}`, "Remove")
	webTestLaptopSelect(t, m, "confirm")
	if s := cfg(); strings.Contains(s, "bastion") {
		t.Errorf("the tunnel is still there:\n%s", s)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBSSL, `{"conn":"ro","mode":"disable"}`); webTestErr(f) != "" {
		t.Errorf("ssl of a read-only connection is a setting, not a write: %v", f)
	}
	webTestLaptopSelect(t, m, "cancel")

	if f := webTestCallBg(t, m, c, webproto.OpDBDisconnect, `{"conn":"prod"}`); webTestErr(f) != "bad_request" {
		t.Errorf("disconnect a closed one: %v", f)
	}
	if f := webTestCallBg(t, m, c, webproto.OpDBTx, `{"conn":"local","verb":"commit"}`); webTestErr(f) != "bad_request" {
		t.Errorf("commit with nothing open: %v", f)
	}
	n := len(*opened)
	var done webproto.DBDone
	webTestData(t, webTestCallBg(t, m, c, webproto.OpDBConnect, `{"conn":"prod"}`), &done)
	if done.Text == "" || len(*opened) == n {
		t.Errorf("connect %+v opened %d", done, len(*opened)-n)
	}
	user := &webTxSession{aiDBSession: &aiDBSession{name: "user-local"}}
	m.databases.sessions["local"] = user
	m.databases.tx("local").open = true
	m.databases.tx("local").writes = 3
	webDBAsk(t, m, c, webproto.OpDBTx, `{"conn":"local","verb":"commit"}`, "Commit", "3")
	webTestLaptopSelect(t, m, "confirm")
	webDBAsk(t, m, c, webproto.OpDBTx, `{"conn":"local","verb":"rollback"}`, "Roll back")
	webTestLaptopSelect(t, m, "cancel")
	user.mu.Lock()
	ends := strings.Join(user.ends, ",")
	user.mu.Unlock()
	if ends != "commit" {
		t.Errorf("the laptop session ended %q (the rollback was cancelled)", ends)
	}
	webDBAsk(t, m, c, webproto.OpDBTx, `{"conn":"prod","verb":"manual"}`, "manual")
	webTestLaptopSelect(t, m, "confirm")
	if !m.databases.manualMode("prod") {
		t.Error("prod is not in manual mode")
	}
	webDBAsk(t, m, c, webproto.OpDBDisconnect, `{"conn":"local"}`, "Disconnect", "3")
	webTestLaptopSelect(t, m, "confirm")
	if m.databases.sessions["local"] != nil {
		t.Error("still connected after the yes")
	}
}

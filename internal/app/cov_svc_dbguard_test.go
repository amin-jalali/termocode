package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Before the Databases view ever opened (no state) the database hooks
// do nothing and never panic.
func TestCovSvcDBNoStateIsQuiet(t *testing.T) {
	m := &Model{}
	if m.databaseRefresh() != nil || m.databaseStartAdd("postgres") != nil || m.databaseRetestPending() != nil ||
		m.onDBIntrospect(dbIntrospectMsg{}) != nil || m.onDBPrompt(promptKindDBHost, "x") != nil ||
		m.onDBTest(dbTestMsg{}) != nil || m.databaseSavePending(true) != nil || m.databaseDisconnectNow("x") != nil {
		t.Error("a hook did something without state")
	}
	m.databaseConfirmDisconnect("x")
	for _, kind := range []confirmKindEnum{confirmKindDBMovePassword, confirmKindDBDisconnect} {
		m.confirmKind = kind
		if cmd, ok := m.handleDBConfirm("move"); !ok || cmd != nil {
			t.Errorf("kind %d: %v %v", kind, ok, cmd)
		}
	}
	m.confirmKind = 0
	if _, ok := m.handleDBConfirm("x"); ok {
		t.Error("a foreign card was taken")
	}
	// A key with no state makes it.
	next, _ := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if next.databases == nil {
		t.Error("no state after a key")
	}
}

// Answers for an old tree or a disconnected connection are dropped; a
// schema error says so on the node.
func TestCovSvcDBStaleAnswers(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	s.generation = 4
	if m.onDBIntrospect(dbIntrospectMsg{path: "app", generation: 3}) != nil {
		t.Error("old generation used")
	}
	s.connectionEpochs["app"] = 2
	if m.onDBIntrospect(dbIntrospectMsg{path: "app", connection: "app", epoch: 1, generation: 4}) != nil {
		t.Error("old epoch used")
	}
	m.onDBIntrospect(dbIntrospectMsg{path: "app", connection: "app", epoch: 2, generation: 4, err: errors.New("no such table: sqlite_master")})
	if !strings.Contains(covSvcToast(m), "Could not load database schema") || s.nodes["app"].problem == "" {
		t.Errorf("toast=%q problem=%q", covSvcToast(m), s.nodes["app"].problem)
	}
	m.onDBIntrospect(dbIntrospectMsg{path: "app", connection: "app", epoch: 2, generation: 4, result: dbclient.IntrospectionResult{
		Databases: []string{"main\x1b[31m"}, Routines: []string{"fn\nx"}, Sequences: []string{"seq"}}})
	if strings.Contains(dbRowNames(s), "\x1b") {
		t.Errorf("escape kept: %q", dbRowNames(s))
	}
	// A test answer for an old tree is dropped too.
	if m.onDBTest(dbTestMsg{generation: 1}) != nil {
		t.Error("old test used")
	}
	// A connected test that worked through a tunnel names it.
	m.onDBTest(dbTestMsg{generation: 4, existing: true, conn: dbclient.Conn{ID: "app", Name: "app"}, via: "bastion.example"})
	if !strings.Contains(covSvcToast(m), "through bastion.example") {
		t.Errorf("via toast = %q", covSvcToast(m))
	}
}

// sqlite3 missing: the connection shows a note and the install command,
// which y copies.
func TestCovSvcDBSQLiteMissingShowsInstall(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	n := s.nodes["app"]
	n.loading = true
	s.nodes["app"] = n
	m.onDBConnect(dbConnectMsg{id: "app", generation: s.generation, err: errors.New("sqlite3 is not installed. apt install sqlite3")})
	if !strings.Contains(dbRowNames(s), "sqlite3 is missing or too old") {
		t.Fatalf("rows = %s", dbRowNames(s))
	}
	var clip []string
	defer SetClipboardSink(func(v string) { clip = append(clip, v) })()
	covSvcDBSelect(t, m, "app")
	covSvcDBKey(m, "y")
	if len(clip) != 1 || !strings.Contains(covSvcToast(m), "install command") {
		t.Errorf("clip=%q toast=%q", clip, covSvcToast(m))
	}
}

// Disconnect drops the connection's transactions and per-database
// sessions, keeps the others.
func TestCovSvcDBDisconnectDropsItsWork(t *testing.T) {
	m, _ := covSvcDB(t, `{"id":"pg","driver":"postgres","host":"h","port":5432,"database":"d"}`)
	s := m.databases
	a, b, c := &covSvcSQL{}, &covSvcSQL{}, &covSvcSQL{}
	s.sessions["pg"], s.sessions["pg/db/other"], s.sessions["app"] = a, b, c
	s.txs = map[string]*dbTxState{"pg": {open: true}, "app": {open: true}}
	s.activeID = "pg"
	m.databaseDisconnectNow("pg")
	if a.closed != 1 || b.closed != 1 || c.closed != 0 || s.txs["pg"] != nil || s.txs["app"] == nil || s.activeID != "" {
		t.Errorf("closed %d %d %d txs %v active %q", a.closed, b.closed, c.closed, s.txs, s.activeID)
	}
}

// Passwords by reference: ${secret:NAME} reads the store, a reference
// that does not resolve is a toast and nothing connects.
func TestCovSvcDBPasswordReferences(t *testing.T) {
	m, _ := covSvcDB(t, `{"id":"sec","driver":"postgres","host":"h","port":5432,"database":"d","password":"${secret:pgpass}"},`+
		`{"id":"env","driver":"postgres","host":"h","port":5432,"database":"d","password":"${env:COVSVC_NOT_SET_EVER}"}`)
	var got []string
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(_ context.Context, _ dbclient.Conn, pw string) (dbclient.Session, error) {
		got = append(got, pw)
		return &covSvcSQL{}, nil
	}
	defer func() { dbSQLSessionFactory = old }()
	if err := databaseSecretStore().Set("pgpass", "from-store"); err != nil {
		t.Fatal(err)
	}
	covSvcRun(t, m, m.databaseConnect("sec"))
	if len(got) != 1 || got[0] != "from-store" {
		t.Errorf("password = %q", got)
	}
	covSvcRun(t, m, m.databaseConnect("env"))
	if !strings.Contains(covSvcToast(m), "Could not resolve database password") || len(got) != 1 {
		t.Errorf("env ref: toast=%q got=%q", covSvcToast(m), got)
	}
	if m.databaseConnect("nope") != nil {
		t.Error("an unknown id connected")
	}
}

// Files that cannot be written: each save says so and keeps the state.
func TestCovSvcDBConfigWriteErrors(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	// .termocode is a file now: the project file cannot be written.
	tc := filepath.Join(s.root, ".termocode")
	if err := os.RemoveAll(tc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tc, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.nodes["suggest-service-pg"] = dbNode{kind: dbNodeSuggestion, name: "pg", driver: "postgres", host: "h", port: 5432, path: "suggest-service-pg"}
	m.databaseAddSuggestion("suggest-service-pg")
	if !strings.Contains(covSvcToast(m), "Cannot save connection") {
		t.Errorf("suggestion toast = %q", covSvcToast(m))
	}
	s.nodes["suggest-sqlite-x"] = dbNode{kind: dbNodeSuggestion, name: "x.db", path: "suggest-sqlite-x"}
	m.databaseAddSuggestion("suggest-sqlite-x")
	if !strings.Contains(covSvcToast(m), "Cannot save connection") {
		t.Errorf("sqlite suggestion toast = %q", covSvcToast(m))
	}
	s.pendingConn = dbclient.Conn{ID: "p", Name: "p", Driver: "postgres", Host: "h", Port: 5432, Database: "d"}
	m.databaseSavePending(true)
	if !strings.Contains(covSvcToast(m), "Cannot save connection") || s.pendingConn.ID != "p" {
		t.Errorf("save toast = %q", covSvcToast(m))
	}
	if err := writeDBConfig(filepath.Join(tc, "databases.json"), nil); err == nil {
		t.Error("write under a file worked")
	}
	m.databaseOpenConfig(filepath.Join(tc, "databases.json"))
	if !strings.Contains(covSvcToast(m), "Cannot open database config") {
		t.Errorf("open config toast = %q", covSvcToast(m))
	}
	// The user config dir is a file: the user save fails too.
	dir, _ := configDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.databaseSavePending(false)
	if !strings.Contains(covSvcToast(m), "Cannot save connection") {
		t.Errorf("user save toast = %q", covSvcToast(m))
	}
}

// Names a new connection gets.
func TestCovSvcDBConnectionIDs(t *testing.T) {
	if got := dbConnectionID(dbclient.Conn{Driver: "sqlite", Path: "/x/My Data.db"}); got != "my-data" {
		t.Errorf("sqlite id = %q", got)
	}
	if got := dbConnectionID(dbclient.Conn{Driver: "postgres", Host: "db.local"}); got != "db-local" {
		t.Errorf("host id = %q", got)
	}
	if got := dbConnectionID(dbclient.Conn{Driver: "mysql", Database: "!!!"}); !strings.HasPrefix(got, "mysql-") {
		t.Errorf("empty id = %q", got)
	}
	if got := dbRedisConnectionID(dbclient.Conn{URL: "redis://cache.local:6379/3"}); got != "redis-cache-local-3" {
		t.Errorf("redis url id = %q", got)
	}
	if dbPasswordFromValue("postgres://u:pw@h/d") != "pw" || dbPasswordFromValue("plain") != "plain" || dbPasswordFromValue("postgres://u@h/d") != "postgres://u@h/d" {
		t.Error("dbPasswordFromValue")
	}
}

// A URL with a SQLite path is made absolute in the workspace; a Redis
// URL gets the Redis name.
func TestCovSvcDBAddFromURLKinds(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.onDBPrompt(promptKindDBAdd, "sqlite:data/other.db")
	if s.pendingConn.Path != "" && !filepath.IsAbs(s.pendingConn.Path) {
		t.Errorf("sqlite path = %q", s.pendingConn.Path)
	}
	m.onDBPrompt(promptKindDBAdd, "redis://cache.local:6379/2")
	if s.pendingConn.ID != "redis-cache-local-2" {
		t.Errorf("redis id = %q", s.pendingConn.ID)
	}
}

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// covSvcSQL is a scripted SQL session: Ping fails with pingErr, the
// schema has the objects, Run answers rows.
type covSvcSQL struct {
	mu      sync.Mutex
	pingErr error
	objects []dbclient.SchemaObject
	closed  int
	ran     []string
}

func (s *covSvcSQL) Ping(context.Context) error { return s.pingErr }
func (s *covSvcSQL) Introspect(_ context.Context, r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	if r.Database == "" && r.Schema == "" {
		return dbclient.IntrospectionResult{Databases: []string{"app"}, Schemas: []string{"public"}, Objects: s.objects}, nil
	}
	return dbclient.IntrospectionResult{Objects: s.objects}, nil
}
func (s *covSvcSQL) Run(_ context.Context, q string, _ dbclient.RunOptions) dbclient.Result {
	s.mu.Lock()
	s.ran = append(s.ran, q)
	s.mu.Unlock()
	return dbclient.Result{Columns: []dbclient.Column{{Name: "n"}}, Rows: [][]dbclient.Cell{{{Text: "1"}}}}
}
func (s *covSvcSQL) Begin(context.Context) error                { return nil }
func (s *covSvcSQL) Commit(context.Context) error               { return nil }
func (s *covSvcSQL) Rollback(context.Context) error             { return nil }
func (s *covSvcSQL) ReadOnlySession() (dbclient.Session, error) { return s, nil }
func (s *covSvcSQL) Cancel()                                    {}
func (s *covSvcSQL) Close() error                               { s.mu.Lock(); s.closed++; s.mu.Unlock(); return nil }

// covSvcDB is the Databases view of a project with a real SQLite file
// (app.db, table users) and, when extra is set, more project
// connections. SQL servers answer through the scripted session.
func covSvcDB(t *testing.T, extra string) (*Model, *covSvcSQL) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	// A sqlite3 child may still write its journal for a moment after a
	// test session closes: remove the project folder with retries.
	root, err := os.MkdirTemp("", "covsvc-db-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(10 * time.Second)
		for os.RemoveAll(root) != nil && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	})
	sess, err := dbclient.NewSQLiteSession(context.Background(), dbclient.Conn{Driver: "sqlite", Path: filepath.Join(root, "app.db")})
	if err != nil {
		t.Skipf("sqlite3: %v", err)
	}
	for _, q := range []string{"CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT)", "INSERT INTO users VALUES (1,'ann'),(2,'bob')"} {
		if r := sess.Run(context.Background(), q, dbclient.RunOptions{}); r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	_ = sess.Close()
	conns := `{"id":"app","driver":"sqlite","path":"app.db"}`
	if extra != "" {
		conns += "," + extra
	}
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(`{"connections":[`+conns+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &covSvcSQL{objects: []dbclient.SchemaObject{{Name: "orders", Kind: "BASE TABLE"}}}
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return fake, nil }
	t.Cleanup(func() { dbSQLSessionFactory = old })
	ws, err := localhost.New(root) // the project file goes here, not into the package folder
	if err != nil {
		t.Fatal(err)
	}
	setProcWorkspace(t, ws)
	st := newDBState()
	st.visible = true
	st.load(root)
	m := &Model{databases: st, w: 120, h: 40, showExp: true, activity: activity.New(), output: newOutputStore()}
	m.activity.SetActive(activity.ViewDatabases)
	t.Cleanup(func() {
		for _, s := range m.databases.sessions {
			_ = s.Close()
		}
	})
	return m, fake
}

func covSvcDBKey(m *Model, s string) tea.Cmd {
	types := map[string]tea.KeyType{"enter": tea.KeyEnter, "esc": tea.KeyEsc, "up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight}
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	if kt, ok := types[s]; ok {
		k = tea.KeyMsg{Type: kt}
	}
	next, cmd := m.handleDatabasesSidebarKey(k)
	*m = next
	return cmd
}

func covSvcDBSelect(t *testing.T, m *Model, name string) dbTreeRow {
	t.Helper()
	for i, r := range m.databases.rows() {
		if r.name == name || r.path == name {
			m.databases.cursor = i
			return r
		}
	}
	t.Fatalf("no row %q in:\n%s", name, dbRowNames(m.databases))
	return dbTreeRow{}
}

func covSvcToast(m *Model) string { return strings.Join(m.toast.View(160, 60), " ") }

// Enter connects the SQLite file and lists its table; R reloads the
// tree; x asks before it disconnects; T tests the saved connection.
func TestCovSvcDBConnectRefreshDisconnect(t *testing.T) {
	m, _ := covSvcDB(t, "")
	covSvcDBSelect(t, m, "app")
	covSvcRun(t, m, covSvcDBKey(m, "enter"))
	if _, ok := m.databases.sessions["app"]; !ok {
		t.Fatalf("not connected: %s %q", dbRowNames(m.databases), covSvcToast(m))
	}
	if !strings.Contains(dbRowNames(m.databases), "users") {
		covSvcDBSelect(t, m, "main")
		covSvcRun(t, m, covSvcDBKey(m, "enter"))
	}
	if !strings.Contains(dbRowNames(m.databases), "users") {
		t.Fatalf("no users table:\n%s", dbRowNames(m.databases))
	}
	// left folds the connection, right opens it again.
	covSvcDBSelect(t, m, "app")
	covSvcDBKey(m, "left")
	if !m.databases.folded["app"] {
		t.Error("left did not fold")
	}
	covSvcDBKey(m, "right")
	if m.databases.folded["app"] {
		t.Error("right did not open")
	}
	covSvcDBSelect(t, m, "app")
	covSvcRun(t, m, covSvcDBKey(m, "T"))
	if !strings.Contains(covSvcToast(m), "Connection works") {
		t.Errorf("test toast = %q", covSvcToast(m))
	}
	covSvcDBSelect(t, m, "app")
	covSvcDBKey(m, "x")
	if !m.confirmOpen || m.confirmKind != confirmKindDBDisconnect {
		t.Fatal("x: no card")
	}
	if cmd, ok := m.handleDBConfirm("cancel"); !ok || cmd != nil || m.databases.sessions["app"] == nil {
		t.Error("cancel disconnected")
	}
	covSvcDBKey(m, "x")
	m.handleDBConfirm("disconnect")
	if _, ok := m.databases.sessions["app"]; ok {
		t.Error("still connected")
	}
	covSvcRun(t, m, covSvcDBKey(m, "R"))
	if m.databases.generation == 0 || !strings.Contains(dbRowNames(m.databases), "app") {
		t.Errorf("refresh: gen=%d rows=%s", m.databases.generation, dbRowNames(m.databases))
	}
}

// A connection that fails says why in the tree and a toast; a missing
// SQLite file is the same.
func TestCovSvcDBConnectFailure(t *testing.T) {
	m, fake := covSvcDB(t, `{"id":"pg","driver":"postgres","host":"db.example","port":5432,"user":"app","database":"app"}`)
	fake.pingErr = errors.New("password authentication failed for user \"app\"")
	covSvcDBSelect(t, m, "pg")
	covSvcRun(t, m, covSvcDBKey(m, "enter"))
	if !strings.Contains(covSvcToast(m), "Database connection failed") || m.databases.nodes["pg"].problem == "" {
		t.Errorf("toast=%q problem=%q", covSvcToast(m), m.databases.nodes["pg"].problem)
	}
	covSvcDBSelect(t, m, "pg")
	covSvcRun(t, m, covSvcDBKey(m, "T"))
	if !strings.Contains(covSvcToast(m), "Connection test failed") {
		t.Errorf("test toast = %q", covSvcToast(m))
	}
	// The scripted server now answers: Enter connects and lists orders.
	fake.pingErr = nil
	covSvcDBSelect(t, m, "pg")
	covSvcRun(t, m, covSvcDBKey(m, "enter"))
	if m.databases.sessions["pg"] == nil || m.databases.nodes["pg"].problem != "" {
		t.Errorf("pg not connected: %q", m.databases.nodes["pg"].problem)
	}
}

// Keys that open cards, pickers and prompts on the tree.
func TestCovSvcDBTreeKeys(t *testing.T) {
	m, _ := covSvcDB(t, `{"id":"pg","driver":"postgres","host":"db.example","port":5432,"user":"app","database":"app"}`)
	covSvcDBKey(m, "?")
	if !m.confirmOpen || !strings.Contains(cardText(m), "Database keys") {
		t.Errorf("? card = %s", cardText(m))
	}
	m.confirmOpen = false
	covSvcDBKey(m, "a")
	if !m.pickerOpen || m.pickerKind != pickerKindDBType {
		t.Error("a: no type picker")
	}
	m.pickerOpen = false
	covSvcDBSelect(t, m, "pg")
	covSvcDBKey(m, "p")
	if !m.promptOpen || m.promptKind != promptKindDBPassword || m.databases.pendingSecretID != "pg" {
		t.Error("p: no password prompt")
	}
	m.promptOpen = false
	covSvcDBSelect(t, m, "pg")
	covSvcDBKey(m, "t")
	if !m.pickerOpen {
		t.Error("t: no transaction picker")
	}
	m.pickerOpen = false
	// e on a project connection opens .termocode/databases.json; m opens
	// the user file (created from the template).
	_, statErr := os.Stat(filepath.Join(".termocode", "databases.json"))
	covSvcDBSelect(t, m, "pg")
	covSvcDBKey(m, "e")
	if _, err := os.Stat(filepath.Join(".termocode", "databases.json")); err == nil && statErr != nil {
		os.RemoveAll(".termocode")
		t.Error("e wrote .termocode/databases.json into the package folder, not the test workspace")
	}
	covSvcDBKey(m, "m")
	dir, _ := configDir()
	if b, err := os.ReadFile(filepath.Join(dir, "db", "connections.json")); err != nil || !strings.Contains(string(b), "connections") {
		t.Errorf("user config: %v %q", err, b)
	}
	covSvcDBKey(m, "j")
	covSvcDBKey(m, "k")
	covSvcDBKey(m, "G")
	covSvcDBKey(m, "g")
	if m.databases.cursor != 0 {
		t.Errorf("g: cursor %d", m.databases.cursor)
	}
	m.focus = FocusExplorer
	covSvcDBKey(m, "esc")
	if m.focus != FocusEditor {
		t.Error("esc kept the focus")
	}
}

// A user connection with a password in its file asks to move it to the
// secret store; Move saves it there, rewrites the file without it and
// connects.
func TestCovSvcDBMovePasswordToStore(t *testing.T) {
	m, fake := covSvcDB(t, "")
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(dir, "db", "connections.json")
	if err := writeDBConfig(userFile, []dbclient.Conn{{ID: "mine", Driver: "postgres", Host: "h", Port: 5432, User: "u", Database: "d", Password: "hunter22"}}); err != nil {
		t.Fatal(err)
	}
	covSvcRun(t, m, m.databaseRefresh())
	covSvcDBSelect(t, m, "mine")
	covSvcDBKey(m, "enter")
	if !m.confirmOpen || m.confirmKind != confirmKindDBMovePassword {
		t.Fatalf("no move card: %q", covSvcToast(m))
	}
	if cmd, _ := m.handleDBConfirm("cancel"); cmd != nil || m.databases.pendingSecretID != "" {
		t.Error("cancel moved")
	}
	covSvcDBSelect(t, m, "mine")
	covSvcDBKey(m, "enter")
	covSvcRun(t, m, func() tea.Cmd { c, _ := m.handleDBConfirm("move"); return c }())
	b, _ := os.ReadFile(userFile)
	if strings.Contains(string(b), "hunter22") {
		t.Errorf("password still in the file: %s", b)
	}
	if v, ok, _ := databaseSecretStore().Get("db.mine"); !ok || v != "hunter22" {
		t.Errorf("store = %q %v", v, ok)
	}
	if m.databases.sessions["mine"] != fake {
		t.Error("not connected after the move")
	}
	if p, _ := databaseLiteralPassword(dbclient.Conn{URL: "postgres://u:pw@h/d"}); p != "pw" {
		t.Errorf("url password = %q", p)
	}
	if c := databaseRedactLiteralPassword(dbclient.Conn{URL: "postgres://u:pw@h/d", Password: "x"}); strings.Contains(c.URL, "pw") || c.Password != "" {
		t.Errorf("redacted = %+v", c)
	}
}

// p on a connection saves the typed password in the store and connects.
func TestCovSvcDBSetPassword(t *testing.T) {
	m, fake := covSvcDB(t, `{"id":"pg","driver":"postgres","host":"db.example","port":5432,"user":"app","database":"app"}`)
	covSvcDBSelect(t, m, "pg")
	covSvcDBKey(m, "p")
	m.promptOpen = false
	covSvcRun(t, m, m.onDBPrompt(promptKindDBPassword, "s3cret-pass"))
	if v, ok, _ := databaseSecretStore().Get("db.pg"); !ok || v != "s3cret-pass" {
		t.Errorf("store = %q %v", v, ok)
	}
	if m.databases.sessions["pg"] != fake {
		t.Error("not connected")
	}
	if m.onDBPrompt(promptKindDBPassword, "x") != nil {
		t.Error("a password with no target did something")
	}
}

// Adding connections step by step: a bad port or an empty name is
// refused with a note; a good SQLite path is tested and saved.
func TestCovSvcDBAddSteps(t *testing.T) {
	m, fake := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("postgres")
	if m.promptKind != promptKindDBHost {
		t.Fatal("no host prompt")
	}
	m.onDBPrompt(promptKindDBHost, " ")
	if !strings.Contains(covSvcToast(m), "Server address is needed") {
		t.Errorf("host toast = %q", covSvcToast(m))
	}
	m.onDBPrompt(promptKindDBHost, "db.local")
	m.onDBPrompt(promptKindDBPort, "99999")
	if !strings.Contains(covSvcToast(m), "That port is not valid") {
		t.Errorf("port toast = %q", covSvcToast(m))
	}
	m.onDBPrompt(promptKindDBPort, "5433")
	m.onDBPrompt(promptKindDBUser, "app")
	m.onDBPrompt(promptKindDBName, " ")
	if !strings.Contains(covSvcToast(m), "Database name is needed") {
		t.Errorf("name toast = %q", covSvcToast(m))
	}
	m.onDBPrompt(promptKindDBName, "Shop DB")
	if s.pendingConn.ID != "shop-db" || m.promptKind != promptKindDBPassword {
		t.Fatalf("pending = %+v kind=%d", s.pendingConn, m.promptKind)
	}
	// A wrong ${…} reference keeps the prompt with an error.
	m.onDBPrompt(promptKindDBPassword, "${env:NOPE_NOT_SET}")
	if !m.promptOpen {
		t.Error("bad reference closed the prompt")
	}
	covSvcRun(t, m, m.onDBPrompt(promptKindDBPassword, "pw"))
	if !m.pickerOpen || m.pickerKind != pickerKindDBSave {
		t.Fatalf("no save picker: %q", covSvcToast(m))
	}
	m.pickerOpen = false
	if cmd := m.databaseSavePending(false); cmd != nil {
		t.Fatalf("save: %q", covSvcToast(m))
	}
	if _, ok := s.connection("shop-db"); !ok {
		t.Error("shop-db not added")
	}
	if v, ok, _ := databaseSecretStore().Get("db.shop-db"); !ok || v != "pw" {
		t.Errorf("stored pw = %q %v", v, ok)
	}
	// Redis steps: index must be a number.
	m.databaseStartAdd("redis")
	m.onDBPrompt(promptKindDBHost, "cache.local")
	m.onDBPrompt(promptKindDBPort, "6380")
	m.onDBPrompt(promptKindDBUser, "")
	m.onDBPrompt(promptKindDBName, "-1")
	if !strings.Contains(covSvcToast(m), "That database index is not valid") {
		t.Errorf("index toast = %q", covSvcToast(m))
	}
	m.onDBPrompt(promptKindDBName, "2")
	if s.pendingConn.ID != "redis-cache-local-2" {
		t.Errorf("redis id = %q", s.pendingConn.ID)
	}
	// MySQL and a URL.
	m.databaseStartAdd("mysql")
	if s.pendingConn.Port != 3306 {
		t.Errorf("mysql port %d", s.pendingConn.Port)
	}
	m.databaseStartAdd("url")
	m.onDBPrompt(promptKindDBAdd, "::not a url")
	if !strings.Contains(covSvcToast(m), "could not be read") {
		t.Errorf("url toast = %q", covSvcToast(m))
	}
	m.onDBPrompt(promptKindDBAdd, "postgres://u@h:5432/shop-db")
	if !strings.Contains(covSvcToast(m), "Connection already exists") {
		t.Errorf("dup toast = %q", covSvcToast(m))
	}
	m.onDBPrompt(promptKindDBAdd, "postgres://u@h:5432/other")
	if m.promptKind != promptKindDBPassword {
		t.Error("url without password: no password prompt")
	}
	covSvcRun(t, m, m.onDBPrompt(promptKindDBAdd, "postgres://u:pw@h:5432/third"))
	// SQLite: an empty path is refused; a file is tested and saved to
	// the project.
	m.databaseStartAdd("sqlite")
	m.onDBPrompt(promptKindDBSQLitePath, "")
	if !strings.Contains(covSvcToast(m), "Database file is needed") {
		t.Errorf("path toast = %q", covSvcToast(m))
	}
	covSvcRun(t, m, m.onDBPrompt(promptKindDBSQLitePath, "app.db"))
	if !m.pickerOpen || m.pickerKind != pickerKindDBSave {
		t.Fatalf("sqlite: no save picker: %q", covSvcToast(m))
	}
	// app already exists by id: save under the project file anyway
	s.pendingConn.ID, s.pendingConn.Name = "app2", "app2"
	if cmd := m.databaseSavePending(true); cmd != nil {
		t.Fatalf("project save: %q", covSvcToast(m))
	}
	b, _ := os.ReadFile(filepath.Join(s.root, ".termocode", "databases.json"))
	if !strings.Contains(string(b), `"app2"`) {
		t.Errorf("project file = %s", b)
	}
	// A failing test offers the next steps.
	fake.pingErr = errors.New("connection refused")
	s.pendingConn = dbclient.Conn{ID: "down", Name: "down", Driver: "postgres", Host: "h", Port: 5432, Database: "d", Source: dbclient.SourceUser}
	covSvcRun(t, m, m.databaseRetestPending())
	if !m.pickerOpen || m.pickerKind != pickerKindDBTestResult {
		t.Errorf("failed test: picker=%v kind=%d toast=%q", m.pickerOpen, m.pickerKind, covSvcToast(m))
	}
	if m.databaseSavePending(true) != nil && m.databases.pendingConn.ID == "" {
		t.Error("save without a pending connection")
	}
}

// Suggestions from .env, compose services and SQLite files become
// connections; a duplicate or an unreadable value is a note.
func TestCovSvcDBSuggestions(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	if err := os.WriteFile(filepath.Join(s.root, ".env"), []byte("export DATABASE_URL=\"postgres://app:pw@localhost:5432/app\"\nBROKEN_URL=postgres://\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, ok := databaseEnvValue(filepath.Join(s.root, ".env"), "DATABASE_URL"); !ok || !strings.HasPrefix(v, "postgres://") {
		t.Errorf("env value = %q", v)
	}
	if _, ok := databaseEnvValue(filepath.Join(s.root, ".env"), "MISSING"); ok {
		t.Error("missing key found")
	}
	if _, ok := databaseEnvValue(filepath.Join(s.root, "nope"), "X"); ok {
		t.Error("missing file read")
	}
	s.nodes["suggest-env-database_url"] = dbNode{kind: dbNodeSuggestion, name: "DATABASE_URL", path: "suggest-env-database_url"}
	covSvcRun(t, m, m.databaseAddSuggestion("suggest-env-database_url"))
	if s.pendingConn.ID != "database-url" || !strings.HasPrefix(s.pendingConn.Password, "${dotenv:") {
		t.Errorf("env pending = %+v %q", s.pendingConn, s.pendingPassword)
	}
	s.nodes["suggest-env-missing"] = dbNode{kind: dbNodeSuggestion, name: "MISSING", path: "suggest-env-missing"}
	m.databaseAddSuggestion("suggest-env-missing")
	if !strings.Contains(covSvcToast(m), "Connection suggestion unavailable") {
		t.Errorf("missing toast = %q", covSvcToast(m))
	}
	s.nodes["suggest-env-broken"] = dbNode{kind: dbNodeSuggestion, name: "BROKEN_URL", path: "suggest-env-broken"}
	m.databaseAddSuggestion("suggest-env-broken")
	if !strings.Contains(covSvcToast(m), "Invalid connection suggestion") {
		t.Errorf("broken toast = %q", covSvcToast(m))
	}
	// A compose service suggestion is saved to the project file.
	s.nodes["suggest-service-postgres"] = dbNode{kind: dbNodeSuggestion, name: "postgres", driver: "postgres", host: "localhost", port: 5432, path: "suggest-service-postgres"}
	covSvcDBSelect(t, m, "app")
	if cmd := m.databaseAddSuggestion("suggest-service-postgres"); cmd != nil {
		t.Fatalf("service suggestion: %q", covSvcToast(m))
	}
	if _, ok := s.connection("postgres"); !ok || !s.hidden["suggest-service-postgres"] {
		t.Error("service suggestion not added")
	}
	// The same name again gets "-db".
	m.databaseAddSuggestion("suggest-service-postgres")
	if _, ok := s.connection("postgres-db"); !ok {
		t.Error("second service suggestion not renamed")
	}
	s.nodes["suggest-local-mysql"] = dbNode{kind: dbNodeSuggestion, name: "MySQL on this computer", driver: "mysql", host: "127.0.0.1", port: 3306, path: "suggest-local-mysql"}
	m.databaseAddSuggestion("suggest-local-mysql")
	if c, ok := s.connection("local-mysql"); !ok || c.User != "root" {
		t.Errorf("local mysql = %+v", c)
	}
	// A SQLite file suggestion.
	s.nodes["suggest-sqlite-data"] = dbNode{kind: dbNodeSuggestion, name: "data/My Shop.db", path: "suggest-sqlite-data"}
	m.databaseAddSuggestion("suggest-sqlite-data")
	if c, ok := s.connection("my-shop"); !ok || c.Path != "data/My Shop.db" {
		t.Errorf("sqlite suggestion = %+v", c)
	}
	// x hides a suggestion.
	s.nodes["suggest-env-database_url"] = dbNode{kind: dbNodeSuggestion, name: "DATABASE_URL", path: "suggest-env-database_url"}
}

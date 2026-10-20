package app

import (
	"context"
	"errors"
	"github.com/amin-jalali/termocode/internal/activity"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDatabaseTreeRowsFoldAndFilter(t *testing.T) {
	st := newDBState()
	st.conns = []dbclient.Conn{{ID: "app", Driver: "postgres", Host: "localhost", Port: 5432}}
	st.nodes["app"] = dbNode{kind: dbNodeConnection, name: "app", loaded: true, children: []string{"app/db"}}
	st.nodes["app/db"] = dbNode{kind: dbNodeDatabase, name: "main", children: []string{"app/db/public"}}
	st.nodes["app/db/public"] = dbNode{kind: dbNodeSchema, name: "public", loaded: true, children: []string{"app/db/public/Tables", "app/db/public/Views"}}
	st.nodes["app/db/public/Tables"] = dbNode{kind: dbNodeGroup, name: "Tables", loaded: true, children: []string{"app/db/public/Tables/users"}}
	st.nodes["app/db/public/Tables/users"] = dbNode{kind: dbNodeTable, name: "users"}
	st.nodes["app/db/public/Views"] = dbNode{kind: dbNodeGroup, name: "Views", loaded: true}

	rows := st.rows()
	if len(rows) != 6 || rows[4].name != "users" {
		t.Fatalf("expanded rows = %#v", rows)
	}
	st.folded["app/db/public"] = true
	if got := len(st.rows()); got != 3 {
		t.Fatalf("folded schema has %d rows, want 3", got)
	}
	st.folded = map[string]bool{}
	st.filter = "user"
	rows = st.rows()
	if len(rows) != 1 || rows[0].name != "users" {
		t.Fatalf("filtered rows = %#v", rows)
	}
}

func TestDatabaseTreeScrollKeepsCursorVisibleAndMouseUsesOffset(t *testing.T) {
	s := newDBState()
	for i := 0; i < 12; i++ {
		id := "db-" + itoa(i)
		s.conns = append(s.conns, dbclient.Conn{ID: id, Name: id, Driver: "postgres"})
		s.nodes[id] = dbNode{kind: dbNodeConnection, name: id, path: id, conn: id, loaded: true}
	}
	m := Model{databases: s, w: 40, h: 9, showExp: true, activity: activity.New()}
	s.cursor = 1
	m.renderDatabasesSidebarRaw(40, 9, true)
	if s.scroll != 0 {
		t.Fatalf("cursor moved one row but scroll jumped to %d", s.scroll)
	}
	s.cursor = 8
	m.renderDatabasesSidebarRaw(40, 9, true)
	if s.scroll == 0 || s.cursor < s.scroll || s.cursor >= s.scroll+3 {
		t.Fatalf("cursor %d is outside visible rows starting at %d", s.cursor, s.scroll)
	}
	_, _ = m.handleDatabasesSidebarMouse(1, 5, tea.MouseLeft)
	if want := s.scroll + 1; s.cursor != want {
		t.Fatalf("mouse click selected row %d, want visible row %d", s.cursor, want)
	}
	before := s.cursor
	_, _ = m.handleDatabasesSidebarMouse(1, 5, tea.MouseWheelDown)
	if s.cursor != before+1 {
		t.Fatalf("mouse wheel left cursor at %d, want %d", s.cursor, before+1)
	}
}

func TestDatabaseSidebarShowsOnlySupportedActionsAndPrimaryKey(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "local", Name: "local", Driver: "postgres"}}
	s.nodes["local"] = dbNode{kind: dbNodeConnection, name: "local", path: "local", conn: "local", loaded: true, children: []string{"table"}}
	s.nodes["table"] = dbNode{kind: dbNodeTable, name: "users", path: "table", conn: "local", loaded: true, children: []string{"id"}}
	s.nodes["id"] = dbNode{kind: dbNodeColumn, name: "id  integer", path: "id", detail: "primary key"}
	s.cursor = 2
	m := Model{databases: s, w: 80, h: 12}
	got := stripSGRForTest(m.renderDatabasesSidebarRaw(80, 12, false))
	if strings.Contains(got, "d describe") || strings.Contains(got, "y copy name") {
		t.Fatalf("footer advertises unsupported actions:\n%s", got)
	}
	if !strings.Contains(got, "primary key") {
		t.Fatalf("column primary key detail is missing:\n%s", got)
	}
	if strings.Contains(got, "⋯ ⋯") {
		t.Fatalf("header contains a duplicated more glyph:\n%s", got)
	}
}

func TestDatabaseSidebarActionLineFitsAtWidthThresholds(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "local"}}
	s.nodes["local"] = dbNode{kind: dbNodeConnection, name: "local", path: "local", conn: "local"}
	m := Model{databases: s}
	for _, tc := range []struct {
		width int
		want  string
	}{{30, "a add  R refresh"}, {31, "a add   R refresh   / filter"}, {18, "a add  R refresh"}, {17, "a add"}} {
		lines := strings.Split(stripSGRForTest(m.renderDatabasesSidebarRaw(tc.width, 8, false)), "\n")
		if !strings.Contains(lines[2], tc.want) {
			t.Errorf("width %d action line %q does not contain full %q", tc.width, lines[2], tc.want)
		}
	}
}

func TestDatabaseEnvConnectionKeepsSecretOutOfConfig(t *testing.T) {
	root := t.TempDir()
	c, password, err := databaseEnvConnection(root, "DATABASE_URL", "postgres://app:secret@localhost:5432/sample")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "database-url" || c.Driver != "postgres" || c.Database != "sample" || c.Source != dbclient.SourceProject {
		t.Fatalf("connection = %#v", c)
	}
	if password != "secret" || c.Password != "${dotenv:DATABASE_URL}" || strings.Contains(c.URL, "secret") {
		t.Fatalf("password leaked into config: connection=%#v password=%q", c, password)
	}

	c, password, err = databaseEnvConnection(root, "MYSQL_URL", "mysql://app@localhost/sample")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "mysql-url" || c.Driver != "mysql" || c.Password != "" || password != "" {
		t.Fatalf("connection=%#v password=%q", c, password)
	}
}

func TestDatabaseDotenvReferenceResolvesWithoutSecretStore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("DB_PASS=from-dotenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := dbclient.Conn{ID: "local", Driver: "postgres", Host: "localhost", Port: 5432, Password: "${dotenv:DB_PASS}", Source: dbclient.SourceProject}
	s := newDBState()
	s.root, s.conns = root, []dbclient.Conn{c}
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.ID, path: c.ID, conn: c.ID}
	oldFactory := dbSQLSessionFactory
	var got string
	dbSQLSessionFactory = func(_ context.Context, _ dbclient.Conn, password string) (dbclient.Session, error) {
		got = password
		return nil, errors.New("stop after resolution")
	}
	defer func() { dbSQLSessionFactory = oldFactory }()
	m := Model{databases: s, activity: activity.New(), showExp: true}
	m.activity.SetActive(activity.ViewDatabases)
	cmd := m.databaseConnect(c.ID)
	if cmd == nil {
		t.Fatal("database connection command was not created")
	}
	_ = cmd()
	if got != "from-dotenv" {
		t.Fatalf("resolved password = %q, want .env value", got)
	}
}

func TestDatabaseSavePreservesDotenvReference(t *testing.T) {
	root := t.TempDir()
	c := dbclient.Conn{ID: "local", Name: "local", Driver: "postgres", Password: "${dotenv:DB_PASS}"}
	s := newDBState()
	s.root, s.pendingConn, s.pendingPassword = root, c, "resolved-value"
	m := Model{databases: s}
	_ = m.databaseSavePending(true)
	b, err := os.ReadFile(filepath.Join(root, ".termocode", "databases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "${dotenv:DB_PASS}") || strings.Contains(string(b), "resolved-value") {
		t.Fatalf("saved config did not preserve the dotenv reference: %s", b)
	}
}

func TestDatabaseTreeSanitizesServerAndFileNames(t *testing.T) {
	const hostile = "orders\x1b[31m\nDROP TABLE users"
	s := newDBState()
	s.loaded = true
	s.conns = []dbclient.Conn{{ID: "bad", Name: hostile, Driver: "postgres"}}
	s.nodes["bad"] = dbNode{kind: dbNodeConnection, name: hostile, path: "bad", conn: "bad", detail: "host\x1b[31m\nsecond line", loaded: true}
	m := Model{databases: s, w: 60, h: 12, focus: FocusExplorer}
	got := stripSGRForTest(m.renderDatabasesSidebarRaw(60, 12, true))
	if strings.Contains(got, "\x1b[31m") || strings.Contains(got, "DROP TABLE") || strings.Contains(got, "second line") {
		t.Fatalf("untrusted database text reached the tree:\n%s", got)
	}
	if !strings.Contains(got, "orders") {
		t.Fatalf("normal part of the row was lost:\n%s", got)
	}
	clean := sanitizeDBIntrospection(dbclient.IntrospectionResult{
		Objects: []dbclient.SchemaObject{{Name: hostile, Kind: "BASE TABLE"}},
		Columns: []dbclient.Column{{Name: hostile, Type: "TEXT"}},
	})
	if clean.Objects[0].Name != "orders" || clean.Columns[0].Name != "orders" {
		t.Fatalf("server supplied object names were not sanitized: %+v", clean)
	}
}

type closeCountSession struct {
	dbclient.Session
	closed int
	done   chan struct{}
}

type introspectCaptureSession struct {
	dbclient.Session
	req dbclient.IntrospectionRequest
}

func (s *introspectCaptureSession) Introspect(_ context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	s.req = req
	return dbclient.IntrospectionResult{}, nil
}

func (s *closeCountSession) Close() error {
	s.closed++
	if s.done != nil {
		close(s.done)
	}
	return nil
}

func TestDatabaseSessionClosesWhenViewSwitches(t *testing.T) {
	done, exited := make(chan struct{}), make(chan struct{})
	sess := &closeCountSession{done: done}
	go func() {
		<-done
		close(exited)
	}()
	db := newDBState()
	db.visible = true
	db.sessions["local"] = sess
	m := Model{databases: db, activity: activity.New(), showExp: true}
	m.activity.SetActive(activity.ViewDatabases)
	updated, _ := m.Update(activity.SwitchMsg{View: activity.ViewFiles})
	got := updated.(Model)
	if sess.closed != 1 || len(got.databases.sessions) != 0 || got.databases.visible {
		t.Fatalf("hidden database view retained its session: closes=%d sessions=%d visible=%t", sess.closed, len(got.databases.sessions), got.databases.visible)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("database worker remained after the view was hidden")
	}
}

func TestDatabaseDisconnectAndConfigureUseUnclassifiedConfirm(t *testing.T) {
	for _, id := range []string{"db-disconnect", "db-configure"} {
		p, ok := aiActionPolicies[id]
		if !ok || p.Gate != aiGateConfirm || p.Class != "" {
			t.Errorf("%s policy = %+v, want confirm without class", id, p)
		}
	}
}

func TestFailedDatabaseTestKeepsSetPasswordActionAvailable(t *testing.T) {
	s := newDBState()
	s.root, s.visible = t.TempDir(), true
	s.pendingConn = dbclient.Conn{ID: "local", Driver: "postgres", Host: "localhost", Port: 5432}
	m := Model{databases: s, activity: activity.New(), showExp: true}
	m.activity.SetActive(activity.ViewDatabases)
	cmd := m.onDBTest(dbTestMsg{conn: s.pendingConn, generation: s.generation, err: errors.New("password authentication failed")})
	if cmd == nil || !m.pickerOpen || m.pickerKind != pickerKindDBTestResult || m.databases.pendingConn.ID != "local" {
		t.Fatalf("failed test did not keep its pending connection and options: picker=%t kind=%d conn=%#v", m.pickerOpen, m.pickerKind, m.databases.pendingConn)
	}
	if _, ok := m.handleHTTPPickerSelect("db-test-password"); !ok || m.promptKind != promptKindDBPassword {
		t.Fatal("Set password action did not open the masked password prompt")
	}
}

func TestDatabaseTestFailureCommandKeepsSetPasswordAvailable(t *testing.T) {
	oldFactory := dbSQLSessionFactory
	dbSQLSessionFactory = func(context.Context, dbclient.Conn, string) (dbclient.Session, error) {
		var failed *dbclient.SQLSession
		return failed, errors.New("password authentication failed")
	}
	defer func() { dbSQLSessionFactory = oldFactory }()

	s := newDBState()
	s.root, s.visible = t.TempDir(), true
	s.pendingConn = dbclient.Conn{ID: "local", Driver: "postgres", Host: "localhost", Port: 5432}
	m := Model{databases: s, activity: activity.New(), showExp: true}
	m.activity.SetActive(activity.ViewDatabases)
	msg := dbTestCommand(s.pendingConn, "wrong", s.root, s.generation)().(dbTestMsg)
	cmd := m.onDBTest(msg)
	if cmd == nil {
		t.Fatal("failed test did not show its result picker")
	}
	if _, ok := m.handleHTTPPickerSelect("db-test-password"); !ok || m.promptKind != promptKindDBPassword {
		t.Fatal("Set password step did not open the masked password prompt")
	}
}

func TestDatabaseIntrospectionReplacesChildrenOnReconnect(t *testing.T) {
	s := newDBState()
	s.nodes["db"] = dbNode{kind: dbNodeConnection, path: "db", conn: "db"}
	result := dbclient.IntrospectionResult{Databases: []string{"app"}}
	s.addIntrospection("db", result)
	s.addIntrospection("db", result)
	if got := s.nodes["db"].children; len(got) != 1 || got[0] != "db/db/app" {
		t.Fatalf("connection children after reconnect = %v", got)
	}
}

func TestDatabaseRightClickActionsApplyReturnedModel(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want func(Model) bool
	}{{"disconnect", func(m Model) bool { return m.confirmOpen && m.confirmKind == confirmKindDBDisconnect }},
		{"password", func(m Model) bool { return m.promptOpen && m.promptKind == promptKindDBPassword }}} {
		s := newDBState()
		s.conns = []dbclient.Conn{{ID: "local", Name: "local", Driver: "postgres"}}
		s.nodes["local"] = dbNode{kind: dbNodeConnection, name: "local", path: "local", conn: "local"}
		s.menuRow = dbTreeRow{kind: dbNodeConnection, path: "local", conn: "local"}
		m := Model{databases: s, activity: activity.New(), showExp: true}
		m.activity.SetActive(activity.ViewDatabases)
		_ = m.handleDatabasesMenuSelect(tc.id)
		if !tc.want(m) {
			t.Errorf("menu action %q did not update the model", tc.id)
		}
	}
}

func TestDatabaseDemoTreeMatchesBuiltTree(t *testing.T) {
	s := newDBState()
	conn := "local-pg"
	dbPath := conn + "/db/app"
	s.nodes[conn] = dbNode{kind: dbNodeConnection, name: "local-pg", path: conn, conn: conn, detail: "postgres  localhost:5432"}
	s.addIntrospection(conn, dbclient.IntrospectionResult{Databases: []string{"app"}})
	s.addIntrospection(dbPath, dbclient.IntrospectionResult{Schemas: []string{"public"}})
	schemaPath := dbPath + "/schema/public"
	s.addIntrospection(schemaPath, dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}}})
	s.addIntrospection(schemaPath+"/Tables/users", dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "bigint", PK: true}, {Name: "email", Type: "text"}, {Name: "plan", Type: "text"}}})
	if _, ok := s.nodes[schemaPath+"/Tables/users"]; !ok {
		t.Fatalf("Postgres tree did not reach table: %#v", s.nodes)
	}
	demo := newDBState()
	m := Model{databases: demo, activity: activity.New()}
	m.databasesDemo("")
	for path, want := range demo.nodes {
		got, ok := s.nodes[path]
		if !ok || got.kind != want.kind || got.name != want.name || got.detail != want.detail || strings.Join(got.children, "\n") != strings.Join(want.children, "\n") {
			t.Errorf("real tree node %q = %+v (exists=%t), dbdemo = %+v", path, got, ok, want)
		}
	}

	mysql := newDBState()
	mysql.nodes["mysql"] = dbNode{kind: dbNodeConnection, path: "mysql", conn: "mysql"}
	mysql.addIntrospection("mysql", dbclient.IntrospectionResult{Databases: []string{"app"}})
	mysql.addIntrospection("mysql/db/app", dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}}})
	if _, ok := mysql.nodes["mysql/db/app/Tables/users"]; !ok {
		t.Fatalf("MySQL database did not list its tables: %#v", mysql.nodes)
	}
}

func TestExpandingOtherPostgresDatabaseOpensTargetSession(t *testing.T) {
	c := dbclient.Conn{ID: "local", Driver: "postgres", Database: "default"}
	s := newDBState()
	s.visible = true
	s.conns = []dbclient.Conn{c}
	s.passwords[c.ID] = "same-secret"
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.ID, path: c.ID, conn: c.ID, loaded: true}
	dbPath := c.ID + "/db/other"
	s.nodes[dbPath] = dbNode{kind: dbNodeDatabase, name: "other", path: dbPath, conn: c.ID}
	s.sessions[c.ID] = &closeCountSession{}
	oldFactory := dbSQLSessionFactory
	var gotConn dbclient.Conn
	var gotPassword string
	dbSQLSessionFactory = func(_ context.Context, conn dbclient.Conn, password string) (dbclient.Session, error) {
		gotConn, gotPassword = conn, password
		return nil, errors.New("stop after target session creation")
	}
	defer func() { dbSQLSessionFactory = oldFactory }()
	m := Model{databases: s, activity: activity.New(), showExp: true}
	m.activity.SetActive(activity.ViewDatabases)
	cmd := m.databaseExpand(dbPath)
	if cmd == nil {
		t.Fatal("expanding another database did not create a session command")
	}
	msg := cmd().(dbConnectMsg)
	if gotConn.Database != "other" || gotConn.Host != c.Host || gotPassword != "same-secret" || msg.id != dbPath {
		t.Fatalf("target session conn=%+v password=%q msg=%+v", gotConn, gotPassword, msg)
	}
}

func TestMySQLTreeUsesDatabaseNameAsSchema(t *testing.T) {
	s := newDBState()
	s.visible = true
	s.conns = []dbclient.Conn{{ID: "mysql", Driver: "mysql"}}
	path := "mysql/db/app/Tables/users"
	s.nodes[path] = dbNode{kind: dbNodeTable, name: "users", path: path, conn: "mysql"}
	capture := &introspectCaptureSession{}
	s.sessions["mysql"] = capture
	m := Model{databases: s, activity: activity.New(), showExp: true}
	m.activity.SetActive(activity.ViewDatabases)
	cmd := m.databaseExpand(path)
	if cmd == nil {
		t.Fatal("table expansion did not create introspection command")
	}
	_ = cmd()
	if capture.req.Database != "app" || capture.req.Schema != "app" || capture.req.Object != "users" {
		t.Fatalf("MySQL table introspection request = %+v", capture.req)
	}
}

func TestDatabaseHideClosesConnectionAndDatabaseSessions(t *testing.T) {
	base, child := &closeCountSession{}, &closeCountSession{}
	s := newDBState()
	s.visible = true
	s.sessions["local"] = base
	s.sessions["local/db/other"] = child
	m := Model{databases: s, activity: activity.New(), showExp: true}
	m.activity.SetActive(activity.ViewDatabases)
	updated, _ := m.Update(activity.SwitchMsg{View: activity.ViewFiles})
	got := updated.(Model)
	if base.closed != 1 || child.closed != 1 || len(got.databases.sessions) != 0 {
		t.Fatalf("hiding database view retained sessions: base=%d child=%d active=%d", base.closed, child.closed, len(got.databases.sessions))
	}
}

func TestUserConfigLiteralPasswordPromptsBeforeMoving(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, c := range []dbclient.Conn{
		{ID: "local", Name: "local", Driver: "postgres", Host: "localhost", Port: 5432, Password: "not-copied-yet", Source: dbclient.SourceUser},
		{ID: "url", Name: "url", Driver: "postgres", Host: "localhost", Port: 5432, URL: "postgres://app:not-copied-yet@localhost:5432/sample", Source: dbclient.SourceUser},
	} {
		s := newDBState()
		s.root, s.conns, s.userConns = t.TempDir(), []dbclient.Conn{c}, []dbclient.Conn{c}
		s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.Name, path: c.ID, conn: c.ID}
		m := Model{databases: s, activity: activity.New(), showExp: true}
		if cmd := m.databaseConnect(c.ID); cmd != nil {
			t.Fatal("literal password was copied without asking")
		}
		if !m.confirmOpen || m.confirmKind != confirmKindDBMovePassword {
			t.Fatalf("expected a move-to-secret-store card for %q, confirm=%t kind=%d", c.ID, m.confirmOpen, m.confirmKind)
		}
	}
}

func TestDatabaseEnvSQLiteConnectionUsesWorkspacePath(t *testing.T) {
	root := t.TempDir()
	c, password, err := databaseEnvConnection(root, "DATABASE_URL", "sqlite:./data.db")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "sqlite" || c.Path != filepath.Join(root, "data.db") || password != "" {
		t.Fatalf("connection=%#v password=%q", c, password)
	}
}

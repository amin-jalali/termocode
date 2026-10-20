package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

type dbNodeKind int

const (
	dbNodeConnection dbNodeKind = iota
	dbNodeDatabase
	dbNodeSchema
	dbNodeGroup
	dbNodeTable
	dbNodeView
	dbNodeRoutine
	dbNodeSequence
	dbNodeColumn
	dbNodeDetail
	dbNodeSuggestion
	dbNodeSuggestionsHeader
	dbNodeCode
	dbNodeNote
)

type dbNode struct {
	kind     dbNodeKind
	name     string
	path     string
	conn     string
	detail   string
	driver   string
	host     string
	port     int
	children []string
	loaded   bool
	loading  bool
	problem  string
}

type dbTreeRow struct {
	kind                     dbNodeKind
	path, name, conn, detail string
	depth                    int
	selectable               bool
}

type dbState struct {
	console             *dbConsoleState
	loaded              bool
	activeID            string
	generation          uint64
	root                string
	conns               []dbclient.Conn
	userConns           []dbclient.Conn
	projectConns        []dbclient.Conn
	pendingConn         dbclient.Conn
	pendingPassword     string
	addFromURL          bool
	menuRow             dbTreeRow
	pendingDisconnectID string
	sessions            map[string]dbclient.Session
	passwords           map[string]string
	connectionEpochs    map[string]uint64
	nodes               map[string]dbNode
	folded              map[string]bool
	hidden              map[string]bool
	cursor              int
	scroll              int
	pendingSecretID     string
	filter              string
	seq                 uint64
	render              dbRenderCache
	visible             bool
}

type dbRenderCache struct {
	sync.Mutex
	key, out string
}

func newDBState() *dbState {
	return &dbState{sessions: map[string]dbclient.Session{}, passwords: map[string]string{}, connectionEpochs: map[string]uint64{}, nodes: map[string]dbNode{}, folded: map[string]bool{}, hidden: map[string]bool{}}
}
func (s *dbState) touch() { s.seq++ }

func (s *dbState) rows() []dbTreeRow {
	if s == nil {
		return nil
	}
	var out []dbTreeRow
	var walk func(string, int)
	walk = func(path string, depth int) {
		n, ok := s.nodes[path]
		if !ok {
			return
		}
		if s.filter != "" && !strings.Contains(strings.ToLower(n.name), strings.ToLower(s.filter)) {
			for _, child := range n.children {
				walk(child, depth+1)
			}
			return
		}
		out = append(out, dbTreeRow{kind: n.kind, path: path, name: n.name, conn: n.conn, detail: n.detail, depth: depth, selectable: n.kind != dbNodeNote})
		if s.folded[path] {
			return
		}
		if n.problem != "" {
			out = append(out, dbTreeRow{kind: dbNodeNote, path: path + "/problem", name: n.problem, depth: depth + 1})
			return
		}
		for _, child := range n.children {
			walk(child, depth+1)
		}
		if n.loading {
			out = append(out, dbTreeRow{kind: dbNodeNote, path: path + "/loading", name: "Loading…", depth: depth + 1})
		}
	}
	for _, c := range s.conns {
		walk(c.ID, 0)
	}
	var suggestionPaths []string
	for path, n := range s.nodes {
		if n.kind == dbNodeSuggestion && !s.hidden[path] {
			suggestionPaths = append(suggestionPaths, path)
		}
	}
	sort.Strings(suggestionPaths)
	if len(suggestionPaths) > 0 {
		out = append(out, dbTreeRow{kind: dbNodeSuggestionsHeader, name: "SUGGESTED", selectable: false})
	}
	for _, path := range suggestionPaths {
		walk(path, 0)
	}
	if len(out) == 0 && !s.loaded {
		out = append(out, dbTreeRow{kind: dbNodeNote, name: "Loading database connections…"})
	}
	return out
}

func (m Model) databasesVisible() bool {
	return m.showExp && !m.zenMode && m.activity.Active() == activity.ViewDatabases && !(m.pickerOpen && m.pickerKind == pickerKindPalette)
}

func (m *Model) revealDatabasesView(focus bool) tea.Cmd {
	m.activity.SetActive(activity.ViewDatabases)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	if focus {
		m.focus = FocusExplorer
	}
	m.ensureDatabasesLoaded()
	m.databases.addRuntimeSuggestions(m.services)
	m.databases.visible = true
	return nil
}

// ensureDatabasesLoaded reads the connection files once (and the saved tree
// state) without showing the view: SQL consoles need the connection list
// while the sidebar shows another view.
func (m *Model) ensureDatabasesLoaded() {
	if m.databases == nil {
		m.databases = newDBState()
	}
	if !m.databases.loaded {
		m.databases.load(m.databaseWorkspaceRoot())
		stored := loadSession()
		m.databases.filter = stored.DBFilter
		for _, path := range stored.DBExpanded {
			m.databases.folded[path] = false
		}
		for _, path := range stored.DBHiddenSuggestions {
			m.databases.hidden[path] = true
		}
	}
}

func (m *Model) databasesSaveSession() {
	if m.databases == nil {
		return
	}
	sess := loadSession()
	sess.DBFilter = m.databases.filter
	sess.DBExpanded = nil
	sess.DBHiddenSuggestions = nil
	for path, n := range m.databases.nodes {
		if len(n.children) > 0 && !m.databases.folded[path] {
			sess.DBExpanded = append(sess.DBExpanded, path)
		}
		if n.kind == dbNodeSuggestion && m.databases.hidden[path] {
			sess.DBHiddenSuggestions = append(sess.DBHiddenSuggestions, path)
		}
	}
	sort.Strings(sess.DBExpanded)
	sort.Strings(sess.DBHiddenSuggestions)
	saveSession(sess)
}

func (m *Model) databaseWorkspaceRoot() string {
	if root := aiWorkspaceRoot(); root != "" {
		return root
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func (s *dbState) load(root string) {
	s.root = root
	projectPath := filepath.Join(root, ".termocode", "databases.json")
	userDir, err := configDir()
	var conns []dbclient.Conn
	if err == nil {
		if b, e := os.ReadFile(filepath.Join(userDir, "db", "connections.json")); e == nil {
			cs, _ := dbclient.ParseConfig(b, dbclient.SourceUser, filepath.Join(userDir, "db", "connections.json"), root)
			s.userConns = append([]dbclient.Conn(nil), cs...)
			conns = append(conns, cs...)
		}
	}
	if b, e := os.ReadFile(projectPath); e == nil {
		cs, parseErr := dbclient.ParseConfig(b, dbclient.SourceProject, projectPath, root)
		if parseErr == nil {
			s.projectConns = append([]dbclient.Conn(nil), cs...)
			conns = dbclient.MergeConfig(cs, conns)
		}
	}
	s.conns = conns
	for _, c := range conns {
		n := dbNode{kind: dbNodeConnection, name: dbConnName(c), path: c.ID, conn: c.ID, detail: dbConnDetail(c)}
		if c.Problem != "" {
			n.problem = dbFriendlyProblem(c.Problem)
		}
		s.nodes[c.ID] = n
	}
	s.findSuggestions()
	s.loaded = true
	s.touch()
}

func dbConnName(c dbclient.Conn) string {
	if c.Name != "" {
		return c.Name
	}
	return c.ID
}
func dbConnDetail(c dbclient.Conn) string {
	if c.Driver == "sqlite" {
		return "sqlite"
	}
	d := c.Driver
	if d == "mysql" {
		d = "mysql"
	}
	return d + "  " + c.Host + ":" + itoa(c.Port)
}
func dbFriendlyProblem(s string) string {
	if strings.Contains(s, "literal password") {
		return "Password in project file. Remove it and use p to set it."
	}
	return s
}
func appendDBChild(nodes map[string]dbNode, parent, child string) {
	n := nodes[parent]
	n.children = append(n.children, child)
	nodes[parent] = n
}

func (s *dbState) findSuggestions() {
	known := map[string]bool{}
	for _, c := range s.conns {
		if c.Path != "" {
			known[filepath.Clean(c.Path)] = true
		}
	}
	_ = filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(s.root, path)
		depth := strings.Count(rel, string(filepath.Separator))
		if (d.IsDir() && depth >= 2) || (!d.IsDir() && depth > 2) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".db" && ext != ".sqlite" && ext != ".sqlite3" {
			return nil
		}
		if known[filepath.Clean(path)] {
			return nil
		}
		id := "suggest-" + strings.ReplaceAll(filepath.ToSlash(rel), "/", "-")
		s.nodes[id] = dbNode{kind: dbNodeSuggestion, name: filepath.ToSlash(rel), path: id, detail: "SQLite file", conn: id}
		return nil
	})
	// A simple dotenv scan offers connection URLs without displaying values.
	if b, err := os.ReadFile(filepath.Join(s.root, ".env")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok || strings.HasPrefix(key, "#") {
				continue
			}
			val = strings.Trim(strings.TrimSpace(val), "\"'")
			if key != "DATABASE_URL" && key != "REDIS_URL" && key != "MYSQL_URL" {
				continue
			}
			if strings.Contains(val, "@") || strings.Contains(val, "://") || strings.HasPrefix(val, "sqlite:") || strings.HasPrefix(val, "file:") {
				id := "suggest-env-" + strings.ToLower(key)
				s.nodes[id] = dbNode{kind: dbNodeSuggestion, name: key, path: id, detail: "connection string in .env", conn: id}
			}
		}
	}
}

func (s *dbState) addRuntimeSuggestions(st *servicesState) {
	if st == nil {
		return
	}
	for _, svc := range st.compose {
		driver := dbDriverForImage(svc.Image)
		if driver == "" {
			continue
		}
		port := defaultDBPort(driver)
		for _, p := range svc.Ports {
			if p.Published > 0 {
				port = p.Published
				break
			}
		}
		id := "suggest-service-" + svc.Name
		if _, exists := s.nodes[id]; exists {
			continue
		}
		s.nodes[id] = dbNode{kind: dbNodeSuggestion, name: svc.Name + " (compose)", path: id, conn: id, detail: driver + "  localhost:" + itoa(port), driver: driver, host: "localhost", port: port}
	}
	for _, c := range st.containerRows {
		driver := dbDriverForImage(c.Image)
		if driver == "" {
			continue
		}
		port := defaultDBPort(driver)
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				port = p.PublicPort
				break
			}
		}
		name := c.Image
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		id := "suggest-container-" + c.ID
		if _, exists := s.nodes[id]; exists {
			continue
		}
		s.nodes[id] = dbNode{kind: dbNodeSuggestion, name: name + " (container)", path: id, conn: id, detail: driver + "  localhost:" + itoa(port), driver: driver, host: "localhost", port: port}
	}
}

func dbDriverForImage(image string) string {
	v := strings.ToLower(image)
	switch {
	case strings.Contains(v, "postgres"):
		return "postgres"
	case strings.Contains(v, "mariadb"), strings.Contains(v, "mysql"):
		return "mysql"
	}
	return ""
}
func defaultDBPort(driver string) int {
	switch driver {
	case "postgres":
		return 5432
	case "mysql":
		return 3306
	case "redis":
		return 6379
	}
	return 0
}

type dbConnectMsg struct {
	id         string
	connection string
	epoch      uint64
	generation uint64
	session    dbclient.Session
	password   string
	err        error
}
type dbIntrospectMsg struct {
	path       string
	connection string
	epoch      uint64
	generation uint64
	result     dbclient.IntrospectionResult
	err        error
}

type dbTestMsg struct {
	conn       dbclient.Conn
	password   string
	generation uint64
	err        error
}

// dbSQLSessionFactory is kept replaceable for command-path tests.
var dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, password string) (dbclient.Session, error) {
	sess, err := dbclient.NewSQLSession(ctx, c, password)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// A disconnect invalidates only this connection's outstanding commands.
// Other connections can finish loading without having their results discarded.
func (s *dbState) connectionCommand(id string, cmd tea.Cmd) tea.Cmd {
	epoch := s.connectionEpochs[id]
	return func() tea.Msg {
		switch msg := cmd().(type) {
		case dbConnectMsg:
			msg.connection, msg.epoch = id, epoch
			return msg
		case dbIntrospectMsg:
			msg.connection, msg.epoch = id, epoch
			return msg
		default:
			return msg
		}
	}
}

func dbConnectCommand(c dbclient.Conn, root, password string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var sess dbclient.Session
		var err error
		switch c.Driver {
		case "postgres", "mysql":
			sess, err = dbSQLSessionFactory(ctx, c, password)
		case "sqlite":
			if !filepath.IsAbs(c.Path) {
				c.Path = filepath.Join(root, c.Path)
			}
			sess, err = dbclient.NewSQLiteSession(ctx, c)
		default:
			err = os.ErrInvalid
		}
		if err != nil {
			sess = nil
		}
		if err == nil {
			err = sess.Ping(ctx)
			if err != nil {
				_ = sess.Close()
				sess = nil
			}
		}
		return dbConnectMsg{id: c.ID, generation: generation, session: sess, password: password, err: err}
	}
}

func dbDatabaseConnectCommand(path string, c dbclient.Conn, password string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sess, err := dbSQLSessionFactory(ctx, c, password)
		if err != nil {
			sess = nil
		}
		return dbConnectMsg{id: path, generation: generation, session: sess, password: password, err: err}
	}
}

func dbIntrospectCommand(path string, sess dbclient.Session, req dbclient.IntrospectionRequest, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		res, err := sess.Introspect(ctx, req)
		return dbIntrospectMsg{path: path, generation: generation, result: res, err: err}
	}
}

func dbTestCommand(c dbclient.Conn, password, root string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var sess dbclient.Session
		var err error
		switch c.Driver {
		case "postgres", "mysql":
			sess, err = dbSQLSessionFactory(ctx, c, password)
		case "sqlite":
			if !filepath.IsAbs(c.Path) {
				c.Path = filepath.Join(root, c.Path)
			}
			sess, err = dbclient.NewSQLiteSession(ctx, c)
		default:
			err = fmt.Errorf("this connection type is not available yet")
		}
		if err != nil {
			sess = nil
		}
		if err == nil {
			err = sess.Ping(ctx)
		}
		if sess != nil {
			_ = sess.Close()
		}
		return dbTestMsg{conn: c, password: password, generation: generation, err: err}
	}
}

func (m *Model) databasesClose() {
	if m.databases == nil {
		return
	}
	m.databases.console.stop("")
	for id, s := range m.databases.sessions {
		_ = s.Close()
		delete(m.databases.sessions, id)
	}
	for _, c := range m.databases.conns {
		m.databases.resetConnectionTree(c.ID)
	}
	clear(m.databases.passwords)
	if m.databases.visible {
		m.databases.generation++
	}
	m.databases.activeID = ""
	m.databases.visible = false
}

func (s *dbState) resetConnectionTree(id string) {
	for path, n := range s.nodes {
		if n.conn == id && path != id {
			delete(s.nodes, path)
		}
	}
	n := s.nodes[id]
	n.children, n.loaded, n.loading, n.problem = nil, false, false, ""
	s.nodes[id] = n
	s.touch()
}

func (s *dbState) addIntrospection(path string, result dbclient.IntrospectionResult) {
	n := s.nodes[path]
	n.children = nil
	n.problem = ""
	s.nodes[path] = n
	if n.kind == dbNodeConnection {
		for _, name := range result.Databases {
			child := path + "/db/" + name
			s.nodes[child] = dbNode{kind: dbNodeDatabase, name: name, path: child, conn: n.conn}
			n.children = append(n.children, child)
		}
		if len(n.children) == 0 && len(result.Schemas) > 0 {
			n.children = append(n.children, path+"/schema")
			s.nodes[path+"/schema"] = dbNode{kind: dbNodeSchema, name: "public", path: path + "/schema", conn: n.conn}
		}
	} else if n.kind == dbNodeDatabase || n.kind == dbNodeSchema {
		for _, name := range result.Schemas {
			child := path + "/schema/" + name
			s.nodes[child] = dbNode{kind: dbNodeSchema, name: name, path: child, conn: n.conn}
			n.children = append(n.children, child)
		}
		if len(n.children) == 0 {
			// addGroups appends to the stored node, so keep our copy in step.
			s.nodes[path] = n
			s.addGroups(path, n.conn, result)
			n = s.nodes[path]
		}
	} else if n.kind == dbNodeGroup {
		s.nodes[path] = n
		s.addObjects(path, n.conn, result)
		n = s.nodes[path]
	} else if n.kind == dbNodeTable || n.kind == dbNodeView {
		for _, col := range result.Columns {
			child := path + "/col/" + col.Name
			mark := ""
			if col.PK {
				mark = "PK"
			}
			s.nodes[child] = dbNode{kind: dbNodeColumn, name: col.Name + "  " + col.Type, path: child, conn: n.conn, detail: mark}
			n.children = append(n.children, child)
		}
		for _, key := range result.Keys {
			child := path + "/key/" + key
			s.nodes[child] = dbNode{kind: dbNodeDetail, name: "Key · " + key, path: child, conn: n.conn}
			n.children = append(n.children, child)
		}
		for _, idx := range result.Indexes {
			child := path + "/index/" + idx.Name
			s.nodes[child] = dbNode{kind: dbNodeDetail, name: "Index · " + idx.Name, path: child, conn: n.conn}
			n.children = append(n.children, child)
		}
		for _, fk := range result.ForeignKeys {
			child := path + "/fk/" + fk.Name
			s.nodes[child] = dbNode{kind: dbNodeDetail, name: "Foreign key · " + fk.Column + " → " + fk.RefTable + "." + fk.RefColumn, path: child, conn: n.conn}
			n.children = append(n.children, child)
		}
	}
	n.loaded = true
	n.loading = false
	s.nodes[path] = n
	s.touch()
}

func (s *dbState) addGroups(path, conn string, r dbclient.IntrospectionResult) {
	groups := []struct {
		name    string
		kind    dbNodeKind
		objects []dbclient.SchemaObject
	}{{"Tables", dbNodeTable, nil}, {"Views", dbNodeView, nil}, {"Routines", dbNodeRoutine, nil}, {"Sequences", dbNodeSequence, nil}}
	for _, g := range groups {
		child := path + "/" + g.name
		s.nodes[child] = dbNode{kind: dbNodeGroup, name: g.name, path: child, conn: conn, loaded: true}
		appendDBChild(s.nodes, path, child)
	}
	s.addObjects(path, conn, r)
}
func (s *dbState) addObjects(path, conn string, r dbclient.IntrospectionResult) {
	for _, o := range r.Objects {
		kind := dbNodeTable
		if strings.Contains(strings.ToLower(o.Kind), "view") {
			kind = dbNodeView
		}
		group := "Tables"
		if kind == dbNodeView {
			group = "Views"
		}
		gpath := path + "/" + group
		if _, ok := s.nodes[gpath]; !ok {
			s.nodes[gpath] = dbNode{kind: dbNodeGroup, name: group, path: gpath, conn: conn, loaded: true}
			appendDBChild(s.nodes, path, gpath)
		}
		child := gpath + "/" + o.Name
		s.nodes[child] = dbNode{kind: kind, name: o.Name, path: child, conn: conn}
		appendDBChild(s.nodes, gpath, child)
	}
	for _, name := range r.Routines {
		gpath := path + "/Routines"
		child := gpath + "/" + name
		s.nodes[child] = dbNode{kind: dbNodeRoutine, name: name, path: child, conn: conn}
		appendDBChild(s.nodes, gpath, child)
	}
	for _, name := range r.Sequences {
		gpath := path + "/Sequences"
		child := gpath + "/" + name
		s.nodes[child] = dbNode{kind: dbNodeSequence, name: name, path: child, conn: conn}
		appendDBChild(s.nodes, gpath, child)
	}
}

func sortedDBConnIDs(nodes map[string]dbNode) []string {
	ids := make([]string, 0, len(nodes))
	for id, n := range nodes {
		if n.kind == dbNodeConnection {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

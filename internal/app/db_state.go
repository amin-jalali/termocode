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
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/services"
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
	dbNodeRedisNS   // a key namespace ("session:")
	dbNodeRedisKey  // a key with its type
	dbNodeRedisMore // the scan stopped at db_redis_max_keys / no keys
	dbNodeLoading   // "Loading…": the node's children are on their way (quiet, not a problem)
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
	// Redis rows: the database index, the full key (or namespace prefix),
	// the type and the TTL in ms (-1: none) from the scan.
	rdb   int
	key   string
	rtype string
	ttl   int64
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
	pendingPasswordRef  dbTypedRef // what was typed when pendingPassword came from a reference
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
	filterTyping        bool // the inline "/" filter takes the keys (db_filter.go)
	seq                 uint64
	rowsHint            int // the length of the last rows() list (a size hint only)
	render              dbRenderCache
	visible             bool
	schemaVer           uint64                         // bumps when the tree or the column cache changes
	cols                map[string]map[string][]string // connection -> lower-case table -> columns (completion)
	manual              map[string]bool                // connection -> manual transaction mode
	txs                 map[string]*dbTxState          // session key -> its transaction
	txModeConn          string                         // the connection the mode picker is for
	txCardConn          string                         // the uncommitted-changes card
	txCardAfter         func(*Model) tea.Cmd
	tunnels             map[string]*dbclient.Tunnel // connection -> its SSH tunnel (db_ssh.go)
	pendingSSH          dbSSHSecrets                // the SSH secrets of the connection being added
	hostAsk             *dbHostAsk                  // the unknown-host card
	secretAsk           *dbSecretAsk                // the SSH password / passphrase prompt
	sshEdit             *dbSSHEdit                  // Add SSH Tunnel / SSL Settings prompts
	compare             *dbCompareState             // Compare Schemas (db_compare.go)
	lastClickAt         time.Time                   // double-click detection (db_view.go)
	lastClickPath       string
	remoteNext          bool // "Add remote…" picked: the type picker is open
	remoteAdd           bool // the connection being added goes through an SSH tunnel
	discovering         bool // DB-16: a discovery runs (db_discover.go)
	discoverGen         uint64
	discoverDone        bool
	discovered          []containers.Container // the container list it read
	discoveredPorts     []int                  // local ports that answered
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
	// Sized like the last list: a big tree grows its slice once, not ten times.
	out := make([]dbTreeRow, 0, s.rowsHint)
	filter := strings.ToLower(s.filter)
	var walk func(string, int)
	walk = func(path string, depth int) {
		n, ok := s.nodes[path]
		if !ok {
			return
		}
		if filter != "" && !strings.Contains(strings.ToLower(n.name), filter) {
			for _, child := range n.children {
				walk(child, depth+1)
			}
			return
		}
		out = append(out, dbTreeRow{kind: n.kind, path: path, name: n.name, conn: n.conn, detail: n.detail, depth: depth, selectable: n.kind != dbNodeNote && n.kind != dbNodeRedisMore})
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
			out = append(out, dbTreeRow{kind: dbNodeLoading, path: path + "/loading", name: "Loading…", depth: depth + 1})
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
		out = append(out, dbTreeRow{kind: dbNodeLoading, name: "Loading database connections…"})
	}
	s.rowsHint = len(out)
	return out
}

// databasesVisible reports whether the Databases view is shown in the
// sidebar. Overlays (the palette, pickers, cards) do not hide it: they must
// never close sessions or cancel a running query. Only another view, a
// hidden sidebar, zen mode, disconnect or quit do.
func (m Model) databasesVisible() bool {
	return m.showExp && !m.zenMode && m.activity.Active() == activity.ViewDatabases
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
	opening := !m.databases.visible
	m.databases.visible = true
	if opening {
		return m.databaseDiscover()
	}
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
	if cwd, err := m.rootDir(); err == nil {
		return cwd
	}
	return "."
}

// dbLoadConfigs reads the user and the project connection files (no
// state, no UI): the view and the AI tools see the same list.
func dbLoadConfigs(root string) (user, project, merged []dbclient.Conn) {
	projectPath := filepath.Join(root, ".termocode", "databases.json")
	if userDir, err := configDir(); err == nil {
		if b, e := os.ReadFile(filepath.Join(userDir, "db", "connections.json")); e == nil {
			cs, _ := dbclient.ParseConfig(b, dbclient.SourceUser, filepath.Join(userDir, "db", "connections.json"), root)
			user = append([]dbclient.Conn(nil), cs...)
			merged = append(merged, cs...)
		}
	}
	if b, e := os.ReadFile(projectPath); e == nil {
		cs, parseErr := dbclient.ParseConfig(b, dbclient.SourceProject, projectPath, root)
		if parseErr == nil {
			project = append([]dbclient.Conn(nil), cs...)
			merged = dbclient.MergeConfig(cs, merged)
		}
	}
	return user, project, merged
}

func (s *dbState) load(root string) {
	s.root = root
	user, project, conns := dbLoadConfigs(root)
	if user != nil {
		s.userConns = user
	}
	if project != nil {
		s.projectConns = project
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
	s.fixCursor()
	s.touch()
}

// fixCursor moves the cursor off a heading (SUGGESTED) to the first row
// that can be selected.
func (s *dbState) fixCursor() {
	rows := s.rows()
	if s.cursor < 0 || s.cursor >= len(rows) || rows[s.cursor].selectable {
		return
	}
	for i := s.cursor; i < len(rows); i++ {
		if rows[i].selectable {
			s.cursor = i
			return
		}
	}
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
	// The tunnel and SSL tags come right after the driver so a narrow
	// sidebar still shows them.
	if c.SSH != nil {
		d += " ssh"
	}
	if mode, err := dbclient.EffectiveSSLMode(c.SSL); err == nil && mode != "" && mode != dbclient.SSLDisable || c.SSL == nil && c.TLS {
		d += " ssl"
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
			// A relative path is relative to the workspace, as on connect.
			if !filepath.IsAbs(c.Path) {
				known[filepath.Clean(filepath.Join(s.root, c.Path))] = true
			} else {
				known[filepath.Clean(c.Path)] = true
			}
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

// dropDiscoverySuggestions removes the container and local-port rows, so a
// new discovery result replaces them.
func (s *dbState) dropDiscoverySuggestions() {
	for path := range s.nodes {
		if strings.HasPrefix(path, "suggest-container-") || strings.HasPrefix(path, "suggest-local-") {
			delete(s.nodes, path)
		}
	}
}

func (s *dbState) addRuntimeSuggestions(st *servicesState) {
	var composeRows []services.Service
	rows := append([]containers.Container(nil), s.discovered...)
	if st != nil {
		composeRows = st.compose
		rows = append(rows, st.containerRows...)
	}
	for _, svc := range composeRows {
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
	for _, c := range rows {
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
		s.nodes[id] = dbNode{kind: dbNodeSuggestion, name: sanitizeDBText(name) + " (container)", path: id, conn: id, detail: driver + "  localhost:" + itoa(port), driver: driver, host: "localhost", port: port}
	}
	s.addLocalSuggestions()
}

// addLocalSuggestions turns each local port that answered into a row,
// unless a saved connection or a container row already covers it.
func (s *dbState) addLocalSuggestions() {
	covered := map[int]bool{}
	for _, c := range s.conns {
		if c.Driver != "sqlite" && dbLocalHost(c.Host) && c.Port > 0 {
			covered[c.Port] = true
		}
	}
	for _, n := range s.nodes {
		if n.kind == dbNodeSuggestion && n.port > 0 && dbLocalHost(n.host) {
			covered[n.port] = true
		}
	}
	for _, port := range s.discoveredPorts {
		if covered[port] {
			continue
		}
		driver := ""
		for _, t := range dbProbeTargets {
			if t.port == port {
				driver = t.driver
			}
		}
		if driver == "" {
			continue
		}
		id := "suggest-local-" + itoa(port)
		s.nodes[id] = dbNode{kind: dbNodeSuggestion, name: dbSuggestTitle(driver) + " on localhost:" + itoa(port), path: id, conn: id, detail: driver + "  localhost:" + itoa(port), driver: driver, host: "localhost", port: port}
	}
}

func dbSuggestTitle(driver string) string {
	if driver == "redis" {
		return "Redis"
	}
	return dbDriverTitle(driver)
}

func dbDriverForImage(image string) string {
	v := strings.ToLower(image)
	switch {
	case strings.Contains(v, "postgres"):
		return "postgres"
	case strings.Contains(v, "mariadb"), strings.Contains(v, "mysql"):
		return "mysql"
	}
	// redis:7, valkey/valkey:8, bitnami/redis, redis/redis-stack-server
	// (not GUI images such as redis/redisinsight).
	name := v
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name, _, _ = strings.Cut(name, ":")
	name, _, _ = strings.Cut(name, "@")
	switch name {
	case "redis", "valkey", "redis-stack", "redis-stack-server":
		return "redis"
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
	tunnel     *dbclient.Tunnel // opened for this connect (nil: none or per-database)
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
	existing   bool          // a saved row's test (not the add flow)
	via        string        // the SSH server the test went through
	elapsed    time.Duration // connect + ping
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

func dbConnectCommand(c dbclient.Conn, root, password string, generation uint64, sec dbSSHSecrets) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbTestTimeout)
		defer cancel()
		sess, tun, err := dbOpenSession(ctx, c, root, password, sec)
		return dbConnectMsg{id: c.ID, generation: generation, session: sess, password: password, tunnel: tun, err: err}
	}
}

// dbOpenSession opens and pings a session of c, through a new SSH tunnel
// when c has one (the caller owns both; on error neither is left open).
func dbOpenSession(ctx context.Context, c dbclient.Conn, root, password string, sec dbSSHSecrets) (dbclient.Session, *dbclient.Tunnel, error) {
	c = dbResolveConnPaths(c, root, dbSSHHome())
	var tun *dbclient.Tunnel
	if c.SSH != nil && c.Driver != "sqlite" {
		var err error
		tun, err = dbOpenTunnel(ctx, c, sec)
		if err != nil {
			return nil, nil, err
		}
		c.Dial = tun.DialContext
	}
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
	case "redis":
		sess, err = dbRedisSessionFactory(ctx, c, password)
	default:
		err = os.ErrInvalid
	}
	if err == nil {
		if err = sess.Ping(ctx); err != nil {
			_ = sess.Close()
		}
	}
	if err != nil {
		if tun != nil {
			_ = tun.Close()
		}
		return nil, nil, err
	}
	return sess, tun, nil
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
	return dbTestCommandSSH(c, password, root, generation, dbSSHSecrets{}, false)
}

// dbTestCommandSSH opens a throwaway session (and tunnel) of c, pings it
// and closes both.
func dbTestCommandSSH(c dbclient.Conn, password, root string, generation uint64, sec dbSSHSecrets, existing bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbTestTimeout)
		defer cancel()
		start := time.Now()
		var err error
		if c.Driver != "postgres" && c.Driver != "mysql" && c.Driver != "sqlite" && c.Driver != "redis" {
			err = fmt.Errorf("this connection type is not available yet")
		}
		var sess dbclient.Session
		var tun *dbclient.Tunnel
		if err == nil {
			sess, tun, err = dbOpenSession(ctx, c, root, password, sec)
		}
		msg := dbTestMsg{conn: c, password: password, generation: generation, err: err, existing: existing, elapsed: time.Since(start)}
		if tun != nil {
			msg.via = tun.Target().Host
		}
		if sess != nil {
			_ = sess.Close()
		}
		if tun != nil {
			_ = tun.Close()
		}
		return msg
	}
}

// databasesClose closes the sessions (the view hid, or the app quits).
// keepTx keeps the connections that hold an open transaction: hiding the
// view must not roll back what the user has not committed yet.
func (m *Model) databasesClose(keepTx bool) {
	if m.databases == nil {
		return
	}
	d := m.databases
	d.console.stop("")
	kept := map[string]bool{}
	for id, s := range d.sessions {
		if keepTx && d.txOpen(id) {
			kept[dbTxConn(id)] = true
			continue
		}
		_ = s.Close()
		delete(d.sessions, id)
	}
	for _, c := range d.conns {
		if kept[c.ID] {
			continue
		}
		d.resetConnectionTree(c.ID)
		delete(d.passwords, c.ID)
	}
	d.closeTunnels(kept)
	if !keepTx {
		clear(d.passwords)
		d.txs = nil
	}
	for key := range d.txs {
		if d.sessions[key] == nil {
			delete(d.txs, key)
		}
	}
	if d.visible {
		d.generation++
	}
	if !kept[d.activeID] {
		d.activeID = ""
	}
	d.visible = false
}

func (s *dbState) resetConnectionTree(id string) {
	delete(s.cols, id)
	s.schemaVer++
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
	s.schemaVer++
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
		names := make([]string, 0, len(result.Columns))
		for _, col := range result.Columns {
			names = append(names, col.Name)
		}
		s.setColumns(n.conn, n.name, names)
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

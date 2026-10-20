package app

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

func databasesPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "db-focus", Title: "Databases: Focus"},
		{ID: "db-new-console", Title: "Databases: New Console"},
		{ID: "db-run", Title: "Databases: Run Statement or Selection"},
		{ID: "db-run-file", Title: "Databases: Run File"},
		{ID: "db-explain", Title: "Databases: Explain Statement"},
		{ID: "db-explain-analyze", Title: "Databases: Explain Analyze Statement"},
		{ID: "db-history", Title: "Databases: History…"},
		{ID: "db-cancel", Title: "Databases: Cancel Query"},
		{ID: "db-add", Title: "Databases: Add Connection…"},
		{ID: "db-connect", Title: "Databases: Connect Selected"},
		{ID: "db-disconnect", Title: "Databases: Disconnect Selected"},
		{ID: "db-refresh", Title: "Databases: Refresh"},
		{ID: "db-configure", Title: "Databases: Configure"},
		{ID: "db-test-connection", Title: "Databases: Test Connection"},
		{ID: "db-data", Title: "Databases: Open Table Data"},
		{ID: "db-select", Title: "Databases: Select 100 Rows in Console"},
		{ID: "db-describe", Title: "Databases: Describe Table"},
		{ID: "db-ddl", Title: "Databases: Show DDL"},
		{ID: "db-count-rows", Title: "Databases: Count Rows"},
		{ID: "db-transaction-mode", Title: "Databases: Transaction Mode…"},
		{ID: "db-commit", Title: "Databases: Commit"},
		{ID: "db-rollback", Title: "Databases: Rollback"},
		{ID: "db-submit-changes", Title: "Databases: Submit Pending Changes"},
		{ID: "db-revert-changes", Title: "Databases: Revert Pending Changes"},
		{ID: "db-result-pin", Title: "Databases: Pin Result Tab"},
		{ID: "db-export", Title: "Databases: Export Result…"},
		{ID: "db-import-csv", Title: "Databases: Import CSV into Table…"},
	}
}

func (m *Model) dispatchDatabasesPalette(id string) (tea.Cmd, bool) {
	if !strings.HasPrefix(id, "db-") {
		return nil, false
	}
	switch id {
	case "db-run":
		return m.databaseSend(false), true
	case "db-run-file":
		return m.databaseSend(true), true
	case "db-explain":
		return m.databaseExplainKey(false), true
	case "db-explain-analyze":
		return m.databaseExplainKey(true), true
	case "db-history":
		return m.databaseHistory(), true
	case "db-cancel":
		return m.databaseCancelRun(), true
	case "db-transaction-mode":
		return m.databaseTxModePicker(m.databaseContextConn()), true
	case "db-commit":
		return m.databaseCommitKey(true), true
	case "db-rollback":
		return m.databaseCommitKey(false), true
	case "db-submit-changes":
		return m.databaseSubmitCard(), true
	case "db-revert-changes":
		if t := m.dbConsole().curTab(); !t.dirty() {
			return m.pushToast(toast.Info, "No pending changes"), true
		}
		return m.databaseRevertKey(m.dbConsole().curTab(), -1, -1), true
	case "db-result-pin":
		return m.databasePinTab(), true
	case "db-export":
		return m.databaseExportStart(), true
	}
	open := m.revealDatabasesView(id == "db-focus")
	switch id {
	case "db-data":
		return tea.Batch(open, m.databaseOpenTable()), true
	case "db-select":
		return tea.Batch(open, m.databaseSelectInConsole()), true
	case "db-describe":
		return tea.Batch(open, m.databaseTableMeta(false)), true
	case "db-ddl":
		return tea.Batch(open, m.databaseTableMeta(true)), true
	case "db-count-rows":
		return tea.Batch(open, m.databaseCountFromTree()), true
	case "db-import-csv":
		return tea.Batch(open, m.databaseImportStart()), true
	case "db-new-console":
		if r, ok := m.databaseCurrentRow(); ok {
			return m.databaseNewConsole(r.conn), true
		}
		return nil, true
	case "db-focus":
		return open, true
	case "db-add":
		return tea.Batch(open, m.databaseConfigure(true)), true
	case "db-configure":
		return tea.Batch(open, m.databaseConfigure(false)), true
	case "db-refresh":
		return tea.Batch(open, m.databaseRefresh()), true
	case "db-connect", "db-test-connection":
		r, ok := m.databaseCurrentRow()
		if ok && r.kind == dbNodeConnection {
			return tea.Batch(open, m.databaseConnect(r.conn)), true
		}
		return open, true
	case "db-disconnect":
		r, ok := m.databaseCurrentRow()
		if ok && r.kind == dbNodeConnection {
			return tea.Batch(open, m.databaseDisconnectAsk(r.conn)), true
		}
		return open, true
	default:
		return nil, false
	}
}

func (m Model) handleDatabasesSidebarKey(k tea.KeyMsg) (Model, tea.Cmd) {
	s := m.databases
	if s == nil {
		s = newDBState()
		m.databases = s
	}
	rows := s.rows()
	row, have := m.databaseCurrentRow()
	switch k.String() {
	case "up", "k":
		m.databaseMove(-1)
		s.touch()
	case "down", "j":
		m.databaseMove(1)
		s.touch()
	case "home", "g":
		s.cursor = 0
		s.touch()
	case "end", "G":
		s.cursor = max(len(rows)-1, 0)
		s.touch()
	case "left", "h":
		if have {
			if n := s.nodes[row.path]; n.path != "" {
				s.folded[n.path] = true
				s.touch()
			}
		}
	case "right", "l", "enter", " ":
		if !have {
			return m, nil
		}
		if row.kind == dbNodeSuggestion {
			return m, m.databaseAddSuggestion(row.path)
		}
		if row.kind == dbNodeConnection {
			if _, ok := s.sessions[row.conn]; !ok {
				return m, m.databaseConnect(row.conn)
			}
		}
		if n, ok := s.nodes[row.path]; ok && !n.loaded && !n.loading {
			s.folded[row.path] = false
			return m, m.databaseExpand(row.path)
		}
		if n, ok := s.nodes[row.path]; ok && len(n.children) > 0 {
			s.folded[row.path] = !s.folded[row.path]
			s.touch()
		}
	case "n":
		if have {
			return m, m.databaseNewConsole(row.conn)
		}
	case "H":
		return m, m.databaseHistory()
	case "a":
		if have && row.kind == dbNodeSuggestion {
			return m, m.databaseAddSuggestion(row.path)
		}
		return m, m.openDatabaseTypePicker()
	case "e":
		if have && row.kind == dbNodeConnection {
			return m, m.databaseEdit(row.conn)
		}
		return m, m.databaseConfigure(false)
	case "x":
		if have && row.kind == dbNodeSuggestion {
			s.hidden[row.path] = true
			s.touch()
			return m, nil
		}
		if have && row.kind == dbNodeConnection {
			return m, m.databaseDisconnectAsk(row.conn)
		}
	case "p":
		if have && row.kind == dbNodeConnection {
			s.pendingSecretID = row.conn
			m.prompt = prompt.New("Databases: Set Password", "Password:", "")
			m.prompt.SetMasked(true)
			m.prompt.SetSize(m.w, m.h)
			m.promptOpen = true
			m.promptKind = promptKindDBPassword
			return m, nil
		}
	case "R":
		return m, m.databaseRefresh()
	case "t":
		if have && row.conn != "" {
			return m, m.databaseTxModePicker(row.conn)
		}
	case "y":
		if have && row.kind == dbNodeCode {
			m.copyToClipboard(row.name)
			return m, m.pushToastDetail(toast.Info, "Copied install command", row.name)
		}
		return m, m.databaseCopyTableName()
	case "o":
		return m, m.databaseOpenTable()
	case "d":
		return m, m.databaseTableMeta(false)
	case "D":
		return m, m.databaseTableMeta(true)
	case "s":
		return m, m.databaseSelectInConsole()
	case "#":
		return m, m.databaseCountFromTree()
	case "i":
		return m, m.databaseImportStart()
	case "/":
		cmd := m.openHTTPPrompt(promptKindDBFilter, "Databases: Filter", "Name contains:", "")
		m.prompt.SetAllowEmpty(true)
		return m, cmd
	case "?":
		return m, m.pushToastDetail(toast.Info, "Database keys", "j/k move · Enter connect or expand · a add · e edit · t transaction mode · R refresh · x disconnect · / filter · on a table: o data · d describe · D DDL · s select · i import CSV · # count · y copy name")
	case "esc":
		m.focus = FocusEditor
	case "m":
		return m, m.databaseConfigure(false)
	}
	m.databasesSaveSession()
	return m, nil
}

const confirmKindDBDisconnect confirmKindEnum = 1200
const confirmKindDBMovePassword confirmKindEnum = 1201

func (m *Model) databaseConfirmDisconnect(id string) {
	if m.databases == nil {
		return
	}
	n := m.databases.nodes[id]
	m.databases.pendingDisconnectID = id
	m.confirm = confirm.New("Disconnect database?", "Disconnect from "+sanitizeDBText(dbConnName(dbclient.Conn{ID: id, Name: n.name}))+" and close its session?", []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "disconnect", Title: "Disconnect", Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBDisconnect
}

func (m *Model) handleDBConfirm(id string) (tea.Cmd, bool) {
	if cmd, ok := m.databaseConsoleConfirm(id); ok {
		return cmd, true
	}
	if m.confirmKind == confirmKindDBMovePassword {
		if m.databases == nil {
			return nil, true
		}
		connID := m.databases.pendingSecretID
		m.databases.pendingSecretID = ""
		if id != "move" || m.databases == nil {
			return nil, true
		}
		c, ok := m.databases.connection(connID)
		if !ok || c.Source != dbclient.SourceUser {
			return nil, true
		}
		literal, hasLiteral := databaseLiteralPassword(c)
		if !hasLiteral {
			return nil, true
		}
		store := databaseSecretStore()
		if store == nil {
			return m.pushToastDetail(toast.Errr, "Secret store unavailable", "Could not open the password store."), true
		}
		if err := store.Set("db."+connID, literal); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not save password", sanitizeDBText(err.Error())), true
		}
		for i := range m.databases.userConns {
			if m.databases.userConns[i].ID == connID {
				m.databases.userConns[i] = databaseRedactLiteralPassword(m.databases.userConns[i])
				c = m.databases.userConns[i]
			}
		}
		dir, err := configDir()
		if err != nil {
			return m.pushToastDetail(toast.Errr, "Password saved, but the config could not be updated", sanitizeDBText(err.Error())), true
		}
		if err := writeDBConfig(filepath.Join(dir, "db", "connections.json"), m.databases.userConns); err != nil {
			return m.pushToastDetail(toast.Errr, "Password saved, but the config could not be updated", sanitizeDBText(err.Error())), true
		}
		for i := range m.databases.conns {
			if m.databases.conns[i].ID == connID {
				m.databases.conns[i] = databaseRedactLiteralPassword(m.databases.conns[i])
			}
		}
		return m.databaseConnect(connID), true
	}
	if m.confirmKind != confirmKindDBDisconnect {
		return nil, false
	}
	if m.databases == nil {
		return nil, true
	}
	connID := m.databases.pendingDisconnectID
	m.databases.pendingDisconnectID = ""
	if id != "disconnect" || m.databases == nil {
		return nil, true
	}
	return m.databaseDisconnectNow(connID), true
}

// databaseDisconnectNow closes every session of connID (an open
// transaction ends with it: the server rolls it back).
func (m *Model) databaseDisconnectNow(connID string) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	for key := range m.databases.txs {
		if dbTxConn(key) == connID {
			delete(m.databases.txs, key)
		}
	}
	m.databases.console.stop(connID)
	m.databases.connectionEpochs[connID]++
	for key, sess := range m.databases.sessions {
		if key == connID || strings.HasPrefix(key, connID+"/db/") {
			_ = sess.Close()
			delete(m.databases.sessions, key)
		}
	}
	delete(m.databases.passwords, connID)
	if m.databases.activeID == connID {
		m.databases.activeID = ""
	}
	m.databases.resetConnectionTree(connID)
	return nil
}

func databaseLiteralPassword(c dbclient.Conn) (string, bool) {
	if c.Password != "" {
		if _, err := secrets.ParseRef(c.Password); err != nil {
			return c.Password, true
		}
	}
	if c.URL != "" {
		if parsed, err := dbclient.ParseURL(c.URL); err == nil && parsed.Password != "" {
			return parsed.Password, true
		}
	}
	return "", false
}

func databaseRedactLiteralPassword(c dbclient.Conn) dbclient.Conn {
	if c.URL != "" {
		if parsed, err := dbclient.ParseURL(c.URL); err == nil && parsed.Password != "" {
			c.URL = parsed.URL
		}
	}
	c.Password = ""
	return c
}

func (m *Model) databaseMove(step int) {
	rows := m.databases.rows()
	i := m.databases.cursor + step
	for i >= 0 && i < len(rows) && !rows[i].selectable {
		i += step
	}
	if i >= 0 && i < len(rows) {
		m.databases.cursor = i
	}
}

func (m *Model) databaseConnect(id string) tea.Cmd {
	s := m.databases
	if s == nil || s.nodes[id].loading {
		return nil
	}
	c, ok := s.connection(id)
	if !ok {
		return nil
	}
	if strings.HasPrefix(c.Password, "${secret:") && databaseSecretStore() == nil {
		return m.pushToastDetail(toast.Errr, "Secret store unavailable", "Unlock or configure the secret store before connecting.")
	}
	if c.Source == dbclient.SourceUser {
		if _, hasLiteral := databaseLiteralPassword(c); hasLiteral {
			s.pendingSecretID = id
			m.confirm = confirm.New("Move password to secret storage?", "This connection file contains a password. Move it to the secret store before connecting?", []confirm.Button{
				{ID: "cancel", Title: "Cancel"},
				{ID: "move", Title: "Move password", Style: confirm.StylePrimary},
			})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindDBMovePassword
			return nil
		}
	}
	for key, old := range s.sessions {
		if key == id || strings.HasPrefix(key, id+"/db/") {
			_ = old.Close()
			delete(s.sessions, key)
		}
	}
	s.connectionEpochs[id]++
	generation := s.generation
	n := s.nodes[id]
	n.loading = true
	s.nodes[id] = n
	s.touch()
	password := ""
	if strings.HasPrefix(c.Password, "${dotenv:") && strings.HasSuffix(c.Password, "}") {
		name := strings.TrimSuffix(strings.TrimPrefix(c.Password, "${dotenv:"), "}")
		password = os.Getenv(name)
		if password == "" {
			password, _ = databaseEnvValue(filepath.Join(s.root, ".env"), name)
		}
		if password != "" {
			ai.RegisterSecret(password)
		}
	}
	if store := databaseSecretStore(); store != nil && !strings.HasPrefix(c.Password, "${dotenv:") {
		if c.Password != "" {
			if strings.HasPrefix(c.Password, "${secret:") {
				name := strings.TrimSuffix(strings.TrimPrefix(c.Password, "${secret:"), "}")
				if name != "" {
					password, _, _ = store.Get(name)
				}
			} else if ref, err := secrets.ParseRef(c.Password); err == nil {
				password, err = (&secrets.Resolver{Workspace: s.root, Getenv: func(key string) string { return os.Getenv(key) }}).Resolve(ref)
				if err != nil {
					return m.pushToastDetail(toast.Errr, "Could not resolve database password", sanitizeDBText(err.Error()))
				}
				ai.RegisterSecret(password)
			} else if c.Source == dbclient.SourceUser {
				password = c.Password
				_ = store.Set("db."+id, password)
			}
		}
		if password == "" {
			if value, ok, err := store.Get("db." + id); err != nil {
				n.problem = sanitizeDBText(err.Error())
				s.nodes[id] = n
				s.touch()
				return m.pushToastDetail(toast.Errr, "Could not read database password", sanitizeDBText(err.Error()))
			} else if ok {
				password = value
			}
		}
	}
	return s.connectionCommand(id, dbConnectCommand(c, s.root, password, generation))
}

func (s *dbState) connection(id string) (dbclient.Conn, bool) {
	for _, c := range s.conns {
		if c.ID == id {
			return c, true
		}
	}
	return dbclient.Conn{}, false
}

func (m *Model) databaseExpand(path string) tea.Cmd {
	s := m.databases
	n, ok := s.nodes[path]
	if !ok || n.loading || !dbNodeExpandable(n.kind) {
		return nil
	}
	sess := s.sessions[path]
	// Schema and table rows must use the session of their database, which
	// can differ from the default database of the connection.
	for parent := path; sess == nil; {
		i := strings.LastIndex(parent, "/")
		if i < 0 {
			break
		}
		parent = parent[:i]
		sess = s.sessions[parent]
	}
	if sess == nil {
		sess = s.sessions[n.conn]
	}
	if sess == nil {
		if _, ok := s.nodes[n.conn]; ok {
			return m.databaseConnect(n.conn)
		}
		return nil
	}
	n.loading = true
	s.nodes[path] = n
	s.touch()
	req := dbclient.IntrospectionRequest{}
	if n.kind == dbNodeConnection {
		req.Database = ""
	}
	if n.kind == dbNodeDatabase {
		if c, ok := s.connection(n.conn); ok && c.Driver == "mysql" {
			req.Database, req.Schema = n.name, n.name
		} else if ok && c.Driver == "sqlite" {
			// One file, one session: main and attached databases share it.
			req.Database = n.name
		} else {
			req.Database = n.name
			c, exists := s.connection(n.conn)
			if !exists {
				return nil
			}
			// A session already stored for this path is the target session;
			// only open one when it is missing.
			if s.sessions[path] == nil && c.Database != n.name {
				c.Database = n.name
				if c.URL != "" {
					if target, err := url.Parse(c.URL); err == nil {
						target.Path, target.RawPath = "/"+n.name, ""
						c.URL = target.String()
					}
				}
				n.loading = true
				s.nodes[path] = n
				s.touch()
				return s.connectionCommand(n.conn, dbDatabaseConnectCommand(path, c, s.passwords[n.conn], s.generation))
			}
		}
	}
	if n.kind == dbNodeSchema {
		req.Database = dbAncestorDatabase(n.path)
		req.Schema = n.name
	}
	if n.kind == dbNodeGroup {
		req.Database = dbAncestorDatabase(n.path)
		req.Schema = dbAncestorSchema(n.path)
		if req.Schema == "" {
			if c, ok := s.connection(n.conn); ok && c.Driver == "mysql" {
				req.Schema = req.Database
			}
		}
	}
	if n.kind == dbNodeTable || n.kind == dbNodeView {
		req.Database = dbAncestorDatabase(n.path)
		req.Schema = dbAncestorSchema(n.path)
		if req.Schema == "" {
			if c, ok := s.connection(n.conn); ok && c.Driver == "mysql" {
				req.Schema = req.Database
			}
		}
		req.Object = n.name
	}
	return s.connectionCommand(n.conn, dbIntrospectCommand(path, sess, req, s.generation))
}

func dbAncestorDatabase(path string) string {
	p := strings.Split(path, "/")
	for i, v := range p {
		if v == "db" && i+1 < len(p) {
			return p[i+1]
		}
	}
	return ""
}
func dbAncestorSchema(path string) string {
	p := strings.Split(path, "/")
	for i, v := range p {
		if v == "schema" && i+1 < len(p) {
			return p[i+1]
		}
	}
	return ""
}

func (m *Model) databaseRefresh() tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	for id, sess := range s.sessions {
		_ = sess.Close()
		delete(s.sessions, id)
	}
	root := s.root
	generation := s.generation + 1
	filter, folded, hidden, cursor := s.filter, s.folded, s.hidden, s.cursor
	m.databases = newDBState()
	m.databases.generation = generation
	m.databases.load(root)
	m.databases.filter = filter
	m.databases.hidden = hidden
	for path, isFolded := range folded {
		m.databases.folded[path] = isFolded
	}
	m.databases.cursor = min(cursor, max(len(m.databases.rows())-1, 0))
	m.databases.addRuntimeSuggestions(m.services)
	return nil
}

func (m *Model) databaseConfigure(add bool) tea.Cmd {
	if add {
		return m.openDatabaseTypePicker()
	}
	return m.databaseEdit("")
}

func (m *Model) openDatabaseTypePicker() tea.Cmd {
	items := []picker.Item{
		{ID: "postgres", Title: "PostgreSQL", Hint: "default port 5432"},
		{ID: "mysql", Title: "MySQL / MariaDB", Hint: "default port 3306"},
		{ID: "sqlite", Title: "SQLite", Hint: "a file on this computer"},
		{ID: "url", Title: "From a URL", Hint: "paste a connection URL"},
	}
	m.picker = picker.NewItems(" Add a database connection ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBType
	return nil
}

func (m *Model) databaseStartAdd(kind string) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	s.pendingConn = dbclient.Conn{Source: dbclient.SourceUser}
	s.pendingPassword = ""
	s.addFromURL = kind == "url"
	switch kind {
	case "postgres":
		s.pendingConn.Driver, s.pendingConn.Port, s.pendingConn.User, s.pendingConn.Database = "postgres", 5432, "postgres", "postgres"
		return m.databaseAddPrompt(promptKindDBHost, "Server address", "Host name or address:", "localhost", false)
	case "mysql":
		s.pendingConn.Driver, s.pendingConn.Port, s.pendingConn.User = "mysql", 3306, "root"
		return m.databaseAddPrompt(promptKindDBHost, "Server address", "Host name or address:", "localhost", false)
	case "sqlite":
		s.pendingConn.Driver = "sqlite"
		return m.databaseAddPrompt(promptKindDBSQLitePath, "SQLite file", "Database file:", "data.db", false)
	case "url":
		return m.databaseAddPrompt(promptKindDBAdd, "Connection URL", "Paste a database URL:", "postgres://user@localhost:5432/database", false)
	}
	return nil
}

func (m *Model) databaseAddPrompt(kind promptKindEnum, title, label, value string, masked bool) tea.Cmd {
	m.prompt = prompt.New(title, label, value)
	if masked {
		m.prompt.SetMasked(true)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
	return nil
}

func (m *Model) databasePromptPasswordForPending() tea.Cmd {
	return m.databaseAddPrompt(promptKindDBPassword, "Database password", "Password (leave blank if none):", "", true)
}

func (m *Model) databaseRetestPending() tea.Cmd {
	if m.databases == nil || m.databases.pendingConn.ID == "" {
		return nil
	}
	return dbTestCommand(m.databases.pendingConn, m.databases.pendingPassword, m.databases.root, m.databases.generation)
}

func dbConnectionID(c dbclient.Conn) string {
	base := c.Database
	if c.Driver == "sqlite" {
		base = strings.TrimSuffix(filepath.Base(c.Path), filepath.Ext(c.Path))
	}
	if base == "" {
		base = c.Host
	}
	base = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(base), " ", "-"))
	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = c.Driver + "-" + itoa(int(time.Now().Unix()%100000))
	}
	return base
}

func (s *dbState) duplicateConnectionID(id string) bool {
	for _, existing := range s.conns {
		if existing.ID == id {
			return true
		}
	}
	return false
}

func sanitizeDBText(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(sanitizeOutputLine(line))
}

func (m *Model) databaseEdit(id string) tea.Cmd {
	root := m.databaseWorkspaceRoot()
	path := filepath.Join(root, ".termocode", "databases.json")
	if id != "" {
		if c, ok := m.databases.connection(id); ok && c.Source == dbclient.SourceUser {
			if dir, err := configDir(); err == nil {
				path = filepath.Join(dir, "db", "connections.json")
			}
		}
		if m.nvim != nil {
			return m.databaseOpenConfig(path)
		}
	}
	if id == "" {
		if dir, err := configDir(); err == nil {
			path = filepath.Join(dir, "db", "connections.json")
		}
	}
	return m.databaseOpenConfig(path)
}

func (m *Model) databaseOpenConfig(path string) tea.Cmd {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot open database config", sanitizeDBText(err.Error()))
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(dbclient.ConfigTemplate()), 0o600); err != nil {
			return m.pushToastDetail(toast.Errr, "Cannot create database config", sanitizeDBText(err.Error()))
		}
	}
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.OpenFile("edit", path)
		m.focus = FocusEditor
	}
	return nil
}

func (m *Model) databaseAddSuggestion(path string) tea.Cmd {
	s := m.databases
	n := s.nodes[path]
	if strings.HasPrefix(path, "suggest-env-") {
		raw, ok := databaseEnvValue(filepath.Join(s.root, ".env"), n.name)
		if !ok {
			return m.pushToastDetail(toast.Errr, "Connection suggestion unavailable", "The value could not be read from .env.")
		}
		c, _, err := databaseEnvConnection(s.root, n.name, raw)
		if err != nil {
			return m.pushToastDetail(toast.Errr, "Invalid connection suggestion", sanitizeDBText(err.Error()))
		}
		for _, existing := range s.conns {
			if existing.ID == c.ID {
				return m.pushToastDetail(toast.Warn, "Connection already exists", "Edit the existing connection to change its settings.")
			}
		}
		password := ""
		if c.Password != "" {
			ref, parseErr := secrets.ParseRef(c.Password)
			if parseErr == nil {
				password, err = (&secrets.Resolver{Workspace: s.root}).Resolve(ref)
				if err != nil {
					return m.pushToastDetail(toast.Errr, "Could not read the suggested password", sanitizeDBText(err.Error()))
				}
				ai.RegisterSecret(password)
			}
		}
		s.pendingConn = c
		s.pendingPassword = password
		return dbTestCommand(c, password, s.root, s.generation)
	}
	if strings.HasPrefix(path, "suggest-service-") || strings.HasPrefix(path, "suggest-container-") {
		id := strings.TrimPrefix(strings.TrimPrefix(path, "suggest-service-"), "suggest-container-")
		id = strings.ToLower(strings.ReplaceAll(id, "_", "-"))
		id = strings.ReplaceAll(id, "/", "-")
		if id == "" {
			id = "database"
		}
		for _, existing := range s.conns {
			if existing.ID == id {
				id += "-db"
				break
			}
		}
		c := dbclient.Conn{ID: id, Name: n.name, Driver: n.driver, Host: n.host, Port: n.port, Source: dbclient.SourceProject}
		project := append([]dbclient.Conn(nil), s.projectConns...)
		project = append(project, c)
		if err := writeDBConfig(filepath.Join(s.root, ".termocode", "databases.json"), project); err != nil {
			return m.pushToastDetail(toast.Errr, "Cannot save connection", sanitizeDBText(err.Error()))
		}
		s.projectConns = project
		s.conns = append(s.conns, c)
		s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.Name, path: c.ID, conn: c.ID, detail: dbConnDetail(c)}
		s.hidden[path] = true
		s.touch()
		return nil
	}
	full := filepath.Join(s.root, filepath.FromSlash(n.name))
	rel, _ := filepath.Rel(s.root, full)
	id := strings.ToLower(strings.TrimSuffix(filepath.Base(n.name), filepath.Ext(n.name)))
	id = strings.ReplaceAll(id, " ", "-")
	if id == "" {
		id = "sqlite"
	}
	c := dbclient.Conn{ID: id, Name: filepath.Base(n.name), Driver: "sqlite", Path: filepath.ToSlash(rel), Source: dbclient.SourceProject}
	if err := dbclient.Validate(c); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot add database", sanitizeDBText(err.Error()))
	}
	project := append([]dbclient.Conn(nil), s.projectConns...)
	project = append(project, c)
	if err := writeDBConfig(filepath.Join(s.root, ".termocode", "databases.json"), project); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot save connection", sanitizeDBText(err.Error()))
	}
	delete(s.nodes, path)
	s.projectConns = project
	s.conns = append(s.conns, c)
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.Name, path: c.ID, conn: c.ID, detail: "sqlite"}
	s.touch()
	return nil
}

func writeDBConfig(path string, conns []dbclient.Conn) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Connections []dbclient.Conn `json:"connections"`
	}{conns}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func databaseEnvConnection(root, key, raw string) (dbclient.Conn, string, error) {
	c, err := dbclient.ParseURL(raw)
	if err != nil {
		return dbclient.Conn{}, "", err
	}
	if c.Driver == "redis" {
		return dbclient.Conn{}, "", os.ErrInvalid
	}
	if c.Driver == "sqlite" && !filepath.IsAbs(c.Path) {
		c.Path = filepath.Clean(filepath.Join(root, c.Path))
	}
	c.ID = strings.ToLower(strings.ReplaceAll(key, "_", "-"))
	c.Name = key
	c.Source = dbclient.SourceProject
	password := c.Password
	if password != "" {
		c.Password = "${dotenv:" + key + "}"
	}
	if err := dbclient.Validate(c); err != nil {
		return dbclient.Conn{}, "", err
	}
	return c, password, nil
}

func databaseEnvValue(path, key string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		name, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(name) == key {
			return strings.Trim(strings.TrimSpace(value), "\"'"), true
		}
	}
	return "", false
}

func (m *Model) onDBConnect(msg dbConnectMsg) tea.Cmd {
	s := m.databases
	if s == nil || msg.generation != s.generation || !m.databasesVisible() || msg.connection != "" && msg.epoch != s.connectionEpochs[msg.connection] {
		if msg.session != nil {
			_ = msg.session.Close()
		}
		return nil
	}
	n := s.nodes[msg.id]
	n.loading = false
	if msg.err != nil {
		if s.console != nil && s.console.waiting != nil && s.console.waiting.conn == msg.id {
			s.console.waiting = nil
		}
		n.problem = dbErrorText(msg.err)
		if c, ok := s.connection(msg.id); ok && c.Driver == "sqlite" && (strings.Contains(strings.ToLower(msg.err.Error()), "sqlite3 is not installed") || strings.Contains(strings.ToLower(msg.err.Error()), "too old")) {
			n.problem = ""
			n.loaded = true
			notePath := msg.id + "/sqlite-note"
			codePath := msg.id + "/sqlite-install"
			s.nodes[notePath] = dbNode{kind: dbNodeNote, name: "sqlite3 is missing or too old", path: notePath, conn: msg.id}
			s.nodes[codePath] = dbNode{kind: dbNodeCode, name: dbclient.SQLiteInstallHint(runtime.GOOS, runtime.GOARCH), path: codePath, conn: msg.id}
			n.children = []string{notePath, codePath}
		}
		s.nodes[msg.id] = n
		s.touch()
		return m.pushToastDetail(toast.Errr, "Database connection failed", dbErrorText(msg.err))
	}
	if old := s.sessions[msg.id]; old != nil && old != msg.session {
		_ = old.Close()
	}
	s.sessions[msg.id] = msg.session
	n.problem = ""
	n.loaded = false
	s.nodes[msg.id] = n
	if _, ok := s.connection(msg.id); !ok {
		// A per-database session: the node path is not a connection id, so
		// leave activeID and the stored password alone.
		s.touch()
		return m.databaseExpand(msg.id)
	}
	s.passwords[msg.id] = msg.password
	s.activeID = msg.id
	s.touch()
	if s.console != nil && s.console.waiting != nil && s.console.waiting.conn == msg.id {
		return tea.Batch(m.databaseExpand(msg.id), m.databaseStartRun(s.console.waiting))
	}
	return m.databaseExpand(msg.id)
}

func (m *Model) onDBIntrospect(msg dbIntrospectMsg) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	if msg.generation != s.generation || !m.databasesVisible() {
		return nil
	}
	if msg.connection != "" && msg.epoch != s.connectionEpochs[msg.connection] {
		return nil
	}
	n := s.nodes[msg.path]
	n.loading = false
	if msg.err != nil {
		n.problem = dbErrorText(msg.err)
		s.nodes[msg.path] = n
		s.touch()
		return m.pushToastDetail(toast.Errr, "Could not load database schema", dbErrorText(msg.err))
	}
	msg.result = sanitizeDBIntrospection(msg.result)
	s.addIntrospection(msg.path, msg.result)
	m.databaseSchemaChanged()
	return nil
}

func sanitizeDBIntrospection(in dbclient.IntrospectionResult) dbclient.IntrospectionResult {
	for i := range in.Databases {
		in.Databases[i] = sanitizeDBText(in.Databases[i])
	}
	for i := range in.Schemas {
		in.Schemas[i] = sanitizeDBText(in.Schemas[i])
	}
	for i := range in.Objects {
		in.Objects[i].Name = sanitizeDBText(in.Objects[i].Name)
		in.Objects[i].Kind = sanitizeDBText(in.Objects[i].Kind)
	}
	for i := range in.Columns {
		in.Columns[i].Name = sanitizeDBText(in.Columns[i].Name)
		in.Columns[i].Type = sanitizeDBText(in.Columns[i].Type)
	}
	for i := range in.Keys {
		in.Keys[i] = sanitizeDBText(in.Keys[i])
	}
	for i := range in.Indexes {
		in.Indexes[i].Name = sanitizeDBText(in.Indexes[i].Name)
		for j := range in.Indexes[i].Columns {
			in.Indexes[i].Columns[j] = sanitizeDBText(in.Indexes[i].Columns[j])
		}
	}
	for i := range in.ForeignKeys {
		in.ForeignKeys[i].Name = sanitizeDBText(in.ForeignKeys[i].Name)
		in.ForeignKeys[i].Column = sanitizeDBText(in.ForeignKeys[i].Column)
		in.ForeignKeys[i].RefTable = sanitizeDBText(in.ForeignKeys[i].RefTable)
		in.ForeignKeys[i].RefColumn = sanitizeDBText(in.ForeignKeys[i].RefColumn)
	}
	for i := range in.Routines {
		in.Routines[i] = sanitizeDBText(in.Routines[i])
	}
	for i := range in.Sequences {
		in.Sequences[i] = sanitizeDBText(in.Sequences[i])
	}
	return in
}

func (m *Model) onDBPrompt(kind promptKindEnum, value string) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	switch kind {
	case promptKindDBFilter:
		s.filter = strings.TrimSpace(value)
		s.cursor = 0
		s.touch()
		m.databasesSaveSession()
	case promptKindDBAdd:
		value = strings.TrimSpace(value)
		c, err := dbclient.ParseURL(value)
		if err != nil {
			return m.pushToastDetail(toast.Errr, "That connection URL could not be read", sanitizeDBText(err.Error()))
		}
		if c.Driver == "sqlite" && !filepath.IsAbs(c.Path) {
			c.Path = filepath.Clean(filepath.Join(s.root, c.Path))
		}
		password := c.Password
		c.Password = ""
		c.Source = dbclient.SourceUser
		c.ID = dbConnectionID(c)
		c.Name = c.ID
		if duplicate := s.duplicateConnectionID(c.ID); duplicate {
			return m.pushToastDetail(toast.Warn, "Connection already exists", "Choose another database name or edit the existing connection.")
		}
		s.pendingConn, s.pendingPassword = c, password
		if password != "" {
			return m.databaseRetestPending()
		}
		return m.databasePromptPasswordForPending()
	case promptKindDBHost:
		s.pendingConn.Host = strings.TrimSpace(value)
		if s.pendingConn.Host == "" {
			return m.pushToastDetail(toast.Warn, "Server address is needed", "Enter a host name or address to continue.")
		}
		return m.databaseAddPrompt(promptKindDBPort, "Server port", "Port number:", itoa(s.pendingConn.Port), false)
	case promptKindDBPort:
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port < 1 || port > 65535 {
			return m.pushToastDetail(toast.Warn, "That port is not valid", "Enter a number from 1 to 65535.")
		}
		s.pendingConn.Port = port
		return m.databaseAddPrompt(promptKindDBUser, "Database user", "User name:", s.pendingConn.User, false)
	case promptKindDBUser:
		s.pendingConn.User = strings.TrimSpace(value)
		return m.databaseAddPrompt(promptKindDBName, "Database name", "Database:", s.pendingConn.Database, false)
	case promptKindDBName:
		s.pendingConn.Database = strings.TrimSpace(value)
		if s.pendingConn.Database == "" {
			return m.pushToastDetail(toast.Warn, "Database name is needed", "Enter the database to connect to.")
		}
		s.pendingConn.ID = dbConnectionID(s.pendingConn)
		s.pendingConn.Name = s.pendingConn.ID
		return m.databasePromptPasswordForPending()
	case promptKindDBSQLitePath:
		path := strings.TrimSpace(value)
		if path == "" {
			return m.pushToastDetail(toast.Warn, "Database file is needed", "Enter a path to a SQLite file.")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Clean(filepath.Join(s.root, path))
		}
		s.pendingConn.Path = path
		s.pendingConn.ID = dbConnectionID(s.pendingConn)
		s.pendingConn.Name = s.pendingConn.ID
		return m.databaseRetestPending()
	case promptKindDBPassword:
		if s.pendingConn.ID != "" {
			s.pendingPassword = value
			return m.databaseRetestPending()
		}
		id := s.pendingSecretID
		s.pendingSecretID = ""
		if id == "" {
			return nil
		}
		store := databaseSecretStore()
		if store == nil {
			return m.pushToastDetail(toast.Errr, "Secret store unavailable", "Could not open the password store.")
		}
		if err := store.Set("db."+id, value); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not save password", sanitizeDBText(err.Error()))
		}
		return m.databaseConnect(id)
	}
	return nil
}

func (m *Model) onDBTest(msg dbTestMsg) tea.Cmd {
	if m.databases == nil || msg.generation != m.databases.generation || !m.databasesVisible() {
		return nil
	}
	if msg.err != nil {
		m.databases.pendingConn = msg.conn
		m.databases.pendingPassword = msg.password
		m.picker = picker.NewItems(" Test failed · choose what to do ", []picker.Item{
			{ID: "db-test-password", Title: "Set password…", Hint: "p"},
			{ID: "db-test-retry", Title: "Test again"},
			{ID: "db-test-cancel", Title: "Cancel"},
		})
		m.picker.SetSize(m.w, m.h)
		m.pickerOpen = true
		m.pickerKind = pickerKindDBTestResult
		return m.pushToastDetail(toast.Warn, "Connection test failed", dbErrorText(msg.err))
	}
	m.databases.pendingConn = msg.conn
	m.databases.pendingPassword = msg.password
	m.picker = picker.NewItems(" Connection works · choose where to save ", []picker.Item{{ID: "db-save-project", Title: "Save to project", Hint: ".termocode/databases.json · no password"}, {ID: "db-save-user", Title: "Save for me only", Hint: "User configuration"}})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBSave
	return nil
}

func (m *Model) databaseSavePending(project bool) tea.Cmd {
	s := m.databases
	if s == nil || s.pendingConn.ID == "" {
		return nil
	}
	c := s.pendingConn
	if !strings.HasPrefix(c.Password, "${dotenv:") {
		if _, err := secrets.ParseRef(c.Password); err != nil {
			c.Password = ""
		}
	}
	var path string
	var all []dbclient.Conn
	if project {
		c.Source = dbclient.SourceProject
		path = filepath.Join(s.root, ".termocode", "databases.json")
		all = append(append([]dbclient.Conn(nil), s.projectConns...), c)
	} else {
		c.Source = dbclient.SourceUser
		dir, err := configDir()
		if err != nil {
			return m.pushToastDetail(toast.Errr, "Cannot save connection", sanitizeDBText(err.Error()))
		}
		path = filepath.Join(dir, "db", "connections.json")
		all = append(append([]dbclient.Conn(nil), s.userConns...), c)
	}
	if s.pendingPassword != "" && !strings.HasPrefix(c.Password, "${dotenv:") {
		store := databaseSecretStore()
		if store == nil {
			return m.pushToastDetail(toast.Errr, "Secret store unavailable", "Could not open the password store.")
		}
		if err := store.Set("db."+c.ID, s.pendingPassword); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not save password", sanitizeDBText(err.Error()))
		}
		c.Password = "${secret:db." + c.ID + "}"
	}
	if err := writeDBConfig(path, all); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot save connection", sanitizeDBText(err.Error()))
	}
	if project {
		s.projectConns = all
	} else {
		s.userConns = all
	}
	s.conns = append(s.conns, c)
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.Name, path: c.ID, conn: c.ID, detail: dbConnDetail(c)}
	if c.Name != "" {
		s.hidden["suggest-env-"+strings.ToLower(c.Name)] = true
	}
	s.pendingConn = dbclient.Conn{}
	s.pendingPassword = ""
	s.touch()
	return nil
}

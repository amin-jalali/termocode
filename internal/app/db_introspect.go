package app

import (
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// Introspection control (DBX-29): db_introspect_level sets how much a
// table row loads (auto: less in big schemas), r in the tree reloads one
// object, every load is kept in db/schema-cache.json (names and types
// only) and a load that differs from the cached one marks the row
// "changed since …"; Databases: Show Schema Cache Info says when a
// connection was last loaded; db_complete_catalogs adds the system
// catalogs to completion. When the server is down the tree is built from
// the cache and the connection row says "offline · cached …".

func (s dbConsoleSettings) introspectLevel(objects int) dbclient.IntrospectLevel {
	if s.IntrospectLevel != nil && !strings.EqualFold(strings.TrimSpace(*s.IntrospectLevel), "auto") {
		if l, ok := dbclient.ParseIntrospectLevel(*s.IntrospectLevel); ok {
			return l
		}
	}
	return dbclient.AutoIntrospectLevel(objects)
}

func (s dbConsoleSettings) completeCatalogs() bool {
	return s.CompleteCatalogs != nil && *s.CompleteCatalogs
}

// tableLevel is the level a table or view row at path loads: auto counts
// the objects of its group.
func (s *dbState) tableLevel(path string) dbclient.IntrospectLevel {
	count := 0
	if i := strings.LastIndex(path, "/"); i > 0 {
		count = len(s.nodes[path[:i]].children)
	}
	return loadDBConsoleSettings().introspectLevel(count)
}

var dbSchemaCaches sync.Map // file path -> *dbclient.SchemaCache (one mutex per file)

func (m *Model) dbSchemaCache() *dbclient.SchemaCache {
	p := dbConfigPath("schema-cache.json")
	v, _ := dbSchemaCaches.LoadOrStore(p, &dbclient.SchemaCache{Path: p})
	return v.(*dbclient.SchemaCache)
}

// dbSchemaKey is the cache key of a tree path.
func dbSchemaKey(path, conn string) dbclient.SchemaKey {
	k := dbclient.SchemaKey{Conn: conn}
	parts := strings.Split(path, "/")
	for i := 1; i+1 < len(parts); i++ {
		switch parts[i] {
		case "db":
			k.Database = parts[i+1]
		case "schema":
			k.Schema = parts[i+1]
		case "Tables", "Views":
			k.Object = parts[i+1]
		}
	}
	return k
}

type dbSchemaCacheMsg struct {
	path    string
	changed bool
	since   time.Time
	err     error
}

// databaseCacheSchema stores one load in the schema cache (off the UI
// goroutine) and reports whether it differs from the cached one.
func (m *Model) databaseCacheSchema(path string, r dbclient.IntrospectionResult) tea.Cmd {
	if m.databases == nil {
		return nil
	}
	n, ok := m.databases.nodes[path]
	if !ok {
		return nil
	}
	level := dbclient.LevelFull
	switch n.kind {
	case dbNodeDatabase, dbNodeSchema:
	case dbNodeTable, dbNodeView:
		level = m.databases.tableLevel(path)
	default:
		return nil
	}
	c := m.dbSchemaCache()
	k := dbSchemaKey(path, n.conn)
	return func() tea.Msg {
		old, had, _ := c.Get(k)
		changed := had && old.Level == level && old.Fingerprint != dbclient.Fingerprint(level.Trim(r))
		err := c.Put(k, level, r)
		return dbSchemaCacheMsg{path: path, changed: changed, since: old.RefreshedAt, err: err}
	}
}

func (m *Model) onDBSchemaCache(msg dbSchemaCacheMsg) tea.Cmd {
	if m.databases == nil || !msg.changed {
		return nil
	}
	n, ok := m.databases.nodes[msg.path]
	if !ok {
		return nil
	}
	n.detail = "changed since " + msg.since.Local().Format("Jan 2 15:04")
	m.databases.nodes[msg.path] = n
	m.databases.touch()
	return nil
}

// databaseRefreshNode loads one tree object again; the rest of the tree
// and the sessions stay.
func (m *Model) databaseRefreshNode(path string) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	n, ok := s.nodes[path]
	if !ok || n.loading || !dbNodeExpandable(n.kind) {
		return nil
	}
	if n.kind == dbNodeConnection {
		if _, open := s.sessions[n.conn]; !open {
			return m.databaseConnect(n.conn)
		}
		s.resetConnectionTree(n.conn)
		return m.databaseExpand(path)
	}
	prefix := path + "/"
	for p := range s.nodes {
		if strings.HasPrefix(p, prefix) {
			delete(s.nodes, p)
		}
	}
	n.children, n.loaded, n.problem = nil, false, ""
	if strings.HasPrefix(n.detail, "changed since") {
		n.detail = ""
	}
	s.nodes[path] = n
	s.schemaVer++
	s.folded[path] = false
	s.touch()
	return m.databaseExpand(path)
}

// databaseRefreshObjectKey is r in the tree (and the palette command).
func (m *Model) databaseRefreshObjectKey() tea.Cmd {
	row, ok := m.databaseCurrentRow()
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a tree object", "r reloads the object under the cursor.")
	}
	if n, found := m.databases.nodes[row.path]; !found || !dbNodeExpandable(n.kind) {
		return m.pushToastDetail(toast.Info, "Nothing to reload here", "Pick a connection, database, schema, group or table.")
	}
	return m.databaseRefreshNode(row.path)
}

// databaseSchemaCacheInfo says when the connection's schema was last loaded.
func (m *Model) databaseSchemaCacheInfo() tea.Cmd {
	conn := m.databaseContextConn()
	if conn == "" {
		return m.pushToastDetail(toast.Info, "Select a connection", "The schema cache is kept per connection.")
	}
	at, ok := m.dbSchemaCache().LastRefresh(conn)
	name := m.dbConnLabel(conn)
	if !ok {
		return m.pushToastDetail(toast.Info, "No cached schema for "+name, "Expand the connection in the tree to load it.")
	}
	ago := time.Since(at).Round(time.Minute)
	return m.pushToastDetail(toast.Info, "Schema of "+name+" last loaded "+at.Local().Format("Jan 2 15:04"), ago.String()+" ago. r in the tree reloads one object, R everything.")
}

// dbOfflineDetail starts the detail of a connection row whose tree comes
// from the schema cache because the server could not be reached.
const dbOfflineDetail = "offline · cached "

type dbOfflineTreeMsg struct {
	path       string
	generation uint64
	entry      dbclient.SchemaCacheEntry
	found      bool
	err        error
}

// databaseOfflineTree reads the cached schema of one tree row (off the UI
// goroutine) so the tree can be browsed while the server is down.
func (m *Model) databaseOfflineTree(path string) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	n, ok := s.nodes[path]
	if !ok || !dbNodeExpandable(n.kind) {
		return nil
	}
	c := m.dbSchemaCache()
	k := dbSchemaKey(path, n.conn)
	gen := s.generation
	return func() tea.Msg {
		e, found, err := c.Get(k)
		return dbOfflineTreeMsg{path: path, generation: gen, entry: e, found: found, err: err}
	}
}

func (m *Model) onDBOfflineTree(msg dbOfflineTreeMsg) tea.Cmd {
	s := m.databases
	if s == nil || msg.generation != s.generation {
		return nil
	}
	n, ok := s.nodes[msg.path]
	if !ok || s.sessions[n.conn] != nil {
		return nil // gone, or the server came back meanwhile
	}
	if msg.err != nil || !msg.found {
		if msg.path == n.conn {
			return nil // the connect error toast already explains it
		}
		return m.pushToastDetail(toast.Info, "Not in the schema cache", "This object was never loaded while the server was up. r on the connection tries again.")
	}
	if s.offline == nil {
		s.offline = map[string]time.Time{}
	}
	if _, was := s.offline[n.conn]; !was || msg.path == n.conn {
		s.offline[n.conn] = msg.entry.RefreshedAt
	}
	s.addIntrospection(msg.path, sanitizeDBIntrospection(msg.entry.Result))
	if c, ok := s.nodes[n.conn]; ok {
		c.detail = dbOfflineDetail + s.offline[n.conn].Local().Format("Jan 2 15:04")
		s.nodes[n.conn] = c
	}
	s.touch()
	return nil
}

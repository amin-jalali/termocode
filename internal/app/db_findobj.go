package app

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Databases: Find Object… (DBX-14): the schemas of every connected
// session are read in the background on read-only sessions, and a picker
// lists their tables, views, columns and routines. Enter moves the tree
// cursor to the object (or its table); an object whose schema is not
// expanded yet opens its description instead.

const pickerKindDBFindObject pickerKindEnum = 3861

// dbFindMaxSchemas caps the schemas read per session, so a huge server
// stays quick.
const dbFindMaxSchemas = 50

const dbFindTimeout = 20 * time.Second

// dbFindHit is one picker row: the object and the session that reaches it.
type dbFindHit struct {
	dbclient.ObjectHit
	sessKey, driver string
}

const dbFindSep = "\x1f"

func dbFindID(h dbFindHit) string {
	return strings.Join([]string{h.sessKey, h.driver, h.Conn, h.Schema, h.Kind, h.Name, h.Parent}, dbFindSep)
}

func dbParseFindID(id string) (dbFindHit, bool) {
	f := strings.Split(id, dbFindSep)
	if len(f) != 7 {
		return dbFindHit{}, false
	}
	return dbFindHit{sessKey: f[0], driver: f[1], ObjectHit: dbclient.ObjectHit{Conn: f[2], Schema: f[3], Kind: f[4], Name: f[5], Parent: f[6]}}, true
}

// dbFindSource is one connected session to search.
type dbFindSource struct {
	key, conn, label, driver string
	sess                     dbclient.Session
}

// databaseFindObjectStart opens the picker; its loader reads the schemas.
func (m *Model) databaseFindObjectStart() tea.Cmd {
	s := m.databases
	if s == nil || len(s.sessions) == 0 {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect to a database first: Enter on a connection in the Databases view.")
	}
	var srcs []dbFindSource
	for key, sess := range s.sessions {
		if sess == nil {
			continue
		}
		conn := s.nodes[key].conn
		if conn == "" {
			conn = key
		}
		c, ok := s.connection(conn)
		if !ok || dbclient.DialectFor(c.Driver) == nil {
			continue
		}
		label := dbConnName(c)
		if db := dbAncestorDatabase(key); db != "" {
			label += "/" + db
		}
		srcs = append(srcs, dbFindSource{key: key, conn: conn, label: sanitizeDBText(label), driver: dbDriverFamily(c.Driver), sess: sess})
	}
	if len(srcs) == 0 {
		return m.pushToastDetail(toast.Info, "Find Object needs a SQL connection", "Connect to PostgreSQL, MySQL / MariaDB or SQLite first.")
	}
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].key < srcs[j].key })
	m.picker = picker.NewWith(" Find Object ", func() []picker.Item { return dbFindObjectItems(srcs) })
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDBFindObject
	return m.picker.Init()
}

// dbFindObjectItems reads every source (in the picker's loader goroutine)
// and turns the objects into rows. A source that fails is skipped.
func dbFindObjectItems(srcs []dbFindSource) []picker.Item {
	ctx, cancel := context.WithTimeout(context.Background(), dbFindTimeout)
	defer cancel()
	var items []picker.Item
	for _, src := range srcs {
		hits := dbFindRead(ctx, src)
		for _, h := range hits {
			hint := h.Kind + " · " + src.label
			if h.Schema != "" && src.driver != "sqlite" {
				hint += " · " + sanitizeDBText(h.Schema)
			}
			items = append(items, picker.Item{ID: dbFindID(dbFindHit{ObjectHit: h, sessKey: src.key, driver: src.driver}), Title: sanitizeDBText(h.Path()), Hint: hint})
		}
	}
	return items
}

func dbFindRead(ctx context.Context, src dbFindSource) []dbclient.ObjectHit {
	ro, err := src.sess.ReadOnlySession()
	if err != nil {
		return nil
	}
	defer ro.Close()
	var schemas []string
	switch src.driver {
	case "sqlite":
		schemas = []string{"main"}
	case "mysql":
		r := ro.Run(ctx, "SELECT DATABASE()", dbclient.RunOptions{MaxRows: 1})
		if r.Err == nil && len(r.Rows) > 0 && len(r.Rows[0]) > 0 && !r.Rows[0][0].Null {
			schemas = []string{r.Rows[0][0].Text}
		}
	default:
		q := dbclient.DialectFor(src.driver).CatalogQueries()["schemas"]
		r := ro.Run(ctx, q, dbclient.RunOptions{MaxRows: dbFindMaxSchemas})
		if r.Err == nil {
			for _, row := range r.Rows {
				if len(row) > 0 && !row[0].Null {
					schemas = append(schemas, row[0].Text)
				}
			}
		}
	}
	one := dbclient.ObjectSource{Conn: src.conn}
	for _, sc := range schemas {
		cs, err := dbclient.ReadCanonSchema(ctx, ro, src.driver, sc)
		if err != nil {
			continue
		}
		one.Schemas = append(one.Schemas, cs)
	}
	return dbclient.ListObjects([]dbclient.ObjectSource{one})
}

// findObjectNode is the loaded tree node of a hit (a column's table), or
// "" when its schema is not expanded yet.
func (s *dbState) findObjectNode(h dbFindHit) string {
	name, kinds := h.Name, []dbNodeKind{dbNodeTable}
	switch h.Kind {
	case "column":
		name = h.Parent
	case "view":
		kinds = []dbNodeKind{dbNodeView}
	case "routine":
		kinds = []dbNodeKind{dbNodeRoutine}
	}
	best := ""
	for path, n := range s.nodes {
		if n.conn != h.Conn || n.name != name || (path != h.sessKey && !strings.HasPrefix(path, h.sessKey+"/")) {
			continue
		}
		match := false
		for _, k := range kinds {
			match = match || n.kind == k
		}
		if !match || dbERSchemaOf(s.nodes, path, h.driver) != h.Schema && h.driver != "sqlite" {
			continue
		}
		if best == "" || path < best {
			best = path
		}
	}
	return best
}

// databaseFindObjectPicker is Enter on a Find Object row.
func (m *Model) databaseFindObjectPicker(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBFindObject {
		return nil, false
	}
	s := m.databases
	h, ok := dbParseFindID(id)
	if s == nil || !ok {
		return nil, true
	}
	open := m.revealDatabasesView(true)
	if path := s.findObjectNode(h); path != "" {
		s.filter, s.filterTyping = "", false
		if s.folded == nil {
			s.folded = map[string]bool{}
		}
		for p := path; ; {
			i := strings.LastIndex(p, "/")
			if i < 0 {
				break
			}
			p = p[:i]
			s.folded[p] = false
		}
		s.databaseSelectPath(path)
		return open, true
	}
	if h.Kind == "routine" {
		return tea.Batch(open, m.pushToastDetail(toast.Info, "Expand its schema to see "+sanitizeDBText(h.Name),
			"The routine is in "+sanitizeDBText(h.Schema)+"; open that schema in the Databases tree.")), true
	}
	name := h.Name
	if h.Kind == "column" {
		name = h.Parent
	}
	schema := h.Schema
	if h.driver == "sqlite" {
		schema = ""
	}
	t := dbTable{conn: h.Conn, driver: h.driver, sessKey: h.sessKey, schema: schema, name: name, view: h.Kind == "view", path: h.sessKey}
	return tea.Batch(open, m.databaseDescribeTable(t)), true
}

// databaseDescribeTable describes t when its session is still open.
func (m *Model) databaseDescribeTable(t dbTable) tea.Cmd {
	if m.databases.sessions[t.sessKey] == nil {
		return m.pushToastDetail(toast.Warn, "Not connected any more", "")
	}
	return m.databaseLoadMeta(t, false)
}

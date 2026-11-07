package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Virtual objects (DBX-31): <project>/.termocode/virtual/<connection>.jsonc
// adds foreign keys the database does not declare (by hand or by a name
// rule such as "<table>_id"), computed columns and saved read-only
// SELECTs ("virtual views"). They show in the tree (keys and columns of a
// table, a Virtual Views group under the connection), in completion (the
// virtual column names), in Describe and Quick Documentation, and in
// "Select 100 rows" (the virtual columns are added to the SELECT). Nothing
// is ever written to the database. Databases: Edit Virtual Objects… opens
// the file (a commented template the first time).

// dbNodeVirtualView is a saved SELECT under a connection's Virtual Views.
const dbNodeVirtualView = dbNodeExtension + 1

const dbVirtualGroup = "Virtual Views"

// dbVirtualTemplate is the file Edit Virtual Objects creates.
const dbVirtualTemplate = `// Virtual objects of this connection: only termocode reads them, the
// database never changes. Remove the examples you do not need.
{
  // Foreign keys the database does not declare.
  "foreignKeys": [
    // {"table": "orders", "column": "buyer", "refTable": "users", "refColumn": "id"}
  ],
  // A column named <table><suffix> points at <table> (or <table>s).
  "nameRules": [
    // {"suffix": "_id", "refColumn": "id"}
  ],
  // Computed columns: a read-only SQL expression over the table's columns.
  "columns": [
    // {"table": "orders", "name": "total_eur", "expr": "total * 0.92"}
  ],
  // Saved SELECTs shown under Virtual Views; Enter runs one in the console.
  "views": [
    // {"name": "big_orders", "sql": "SELECT * FROM orders WHERE total > 100"}
  ]
}
`

// dbVirtualPath is the virtual objects file of conn in the project root;
// "" for an id that is not a plain file name.
func dbVirtualPath(root, conn string) string {
	if conn == "" || conn != filepath.Base(conn) || strings.ContainsAny(conn, `/\`) || conn == "." || conn == ".." {
		return ""
	}
	return filepath.Join(root, ".termocode", "virtual", conn+".jsonc")
}

// dbVirtual reads the virtual objects of conn: empty when there is no
// file; problems lists the dropped entries (or the JSON error).
func (s *dbState) dbVirtual(conn string) (dbclient.VirtualObjects, []string) {
	p := dbVirtualPath(s.root, conn)
	if p == "" {
		return dbclient.VirtualObjects{}, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dbclient.VirtualObjects{}, nil
		}
		return dbclient.VirtualObjects{}, []string{err.Error()}
	}
	driver := ""
	if c, ok := s.connection(conn); ok {
		driver = c.Driver
	}
	v, problems, err := dbclient.ParseVirtualObjects(data, driver)
	if err != nil {
		return dbclient.VirtualObjects{}, []string{filepath.Base(p) + ": " + err.Error()}
	}
	return v, problems
}

// dbApplyVirtual adds the virtual keys and columns of the table or view
// name to its introspection result; tables are the table names the name
// rules may point at.
func dbApplyVirtual(v dbclient.VirtualObjects, kind dbNodeKind, name string, tables []string, r dbclient.IntrospectionResult) dbclient.IntrospectionResult {
	if kind != dbNodeTable && kind != dbNodeView {
		return r
	}
	r.ForeignKeys = append(r.ForeignKeys, v.ForeignKeysFor(name, r.Columns, r.ForeignKeys, tables)...)
	for _, c := range v.ColumnsFor(name) {
		r.Columns = append(r.Columns, dbclient.Column{Name: sanitizeDBText(c.Name), Type: "virtual: " + sanitizeDBText(c.Expr)})
	}
	return r
}

// dbTableNames lists the loaded table names of conn.
func (s *dbState) dbTableNames(conn string) []string {
	var out []string
	for _, n := range s.nodes {
		if n.conn == conn && n.kind == dbNodeTable {
			out = append(out, n.name)
		}
	}
	sort.Strings(out)
	return out
}

// addVirtualViews puts a Virtual Views group with v's views under the
// connection node at path (replacing an older one).
func (s *dbState) addVirtualViews(path, conn string, v dbclient.VirtualObjects) {
	gpath := path + "/" + dbVirtualGroup
	for p := range s.nodes {
		if p == gpath || strings.HasPrefix(p, gpath+"/") {
			delete(s.nodes, p)
		}
	}
	n := s.nodes[path]
	kept := n.children[:0:0]
	for _, c := range n.children {
		if c != gpath {
			kept = append(kept, c)
		}
	}
	n.children = kept
	s.nodes[path] = n
	views := v.ViewObjects()
	if len(views) == 0 {
		return
	}
	s.nodes[gpath] = dbNode{kind: dbNodeGroup, name: dbVirtualGroup, path: gpath, conn: conn, loaded: true}
	appendDBChild(s.nodes, path, gpath)
	for _, o := range views {
		child := gpath + "/" + o.Name
		s.nodes[child] = dbNode{kind: dbNodeVirtualView, name: sanitizeDBText(o.Name), path: child, conn: conn, detail: "virtual", loaded: true}
		appendDBChild(s.nodes, gpath, child)
	}
	s.touch()
}

// databaseWithVirtual merges the virtual objects into a result that just
// arrived for the node at path.
func (m *Model) databaseWithVirtual(path string, r dbclient.IntrospectionResult) dbclient.IntrospectionResult {
	s := m.databases
	n := s.nodes[path]
	if n.kind != dbNodeTable && n.kind != dbNodeView {
		return r
	}
	v, _ := s.dbVirtual(n.conn)
	return dbApplyVirtual(v, n.kind, n.name, s.dbTableNames(n.conn), r)
}

// databaseVirtualOnConnection adds the Virtual Views of the connection
// node at path and reports a broken file.
func (m *Model) databaseVirtualOnConnection(path string) tea.Cmd {
	s := m.databases
	n := s.nodes[path]
	if n.kind != dbNodeConnection {
		return nil
	}
	v, problems := s.dbVirtual(n.conn)
	s.addVirtualViews(path, n.conn, v)
	if len(problems) > 0 {
		return m.pushToastDetail(toast.Warn, "Virtual objects: "+dbPlural(len(problems), "entry")+" skipped", sanitizeDBText(strings.Join(problems, "\n")))
	}
	return nil
}

// databaseVirtualSelectList is ", (expr) AS name…" for the virtual
// columns of t ("" when it has none).
func (m *Model) databaseVirtualSelectList(t dbTable) string {
	if m.databases == nil {
		return ""
	}
	v, _ := m.databases.dbVirtual(t.conn)
	d := dbclient.DialectFor(t.driver)
	if d == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range v.ColumnsFor(t.name) {
		b.WriteString(", (" + c.Expr + ") AS " + d.QuoteIdent(c.Name))
	}
	return b.String()
}

// databaseOpenVirtualView runs the saved SELECT of a virtual view row in
// its connection's console.
func (m *Model) databaseOpenVirtualView(r dbTreeRow) tea.Cmd {
	s := m.databases
	c, ok := s.connection(r.conn)
	if !ok {
		return nil
	}
	if s.sessions[r.conn] == nil {
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	v, _ := s.dbVirtual(r.conn)
	sql, ok := v.ViewSQL(r.name)
	if !ok {
		return m.pushToastDetail(toast.Info, "Virtual view not found", "The file changed: Refresh the Databases view.")
	}
	if strings.Contains(sql, "--") {
		return m.pushToastDetail(toast.Info, "Virtual view has a -- comment", "Use /* */ comments in a virtual view: it runs as one console line.")
	}
	stmt := strings.TrimRight(strings.Join(strings.Fields(sql), " "), "; ") + ";"
	return m.databaseConsoleRun(r.conn, c.Driver, s.sessionKeyFor(r.path, r.conn), stmt)
}

// databaseEditVirtual opens the virtual objects file of the connection
// under the tree cursor, written from the template the first time.
func (m *Model) databaseEditVirtual() tea.Cmd {
	m.ensureDatabasesLoaded()
	r, ok := m.databaseCurrentRow()
	if !ok || r.conn == "" {
		return m.pushToastDetail(toast.Info, "Select a connection", "Pick a connection (or anything in it) in the Databases view first.")
	}
	p := dbVirtualPath(m.databases.root, r.conn)
	if p == "" || m.databases.root == "" {
		return m.pushToastDetail(toast.Info, "No project folder", "Virtual objects live in the project's .termocode folder.")
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not create the virtual objects file", err.Error())
		}
		if err := os.WriteFile(p, []byte(dbVirtualTemplate), 0o644); err != nil {
			return m.pushToastDetail(toast.Errr, "Could not create the virtual objects file", err.Error())
		}
	}
	m.openFileAtLine(p, 1)
	return tea.Batch(m.fetchStateCmd(), m.pushToastDetail(toast.Info, "Virtual objects of "+sanitizeDBText(r.conn), "Save the file, then Refresh (R) the Databases view."))
}

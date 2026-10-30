package webproto

// The database data (WEB-43, web design 2.9, T43): the connections with
// no host, user, URL or password; the tables of one; and rows, read in a
// read-only session. Every cell passes the database redaction; a cell of
// a column whose name says it holds a secret is masked whole.

func init() {
	registerResult(OpDBConns, DBConns{})
	registerResult(OpDBTables, DBTables{})
	registerResult(OpDBRows, DBRows{})
	registerResult(OpDBQuery, DBRows{})
	registerResult(OpDBExec, AskRef{})
}

// Database limits.
const (
	MaxDBConns    = 100
	MaxDBTables   = 2000
	DBPageRows    = 100      // rows of one db.rows page
	MaxDBRows     = 1000     // rows of one db.query
	MaxDBBytes    = 1 << 20  // bytes of cell text in one answer
	MaxDBCell     = 4 << 10  // bytes of one cell; more is cut
	MaxDBColumns  = 200      // columns shown
	DBMaskedValue = "••••••" // what a masked cell shows
)

// DBConns answers db.conns.
type DBConns struct {
	Conns []DBConn `json:"conns"`
}

// DBConn is one connection: never its host, user, URL or password.
type DBConn struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Driver   string `json:"driver" enum:"postgres|mysql|sqlite|redis|other"`
	Database string `json:"database,omitempty"` // the database name (a file name for sqlite)
	Env      string `json:"env,omitempty"`      // "production" …
	ReadOnly bool   `json:"readonly,omitempty"`
	Problem  string `json:"problem,omitempty"`
	SQL      bool   `json:"sql"` // the web can browse it (postgres, mysql, sqlite)
}

// DBTables answers db.tables.
type DBTables struct {
	Conn   string    `json:"conn"`
	Schema string    `json:"schema,omitempty"`
	Tables []DBTable `json:"tables"`
	More   int       `json:"more,omitempty"`
}

// DBTable is one table or view.
type DBTable struct {
	Name string `json:"name"`
	View bool   `json:"view,omitempty"`
}

// DBRows answers db.rows and db.query.
type DBRows struct {
	Columns   []string   `json:"columns"`
	Rows      [][]DBCell `json:"rows"`
	From      int        `json:"from"`
	Next      int        `json:"next,omitempty"`      // db.rows: the from of the next page; 0: the last page
	Truncated bool       `json:"truncated,omitempty"` // rows or bytes were cut
	Masked    int        `json:"masked,omitempty"`    // cells masked as secrets
	Ms        int64      `json:"ms"`
}

// DBCell is one value: its text, or null, or masked.
type DBCell struct {
	V string `json:"v,omitempty"`
	N bool   `json:"n,omitempty"` // NULL
	M bool   `json:"m,omitempty"` // masked: V is DBMaskedValue
	C bool   `json:"c,omitempty"` // cut at MaxDBCell
}

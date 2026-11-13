package dbclient

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Spark SQL / Hive (DBX-65): HiveServer2 and the Spark Thrift Server speak
// the same Thrift protocol. The client library (github.com/beltran/gohive;
// Kerberos goes through cgo) is only linked in with -tags hive (hive_on.go);
// the default build gets hive_off.go and a clear message. The rest is plain
// Go: the engine runs on SQLSession through a small database/sql driver
// (hiveConnector) over a hiveBackend, so tests use a fake backend.

// errHiveMissing is what a connection to Hive says in the default build.
var errHiveMissing = errors.New("built without Hive: rebuild with -tags hive")

// HiveConfig is what the backend needs to open one session.
type HiveConfig struct {
	Host      string
	Port      int
	Auth      string // NONE, NOSASL, LDAP, KERBEROS or CUSTOM
	Username  string
	Password  string
	Service   string // the Kerberos service name
	Database  string // the database to open ("" = default)
	Transport string // binary or http
	HTTPPath  string
	TLS       *tls.Config
	Dial      DialFunc // the SSH tunnel
}

// hiveBackend is one open HiveServer2 session.
type hiveBackend interface {
	// Query runs one statement and returns its result set (no columns for
	// a statement without one).
	Query(ctx context.Context, query string) (hiveResult, error)
	Close() error
}

// hiveResult is the rows of one statement.
type hiveResult interface {
	Columns() []string
	Types() []string
	Next() ([]driver.Value, error) // io.EOF after the last row
	Close() error
}

// hiveOpenBackend opens the backend: hive_on.go gives the real one,
// hive_off.go an error; tests replace it with a fake.
var hiveOpenBackend = hiveDriverBackend

// HiveSettings is the session configuration of c: auth NONE without a
// password, LDAP with one, or params.auth (none, nosasl, ldap, kerberos,
// custom); params.service (Kerberos), params.transportMode (binary,
// http) and params.httpPath. c.Database is the Hive database to open.
func HiveSettings(c Conn, password string) (HiveConfig, error) {
	host, port := extraHostPort(c)
	cfg := HiveConfig{Host: host, Port: port, Username: c.User, Password: password, Database: c.Database, Transport: "binary", Dial: c.Dial}
	cfg.Auth = "NONE"
	if password != "" {
		cfg.Auth = "LDAP"
	}
	keys := make([]string, 0, len(c.Params))
	for k := range c.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := c.Params[k]
		switch k {
		case "auth":
			a := strings.ToUpper(v)
			if !slices.Contains([]string{"NONE", "NOSASL", "LDAP", "KERBEROS", "CUSTOM"}, a) {
				return cfg, fmt.Errorf("unknown Hive auth %q (none, nosasl, ldap, kerberos, custom)", v)
			}
			cfg.Auth = a
		case "service":
			cfg.Service = v
		case "transportMode":
			if v != "binary" && v != "http" {
				return cfg, fmt.Errorf("unknown Hive transportMode %q (binary or http)", v)
			}
			cfg.Transport = v
		case "httpPath":
			cfg.HTTPPath = v
		default:
			return cfg, fmt.Errorf("unknown Hive parameter %q (auth, service, transportMode, httpPath)", k)
		}
	}
	if c.SSL != nil {
		t, err := TLSConfig(c.SSL, c.Host)
		if err != nil {
			return cfg, err
		}
		cfg.TLS = t
	}
	return cfg, nil
}

// hiveConnector is the database/sql driver of the engine.
type hiveConnector struct{ cfg HiveConfig }

func (c hiveConnector) Driver() driver.Driver { return hiveDriver{} }
func (c hiveConnector) Connect(ctx context.Context) (driver.Conn, error) {
	b, err := hiveOpenBackend(ctx, c.cfg)
	if err != nil {
		return nil, err
	}
	return &hiveConn{b: b}, nil
}

type hiveDriver struct{}

func (hiveDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("open a Hive connection through its connector")
}

// hiveConn is one session. Hive has no bind API and no transactions.
type hiveConn struct{ b hiveBackend }

func (c *hiveConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Hive runs statements directly")
}
func (c *hiveConn) Close() error { return c.b.Close() }
func (c *hiveConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Hive has no transactions: each statement commits on its own")
}
func (c *hiveConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *hiveConn) Ping(ctx context.Context) error {
	r, err := c.b.Query(ctx, "SELECT 1")
	if err != nil {
		return err
	}
	return r.Close()
}

func (c *hiveConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.query(ctx, q, args)
	if err != nil {
		return nil, err
	}
	_ = r.Close()
	return driver.RowsAffected(0), nil
}

func (c *hiveConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.query(ctx, q, args)
	if err != nil {
		return nil, err
	}
	return hiveRows{r}, nil
}

func (c *hiveConn) query(ctx context.Context, q string, args []driver.NamedValue) (hiveResult, error) {
	vals := make([]any, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	q, err := hiveInlineArgs(q, vals)
	if err != nil {
		return nil, err
	}
	return c.b.Query(ctx, strings.TrimRight(strings.TrimSpace(q), ";"))
}

// hiveRows adapts a hiveResult to driver.Rows.
type hiveRows struct{ r hiveResult }

func (h hiveRows) Columns() []string { return h.r.Columns() }
func (h hiveRows) Close() error      { return h.r.Close() }
func (h hiveRows) Next(dest []driver.Value) error {
	row, err := h.r.Next()
	if err != nil {
		return err
	}
	copy(dest, row)
	return nil
}
func (h hiveRows) ColumnTypeDatabaseTypeName(i int) string {
	if t := h.r.Types(); i < len(t) {
		return t[i]
	}
	return ""
}

// hiveLiteral is v as a Hive / Spark SQL literal: strings escape ' and \
// with a backslash (a doubled quote would read as two literals).
func hiveLiteral(v any) string {
	if s, ok := v.(string); ok {
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
	}
	if b, ok := v.([]byte); ok {
		return hiveLiteral(string(b))
	}
	return sqlLiteral(v)
}

// hiveInlineArgs puts args into the ? placeholders of query. A ? inside a
// string, a quoted name or a comment is left alone.
func hiveInlineArgs(query string, args []any) (string, error) {
	var b strings.Builder
	n := 0
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			for j < len(query) && query[j] != c {
				if query[j] == '\\' && c != '`' {
					j++
				}
				j++
			}
			if j >= len(query) {
				j = len(query) - 1
			}
			b.WriteString(query[i : j+1])
			i = j
		case c == '-' && i+1 < len(query) && query[i+1] == '-':
			j := strings.IndexByte(query[i:], '\n')
			if j < 0 {
				j = len(query) - i - 1
			}
			b.WriteString(query[i : i+j+1])
			i += j
		case c == '?':
			if n >= len(args) {
				return "", errors.New("more placeholders than arguments")
			}
			b.WriteString(hiveLiteral(args[n]))
			n++
		default:
			b.WriteByte(c)
		}
	}
	if n != len(args) {
		return "", errors.New("more arguments than placeholders")
	}
	return b.String(), nil
}

// openHive is openExtraSQLDB's Hive branch.
func openHive(conn Conn, password string) (driver.Connector, error) {
	cfg, err := HiveSettings(conn, password)
	if err != nil {
		return nil, err
	}
	return hiveConnector{cfg}, nil
}

// hiveIntrospect reads the tree with SHOW / DESCRIBE: Hive and Spark have
// no common information_schema.
func (s *SQLSession) hiveIntrospect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	d := HiveDialect{}
	switch {
	case req.Database == "" && req.Schema == "" && req.Object == "":
		return out, s.readCatalog(ctx, "SHOW DATABASES", &out.Databases)
	case req.Schema == "" && req.Object == "":
		out.Schemas = []string{req.Database}
		return out, nil
	}
	schema := req.Schema
	if schema == "" {
		schema = req.Database
	}
	if schema == "" {
		cur := s.Run(ctx, "SELECT current_database()", RunOptions{MaxRows: 1})
		if cur.Err != nil || len(cur.Rows) == 0 || len(cur.Rows[0]) == 0 {
			schema = "default"
		} else {
			schema = cur.Rows[0][0].Text
		}
	}
	in := d.QuoteIdent(schema)
	if req.Object == "" {
		tables := s.Run(ctx, "SHOW TABLES IN "+in, RunOptions{MaxRows: 10000})
		if tables.Err != nil {
			return out, tables.Err
		}
		views := map[string]bool{}
		if v := s.Run(ctx, "SHOW VIEWS IN "+in, RunOptions{MaxRows: 10000}); v.Err == nil {
			for _, row := range v.Rows {
				if len(row) > 0 {
					views[row[len(row)-1].Text] = true
				}
			}
		}
		for _, row := range tables.Rows {
			name := hiveTableName(row)
			if name == "" {
				continue
			}
			kind := "BASE TABLE"
			if views[name] {
				kind = "VIEW"
			}
			out.Objects = append(out.Objects, SchemaObject{Name: name, Kind: kind})
		}
		return out, nil
	}
	full := in + "." + d.QuoteIdent(req.Object)
	desc := s.Run(ctx, "DESCRIBE "+full, RunOptions{MaxRows: 10000})
	if desc.Err != nil {
		return out, desc.Err
	}
	seen := map[string]bool{}
	for _, row := range desc.Rows {
		if len(row) < 2 {
			continue
		}
		name := strings.TrimSpace(row[0].Text)
		switch {
		case row[0].Null || name == "":
			continue // the blank row before the partition section
		case strings.HasPrefix(name, "#"):
			if strings.Contains(name, "Detailed") || strings.Contains(name, "Metadata") {
				return out.withDDL(s, ctx, full), nil
			}
			continue // "# Partition Information" and its header row
		case seen[name]:
			continue // a partition column is listed twice
		}
		seen[name] = true
		out.Columns = append(out.Columns, Column{Name: name, Type: strings.TrimSpace(row[1].Text), Table: req.Object})
	}
	return out.withDDL(s, ctx, full), nil
}

// withDDL adds SHOW CREATE TABLE (views and some tables refuse it).
func (r IntrospectionResult) withDDL(s *SQLSession, ctx context.Context, full string) IntrospectionResult {
	ddl := s.Run(ctx, "SHOW CREATE TABLE "+full, RunOptions{MaxRows: 1000})
	if ddl.Err == nil {
		lines := make([]string, 0, len(ddl.Rows))
		for _, row := range ddl.Rows {
			if len(row) > 0 {
				lines = append(lines, row[0].Text)
			}
		}
		r.DDL = strings.Join(lines, "\n")
	}
	return r
}

// hiveTableName is the table of a SHOW TABLES row: Hive answers one
// column (tab_name), Spark three (database, tableName, isTemporary).
func hiveTableName(row []Cell) string {
	switch {
	case len(row) >= 3:
		return row[1].Text
	case len(row) >= 1:
		return row[0].Text
	}
	return ""
}

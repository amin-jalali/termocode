package dbclient

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// SQLSession implements Session for PostgreSQL and MySQL.
type SQLSession struct {
	db       *sql.DB
	ownsDB   bool
	newDB    func() (*sql.DB, error)
	dialect  Dialect
	name     string
	conn     Conn
	password string
	readonly bool
	mariadb  bool

	mu     sync.Mutex
	manual *sql.Conn
	tx     *sql.Tx
	cancel context.CancelFunc
	closed bool
}

func NewSQLSession(ctx context.Context, conn Conn, password string) (*SQLSession, error) {
	driver := normDriver(conn.Driver)
	var dialect Dialect
	var db *sql.DB
	var err error
	switch driver {
	case "postgres":
		cfg, parseErr := pgx.ParseConfig(conn.DSN(password))
		if parseErr != nil {
			return nil, fmt.Errorf("invalid PostgreSQL connection settings: %s", Redact(parseErr.Error()))
		}
		db = stdlib.OpenDB(*cfg)
		dialect = PostgresDialect{}
	case "mysql":
		cfg := mysql.NewConfig()
		if conn.URL != "" {
			parsed, parseErr := ParseURL(conn.URL)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid MySQL connection settings: %s", Redact(parseErr.Error()))
			}
			conn.Host, conn.Port, conn.User, conn.Database = parsed.Host, parsed.Port, parsed.User, parsed.Database
			if len(conn.Params) == 0 {
				conn.Params = parsed.Params
			}
		}
		cfg.User, cfg.Passwd, cfg.DBName = conn.User, password, conn.Database
		cfg.Net = "tcp"
		cfg.Addr = netJoinHostPort(conn.Host, conn.Port)
		cfg.ParseTime = true
		cfg.Params = conn.Params
		db, err = sql.Open("mysql", cfg.FormatDSN())
		dialect = MySQLDialect{}
	default:
		return nil, fmt.Errorf("unsupported SQL driver %q", conn.Driver)
	}
	if err != nil {
		return nil, fmt.Errorf("could not open database: %s", Redact(err.Error()))
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)
	s := newSQLSession(db, dialect, driver, conn, false)
	s.newDB = func() (*sql.DB, error) { return openSQLDB(conn, password) }
	s.ownsDB = true
	s.password = password
	if err := s.Ping(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if driver == "mysql" {
		var version string
		if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err == nil {
			s.mariadb = strings.Contains(strings.ToLower(version), "mariadb")
		}
	}
	return s, nil
}

// NewPostgresSession and NewMySQLSession are explicit constructors for callers
// that already selected a driver.
func NewPostgresSession(ctx context.Context, conn Conn, password string) (*SQLSession, error) {
	conn.Driver = "postgres"
	return NewSQLSession(ctx, conn, password)
}
func NewMySQLSession(ctx context.Context, conn Conn, password string) (*SQLSession, error) {
	conn.Driver = "mysql"
	return NewSQLSession(ctx, conn, password)
}

func netJoinHostPort(host string, port int) string {
	if port == 0 {
		port = 3306
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func newSQLSession(db *sql.DB, dialect Dialect, name string, conn Conn, readonly bool) *SQLSession {
	return &SQLSession{db: db, dialect: dialect, name: name, conn: conn, readonly: readonly}
}

func openSQLDB(conn Conn, password string) (*sql.DB, error) {
	driver := normDriver(conn.Driver)
	switch driver {
	case "postgres":
		cfg, err := pgx.ParseConfig(conn.DSN(password))
		if err != nil {
			return nil, err
		}
		return stdlib.OpenDB(*cfg), nil
	case "mysql":
		cfg := mysql.NewConfig()
		if conn.URL != "" {
			parsed, err := ParseURL(conn.URL)
			if err != nil {
				return nil, err
			}
			conn.Host, conn.Port, conn.User, conn.Database = parsed.Host, parsed.Port, parsed.User, parsed.Database
			if len(conn.Params) == 0 {
				conn.Params = parsed.Params
			}
		}
		cfg.User, cfg.Passwd, cfg.DBName = conn.User, password, conn.Database
		cfg.Net, cfg.Addr, cfg.ParseTime, cfg.Params = "tcp", netJoinHostPort(conn.Host, conn.Port), true, conn.Params
		return sql.Open("mysql", cfg.FormatDSN())
	default:
		return nil, fmt.Errorf("unsupported SQL driver %q", conn.Driver)
	}
}

func (s *SQLSession) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return errors.New(Redact(err.Error()))
	}
	return nil
}

func (s *SQLSession) ReadOnlySession() (Session, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, errors.New("database session is closed")
	}
	if s.newDB == nil {
		return nil, errors.New("read-only database pool is unavailable")
	}
	db, err := s.newDB()
	if err != nil {
		return nil, safeSQLError(err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(0)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)
	clone := newSQLSession(db, s.dialect, s.name, s.conn, true)
	clone.ownsDB = true
	clone.newDB = s.newDB
	clone.password = s.password
	clone.mariadb = s.mariadb
	return clone, nil
}

func (s *SQLSession) Run(ctx context.Context, statement string, opts RunOptions) (result Result) {
	start := time.Now()
	defer func() { result.Elapsed = time.Since(start) }()
	_, statements := Classify(statement, s.name)
	if len(statements) == 0 {
		result.Err = errors.New("no SQL statement to run")
		return result
	}
	if len(statements) > 1 {
		result.Err = errors.New("multiple SQL statements are not supported")
		return result
	}
	maxRows := opts.MaxRows
	if maxRows <= 0 {
		maxRows = 10000
	}
	for _, st := range statements {
		one := s.runOne(ctx, st.SQL, st.Kind, maxRows, opts.Args)
		result.Elapsed += one.Elapsed
		if one.Err != nil {
			result.Err = one.Err
			return result
		}
		result.Columns = one.Columns
		result.Rows = append(result.Rows, one.Rows...)
		result.Notices = append(result.Notices, one.Notices...)
		result.Affected += one.Affected
		result.Truncated = result.Truncated || one.Truncated
	}
	return result
}

// Explain runs an explain form and guarantees that ANALYZE side effects are
// rolled back when the analyzed statement is not read-only.
func (s *SQLSession) Explain(ctx context.Context, statement string, analyze bool) Result {
	if !analyze {
		return s.Run(ctx, s.dialect.Explain(statement, false), RunOptions{})
	}
	explain := s.dialect.Explain(statement, true)
	if s.mariadb {
		explain = "ANALYZE " + statement
	}
	kind, _ := Classify(statement, s.name)
	if kind == KindRead {
		return s.Run(ctx, explain, RunOptions{})
	}
	if err := s.Begin(ctx); err != nil {
		return Result{Err: err}
	}
	r := s.Run(ctx, explain, RunOptions{})
	rollbackErr := s.Rollback(context.Background())
	if r.Err == nil && rollbackErr != nil {
		r.Err = rollbackErr
	}
	return r
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *SQLSession) runOne(ctx context.Context, query string, kind Kind, maxRows int, args []any) (out Result) {
	started := time.Now()
	defer func() { out.Elapsed = time.Since(started) }()
	ctx, timeoutCancel := context.WithTimeout(ctx, 30*time.Second)
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		timeoutCancel()
		out.Err = errors.New("database session is closed")
		return
	}
	s.cancel = cancel
	var runner sqlRunner = s.db
	if s.tx != nil {
		runner = s.tx
	}
	manualTx := s.tx != nil
	readonly := s.readonly && !manualTx
	conn := s.manual
	s.mu.Unlock()
	defer func() { cancel(); timeoutCancel(); s.mu.Lock(); s.cancel = nil; s.mu.Unlock() }()
	if readonly && kind != KindRead {
		out.Err = errors.New("read-only session refused a non-read-only statement")
		return
	}
	if readonly {
		var err error
		conn, err = s.db.Conn(ctx)
		if err != nil {
			out.Err = safeSQLError(err)
			return
		}
		defer conn.Close()
		if s.name == "mysql" {
			timeoutSetting := "SET SESSION max_execution_time = 30000"
			if s.mariadb {
				timeoutSetting = "SET SESSION max_statement_time = 30"
			}
			if _, err = conn.ExecContext(ctx, timeoutSetting); err == nil {
				_, err = conn.ExecContext(ctx, "START TRANSACTION READ ONLY")
			}
		} else {
			_, err = conn.ExecContext(ctx, s.dialect.ReadOnlyBegin())
			if err == nil {
				_, err = conn.ExecContext(ctx, "SET LOCAL statement_timeout = '30s'")
			}
		}
		if err != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
			out.Err = safeSQLError(err)
			return
		}
		runner = conn
		defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
	} else if s.name == "mysql" && !manualTx {
		var err error
		conn, err = s.db.Conn(ctx)
		if err != nil {
			out.Err = safeSQLError(err)
			return
		}
		defer conn.Close()
		runner = conn
	}
	if s.name == "mysql" && !manualTx && isNativeMySQLDB(s.db) {
		var connectionID uint64
		if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&connectionID); err == nil {
			finished := make(chan struct{})
			defer close(finished)
			go func() {
				select {
				case <-ctx.Done():
					s.killMySQLQuery(connectionID)
				case <-finished:
				}
			}()
		}
	}
	if queryReturnsRows(query, s.name) || s.mariadb && firstSQLWord(query, s.name) == "ANALYZE" {
		rows, err := runner.QueryContext(ctx, query, args...)
		if err != nil {
			out.Err = queryError(ctx, err)
			return
		}
		defer rows.Close()
		out.Columns, err = columnsFromRows(rows)
		if err != nil {
			out.Err = safeSQLError(err)
			return
		}
		for len(out.Rows) < maxRows && rows.Next() {
			values := make([]any, len(out.Columns))
			dests := make([]any, len(values))
			for i := range values {
				dests[i] = &values[i]
			}
			if err := rows.Scan(dests...); err != nil {
				out.Err = safeSQLError(err)
				return
			}
			row := make([]Cell, len(values))
			for i, v := range values {
				typ := ""
				if i < len(out.Columns) {
					typ = out.Columns[i].Type
				}
				row[i] = sqlCell(v, typ)
				if row[i].Kind == "binary" && !containsString(out.Notices, "Binary values are shown as hexadecimal.") {
					out.Notices = append(out.Notices, "Binary values are shown as hexadecimal.")
				}
			}
			out.Rows = append(out.Rows, row)
		}
		if len(out.Rows) == maxRows && rows.Next() {
			out.Truncated = true
			_ = rows.Close()
		}
		if err := rows.Err(); err != nil {
			out.Err = queryError(ctx, err)
			return
		}
		if isDMLStatement(query, s.name) || kind == KindWrite && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "WITH") {
			out.Affected = int64(len(out.Rows))
		}
		if s.name == "mysql" && !manualTx {
			if err := mysqlWarningsInterrupted(ctx, runner); err != nil {
				out.Err = err
			}
		}
		return
	}
	res, err := runner.ExecContext(ctx, query, args...)
	if err != nil {
		out.Err = queryError(ctx, err)
		return
	}
	if isDMLStatement(query, s.name) {
		if n, e := res.RowsAffected(); e == nil {
			out.Affected = n
		}
	}
	return
}

func containsString(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func isNativeMySQLDB(db *sql.DB) bool {
	_, ok := db.Driver().(*mysql.MySQLDriver)
	return ok
}

func (s *SQLSession) killMySQLQuery(connectionID uint64) {
	if s.newDB == nil {
		return
	}
	db, err := s.newDB()
	if err != nil {
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = db.ExecContext(ctx, fmt.Sprintf("KILL QUERY %d", connectionID))
}

func queryError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("stopped after 30 s (timeout)")
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && (mysqlErr.Number == 3024 || mysqlErr.Number == 1969 || mysqlErr.Number == 1317) {
		return errors.New("stopped after 30 s (timeout)")
	}
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "57014" {
		return errors.New("stopped after 30 s (timeout)")
	}
	return safeSQLError(err)
}

func mysqlWarningsInterrupted(ctx context.Context, runner sqlRunner) error {
	rows, err := runner.QueryContext(ctx, "SHOW WARNINGS")
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var level, message string
		var code int
		if err := rows.Scan(&level, &code, &message); err != nil {
			return nil
		}
		text := strings.ToLower(message)
		if strings.Contains(text, "interrupted") || strings.Contains(text, "max_statement_time") || strings.Contains(text, "maximum statement execution time") {
			return errors.New("stopped after 30 s (timeout)")
		}
	}
	return nil
}

func queryReturnsRows(query, dialect string) bool {
	words := tokenWordsDialect(query, dialectArg([]string{dialect}))
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		if word == "RETURNING" {
			return true
		}
	}
	switch words[0] {
	case "SELECT", "WITH", "SHOW", "DESCRIBE", "DESC", "VALUES", "TABLE", "EXPLAIN":
		return true
	case "ANALYZE":
		return len(words) > 1 && (words[1] == "SELECT" || words[1] == "WITH")
	}
	return false
}

func firstSQLWord(query, dialect string) string {
	words := tokenWordsDialect(query, dialectArg([]string{dialect}))
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

func isDMLStatement(query, dialect string) bool {
	words := tokenWordsDialect(query, dialectArg([]string{dialect}))
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "INSERT", "UPDATE", "DELETE", "MERGE", "REPLACE":
		return true
	}
	return false
}

func columnsFromRows(rows *sql.Rows) ([]Column, error) {
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	types, _ := rows.ColumnTypes()
	out := make([]Column, len(names))
	for i, name := range names {
		out[i].Name = name
		if i < len(types) && types[i] != nil {
			out[i].Type = types[i].DatabaseTypeName()
		}
	}
	return out, nil
}
func safeSQLError(err error) error { return errors.New(Redact(err.Error())) }

func (s *SQLSession) Begin(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("database session is closed")
	}
	if s.readonly {
		return errors.New("read-only sessions cannot start a writable transaction")
	}
	if s.tx != nil {
		return errors.New("transaction already active")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return safeSQLError(err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		conn.Close()
		return safeSQLError(err)
	}
	s.manual, s.tx = conn, tx
	return nil
}
func (s *SQLSession) Commit(ctx context.Context) error {
	s.mu.Lock()
	tx, conn := s.tx, s.manual
	s.tx, s.manual = nil, nil
	s.mu.Unlock()
	if tx == nil {
		return errors.New("no transaction is active")
	}
	err := tx.Commit()
	_ = conn.Close()
	if err != nil {
		return safeSQLError(err)
	}
	return nil
}
func (s *SQLSession) Rollback(ctx context.Context) error {
	s.mu.Lock()
	tx, conn := s.tx, s.manual
	s.tx, s.manual = nil, nil
	s.mu.Unlock()
	if tx == nil {
		return errors.New("no transaction is active")
	}
	err := tx.Rollback()
	_ = conn.Close()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		return safeSQLError(err)
	}
	return nil
}
func (s *SQLSession) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (s *SQLSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cancel, tx, conn := s.cancel, s.tx, s.manual
	s.cancel, s.tx, s.manual = nil, nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if tx != nil {
		_ = tx.Rollback()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if s.ownsDB {
		return s.db.Close()
	}
	return nil
}

func (s *SQLSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	queries := s.dialect.CatalogQueries()
	result := IntrospectionResult{}
	if req.Object != "" && req.Schema == "" {
		currentSchema := "SELECT current_schema()"
		if normDriver(s.name) == "mysql" {
			currentSchema = "SELECT DATABASE()"
		}
		if err := s.readCatalog(ctx, currentSchema, &result.Schemas); err != nil {
			return result, err
		}
		if len(result.Schemas) > 0 {
			req.Schema = result.Schemas[0]
		}
	}
	if req.Database == "" && req.Schema == "" && req.Object == "" {
		if err := s.readCatalog(ctx, queries["databases"], &result.Databases); err != nil {
			return result, err
		}
		return result, nil
	}
	if req.Schema == "" && req.Object == "" {
		if err := s.readCatalog(ctx, queries["schemas"], &result.Schemas); err != nil {
			return result, err
		}
		return result, nil
	}
	if req.Object == "" {
		if err := s.readObjects(ctx, queries["objects"], &result.Objects, req.Schema); err != nil {
			return result, err
		}
		for _, key := range []string{"routines", "sequences"} {
			dst := &result.Routines
			if key == "sequences" {
				dst = &result.Sequences
			}
			args := []any(nil)
			if queries[key] != "" && (strings.Contains(queries[key], "$1") || strings.Contains(queries[key], "?")) {
				args = []any{req.Schema}
			}
			if err := s.readCatalog(ctx, queries[key], dst, args...); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	return s.introspectObject(ctx, req, queries, result)
}

func (s *SQLSession) readCatalog(ctx context.Context, query string, dst *[]string, args ...any) error {
	if query == "" {
		return nil
	}
	r := s.Run(ctx, query, RunOptions{MaxRows: 10000, Args: args})
	if r.Err != nil {
		return r.Err
	}
	for _, row := range r.Rows {
		if len(row) > 0 && !row[0].Null {
			*dst = append(*dst, row[0].Text)
		}
	}
	return nil
}
func (s *SQLSession) readObjects(ctx context.Context, query string, dst *[]SchemaObject, args ...any) error {
	r := s.Run(ctx, query, RunOptions{MaxRows: 10000, Args: args})
	if r.Err != nil {
		return r.Err
	}
	for _, row := range r.Rows {
		if len(row) > 1 {
			*dst = append(*dst, SchemaObject{Name: row[0].Text, Kind: row[1].Text})
		}
	}
	return nil
}
func (s *SQLSession) introspectObject(ctx context.Context, req IntrospectionRequest, queries map[string]string, out IntrospectionResult) (IntrospectionResult, error) {
	for _, key := range []string{"columns", "keys", "indexes", "foreign_keys"} {
		q := queries[key]
		if q == "" {
			continue
		}
		args := []any{req.Schema, req.Object}
		r := s.Run(ctx, q, RunOptions{MaxRows: 10000, Args: args})
		if r.Err != nil {
			return out, r.Err
		}
		for _, row := range r.Rows {
			switch key {
			case "columns":
				if len(row) > 1 {
					out.Columns = append(out.Columns, Column{Name: row[0].Text, Type: row[1].Text, Table: req.Object})
				}
			case "keys":
				if len(row) > 0 {
					out.Keys = append(out.Keys, row[0].Text)
				}
			case "indexes":
				if len(row) > 0 {
					idx := SchemaIndex{Name: row[0].Text}
					if len(row) > 1 {
						idx.Columns = strings.FieldsFunc(row[1].Text, func(r rune) bool { return r == ',' || r == ' ' })
					}
					if len(row) > 2 {
						idx.Unique = row[2].Text == "true" || row[2].Text == "1"
					}
					out.Indexes = append(out.Indexes, idx)
				}
			case "foreign_keys":
				if len(row) >= 4 {
					out.ForeignKeys = append(out.ForeignKeys, SchemaForeignKey{Name: row[0].Text, Column: row[1].Text, RefTable: row[2].Text, RefColumn: row[3].Text})
				}
			}
		}
	}
	for i := range out.Columns {
		for _, key := range out.Keys {
			if out.Columns[i].Name == key {
				out.Columns[i].PK = true
				break
			}
		}
	}
	ddlSQL, ddlArgs := s.dialect.DDL(req.Object)
	ddl := s.Run(ctx, ddlSQL, RunOptions{Args: ddlArgs})
	if ddl.Err != nil {
		return out, ddl.Err
	}
	if len(ddl.Rows) > 0 && len(ddl.Rows[0]) > 0 {
		cell := ddl.Rows[0][0]
		if s.name == "mysql" && len(ddl.Rows[0]) > 1 {
			cell = ddl.Rows[0][1]
		}
		out.DDL = cell.Text
	}
	return out, nil
}

func sqlCell(value any, typ string) Cell {
	cell := CellOf(value)
	t := strings.ToUpper(typ)
	if cell.Null {
		return cell
	}
	if date, ok := value.(time.Time); ok && (t == "DATE" || strings.HasPrefix(t, "DATE ")) {
		cell.Text = date.Format("2006-01-02")
		cell.Kind = ""
		return cell
	}
	if b, ok := value.([]byte); ok && isTextSQLType(t) {
		cell.Text = string(b)
		cell.Kind = ""
	}
	if strings.Contains(t, "JSON") {
		cell.Kind = "json"
		if b, ok := value.([]byte); ok {
			cell.Text = string(b)
		}
		return cell
	}
	if strings.Contains(t, "INT") || strings.Contains(t, "DECIMAL") || strings.Contains(t, "NUMERIC") || strings.Contains(t, "FLOAT") || strings.Contains(t, "DOUBLE") || strings.Contains(t, "REAL") {
		cell.Kind = "number"
	}
	if strings.Contains(t, "BOOL") {
		cell.Kind = "bool"
	}
	return cell
}

func isTextSQLType(typ string) bool {
	for _, name := range []string{"CHAR", "TEXT", "CLOB", "ENUM", "SET", "JSON", "DECIMAL", "NUMERIC", "DATE", "TIME", "YEAR"} {
		if strings.Contains(typ, name) {
			return true
		}
	}
	return strings.Contains(typ, "VARCHAR") || strings.Contains(typ, "STRING")
}

var _ Session = (*SQLSession)(nil)

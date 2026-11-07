package dbclient

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/azuread"
	"github.com/microsoft/go-mssqldb/msdsn"
)

// SQL Server / Azure SQL connections (DBX-62): the go-mssqldb driver (pure
// Go), the DSN, Microsoft Entra (AD) sign-in through its azuread package
// when "fedauth" is set, Windows (NTLM) sign-in with a DOMAIN\user name,
// and the SHOWPLAN XML plan on one connection.

// errNoOracleDriver is the reason an Oracle session does not open (DBX-63).
var errNoOracleDriver = errors.New("Oracle support needs the go-ora driver, which is not built in yet")

// DriverMissing is the error of a known engine whose driver is not built
// in; nil for the others.
func DriverMissing(driver string) error {
	switch normDriver(driver) {
	case "oracle":
		return errNoOracleDriver
	}
	return nil
}

// IsAzureSQLHost reports a host of Azure SQL Database / Managed Instance.
func IsAzureSQLHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range []string{"database.windows.net", "database.azure.com", "database.usgovcloudapi.net", "database.chinacloudapi.cn"} {
		if strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// SQLServerDSN is the go-mssqldb URL of c: sqlserver://user:pass@host:port
// ?database=…, with encrypt=true for Azure SQL and c.Params as query
// options (fedauth=ActiveDirectoryDefault and the other AD modes,
// TrustServerCertificate, …). The password is put in only here.
func SQLServerDSN(c Conn, password string) string {
	var u *url.URL
	if c.URL != "" {
		p, err := url.Parse(c.URL)
		if err != nil {
			return c.URL // the driver refuses it with its own reason
		}
		u = p
	} else {
		u = &url.URL{Host: c.Host}
		if c.Port > 0 {
			u.Host = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		}
	}
	u.Scheme = "sqlserver"
	user := c.User
	if user == "" && u.User != nil {
		user = u.User.Username()
	}
	switch {
	case user != "" && password != "":
		u.User = url.UserPassword(user, password)
	case user != "":
		u.User = url.User(user)
	}
	q := u.Query()
	if c.Database != "" && q.Get("database") == "" {
		q.Set("database", c.Database)
	}
	for k, v := range c.Params {
		q.Set(k, v)
	}
	if IsAzureSQLHost(u.Hostname()) && q.Get("encrypt") == "" {
		q.Set("encrypt", "true") // Azure only takes TLS
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// sqlServerConnector is the go-mssqldb connector of conn: the DSN, the
// TLS settings of conn.SSL, the SSH tunnel dial (the server resolves the
// host name) and the start SQL, which runs again after each pool reset.
func sqlServerConnector(conn Conn, password string) (driver.Connector, error) {
	dsn := SQLServerDSN(conn, password)
	var c *mssql.Connector
	if fedauth := sqlServerParam(conn, "fedauth"); fedauth != "" {
		// Microsoft Entra (AD): azidentity finds the credential (the
		// password is the secret of a service principal or a user).
		ac, err := azuread.NewConnector(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid SQL Server connection settings: %s", Redact(err.Error()))
		}
		c = ac
	} else {
		cfg, err := msdsn.Parse(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid SQL Server connection settings: %s", Redact(err.Error()))
		}
		if conn.SSL != nil {
			host := conn.Host
			if host == "" {
				host = cfg.Host
			}
			tlsCfg, err := TLSConfig(conn.SSL, host)
			if err != nil {
				return nil, err
			}
			if tlsCfg != nil {
				cfg.TLSConfig = tlsCfg
				if cfg.Encryption == msdsn.EncryptionOff || cfg.Encryption == msdsn.EncryptionDisabled {
					cfg.Encryption = msdsn.EncryptionRequired
				}
			}
		}
		c = mssql.NewConnectorConfig(cfg)
	}
	if conn.Dial != nil {
		c.Dialer = sqlServerTunnelDialer{dial: conn.Dial, host: conn.Host}
	}
	stmts := connStartSQL(conn)
	if len(stmts) == 0 {
		return c, nil
	}
	c.SessionInitSQL = strings.Join(stmts, ";\n") // after sp_reset_connection
	return startSQLConnector{Connector: c, stmts: stmts}, nil
}

// sqlServerParam is the value of a connection param, any case.
func sqlServerParam(conn Conn, key string) string {
	for k, v := range conn.Params {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	if conn.URL != "" {
		if u, err := url.Parse(conn.URL); err == nil {
			for k, v := range u.Query() {
				if strings.EqualFold(k, key) && len(v) > 0 {
					return v[0]
				}
			}
		}
	}
	return ""
}

// sqlServerTunnelDialer sends the connection through the SSH tunnel. It is
// a HostDialer, so go-mssqldb does not look the host up here: the SSH
// server resolves it.
type sqlServerTunnelDialer struct {
	dial DialFunc
	host string
}

func (d sqlServerTunnelDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.dial(ctx, network, addr)
}
func (d sqlServerTunnelDialer) HostName() string { return d.host }

var _ mssql.HostDialer = sqlServerTunnelDialer{}

// The plan forms of SQL Server (ExplainForms). T-SQL has no EXPLAIN: the
// plan needs SET SHOWPLAN_XML (or STATISTICS XML) in a batch of its own,
// on the same connection, so the form is the statement behind a marker
// that SQLSession.Run turns into those batches.
const (
	sqlServerShowplanMark   = "-- termocode:showplan_xml\n"
	sqlServerStatisticsMark = "-- termocode:statistics_xml\n"
)

// SQLServerPlanForm is the plan form of stmt.
func SQLServerPlanForm(stmt string, analyze bool) string {
	if analyze {
		return sqlServerStatisticsMark + stmt
	}
	return sqlServerShowplanMark + stmt
}

// sqlServerPlanTarget reads a plan form back: the statement and whether it
// runs (STATISTICS XML).
func sqlServerPlanTarget(form string) (stmt string, analyze, ok bool) {
	if s, cut := strings.CutPrefix(form, sqlServerShowplanMark); cut {
		return s, false, true
	}
	if s, cut := strings.CutPrefix(form, sqlServerStatisticsMark); cut {
		return s, true, true
	}
	return "", false, false
}

// runSQLServerPlan runs the batches of SQLServerShowplanBatches on one
// connection (inside the open transaction when there is one) and returns
// the XML plan rows. SHOWPLAN_XML never runs the statement; a read-only
// session refuses STATISTICS XML for a statement that is not a read.
func (s *SQLSession) runSQLServerPlan(ctx context.Context, stmt string, analyze bool) (out Result) {
	start := time.Now()
	defer func() { out.Elapsed = time.Since(start) }()
	kind, statements := Classify(stmt, s.name)
	if len(statements) != 1 {
		return Result{Err: errors.New("explain one statement")}
	}
	if analyze && s.readonly && kind != KindRead {
		return Result{Err: errors.New("read-only session refused a non-read-only statement")}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Result{Err: errors.New("database session is closed")}
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	var runner sqlRunner
	var conn *sql.Conn
	if s.tx != nil {
		runner = s.tx
	}
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); s.cancel = nil; s.mu.Unlock() }()
	if runner == nil {
		var err error
		conn, err = s.db.Conn(ctx)
		if err != nil {
			return Result{Err: safeSQLError(err)}
		}
		defer conn.Close()
		runner = conn
	}
	batches := SQLServerShowplanBatches(statements[0].SQL, analyze)
	if _, err := runner.ExecContext(ctx, batches[0]); err != nil {
		return Result{Err: safeSQLError(err)}
	}
	defer func() {
		if _, err := runner.ExecContext(context.Background(), batches[2]); err != nil && conn != nil {
			// The connection would keep SHOWPLAN on: drop it from the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	rows, err := runner.QueryContext(ctx, batches[1])
	if err != nil {
		return Result{Err: queryError(ctx, err)}
	}
	defer rows.Close()
	// STATISTICS XML sends the statement's own rows first; the plan is
	// the result set whose column names the showplan.
	for {
		cols, err := columnsFromRows(rows)
		if err != nil {
			return Result{Err: safeSQLError(err)}
		}
		plan := len(cols) == 1 && strings.Contains(strings.ToLower(cols[0].Name), "showplan")
		var set [][]Cell
		for rows.Next() {
			var v any
			if len(cols) != 1 {
				continue
			}
			if err := rows.Scan(&v); err != nil {
				return Result{Err: safeSQLError(err)}
			}
			if b, ok := v.([]byte); ok {
				v = string(b) // the plan XML, not hex
			}
			set = append(set, []Cell{CellOf(v)})
		}
		if plan || !analyze {
			out.Columns, out.Rows = cols, append(out.Rows, set...)
		}
		if !rows.NextResultSet() {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return Result{Err: queryError(ctx, err)}
	}
	return out
}

// sqlServerCell formats the SQL Server types the generic reader shows
// badly: a UNIQUEIDENTIFIER as its GUID text (the driver sends the
// mixed-endian bytes), MONEY and XML as text.
func sqlServerCell(value any, typ string) (Cell, bool) {
	b, ok := value.([]byte)
	if !ok {
		return Cell{}, false
	}
	switch strings.ToUpper(typ) {
	case "UNIQUEIDENTIFIER":
		var u mssql.UniqueIdentifier
		if len(b) != 16 || u.Scan(b) != nil {
			return Cell{}, false
		}
		return Cell{Text: u.String()}, true
	case "MONEY", "SMALLMONEY":
		return Cell{Text: string(b), Kind: "number"}, true
	case "XML":
		return Cell{Text: string(b)}, true
	}
	return Cell{}, false
}

// OrderedLimit is Limit with the ORDER BY that T-SQL needs before OFFSET /
// FETCH, for a query that has no ORDER BY of its own.
func OrderedLimit(d Dialect, limit, offset int) string {
	tail := d.Limit(limit, offset)
	if _, ms := d.(SQLServerDialect); ms && tail != "" {
		return " ORDER BY (SELECT NULL)" + tail
	}
	return tail
}

// sqlServerSavepoint turns the standard savepoint statements the app
// sends (SAVEPOINT x, ROLLBACK TO SAVEPOINT x, RELEASE SAVEPOINT x) into
// T-SQL: SAVE TRANSACTION x, ROLLBACK TRANSACTION x, and nothing for
// RELEASE (T-SQL frees savepoints at the end of the transaction).
func sqlServerSavepoint(stmt string) (string, bool) {
	f := strings.Fields(strings.TrimRight(strings.TrimSpace(stmt), ";"))
	switch {
	case len(f) == 2 && strings.EqualFold(f[0], "SAVEPOINT"):
		return "SAVE TRANSACTION " + f[1], true
	case len(f) == 4 && strings.EqualFold(f[0], "ROLLBACK") && strings.EqualFold(f[1], "TO") && strings.EqualFold(f[2], "SAVEPOINT"):
		return "ROLLBACK TRANSACTION " + f[3], true
	case len(f) == 3 && strings.EqualFold(f[0], "RELEASE") && strings.EqualFold(f[1], "SAVEPOINT"):
		return "", true
	}
	return stmt, false
}

package dbclient

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	hdb "github.com/SAP/go-hdb/driver"
	"github.com/SAP/go-hdb/driver/dial"
	_ "github.com/exasol/exasol-driver-go"
	_ "github.com/nakagami/firebirdsql"
	vertica "github.com/vertica/vertica-sql-go"
)

// More SQL engines on pure-Go drivers (DBX-71): Firebird, Vertica,
// Exasol and SAP HANA. They run on SQLSession with their own dialect;
// only the way to open the pool differs. IBM Db2 is here too, but its
// driver needs cgo: it is linked in only with -tags db2 (db2.go).

// ExtraSQLEngine is one of these engines.
type ExtraSQLEngine struct {
	ID    string // the driver name in the connection ("firebird")
	Title string // the name in the add picker and on the tree
	Port  int    // the default port
	User  string // the usual admin user, filled in by the add flow
	Hint  string // the add picker hint
	// DBLabel is the add flow's question for Conn.Database; DBOptional
	// lets it stay empty.
	DBLabel    string
	DBOptional bool
}

var extraSQLEngines = []ExtraSQLEngine{
	{ID: "firebird", Title: "Firebird", Port: 3050, User: "SYSDBA", Hint: "default port 3050; the database is a path or alias on the server",
		DBLabel: "Database path or alias on the server:"},
	{ID: "vertica", Title: "Vertica", Port: 5433, User: "dbadmin", Hint: "default port 5433", DBLabel: "Database:"},
	{ID: "exasol", Title: "Exasol", Port: 8563, User: "sys", Hint: "default port 8563; TLS on by default",
		DBLabel: "Schema to open (empty: none):", DBOptional: true},
	{ID: "db2", Title: "IBM Db2", Port: 50000, User: "db2inst1", Hint: db2Hint, DBLabel: "Database name:"},
	{ID: "hana", Title: "SAP HANA", Port: 39017, User: "SYSTEM", Hint: "default port 39017 (tenant 3NN15/3NN41 also)",
		DBLabel: "Default schema (empty: the user's schema):", DBOptional: true},
}

// ExtraSQLEngineTitle is the name of an engine ("" for another driver).
func ExtraSQLEngineTitle(id string) string {
	e, _ := ExtraSQLEngineByID(id)
	return e.Title
}

// ExtraSQLEngines lists the engines in menu order.
func ExtraSQLEngines() []ExtraSQLEngine { return append([]ExtraSQLEngine(nil), extraSQLEngines...) }

// ExtraSQLEngineByID finds an engine by its driver name (aliases too).
func ExtraSQLEngineByID(id string) (ExtraSQLEngine, bool) {
	id = normDriver(strings.ToLower(id))
	for _, e := range extraSQLEngines {
		if e.ID == id {
			return e, true
		}
	}
	return ExtraSQLEngine{}, false
}

// IsExtraSQL reports one of these engines.
func IsExtraSQL(driver string) bool {
	_, ok := ExtraSQLEngineByID(driver)
	return ok
}

// extraSQLDialect is the dialect of an engine (nil for the others).
func extraSQLDialect(driver string) Dialect {
	switch normDriver(driver) {
	case "firebird":
		return FirebirdDialect{}
	case "vertica":
		return VerticaDialect{}
	case "exasol":
		return ExasolDialect{}
	case "hana":
		return HANADialect{}
	case "db2":
		return Db2Dialect{}
	}
	return nil
}

// extraHostPort is the address of c with the engine port by default.
func extraHostPort(c Conn) (string, int) {
	port := c.Port
	if port == 0 {
		if e, ok := ExtraSQLEngineByID(c.Driver); ok {
			port = e.Port
		}
	}
	return c.Host, port
}

// FirebirdDSN is the firebirdsql DSN of c: firebird://user:pass@host:port/
// path, where the database is a server path (/var/lib/…/db.fdb) or an
// alias (employee). Firebird has no TLS: wire_crypt (on by default in
// Firebird 3+) encrypts the link; c.Params are added as options (role,
// charset, wire_crypt, auth_plugin_name, …).
func FirebirdDSN(c Conn, password string) string {
	host, port := extraHostPort(c)
	u := &url.URL{Scheme: "firebird", Host: net.JoinHostPort(host, strconv.Itoa(port))}
	switch {
	case c.User != "" && password != "":
		u.User = url.UserPassword(c.User, password)
	case c.User != "":
		u.User = url.User(c.User)
	}
	db := c.Database
	if !strings.HasPrefix(db, "/") {
		db = "/" + db
	}
	u.Path = db
	q := url.Values{}
	for k, v := range c.Params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// VerticaDSN is the vertica-sql-go URL of c: vertica://user:pass@host:
// port/database?tlsmode=…. tlsName is a TLS config registered with the
// driver ("" = the mode from c.Params, else none).
func VerticaDSN(c Conn, password, tlsName string) string {
	host, port := extraHostPort(c)
	u := &url.URL{Scheme: "vertica", Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: "/" + c.Database}
	switch {
	case c.User != "" && password != "":
		u.User = url.UserPassword(c.User, password)
	case c.User != "":
		u.User = url.User(c.User)
	}
	q := url.Values{}
	for k, v := range c.Params {
		q.Set(k, v)
	}
	if tlsName != "" {
		q.Set("tlsmode", tlsName)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// exasolEscape escapes the separator of an Exasol DSN value.
func exasolEscape(s string) string { return strings.ReplaceAll(s, ";", `\;`) }

// ExasolDSN is the Exasol DSN of c: exa:host:port;user=…;password=…;
// schema=…. c.Database is the schema to open. With an ssl block, mode
// disable turns encryption off, require encrypts without checking the
// server, verify-ca / verify-full check it (the driver default). c.Params
// are added as they are (certificatefingerprint, fetchsize, …).
func ExasolDSN(c Conn, password string) (string, error) {
	host, port := extraHostPort(c)
	parts := []string{"exa:" + host + ":" + strconv.Itoa(port)}
	add := func(k, v string) { parts = append(parts, k+"="+exasolEscape(v)) }
	if c.User != "" {
		add("user", c.User)
	}
	if password != "" {
		add("password", password)
	}
	if c.Database != "" {
		add("schema", c.Database)
	}
	if c.SSL != nil {
		mode, err := EffectiveSSLMode(c.SSL)
		if err != nil {
			return "", err
		}
		if c.SSL.CA != "" || c.SSL.Cert != "" {
			return "", errors.New("Exasol checks the server by params.certificatefingerprint, not by a CA or client certificate file")
		}
		switch mode {
		case SSLDisable:
			add("encryption", "0")
		case SSLRequire:
			add("encryption", "1")
			add("validateservercertificate", "0")
		default:
			add("encryption", "1")
			add("validateservercertificate", "1")
		}
	}
	keys := make([]string, 0, len(c.Params))
	for k := range c.Params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		add(k, c.Params[k])
	}
	return strings.Join(parts, ";"), nil
}

// HANAConfig is the go-hdb connector config of c: host:port, the tenant
// from params.databaseName, the default schema from c.Database, TLS from
// c.SSL and the SSH tunnel as the dialer.
func HANAConfig(c Conn, password string) (*hdb.ConnectorConfig, error) {
	host, port := extraHostPort(c)
	cfg := hdb.NewConnectorConfig()
	cfg.Host = net.JoinHostPort(host, strconv.Itoa(port))
	cfg.Username, cfg.Password = c.User, password
	cfg.DefaultSchema = c.Database
	for k, v := range c.Params {
		switch k {
		case "databaseName":
			cfg.DatabaseName = v
		case "defaultSchema":
			cfg.DefaultSchema = v
		case "locale":
			cfg.Locale = v
		default:
			return nil, fmt.Errorf("unknown SAP HANA parameter %q (use databaseName, defaultSchema or locale)", sanitizeLine(k))
		}
	}
	if c.SSL != nil {
		t, err := TLSConfig(c.SSL, host)
		if err != nil {
			return nil, err
		}
		cfg.TLSConfig = t
	}
	if c.Dial != nil {
		cfg.Dialer = hanaDialer(c.Dial)
	}
	return cfg, nil
}

// hanaDialer sends go-hdb connections through the SSH tunnel.
type hanaDialer DialFunc

func (d hanaDialer) DialContext(ctx context.Context, address string, _ dial.DialerOptions) (net.Conn, error) {
	return d(ctx, "tcp", address)
}

// openExtraSQLDB opens the pool of one of these engines. A driver that
// dials by itself goes through a local relay over the SSH tunnel; release
// closes the relay. Start SQL runs on every new connection.
func openExtraSQLDB(conn Conn, password string) (*sql.DB, func(), error) {
	noop := func() {}
	engine, _ := ExtraSQLEngineByID(conn.Driver)
	release := noop
	relayed := conn
	if conn.Dial != nil && engine.ID != "hana" {
		host, port := extraHostPort(conn)
		addr, stop, err := localRelay(conn.Dial, net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return nil, noop, fmt.Errorf("could not open the local end of the SSH tunnel: %s", sanitizeLine(errText(err)))
		}
		h, p, _ := net.SplitHostPort(addr)
		relayed.Host = h
		relayed.Port, _ = strconv.Atoi(p)
		release = stop
	}
	var connector driver.Connector
	var err error
	switch engine.ID {
	case "firebird":
		if conn.SSL != nil {
			if mode, e := EffectiveSSLMode(conn.SSL); e != nil || mode != SSLDisable {
				release()
				return nil, noop, errors.New("Firebird has no TLS: wire_crypt encrypts the link (on by default since Firebird 3); remove the ssl block or use an SSH tunnel")
			}
		}
		connector, err = dsnConnector("firebirdsql", FirebirdDSN(relayed, password))
	case "vertica":
		tlsName := ""
		if conn.SSL != nil {
			t, e := TLSConfig(conn.SSL, conn.Host) // the real host name, also through the relay
			if e != nil {
				release()
				return nil, noop, e
			}
			if t == nil {
				tlsName = "none"
			} else {
				tlsName = "termocode-" + conn.ID
				if conn.ID == "" {
					tlsName = "termocode"
				}
				_ = vertica.RegisterTLSConfig(tlsName, t)
			}
		}
		connector, err = dsnConnector("vertica", VerticaDSN(relayed, password, tlsName))
	case "exasol":
		dsn, e := ExasolDSN(relayed, password)
		if e != nil {
			release()
			return nil, noop, e
		}
		connector, err = dsnConnector("exasol", dsn)
	case "db2":
		connector, err = openDb2(relayed, password)
		if errors.Is(err, errDb2Missing) {
			release()
			return nil, noop, err
		}
	case "hana":
		cfg, e := HANAConfig(conn, password)
		if e != nil {
			return nil, noop, e
		}
		connector, err = hdb.NewConfigConnector(cfg)
	default:
		return nil, noop, fmt.Errorf("unsupported SQL driver %q", conn.Driver)
	}
	if err != nil {
		release()
		return nil, noop, fmt.Errorf("invalid %s connection settings: %s", engine.Title, Redact(err.Error()))
	}
	if stmts := connStartSQL(conn); len(stmts) > 0 {
		connector = startSQLConnector{Connector: connector, stmts: stmts}
	}
	return sql.OpenDB(connector), release, nil
}

// dsnConnector is the connector of a registered driver for dsn.
func dsnConnector(name, dsn string) (driver.Connector, error) {
	db, err := sql.Open(name, "")
	if err != nil {
		return nil, err
	}
	drv := db.Driver()
	_ = db.Close()
	if dc, ok := drv.(driver.DriverContext); ok {
		return dc.OpenConnector(dsn)
	}
	return plainConnector{drv: drv, dsn: dsn}, nil
}

// plainConnector opens a driver that has no OpenConnector.
type plainConnector struct {
	drv driver.Driver
	dsn string
}

func (c plainConnector) Connect(context.Context) (driver.Conn, error) { return c.drv.Open(c.dsn) }
func (c plainConnector) Driver() driver.Driver                        { return c.drv }

// startSQLConnector runs the connection's start SQL on every new
// connection (DBX-28) for drivers without a connect hook.
type startSQLConnector struct {
	driver.Connector
	stmts []string
}

func (c startSQLConnector) Connect(ctx context.Context) (driver.Conn, error) {
	dc, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	for _, q := range c.stmts {
		if err := execDriverConn(ctx, dc, q); err != nil {
			_ = dc.Close()
			return nil, fmt.Errorf("start SQL %q: %s", sanitizeLine(firstLine(q)), Redact(err.Error()))
		}
	}
	return dc, nil
}

// execDriverConn runs q on a raw driver connection.
func execDriverConn(ctx context.Context, dc driver.Conn, q string) error {
	if ex, ok := dc.(driver.ExecerContext); ok {
		_, err := ex.ExecContext(ctx, q, nil)
		if !errors.Is(err, driver.ErrSkip) {
			return err
		}
	}
	st, err := dc.Prepare(q)
	if err != nil {
		return err
	}
	defer st.Close()
	if sc, ok := st.(driver.StmtExecContext); ok {
		_, err = sc.ExecContext(ctx, nil)
		return err
	}
	_, err = st.Exec(nil) //nolint:staticcheck // the old interface is the fallback
	return err
}

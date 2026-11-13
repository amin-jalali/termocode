package dbclient

import (
	"database/sql/driver"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// IBM Db2 (DBX-71). Its Go driver (github.com/ibmdb/go_ibm_db) calls IBM's
// CLI driver through cgo, so it is only linked in with -tags db2 (db2_on.go);
// the default build gets db2_off.go and a clear message. Everything else
// (DSN, dialect, session) is plain Go and always built.

// errDb2Missing is what a connection to Db2 says in the default build.
var errDb2Missing = errors.New("built without Db2: rebuild with -tags db2")

// db2OpenConnector opens the connector of a Db2 DSN. db2_on.go and
// db2_off.go give it; tests replace it with a fake.
var db2OpenConnector = db2DriverConnector

// db2Escape quotes a value of a Db2 CLI connection string: braces keep
// ';' safe, a '}' inside is doubled.
func db2Escape(s string) string {
	if !strings.ContainsAny(s, ";{}=") {
		return s
	}
	return "{" + strings.ReplaceAll(s, "}", "}}") + "}"
}

// Db2DSN is the CLI connection string of c: HOSTNAME=…;DATABASE=…;PORT=…;
// UID=…;PWD=…. A ssl block (any mode but disable) adds SECURITY=SSL and the
// CA file as SSLServerCertificate. c.Params are added as they are
// (CurrentSchema, ConnectTimeout, Authentication, …).
func Db2DSN(c Conn, password string) (string, error) {
	host, port := extraHostPort(c)
	parts := []string{}
	add := func(k, v string) { parts = append(parts, k+"="+v) }
	add("HOSTNAME", db2Escape(host))
	add("DATABASE", db2Escape(c.Database))
	add("PORT", strconv.Itoa(port))
	if c.User != "" {
		add("UID", db2Escape(c.User))
	}
	if password != "" {
		add("PWD", "{"+strings.ReplaceAll(password, "}", "}}")+"}")
	}
	if c.SSL != nil {
		mode, err := EffectiveSSLMode(c.SSL)
		if err != nil {
			return "", err
		}
		if mode != SSLDisable {
			add("SECURITY", "SSL")
			if c.SSL.CA != "" {
				add("SSLServerCertificate", db2Escape(c.SSL.CA))
			}
		}
	}
	keys := make([]string, 0, len(c.Params))
	for k := range c.Params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		add(k, db2Escape(c.Params[k]))
	}
	return strings.Join(parts, ";"), nil
}

// openDb2 is openExtraSQLDB's Db2 branch.
func openDb2(relayed Conn, password string) (driver.Connector, error) {
	dsn, err := Db2DSN(relayed, password)
	if err != nil {
		return nil, err
	}
	return db2OpenConnector(dsn)
}

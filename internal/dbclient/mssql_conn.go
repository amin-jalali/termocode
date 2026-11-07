package dbclient

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// SQL Server / Azure SQL connections (DBX-62). The dialect, DSN and the
// SHOWPLAN XML reader are here; the go-mssqldb driver is a new module the
// owner has to approve, so a session says so instead of connecting.

// errNoMSSQLDriver is the reason a SQL Server session does not open.
var errNoMSSQLDriver = errors.New("SQL Server support needs the go-mssqldb driver, which is not built in yet")

// DriverMissing is the error of a known engine whose driver is not built
// in; nil for the others.
func DriverMissing(driver string) error {
	switch normDriver(driver) {
	case "sqlserver":
		return errNoMSSQLDriver
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

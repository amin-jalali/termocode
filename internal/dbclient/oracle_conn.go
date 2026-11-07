package dbclient

import (
	"context"
	"database/sql/driver"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	go_ora "github.com/sijms/go-ora/v2"
)

// Oracle connections (DBX-63): tnsnames.ora, wallets and the go-ora URL.
// The pure-Go go-ora driver opens the session (openOracleDB).

// TNSEntry is one net service name of a tnsnames.ora file.
type TNSEntry struct {
	Alias   string // upper case, as Oracle compares them
	Host    string
	Port    int
	Service string // SERVICE_NAME
	SID     string
	TLS     bool // PROTOCOL = TCPS
}

var tnsKey = regexp.MustCompile(`(?i)\(\s*(HOST|PORT|SERVICE_NAME|SID|PROTOCOL)\s*=\s*([^()\s]+)\s*\)`)

// ParseTNSNames reads the entries of a tnsnames.ora text. An entry whose
// parentheses do not close is skipped; "A, B = (…)" gives two entries.
func ParseTNSNames(text string) []TNSEntry {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	s := b.String()
	var out []TNSEntry
	for len(s) > 0 {
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		names := s[:eq]
		rest := strings.TrimLeft(s[eq+1:], " \t\r\n")
		if !strings.HasPrefix(rest, "(") {
			s = rest
			continue
		}
		depth, end := 0, -1
		for i, r := range rest {
			if r == '(' {
				depth++
			} else if r == ')' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end < 0 {
			break // unclosed: the rest is not an entry
		}
		body := rest[:end+1]
		s = rest[end+1:]
		e := TNSEntry{}
		seen := map[string]bool{}
		for _, m := range tnsKey.FindAllStringSubmatch(body, -1) {
			k := strings.ToUpper(m[1])
			if seen[k] {
				continue
			}
			seen[k] = true
			switch k {
			case "HOST":
				e.Host = m[2]
			case "PORT":
				e.Port, _ = strconv.Atoi(m[2])
			case "SERVICE_NAME":
				e.Service = m[2]
			case "SID":
				e.SID = m[2]
			case "PROTOCOL":
				e.TLS = strings.EqualFold(m[2], "TCPS")
			}
		}
		for _, n := range strings.Split(names, ",") {
			if n = strings.ToUpper(strings.TrimSpace(n)); n != "" && !strings.ContainsAny(n, "() \t\r\n") {
				x := e
				x.Alias = n
				out = append(out, x)
			}
		}
	}
	return out
}

// LookupTNS finds an alias (any case).
func LookupTNS(entries []TNSEntry, alias string) (TNSEntry, bool) {
	alias = strings.ToUpper(strings.TrimSpace(alias))
	for _, e := range entries {
		if e.Alias == alias {
			return e, true
		}
	}
	return TNSEntry{}, false
}

// TNSNamesPath is the tnsnames.ora to read: in dir (a wallet folder),
// else in $TNS_ADMIN, else in $ORACLE_HOME/network/admin; "" if none.
func TNSNamesPath(dir string) string {
	dirs := []string{dir, os.Getenv("TNS_ADMIN")}
	if h := os.Getenv("ORACLE_HOME"); h != "" {
		dirs = append(dirs, filepath.Join(h, "network", "admin"))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(d, "tnsnames.ora")
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// OracleDSN is the go-ora URL of c: oracle://user:pass@host:port/service.
// Params: "tns" names an entry of tns (host, port, service or SID, TCPS),
// "wallet" is the wallet folder (WALLET=…, and TLS); the others are go-ora
// URL options as they are. The password is put in only here.
func OracleDSN(c Conn, password string, tns []TNSEntry) string {
	var u *url.URL
	if c.URL != "" {
		p, err := url.Parse(c.URL)
		if err != nil {
			return c.URL // the driver refuses it with its own reason
		}
		u = p
	} else {
		u = &url.URL{Host: c.Host, Path: "/" + c.Database}
		if c.Port > 0 {
			u.Host = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		}
	}
	u.Scheme = "oracle"
	q := u.Query()
	if alias := c.Params["tns"]; alias != "" {
		if e, ok := LookupTNS(tns, alias); ok {
			port := e.Port
			if port == 0 {
				port = 1521
			}
			u.Host = net.JoinHostPort(e.Host, strconv.Itoa(port))
			u.Path = "/" + e.Service
			if e.Service == "" && e.SID != "" {
				u.Path = ""
				q.Set("SID", e.SID)
			}
			if e.TLS {
				q.Set("SSL", "true")
			}
		}
	}
	for k, v := range c.Params {
		switch k {
		case "tns":
		case "wallet":
			q.Set("WALLET", v)
			q.Set("SSL", "true")
		default:
			q.Set(k, v)
		}
	}
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
	u.RawQuery = q.Encode()
	return u.String()
}

// oracleConnString is the go-ora URL of c. A "tns" alias is looked up in
// the tnsnames.ora of the wallet folder, $TNS_ADMIN or $ORACLE_HOME; an
// alias that is not there is an error, not a silent localhost.
func oracleConnString(c Conn, password string) (string, error) {
	var tns []TNSEntry
	if alias := strings.TrimSpace(c.Params["tns"]); alias != "" {
		path := TNSNamesPath(c.Params["wallet"])
		if path == "" {
			return "", fmt.Errorf("TNS name %s: no tnsnames.ora in the wallet folder, $TNS_ADMIN or $ORACLE_HOME/network/admin", strings.ToUpper(alias))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read tnsnames.ora: %w", err)
		}
		tns = ParseTNSNames(string(data))
		if _, ok := LookupTNS(tns, alias); !ok {
			return "", fmt.Errorf("TNS name %s is not in %s", strings.ToUpper(alias), path)
		}
	}
	if mode, err := EffectiveSSLMode(c.SSL); err == nil && c.SSL != nil && mode != SSLDisable && c.Params["SSL"] == "" {
		p := make(map[string]string, len(c.Params)+1)
		for k, v := range c.Params {
			p[k] = v
		}
		p["SSL"] = "true" // the "ssl" block turns TLS on (TCPS)
		c.Params = p
	}
	return OracleDSN(c, password, tns), nil
}

// oracleConnector is the go-ora connector of c: TLS from c.SSL, the SSH
// tunnel from c.Dial. Nothing connects until the first ping.
func oracleConnector(c Conn, password string) (driver.Connector, error) {
	dsn, err := oracleConnString(c, password)
	if err != nil {
		return nil, err
	}
	connector, ok := go_ora.NewConnector(dsn).(*go_ora.OracleConnector)
	if !ok {
		return nil, fmt.Errorf("the Oracle driver gave no connector")
	}
	if c.SSL != nil {
		tlsCfg, err := TLSConfig(c.SSL, c.Host)
		if err != nil {
			return nil, err
		}
		if tlsCfg != nil {
			connector.WithTLSConfig(tlsCfg)
		}
	}
	if c.Dial != nil {
		connector.Dialer(oracleDialer(c.Dial))
	}
	return connector, nil
}

// oracleDialer adapts the tunnel dial func to go-ora's dialer.
type oracleDialer func(ctx context.Context, network, addr string) (net.Conn, error)

func (d oracleDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

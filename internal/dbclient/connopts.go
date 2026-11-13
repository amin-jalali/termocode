package dbclient

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// ConnOptions are the per-connection session options saved with a
// connection ("options" in the config). The zero value changes nothing.
type ConnOptions struct {
	// InitSQL runs after the options below on every new session.
	InitSQL string `json:"initSQL,omitempty"`
	// Schema is the default schema: the PostgreSQL search_path (a comma
	// list) or the MySQL database to USE.
	Schema string `json:"schema,omitempty"`
	// TimeZone is the session time zone ("UTC", "Europe/Berlin", "+00:00").
	TimeZone string `json:"timeZone,omitempty"`
	// Isolation is the session isolation level ("read committed", ...).
	Isolation string `json:"isolation,omitempty"`
	// TxMode is the console's start transaction mode: "auto" or "manual".
	TxMode string `json:"txMode,omitempty"`
	// IdleMinutes closes the session after this many idle minutes (0: never).
	IdleMinutes int `json:"idleMinutes,omitempty"`
	// KeepAliveSeconds pings the connected session this often (0: never;
	// at least 5). AutoReconnect connects again with backoff when a ping
	// fails (DBX-21).
	KeepAliveSeconds int  `json:"keepAliveSeconds,omitempty"`
	AutoReconnect    bool `json:"autoReconnect,omitempty"`
	// Socket is a unix socket path (PostgreSQL: the socket directory).
	Socket string `json:"socket,omitempty"`
	// Databases and Schemas list the only ones shown (globs; empty: all).
	Databases []string `json:"databases,omitempty"`
	Schemas   []string `json:"schemas,omitempty"`
	// Objects filters tree objects by name: globs show, "!glob" hides.
	Objects []string `json:"objects,omitempty"`
	// ReadOnlyTables are "schema.table" or "table" globs that refuse edits.
	ReadOnlyTables []string `json:"readOnlyTables,omitempty"`
}

var isolationLevels = map[string]string{
	"read uncommitted": "READ UNCOMMITTED",
	"read committed":   "READ COMMITTED",
	"repeatable read":  "REPEATABLE READ",
	"serializable":     "SERIALIZABLE",
}

var timeZonePattern = lazyre.New(`^[A-Za-z0-9_/+:.-]{1,64}$`)

func normIsolation(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func schemaList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// Check reports the first invalid option for driver.
func (o ConnOptions) Check(driver string) error {
	driver = normDriver(driver)
	if o.Isolation != "" && isolationLevels[normIsolation(o.Isolation)] == "" {
		return fmt.Errorf("unknown isolation level")
	}
	if o.TxMode != "" && o.TxMode != "auto" && o.TxMode != "manual" {
		return fmt.Errorf("tx mode must be auto or manual")
	}
	if o.IdleMinutes < 0 {
		return fmt.Errorf("idle minutes must not be negative")
	}
	if o.KeepAliveSeconds < 0 {
		return fmt.Errorf("keep-alive seconds must not be negative")
	}
	if o.TimeZone != "" && !timeZonePattern.MatchString(o.TimeZone) {
		return fmt.Errorf("invalid time zone")
	}
	for _, s := range schemaList(o.Schema) {
		if s == "" || strings.ContainsRune(s, 0) {
			return fmt.Errorf("invalid default schema")
		}
	}
	if o.Socket != "" {
		if driver != "postgres" && driver != "mysql" {
			return fmt.Errorf("a unix socket needs postgres or mysql")
		}
		if !filepath.IsAbs(o.Socket) {
			return fmt.Errorf("socket path must be absolute")
		}
	}
	for _, list := range [][]string{o.Databases, o.Schemas, o.Objects, o.ReadOnlyTables} {
		for _, p := range list {
			if _, err := path.Match(strings.TrimPrefix(p, "!"), ""); err != nil || strings.TrimPrefix(p, "!") == "" {
				return fmt.Errorf("invalid filter pattern %q", p)
			}
		}
	}
	if strings.TrimSpace(o.InitSQL) != "" && driver != "redis" {
		if _, err := Split(o.InitSQL, driver); err != nil {
			return fmt.Errorf("init SQL: %v", err)
		}
	}
	return nil
}

// StartSQL is the SQL that a new session runs first: schema, time zone,
// isolation level, then the init SQL statements. Values are checked by
// Check; identifiers are quoted by the dialect.
func (o ConnOptions) StartSQL(driver string) []string {
	driver = normDriver(driver)
	d := DialectFor(driver)
	if d == nil {
		return nil
	}
	var out []string
	level := isolationLevels[normIsolation(o.Isolation)]
	switch driver {
	case "postgres":
		if list := schemaList(o.Schema); len(list) > 0 {
			q := make([]string, len(list))
			for i, s := range list {
				q[i] = d.QuoteIdent(s)
			}
			out = append(out, "SET search_path TO "+strings.Join(q, ", "))
		}
		if o.TimeZone != "" {
			out = append(out, "SET TIME ZONE '"+o.TimeZone+"'")
		}
		if level != "" {
			out = append(out, "SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL "+level)
		}
	case "mysql":
		if list := schemaList(o.Schema); len(list) > 0 {
			out = append(out, "USE "+d.QuoteIdent(list[0]))
		}
		if o.TimeZone != "" {
			out = append(out, "SET time_zone = '"+o.TimeZone+"'")
		}
		if level != "" {
			out = append(out, "SET SESSION TRANSACTION ISOLATION LEVEL "+level)
		}
	case "sqlserver":
		// The default schema belongs to the database user in T-SQL; only
		// the isolation level is a session setting.
		if level != "" {
			out = append(out, "SET TRANSACTION ISOLATION LEVEL "+level)
		}
	case "vertica": // DBX-71
		if list := schemaList(o.Schema); len(list) > 0 {
			q := make([]string, len(list))
			for i, s := range list {
				q[i] = d.QuoteIdent(s)
			}
			out = append(out, "SET SEARCH_PATH TO "+strings.Join(q, ", "))
		}
		if o.TimeZone != "" {
			out = append(out, "SET TIME ZONE TO '"+o.TimeZone+"'")
		}
		if level != "" {
			out = append(out, "SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL "+level)
		}
	case "exasol":
		if list := schemaList(o.Schema); len(list) > 0 {
			out = append(out, "OPEN SCHEMA "+d.QuoteIdent(list[0]))
		}
		if o.TimeZone != "" {
			out = append(out, "ALTER SESSION SET TIME_ZONE = '"+o.TimeZone+"'")
		}
	case "db2":
		if list := schemaList(o.Schema); len(list) > 0 {
			out = append(out, "SET SCHEMA "+d.QuoteIdent(list[0]))
		}
	case "hana":
		if list := schemaList(o.Schema); len(list) > 0 {
			out = append(out, "SET SCHEMA "+d.QuoteIdent(list[0]))
		}
	case "firebird":
		if o.TimeZone != "" {
			out = append(out, "SET TIME ZONE '"+o.TimeZone+"'") // Firebird 4+
		}
	}
	if strings.TrimSpace(o.InitSQL) != "" {
		stmts, err := Split(o.InitSQL, driver)
		if err != nil {
			return out
		}
		for _, s := range stmts {
			s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";"))
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// IdleTimeout is how long an idle session stays open (0: forever).
func (o *ConnOptions) IdleTimeout() time.Duration {
	if o == nil || o.IdleMinutes <= 0 {
		return 0
	}
	return time.Duration(o.IdleMinutes) * time.Minute
}

func matchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return ok
		}
	}
	return false
}

// ShowDatabase reports whether the database is listed in the tree.
func (o *ConnOptions) ShowDatabase(name string) bool {
	return o == nil || len(o.Databases) == 0 || matchAny(o.Databases, name)
}

// ShowSchema reports whether the schema is listed in the tree.
func (o *ConnOptions) ShowSchema(name string) bool {
	return o == nil || len(o.Schemas) == 0 || matchAny(o.Schemas, name)
}

// ShowObject reports whether a table, view or routine is listed: a "!"
// pattern hides it; with any plain pattern only matching names show.
func (o *ConnOptions) ShowObject(name string) bool {
	if o == nil {
		return true
	}
	var show []string
	for _, p := range o.Objects {
		if strings.HasPrefix(p, "!") {
			if ok, _ := path.Match(p[1:], name); ok {
				return false
			}
			continue
		}
		show = append(show, p)
	}
	return len(show) == 0 || matchAny(show, name)
}

// TableReadOnly reports whether edits to schema.table are refused. A
// pattern without a dot matches the table in any schema.
func (o *ConnOptions) TableReadOnly(schema, table string) bool {
	if o == nil {
		return false
	}
	for _, p := range o.ReadOnlyTables {
		name := table
		if strings.Contains(p, ".") {
			name = schema + "." + table
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(l); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return l
}

// connStartSQL is the start SQL of conn (nil without options).
func connStartSQL(conn Conn) []string {
	if conn.Options == nil {
		return nil
	}
	return conn.Options.StartSQL(conn.Driver)
}

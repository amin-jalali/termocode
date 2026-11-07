package dbclient

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// PGPreset is a database of the PostgreSQL family that speaks the PG
// wire protocol: it connects with the postgres driver and only changes
// defaults and what the tree and console offer (DBX-60).
type PGPreset struct {
	ID    string
	Label string
	Port  int // default port
	// RequireSSL: the hosted service only takes TLS; the DSN asks for
	// sslmode=require unless the user chose a mode.
	RequireSSL bool
	// Schemes are URL schemes besides postgres:// (cockroachdb://).
	Schemes []string
	// Hosts are the host name suffixes of the hosted service.
	Hosts []string
	// Version is text that SELECT version() holds on this server.
	Version string
	Quirks  PGQuirks
}

// PGQuirks are the dialect and catalog differences of a preset.
type PGQuirks struct {
	NoExplainAnalyze bool // EXPLAIN only, no ANALYZE
	NoSequences      bool // no CREATE SEQUENCE (IDENTITY columns only)
	NoRoutineDDL     bool // no pg_get_functiondef
	NoMatViews       bool // no materialized views in pg_matviews
	Hypertables      bool // TimescaleDB hypertables as tree nodes
	Distributed      bool // tables are spread over nodes (Citus, Greenplum)
}

var pgPresets = []PGPreset{
	{ID: "cockroachdb", Label: "CockroachDB", Port: 26257, Schemes: []string{"cockroachdb", "cockroach", "crdb"},
		Hosts: []string{"cockroachlabs.cloud"}, Version: "CockroachDB",
		Quirks: PGQuirks{NoRoutineDDL: true}},
	{ID: "yugabytedb", Label: "YugabyteDB", Port: 5433, Schemes: []string{"yugabytedb", "yugabyte"},
		Hosts: []string{"yugabyte.cloud", "ybdb.io"}, Version: "-YB-"},
	{ID: "timescaledb", Label: "TimescaleDB", Port: 5432, Schemes: []string{"timescaledb", "timescale"},
		Hosts: []string{"tsdb.cloud.timescale.com"}, Quirks: PGQuirks{Hypertables: true}},
	{ID: "supabase", Label: "Supabase", Port: 5432, RequireSSL: true, Schemes: []string{"supabase"},
		Hosts: []string{"supabase.co", "supabase.com"}},
	{ID: "neon", Label: "Neon", Port: 5432, RequireSSL: true, Schemes: []string{"neon"},
		Hosts: []string{"neon.tech"}},
	{ID: "redshift", Label: "Amazon Redshift", Port: 5439, RequireSSL: true, Schemes: []string{"redshift"},
		Hosts: []string{"redshift.amazonaws.com", "redshift-serverless.amazonaws.com"}, Version: "Redshift",
		Quirks: PGQuirks{NoExplainAnalyze: true, NoSequences: true, NoRoutineDDL: true, NoMatViews: true}},
	{ID: "greenplum", Label: "Greenplum", Port: 5432, Schemes: []string{"greenplum"}, Version: "Greenplum",
		Quirks: PGQuirks{Distributed: true}},
	{ID: "alloydb", Label: "AlloyDB", Port: 5432, Schemes: []string{"alloydb"}},
	{ID: "citus", Label: "Citus", Port: 5432, Schemes: []string{"citus"},
		Hosts: []string{"postgres.cosmos.azure.com"}, Quirks: PGQuirks{Distributed: true}},
}

// PGPresets lists the PostgreSQL family presets in menu order.
func PGPresets() []PGPreset { return append([]PGPreset(nil), pgPresets...) }

// PGPresetByID finds a preset by its id ("cockroachdb").
func PGPresetByID(id string) (PGPreset, bool) {
	id = strings.ToLower(id)
	for _, p := range pgPresets {
		if p.ID == id {
			return p, true
		}
	}
	return PGPreset{}, false
}

// pgPresetForScheme finds the preset of a URL scheme or driver name
// (its id or one of its schemes).
func pgPresetForScheme(s string) (PGPreset, bool) {
	s = strings.ToLower(s)
	for _, p := range pgPresets {
		if p.ID == s {
			return p, true
		}
		for _, x := range p.Schemes {
			if x == s {
				return p, true
			}
		}
	}
	return PGPreset{}, false
}

// DetectPGPreset names the preset of a server from SELECT version()
// (wins) or the host name of a hosted service; "" = plain PostgreSQL.
func DetectPGPreset(host, version string) string {
	for _, p := range pgPresets {
		if p.Version != "" && strings.Contains(version, p.Version) {
			return p.ID
		}
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, p := range pgPresets {
		for _, h := range p.Hosts {
			if host == h || strings.HasSuffix(host, "."+h) {
				return p.ID
			}
		}
	}
	return ""
}

// PGPreset is the preset of a postgres connection: the one it names,
// else the one its host shows.
func (c Conn) PGPreset() (PGPreset, bool) {
	if normDriver(c.Driver) != "postgres" {
		return PGPreset{}, false
	}
	if c.Preset != "" {
		return PGPresetByID(c.Preset)
	}
	host := c.Host
	if host == "" && c.URL != "" {
		if u, err := url.Parse(c.URL); err == nil {
			if p, ok := pgPresetForScheme(u.Scheme); ok {
				return p, true
			}
			host = u.Hostname()
		}
	}
	return PGPresetByID(DetectPGPreset(host, ""))
}

// EngineLabel names the database engine for the UI.
func (c Conn) EngineLabel() string {
	if p, ok := c.PGPreset(); ok {
		return p.Label
	}
	switch normDriver(c.Driver) {
	case "postgres":
		return "PostgreSQL"
	case "mysql":
		return "MySQL"
	case "sqlite":
		return "SQLite"
	case "redis":
		return "Redis"
	}
	return c.Driver
}

// presetDSN fixes a postgres URL for the preset: the postgres scheme,
// the preset port when the URL has none, and sslmode=require for a
// hosted service that needs TLS (unless a mode or an ssl block is set).
func (c Conn) presetDSN(u *url.URL) {
	p, ok := c.PGPreset()
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		if sp, sok := pgPresetForScheme(u.Scheme); sok && !ok {
			p, ok = sp, true
		}
		u.Scheme = "postgres"
	}
	if !ok {
		return
	}
	if u.Port() == "" && u.Hostname() != "" && p.Port != 5432 {
		u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(p.Port))
	}
	if p.RequireSSL && c.SSL == nil {
		q := u.Query()
		if q.Get("sslmode") == "" {
			q.Set("sslmode", "require")
			u.RawQuery = q.Encode()
		}
	}
}

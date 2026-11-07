package dbclient

import (
	"net/url"
	"testing"
)

// DBX-60: the PostgreSQL family are named presets on the PG driver.
func TestPGPresetsList(t *testing.T) {
	want := []string{"cockroachdb", "yugabytedb", "timescaledb", "supabase", "neon", "redshift", "greenplum", "alloydb", "citus"}
	got := PGPresets()
	if len(got) != len(want) {
		t.Fatalf("%d presets, want %d", len(got), len(want))
	}
	for i, p := range got {
		if p.ID != want[i] || p.Label == "" || p.Port == 0 {
			t.Errorf("preset %d = %+v, want id %q with a label and a port", i, p, want[i])
		}
	}
	for id, port := range map[string]int{"cockroachdb": 26257, "yugabytedb": 5433, "redshift": 5439, "neon": 5432} {
		if p, ok := PGPresetByID(id); !ok || p.Port != port {
			t.Errorf("%s port %d ok %v, want %d", id, p.Port, ok, port)
		}
	}
	if _, ok := PGPresetByID("postgres"); ok {
		t.Error("plain postgres is not a preset")
	}
	if p, _ := PGPresetByID("timescaledb"); !p.Quirks.Hypertables {
		t.Error("TimescaleDB lists hypertables in the tree")
	}
	if p, _ := PGPresetByID("redshift"); !p.Quirks.NoExplainAnalyze || !p.Quirks.NoSequences || !p.Quirks.NoRoutineDDL || !p.RequireSSL {
		t.Errorf("Redshift quirks %+v ssl %v", p.Quirks, p.RequireSSL)
	}
}

func TestParseURLPresetSchemes(t *testing.T) {
	cases := []struct {
		url, preset string
		port        int
	}{
		{"cockroachdb://root@localhost/defaultdb", "cockroachdb", 26257},
		{"cockroach://root@db:26000/bank", "cockroachdb", 26000},
		{"yugabytedb://yugabyte@yb/yugabyte", "yugabytedb", 5433},
		{"redshift://admin@wh.abc.us-east-1.redshift.amazonaws.com/dev", "redshift", 5439},
		{"timescaledb://u@ts/metrics", "timescaledb", 5432},
		{"postgres://u@db.abcd.supabase.co/postgres", "supabase", 5432},
		{"postgresql://u@ep-cool-1.eu-central-1.aws.neon.tech/neondb", "neon", 5432},
		{"postgres://u@localhost/app", "", 5432},
	}
	for _, tc := range cases {
		c, err := ParseURL(tc.url)
		if err != nil {
			t.Fatalf("%s: %v", tc.url, err)
		}
		if c.Driver != "postgres" || c.Preset != tc.preset || c.Port != tc.port {
			t.Errorf("%s: driver %q preset %q port %d, want postgres %q %d", tc.url, c.Driver, c.Preset, c.Port, tc.preset, tc.port)
		}
	}
}

func TestPGPresetDSN(t *testing.T) {
	// A preset scheme is sent to the driver as postgres://.
	c, _ := ParseURL("cockroachdb://root@localhost/defaultdb?application_name=x")
	u, err := url.Parse(c.DSN(""))
	if err != nil || u.Scheme != "postgres" || u.Port() != "26257" || u.Query().Get("application_name") != "x" {
		t.Fatalf("dsn %q", c.DSN(""))
	}
	if u.Query().Get("sslmode") != "" {
		t.Fatalf("a local CockroachDB keeps its own TLS choice: %q", c.DSN(""))
	}
	// A hosted service that needs TLS gets sslmode=require unless the
	// user chose a mode.
	c = Conn{Driver: "postgres", Preset: "neon", Host: "ep-1.neon.tech", Port: 5432, User: "u", Database: "neondb"}
	if u, _ = url.Parse(c.DSN("pw")); u.Query().Get("sslmode") != "require" {
		t.Fatalf("neon dsn %q", c.DSN("pw"))
	}
	c, _ = ParseURL("postgres://u@db.x.supabase.co/postgres?sslmode=verify-full")
	if u, _ = url.Parse(c.DSN("")); u.Query().Get("sslmode") != "verify-full" {
		t.Fatalf("the user's sslmode wins: %q", c.DSN(""))
	}
	c = Conn{Driver: "postgres", Preset: "redshift", Host: "h", Port: 5439, SSL: &SSLConfig{Mode: "disable"}}
	if u, _ = url.Parse(c.DSN("")); u.Query().Get("sslmode") != "" {
		t.Fatalf("an ssl block is applied elsewhere and wins: %q", c.DSN(""))
	}
}

func TestDetectPGPreset(t *testing.T) {
	cases := []struct{ host, version, want string }{
		{"", "CockroachDB CCL v23.2.4 (x86_64-pc-linux-gnu)", "cockroachdb"},
		{"", "PostgreSQL 11.2-YB-2.20.1.0-b0 on x86_64-pc-linux-gnu", "yugabytedb"},
		{"", "PostgreSQL 8.0.2 on i686-pc-linux-gnu, compiled by GCC gcc (GCC) 3.4.2 20041017 (Red Hat 3.4.2-6.fc3), Redshift 1.0.77467", "redshift"},
		{"", "PostgreSQL 12.12 (Greenplum Database 7.0.0 build commit:abc)", "greenplum"},
		{"db.x.supabase.co", "PostgreSQL 15.1", "supabase"},
		{"aws-0-eu-central-1.pooler.supabase.com", "", "supabase"},
		{"c-grp.postgres.cosmos.azure.com", "", "citus"},
		{"svc.tsdb.cloud.timescale.com", "", "timescaledb"},
		{"free-tier.gcp-us-central1.cockroachlabs.cloud", "", "cockroachdb"},
		{"localhost", "PostgreSQL 16.2 on x86_64", ""},
		{"evil-supabase.co.example.com", "", ""},
	}
	for _, tc := range cases {
		if got := DetectPGPreset(tc.host, tc.version); got != tc.want {
			t.Errorf("DetectPGPreset(%q, %q) = %q, want %q", tc.host, tc.version, got, tc.want)
		}
	}
}

func TestParseConfigPreset(t *testing.T) {
	data := []byte(`{"connections": [
		{"id": "crdb", "driver": "cockroachdb", "host": "localhost"},
		{"id": "rs", "driver": "postgres", "preset": "redshift", "host": "h"},
		{"id": "bad", "driver": "postgres", "preset": "oracle", "host": "h"},
		{"id": "my", "driver": "mysql", "preset": "neon", "host": "h"}
	]}`)
	conns, err := ParseConfig(data, SourceUser, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c := conns[0]; c.Driver != "postgres" || c.Preset != "cockroachdb" || c.Port != 26257 || c.Problem != "" {
		t.Errorf("driver name as preset: %+v", c)
	}
	if c := conns[1]; c.Preset != "redshift" || c.Port != 5439 || c.Problem != "" {
		t.Errorf("preset field: %+v", c)
	}
	if conns[2].Problem != "unknown PostgreSQL preset" {
		t.Errorf("unknown preset problem %q", conns[2].Problem)
	}
	if conns[3].Problem != "preset needs the postgres driver" {
		t.Errorf("preset on mysql problem %q", conns[3].Problem)
	}
}

func TestConnEngineLabel(t *testing.T) {
	for _, tc := range []struct {
		c    Conn
		want string
	}{
		{Conn{Driver: "postgres"}, "PostgreSQL"},
		{Conn{Driver: "postgres", Preset: "cockroachdb"}, "CockroachDB"},
		{Conn{Driver: "postgres", Host: "ep-1.neon.tech"}, "Neon"},
		{Conn{Driver: "mysql"}, "MySQL"},
		{Conn{Driver: "sqlite"}, "SQLite"},
		{Conn{Driver: "redis"}, "Redis"},
	} {
		if got := tc.c.EngineLabel(); got != tc.want {
			t.Errorf("%+v label %q, want %q", tc.c, got, tc.want)
		}
	}
}

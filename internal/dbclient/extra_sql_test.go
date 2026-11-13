package dbclient

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SAP/go-hdb/driver/dial"
)

// DBX-71: Firebird, Vertica, Exasol and SAP HANA on the SQL session.

func TestExtraSQLEnginesKnownEverywhere(t *testing.T) {
	ports := map[string]int{"firebird": 3050, "vertica": 5433, "exasol": 8563, "hana": 39017, "db2": 50000}
	titles := map[string]string{"firebird": "Firebird", "vertica": "Vertica", "exasol": "Exasol", "hana": "SAP HANA", "db2": "IBM Db2"}
	if got := len(ExtraSQLEngines()); got != len(ports) {
		t.Fatalf("engines = %d", got)
	}
	for id, port := range ports {
		if !IsExtraSQL(id) || defaultPort(id) != port {
			t.Errorf("%s: extra=%v port=%d", id, IsExtraSQL(id), defaultPort(id))
		}
		if DialectFor(id) == nil || extraSQLDialect(id) == nil {
			t.Errorf("%s has no dialect", id)
		}
		if got := (Conn{Driver: id}).EngineLabel(); got != titles[id] {
			t.Errorf("%s label = %q", id, got)
		}
		if ServerVersionQuery(id) == "" {
			t.Errorf("%s has no version query", id)
		}
		if err := Validate(Conn{ID: "x", Driver: id, Host: "db", Port: port}); err != nil {
			t.Errorf("%s does not validate: %v", id, err)
		}
	}
	for alias, want := range map[string]string{"firebirdsql": "firebird", "hdb": "hana", "saphana": "hana", "exa": "exasol"} {
		if normDriver(alias) != want || !IsExtraSQL(alias) {
			t.Errorf("alias %s -> %s", alias, normDriver(alias))
		}
	}
	for _, other := range []string{"postgres", "mysql", "informix", "oracle", ""} {
		if IsExtraSQL(other) {
			t.Errorf("%q counted as an extra engine", other)
		}
	}
}

func TestFirebirdDSNPathAliasAndParams(t *testing.T) {
	c := Conn{Driver: "firebird", Host: "db", User: "SYSDBA", Database: "/var/lib/firebird/data/app.fdb", Params: map[string]string{"role": "RDB$ADMIN"}}
	got := FirebirdDSN(c, "p@ss")
	if !strings.HasPrefix(got, "firebird://SYSDBA:p%40ss@db:3050/var/lib/firebird/data/app.fdb?") {
		t.Fatalf("path dsn = %q", got)
	}
	if !strings.Contains(got, "role=RDB%24ADMIN") {
		t.Fatalf("params missing: %q", got)
	}
	c.Database, c.Port = "employee", 3051
	if got := FirebirdDSN(c, ""); got != "firebird://SYSDBA@db:3051/employee?role=RDB%24ADMIN" {
		t.Fatalf("alias dsn = %q", got)
	}
}

func TestVerticaDSNTLSName(t *testing.T) {
	c := Conn{Driver: "vertica", Host: "v", User: "dbadmin", Database: "vmart"}
	if got := VerticaDSN(c, "pw", ""); got != "vertica://dbadmin:pw@v:5433/vmart" {
		t.Fatalf("dsn = %q", got)
	}
	if got := VerticaDSN(c, "", "termocode-v"); !strings.HasSuffix(got, "?tlsmode=termocode-v") {
		t.Fatalf("tls dsn = %q", got)
	}
}

func TestExasolDSNEscapesAndSSLModes(t *testing.T) {
	c := Conn{Driver: "exasol", Host: "exa", User: "sys", Database: "RETAIL", Params: map[string]string{"fetchsize": "500"}}
	got, err := ExasolDSN(c, "a;b")
	if err != nil {
		t.Fatal(err)
	}
	if got != `exa:exa:8563;user=sys;password=a\;b;schema=RETAIL;fetchsize=500` {
		t.Fatalf("dsn = %q", got)
	}
	for mode, want := range map[string]string{"disable": "encryption=0", "require": "validateservercertificate=0", "verify-full": "validateservercertificate=1"} {
		c.SSL = &SSLConfig{Mode: mode}
		got, err := ExasolDSN(c, "")
		if err != nil || !strings.Contains(got, want) {
			t.Errorf("%s: %q %v", mode, got, err)
		}
	}
	c.SSL = &SSLConfig{Mode: "verify-ca", CA: "/ca.pem"}
	if _, err := ExasolDSN(c, ""); err == nil || !strings.Contains(err.Error(), "certificatefingerprint") {
		t.Fatalf("a CA file was taken: %v", err)
	}
}

func TestHANAConfigTenantTLSAndTunnel(t *testing.T) {
	c := Conn{Driver: "hana", Host: "h", User: "SYSTEM", Database: "APP", Params: map[string]string{"databaseName": "HXE"}}
	cfg, err := HANAConfig(c, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "h:39017" || cfg.DatabaseName != "HXE" || cfg.DefaultSchema != "APP" || cfg.Username != "SYSTEM" || cfg.Password != "pw" || cfg.TLSConfig != nil {
		t.Fatalf("cfg = %+v", cfg)
	}
	c.SSL = &SSLConfig{Mode: "verify-full"}
	dialed := ""
	c.Dial = func(_ context.Context, _, addr string) (net.Conn, error) { dialed = addr; return nil, io.EOF }
	cfg, err = HANAConfig(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSConfig == nil || cfg.TLSConfig.ServerName != "h" {
		t.Fatalf("tls = %+v", cfg.TLSConfig)
	}
	if _, ok := cfg.Dialer.(hanaDialer); !ok {
		t.Fatalf("dialer = %T", cfg.Dialer)
	}
	_, _ = cfg.Dialer.DialContext(context.Background(), "h:39017", dial.DialerOptions{})
	if dialed != "h:39017" {
		t.Fatalf("tunnel dialed %q", dialed)
	}
	c.Params = map[string]string{"bogus": "1"}
	if _, err := HANAConfig(c, ""); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("unknown parameter taken: %v", err)
	}
}

func TestParseURLExtraEngines(t *testing.T) {
	for raw, want := range map[string]Conn{
		"firebird://SYSDBA@db/employee":                {Driver: "firebird", Host: "db", Port: 3050, User: "SYSDBA", Database: "employee"},
		"firebird://SYSDBA@db:3051/var/lib/fb/app.fdb": {Driver: "firebird", Host: "db", Port: 3051, User: "SYSDBA", Database: "/var/lib/fb/app.fdb"},
		"vertica://dbadmin@v/vmart":                    {Driver: "vertica", Host: "v", Port: 5433, User: "dbadmin", Database: "vmart"},
		"exasol://sys@exa:8563/RETAIL":                 {Driver: "exasol", Host: "exa", Port: 8563, User: "sys", Database: "RETAIL"},
		"hdb://SYSTEM@h:39017?databaseName=HXE":        {Driver: "hana", Host: "h", Port: 39017, User: "SYSTEM"},
		"saphana://SYSTEM@h":                           {Driver: "hana", Host: "h", Port: 39017, User: "SYSTEM"},
	} {
		got, err := ParseURL(raw)
		if err != nil {
			t.Errorf("%s: %v", raw, err)
			continue
		}
		if got.Driver != want.Driver || got.Host != want.Host || got.Port != want.Port || got.User != want.User || got.Database != want.Database {
			t.Errorf("%s = %+v", raw, got)
		}
	}
	if c, _ := ParseURL("hdb://SYSTEM@h:39017?databaseName=HXE"); c.Params["databaseName"] != "HXE" {
		t.Errorf("tenant lost: %+v", c.Params)
	}
	if c, _ := ParseURL("vertica://dbadmin:secret@v/vmart"); c.Password != "secret" || strings.Contains(c.URL, "secret") {
		t.Errorf("password kept in the URL: %+v", c)
	}
}

// Every catalog query binds as many ? as Introspect passes arguments.
func TestExtraDialectCatalogArguments(t *testing.T) {
	want := map[string]int{"databases": 0, "schemas": 0, "roles": 0, "objects": 1, "routines": 1, "sequences": 1, "triggers": 1, "columns": 2, "keys": 2, "indexes": 2, "foreign_keys": 2}
	for _, id := range []string{"firebird", "vertica", "exasol", "hana"} {
		d := DialectFor(id)
		q := d.CatalogQueries()
		for _, key := range []string{"schemas", "objects", "columns"} {
			if q[key] == "" {
				t.Errorf("%s lacks %s", id, key)
			}
		}
		for key, query := range q {
			n, ok := want[key]
			if !ok {
				t.Errorf("%s: unexpected catalog key %s", id, key)
				continue
			}
			if got := strings.Count(query, "?"); got != n {
				t.Errorf("%s %s binds %d, Introspect passes %d", id, key, got, n)
			}
		}
		ddl, args := d.DDL("T")
		if strings.Count(ddl, "?") != 1 || len(args) != 1 || args[0] != "T" {
			t.Errorf("%s DDL = %q %v", id, ddl, args)
		}
		if _, ok := d.(currentSchemaQuerier); !ok {
			t.Errorf("%s has no current schema query", id)
		}
		if d.Placeholder(3) != "?" || d.QuoteIdent(`a"b`) != `"a""b"` || d.Limit(0, 0) != "" || !strings.Contains(d.Limit(10, 20), "10") {
			t.Errorf("%s basic syntax", id)
		}
	}
	if got := (FirebirdDialect{}).Limit(10, 20); got != " OFFSET 20 ROWS FETCH NEXT 10 ROWS ONLY" {
		t.Errorf("firebird limit = %q", got)
	}
	if (VerticaDialect{}).Explain("SELECT 1", true) != "PROFILE SELECT 1" || (VerticaDialect{}).Explain("SELECT 1", false) != "EXPLAIN SELECT 1" {
		t.Error("vertica explain")
	}
	for _, d := range []Dialect{FirebirdDialect{}, ExasolDialect{}, HANADialect{}} {
		if d.Explain("SELECT 1", false) != "" {
			t.Errorf("%T claims an EXPLAIN form", d)
		}
	}
	if (ExasolDialect{}).ReadOnlyBegin() != "" {
		t.Error("exasol has no read-only transaction")
	}
}

func TestExtraExplainWithoutSQLFormSaysSo(t *testing.T) {
	s := newSQLSession(nil, FirebirdDialect{}, "firebird", Conn{Driver: "firebird"}, false)
	r := s.Explain(context.Background(), "SELECT 1 FROM RDB$DATABASE", false)
	if r.Err == nil || !strings.Contains(r.Err.Error(), "Firebird has no EXPLAIN") {
		t.Fatalf("explain = %v", r.Err)
	}
}

func TestExtraStartSQLPerEngine(t *testing.T) {
	o := ConnOptions{Schema: "app, util", TimeZone: "UTC", Isolation: "read committed", InitSQL: "SELECT 1;"}
	got := map[string]string{}
	for _, id := range []string{"firebird", "vertica", "exasol", "hana"} {
		got[id] = strings.Join(o.StartSQL(id), " | ")
	}
	if got["vertica"] != `SET SEARCH_PATH TO "app", "util" | SET TIME ZONE TO 'UTC' | SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL READ COMMITTED | SELECT 1` {
		t.Errorf("vertica = %q", got["vertica"])
	}
	if got["exasol"] != `OPEN SCHEMA "app" | ALTER SESSION SET TIME_ZONE = 'UTC' | SELECT 1` {
		t.Errorf("exasol = %q", got["exasol"])
	}
	if got["hana"] != `SET SCHEMA "app" | SELECT 1` {
		t.Errorf("hana = %q", got["hana"])
	}
	if got["firebird"] != `SET TIME ZONE 'UTC' | SELECT 1` {
		t.Errorf("firebird = %q", got["firebird"])
	}
}

func TestOpenExtraSQLDBRefusesFirebirdTLS(t *testing.T) {
	c := Conn{Driver: "firebird", Host: "db", User: "SYSDBA", Database: "employee", SSL: &SSLConfig{Mode: "require"}}
	if _, _, err := openSQLDB(c, ""); err == nil || !strings.Contains(err.Error(), "wire_crypt") {
		t.Fatalf("err = %v", err)
	}
	c.SSL = &SSLConfig{Mode: "disable"}
	db, release, err := openSQLDB(c, "")
	if err != nil {
		t.Fatalf("disable refused: %v", err)
	}
	_ = db.Close()
	release()
}

func TestOpenExtraSQLDBRelaysThroughTheTunnel(t *testing.T) {
	dialed := make(chan string, 8)
	c := Conn{Driver: "vertica", Host: "10.0.0.5", User: "dbadmin", Database: "vmart",
		Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			select {
			case dialed <- addr:
			default:
			}
			return nil, io.EOF
		}}
	db, release, err := openSQLDB(c, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = db.PingContext(ctx) // the relay accepts, the tunnel refuses
	_ = db.Close()
	release()
	select {
	case got := <-dialed:
		if got != "10.0.0.5:5433" {
			t.Fatalf("tunnel dialed %s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the driver did not go through the tunnel")
	}
}

func TestLocalRelayPipesAndStops(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }()
		}
	}()
	var mu sync.Mutex
	var targets []string
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		targets = append(targets, addr)
		mu.Unlock()
		var d net.Dialer
		return d.DialContext(ctx, network, ln.Addr().String())
	}
	addr, stop, err := localRelay(dialer, "db.internal:3050")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("relay listens on %s", addr)
	}
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo = %q %v", buf, err)
	}
	stop()
	if _, err := c.Read(buf); err == nil {
		t.Fatal("an open pipe outlived stop")
	}
	_ = c.Close()
	mu.Lock()
	if len(targets) != 1 || targets[0] != "db.internal:3050" {
		t.Fatalf("targets = %v", targets)
	}
	mu.Unlock()
	if c2, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		_ = c2.Close()
		t.Fatal("the relay still listens after stop")
	}
	stop() // twice is fine
}

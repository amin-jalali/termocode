package dbclient

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func TestStripCQLCommentsAndReadOnly(t *testing.T) {
	for in, want := range map[string]string{
		"SELECT 1 -- tail":           "SELECT 1  ",
		"SELECT 1 // tail\nFROM t":   "SELECT 1  FROM t",
		"SELECT /* c */ 1":           "SELECT   1",
		"SELECT '-- not' , \"//no\"": "SELECT '-- not' , \"//no\"",
		"SELECT 'it''s -- kept'":     "SELECT 'it''s -- kept'",
		"SELECT /* open":             "SELECT  ",
		"SELECT 'open -- x":          "SELECT 'open -- x",
	} {
		if got := stripCQLComments(in); got != want {
			t.Errorf("stripCQLComments(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]bool{
		"SELECT * FROM t": true, "desc table t": true, "DESCRIBE t;": true, "-- c\nSELECT 1": true,
		"INSERT INTO t": false, "SELECT 1; SELECT 2": false, "": false, "  ;": false, "TRUNCATE t": false,
	} {
		if got := CQLReadOnly(in); got != want {
			t.Errorf("CQLReadOnly(%q) = %v, want %v", in, got, want)
		}
	}
	u := gocql.TimeUUID()
	if c := cqlCell(u); c.Text != u.String() {
		t.Errorf("uuid cell = %+v", c)
	}
	if c := cqlCell(nil); !c.Null {
		t.Errorf("nil cell = %+v", c)
	}
}

func TestCassandraTLSRules(t *testing.T) {
	cfg, err := cassandraTLS(Conn{Host: "db.example", TLS: true})
	if err != nil || cfg == nil || cfg.ServerName != "db.example" {
		t.Errorf("tls flag = %+v %v", cfg, err)
	}
	if cfg, err := cassandraTLS(Conn{Host: "h", SSL: &SSLConfig{Mode: SSLDisable}}); err != nil || cfg != nil {
		t.Errorf("ssl disabled = %+v %v", cfg, err)
	}
	if _, err := cassandraTLS(Conn{Host: "h", TLS: true, SSL: &SSLConfig{Mode: SSLDisable}}); err == nil || !strings.Contains(err.Error(), "ssl mode is disable") {
		t.Errorf("tls against disable = %v", err)
	}
	if _, err := cassandraTLS(Conn{Host: "h", SSL: &SSLConfig{Mode: "nonsense"}}); err == nil {
		t.Error("an unknown ssl mode: want error")
	}
	if _, err := cassandraTLS(Conn{Host: "h", SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}); err == nil {
		t.Error("an unreadable CA: want error")
	}
}

func TestNewCassandraSessionFailsCleanly(t *testing.T) {
	ctx := context.Background()
	if _, err := NewCassandraSession(ctx, Conn{Driver: "cassandra", Host: "h", TLS: true, SSL: &SSLConfig{Mode: SSLDisable}}, ""); err == nil {
		t.Error("a TLS contradiction: want error before any connection")
	}
	dialed := false
	c := Conn{Driver: "cassandra", Host: "127.0.0.1", User: "u", Database: "ks", Dial: func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("refused")
	}}
	done := make(chan error, 1)
	go func() { _, err := NewCassandraSession(ctx, c, "pw"); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cassandra:") {
			t.Errorf("an unreachable cluster = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("connecting did not give up")
	}
	if !dialed {
		t.Error("the tunnel dial hook should be used")
	}
	if got, err := (cqlDialer(func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("x") })).DialContext(ctx, "tcp", "a"); got != nil || err == nil {
		t.Error("cqlDialer passes the call on")
	}
}

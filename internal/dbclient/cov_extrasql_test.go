package dbclient

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestOpenExtraSQLDBPerEngine(t *testing.T) {
	for _, drv := range []string{"firebird", "vertica", "exasol", "hana"} {
		db, release, err := openExtraSQLDB(Conn{ID: "c", Driver: drv, Host: "db.example", User: "u", Database: "d", Options: &ConnOptions{InitSQL: "SET x = 1"}}, "pw")
		if err != nil {
			t.Errorf("%s: %v", drv, err)
			continue
		}
		release()
		_ = db.Close()
	}
	if _, _, err := openExtraSQLDB(Conn{Driver: "nope"}, ""); err == nil || !strings.Contains(err.Error(), "unsupported SQL driver") {
		t.Errorf("unknown engine = %v", err)
	}
	if _, _, err := openExtraSQLDB(Conn{Driver: "firebird", Host: "h", SSL: &SSLConfig{Mode: "require"}}, ""); err == nil || !strings.Contains(err.Error(), "Firebird has no TLS") {
		t.Errorf("firebird with TLS = %v", err)
	}
	if db, release, err := openExtraSQLDB(Conn{Driver: "firebird", Host: "h", SSL: &SSLConfig{Mode: SSLDisable}}, ""); err != nil {
		t.Errorf("firebird with ssl disabled = %v", err)
	} else {
		release()
		_ = db.Close()
	}
	if _, _, err := openExtraSQLDB(Conn{Driver: "vertica", Host: "h", SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}, ""); err == nil {
		t.Error("vertica with an unreadable CA: want error")
	}
	for _, mode := range []string{SSLDisable, SSLRequire} {
		db, release, err := openExtraSQLDB(Conn{ID: "v", Driver: "vertica", Host: "h", SSL: &SSLConfig{Mode: mode}}, "")
		if err != nil {
			t.Errorf("vertica ssl %s: %v", mode, err)
			continue
		}
		release()
		_ = db.Close()
	}
	if _, _, err := openExtraSQLDB(Conn{Driver: "exasol", Host: "h", SSL: &SSLConfig{Mode: "require", CA: "x.pem"}}, ""); err == nil {
		t.Error("exasol with a CA file: want error")
	}
	if _, _, err := openExtraSQLDB(Conn{Driver: "hana", Host: "h", SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}, ""); err == nil {
		t.Error("hana with an unreadable CA: want error")
	}
}

func TestOpenExtraSQLDBThroughATunnelRelay(t *testing.T) {
	target := make(chan string, 1)
	dial := func(_ context.Context, _, addr string) (net.Conn, error) {
		target <- addr
		a, b := net.Pipe()
		_ = b.Close()
		return a, nil
	}
	db, release, err := openExtraSQLDB(Conn{Driver: "exasol", Host: "db.example", Dial: dial}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer release()
	// The pool dials the relay address, and the relay dials the tunnel.
	go func() { _ = db.PingContext(context.Background()) }()
	select {
	case got := <-target:
		if got != "db.example:8563" {
			t.Errorf("the tunnel target = %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Error("the driver never reached the relay")
	}
}

func TestExecDriverConnPaths(t *testing.T) {
	ctx := context.Background()
	f := (&fakeSQL{}).fail("bad", errFake)
	conn, err := fakeDriver{f}.Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := execDriverConn(ctx, conn, "SET a = 1"); err != nil {
		t.Errorf("a statement that works = %v", err)
	}
	if err := execDriverConn(ctx, conn, "bad statement"); err == nil {
		t.Error("a failing statement: want error")
	}
	// A connector that runs start SQL on every new connection.
	sc := startSQLConnector{Connector: fakeConnector{f}, stmts: []string{"SET a = 1", "SET b = 2"}}
	c, err := sc.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if ran := strings.Join(f.ran(), "|"); !strings.Contains(ran, "SET b = 2") {
		t.Errorf("start SQL ran %q", ran)
	}
	bad := startSQLConnector{Connector: fakeConnector{f}, stmts: []string{"bad one\nsecond line"}}
	if _, err := bad.Connect(ctx); err == nil || !strings.Contains(err.Error(), "start SQL") {
		t.Errorf("a failing start statement = %v", err)
	}
	down := startSQLConnector{Connector: downConnector{}, stmts: []string{"SET a = 1"}}
	if _, err := down.Connect(ctx); err == nil {
		t.Error("a connector that cannot connect: want error")
	}
	pc := plainConnector{drv: fakeDriver{f}, dsn: ""}
	if c, err := pc.Connect(ctx); err != nil || pc.Driver() == nil {
		t.Errorf("plain connector = %v", err)
	} else {
		_ = c.Close()
	}
}

type fakeConnector struct{ f *fakeSQL }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{c.f}, nil }
func (c fakeConnector) Driver() driver.Driver                        { return fakeDriver{c.f} }

type downConnector struct{}

func (downConnector) Connect(context.Context) (driver.Conn, error) { return nil, errors.New("down") }
func (downConnector) Driver() driver.Driver                        { return fakeDriver{} }

var _ = io.EOF

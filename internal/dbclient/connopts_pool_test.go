package dbclient

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

type fakeStartConn struct {
	driver.Conn
	ran *[]string
	err error
}

func (c fakeStartConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	*c.ran = append(*c.ran, q)
	return driver.RowsAffected(0), c.err
}
func (c fakeStartConn) Close() error { return nil }

type fakeStartConnector struct {
	ran *[]string
	err error
}

func (f fakeStartConnector) Connect(context.Context) (driver.Conn, error) {
	return fakeStartConn{ran: f.ran, err: f.err}, nil
}
func (f fakeStartConnector) Driver() driver.Driver { return nil }

// DBX-28: every new pooled connection runs the start SQL first.
func TestStartSQLConnectorRunsOnEveryConnection(t *testing.T) {
	var ran []string
	c := startSQLConnector{Connector: fakeStartConnector{ran: &ran}, stmts: []string{"USE `app`", "SET time_zone = 'UTC'"}}
	for i := 0; i < 2; i++ {
		if _, err := c.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(ran) != 4 || ran[0] != "USE `app`" || ran[3] != "SET time_zone = 'UTC'" {
		t.Fatalf("ran %q", ran)
	}
	ran = nil
	bad := startSQLConnector{Connector: fakeStartConnector{ran: &ran, err: errors.New("no such database")}, stmts: []string{"USE `x`"}}
	if _, err := bad.Connect(context.Background()); err == nil {
		t.Fatal("a failing start statement did not fail the connection")
	}
}

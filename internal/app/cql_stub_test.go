package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// cqlStub is a CQL server for tests: fn answers each page; calls records
// the statements (with the page state) that reached it.
type cqlStub struct {
	mu    sync.Mutex
	fn    func(stmt string, args []any, size int, state []byte) (dbclient.CQLPage, error)
	calls []string
}

func (c *cqlStub) Page(_ context.Context, stmt string, args []any, size int, state []byte) (dbclient.CQLPage, error) {
	c.mu.Lock()
	c.calls = append(c.calls, stmt)
	fn := c.fn
	c.mu.Unlock()
	return fn(stmt, args, size, state)
}
func (c *cqlStub) Close() {}

func cqlRow(vals ...string) []dbclient.Cell {
	out := make([]dbclient.Cell, len(vals))
	for i, v := range vals {
		out[i] = dbclient.Cell{Text: v}
	}
	return out
}

// useCQLStub makes every Cassandra session in the test talk to stub.
func useCQLStub(t *testing.T, stub *cqlStub) {
	t.Helper()
	old := dbCassandraSessionFactory
	t.Cleanup(func() { dbCassandraSessionFactory = old })
	dbCassandraSessionFactory = func(_ context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
		return dbclient.NewCassandraSessionWith(c, stub), nil
	}
}

const cqlTestConns = `{"connections":[{"id":"wide","driver":"cassandra","host":"cass.local","port":9042,"database":"shop"}]}`

// writeCQLConns puts the "wide" Cassandra connection in root's project file.
func writeCQLConns(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(cqlTestConns), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shopStub is a keyspace "shop" with tables orders and users; a table read
// has 250 rows, `size` at a time (the page state is one byte: the next page).
func shopStub() *cqlStub { return shopStubN(250) }

// shopStubN is shopStub with n rows in a table.
func shopStubN(n int) *cqlStub {
	var rows [][]dbclient.Cell
	for i := 1; i <= n; i++ {
		rows = append(rows, cqlRow(strconv.Itoa(i), "n"+strconv.Itoa(i)))
	}
	return &cqlStub{fn: func(stmt string, args []any, size int, state []byte) (dbclient.CQLPage, error) {
		switch stmt {
		case "SELECT keyspace_name FROM system_schema.keyspaces":
			return dbclient.CQLPage{Rows: [][]dbclient.Cell{cqlRow("shop"), cqlRow("system")}}, nil
		case "SELECT table_name FROM system_schema.tables WHERE keyspace_name = ?":
			if args[0] == "shop" {
				return dbclient.CQLPage{Rows: [][]dbclient.Cell{cqlRow("orders"), cqlRow("users")}}, nil
			}
			return dbclient.CQLPage{Rows: [][]dbclient.Cell{cqlRow("local")}}, nil
		case "SELECT view_name FROM system_schema.views WHERE keyspace_name = ?":
			return dbclient.CQLPage{}, nil
		case "SELECT column_name, type, kind, position, clustering_order FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?":
			return dbclient.CQLPage{Rows: [][]dbclient.Cell{cqlRow("id", "int", "partition_key", "0", "none"), cqlRow("name", "text", "regular", "-1", "none")}}, nil
		case "SELECT release_version FROM system.local":
			return dbclient.CQLPage{Rows: [][]dbclient.Cell{cqlRow("5.0")}}, nil
		}
		// A table or console read: pages of `size` rows; state is the offset in two bytes.
		from := 0
		if len(state) > 0 {
			from = int(state[0])<<8 | int(state[1])
		}
		end := min(from+size, len(rows))
		p := dbclient.CQLPage{Columns: []dbclient.Column{{Name: "id"}, {Name: "name"}}, Rows: rows[from:end]}
		if end < len(rows) {
			p.Next = []byte{byte(end >> 8), byte(end)}
		}
		return p, nil
	}}
}

package app

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Helpers of the db-edit-result coverage tests (COV-APP-DBEDIT): a
// PostgreSQL connection "pg" with the tables orders (id, customer_id → customers.id,
// note) and customers (id, name) held by a scripted session, and a pump that
// feeds the answers of a command back to the model like the program loop.

// dbeTable is one fake table: its columns, key, foreign keys and rows.
type dbeTable struct {
	cols []dbclient.Column
	keys []string
	fks  []dbclient.SchemaForeignKey
	rows [][]dbclient.Cell
}

// dbeSession answers the data view reads of the tables and records every
// statement; run overrides the answer of a statement when it returns true.
type dbeSession struct {
	cdbSession
	tables map[string]*dbeTable
	over   func(sql string) (dbclient.Result, bool)
}

func dbeNum(s string) dbclient.Cell { return dbclient.Cell{Text: s, Kind: "number"} }

func newDBESession() *dbeSession {
	s := &dbeSession{tables: map[string]*dbeTable{
		"orders": {
			cols: []dbclient.Column{{Name: "id", Type: "int4", PK: true}, {Name: "customer_id", Type: "int4"}, {Name: "note", Type: "text"}},
			keys: []string{"id"},
			fks:  []dbclient.SchemaForeignKey{{Name: "orders_customer_fk", Column: "customer_id", RefTable: "customers", RefColumn: "id"}},
			rows: [][]dbclient.Cell{{dbeNum("1"), dbeNum("5"), cdbCell("first")}, {dbeNum("2"), {Null: true}, cdbCell("second")}},
		},
		"files": {
			cols: []dbclient.Column{{Name: "id", Type: "int4", PK: true}, {Name: "data", Type: "bytea"}, {Name: "body", Type: "text"}, {Name: "n", Type: "int4"}},
			keys: []string{"id"},
			rows: [][]dbclient.Cell{
				{dbeNum("1"), dbclient.CellOfBytes([]byte("\x89PNG\r\n\x1a\nrest")), cdbCell("hello"), dbeNum("3")},
				{dbeNum("2"), {Null: true}, cdbCell("x"), dbeNum("4")},
			},
		},
		"customers": {
			cols: []dbclient.Column{{Name: "id", Type: "int4", PK: true}, {Name: "name", Type: "text"}},
			keys: []string{"id"},
			rows: [][]dbclient.Cell{{dbeNum("5"), cdbCell("ann")}, {dbeNum("6"), cdbCell("bob")}},
		},
	}}
	s.intro = func(r dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
		if tb := s.tables[r.Object]; tb != nil {
			return dbclient.IntrospectionResult{Columns: tb.cols, Keys: tb.keys, ForeignKeys: tb.fks}, nil
		}
		return dbclient.IntrospectionResult{}, nil
	}
	s.run = func(sql string) dbclient.Result {
		if s.over != nil {
			if r, ok := s.over(sql); ok {
				return r
			}
		}
		for name, tb := range s.tables {
			if strings.Contains(sql, `FROM "public"."`+name+`"`) && strings.HasPrefix(strings.TrimSpace(sql), "SELECT") {
				return dbclient.Result{Columns: tb.cols, Rows: tb.rows}
			}
		}
		return dbclient.Result{Affected: 1}
	}
	return s
}

// dbeModel is a Model with the pg connection, its tree (orders and
// customers under public) and the session.
func dbeModel(t *testing.T) (*Model, *dbeSession) {
	t.Helper()
	m := cdbModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432, Database: "app"})
	s := m.databases
	cdbAddNode(s, "pg", "pg/db/app", dbNodeDatabase, "app")
	cdbAddNode(s, "pg/db/app", "pg/db/app/schema/public", dbNodeSchema, "public")
	cdbAddNode(s, "pg/db/app/schema/public", "pg/db/app/schema/public/Tables", dbNodeGroup, "Tables")
	cdbAddNode(s, "pg/db/app/schema/public/Tables", "pg/db/app/schema/public/Tables/orders", dbNodeTable, "orders")
	cdbAddNode(s, "pg/db/app/schema/public/Tables", "pg/db/app/schema/public/Tables/customers", dbNodeTable, "customers")
	cdbAddNode(s, "pg/db/app/schema/public/Tables", "pg/db/app/schema/public/Tables/files", dbNodeTable, "files")
	sess := newDBESession()
	s.sessions["pg"] = sess
	return m, sess
}

// dbeOpen opens the data view of a table from the tree and waits for its
// first page.
func dbeOpen(t *testing.T, m *Model, table string) {
	t.Helper()
	m.databases.databaseSelectPath("pg/db/app/schema/public/Tables/" + table)
	dbePump(m, m.databaseOpenTable())
	r := m.dbConsole().shown
	if r == nil || r.tv == nil || r.tv.name != table || r.result.Err != nil {
		t.Fatalf("the data view of %s did not open: %+v (toast %q)", table, r, cdbToast(m))
	}
}

// dbeMsgs runs a command and returns its messages; a command that waits
// (a toast timer) is dropped after a short while.
func dbeMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []tea.Msg
		run func(c tea.Cmd)
	)
	run = func(c tea.Cmd) {
		defer wg.Done()
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, x := range b {
				if x != nil {
					wg.Add(1)
					go run(x)
				}
			}
			return
		}
		if msg != nil {
			mu.Lock()
			out = append(out, msg)
			mu.Unlock()
		}
	}
	wg.Add(1)
	go run(cmd)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(120 * time.Millisecond):
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]tea.Msg(nil), out...)
}

// dbePump feeds the answers of cmd to the model's handlers, and the
// commands those return, like the program loop would.
func dbePump(m *Model, cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for i := 0; i < 40 && len(queue) > 0; i++ {
		c := queue[0]
		queue = queue[1:]
		for _, msg := range dbeMsgs(c) {
			var next tea.Cmd
			switch x := msg.(type) {
			case applyMsg:
				next = x(m)
			case dbTableMetaMsg:
				next = m.onDBTableMeta(x)
			case dbRunMsg:
				next = m.databaseFinishRun(x)
			default:
				nm, n := m.Update(msg)
				*m = nm.(Model)
				next = n
			}
			if next != nil {
				queue = append(queue, next)
			}
		}
	}
}

// dbeToastHas fails unless the toast holds want.
func dbeToastHas(t *testing.T, m *Model, want string) {
	t.Helper()
	if got := cdbToast(m); !strings.Contains(got, want) {
		t.Fatalf("toast lacks %q: %q", want, got)
	}
}

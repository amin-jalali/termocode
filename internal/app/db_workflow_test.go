package app_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

type workflowDBSession struct {
	dbclient.Session
	conn   dbclient.Conn
	closed atomic.Int32
	calls  atomic.Int32
}

func (s *workflowDBSession) Ping(context.Context) error { return nil }
func (s *workflowDBSession) Close() error               { s.closed.Add(1); return nil }
func (s *workflowDBSession) Introspect(_ context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	s.calls.Add(1)
	if s.conn.Driver == "postgres" && req.Database != "" && req.Database != s.conn.Database {
		return dbclient.IntrospectionResult{}, fmt.Errorf("wrong session: %s for %s", s.conn.Database, req.Database)
	}
	switch {
	case req.Object != "":
		return dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "bigint", PK: true}}}, nil
	case req.Schema != "":
		return dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "active_users", Kind: "VIEW"}}}, nil
	case req.Database != "":
		return dbclient.IntrospectionResult{Schemas: []string{"public"}}, nil
	default:
		return dbclient.IntrospectionResult{Databases: []string{"app", "other"}}, nil
	}
}

type workflowDBFactory struct {
	mu       sync.Mutex
	sessions []*workflowDBSession
}

func (f *workflowDBFactory) open(_ context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Bound a regression's reconnect loop without opening any real sockets.
	if len(f.sessions) >= 4 {
		return nil, errors.New("unexpected repeated connection")
	}
	s := &workflowDBSession{conn: c}
	f.sessions = append(f.sessions, s)
	return s, nil
}

func (f *workflowDBFactory) snapshot() []*workflowDBSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*workflowDBSession(nil), f.sessions...)
}

func databaseWorkflow(t *testing.T, driver string) (*testkit.Driver, *workflowDBFactory) {
	t.Helper()
	f := &workflowDBFactory{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(f.open))
	d := testkit.New(t, testkit.Options{W: 80, H: 24, Files: map[string]string{
		"README.md":                 "database fixture\n",
		".termocode/databases.json": fmt.Sprintf(`{"connections":[{"id":"local","name":"local","driver":%q,"host":"localhost","database":"app"}]}`, driver),
	}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	if !d.WaitForText("DATABASES", time.Second) {
		t.Fatalf("database sidebar did not open:\n%s", ansi.Strip(d.Frame()))
	}
	return d, f
}

func expandWorkflowDatabase(t *testing.T, d *testkit.Driver, driver string) {
	t.Helper()
	d.Key("enter", "down", "enter")
	if driver == "postgres" {
		d.Key("down", "enter")
	}
	for _, name := range []string{"Tables", "Views", "users", "active_users"} {
		if !d.WaitForText(name, time.Second) {
			t.Fatalf("introspection did not show %s:\n%s", name, ansi.Strip(d.Frame()))
		}
	}
}

func TestDatabaseWorkflowIntrospection(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			d, _ := databaseWorkflow(t, driver)
			expandWorkflowDatabase(t, d, driver)
			d.Key("down", "down", "enter", "down")
			if !d.WaitForText("primary key", time.Second) {
				t.Fatalf("primary key footer missing:\n%s", ansi.Strip(d.Frame()))
			}
			found := false
			for _, line := range strings.Split(ansi.Strip(d.Frame()), "\n") {
				if strings.Contains(line, "id  bigint") && strings.Contains(line, "[PK]") {
					found = true
				}
			}
			if !found {
				t.Fatalf("primary key marker does not fit the sidebar:\n%s", ansi.Strip(d.Frame()))
			}
		})
	}
}

func TestDatabaseWorkflowExpandNeverLoadedFoldedNode(t *testing.T) {
	for _, tc := range []struct {
		name, driver, child string
		keys                []string
	}{
		{"database", "postgres", "public", []string{"enter", "down"}},
		{"schema", "postgres", "users", []string{"enter", "down", "enter", "down"}},
		{"table", "postgres", "bigint", []string{"enter", "down", "enter", "down", "enter", "down", "down"}},
		{"mysql-database", "mysql", "users", []string{"enter", "down"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := databaseWorkflow(t, tc.driver)
			d.Key(tc.keys...)
			d.Key("h", "enter")
			if !d.WaitForText(tc.child, time.Second) {
				t.Fatalf("one Enter did not open the folded node:\n%s", ansi.Strip(d.Frame()))
			}
		})
	}
}

func TestDatabaseWorkflowHintsFit80Columns(t *testing.T) {
	for _, tc := range []struct {
		name, hint string
		keys       []string
	}{
		{"database", "Enter expand · h fold", []string{"enter", "down"}},
		{"table", "Enter expand · ? help", []string{"enter", "down", "enter", "down", "enter", "down", "down"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := databaseWorkflow(t, "postgres")
			d.Key(tc.keys...)
			for _, line := range strings.Split(ansi.Strip(d.Frame()), "\n") {
				if strings.Contains(line, "Enter expand") {
					hint := strings.TrimSpace(ansi.Cut(strings.Split(line, "▕")[0], 5, 80))
					if hint != tc.hint {
						t.Fatalf("database hint at 80 columns = %q, want %q", hint, tc.hint)
					}
					return
				}
			}
			t.Fatalf("database hint missing:\n%s", ansi.Strip(d.Frame()))
		})
	}
}

func TestDatabaseWorkflowSessions(t *testing.T) {
	d, f := databaseWorkflow(t, "postgres")
	d.Key("enter", "down", "down", "enter") // other database
	if !d.WaitForText("public", time.Second) {
		t.Fatalf("other database did not expand:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "enter", "down", "down", "enter") // schema, table
	if !d.WaitForText("bigint", time.Second) {
		t.Fatalf("other database table did not expand:\n%s", ansi.Strip(d.Frame()))
	}
	if active, count := d.Model().DatabaseStateForTest(); active != "local" || count != 2 {
		t.Fatalf("active=%q sessions=%d, want local and 2", active, count)
	}
	d.Key("home", "down", "down", "h", "enter", "h", "enter")
	opened := f.snapshot()
	if len(opened) != 2 || opened[0].conn.Database != "app" || opened[1].conn.Database != "other" {
		t.Fatalf("opened %d sessions, want one per database", len(opened))
	}
	d.Key("home", "x")
	if !d.WaitForText("Disconnect from local", time.Second) {
		t.Fatal("disconnect card did not name local")
	}
	d.Key("right", "enter")
	if _, count := d.Model().DatabaseStateForTest(); count != 0 {
		t.Fatalf("disconnect left %d sessions", count)
	}
	for _, s := range opened {
		if s.closed.Load() != 1 {
			t.Errorf("%s closed %d times", s.conn.Database, s.closed.Load())
		}
	}
}

func TestPerfGuardDatabaseWorkflowHidden(t *testing.T) {
	d, f := databaseWorkflow(t, "postgres")
	expandWorkflowDatabase(t, d, "postgres")
	d.Key("ctrl+b")
	if _, count := d.Model().DatabaseStateForTest(); count != 0 {
		t.Fatalf("hidden view left %d sessions", count)
	}
	opened := f.snapshot()
	for _, s := range opened {
		if s.closed.Load() != 1 {
			t.Fatal("hidden view did not close session")
		}
	}
	d.Settle(time.Second)
	before, _ := d.Updates()
	goroutines := runtime.NumGoroutine()
	calls := opened[0].calls.Load()
	d.WaitFor(func() bool { return false }, 5*time.Second)
	for _, msg := range d.UpdatesSince(before) {
		// The git refresh and the settings.json reload ticks are app-wide,
		// not the database view's.
		if msg != "app.gitAutoRefreshMsg" && msg != "app.GitMsg" && msg != "app.settingsReloadTickMsg" {
			t.Fatalf("hidden view produced message: %s", msg)
		}
	}
	if n := runtime.NumGoroutine(); n > goroutines {
		t.Fatalf("hidden goroutines grew: %d -> %d", goroutines, n)
	}
	if len(f.snapshot()) != len(opened) || opened[0].calls.Load() != calls {
		t.Fatal("hidden view opened or queried a session")
	}
}

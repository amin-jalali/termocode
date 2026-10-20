package app_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
	"github.com/charmbracelet/x/ansi"
)

type sidebarDBSession struct {
	dbclient.Session
	conn          dbclient.Conn
	mu            sync.Mutex
	closed, calls int
}

func (s *sidebarDBSession) Ping(context.Context) error { return nil }
func (s *sidebarDBSession) Close() error               { s.mu.Lock(); defer s.mu.Unlock(); s.closed++; return nil }
func (s *sidebarDBSession) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed, s.calls
}
func (s *sidebarDBSession) Introspect(_ context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	switch {
	case req.Database == "":
		return dbclient.IntrospectionResult{Databases: []string{"other", "third"}}, nil
	case s.conn.Driver == "postgres" && req.Database != s.conn.Database:
		return dbclient.IntrospectionResult{}, fmt.Errorf("wrong database session: %s", s.conn.Database)
	case s.conn.Driver == "postgres" && req.Schema == "":
		return dbclient.IntrospectionResult{Schemas: []string{"public"}}, nil
	case req.Schema != req.Database && s.conn.Driver == "mysql":
		return dbclient.IntrospectionResult{}, fmt.Errorf("wrong MySQL schema: %s", req.Schema)
	case req.Object != "":
		return dbclient.IntrospectionResult{Columns: []dbclient.Column{{Name: "id", Type: "bigint", PK: true}}}, nil
	default:
		return dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "users", Kind: "BASE TABLE"}, {Name: "active_users", Kind: "VIEW"}}}, nil
	}
}

func TestDatabasesKeysIntrospectionAndSessionLifetime(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			var mu sync.Mutex
			var opened []*sidebarDBSession
			restore := app.SetDatabaseSessionFactoryForTest(func(_ context.Context, c dbclient.Conn, _ string) (dbclient.Session, error) {
				mu.Lock()
				defer mu.Unlock()
				if len(opened) >= 3 {
					return nil, fmt.Errorf("unexpected extra connection")
				}
				s := &sidebarDBSession{conn: c}
				opened = append(opened, s)
				return s, nil
			})
			t.Cleanup(restore)
			count := func() int { mu.Lock(); defer mu.Unlock(); return len(opened) }
			d := testkit.New(t, testkit.Options{W: 80, H: 24, Files: map[string]string{
				".termocode/databases.json": fmt.Sprintf(`{"connections":[{"id":"local","driver":%q,"host":"localhost","port":5432,"database":"default"}]}`, driver),
			}})
			d.Key("f1")
			d.Type("Databases: Focus")
			d.Key("enter")
			if count() != 0 {
				t.Fatal("opening view connected before Enter")
			}
			d.Key("enter", "down", "enter") // connect, then open other
			if driver == "postgres" {
				d.Key("down", "enter")
			} // public schema
			if !d.WaitForText("active_users", time.Second) || !strings.Contains(ansi.Strip(d.Frame()), "users") {
				t.Fatalf("introspection did not produce visible tables and views:\n%s", ansi.Strip(d.Frame()))
			}
			want := 1
			if driver == "postgres" {
				want = 2
			}
			if count() != want {
				t.Fatalf("opened %d sessions, want %d", count(), want)
			}
			active, sessions := d.Model().DatabaseStateForTest()
			if active != "local" || sessions != want {
				t.Fatalf("active=%q sessions=%d", active, sessions)
			}
			// Fold and unfold the selected database/schema using real keys.
			d.Key("enter", "enter")
			if count() != want {
				t.Fatal("folding and expanding reopened a session")
			}
			// Expand the table and select its primary key at normal sidebar width.
			d.Key("down", "down", "enter", "down")
			if !d.WaitForText("primary key", time.Second) {
				t.Fatalf("primary key detail missing at 80x24:\n%s", ansi.Strip(d.Frame()))
			}
			// Open a second database through the filter prompt, then revisit it.
			d.Key("/")
			d.Type("third")
			d.Key("enter", "enter")
			if driver == "postgres" {
				want++
			}
			if count() != want {
				t.Fatalf("second database opened %d sessions, want %d", count(), want)
			}
			d.Key("enter", "enter")
			if count() != want {
				t.Fatal("re-expanding second database opened a session")
			}
			d.Key("/", "enter") // clear filter
			// The real disconnect card closes the root and every child session.
			d.Key("g", "x")
			if !d.WaitForText("Disconnect database?", time.Second) {
				t.Fatalf("disconnect card did not open:\n%s", ansi.Strip(d.Frame()))
			}
			d.Key("right", "enter")
			_, sessions = d.Model().DatabaseStateForTest()
			if sessions != 0 {
				t.Fatalf("disconnect left %d sessions", sessions)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, s := range opened {
				if closed, _ := s.counts(); closed != 1 {
					t.Errorf("%s closed %d times", s.conn.Database, closed)
				}
			}
		})
	}
}

func TestPerfGuardDatabasesHiddenWithRealKeys(t *testing.T) {
	var calls int
	sess := &sidebarDBSession{conn: dbclient.Conn{Driver: "mysql"}}
	restore := app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) {
		calls++
		return sess, nil
	})
	t.Cleanup(restore)
	d := testkit.New(t, testkit.Options{W: 80, H: 24, Files: map[string]string{
		".termocode/databases.json": `{"connections":[{"id":"local","driver":"mysql","host":"localhost","database":"default"}]}`,
	}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("enter")
	if calls != 1 {
		t.Fatalf("connect called factory %d times", calls)
	}
	d.Key("ctrl+b") // hide the sidebar
	d.Settle(time.Second)
	before, _ := d.Updates()
	goroutines := runtime.NumGoroutine()
	d.WaitFor(func() bool { return false }, 500*time.Millisecond)
	// The git status poll is unrelated to Databases and may fire under
	// load; any other message while hidden is a regression.
	var stray []string
	for _, msg := range d.UpdatesSince(before) {
		if msg != "app.gitAutoRefreshMsg" && msg != "app.GitMsg" {
			stray = append(stray, msg)
		}
	}
	_, sessions := d.Model().DatabaseStateForTest()
	closed, introspections := sess.counts()
	if calls != 1 || closed != 1 || introspections != 1 || sessions != 0 || len(stray) != 0 || runtime.NumGoroutine() > goroutines {
		t.Fatalf("hidden idle: calls=%d sessions=%d closed=%d introspections=%d stray=%v goroutines=%d -> %d", calls, sessions, closed, introspections, stray, goroutines, runtime.NumGoroutine())
	}
}

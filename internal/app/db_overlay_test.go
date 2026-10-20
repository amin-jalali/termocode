package app_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
	"github.com/charmbracelet/x/ansi"
)

// closeCountSession counts Close calls on top of the console fake.
type closeCountSession struct {
	consoleSession
	closed atomic.Int32
}

func (s *closeCountSession) Close() error { s.closed.Add(1); return nil }

// Bug (DB-3): opening the command palette treated the Databases view as
// hidden: it closed every session and cancelled the running query. An
// overlay must leave both alone; only a truly hidden view may close them.
func TestDBPaletteKeepsSessionsAndRunningQuery(t *testing.T) {
	s := &closeCountSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: 80, H: 24, Nvim: true, Files: map[string]string{"query.sql": "-- @conn local\nSELECT slow;\n", ".termocode/databases.json": dbTestConns}})
	d.Action("open:query.sql")
	d.Key("esc", "down")
	dbSend(d)
	dbWaitPage(t, d, "Running")
	d.Key("f1")
	if !d.WaitForText("Command Palette", time.Second) {
		t.Fatalf("palette did not open: %s", ansi.Strip(d.Frame()))
	}
	d.Settle(200 * time.Millisecond)
	d.Key("esc")
	d.Settle(200 * time.Millisecond)
	if s.closed.Load() != 0 {
		t.Fatalf("the palette closed the database session %d times", s.closed.Load())
	}
	if strings.Contains(dbText(t, d), "Cancelled") {
		t.Fatalf("the palette cancelled the running query:\n%s", dbText(t, d))
	}
	// The query still answers to the user's own cancel.
	d.Key("esc")
	dbWaitPage(t, d, "Cancelled")
}

// Hiding the view for real (another activity view) still closes sessions.
func TestDBHiddenViewStillClosesSessions(t *testing.T) {
	s := &closeCountSession{}
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return s, nil }))
	d := testkit.New(t, testkit.Options{W: 80, H: 24, Nvim: true, Files: map[string]string{"query.sql": "-- @conn local\nSELECT 1;\n", ".termocode/databases.json": dbTestConns}})
	d.Action("open:query.sql")
	d.Key("esc", "down")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	d.Key("f1")
	d.Type("View: Toggle Explorer")
	d.Key("enter")
	if !d.WaitFor(func() bool { return s.closed.Load() >= 1 }, 2*time.Second) {
		t.Fatalf("switching to another view kept the session open: %s", ansi.Strip(d.Frame()))
	}
}

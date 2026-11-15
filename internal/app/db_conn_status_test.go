package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Owner 2026-11-15: "Which remote databases do we have connected?" got
// three rows that all said "not connected", while BaaytonaDB cannot
// connect at all and rentcar-db-1 has no password. db_connections must
// give each connection its true state from a cheap check (no network
// dial): connected, ready to connect, missing info (which field),
// unreachable or SSH tunnel down (the last error and when), or a broken
// config.

func TestDBConnStatusOf(t *testing.T) {
	at := time.Date(2026, 11, 15, 14, 2, 5, 0, time.UTC)
	pg := func(mod func(c *dbclient.Conn)) dbclient.Conn {
		c := dbclient.Conn{ID: "pg", Driver: "postgres", Host: "db.example", Port: 5432, User: "app", Database: "app", Password: "pw", Source: dbclient.SourceUser}
		if mod != nil {
			mod(&c)
		}
		return c
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PG_PW=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TC_STATUS_PW", "y")
	env := dbStatusEnv{root: root, names: map[string]bool{"pg-secret": true, "db.saved": true}, store: true}
	for _, tc := range []struct {
		name        string
		c           dbclient.Conn
		live        dbStatusLive
		last        *dbConnErr
		kind, field string
		detail      string
	}{
		{name: "connected", c: pg(nil), live: dbStatusLive{connected: true}, kind: dbStatusConnected},
		{name: "connecting", c: pg(nil), live: dbStatusLive{connecting: true}, kind: dbStatusConnecting},
		{name: "complete", c: pg(nil), kind: dbStatusReady},
		{name: "missing password", c: pg(func(c *dbclient.Conn) { c.Password = "" }), kind: dbStatusMissing, field: "password"},
		{name: "password saved in the store", c: pg(func(c *dbclient.Conn) { c.ID, c.Password = "saved", "" }), kind: dbStatusReady},
		{name: "secret present", c: pg(func(c *dbclient.Conn) { c.Password = "${secret:pg-secret}" }), kind: dbStatusReady},
		{name: "secret not saved", c: pg(func(c *dbclient.Conn) { c.Password = "${secret:other}" }), kind: dbStatusMissing, field: "password", detail: "secret other"},
		{name: "env set", c: pg(func(c *dbclient.Conn) { c.Password = "${env:TC_STATUS_PW}" }), kind: dbStatusReady},
		{name: "env not set", c: pg(func(c *dbclient.Conn) { c.Password = "${env:TC_STATUS_NOPE}" }), kind: dbStatusMissing, field: "password", detail: "TC_STATUS_NOPE"},
		{name: "dotenv set", c: pg(func(c *dbclient.Conn) { c.Password = "${dotenv:PG_PW}" }), kind: dbStatusReady},
		{name: "dotenv not set", c: pg(func(c *dbclient.Conn) { c.Password = "${dotenv:NOPE}" }), kind: dbStatusMissing, field: "password", detail: "NOPE"},
		{name: "url password", c: pg(func(c *dbclient.Conn) { c.Password, c.URL = "", "postgres://app:pw@db.example:5432/app" }), kind: dbStatusReady},
		{name: "missing host", c: pg(func(c *dbclient.Conn) { c.Host, c.Problem = "", "host and valid port are required" }), kind: dbStatusMissing, field: "host"},
		{name: "missing port", c: pg(func(c *dbclient.Conn) { c.Port, c.Problem = 0, "host and valid port are required" }), kind: dbStatusMissing, field: "port"},
		{name: "missing user", c: pg(func(c *dbclient.Conn) { c.Driver, c.User = "sqlserver", "" }), kind: dbStatusMissing, field: "user"},
		{name: "postgres takes the login's user", c: pg(func(c *dbclient.Conn) { c.User = "" }), kind: dbStatusReady},
		{name: "ssh without a host", c: pg(func(c *dbclient.Conn) { c.SSH = &dbclient.SSHConfig{User: "deploy"} }), kind: dbStatusMissing, field: "ssh host"},
		{name: "tunnel target known", c: pg(func(c *dbclient.Conn) { c.SSH = &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy"} }),
			kind: dbStatusReady, detail: "SSH tunnel to deploy@65.109.212.14 opens on connect"},
		{name: "container target known", c: pg(func(c *dbclient.Conn) {
			c.SSH = &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy"}
			c.Container = &dbclient.ContainerRef{Name: "rentcar-db-1", Context: "Baaytona"}
		}), kind: dbStatusReady, detail: "container rentcar-db-1 on Baaytona"},
		{name: "last error", c: pg(nil), last: &dbConnErr{Text: "nothing answers at 127.0.0.1:5432", At: at}, kind: dbStatusUnreachable, detail: "nothing answers at 127.0.0.1:5432"},
		{name: "tunnel down", c: pg(func(c *dbclient.Conn) { c.SSH = &dbclient.SSHConfig{Host: "65.109.212.14"} }),
			last: &dbConnErr{Text: "ssh: handshake failed", At: at, Tunnel: true}, kind: dbStatusTunnelDown, detail: "ssh: handshake failed"},
		{name: "config problem", c: pg(func(c *dbclient.Conn) { c.Problem = "password must be a reference" }), kind: dbStatusMissing, field: "password"},
		{name: "other config problem", c: pg(func(c *dbclient.Conn) { c.Problem = "unknown driver" }), kind: dbStatusProblem, detail: "unknown driver"},
		{name: "sqlite needs no password", c: dbclient.Conn{ID: "notes", Driver: "sqlite", Path: "data/notes.db"}, kind: dbStatusReady},
		{name: "missing info wins over an old error", c: pg(func(c *dbclient.Conn) { c.Host, c.Problem = "", "host and valid port are required" }), last: &dbConnErr{Text: "no host", At: at}, kind: dbStatusMissing, field: "host"},
		{name: "a failed try wins over no saved password", c: pg(func(c *dbclient.Conn) { c.Password = "" }), last: &dbConnErr{Text: "nothing answers", At: at}, kind: dbStatusUnreachable, detail: "nothing answers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := dbConnStatusOf(tc.c, env, tc.live, tc.last)
			if st.Kind != tc.kind || st.Field != tc.field {
				t.Fatalf("status = %+v, want %s/%s", st, tc.kind, tc.field)
			}
			if tc.detail != "" && !strings.Contains(st.Detail, tc.detail) {
				t.Errorf("detail %q lacks %q", st.Detail, tc.detail)
			}
			if tc.last != nil && (st.Kind == dbStatusUnreachable || st.Kind == dbStatusTunnelDown) && !st.At.Equal(at) {
				t.Errorf("at = %v, want %v", st.At, at)
			}
			if strings.Contains(st.Label(at), "pw") && tc.c.Password == "pw" {
				t.Errorf("label shows the password: %q", st.Label(at))
			}
		})
	}
}

// The label is one line the model and the card read: the state, the
// missing field, the last error with its time.
func TestDBConnStatusLabel(t *testing.T) {
	at := time.Date(2026, 11, 15, 14, 2, 5, 0, time.UTC)
	for _, tc := range []struct {
		st   dbConnStatus
		want string
	}{
		{dbConnStatus{Kind: dbStatusConnected}, "connected"},
		{dbConnStatus{Kind: dbStatusReady}, "ready to connect"},
		{dbConnStatus{Kind: dbStatusMissing, Field: "password", Detail: "nothing saved"}, "missing info: password, nothing saved"},
		{dbConnStatus{Kind: dbStatusMissing, Field: "ssh host"}, "missing info: ssh host"},
		{dbConnStatus{Kind: dbStatusUnreachable, Detail: "nothing answers · at 127.0.0.1:5432", At: at.Add(-3 * time.Minute)}, "unreachable: nothing answers, at 127.0.0.1:5432 (last try 3 min ago)"},
		{dbConnStatus{Kind: dbStatusTunnelDown, Detail: "handshake failed", At: at.Add(-10 * time.Second)}, "SSH tunnel down: handshake failed (last try just now)"},
		{dbConnStatus{Kind: dbStatusProblem, Detail: "unknown driver"}, "problem: unknown driver"},
	} {
		if got := tc.st.Label(at); got != tc.want {
			t.Errorf("label = %q, want %q", got, tc.want)
		}
		if k, f := dbStatusFromLabel(tc.st.Label(at)); k != tc.st.Kind || f != tc.st.Field {
			t.Errorf("parse %q = %s/%s", tc.st.Label(at), k, f)
		}
	}
}

// The owner's three connections (fake data): BaaytonaDB failed on its
// last try, baaytona-db-1 has its password, rentcar-db-1 has none.
func TestAIDBConnectionsTrueStatus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	remoteSetup(t, root, remoteUserConns)
	dbResetConnErrs(t)
	dbNoteConnErr("baaytonadb", "nothing answers at 127.0.0.1:5432 on the server: the port must be published or the connection changed", false)
	tl, _ := ai.FindTool(aiDBTools(root, nil), "db_connections")
	out, err := tl.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"baaytonadb":    "status: unreachable: nothing answers at 127.0.0.1:5432",
		"baaytona-db-1": "status: ready to connect",
		"rentcar-db-1":  "status: missing info: password",
		"notes":         "status: ready to connect",
	} {
		if l := remoteLine(t, out, id); !strings.Contains(l, want) {
			t.Errorf("%s lacks %q: %s", id, want, l)
		}
	}
	if l := remoteLine(t, out, "rentcar-db-1"); !strings.Contains(l, "next: ") {
		t.Errorf("no next step: %s", l)
	}
	if strings.Contains(out, aiDBNotConnectedState) {
		t.Errorf("still the bare %q:\n%s", aiDBNotConnectedState, out)
	}
	if strings.Contains(out, "user-secret-2") {
		t.Error("a password in the list")
	}
}

// The tool tells the model to offer the next step on the card, not
// only to report, and that the list never dials.
func TestAIDBConnectionsOffersNextStep(t *testing.T) {
	tl, _ := ai.FindTool(aiDBTools(t.TempDir(), nil), "db_connections")
	for _, w := range []string{"status", "next step", "Test"} {
		if !strings.Contains(tl.Description, w) {
			t.Errorf("description lacks %q: %s", w, tl.Description)
		}
	}
	if tl.View == nil {
		t.Fatal("db_connections has no card (View)")
	}
}

// A db_* tool whose login fails keeps that error as the connection's
// last one; db_connections then says unreachable.
func TestAIDBOpenFailureIsTheLastError(t *testing.T) {
	e := newAIDBEnv(t)
	dbResetConnErrs(t)
	t.Setenv("PGPASSWORD", "")
	e.factory = func() error { return errors.New("dial tcp 10.0.0.9:5432: connect: connection refused") }
	_, _ = e.tool(t, "db_query").Run(context.Background(), map[string]any{"conn": "local", "sql": "SELECT 1"})
	last := dbLastConnErr("local")
	if last == nil || !strings.Contains(last.Text, "connection refused") || last.At.IsZero() {
		t.Fatalf("last error = %+v", last)
	}
	e.factory = nil
	if _, err := e.tool(t, "db_query").Run(context.Background(), map[string]any{"conn": "local", "sql": "SELECT 1"}); err == nil {
		if dbLastConnErr("local") != nil {
			t.Error("a login that worked keeps the old error")
		}
	}
}

// The card: one row per connection with its status badge, tone and the
// actions; Fix… only when info is missing; each row points at its
// connection so the page can add it to the chat.
func TestAIDBConnectionsCard(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	remoteSetup(t, root, remoteUserConns)
	dbResetConnErrs(t)
	dbNoteConnErr("baaytonadb", "nothing answers at 127.0.0.1:5432", false)
	tl, _ := ai.FindTool(aiDBTools(root, nil), "db_connections")
	out, err := tl.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b := tl.View(nil, out)
	if b == nil {
		t.Fatalf("no card for:\n%s", out)
	}
	if b.Kind != blocks.KindList || b.Source != "" && b.Source != "db_connections" {
		t.Fatalf("card = %+v", b)
	}
	items := map[string]blocks.BlockItem{}
	for _, it := range b.Items {
		items[it.ID] = it
	}
	acts := func(it blocks.BlockItem) string {
		var ids []string
		for _, a := range it.Actions {
			ids = append(ids, a.ID)
		}
		return strings.Join(ids, ",")
	}
	for id, want := range map[string]struct{ tone, badge, acts string }{
		"baaytonadb":    {blocks.ToneBad, "unreachable", "connect,open,tables,query,test"},
		"baaytona-db-1": {blocks.ToneIdle, "ready", "connect,open,tables,query,test"},
		"rentcar-db-1":  {blocks.ToneWarn, "missing password", "fix,connect,open,tables,query,test"},
	} {
		it, ok := items[id]
		if !ok {
			t.Fatalf("no row %s in %+v", id, b.Items)
		}
		if it.Tone != want.tone || it.Badge != want.badge || acts(it) != want.acts {
			t.Errorf("%s = tone %s badge %q actions %s; want %+v", id, it.Tone, it.Badge, acts(it), want)
		}
		if it.Ref == nil || it.Ref.Kind != dbCardRefKind || it.Ref.ID != id {
			t.Errorf("%s ref = %+v", id, it.Ref)
		}
		for _, a := range it.Actions {
			if a.Kind != blocks.ActOp || a.Ref == nil || a.Ref.ID != id {
				t.Errorf("%s action %+v", id, a)
			}
		}
	}
	if fix := items["rentcar-db-1"].Actions[0]; fix.Op != dbCardOpFix+"password" || fix.Label != "Fix password…" {
		t.Errorf("fix = %+v", fix)
	}
	for _, it := range b.Items {
		for _, a := range it.Actions {
			if a.ID == "connect" && a.Op != "db.connect" || a.ID == "tables" && a.Op != "db.tables" || a.ID == "test" && a.Op != "db.test" ||
				a.ID == "open" && a.Op != dbCardOpOpen || a.ID == "query" && a.Op != dbCardOpQuery {
				t.Errorf("%s: %s runs %s", it.ID, a.ID, a.Op)
			}
		}
	}
	if c := *b; func() error { c.ID = "x"; return c.Validate() }() != nil {
		t.Errorf("the card breaks the block caps: %v", c.Validate())
	}
}

// dbResetConnErrs empties the last-error list for one test.
func dbResetConnErrs(t *testing.T) {
	t.Helper()
	dbConnErrs.Lock()
	old := dbConnErrs.m
	dbConnErrs.m = nil
	dbConnErrs.Unlock()
	t.Cleanup(func() {
		dbConnErrs.Lock()
		dbConnErrs.m = old
		dbConnErrs.Unlock()
	})
}

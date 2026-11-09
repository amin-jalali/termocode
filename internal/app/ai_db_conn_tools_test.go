package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// aiDBUserConns writes your connections file (a container-made
// connection among them) and reloads the model's tree.
func aiDBUserConns(t *testing.T, e *aiDBEnv) string {
	t.Helper()
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "db", "connections.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"connections":[{"id":"box-pg","driver":"postgres","host":"10.0.0.5","port":5432,"user":"app","database":"app",` +
		`"container":{"name":"pg-1","context":"box"}}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	e.m.databases.load(e.root)
	return path
}

func aiDBFile(t *testing.T, path string) []dbclient.Conn {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := dbclient.ParseConfig(b, "", path, "")
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func aiDBFind(cs []dbclient.Conn, id string) (dbclient.Conn, bool) {
	for _, c := range cs {
		if c.ID == id {
			return c, true
		}
	}
	return dbclient.Conn{}, false
}

func (e *aiDBEnv) projectFile() string { return filepath.Join(e.root, ".termocode", "databases.json") }

// db_connection asks exactly where the GUI asks: the web's cards (add,
// edit, delete, disconnect, SSH, SSL) ask every time with no "Always
// allow"; test, connect and Open in Databases run at once (the laptop's
// own password prompt is the question there).
func TestAIDBConnectionGateMatchesTheGUI(t *testing.T) {
	e := newAIDBEnv(t)
	for _, tc := range []struct {
		args map[string]any
		ask  bool
	}{
		{map[string]any{"op": "add", "id": "new", "driver": "postgres", "host": "db.local"}, true},
		{map[string]any{"op": "edit", "conn": "local", "host": "db2.local"}, true},
		{map[string]any{"op": "delete", "conn": "local"}, true},
		{map[string]any{"op": "disconnect", "conn": "local"}, true},
		{map[string]any{"op": "ssh", "conn": "local", "ssh_host": "bastion"}, true},
		{map[string]any{"op": "ssh", "conn": "local", "remove": true}, true},
		{map[string]any{"op": "ssl", "conn": "local", "ssl_mode": "verify-full"}, true},
		{map[string]any{"op": "test", "conn": "local"}, false},
		{map[string]any{"op": "connect", "conn": "local"}, false},
		{map[string]any{"op": "open_container", "container": "termocode-postgres-1"}, false},
	} {
		ask, class, err := e.gate(t, "db_connection", tc.args)
		if err != nil || ask != tc.ask || class != "" {
			t.Errorf("%v: ask=%v class=%q err=%v, want ask=%v and no Always allow", tc.args, ask, class, err, tc.ask)
		}
	}
	// The docs page reads the gate from op alone.
	tl := e.tool(t, "db_connection")
	for op, ask := range map[string]bool{"add": true, "edit": true, "delete": true, "disconnect": true, "ssh": true, "ssl": true, "test": false, "connect": false, "open_container": false} {
		if got, class := tl.Gate(map[string]any{"op": op}); got != ask || class != "" {
			t.Errorf("op %s alone: ask=%v class=%q, want %v", op, got, class, ask)
		}
	}
}

// A password never comes from the model: a password (or a URL, which can
// hold one) is refused before the card; bad values are refused as the
// web refuses them.
func TestAIDBConnectionRefusesPasswordsAndBadArgs(t *testing.T) {
	e := newAIDBEnv(t)
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"op": "add", "id": "new", "driver": "postgres", "host": "h", "password": "hunter2-secret"}, "password"},
		{map[string]any{"op": "edit", "conn": "local", "password": "hunter2-secret"}, "password"},
		{map[string]any{"op": "add", "id": "new", "driver": "postgres", "url": "postgres://a:b@h/db"}, "url"},
		{map[string]any{"op": "add", "id": "local", "driver": "postgres", "host": "h"}, "exists"},
		{map[string]any{"op": "add", "id": "Bad Id", "driver": "postgres", "host": "h"}, "id"},
		{map[string]any{"op": "add", "id": "new", "driver": "oracle9", "host": "h"}, "driver"},
		{map[string]any{"op": "add", "id": "new", "driver": "postgres", "host": "h", "file": "elsewhere"}, "file"},
		{map[string]any{"op": "edit", "conn": "nope", "host": "h"}, "no connection"},
		{map[string]any{"op": "edit", "conn": "local"}, "nothing to change"},
		{map[string]any{"op": "ssl", "conn": "local", "ssl_mode": "maybe"}, "mode"},
		{map[string]any{"op": "ssh", "conn": "local"}, "ssh_host"},
		{map[string]any{"op": "open_container"}, "container"},
		{map[string]any{"op": "fly"}, "op"},
	} {
		_, _, err := e.gate(t, "db_connection", tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err %v, want %q", tc.args, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), "hunter2-secret") {
			t.Errorf("the error echoes the password: %v", err)
		}
	}
}

// add saves to the project file by default (the web's file) or to your
// connections file; never with a password. The card names the file.
func TestAIDBConnectionAddSavesToTheChosenFile(t *testing.T) {
	e := newAIDBEnv(t)
	tl := e.tool(t, "db_connection")
	add := map[string]any{"op": "add", "id": "orders", "name": "Orders", "driver": "postgres", "host": "orders.local", "user": "app", "database": "orders", "environment": "staging"}
	card := tl.Human(add)
	for _, want := range []string{"Save this connection to .termocode/databases.json?", "orders.local:5432", "No password is saved"} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	out, err := e.run(t, "db_connection", add)
	if err != nil || !strings.Contains(out, "orders") {
		t.Fatalf("add: %q %v", out, err)
	}
	c, ok := aiDBFind(aiDBFile(t, e.projectFile()), "orders")
	if !ok || c.Host != "orders.local" || c.Port != 5432 || c.Password != "" || c.Environment != "staging" {
		t.Fatalf("project file: %+v %v", c, ok)
	}
	user := map[string]any{"op": "add", "id": "scratch", "driver": "sqlite", "path": "data/scratch.db", "file": "user"}
	if card := tl.Human(user); !strings.Contains(card, "your connections file") {
		t.Errorf("user card: %s", card)
	}
	if _, err := e.run(t, "db_connection", user); err != nil {
		t.Fatal(err)
	}
	dir, _ := configDir()
	if _, ok := aiDBFind(aiDBFile(t, filepath.Join(dir, "db", "connections.json")), "scratch"); !ok {
		t.Fatal("not in your connections file")
	}
	if _, ok := aiDBFind(aiDBFile(t, e.projectFile()), "scratch"); ok {
		t.Fatal("a user connection went to the project file")
	}
	if _, _, err := e.gate(t, "db_connection", map[string]any{"op": "add", "id": "out", "driver": "sqlite", "path": "../outside.db"}); err == nil {
		t.Fatal("a sqlite file outside the workspace was accepted")
	}
}

// edit, ssh and ssl change the file that holds the connection; delete
// removes it from that file only.
func TestAIDBConnectionChangesTheFileThatHoldsIt(t *testing.T) {
	e := newAIDBEnv(t)
	userPath := aiDBUserConns(t, e)
	card := e.tool(t, "db_connection").Human(map[string]any{"op": "edit", "conn": "box-pg", "database": "shop"})
	if !strings.Contains(card, "database: app → shop") || !strings.Contains(card, "your connections file") {
		t.Errorf("edit card:\n%s", card)
	}
	if _, err := e.run(t, "db_connection", map[string]any{"op": "edit", "conn": "box-pg", "database": "shop", "read_only": true}); err != nil {
		t.Fatal(err)
	}
	c, _ := aiDBFind(aiDBFile(t, userPath), "box-pg")
	if c.Database != "shop" || !c.ReadOnly || c.Container == nil || c.Container.Name != "pg-1" {
		t.Fatalf("user file after edit: %+v", c)
	}
	if _, ok := aiDBFind(aiDBFile(t, e.projectFile()), "box-pg"); ok {
		t.Fatal("edit wrote the project file")
	}

	if _, err := e.run(t, "db_connection", map[string]any{"op": "ssh", "conn": "local", "ssh_host": "bastion", "ssh_user": "ops", "ssh_auth": "agent"}); err != nil {
		t.Fatal(err)
	}
	c, _ = aiDBFind(aiDBFile(t, e.projectFile()), "local")
	if c.SSH == nil || c.SSH.Host != "bastion" || c.SSH.User != "ops" {
		t.Fatalf("ssh not saved: %+v", c.SSH)
	}
	if _, err := e.run(t, "db_connection", map[string]any{"op": "ssh", "conn": "local", "remove": true}); err != nil {
		t.Fatal(err)
	}
	if c, _ = aiDBFind(aiDBFile(t, e.projectFile()), "local"); c.SSH != nil {
		t.Fatalf("ssh not removed: %+v", c.SSH)
	}
	if _, err := e.run(t, "db_connection", map[string]any{"op": "ssl", "conn": "local", "ssl_mode": "verify-full", "ssl_ca": "certs/ca.pem"}); err != nil {
		t.Fatal(err)
	}
	if c, _ = aiDBFind(aiDBFile(t, e.projectFile()), "local"); c.SSL == nil || c.SSL.Mode != "verify-full" || c.SSL.CA != "certs/ca.pem" {
		t.Fatalf("ssl not saved: %+v", c.SSL)
	}

	del := map[string]any{"op": "delete", "conn": "box-pg"}
	if card := e.tool(t, "db_connection").Human(del); !strings.Contains(card, "Delete connection box-pg?") || !strings.Contains(card, "The database itself is not touched") {
		t.Errorf("delete card:\n%s", card)
	}
	if _, err := e.run(t, "db_connection", del); err != nil {
		t.Fatal(err)
	}
	if _, ok := aiDBFind(aiDBFile(t, userPath), "box-pg"); ok {
		t.Fatal("box-pg still in your connections file")
	}
	if _, err := e.run(t, "db_connection", map[string]any{"op": "delete", "conn": "ro"}); err != nil {
		t.Fatal(err)
	}
	proj := aiDBFile(t, e.projectFile())
	if _, ok := aiDBFind(proj, "ro"); ok || len(proj) != 2 {
		t.Fatalf("project file after delete: %+v", proj)
	}
	if _, ok := e.m.databases.connection("ro"); ok {
		t.Fatal("the tree still has the deleted connection")
	}
}

// test logs in fresh (never a clone of the open session) and pings;
// connect starts the view's own connect (it asks the user for a missing
// password); disconnect closes the user's sessions.
func TestAIDBConnectionTestConnectDisconnect(t *testing.T) {
	e := newAIDBEnv(t)
	user := &aiDBSession{name: "user"}
	e.m.databases.sessions["local"] = user
	out, err := e.run(t, "db_connection", map[string]any{"op": "test", "conn": "local"})
	if err != nil || !strings.Contains(out, "works") {
		t.Fatalf("test: %q %v", out, err)
	}
	if len(user.clones) != 0 || len(e.opened) != 1 || e.opened[0].closed != 1 {
		t.Fatalf("test did not log in fresh: clones %d opened %d", len(user.clones), len(e.opened))
	}
	if out, err := e.run(t, "db_connection", map[string]any{"op": "connect", "conn": "local"}); err != nil || !strings.Contains(out, "already connected") {
		t.Fatalf("connect of a connected one: %q %v", out, err)
	}
	if out, err := e.run(t, "db_connection", map[string]any{"op": "connect", "conn": "prod"}); err != nil || !strings.Contains(out, "Connecting") || !strings.Contains(out, "password") {
		t.Fatalf("connect: %q %v", out, err)
	}
	if _, err := e.run(t, "db_connection", map[string]any{"op": "disconnect", "conn": "local"}); err != nil {
		t.Fatal(err)
	}
	if e.m.databases.sessions["local"] != nil || user.closed != 1 {
		t.Fatalf("disconnect: session %v closed %d", e.m.databases.sessions["local"], user.closed)
	}
	if _, err := e.run(t, "db_connection", map[string]any{"op": "disconnect", "conn": "local"}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("disconnect of a closed one: %v", err)
	}
}

// Open in Databases from the AI: the database container's connection is
// prepared and the laptop's own password prompt asks the user.
func TestAIDBConnectionOpenContainer(t *testing.T) {
	e := newAIDBEnv(t)
	e.m.services = svcHostState()
	out, err := e.run(t, "db_connection", map[string]any{"op": "open_container", "container": "nginx"})
	if err == nil || !strings.Contains(err.Error(), "termocode-postgres-1") {
		t.Fatalf("not a database container: %q %v", out, err)
	}
	out, err = e.run(t, "db_connection", map[string]any{"op": "open_container", "container": "termocode-postgres-1"})
	if err != nil || !strings.Contains(out, "password") {
		t.Fatalf("open: %q %v", out, err)
	}
	if p := e.m.databases.pendingConn; p.Driver != "postgres" || p.Port != 5432 {
		t.Fatalf("pending connection: %+v", p)
	}
}

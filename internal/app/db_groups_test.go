package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-21: connection folders as tree groups, the colour mark on a
// connection row, keep-alive pings and auto-reconnect with backoff.

func TestDBTreeGroupsConnectionsByFolder(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{
		{ID: "a", Name: "a"},
		{ID: "p1", Name: "p1", Folder: "team/prod"},
		{ID: "d1", Name: "d1", Folder: "dev"},
		{ID: "p2", Name: "p2", Folder: "team/prod"},
	}
	for _, c := range s.conns {
		s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.Name, path: c.ID, conn: c.ID}
	}
	s.loaded = true
	var got []string
	for _, r := range s.rows() {
		if r.kind == dbNodeFolderHeader {
			got = append(got, "#"+r.name)
		} else {
			got = append(got, r.name)
		}
	}
	if strings.Join(got, ",") != "a,#dev,d1,#team/prod,p1,p2" {
		t.Errorf("rows = %v", got)
	}
	for _, r := range s.rows() {
		if r.kind == dbNodeFolderHeader && !r.selectable {
			t.Error("a folder heading is not selectable (it folds)")
		}
		if r.name == "p1" && r.depth != 1 {
			t.Errorf("p1 depth = %d", r.depth)
		}
	}

	// A filter that matches nothing in a folder drops its heading.
	s.filter = "d1"
	got = nil
	for _, r := range s.rows() {
		got = append(got, r.name)
	}
	if strings.Join(got, ",") != "dev,d1" {
		t.Errorf("filtered rows = %v", got)
	}
}

func TestDBConnColorTint(t *testing.T) {
	if _, ok := dbConnTint("blue"); !ok {
		t.Error("blue has no tint")
	}
	if _, ok := dbConnTint(""); ok {
		t.Error("no colour has a tint")
	}
	for _, c := range dbclient.ConnColors() {
		if _, ok := dbConnTint(c); !ok {
			t.Errorf("%s has no tint", c)
		}
	}
}

func TestDBKeepAliveEvery(t *testing.T) {
	if dbKeepAliveEvery(dbclient.Conn{}) != 0 {
		t.Error("no options: keep-alive on")
	}
	if d := dbKeepAliveEvery(dbclient.Conn{Options: &dbclient.ConnOptions{KeepAliveSeconds: 1}}); d.Seconds() != 5 {
		t.Errorf("1 s keep-alive = %v, want the 5 s floor", d)
	}
	if d := dbKeepAliveEvery(dbclient.Conn{Options: &dbclient.ConnOptions{AutoReconnect: true}}); d.Seconds() != 30 {
		t.Errorf("auto-reconnect alone = %v, want 30 s", d)
	}
	if dbKeepAliveEvery(dbclient.Conn{Driver: "sqlite", Options: &dbclient.ConnOptions{KeepAliveSeconds: 10}}) != 0 {
		t.Error("sqlite gets keep-alive")
	}
}

type dbPingFake struct {
	dbclient.Session
	err error
}

func (f dbPingFake) Ping(context.Context) error { return f.err }
func (f dbPingFake) Close() error               { return nil }

func TestDBKeepAlivePingAndReconnect(t *testing.T) {
	m := hoverModel(t)
	m.databases = newDBState()
	m.databases.conns = []dbclient.Conn{{ID: "c", Name: "c", Driver: "postgres", Options: &dbclient.ConnOptions{KeepAliveSeconds: 10, AutoReconnect: true}}}
	m.databases.nodes["c"] = dbNode{kind: dbNodeConnection, name: "c", path: "c", conn: "c"}
	m.databases.sessions["c"] = dbPingFake{}
	if cmd := m.dbKeepAliveStart("c"); cmd == nil {
		t.Fatal("no keep-alive tick")
	}
	ka := m.databases.keepAlive["c"]

	// A good ping keeps the session.
	m.onDBPingDone(dbPingDoneMsg{conn: "c", gen: ka.gen})
	if m.databases.sessions["c"] == nil {
		t.Fatal("good ping dropped the session")
	}

	// A failed ping drops the session and plans a reconnect.
	m.onDBPingDone(dbPingDoneMsg{conn: "c", gen: ka.gen, err: errors.New("broken pipe")})
	if m.databases.sessions["c"] != nil {
		t.Fatal("failed ping kept the session")
	}
	ka = m.databases.keepAlive["c"]
	if ka == nil || !ka.reconnecting || ka.fails != 1 {
		t.Fatalf("keep-alive = %+v", ka)
	}

	// A failed reconnect waits longer; after the last attempt it stops.
	for i := 0; i < dbReconnectAttempts; i++ {
		m.dbReconnectFailed("c")
	}
	if ka := m.databases.keepAlive["c"]; ka != nil && ka.reconnecting {
		t.Fatalf("still reconnecting after %d attempts: %+v", dbReconnectAttempts, ka)
	}

	// The user's own disconnect stops the keep-alive.
	m.databases.sessions["c"] = dbPingFake{}
	m.dbKeepAliveStart("c")
	m.databaseDisconnectNow("c")
	if m.databases.keepAlive["c"] != nil {
		t.Error("disconnect left the keep-alive")
	}
}

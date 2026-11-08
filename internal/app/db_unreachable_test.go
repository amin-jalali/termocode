package app

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

// unreachableModel is a model with one saved Postgres connection
// (127.0.0.1:5432, as the owner saved it); sshCfg nil: no tunnel.
func unreachableModel(t *testing.T, sshCfg *dbclient.SSHConfig) *Model {
	t.Helper()
	s := newDBState()
	s.root, s.visible, s.loaded = t.TempDir(), true, true
	c := dbclient.Conn{ID: "baaytona", Name: "baaytona", Driver: "postgres", Host: "127.0.0.1", Port: 5432, User: "app", Database: "app",
		Params: map[string]string{"sslmode": "disable"}, Source: dbclient.SourceUser, SSH: sshCfg}
	s.conns = []dbclient.Conn{c}
	s.userConns = []dbclient.Conn{c}
	s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.ID, path: c.ID, conn: c.ID, detail: dbConnDetail(c)}
	m := dbTestModel(s)
	m.w, m.h = 80, 24
	t.Cleanup(func() { m.databasesClose(false) })
	return m
}

// shortConnectTimeout makes the connect deadline short for one test.
func shortConnectTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := dbTestTimeout
	dbTestTimeout = d
	t.Cleanup(func() { dbTestTimeout = old })
}

// connectEnds runs the connect of id and fails when no result comes in
// within limit (the row would stay on Loading… forever).
func connectEnds(t *testing.T, m *Model, id string, limit time.Duration) dbNode {
	t.Helper()
	cmd := m.databaseConnect(id)
	if !m.databases.nodes[id].loading {
		t.Fatal("the row does not show Loading… while it connects")
	}
	got := make(chan dbConnectMsg, 1)
	go func() {
		if msg, ok := cmd().(dbConnectMsg); ok {
			got <- msg
		}
	}()
	select {
	case msg := <-got:
		_ = m.onDBConnect(msg)
	case <-time.After(limit):
		t.Fatalf("no connect result after %v: the row stays on Loading… forever", limit)
	}
	n := m.databases.nodes[id]
	if n.loading {
		t.Fatal("the row still says Loading… after the connect ended")
	}
	if n.problem == "" || strings.Contains(n.problem, "\n") {
		t.Fatalf("problem = %q, want one plain line", n.problem)
	}
	return n
}

// Owner's case: through SSH to 65.109.212.14, nothing listens on the
// server's 127.0.0.1:5432 (the container's port is not published).
func TestDBTunnelNothingListensEndsWithPlainReason(t *testing.T) {
	srv := dbtest.NewSSHServer() // Forward nil: refused, as sshd says when nothing listens
	srv.Password = "tunnel-pw-31"
	defer srv.Close()
	home := sshTestHome(t, srv)
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "65.109.212.14:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m := unreachableModel(t, &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"})
	dbStoreSet(t, "db.baaytona.ssh", "tunnel-pw-31")
	n := connectEnds(t, m, "baaytona", 5*time.Second)
	want := "Nothing answers at 127.0.0.1:5432 on 65.109.212.14 (through SSH). Is the container's port published?"
	if n.problem != want {
		t.Fatalf("problem = %q\nwant      %q", n.problem, want)
	}
}

// The SSH server opens the channel but the far end never answers: the
// connect ends at the deadline and says what did not answer.
func TestDBTunnelSilentTargetEndsAtTheDeadline(t *testing.T) {
	shortConnectTimeout(t, 400*time.Millisecond)
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-32"
	var peers []net.Conn
	srv.Forward = func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		peers = append(peers, b)
		return a, nil
	}
	defer srv.Close()
	defer func() {
		for _, p := range peers {
			_ = p.Close()
		}
	}()
	home := sshTestHome(t, srv)
	if err := dbclient.TrustHostKey(filepath.Join(home, ".ssh", "known_hosts"), "65.109.212.14:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	m := unreachableModel(t, &dbclient.SSHConfig{Host: "65.109.212.14", User: "deploy", Auth: "password"})
	dbStoreSet(t, "db.baaytona.ssh", "tunnel-pw-32")
	n := connectEnds(t, m, "baaytona", 5*time.Second)
	want := "No answer from 127.0.0.1:5432 on 65.109.212.14 (through SSH) in time."
	if n.problem != want {
		t.Fatalf("problem = %q\nwant      %q", n.problem, want)
	}
}

// A driver that ignores the deadline must not keep the row on
// Loading…: the connect gives up at the deadline (plus a short grace).
func TestDBConnectGivesUpWhenTheDriverIgnoresTheDeadline(t *testing.T) {
	shortConnectTimeout(t, 200*time.Millisecond)
	release, returned := make(chan struct{}), make(chan struct{})
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(context.Context, dbclient.Conn, string) (dbclient.Session, error) {
		defer close(returned)
		<-release // stuck: never looks at ctx
		return nil, errors.New("late")
	}
	t.Cleanup(func() {
		close(release)
		select { // the stuck call ends before the factory is put back
		case <-returned:
		case <-time.After(5 * time.Second):
		}
		dbSQLSessionFactory = old
	})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := unreachableModel(t, nil)
	n := connectEnds(t, m, "baaytona", 5*time.Second)
	if want := "No answer from 127.0.0.1:5432 in time."; n.problem != want {
		t.Fatalf("problem = %q, want %q", n.problem, want)
	}
}

package app

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

type fakeCassandraExec struct{ pinged int }

func (f *fakeCassandraExec) Page(_ context.Context, _ string, _ []any, _ int, _ []byte) (dbclient.CQLPage, error) {
	f.pinged++
	return dbclient.CQLPage{Rows: [][]dbclient.Cell{{{Text: "5.0.2"}}}}, nil
}
func (f *fakeCassandraExec) Close() {}

// DBX-67: a Cassandra / ScyllaDB connection opens through its own factory
// and is pinged like every other session.
func TestDBOpenSessionCassandra(t *testing.T) {
	f := &fakeCassandraExec{}
	old := dbCassandraSessionFactory
	t.Cleanup(func() { dbCassandraSessionFactory = old })
	var gotPassword string
	dbCassandraSessionFactory = func(_ context.Context, c dbclient.Conn, password string) (dbclient.Session, error) {
		gotPassword = password
		return dbclient.NewCassandraSessionWith(c, f), nil
	}
	c := dbclient.Conn{ID: "wide", Driver: "cassandra", Host: "db.local", Port: 9042}
	sess, tun, err := dbOpenSession(context.Background(), c, t.TempDir(), "pw", dbSSHSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if tun != nil {
		t.Fatal("no SSH configured, but a tunnel was opened")
	}
	if _, ok := sess.(*dbclient.CassandraSession); !ok {
		t.Fatalf("session type %T", sess)
	}
	if f.pinged == 0 || gotPassword != "pw" {
		t.Fatalf("pinged=%d password passed=%v", f.pinged, gotPassword == "pw")
	}
}

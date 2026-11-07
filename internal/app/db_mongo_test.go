package app

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

type fakeMongoExec struct{ pinged int }

func (f *fakeMongoExec) ListDatabases(context.Context) ([]string, error) { return nil, nil }
func (f *fakeMongoExec) Command(_ context.Context, _ string, cmd dbclient.MongoDoc) (dbclient.MongoDoc, error) {
	if len(cmd) > 0 && cmd[0].Key == "ping" {
		f.pinged++
	}
	return dbclient.MongoDoc{{Key: "ok", Value: 1.0}}, nil
}
func (f *fakeMongoExec) Close() {}

// DBX-68: a MongoDB connection opens through its own factory and is
// pinged like every other session.
func TestDBOpenSessionMongo(t *testing.T) {
	f := &fakeMongoExec{}
	old := dbMongoSessionFactory
	t.Cleanup(func() { dbMongoSessionFactory = old })
	var gotPassword string
	dbMongoSessionFactory = func(_ context.Context, c dbclient.Conn, password string) (dbclient.Session, error) {
		gotPassword = password
		return dbclient.NewMongoSessionWith(c, f), nil
	}
	c := dbclient.Conn{ID: "docs", Driver: "mongodb", Host: "db.local", Port: 27017}
	sess, tun, err := dbOpenSession(context.Background(), c, t.TempDir(), "pw", dbSSHSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if tun != nil {
		t.Fatal("no SSH configured, but a tunnel was opened")
	}
	if _, ok := sess.(*dbclient.MongoSession); !ok {
		t.Fatalf("session type %T", sess)
	}
	if f.pinged == 0 || gotPassword != "pw" {
		t.Fatalf("pinged=%d password passed=%v", f.pinged, gotPassword == "pw")
	}
}

// MongoDB is in the add picker, for local and SSH connections.
func TestDBTypePickerHasMongo(t *testing.T) {
	for _, remote := range []bool{false, true} {
		found := false
		for _, it := range dbTypePickerItems(remote) {
			if it.ID == "mongodb" {
				found = true
			}
		}
		if !found {
			t.Fatalf("remote=%v: no MongoDB item", remote)
		}
	}
}

// The add flow asks host, port, an optional user and the database, with
// the MongoDB defaults filled in.
func TestDBAddMongoFlow(t *testing.T) {
	m, _ := covSvcDB(t, "")
	m.databaseStartAdd("mongodb")
	s := m.databases
	if s.pendingConn.Driver != "mongodb" || s.pendingConn.Port != 27017 || s.pendingConn.Database != "test" {
		t.Fatalf("pending = %+v", s.pendingConn)
	}
	m.onDBPrompt(promptKindDBHost, "db.local")
	m.onDBPrompt(promptKindDBPort, "27017")
	m.onDBPrompt(promptKindDBUser, "")
	m.onDBPrompt(promptKindDBName, "shop")
	if s.pendingConn.Host != "db.local" || s.pendingConn.User != "" || s.pendingConn.Database != "shop" || s.pendingConn.ID == "" {
		t.Fatalf("pending = %+v", s.pendingConn)
	}
}

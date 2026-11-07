package app

import (
	"context"
	"errors"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBCloudPaletteItems(t *testing.T) {
	want := map[string]string{
		"db-run-container":  "Databases: Run New Database Container…",
		"db-cloud-discover": "Databases: Discover Cloud Databases…",
		"db-iam-auth":       "Databases: Use Cloud IAM Token Login…",
	}
	for _, it := range databasesPaletteItems() {
		if w, ok := want[it.ID]; ok {
			if it.Title != w {
				t.Errorf("%s title %q", it.ID, it.Title)
			}
			if p, ok := aiActionPolicies[it.ID]; !ok || p.Gate != aiGateNever {
				t.Errorf("%s policy %+v", it.ID, p)
			}
			delete(want, it.ID)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing %v", want)
	}
}

type kubeFakeSession struct {
	dbclient.Session
	closed *int
}

func (k kubeFakeSession) Close() error { *k.closed++; return nil }

// DBX-49: a kube connection opens a port-forward first and connects to
// its local end; closing the session ends the forward, and a failed
// connect ends it at once.
func TestDBKubeOpen(t *testing.T) {
	stopped, closed := 0, 0
	old := dbKubeForward
	t.Cleanup(func() { dbKubeForward = old })
	dbKubeForward = func(_ context.Context, c dbclient.Conn) (int, func(), error) {
		if c.Kube.Pod != "pg-0" || c.Kube.Port != 5432 {
			t.Errorf("forward of %+v", c.Kube)
		}
		return 41234, func() { stopped++ }, nil
	}
	c := dbclient.Conn{ID: "k", Driver: "postgres", Kube: &dbclient.KubeConfig{Pod: "pg-0", Port: 5432}}
	var seen dbclient.Conn
	sess, err := dbKubeOpen(context.Background(), c, func(c dbclient.Conn) (dbclient.Session, error) {
		seen = c
		return kubeFakeSession{closed: &closed}, nil
	})
	if err != nil || seen.Host != "127.0.0.1" || seen.Port != 41234 {
		t.Fatalf("open %v %+v", err, seen)
	}
	if stopped != 0 {
		t.Fatal("forward stopped while the session is open")
	}
	_ = sess.Close()
	if closed != 1 || stopped != 1 {
		t.Fatalf("closed %d stopped %d", closed, stopped)
	}
	_, err = dbKubeOpen(context.Background(), c, func(dbclient.Conn) (dbclient.Session, error) { return nil, errors.New("refused") })
	if err == nil || stopped != 2 {
		t.Fatalf("failed connect: %v stopped %d", err, stopped)
	}
	plain := dbclient.Conn{ID: "p", Driver: "postgres", Host: "db", Port: 5432}
	if _, err := dbKubeOpen(context.Background(), plain, func(c dbclient.Conn) (dbclient.Session, error) { seen = c; return kubeFakeSession{closed: &closed}, nil }); err != nil || seen.Host != "db" || stopped != 2 {
		t.Fatalf("plain conn went through kube: %+v", seen)
	}
}

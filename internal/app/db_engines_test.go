package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// A couchbase connection opens a Couchbase session over the query
// service (DBX-68), with basic auth from the stored password.
func TestDBOpenSessionCouchbase(t *testing.T) {
	var user, pass, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ = r.BasicAuth()
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, `{"results":[{"$1":1}],"status":"success"}`)
	}))
	defer srv.Close()
	c := dbclient.Conn{ID: "cb", Driver: "couchbase", URL: srv.URL, User: "admin"}
	sess, tun, err := dbOpenSession(context.Background(), c, t.TempDir(), "pw-1", dbSSHSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if tun != nil {
		t.Fatal("a tunnel without SSH")
	}
	if _, ok := sess.(*dbclient.CouchbaseSession); !ok {
		t.Fatalf("session = %T", sess)
	}
	if user != "admin" || pass != "pw-1" || !strings.Contains(body, "SELECT 1") {
		t.Fatalf("auth %q/%q body %q", user, pass, body)
	}
}

// The add picker lists the HTTP engines; choosing one asks for the host,
// the port (the engine's default), the user, then the password.
func TestDBAddPickerOffersCouchbase(t *testing.T) {
	found := false
	for _, it := range dbTypePickerItems(false) {
		if it.ID == "engine:couchbase" {
			found = true
		}
	}
	if !found {
		t.Fatal("Couchbase is not in the add picker")
	}
	m := newTestModel(t)
	m.ensureDatabasesLoaded()
	m.databaseStartAdd("engine:couchbase")
	if !m.promptOpen || m.promptKind != promptKindDBHost || m.databases.pendingConn.Driver != "couchbase" || m.databases.pendingConn.Port != 8093 {
		t.Fatalf("prompt %v kind %v conn %+v", m.promptOpen, m.promptKind, m.databases.pendingConn)
	}
}

// A Couchbase bucket node lists its collections in the same session.
func TestDBCouchbaseIsAnHTTPEngineForTheTree(t *testing.T) {
	if !dbclient.IsHTTPEngine("couchbase") {
		t.Fatal("couchbase is not an HTTP engine")
	}
}

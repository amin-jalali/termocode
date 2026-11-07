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

// A libsql connection opens a remote libSQL session (DBX-64) and sends
// the password as the Bearer token.
func TestDBOpenSessionLibSQL(t *testing.T) {
	var auth, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"type":"ok","response":{"type":"execute","result":{"cols":[{"name":"1"}],"rows":[[{"type":"integer","value":"1"}]],"affected_row_count":0}}},{"type":"ok","response":{"type":"close"}}]}`)
	}))
	defer srv.Close()
	c := dbclient.Conn{ID: "turso", Driver: "libsql", URL: srv.URL, Host: "127.0.0.1"}
	sess, tun, err := dbOpenSession(context.Background(), c, t.TempDir(), "tok-"+"1", dbSSHSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if tun != nil {
		t.Fatal("a tunnel without SSH")
	}
	if _, ok := sess.(*dbclient.LibSQLSession); !ok {
		t.Fatalf("session = %T, want *dbclient.LibSQLSession", sess)
	}
	if auth != "Bearer tok-1" || !strings.Contains(body, "SELECT 1") {
		t.Fatalf("auth %q body %q", auth, body)
	}
}

func TestDBLibSQLUsesTheSQLiteDialectAndDetail(t *testing.T) {
	if _, ok := dbAIDialect("libsql").(dbclient.SQLiteDialect); !ok {
		t.Fatalf("dialect = %T", dbAIDialect("libsql"))
	}
	if got := dbConnDetail(dbclient.Conn{Driver: "libsql", Host: "db.turso.io"}); !strings.HasPrefix(got, "libsql") {
		t.Fatalf("detail = %q", got)
	}
}

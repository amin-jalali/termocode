//go:build unix

package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec: a database op from the page that fails says why in one plain
// line: the server's own reason, with the password hidden and no host or
// user, as a conflict (never the generic internal error, which the page
// shows as "Something failed inside termocode").
func TestWebDBFailureSaysWhy(t *testing.T) {
	m, c, _ := webTestDB(t, nil)
	pw := "pw-" + "web-db-local"
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(context.Context, dbclient.Conn, string) (dbclient.Session, error) {
		return nil, errors.New("failed to connect to `user=app database=app`: 10.0.0.5:5432 (db.internal): dial error: dial tcp 10.0.0.5:5432: connect: connection refused " + pw)
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	for _, op := range []struct{ op, args string }{
		{webproto.OpDBTables, `{"conn":"local"}`},
		{webproto.OpDBTest, `{"conn":"local"}`},
		{webproto.OpDBQuery, `{"conn":"local","sql":"SELECT 1"}`},
	} {
		f := webTestCallBg(t, m, c, op.op, op.args)
		msg, _ := f["msg"].(string)
		if webTestErr(f) != "conflict" {
			t.Errorf("%s: code %q, want conflict (%q)", op.op, webTestErr(f), msg)
		}
		if !strings.Contains(msg, "connection refused") || strings.Contains(msg, "\n") {
			t.Errorf("%s: msg %q, want the one-line reason", op.op, msg)
		}
		for _, leak := range []string{pw, "user=app", "10.0.0.5", "db.internal"} {
			if strings.Contains(msg, leak) {
				t.Errorf("%s: msg %q has %q", op.op, msg, leak)
			}
		}
	}
}

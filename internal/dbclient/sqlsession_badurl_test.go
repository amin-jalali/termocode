package dbclient

import (
	"context"
	"strings"
	"testing"
)

// A PostgreSQL URL that does not parse is refused before connecting,
// without the password in the message; it never falls back to the
// local default database.
func TestOpenSQLDBInvalidPostgresSettings(t *testing.T) {
	_, err := NewSQLSession(context.Background(), Conn{Driver: "postgres", URL: "postgres://amy:hunter2pw@db.test:notaport/app"}, "")
	if err == nil || !strings.Contains(err.Error(), "invalid PostgreSQL connection settings") || strings.Contains(err.Error(), "hunter2pw") {
		t.Fatalf("err = %v", err)
	}
}

package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// An encrypted SQLite connection says so in the sidebar detail.
func TestDBConnDetailSQLCipher(t *testing.T) {
	if got := dbConnDetail(dbclient.Conn{Driver: "sqlite", Preset: dbclient.SQLCipherPreset, Path: "v.db"}); got != "sqlcipher" {
		t.Fatalf("detail = %q, want sqlcipher", got)
	}
	if got := dbConnDetail(dbclient.Conn{Driver: "sqlite", Path: "v.db"}); got != "sqlite" {
		t.Fatalf("plain detail = %q", got)
	}
}

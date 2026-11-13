package dbclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A sqlite3 that exits as soon as it starts makes the session fail with
// a clear error. It must not be restarted over and over while the session
// is only being set up (each restart set it up again, without end).
func TestSQLiteThatExitsAtOnceFailsTheSetupOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	bin := filepath.Join(t.TempDir(), "sqlite3")
	script := "#!/bin/sh\nif [ \"$1\" = \"-version\" ]; then echo '3.45.0 2024'; exit 0; fi\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	lookups := 0
	old := lookupSQLite
	lookupSQLite = func() (string, error) {
		lookups++
		if lookups > 20 { // stops a runaway restart loop, which is the bug
			return "", errors.New("too many restarts")
		}
		return bin, nil
	}
	defer func() { lookupSQLite = old }()
	_, err := NewSQLiteSession(context.Background(), Conn{Driver: "sqlite", Path: ":memory:"})
	if err == nil || !strings.Contains(err.Error(), "configure sqlite3") {
		t.Errorf("a sqlite3 that exits at once = %v", err)
	}
	if lookups > 2 {
		t.Errorf("the setup restarted sqlite3 %d times", lookups)
	}
}

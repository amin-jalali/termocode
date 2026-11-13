//go:build db2

package dbclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// With -tags db2 the real driver is linked. It needs IBM's CLI driver
// (clidriver, IBM_DB_HOME); the test skips when it is missing, and the
// live part needs TC_DB2_TEST_DSN="HOSTNAME=…;DATABASE=…;PORT=…;UID=…;PWD=…".
func TestDb2TaggedBuildOpensWithTheCLIDriver(t *testing.T) {
	home := os.Getenv("IBM_DB_HOME")
	if home == "" {
		t.Skip("IBM_DB_HOME is not set: install the IBM CLI driver (clidriver) to run this")
	}
	if _, err := os.Stat(filepath.Join(home, "lib")); err != nil {
		t.Skipf("IBM CLI driver lib missing in %s: %v", home, err)
	}
	if !Db2Built {
		t.Fatal("Db2Built is false in a -tags db2 build")
	}
	dsn := os.Getenv("TC_DB2_TEST_DSN")
	if dsn == "" {
		t.Skip("TC_DB2_TEST_DSN is not set: no Db2 server to open")
	}
	conn, err := db2OpenConnector(dsn)
	if err != nil {
		t.Fatal(err)
	}
	c, err := conn.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

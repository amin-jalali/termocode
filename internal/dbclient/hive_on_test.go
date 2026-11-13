//go:build hive

package dbclient

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
)

// With -tags hive the real driver (gohive) is linked. The live part needs
// a HiveServer2 or Spark Thrift server: TC_HIVE_TEST_ADDR=host:port.
func TestHiveTaggedBuildTalksToHiveServer2(t *testing.T) {
	if !HiveBuilt {
		t.Fatal("HiveBuilt is false in a -tags hive build")
	}
	addr := os.Getenv("TC_HIVE_TEST_ADDR")
	if addr == "" {
		t.Skip("TC_HIVE_TEST_ADDR is not set: no HiveServer2 to open")
	}
	host, ps, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(ps)
	s, err := NewSQLSession(context.Background(), Conn{ID: "h", Driver: "hive", Host: host, Port: port, User: "hive"}, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := s.Run(context.Background(), "SELECT 1", RunOptions{}); r.Err != nil || len(r.Rows) != 1 {
		t.Fatalf("select 1 = %+v", r)
	}
}

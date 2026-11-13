package dbclient

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestRedisKeyspaceAndScanOddReplies(t *testing.T) {
	mode := "ok"
	srv := newScriptResp(t, func(args []string, _ io.Writer) string {
		if r := scriptBase(args); r != "" && !(mode == "selectfail" && strings.EqualFold(args[0], "SELECT") && args[1] != "0") {
			return r
		}
		switch strings.ToUpper(args[0]) {
		case "SELECT":
			return "-ERR DB index is out of range\r\n"
		case "INFO":
			if mode == "infofail" {
				return "-ERR no info\r\n"
			}
			return respBulk("# Keyspace\r\ndb0:keys=5,expires=0\r\ndbx:keys=1\r\ndb3:keys=2,avg_ttl=0\r\njunk\r\n")
		case "SCAN":
			switch mode {
			case "badscan":
				return "*1\r\n" + respBulk("0")
			case "scanfail":
				return "-ERR scan broke\r\n"
			case "dups":
				return "*2\r\n" + respBulk("0") + "*3\r\n" + respBulk("a") + respBulk("a") + respBulk("b")
			case "more":
				return "*2\r\n" + respBulk("7") + "*3\r\n" + respBulk("a") + respBulk("b") + respBulk("c")
			}
			return "*2\r\n" + respBulk("0") + "*0\r\n"
		case "CLUSTER":
			return "-ERR This instance has cluster support disabled\r\n"
		}
		return "+OK\r\n"
	})
	s, err := NewRedisSession(context.Background(), scriptConn(srv.addr(), nil), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	dbs, err := s.Keyspace(ctx)
	if err != nil || len(dbs) < 2 || dbs[0].Index != 0 || dbs[0].Keys != 5 {
		t.Errorf("keyspace = %+v %v", dbs, err)
	}
	mode = "infofail"
	if _, err := s.Keyspace(ctx); err == nil {
		t.Error("INFO failing: want error")
	}
	mode = "badscan"
	if _, _, err := s.ScanKeys(ctx, -1, "*", 10); err == nil || !strings.Contains(err.Error(), "unexpected SCAN reply") {
		t.Errorf("a bad SCAN reply = %v", err)
	}
	mode = "scanfail"
	if _, _, err := s.ScanKeys(ctx, -1, "*", 10); err == nil {
		t.Error("SCAN failing: want error")
	}
	mode = "dups"
	if keys, more, err := s.ScanKeys(ctx, -1, "*", 0); err != nil || more || len(keys) != 2 {
		t.Errorf("duplicates are dropped = %v %v %v", keys, more, err)
	}
	mode = "more"
	if keys, more, err := s.ScanKeys(ctx, -1, "*", 2); err != nil || !more || len(keys) != 2 {
		t.Errorf("a cap = %v %v %v", keys, more, err)
	}
	mode = "ok"
	// A database the server refuses.
	mode = "selectfail"
	if _, _, err := s.ScanKeys(ctx, 9, "*", 5); err == nil {
		t.Error("a database that cannot be selected: want error")
	}
	// A closed session and a server that is gone.
	_ = s.Close()
	if _, err := s.Keyspace(ctx); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("closed = %v", err)
	}
	gone, err := NewRedisSession(ctx, scriptConn(srv.addr(), nil), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gone.Close()
	_ = srv.ln.Close()
	gone.mu.Lock()
	if gone.conn != nil {
		_ = gone.conn.Close()
	}
	gone.mu.Unlock()
	if _, err := gone.Keyspace(ctx); err == nil {
		t.Error("a server that is gone: want error")
	}
}

func TestRedisClusterMastersFromNodes(t *testing.T) {
	nodes := "a 10.0.0.1:7000@17000 master - 0 0 1 connected 0-100\n" +
		"b :7001@17001 myself,master - 0 0 2 connected 101-200\n" +
		"c 10.0.0.3:7002 slave a 0 0 1 connected\n" +
		"d 10.0.0.4:7003 master,fail - 0 0 3 connected\n" +
		"e 10.0.0.5:0 master - 0 0 4 connected\n" +
		"f bad master - 0 0 5 connected\n" +
		"g 10.0.0.6:7006,hostname master - 0 0 6 connected\n" +
		"short\n"
	got := redisClusterMasters(nodes, "10.9.9.9")
	if strings.Join(got, ",") != "10.0.0.1:7000,10.9.9.9:7001,10.0.0.6:7006" {
		t.Errorf("masters = %v", got)
	}
}

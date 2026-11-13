package dbclient

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestClickHouseSessionSetupAndDialect(t *testing.T) {
	d := ClickHouseDialect{}
	if d.Placeholder(2) != "{p2:String}" || d.Limit(0, 0) != "" || d.Limit(5, 10) != " LIMIT 5 OFFSET 10" || d.ReadOnlyBegin() != "" {
		t.Error("ClickHouseDialect basics")
	}
	if q, _ := d.DDL("t"); !strings.HasPrefix(q, "SHOW CREATE TABLE") {
		t.Errorf("DDL = %q", q)
	}
	if d.Explain("SELECT 1", true) != "EXPLAIN PIPELINE SELECT 1" || d.Explain("SELECT 1", false) != "EXPLAIN SELECT 1" {
		t.Error("Explain")
	}
	ctx := context.Background()
	// The default port, the tunnel hook and TLS are used before anything is sent.
	dialed := false
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, net.ErrClosed
	}
	if _, err := NewClickHouseSession(ctx, Conn{Driver: "clickhouse", Host: "db.example", Dial: dial}, ""); err == nil || !dialed {
		t.Errorf("a tunnel that refuses: err %v dialed %v", err, dialed)
	}
	if _, err := NewClickHouseSession(ctx, Conn{Driver: "clickhouse", Host: "db.example", TLS: true, Dial: dial}, ""); err == nil {
		t.Error("TLS through a refusing tunnel: want error")
	}
	if _, err := NewClickHouseSession(ctx, Conn{Driver: "clickhouse", Host: "h", SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}, ""); err == nil {
		t.Error("an unreadable CA: want error")
	}
}

func TestMemcachedKeysListingAndBigValues(t *testing.T) {
	big := strings.Repeat("v", MemcachedMaxValue+10)
	var dials int
	dial := scriptedMemcached(&dials, func(line string, r *bufio.Reader) string {
		switch {
		case strings.HasPrefix(line, "version"):
			return "VERSION 1.6\r\n"
		case strings.HasPrefix(line, "get"):
			return "VALUE big 0 " + strconv.Itoa(len(big)) + "\r\n" + big + "\r\nEND\r\n"
		case strings.HasPrefix(line, "lru_crawler"):
			return "key=alpha exp=-1 size=10\r\nkey=b%20c exp=5 size=20\r\nnokey=1\r\nkey=zeta exp=-1 size=30\r\nEND\r\n"
		}
		return "ERROR\r\n"
	})
	s, _ := NewMemcachedSession(Conn{Host: "h", Dial: dial})
	defer s.Close()
	ctx := context.Background()
	r := s.Run(ctx, "get big", RunOptions{})
	if r.Err != nil || len(r.Rows) != 1 || len(r.Rows[0][1].Text) > MemcachedMaxValue+64 {
		t.Errorf("a big value is cut: %d bytes, %v", len(r.Rows[0][1].Text), r.Err)
	}
	all := s.Run(ctx, "keys", RunOptions{})
	if all.Err != nil || len(all.Rows) != 3 || all.Rows[1][0].Text != "b c" {
		t.Errorf("keys = %+v", all)
	}
	if f := s.Run(ctx, "keys a", RunOptions{}); len(f.Rows) != 2 {
		t.Errorf("keys with a pattern = %+v", f.Rows)
	}
	if cut := s.Run(ctx, "keys", RunOptions{MaxRows: 2}); !cut.Truncated || len(cut.Rows) != 2 {
		t.Errorf("a cut listing = %+v", cut)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{Database: "keys", Object: "alpha"})
	if err != nil || len(in.Columns) != 4 {
		t.Errorf("columns of a key = %+v %v", in, err)
	}
	if ClassifyMemcached("") != KindUnknown || ClassifyMemcached("# only") != KindUnknown {
		t.Error("an empty line has no kind")
	}
	busy := scriptedMemcached(&dials, func(line string, _ *bufio.Reader) string {
		if strings.HasPrefix(line, "lru_crawler") {
			return "BUSY currently locked\r\n"
		}
		return "END\r\n"
	})
	b, _ := NewMemcachedSession(Conn{Host: "h", Dial: busy})
	defer b.Close()
	if r := b.Run(ctx, "keys", RunOptions{}); r.Err != nil || len(r.Rows) != 0 {
		t.Errorf("a busy crawler = %+v", r)
	}
	// A session whose first write breaks.
	broken := scriptedMemcached(&dials, func(string, *bufio.Reader) string { return "" })
	w, _ := NewMemcachedSession(Conn{Host: "h", Dial: broken})
	defer w.Close()
	if r := w.Run(ctx, "get k", RunOptions{}); r.Err == nil {
		t.Error("a server that closes at once: want error")
	}
}

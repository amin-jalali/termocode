//go:build dbint

package dbclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestRedisLiveServer runs against a real Redis or Valkey named by
// TERMOCODE_REDIS_URL (redis://[user:password@]host:port/db). It writes
// only keys under its own prefix and deletes them.
func TestRedisLiveServer(t *testing.T) {
	raw := os.Getenv("TERMOCODE_REDIS_URL")
	if raw == "" {
		unavailableDBInt(t, fmt.Errorf("TERMOCODE_REDIS_URL is not set"))
	}
	c, err := ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	pw := c.Password
	c.Password = ""
	ctx := context.Background()
	s, err := NewRedisSession(ctx, c, pw, nil)
	if err != nil {
		unavailableDBInt(t, err)
	}
	defer s.Close()
	prefix := fmt.Sprintf("tc_dbint_%d:", os.Getpid())
	defer func() {
		keys, _, _ := s.ScanKeys(ctx, -1, RedisGlobEscape(prefix)+"*", 100000)
		for _, k := range keys {
			_, _ = s.Do(ctx, []string{"DEL", k})
		}
	}()
	for i := 0; i < 1500; i++ {
		if _, err := s.Do(ctx, []string{"HSET", prefix + "h", fmt.Sprintf("f%04d", i), "v"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Do(ctx, []string{"SET", fmt.Sprintf("%sk:%04d", prefix, i), "x"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range [][]string{{"RPUSH", prefix + "l", "a", "b", "c"}, {"SADD", prefix + "s", "m1", "m2"}, {"ZADD", prefix + "z", "1", "a", "2", "b"},
		{"XADD", prefix + "x", "*", "event", "login"}, {"SET", prefix + "bin", "\x1b[31m\xff"}, {"EXPIRE", prefix + "h", "600"}} {
		if _, err := s.Do(ctx, cmd); err != nil {
			t.Fatalf("%v: %v", cmd, err)
		}
	}
	keys, more, err := s.ScanKeys(ctx, -1, RedisGlobEscape(prefix+"k:")+"*", 1000)
	if err != nil || len(keys) != 1000 || !more {
		t.Fatalf("scan cap: %d keys more=%v err=%v", len(keys), more, err)
	}
	info, err := s.KeyInfo(ctx, -1, prefix+"h")
	if err != nil || info.Type != "hash" || info.Len != 1500 || info.TTL <= 0 {
		t.Fatalf("info %+v %v", info, err)
	}
	rows, from, pages := 0, "", 0
	for {
		p, err := s.ValuePage(ctx, -1, prefix+"h", "hash", from, 100)
		if err != nil {
			t.Fatal(err)
		}
		rows += len(p.Rows)
		pages++
		if p.Next == "" {
			break
		}
		from = p.Next
	}
	if rows < 1500 || pages < 2 {
		t.Fatalf("HSCAN paging: %d rows in %d pages", rows, pages)
	}
	for typ, key := range map[string]string{"list": "l", "set": "s", "zset": "z", "stream": "x", "string": "bin"} {
		p, err := s.ValuePage(ctx, -1, prefix+key, typ, "", 100)
		if err != nil || len(p.Rows) == 0 {
			t.Fatalf("%s: %+v %v", typ, p, err)
		}
	}
	res := s.Run(ctx, "GET "+prefix+"bin", RunOptions{})
	if res.Err != nil || strings.ContainsAny(res.Rows[0][0].Text, "\x1b") {
		t.Fatalf("binary value: %+v", res)
	}
	ro := NewRedisReadOnlySession(c, pw, nil)
	defer ro.Close()
	if r := ro.Run(ctx, "SET "+prefix+"ro 1", RunOptions{}); r.Err == nil {
		t.Fatal("a read-only session wrote")
	}
	if r := ro.Run(ctx, "KEYS "+prefix+"k:000*", RunOptions{MaxRows: 100}); r.Err != nil || len(r.Rows) != 10 {
		t.Fatalf("KEYS rewrite: %+v", r)
	}
}

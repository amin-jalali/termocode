package dbclient

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRedisNewKeyArgsForEveryTypeAndRefusal(t *testing.T) {
	for _, c := range []struct {
		typ, key, val string
		want          string
		err           string
	}{
		{"string", "k", "v", "SET k v NX", ""},
		{"hash", "k", "name Ada age 3", "HSET k name Ada age 3", ""},
		{"list", "k", "first", "RPUSH k first", ""},
		{"set", "k", "m", "SADD k m", ""},
		{"zset", "k", "1.5 ada", "ZADD k NX 1.5 ada", ""},
		{"stream", "k", "a 1", "XADD k * a 1", ""},
		{"json", "k", `{"a":1}`, `JSON.SET k $ {"a":1} NX`, ""},
		{"string", " ", "v", "", "type a key name"},
		{"hash", "k", "name", "", "field value pairs"},
		{"hash", "k", `"open`, "", ""},
		{"list", "k", "", "", "first item"},
		{"set", "k", "", "", "first item"},
		{"zset", "k", "1.5", "", "score and a member"},
		{"zset", "k", "x ada", "", "must be a number"},
		{"zset", "k", `"open`, "", ""},
		{"stream", "k", "a", "", "field value pairs"},
		{"json", "k", "{nope", "", "not valid JSON"},
		{"graph", "k", "v", "", "unknown type"},
	} {
		got, err := RedisNewKeyArgs(c.typ, c.key, c.val)
		if c.want != "" {
			if err != nil || strings.Join(got, " ") != c.want {
				t.Errorf("%s %q = %v, %v, want %q", c.typ, c.val, got, err, c.want)
			}
			continue
		}
		if err == nil || (c.err != "" && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s %q: %v, want %q", c.typ, c.val, err, c.err)
		}
	}
}

func TestRedisReplyShapes(t *testing.T) {
	pairs := []any{[]byte("a"), int64(1), []byte("b"), []any{[]byte("x"), []byte("y")}}
	res, ok := RedisPairsResult(pairs)
	if !ok || len(res.Rows) != 2 || res.Rows[1][1].Text != "x=y" {
		t.Errorf("pairs = %+v %v", res, ok)
	}
	if res, ok := RedisPairsResult([]any{}); !ok || len(res.Notices) != 1 {
		t.Errorf("empty pairs = %+v %v", res, ok)
	}
	for _, bad := range []any{"x", []any{[]byte("odd")}, []any{int64(1), int64(2)}} {
		if _, ok := RedisPairsResult(bad); ok {
			t.Errorf("%v is no pair list", bad)
		}
	}
	if got := redisFlat(nil); got != "(nil)" {
		t.Errorf("redisFlat(nil) = %q", got)
	}
	if got := redisFlat([]any{int64(1), []any{[]byte("a"), int64(2)}, nil}); got != "1, a=2, (nil)" && got != "1, a, 2, (nil)" {
		t.Errorf("redisFlat nested = %q", got)
	}
	recs := []any{
		[]any{[]byte("name"), []byte("g1"), []byte("pending"), int64(2)},
		[]any{[]byte("name"), []byte("g2"), []byte("lag"), int64(5)},
	}
	rr, ok := RedisRecordsResult(recs)
	if !ok || len(rr.Columns) != 3 || len(rr.Rows) != 2 || rr.Rows[1][1].Text != "" {
		t.Errorf("records = %+v %v", rr, ok)
	}
	if rr, ok := RedisRecordsResult([]any{}); !ok || len(rr.Notices) != 1 {
		t.Errorf("no records = %+v %v", rr, ok)
	}
	if _, ok := RedisRecordsResult("x"); ok {
		t.Error("a string is no record list")
	}
	if _, ok := RedisRecordsResult([]any{"x"}); ok {
		t.Error("a record that is no pair list")
	}
	nodes := "07c3 127.0.0.1:7000@17000 myself,master - 0 0 1 connected 0-5460\nshort line\n"
	nr := RedisClusterNodesResult(nodes)
	if len(nr.Rows) != 1 || nr.Rows[0][1].Text != "127.0.0.1:7000" || nr.Rows[0][5].Text != "0-5460" {
		t.Errorf("cluster nodes = %+v", nr)
	}
	if nr := RedisClusterNodesResult("junk"); len(nr.Notices) != 1 {
		t.Errorf("no nodes = %+v", nr)
	}
	for _, c := range []struct {
		cmd, sub string
		reply    any
		ok       bool
	}{
		{"MEMORY", "STATS", pairs, true}, {"XINFO", "STREAM", pairs, true}, {"PUBSUB", "NUMSUB", pairs, true}, {"TS.INFO", "", pairs, true},
		{"XINFO", "GROUPS", recs, true}, {"MODULE", "LIST", recs, true},
		{"CLUSTER", "NODES", []byte(nodes), true}, {"CLUSTER", "NODES", nodes, true}, {"CLUSTER", "NODES", int64(1), false},
		{"GET", "", "x", false},
	} {
		if _, ok := redisMoreGrid(c.cmd, c.sub, c.reply); ok != c.ok {
			t.Errorf("redisMoreGrid(%s %s) ok = %v", c.cmd, c.sub, ok)
		}
	}
}

func TestRedisRedirectsAndSentinelAddress(t *testing.T) {
	for _, c := range []struct {
		err     string
		addr    string
		ask, ok bool
	}{
		{"MOVED 3999 127.0.0.1:6381", "127.0.0.1:6381", false, true},
		{"ASK 3999 [::1]:6381", "[::1]:6381", true, true},
		{"MOVED 3999", "", false, false},
		{"MOVED 3999 nonsense", "", false, false},
		{"ERR other", "", false, false},
	} {
		addr, ask, ok := redisRedirectOf(errors.New(c.err))
		if addr != c.addr || ask != c.ask || ok != c.ok {
			t.Errorf("redisRedirectOf(%q) = %q %v %v", c.err, addr, ask, ok)
		}
	}
	if _, _, ok := redisRedirectOf(nil); ok {
		t.Error("no error, no redirect")
	}
	if got, err := redisSentinelAddr([]any{[]byte("10.0.0.5"), []byte("6379")}); err != nil || got != "10.0.0.5:6379" {
		t.Errorf("sentinel = %q, %v", got, err)
	}
	for _, bad := range []any{nil, []any{[]byte("h")}, []any{[]byte(""), []byte("6379")}, []any{[]byte("h"), []byte("")}} {
		if _, err := redisSentinelAddr(bad); err == nil {
			t.Errorf("sentinel %v: want error", bad)
		}
	}
}

func TestRedisExportKeysReadsEveryType(t *testing.T) {
	types := map[string]string{"s": "string", "h": "hash", "l": "list", "st": "set", "z": "zset", "x": "stream", "j": "ReJSON-RL", "gone": "none", "other": "module"}
	do := func(_ context.Context, a []string) (any, error) {
		switch a[0] {
		case "TYPE":
			return types[a[1]], nil
		case "PTTL":
			if a[1] == "s" {
				return int64(1500), nil
			}
			return int64(-1), nil
		case "GET":
			return []byte("value"), nil
		case "HGETALL":
			return []any{[]byte("f"), []byte("v")}, nil
		case "LRANGE":
			return []any{[]byte("b"), []byte("a")}, nil
		case "SMEMBERS":
			return []any{[]byte("b"), []byte("a")}, nil
		case "ZRANGE":
			return []any{[]byte("m"), []byte("1.5")}, nil
		case "XRANGE":
			return []any{[]any{[]byte("1-0"), []any{[]byte("f"), []byte("v")}}, []any{[]byte("bad")}}, nil
		case "JSON.GET":
			if a[1] == "j" {
				return []byte(`{"a":1}`), nil
			}
			return []byte("not json"), nil
		}
		return nil, errors.New("unexpected " + a[0])
	}
	got, err := RedisExportKeys(context.Background(), do, []string{"s", "h", "l", "st", "z", "x", "j", "gone", "other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("entries = %+v", got)
	}
	if got[0].TTLms != 1500 || got[1].TTLms != 0 || got[0].Value != "value" {
		t.Errorf("string entry = %+v", got[0])
	}
	if set := got[3].Value.([]string); set[0] != "a" || set[1] != "b" {
		t.Errorf("a set is sorted: %v", set)
	}
	if z := got[4].Value.([]redisZEntry); len(z) != 1 || z[0].Score != "1.5" {
		t.Errorf("zset = %+v", z)
	}
	if x := got[5].Value.([]redisXEntry); len(x) != 1 || x[0].ID != "1-0" {
		t.Errorf("stream = %+v", x)
	}
	if got[7].Value != nil {
		t.Errorf("an unknown type has no value: %+v", got[7])
	}
	// The same JSON text that is not valid stays text.
	types["j"] = "ReJSON-RL"
	got, _ = RedisExportKeys(context.Background(), func(c context.Context, a []string) (any, error) {
		if a[0] == "JSON.GET" {
			return []byte("not json"), nil
		}
		return do(c, a)
	}, []string{"j"})
	if got[0].Value != "not json" {
		t.Errorf("invalid JSON stays text: %+v", got[0])
	}
	// Failures stop the export and keep what was read.
	fail := errors.New("link down")
	for _, at := range []string{"TYPE", "PTTL", "GET"} {
		out, err := RedisExportKeys(context.Background(), func(c context.Context, a []string) (any, error) {
			if a[0] == at && a[1] == "h" || at != "GET" && a[0] == at && a[1] == "h" {
				return nil, fail
			}
			if at == "GET" && a[0] == "GET" {
				return nil, fail
			}
			return do(c, a)
		}, []string{"l", "h", "s"})
		if err == nil || !errors.Is(err, fail) {
			t.Errorf("%s failing = %v", at, err)
		}
		if at != "GET" && len(out) != 1 {
			t.Errorf("%s failing keeps the first entry: %+v", at, out)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := RedisExportKeys(ctx, do, []string{"s"}); err == nil || len(out) != 0 {
		t.Errorf("a cancelled export = %v, %v", out, err)
	}
}

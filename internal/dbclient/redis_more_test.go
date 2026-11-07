package dbclient

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The new-key form builds one safe command per type: SET and ZADD use NX,
// JSON must be valid, hash and stream need field/value pairs.
func TestRedisNewKeyArgs(t *testing.T) {
	for _, tc := range []struct {
		typ, value string
		want       []string
	}{
		{"string", "hello world", []string{"SET", "k", "hello world", "NX"}},
		{"hash", `name "Ada L"`, []string{"HSET", "k", "name", "Ada L"}},
		{"list", "a", []string{"RPUSH", "k", "a"}},
		{"set", "m", []string{"SADD", "k", "m"}},
		{"zset", "1.5 m", []string{"ZADD", "k", "NX", "1.5", "m"}},
		{"stream", "event login user 7", []string{"XADD", "k", "*", "event", "login", "user", "7"}},
		{"json", `{"a":1}`, []string{"JSON.SET", "k", "$", `{"a":1}`, "NX"}},
	} {
		got, err := RedisNewKeyArgs(tc.typ, "k", tc.value)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %q = %q %v, want %q", tc.typ, tc.value, got, err, tc.want)
		}
	}
	for _, bad := range [][3]string{
		{"string", "", "v"}, {"hash", "k", "only"}, {"zset", "k", "x m"}, {"stream", "k", "a"},
		{"json", "k", "{bad"}, {"nope", "k", "v"}, {"list", "k", ""},
	} {
		if got, err := RedisNewKeyArgs(bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("%q accepted: %q", bad, got)
		}
	}
	if k, _ := ClassifyRedis([]string{"JSON.SET", "k", "$", "1", "NX"}); k != RedisWrite {
		t.Errorf("JSON.SET is %v, want write", k)
	}
}

func TestRedisJSONGetArgs(t *testing.T) {
	if got := RedisJSONGet("k", ""); !reflect.DeepEqual(got, []string{"JSON.GET", "k", "$"}) {
		t.Errorf("default path %q", got)
	}
	if got := RedisJSONGet("k", " $.a[0] "); !reflect.DeepEqual(got, []string{"JSON.GET", "k", "$.a[0]"}) {
		t.Errorf("path %q", got)
	}
}

// Each module type opens its own read-only info command.
func TestRedisModuleInfoArgs(t *testing.T) {
	for typ, want := range map[string][]string{
		"TSDB-TYPE": {"TS.INFO", "k"},
		"MBbloom--": {"BF.INFO", "k"},
		"MBbloomCF": {"CF.INFO", "k"},
		"TopK-TYPE": {"TOPK.INFO", "k"},
		"CMSk-TYPE": {"CMS.INFO", "k"},
		"ReJSON-RL": {"JSON.GET", "k", "$"},
		"stream":    {"XINFO", "GROUPS", "k"},
	} {
		got, ok := RedisModuleInfoArgs(typ, "k")
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %q %v, want %q", typ, got, ok, want)
		}
		if k, _ := ClassifyRedis(got); k != RedisRead {
			t.Errorf("%q is %v, want read", got, k)
		}
	}
	if _, ok := RedisModuleInfoArgs("string", "k"); ok {
		t.Error("a string has no module info")
	}
}

// Module reads run at once; MODULE LIST is a read while the rest of
// MODULE stays on the never-list.
func TestRedisModuleCommandsClassify(t *testing.T) {
	for _, c := range [][]string{
		{"MODULE", "LIST"}, {"TS.INFO", "k"}, {"TS.RANGE", "k", "-", "+"}, {"BF.EXISTS", "k", "a"},
		{"FT._LIST"}, {"FT.INFO", "idx"}, {"FT.SEARCH", "idx", "*"}, {"CF.EXISTS", "k", "a"},
	} {
		if k, why := ClassifyRedis(c); k != RedisRead {
			t.Errorf("%q is %v (%s), want read", c, k, why)
		}
	}
	if k, _ := ClassifyRedis([]string{"MODULE", "LOAD", "/x.so"}); k != RedisDangerous {
		t.Errorf("MODULE LOAD is %v", k)
	}
	if !RedisAIRead([]string{"MODULE", "LIST"}) {
		t.Error("the AI may read MODULE LIST")
	}
}

// Flat field/value replies (MEMORY STATS, TS.INFO, BF.INFO, PUBSUB NUMSUB)
// become field | value rows; nested values are one line.
func TestRedisAdminGridPairs(t *testing.T) {
	reply := []any{[]byte("peak.allocated"), int64(1024), []byte("db.0"), []any{[]byte("overhead.hashtable.main"), int64(72)}}
	res, ok := RedisAdminGrid([]string{"MEMORY", "STATS"}, reply)
	if !ok {
		t.Fatal("MEMORY STATS has no grid")
	}
	if len(res.Columns) != 2 || res.Columns[0].Name != "field" || len(res.Rows) != 2 {
		t.Fatalf("grid %+v", res)
	}
	if res.Rows[0][0].Text != "peak.allocated" || res.Rows[0][1].Text != "1024" {
		t.Errorf("row 0 %+v", res.Rows[0])
	}
	if got := res.Rows[1][1].Text; got != "overhead.hashtable.main=72" {
		t.Errorf("nested %q", got)
	}
	for _, c := range [][]string{{"TS.INFO", "k"}, {"BF.INFO", "k"}, {"PUBSUB", "NUMSUB", "a"}, {"FT.INFO", "i"}} {
		if _, ok := RedisAdminGrid(c, reply); !ok {
			t.Errorf("%q has no grid", c)
		}
	}
	if _, ok := RedisAdminGrid([]string{"MEMORY", "STATS"}, []any{[]byte("odd")}); ok {
		t.Error("an odd reply is not a pair grid")
	}
}

// Lists of records (XINFO GROUPS, XINFO CONSUMERS, MODULE LIST) become one
// row per record with the field names as columns.
func TestRedisAdminGridRecords(t *testing.T) {
	reply := []any{
		[]any{[]byte("name"), []byte("g1"), []byte("consumers"), int64(2), []byte("pending"), int64(5)},
		[]any{[]byte("name"), []byte("g2"), []byte("consumers"), int64(0), []byte("lag"), int64(1)},
	}
	res, ok := RedisAdminGrid([]string{"XINFO", "GROUPS", "s"}, reply)
	if !ok {
		t.Fatal("XINFO GROUPS has no grid")
	}
	var cols []string
	for _, c := range res.Columns {
		cols = append(cols, c.Name)
	}
	if !reflect.DeepEqual(cols, []string{"name", "consumers", "pending", "lag"}) {
		t.Fatalf("columns %q", cols)
	}
	if res.Rows[1][0].Text != "g2" || res.Rows[1][2].Text != "" || res.Rows[1][3].Text != "1" {
		t.Errorf("row 1 %+v", res.Rows[1])
	}
	if res, ok := RedisAdminGrid([]string{"XINFO", "GROUPS", "s"}, []any{}); !ok || len(res.Notices) == 0 {
		t.Errorf("no groups: %+v %v", res, ok)
	}
	if _, ok := RedisAdminGrid([]string{"MODULE", "LIST"}, []any{[]any{[]byte("name"), []byte("ReJSON"), []byte("ver"), int64(20612)}}); !ok {
		t.Error("MODULE LIST has no grid")
	}
}

func TestRedisClusterNodesGrid(t *testing.T) {
	text := "07c37 127.0.0.1:30004@31004 slave e7d1e 0 1426238317239 4 connected\n" +
		"67ed2 127.0.0.1:30002@31002 master - 0 1426238316232 2 connected 5461-10922\n"
	res, ok := RedisAdminGrid([]string{"CLUSTER", "NODES"}, []byte(text))
	if !ok || len(res.Rows) != 2 {
		t.Fatalf("grid %+v %v", res, ok)
	}
	if got := res.Rows[1]; got[0].Text != "67ed2" || got[1].Text != "127.0.0.1:30002" || got[2].Text != "master" || got[5].Text != "5461-10922" {
		t.Errorf("row %+v", got)
	}
	if got := res.Rows[0][3].Text; got != "e7d1e" {
		t.Errorf("master of the replica %q", got)
	}
}

// MOVED and ASK name the node that owns the key; other errors do not.
func TestRedisRedirectOf(t *testing.T) {
	for _, tc := range []struct {
		err  error
		addr string
		ask  bool
		ok   bool
	}{
		{errors.New("MOVED 3999 10.0.0.2:6381"), "10.0.0.2:6381", false, true},
		{errors.New("ASK 3999 10.0.0.3:6382"), "10.0.0.3:6382", true, true},
		{errors.New("ERR wrong"), "", false, false},
		{errors.New("MOVED 3999"), "", false, false},
		{nil, "", false, false},
	} {
		addr, ask, ok := redisRedirectOf(tc.err)
		if addr != tc.addr || ask != tc.ask || ok != tc.ok {
			t.Errorf("%v = %q %v %v", tc.err, addr, ask, ok)
		}
	}
}

func TestRedisSentinelAddr(t *testing.T) {
	if got, err := redisSentinelAddr([]any{[]byte("10.0.0.5"), []byte("6380")}); err != nil || got != "10.0.0.5:6380" {
		t.Errorf("addr %q %v", got, err)
	}
	for _, bad := range []any{nil, []any{}, []any{[]byte("h")}, "x"} {
		if _, err := redisSentinelAddr(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if got := redisTargetOf(Conn{Host: "s", Params: map[string]string{"sentinelMaster": "mymaster"}}).addr; got != "s:26379" {
		t.Errorf("sentinel default port: %q", got)
	}
}

// Export reads each key with the command of its type and keeps the TTL.
func TestRedisExportKeys(t *testing.T) {
	data := map[string][]any{
		"TYPE s": {"string"}, "PTTL s": {int64(-1)}, "GET s": {[]byte("v")},
		"TYPE h": {"hash"}, "PTTL h": {int64(5000)}, "HGETALL h": {[]any{[]byte("f"), []byte("1")}},
		"TYPE z": {"zset"}, "PTTL z": {int64(-1)}, "ZRANGE z 0 -1 WITHSCORES": {[]any{[]byte("m"), []byte("2")}},
		"TYPE gone": {"none"},
	}
	do := func(_ context.Context, args []string) (any, error) {
		v, ok := data[strings.Join(args, " ")]
		if !ok {
			return nil, errors.New("unexpected " + strings.Join(args, " "))
		}
		return v[0], nil
	}
	got, err := RedisExportKeys(context.Background(), do, []string{"s", "h", "z", "gone"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := `[{"key":"s","type":"string","value":"v"},{"key":"h","type":"hash","ttl_ms":5000,"value":{"f":"1"}},{"key":"z","type":"zset","value":[{"member":"m","score":"2"}]}]`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestRedisMessagesResult(t *testing.T) {
	res := RedisMessagesResult([]RedisMessage{{Channel: "news", Pattern: "n*", Data: "hi\x1b"}}, false)
	if len(res.Rows) != 1 || res.Rows[0][1].Text != "news" || strings.Contains(res.Rows[0][3].Text, "\x1b") {
		t.Errorf("rows %+v", res.Rows)
	}
	if res := RedisMessagesResult(nil, false); len(res.Notices) == 0 {
		t.Error("no note when nothing arrived")
	}
}

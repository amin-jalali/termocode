package dbclient

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/gomodule/redigo/redis"
)

// Every edit the value view offers changes the key on the server the way
// its builder says.
func TestRedisEditBuildersChangeTheServerValue(t *testing.T) {
	ctx := context.Background()
	f := dbtest.NewFakeRedis()
	f.HSet(0, "h", "a", "1", "b", "2")
	f.RPush(0, "l", "x", "y", "x")
	f.SAdd(0, "s", "m1", "m2")
	f.ZAdd(0, "z", 1, "low")
	f.XAdd(0, "x", "1-0", "f", "v")
	f.SetString(0, "str", "old")
	f.Expire(0, "str", time.Hour)
	s := redisTestSession(t, f, Conn{}, "")
	for _, cmd := range [][]string{
		RedisHDel("h", "a"), RedisLSet("l", 1, "Y"), RedisRPush("l", "tail"), RedisLRem("l", "x"),
		RedisSAdd("s", "m3"), RedisSRem("s", "m1"), RedisZAdd("z", "2.5", "mid"), RedisZRem("z", "low"),
		RedisXAdd("x", []string{"k", "v2"}), RedisXDel("x", "1-0"), RedisSetString("str", "new", false),
	} {
		if _, err := s.Do(ctx, cmd); err != nil {
			t.Fatalf("%q: %v", cmd, err)
		}
	}
	if got := f.Hash(0, "h"); !reflect.DeepEqual(got, map[string]string{"b": "2"}) {
		t.Errorf("hash = %v", got)
	}
	page := func(key, typ string) [][]string {
		p, err := s.ValuePage(ctx, 0, key, typ, "", 0)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		return p.Rows
	}
	if got := page("l", "list"); !reflect.DeepEqual(got, [][]string{{"0", "Y"}, {"1", "x"}, {"2", "tail"}}) {
		t.Errorf("list = %v", got)
	}
	if got := page("s", "set"); len(got) != 2 {
		t.Errorf("set = %v", got)
	}
	if got := page("z", "zset"); !reflect.DeepEqual(got, [][]string{{"2.5", "mid"}}) {
		t.Errorf("zset = %v", got)
	}
	if got := page("x", "stream"); len(got) != 1 || got[0][1] != "k=v2" {
		t.Errorf("stream = %v", got)
	}
	if f.String(0, "str") != "new" || f.TTL(0, "str") > 0 {
		t.Errorf("SET without KEEPTTL must replace the value and drop the TTL: %q %v", f.String(0, "str"), f.TTL(0, "str"))
	}
	if _, err := s.Do(ctx, RedisDel("str")); err != nil || f.Exists(0, "str") {
		t.Errorf("DEL: %v", err)
	}
}

// A console reply that is an array is shown as numbered rows; nested
// arrays get dotted numbers; the row cap holds.
func TestRedisReplyResultArrays(t *testing.T) {
	reply := []any{[]byte("a"), int64(7), nil, []any{[]byte("x"), []any{}}, "status", redis.Error("ERR bad"), 1.5}
	r := RedisReplyResult(reply, 0)
	var got []string
	for _, row := range r.Rows {
		got = append(got, row[0].Text+"="+row[1].Text)
	}
	want := []string{"1=a", "2=7", "3=", "4.1=x", "4.2=(empty array)", "5=status", "6=(error) ERR bad", "7=1.5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %q", got)
	}
	if !r.Rows[2][1].Null || r.Rows[1][1].Kind != "number" {
		t.Errorf("cells: nil must be NULL, int a number: %+v", r.Rows)
	}
	if r := RedisReplyResult([]any{}, 0); len(r.Rows) != 0 || !reflect.DeepEqual(r.Notices, []string{"(empty array)"}) {
		t.Errorf("empty array = %+v", r)
	}
	if r := RedisReplyResult([]any{int64(1), int64(2), int64(3)}, 2); len(r.Rows) != 2 || !r.Truncated {
		t.Errorf("cap = %+v", r)
	}
}

// Redis has no console transactions; the session says so.
func TestRedisSessionInterfaceBasics(t *testing.T) {
	ctx := context.Background()
	f := dbtest.NewFakeRedis()
	f.SetString(3, "k", "v")
	s := redisTestSession(t, f, Conn{DB: 3}, "")
	for _, err := range []error{s.Begin(ctx), s.Commit(ctx), s.Rollback(ctx)} {
		if err == nil || !strings.Contains(err.Error(), "no transactions") {
			t.Errorf("tx = %v", err)
		}
	}
	if s.ReadOnly() || s.DB() != 3 {
		t.Errorf("ReadOnly=%v DB=%d", s.ReadOnly(), s.DB())
	}
	ro, _ := s.ReadOnlySession()
	if !ro.(*RedisSession).ReadOnly() {
		t.Error("read-only clone is writable")
	}
	_ = ro.Close()
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || !reflect.DeepEqual(in.Databases, []string{"db3"}) {
		t.Errorf("introspect = %v %v", in.Databases, err)
	}
	_ = s.Close()
	if err := s.Ping(ctx); err == nil || err.Error() != "the session is closed" {
		t.Errorf("Ping after Close = %v", err)
	}
	for k, want := range map[RedisKind]string{RedisRead: "read", RedisWrite: "write", RedisDangerous: "dangerous", RedisRefused: "refused"} {
		if k.String() != want {
			t.Errorf("%d.String() = %q", k, k.String())
		}
	}
}

// Console input errors are shown without sending anything.
func TestRedisRunInputErrors(t *testing.T) {
	f := dbtest.NewFakeRedis()
	s := redisTestSession(t, f, Conn{}, "")
	before := len(f.Commands())
	for line, want := range map[string]string{
		"":                 "no command",
		`GET "a`:           "unbalanced quotes",
		`GET 'a`:           "unbalanced quotes",
		"KEYS":             "KEYS takes one pattern",
		"KEYS a b":         "KEYS takes one pattern",
		"MULTI":            "",
		`GET "a"b`:         "closing quote",
		"   \t ":           "no command",
		"SUBSCRIBE chan-1": "",
	} {
		r := s.Run(context.Background(), line, RunOptions{})
		if r.Err == nil || (want != "" && !strings.Contains(r.Err.Error(), want)) {
			t.Errorf("Run(%q) = %v, want %q", line, r.Err, want)
		}
	}
	if got := f.Commands()[before:]; len(got) != 0 {
		t.Errorf("sent %q", got)
	}
	if _, err := s.DoDB(context.Background(), 0, nil); err == nil {
		t.Error("empty DoDB was accepted")
	}
}

// KEYS without a row cap scans up to the default cap; a key in another
// database is reached with SELECT on the same connection.
func TestRedisKeysDefaultCapAndOtherDatabase(t *testing.T) {
	f := dbtest.NewFakeRedis()
	for i := 0; i < 5; i++ {
		f.SetString(0, "k"+strconv.Itoa(i), "v")
	}
	f.SetString(4, "other", "v4")
	s := redisTestSession(t, f, Conn{}, "")
	r := s.Run(context.Background(), "KEYS k*", RunOptions{})
	if r.Err != nil || len(r.Rows) != 5 || r.Truncated {
		t.Fatalf("KEYS = %+v", r)
	}
	keys, more, err := s.ScanKeys(context.Background(), 4, "*", 0)
	if err != nil || more || !reflect.DeepEqual(keys, []string{"other"}) {
		t.Fatalf("db 4 keys = %v %v %v", keys, more, err)
	}
	if !f.Sent("SELECT") {
		t.Error("no SELECT for db 4")
	}
	info, err := s.KeyInfo(context.Background(), 0, "missing")
	if err != nil || info.Type != "none" {
		t.Errorf("missing key = %+v %v", info, err)
	}
	if _, err := s.ValuePage(context.Background(), 0, "k1", "module-type", "", 10); err == nil || !strings.Contains(err.Error(), "cannot be shown") {
		t.Errorf("unknown type = %v", err)
	}
}

// After the server drops the connection, the next command dials again.
func TestRedisReconnectsAfterDroppedConnection(t *testing.T) {
	f := dbtest.NewFakeRedis()
	f.SetString(0, "k", "v")
	dials := 0
	var conns []net.Conn
	dial := func(ctx context.Context, n, a string) (net.Conn, error) {
		dials++
		c, err := f.Dial(ctx, n, a)
		conns = append(conns, c)
		return c, err
	}
	s, err := NewRedisSession(context.Background(), Conn{Driver: "redis", Host: "h", Port: 6379}, "", dial)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = conns[0].Close() // the connection went away
	// The command that hits the dead connection fails (it is not sent
	// twice: a write must never run two times)...
	if r := s.Run(context.Background(), "GET k", RunOptions{}); r.Err == nil {
		t.Fatalf("command on a dead connection = %+v", r)
	}
	// ...and the next one dials again and works.
	r := s.Run(context.Background(), "GET k", RunOptions{})
	if r.Err != nil || dials != 2 || r.Rows[0][0].Text != "v" {
		t.Fatalf("after drop: %+v (dials %d)", r, dials)
	}
}

func TestRedisPlainDialRefused(t *testing.T) {
	port := closedPort(t)
	_, err := NewRedisSession(connectCtx(t), Conn{Driver: "redis", Host: "127.0.0.1", Port: port}, "pw-123456", nil)
	if err == nil || !strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "pw-123456") {
		t.Fatalf("err = %v", err)
	}
}

func TestRedisTLSSettingErrors(t *testing.T) {
	f := dbtest.NewFakeRedis()
	_, err := NewRedisSession(context.Background(), Conn{Driver: "redis", Host: "h", Port: 1, SSL: &SSLConfig{Mode: "sometimes"}}, "", f.Dial)
	if err == nil || !strings.Contains(err.Error(), "unknown SSL mode") {
		t.Errorf("bad mode = %v", err)
	}
	s, err := NewRedisSession(context.Background(), Conn{Driver: "redis", Host: "h", Port: 1, SSL: &SSLConfig{Mode: "disable"}}, "", f.Dial)
	if err != nil {
		t.Fatalf("ssl disable without tls: %v", err)
	}
	_ = s.Close()
}

// A cluster node answers MOVED: the user learns why.
func TestRedisErrorExplainsClusterRedirect(t *testing.T) {
	err := redisError(redis.Error("MOVED 3999 10.0.0.2:6381"), "")
	if err == nil || !strings.Contains(err.Error(), "cluster node") {
		t.Fatalf("err = %v", err)
	}
	if redisError(nil, "x") != nil {
		t.Fatal("nil error")
	}
	if err := redisError(errors.New("dial: context deadline exceeded"), ""); err == nil {
		t.Fatal("lost")
	}
}

func TestRedisNextStreamIDMalformed(t *testing.T) {
	for in, want := range map[string]string{"5-1": "5-2", "nodash": "nodash", "5-x": "5-x"} {
		if got := redisNextStreamID(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// Console quoting round-trips control characters, bad UTF-8 and bidi
// marks, and shows them escaped so a value cannot fake the screen.
func TestRedisEscapesRoundTrip(t *testing.T) {
	args, err := ParseRedisCommand(`SET k "a\r\t\b\a\xZZ"`)
	if err != nil || args[2] != "a\r\t\b\axZZ" {
		t.Fatalf("parse = %q %v", args, err)
	}
	for _, v := range []string{"a\rb\tc", "bad\xffbyte", "rtl‮evil", "line\nnext\u0085"} {
		q := FormatRedisCommand([]string{"SET", "k", v})
		back, err := ParseRedisCommand(q)
		if err != nil || back[2] != v {
			t.Errorf("%q -> %s -> %q %v", v, q, back, err)
		}
	}
	if got := RedisDisplay("a\tb\nc‮\u0085"); got != "a\tb\nc\\u{202e}\\u{85}" {
		t.Errorf("display = %q", got)
	}
}

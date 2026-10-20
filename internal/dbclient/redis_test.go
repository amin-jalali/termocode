package dbclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

func redisTestSession(t *testing.T, f *dbtest.FakeRedis, c Conn, password string) *RedisSession {
	t.Helper()
	if c.Host == "" {
		c.Host, c.Port = "cache.internal", 6379
	}
	c.Driver = "redis"
	s, err := NewRedisSession(context.Background(), c, password, f.Dial)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestRedisDialSendsACLUserPasswordAndDatabase(t *testing.T) {
	f := dbtest.NewFakeRedis()
	f.User, f.Password = "app", "s3cret-pw"
	s := redisTestSession(t, f, Conn{User: "app", DB: 2}, "s3cret-pw")
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	cmds := f.Commands()
	if len(cmds) < 2 || cmds[0] != "AUTH app s3cret-pw" || cmds[1] != "SELECT 2" {
		t.Fatalf("dial commands = %q", cmds)
	}
	if _, err := NewRedisSession(context.Background(), Conn{Driver: "redis", Host: "h", Port: 6379, User: "app"}, "wrong-pw-123", f.Dial); err == nil {
		t.Fatal("a wrong password connected")
	} else if strings.Contains(err.Error(), "wrong-pw-123") || !strings.Contains(err.Error(), "WRONGPASS") {
		t.Fatalf("error must name the server reason and never the password: %v", err)
	}
}

func TestRedisURLConnectionTakesHostDatabaseAndTLS(t *testing.T) {
	for _, tc := range []struct {
		conn Conn
		addr string
		tls  bool
		db   int
		user string
	}{
		{Conn{Driver: "redis", URL: "redis://cache:6380/3"}, "cache:6380", false, 3, ""},
		{Conn{Driver: "redis", URL: "rediss://app@cache.example/1"}, "cache.example:6379", true, 1, "app"},
		{Conn{Driver: "redis", Host: "h", Port: 7000, DB: 4, TLS: true}, "h:7000", true, 4, ""},
		{Conn{Driver: "redis", Host: "::1", Port: 6379}, "[::1]:6379", false, 0, ""},
	} {
		got := redisTargetOf(tc.conn)
		if got.addr != tc.addr || got.tls != tc.tls || got.db != tc.db || got.user != tc.user {
			t.Errorf("%+v: got %+v", tc.conn, got)
		}
	}
}

// selfSigned writes a CA certificate to dir and returns a server config.
func selfSigned(t *testing.T, dir string) (*tls.Config, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cache.internal"}, DNSNames: []string{"cache.internal"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, ca
}

func TestRedisTLSVerifiesTheServerWithTheGivenCA(t *testing.T) {
	f := dbtest.NewFakeRedis()
	cfg, ca := selfSigned(t, t.TempDir())
	f.TLS = cfg
	s := redisTestSession(t, f, Conn{TLS: true, SSL: &SSLConfig{CA: ca}}, "")
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("TLS ping: %v", err)
	}
	// Without the CA the unknown certificate is refused. A loopback socket
	// here: over net.Pipe (no buffer) both ends of a failed handshake block.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = tls.Server(c, cfg).Handshake()
			_ = c.Close()
		}
	}()
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ln.Addr().String())
	}
	if _, err := NewRedisSession(context.Background(), Conn{Driver: "redis", Host: "cache.internal", Port: 6379, TLS: true}, "", dial); err == nil {
		t.Fatal("an unknown certificate was trusted")
	} else if !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("error = %v", err)
	}
}

func TestRedisScanKeysPagesWithSCANAndStopsAtTheCap(t *testing.T) {
	f := dbtest.NewFakeRedis()
	for i := 0; i < 2500; i++ {
		f.SetString(0, fmt.Sprintf("user:%04d", i), "x")
	}
	f.SetString(0, "a*b:1", "x")
	f.SetString(0, "a*b:2", "x")
	f.SetString(0, "axb:3", "x")
	s := redisTestSession(t, f, Conn{}, "")
	keys, more, err := s.ScanKeys(context.Background(), 0, "user:*", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1000 || !more {
		t.Fatalf("cap: %d keys, more=%v", len(keys), more)
	}
	keys, more, err = s.ScanKeys(context.Background(), 0, RedisGlobEscape("a*b:")+"*", 1000)
	if err != nil || more || len(keys) != 2 {
		t.Fatalf("escaped prefix: %q more=%v err=%v", keys, more, err)
	}
	for _, c := range f.Commands() {
		if strings.HasPrefix(c, "KEYS") {
			t.Fatalf("KEYS was sent: %q", c)
		}
		if strings.HasPrefix(c, "SCAN") && !strings.Contains(c, "COUNT 1000") {
			t.Fatalf("SCAN without COUNT 1000: %q", c)
		}
	}
	types, err := s.Types(context.Background(), 0, []string{"user:0001", "missing"})
	if err != nil || types[0] != (RedisKeyMeta{Type: "string", TTL: -1}) || types[1].Type != "none" {
		t.Fatalf("types = %+v, %v", types, err)
	}
}

func TestRedisKeyspaceListsDatabasesWithKeys(t *testing.T) {
	f := dbtest.NewFakeRedis()
	f.SetString(0, "a", "1")
	f.SetString(3, "b", "1")
	f.SetString(3, "c", "1")
	s := redisTestSession(t, f, Conn{DB: 5}, "")
	dbs, err := s.Keyspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 3 || dbs[0] != (RedisDB{Index: 0, Keys: 1}) || dbs[1] != (RedisDB{Index: 3, Keys: 2}) || dbs[2] != (RedisDB{Index: 5, Keys: 0}) {
		t.Fatalf("keyspace = %+v (the connection's own db is always listed)", dbs)
	}
}

func TestRedisValuePagesPerTypeNeverLoadTheWholeValue(t *testing.T) {
	f := dbtest.NewFakeRedis()
	ctx := context.Background()
	for i := 0; i < 250; i++ {
		f.HSet(0, "h", fmt.Sprintf("f%03d", i), fmt.Sprint(i))
		f.RPush(0, "l", fmt.Sprintf("item-%03d", i))
		f.SAdd(0, "s", fmt.Sprintf("m%03d", i))
		f.ZAdd(0, "z", float64(i), fmt.Sprintf("p%03d", i))
		f.XAdd(0, "x", fmt.Sprintf("1700000000000-%d", i), "n", fmt.Sprint(i))
	}
	f.SetString(0, "big", strings.Repeat("a", 200_000))
	f.Expire(0, "h", 90*time.Minute)
	s := redisTestSession(t, f, Conn{}, "")

	info, err := s.KeyInfo(ctx, 0, "h")
	if err != nil || info.Type != "hash" || info.Len != 250 || info.TTL < 80*60*1000 || info.Bytes <= 0 {
		t.Fatalf("info = %+v, %v", info, err)
	}
	collect := func(typ, key string) (pages int, rows [][]string, cols []string) {
		from := ""
		for {
			p, err := s.ValuePage(ctx, 0, key, typ, from, 100)
			if err != nil {
				t.Fatalf("%s: %v", typ, err)
			}
			pages++
			cols = p.Columns
			rows = append(rows, p.Rows...)
			if p.Next == "" || pages > 10 {
				return
			}
			from = p.Next
		}
	}
	for _, tc := range []struct {
		typ, key, first string
		cols            string
	}{
		{"hash", "h", "f000|0", "field|value"},
		{"list", "l", "0|item-000", "index|value"},
		{"set", "s", "m000", "member"},
		{"zset", "z", "0|p000", "score|member"},
		{"stream", "x", "1700000000000-0|n=0", "id|fields"},
	} {
		pages, rows, cols := collect(tc.typ, tc.key)
		if len(rows) != 250 || pages < 3 {
			t.Errorf("%s: %d rows in %d pages", tc.typ, len(rows), pages)
		}
		if strings.Join(cols, "|") != tc.cols || strings.Join(rows[0], "|") != tc.first {
			t.Errorf("%s: cols %q first %q", tc.typ, cols, rows[0])
		}
	}
	p, err := s.ValuePage(ctx, 0, "big", "string", "", 100)
	if err != nil || !p.Truncated || len(p.Rows[0][0]) != RedisStringMax {
		t.Fatalf("big string: truncated=%v len=%d err=%v", p.Truncated, len(p.Rows[0][0]), err)
	}
	for _, c := range f.Commands() {
		for _, bad := range []string{"GET big", "HGETALL", "SMEMBERS", "KEYS", "LRANGE l 0 -1"} {
			if strings.HasPrefix(c, bad) {
				t.Fatalf("whole value loaded: %q", c)
			}
		}
		if strings.HasPrefix(c, "XRANGE") && !strings.Contains(c, "COUNT 100") {
			t.Fatalf("stream read without COUNT: %q", c)
		}
	}
}

func TestRedisParseAndFormatCommandLines(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`SET a b`, []string{"SET", "a", "b"}},
		{`  hset  "my key" field "two words"  `, []string{"hset", "my key", "field", "two words"}},
		{`SET k "line\nnext\x41\""`, []string{"SET", "k", "line\nnextA\""}},
		{`SET k 'it''s'`, nil},
		{`SET k 'a\'b'`, []string{"SET", "k", "a'b"}},
		{`SET k ""`, []string{"SET", "k", ""}},
		{`SET "unclosed`, nil},
	} {
		got, err := ParseRedisCommand(tc.in)
		if tc.want == nil {
			if err == nil {
				t.Errorf("%q: no error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil || strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("%q: got %q, %v", tc.in, got, err)
		}
		back, err := ParseRedisCommand(FormatRedisCommand(got))
		if err != nil || strings.Join(back, "\x00") != strings.Join(got, "\x00") {
			t.Errorf("%q: format round trip %q -> %q", tc.in, FormatRedisCommand(got), back)
		}
	}
	if got := FormatRedisCommand([]string{"SET", "k", "\x1b[31mred\x00"}); got != `SET k "\x1b[31mred\x00"` {
		t.Fatalf("escapes = %s", got)
	}
}

func TestRedisClassifyAllowAndNeverLists(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		kind RedisKind
		ai   bool
	}{
		{"GET a", RedisRead, true},
		{"hgetall h", RedisRead, true},
		{"SCAN 0 MATCH a* COUNT 10", RedisRead, true},
		{"XREAD COUNT 1 STREAMS x 0", RedisRead, true},
		{"XREAD BLOCK 0 STREAMS x $", RedisWrite, false},
		{"MEMORY USAGE a", RedisRead, true},
		{"CONFIG GET maxmemory", RedisRead, false},
		{"KEYS *", RedisRead, true},
		{"SORT l STORE out", RedisWrite, false},
		{"SORT l", RedisWrite, false},
		{"SET a b", RedisWrite, false},
		{"DEL a", RedisWrite, false},
		{"NOSUCHCMD a", RedisWrite, false},
		{"FLUSHALL", RedisDangerous, false},
		{"flushdb async", RedisDangerous, false},
		{"CONFIG SET requirepass x", RedisDangerous, false},
		{"SHUTDOWN NOSAVE", RedisDangerous, false},
		{"DEBUG SLEEP 1", RedisDangerous, false},
		{"MODULE LOAD /x.so", RedisDangerous, false},
		{"SCRIPT FLUSH", RedisDangerous, false},
		{"EVAL \"return 1\" 0", RedisDangerous, false},
		{"FCALL f 0", RedisDangerous, false},
		{"MIGRATE h 6379 k 0 1", RedisDangerous, false},
		{"REPLICAOF h 6379", RedisDangerous, false},
		{"SLAVEOF NO ONE", RedisDangerous, false},
		{"ACL SETUSER bob on", RedisDangerous, false},
		{"CLIENT KILL ID 1", RedisDangerous, false},
		{"SWAPDB 0 1", RedisDangerous, false},
		{"MONITOR", RedisRefused, false},
		{"SUBSCRIBE ch", RedisRefused, false},
		{"PSUBSCRIBE *", RedisRefused, false},
		{"SELECT 1", RedisRefused, false},
		{"MULTI", RedisRefused, false},
		{"HELLO 3", RedisRefused, false},
	} {
		args, err := ParseRedisCommand(tc.cmd)
		if err != nil {
			t.Fatal(err)
		}
		kind, _ := ClassifyRedis(args)
		if kind != tc.kind || RedisAIRead(args) != tc.ai {
			t.Errorf("%s: kind %v ai %v; want %v %v", tc.cmd, kind, RedisAIRead(args), tc.kind, tc.ai)
		}
	}
}

func TestRedisConsoleRunRewritesKEYSAndEscapesValues(t *testing.T) {
	f := dbtest.NewFakeRedis()
	f.SetString(0, "user:1", "\x1b]52;c;evil\x07plain\xff")
	f.SetString(0, "user:2", "b")
	f.SetString(0, "other", "c")
	s := redisTestSession(t, f, Conn{}, "")
	res := s.Run(context.Background(), "KEYS user:*", RunOptions{MaxRows: 100})
	if res.Err != nil || len(res.Rows) != 2 || f.Sent("KEYS") {
		t.Fatalf("KEYS: err %v rows %d sent %v", res.Err, len(res.Rows), f.Sent("KEYS"))
	}
	if len(res.Notices) == 0 || !strings.Contains(res.Notices[0], "SCAN") {
		t.Fatalf("no note about the rewrite: %q", res.Notices)
	}
	res = s.Run(context.Background(), "GET user:1", RunOptions{})
	if res.Err != nil || len(res.Rows) != 1 {
		t.Fatal(res.Err)
	}
	if got := res.Rows[0][0].Text; strings.ContainsAny(got, "\x1b\x07") || !strings.Contains(got, `\x1b]52;c;evil\x07plain\xff`) {
		t.Fatalf("value not escaped: %q", got)
	}
	res = s.Run(context.Background(), "HGET nothere f", RunOptions{})
	if res.Err != nil || !res.Rows[0][0].Null {
		t.Fatalf("nil reply: %+v", res)
	}
	res = s.Run(context.Background(), "NOSUCH", RunOptions{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "unknown command") {
		t.Fatalf("server error lost: %+v", res)
	}
	res = s.Run(context.Background(), "MONITOR", RunOptions{})
	if res.Err == nil || f.Sent("MONITOR") {
		t.Fatal("MONITOR was sent")
	}
}

func TestRedisReadOnlySessionRefusesWritesBeforeSending(t *testing.T) {
	f := dbtest.NewFakeRedis()
	f.SetString(0, "a", "1")
	s := redisTestSession(t, f, Conn{}, "")
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if res := ro.Run(context.Background(), "GET a", RunOptions{}); res.Err != nil || res.Rows[0][0].Text != "1" {
		t.Fatalf("read: %+v", res)
	}
	for _, cmd := range []string{"SET a 2", "FLUSHALL", "CONFIG GET requirepass", "EVAL x 0"} {
		if res := ro.Run(context.Background(), cmd, RunOptions{}); res.Err == nil {
			t.Fatalf("%s ran in a read-only session", cmd)
		}
	}
	for _, c := range f.Commands() {
		if strings.HasPrefix(c, "SET") || strings.HasPrefix(c, "FLUSHALL") || strings.HasPrefix(c, "CONFIG") || strings.HasPrefix(c, "EVAL") {
			t.Fatalf("sent from a read-only session: %q", c)
		}
	}
}

func TestRedisCancelEndsAStuckCommandAndTheSessionRecovers(t *testing.T) {
	f := dbtest.NewFakeRedis()
	s := redisTestSession(t, f, Conn{}, "")
	f.SetHang(true)
	done := make(chan Result, 1)
	go func() { done <- s.Run(context.Background(), "GET a", RunOptions{}) }()
	time.Sleep(50 * time.Millisecond)
	s.Cancel()
	select {
	case r := <-done:
		if r.Err == nil {
			t.Fatal("a cancelled command has no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Cancel did not end the command")
	}
	f.SetHang(false)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("no reconnect after cancel: %v", err)
	}
}

func TestRedisWriteBuildersQuoteExactly(t *testing.T) {
	if got := FormatRedisCommand(RedisHSet("session:ab12", "plan", "pro max")); got != `HSET session:ab12 plan "pro max"` {
		t.Fatal(got)
	}
	if got := FormatRedisCommand(RedisSetTTL("k", 0)); got != "PERSIST k" {
		t.Fatal(got)
	}
	if got := FormatRedisCommand(RedisSetTTL("k", 90)); got != "EXPIRE k 90" {
		t.Fatal(got)
	}
	if got := FormatRedisCommand(RedisSetString("k", "v", true)); got != "SET k v KEEPTTL" {
		t.Fatal(got)
	}
}

func TestRedisDeleteKeysUsesBatchesOfUnlinkAndFallsBackToDel(t *testing.T) {
	for _, noUnlink := range []bool{false, true} {
		f := dbtest.NewFakeRedis()
		f.NoUnlink = noUnlink
		var keys []string
		for i := 0; i < RedisDeleteBatch+10; i++ {
			k := fmt.Sprintf("user:%04d", i)
			f.SetString(0, k, "x")
			keys = append(keys, k)
		}
		f.SetString(0, "keep", "x")
		s := redisTestSession(t, f, Conn{}, "")
		n, err := RedisDeleteKeys(context.Background(), func(ctx context.Context, args []string) (any, error) { return s.DoDB(ctx, 0, args) }, keys)
		if err != nil || n != int64(len(keys)) {
			t.Fatalf("noUnlink=%v: deleted %d, err %v", noUnlink, n, err)
		}
		if !f.Exists(0, "keep") || f.Exists(0, "user:0000") || f.Sent("KEYS") {
			t.Fatalf("noUnlink=%v: wrong keys left; commands %d", noUnlink, len(f.Commands()))
		}
		if noUnlink && (!f.Sent("DEL") || len(f.Commands()) < 3) {
			t.Fatalf("no DEL fallback: sent DEL=%v", f.Sent("DEL"))
		}
		if !noUnlink && (f.Sent("DEL") || !f.Sent("UNLINK")) {
			t.Fatal("UNLINK was not used")
		}
	}
}

func TestRedisDeleteKeysRefusesOnAReadOnlySession(t *testing.T) {
	f := dbtest.NewFakeRedis()
	f.SetString(0, "user:1", "x")
	s := NewRedisReadOnlySession(Conn{Driver: "redis", Host: "cache.internal", Port: 6379}, "", f.Dial)
	t.Cleanup(func() { _ = s.Close() })
	if _, err := RedisDeleteKeys(context.Background(), func(ctx context.Context, args []string) (any, error) { return s.DoDB(ctx, 0, args) }, []string{"user:1"}); err == nil {
		t.Fatal("a read-only session deleted keys")
	}
	if !f.Exists(0, "user:1") {
		t.Fatal("key was deleted")
	}
}

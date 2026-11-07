package dbclient

import (
	"reflect"
	"testing"
)

// Rename and copy send the exact command the card shows; the safe form
// (RENAMENX, COPY without REPLACE) is the default.
func TestRedisRenameAndCopyBuilders(t *testing.T) {
	for _, tc := range []struct {
		got, want []string
	}{
		{RedisRename("a", "b", false), []string{"RENAMENX", "a", "b"}},
		{RedisRename("a", "b", true), []string{"RENAME", "a", "b"}},
		{RedisCopy("a", "b", -1, false), []string{"COPY", "a", "b"}},
		{RedisCopy("a", "b", 3, true), []string{"COPY", "a", "b", "DB", "3", "REPLACE"}},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
	// RENAMENX and COPY are writes: they ask first and the AI never sends them.
	for _, cmd := range [][]string{RedisRename("a", "b", false), RedisCopy("a", "b", -1, false)} {
		if k, _ := ClassifyRedis(cmd); k != RedisWrite {
			t.Errorf("%q is %v, want write", cmd, k)
		}
	}
}

// INFO text turns into sections of name/value fields in server order;
// comments and blank lines are dropped, values keep their colons.
func TestParseRedisInfo(t *testing.T) {
	text := "# Server\r\nredis_version:7.2.4\r\nos:Linux 6.1 x86_64\r\n\r\n# Keyspace\r\ndb0:keys=3,expires=1,avg_ttl=0\r\nweird line\r\nurl:a:b\r\n"
	got := ParseRedisInfo(text)
	want := []RedisInfoSection{
		{Name: "Server", Fields: [][2]string{{"redis_version", "7.2.4"}, {"os", "Linux 6.1 x86_64"}}},
		{Name: "Keyspace", Fields: [][2]string{{"db0", "keys=3,expires=1,avg_ttl=0"}, {"url", "a:b"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
	if got := ParseRedisInfo("a:1\n"); len(got) != 1 || got[0].Name != "" || got[0].Fields[0] != [2]string{"a", "1"} {
		t.Errorf("fields before a header = %#v", got)
	}
}

// SLOWLOG GET replies (old 4-field and new 6-field forms) become entries;
// broken items are skipped, never a panic.
func TestParseRedisSlowlog(t *testing.T) {
	reply := []any{
		[]any{int64(7), int64(1700000000), int64(1500), []any{[]byte("KEYS"), []byte("*")}, []byte("127.0.0.1:5000"), []byte("app")},
		[]any{int64(6), int64(1699999999), int64(20), []any{[]byte("GET"), []byte("k")}},
		[]any{int64(5)},
		"junk",
	}
	got := ParseRedisSlowlog(reply)
	want := []RedisSlowEntry{
		{ID: 7, Unix: 1700000000, Micros: 1500, Args: []string{"KEYS", "*"}, Client: "127.0.0.1:5000", Name: "app"},
		{ID: 6, Unix: 1699999999, Micros: 20, Args: []string{"GET", "k"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
	if ParseRedisSlowlog(nil) != nil {
		t.Error("nil reply must give no entries")
	}
}

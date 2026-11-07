package dbclient

import "testing"

func TestRedisInfoResultIsASectionFieldValueGrid(t *testing.T) {
	res := RedisInfoResult("# Server\r\nredis_version:7.2.4\r\n\r\n# Memory\r\nused_memory_human:1.5M\r\n")
	if len(res.Columns) != 3 || res.Columns[0].Name != "section" || res.Columns[1].Name != "field" || res.Columns[2].Name != "value" {
		t.Fatalf("columns %+v", res.Columns)
	}
	if len(res.Rows) != 2 || res.Rows[0][0].Text != "Server" || res.Rows[0][2].Text != "7.2.4" || res.Rows[1][1].Text != "used_memory_human" {
		t.Fatalf("rows %+v", res.Rows)
	}
}

func TestRedisSlowlogResultListsEachEntry(t *testing.T) {
	reply := []any{[]any{int64(7), int64(1700000000), int64(1500), []any{[]byte("KEYS"), []byte("*")}, []byte("10.0.0.1:5000"), []byte("app")}}
	res := RedisSlowlogResult(reply)
	if len(res.Columns) != 6 || len(res.Rows) != 1 {
		t.Fatalf("grid %+v", res)
	}
	row := res.Rows[0]
	if row[0].Text != "7" || row[2].Text != "1500" || row[3].Text != "KEYS *" || row[4].Text != "10.0.0.1:5000" || row[5].Text != "app" {
		t.Fatalf("row %+v", row)
	}
	if empty := RedisSlowlogResult([]any{}); len(empty.Rows) != 0 || len(empty.Notices) == 0 {
		t.Fatalf("empty %+v", empty)
	}
}

func TestRedisAdminGridFor(t *testing.T) {
	if _, ok := RedisAdminGrid([]string{"info", "memory"}, "# Memory\nused_memory:1\n"); !ok {
		t.Fatal("INFO not shown as a grid")
	}
	if _, ok := RedisAdminGrid([]string{"SLOWLOG", "get", "10"}, []any{}); !ok {
		t.Fatal("SLOWLOG GET not shown as a grid")
	}
	if _, ok := RedisAdminGrid([]string{"GET", "k"}, []byte("v")); ok {
		t.Fatal("GET shown as an admin grid")
	}
}

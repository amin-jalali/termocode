package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// databaseRedisDemo is the canned Redis tree (dbdemo redis): a production
// cache with db0 open, the session: namespace open and a hash key
// selected; nothing is dialled.
func (m *Model) databaseRedisDemo() {
	m.databasesDemo("")
	s := m.databases
	pg := dbDemoConnection()
	cache := dbclient.Conn{ID: "prod-cache", Name: "prod-cache", Driver: "redis", Host: "cache.internal", Port: 6379, Environment: "production"}
	s.conns = []dbclient.Conn{pg, cache}
	s.nodes = map[string]dbNode{}
	s.nodes[pg.ID] = dbNode{kind: dbNodeConnection, name: pg.Name, path: pg.ID, conn: pg.ID, detail: "postgres  localhost:5432"}
	s.folded[pg.ID] = true
	c := cache.ID
	db0, db3 := c+"/db/0", c+"/db/3"
	ns := db0 + "/ns/session:"
	s.nodes[c] = dbNode{kind: dbNodeConnection, name: c, path: c, conn: c, detail: "redis  cache.internal:6379", loaded: true, children: []string{db0, db3}}
	s.nodes[db0] = dbNode{kind: dbNodeDatabase, name: "db0", path: db0, conn: c, rdb: 0, detail: "1234 keys", loaded: true}
	s.nodes[db3] = dbNode{kind: dbNodeDatabase, name: "db3", path: db3, conn: c, rdb: 3, detail: "12 keys"}
	s.folded[db3] = true
	add := func(parent string, n dbNode) {
		s.nodes[n.path] = n
		p := s.nodes[parent]
		p.children = append(p.children, n.path)
		s.nodes[parent] = p
	}
	add(db0, dbNode{kind: dbNodeRedisNS, name: "session:", path: ns, conn: c, key: "session:", detail: "410 keys", loaded: true})
	add(db0, dbNode{kind: dbNodeRedisNS, name: "user:", path: db0 + "/ns/user:", conn: c, key: "user:", detail: "800 keys"})
	s.folded[db0+"/ns/user:"] = true
	for _, k := range []struct{ key, typ string }{{"ab12", "hash"}, {"cd34", "string"}, {"ef56", "hash"}} {
		add(ns, dbNode{kind: dbNodeRedisKey, name: k.key, path: ns + "/key/session:" + k.key, conn: c, key: "session:" + k.key, rtype: k.typ, detail: k.typ, ttl: int64(23 * time.Minute / time.Millisecond)})
	}
	for _, k := range []struct{ key, typ string }{{"counter", "string"}, {"events", "stream"}, {"leaderboard", "zset"}, {"queue:jobs", "list"}, {"tags", "set"}} {
		add(db0, dbNode{kind: dbNodeRedisKey, name: k.key, path: db0 + "/key/" + k.key, conn: c, key: k.key, rtype: k.typ, detail: k.typ, ttl: -1})
	}
	// local-pg, prod-cache, db0, session:, ab12
	s.cursor = 4
	s.touch()
}

// databaseRedisKeyDemo is the hash value page of session:ab12 (dbdemo
// redis-key), as the plan's mock shows it.
func (m *Model) databaseRedisKeyDemo() tea.Cmd {
	m.databaseRedisDemo()
	rows := [][]string{
		{"user_id", "48212"}, {"plan", "pro"}, {"created", "1760770262"}, {"ip", "203.0.113.9"},
		{"ua", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) " + strings.Repeat("x", 120)},
		{"cart", `{"items":[{"sku":"A1","qty":2}],"total":1999}`}, {"flags", ""},
	}
	v := &dbRedisView{key: "session:ab12", info: dbclient.RedisKeyInfo{Type: "hash", TTL: int64(23 * time.Minute / time.Millisecond), Len: 7, Bytes: 1229}, raw: rows, cols: []string{"field", "value"}}
	res := dbclient.Result{Columns: []dbclient.Column{{Name: "field"}, {Name: "value"}}, Elapsed: 2 * time.Millisecond}
	for _, r := range rows {
		value := dbclient.Cell{Text: dbclient.RedisDisplay(r[1])}
		if r[1] == "" {
			value = dbclient.Cell{Text: "<empty>", Kind: "nil"}
		}
		res.Rows = append(res.Rows, []dbclient.Cell{{Text: r[0]}, value})
	}
	r := &dbRun{conn: "prod-cache", sql: "Redis: session:ab12", by: "user", redis: v, result: res}
	s := m.dbConsole()
	s.tabs, s.tab = nil, 0
	s.placeRun(r)
	s.cache, s.size, s.page, s.sortCol, s.filter, s.view = nil, 100, 0, -1, "", dbViewGrid
	s.trans, s.trow = false, 0
	return m.databaseShowPage()
}

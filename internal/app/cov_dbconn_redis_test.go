package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/theme"
)

func cdbRedisSeed() *dbtest.FakeRedis {
	f := dbtest.NewFakeRedis()
	f.HSet(0, "session:ab12", "user_id", "48212", "plan", "pro", "cart", `{"items":[1]}`, "flags", "")
	f.Expire(0, "session:ab12", 23*time.Minute)
	f.SetString(0, "session:cd34", "token")
	f.SetString(0, "user:1", "ada")
	f.RPush(0, "user:2:events", "login", "logout")
	f.SetString(0, "counter", "41")
	f.SetString(0, "doc", `{"a":1,"b":[1,2]}`)
	f.SAdd(0, "tags", "x", "y")
	f.ZAdd(0, "board", 1.5, "ada")
	f.ZAdd(0, "board", 2.5, "bob")
	f.XAdd(0, "events", "1-1", "event", "login")
	f.SetString(3, "other", "x")
	return f
}

// cdbRedisModel is a model with a connected Redis session on the fake.
func cdbRedisModel(t *testing.T, f *dbtest.FakeRedis, conn dbclient.Conn) *Model {
	t.Helper()
	old := dbRedisDial
	dbRedisDial = f.Dial
	t.Cleanup(func() { dbRedisDial = old; f.Close() })
	if conn.ID == "" {
		conn = dbclient.Conn{ID: "cache", Driver: "redis", Host: "cache.internal", Port: 6379}
	}
	m := cdbRealModel(t, conn)
	m.activity.SetActive(activity.ViewDatabases)
	m.showExp = true
	sess, err := dbRedisSessionFactory(t.Context(), conn, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	m.databases.sessions[conn.ID] = sess
	return m
}

// cdbRedisReconnect gives the model a fresh session after a refresh
// closed the old one (a rename, copy or new key refreshes the tree).
func cdbRedisReconnect(t *testing.T, m *Model) {
	t.Helper()
	m.activity.SetActive(activity.ViewDatabases)
	c, _ := m.databases.connection("cache")
	sess, err := dbRedisSessionFactory(t.Context(), c, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	m.databases.sessions["cache"] = sess
	cdbRedisOpenKey(t, m, "counter")
}

// cdbRedisExpand expands path and runs the answer through Update.
func cdbRedisExpand(m *Model, path string) {
	cdbPump(m, m.databaseRedisExpand(path))
}

func TestCovRedisTreeLoadsDatabasesNamespacesAndKeys(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	s := m.databases
	cdbRedisExpand(m, "cache")
	if _, ok := s.nodes["cache/db/0"]; !ok {
		t.Fatalf("no db0: %v", s.nodes)
	}
	cdbRedisExpand(m, "cache/db/0")
	for _, p := range []string{"cache/db/0/ns/session:", "cache/db/0/ns/user:", "cache/db/0/key/counter"} {
		if _, ok := s.nodes[p]; !ok {
			t.Fatalf("no %s", p)
		}
	}
	cdbRedisExpand(m, "cache/db/0/ns/session:")
	if n := s.nodes["cache/db/0/ns/session:/key/session:ab12"]; n.rtype != "hash" || n.ttl <= 0 {
		t.Fatalf("key %+v", n)
	}
	// an empty database says so; a late answer for another epoch is dropped
	cdbRedisExpand(m, "cache/db/0/ns/zzz:")
	s.nodes["cache/db/9"] = dbNode{kind: dbNodeDatabase, name: "db9", path: "cache/db/9", conn: "cache", rdb: 9}
	cdbRedisExpand(m, "cache/db/9")
	if len(s.nodes["cache/db/9"].children) != 1 || s.nodes["cache/db/9/empty"].name != "no keys" {
		t.Fatalf("%+v", s.nodes["cache/db/9"])
	}
	if cmd := m.onDBRedisTree(dbRedisTreeMsg{path: "cache", connection: "cache", epoch: 99, generation: s.generation}); cmd != nil {
		t.Fatal("stale epoch")
	}
	if cmd := m.onDBRedisTree(dbRedisTreeMsg{path: "nope", connection: "cache", generation: s.generation}); cmd != nil {
		t.Fatal("unknown path")
	}
	// a failing scan marks the node
	m.onDBRedisTree(dbRedisTreeMsg{path: "cache/db/0", connection: "cache", generation: s.generation, err: cdbErr{}})
	if s.nodes["cache/db/0"].problem == "" || !strings.Contains(covToastText(m), "Could not read the keys") {
		t.Fatalf("no problem: %+v", s.nodes["cache/db/0"])
	}
	// a scan cut at the cap
	m.onDBRedisTree(dbRedisTreeMsg{path: "cache/db/0", connection: "cache", generation: s.generation, keys: []string{"a", "b"}, more: true, max: 2,
		meta: map[string]dbclient.RedisKeyMeta{"a": {Type: "string", TTL: -1}}})
	if s.nodes["cache/db/0/more"].kind != dbNodeRedisMore {
		t.Fatal("no more row")
	}
	// not connected: expanding connects first
	delete(s.sessions, "cache")
	if cmd := m.databaseRedisExpand("cache/db/0"); cmd == nil {
		t.Fatal("no connect command")
	}
	if cmd := m.databaseRedisExpand("nowhere"); cmd != nil {
		t.Fatal("unknown node")
	}
}

type cdbErr struct{}

func (cdbErr) Error() string { return "boom" }

func TestCovRedisSmallFormatters(t *testing.T) {
	for in, want := range map[int64]string{-1: "", 5000: "5 s", 61000: "1 min", 2 * 3600 * 1000: "2 h", 100 * 3600 * 1000: "4 days"} {
		if got := dbRedisTTL(in); got != want {
			t.Errorf("ttl %d: %q", in, got)
		}
	}
	for in, want := range map[int64]string{-1: "", 12: "12 B", 2048: "2.0 KB", 3 << 20: "3.0 MB"} {
		if got := dbRedisSize(in); got != want {
			t.Errorf("size %d: %q", in, got)
		}
	}
	if dbRedisCount(1, false) != "1 key" || dbRedisCount(5, true) != "5+ keys" {
		t.Fatal("count")
	}
	if dbRedisTypeName("none") != "gone" || dbRedisTypeName("hash") != "hash" {
		t.Fatal("type name")
	}
	for _, typ := range []string{"string", "hash", "list", "set", "zset", "stream", "x"} {
		if dbRedisTypeGlyph(typ) == "" {
			t.Errorf("glyph %s", typ)
		}
	}
	s := dbConsoleSettings{}
	if s.redisSeparator() != ":" || s.redisMaxKeys() != 10000 {
		t.Fatal("defaults")
	}
	sep := "/"
	n := 5
	s = dbConsoleSettings{RedisSep: &sep, RedisMaxKeys: &n}
	if s.redisSeparator() != "/" || s.redisMaxKeys() != 100 {
		t.Fatalf("%q %d", s.redisSeparator(), s.redisMaxKeys())
	}
	neg := -3
	if (dbConsoleSettings{RedisMaxKeys: &neg}).redisMaxKeys() != 10000 {
		t.Fatal("negative cap")
	}
	huge := 1 << 40
	if (dbConsoleSettings{RedisMaxKeys: &huge}).redisMaxKeys() != 1000000 {
		t.Fatal("huge cap")
	}
	v := &dbRedisView{info: dbclient.RedisKeyInfo{Type: "stream", Len: 3, TTL: -1, Bytes: -1}, trunc: true, prev: []string{""}}
	if got := dbRedisSummary(dbclient.Conn{}, v); !strings.Contains(got[0], "3 entries") || !strings.Contains(got[0], "first 64 KB") || !strings.Contains(got[0], "page 2") {
		t.Fatalf("%v", got)
	}
	v.info.Len = 1
	if got := dbRedisSummary(dbclient.Conn{}, v); !strings.Contains(got[0], "1 entry") {
		t.Fatalf("%v", got)
	}
	for _, typ := range []string{"string", "hash", "list", "set", "zset", "stream", "none"} {
		if h := dbRedisHints(200, &dbRedisView{info: dbclient.RedisKeyInfo{Type: typ}}); h == "" {
			t.Errorf("hints %s", typ)
		}
	}
	if redisConnDB(dbclient.Conn{URL: "redis://h:6379/4"}) != 4 || redisConnDB(dbclient.Conn{DB: 2}) != 2 || redisConnDB(dbclient.Conn{URL: "::bad"}) != 0 {
		t.Fatal("conn db")
	}
	if !dbRedisConsoleLine("GET a") || dbRedisConsoleLine("  # c") || dbRedisConsoleLine("// c") || dbRedisConsoleLine(" ") {
		t.Fatal("console lines")
	}
	if len(dbRedisKeyRows()) == 0 {
		t.Fatal("key rows")
	}
	w := &dbRedisWrite{}
	if dbRedisCardTitle(dbclient.Conn{ReadOnly: true}, w) == "" || dbRedisCardTitle(dbclient.Conn{Environment: "production"}, w) == "" ||
		!strings.Contains(dbRedisCardTitle(dbclient.Conn{}, &dbRedisWrite{deleted: true}), "Delete this key") || !strings.Contains(dbRedisCardTitle(dbclient.Conn{}, w), "Run this command") {
		t.Fatal("card titles")
	}
	// the footer rows
	st := &dbState{}
	for _, n := range []dbNode{{kind: dbNodeRedisKey, rtype: "hash", ttl: 61000}, {kind: dbNodeRedisNS, detail: "3 keys"}, {kind: dbNodeRedisMore}, {kind: dbNodeDatabase, detail: "d"}, {kind: dbNodeTable}} {
		_ = st.dbRedisFooter(n)
	}
}

// cdbRedisOpenKey opens key on the value page and returns the view.
func cdbRedisOpenKey(t *testing.T, m *Model, key string) *dbRedisView {
	t.Helper()
	cdbPump(m, m.databaseRedisOpen("cache", 0, key, "", nil, nil))
	s := m.databases.console
	if s == nil || s.shown == nil || s.shown.redis == nil || s.shown.redis.key != key {
		t.Fatalf("value page of %s not shown (toast %q)", key, covToastText(m))
	}
	return s.shown.redis
}

// cdbPageKey presses a value page key on row and returns whether it was ours.
func cdbPageKey(m *Model, key string, row int) bool {
	cmd, ok := m.databaseRedisPageKey(key, row, 0)
	cdbPump(m, cmd)
	return ok
}

func TestCovRedisValuePageHash(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	v := cdbRedisOpenKey(t, m, "session:ab12")
	if v.info.Type != "hash" || len(v.raw) != 4 {
		t.Fatalf("%+v", v)
	}
	// unknown keys go to the generic page
	if cdbPageKey(m, "zzz", 0) {
		t.Fatal("unknown key handled")
	}
	// no-op keys
	for _, k := range []string{"]", "[", "x", "K"} {
		if !cdbPageKey(m, k, 0) {
			t.Fatalf("%s not handled", k)
		}
	}
	// edit a field: prompt -> card -> run -> reload
	cdbPageKey(m, "e", 2) // plan
	if !m.promptOpen || m.promptKind != promptKindDBRedisValue {
		t.Fatalf("no edit prompt: %q", covToastText(m))
	}
	cdbSubmitP(t, m, "max")
	if !m.confirmOpen || m.confirmKind != confirmKindDBRedisWrite {
		t.Fatal("no write card")
	}
	cdbConfirmP(t, m, "run")
	if f.Hash(0, "session:ab12")["plan"] != "max" {
		t.Fatalf("hash %v", f.Hash(0, "session:ab12"))
	}
	// add a field
	cdbPageKey(m, "+", 0)
	cdbSubmitP(t, m, "color")
	cdbSubmitP(t, m, "blue")
	cdbConfirmP(t, m, "run")
	if f.Hash(0, "session:ab12")["color"] != "blue" {
		t.Fatalf("hash %v", f.Hash(0, "session:ab12"))
	}
	// an empty field name cancels the add
	cdbPageKey(m, "+", 0)
	cdbSubmitP(t, m, "  ")
	// delete a field
	cdbPageKey(m, "-", 1)
	cdbConfirmP(t, m, "run")
	// no row under the cursor
	cdbPageKey(m, "-", 99)
	cdbPageKey(m, "e", 99)
	// ttl
	cdbPageKey(m, "t", 0)
	if !m.promptOpen || m.promptKind != promptKindDBRedisTTL {
		t.Fatal("no ttl prompt")
	}
	cdbSubmitP(t, m, "soon")
	if !strings.Contains(covToastText(m), "not valid") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbPageKey(m, "t", 0)
	cdbSubmitP(t, m, "2h")
	cdbConfirmP(t, m, "run")
	if f.TTL(0, "session:ab12") < time.Hour {
		t.Fatalf("ttl %v", f.TTL(0, "session:ab12"))
	}
	// cancel a card
	cdbPageKey(m, "t", 0)
	cdbSubmitP(t, m, "")
	cdbConfirmP(t, m, "cancel")
	// delete the key
	cdbPageKey(m, "D", 0)
	cdbConfirmP(t, m, "run")
	if f.Exists(0, "session:ab12") {
		t.Fatal("key kept")
	}
}

func TestCovRedisValuePageStringsAndJSON(t *testing.T) {
	f := cdbRedisSeed()
	f.SetString(0, "multi", "line one\nline two")
	m := cdbRedisModel(t, f, dbclient.Conn{})
	v := cdbRedisOpenKey(t, m, "doc")
	if v.info.Type != "string" {
		t.Fatalf("%+v", v)
	}
	// e opens the value in an editor buffer; :w sends the text back
	cdbPageKey(m, "e", 0)
	if m.databases.console.redisEdit == nil {
		t.Fatalf("no edit started: %q", covToastText(m))
	}
	cdbPump(m, m.databaseRedisBufferDone(`{"a":2}`))
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	cdbConfirmP(t, m, "run")
	if f.String(0, "doc") != `{"a":2}` {
		t.Fatalf("doc %q", f.String(0, "doc"))
	}
	// :w with no edit pending does nothing
	if cmd := m.databaseRedisBufferDone("x"); cmd != nil {
		t.Fatal("no edit")
	}
	// a multi-line value
	cdbRedisOpenKey(t, m, "multi")
	cdbPageKey(m, "e", 0)
	cdbPump(m, m.databaseRedisBufferDone("changed"))
	cdbConfirmP(t, m, "cancel")
	// an empty string and a missing key
	f.SetString(0, "empty", "")
	cdbRedisOpenKey(t, m, "empty")
	cdbPump(m, m.databaseRedisOpen("cache", 0, "ghost", "", nil, nil))
	if !strings.Contains(covToastText(m), "") {
		t.Fatal()
	}
	// the string view itself: JSON pretty, long text, truncated
	p := &dbPage{}
	databaseRedisStringView(p, &dbRedisView{raw: [][]string{{`{"a":[1,2]}`}}})
	if len(p.lines) < 3 {
		t.Fatalf("not pretty: %q", p.lines)
	}
	p = &dbPage{}
	databaseRedisStringView(p, &dbRedisView{raw: [][]string{{strings.Repeat("x\n", 3000)}}, trunc: true})
	if len(p.lines) < 2000 {
		t.Fatal("lines")
	}
	p = &dbPage{}
	databaseRedisStringView(p, &dbRedisView{})
	if !strings.Contains(strings.Join(p.lines, "|"), "empty string") {
		t.Fatalf("%q", p.lines)
	}
	// a too-long string cannot be edited here
	cdbRedisOpenKey(t, m, "doc")
	m.databases.console.shown.redis.trunc = true
	m.databases.console.shown.redis.info.Type = "string"
	cdbPageKey(m, "e", 0)
	if !strings.Contains(covToastText(m), "too long to edit") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// - on a string: nothing to delete
	cdbPageKey(m, "-", 0)
}

func TestCovRedisValuePageListSetZsetStream(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})

	// list: edit an item, push, remove
	cdbRedisOpenKey(t, m, "user:2:events")
	cdbPageKey(m, "e", 0)
	cdbSubmitP(t, m, "signup")
	cdbConfirmP(t, m, "run")
	cdbPageKey(m, "+", 0)
	cdbSubmitP(t, m, "extra")
	cdbConfirmP(t, m, "run")
	cdbPageKey(m, "-", 0)
	cdbConfirmP(t, m, "run")

	// set: add, remove, e says no
	cdbRedisOpenKey(t, m, "tags")
	cdbPageKey(m, "e", 0)
	if !strings.Contains(covToastText(m), "cannot be changed in place") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbPageKey(m, "+", 0)
	cdbSubmitP(t, m, "z")
	cdbConfirmP(t, m, "run")
	cdbPageKey(m, "-", 0)
	cdbConfirmP(t, m, "run")

	// zset: edit a score (bad, then good), add a member with a score
	cdbRedisOpenKey(t, m, "board")
	cdbPageKey(m, "e", 0)
	cdbSubmitP(t, m, "high")
	if !strings.Contains(covToastText(m), "A score is a number") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbPageKey(m, "e", 0)
	cdbSubmitP(t, m, "9")
	cdbConfirmP(t, m, "run")
	cdbPageKey(m, "+", 0)
	cdbSubmitP(t, m, "carl")
	cdbSubmitP(t, m, "x")
	if !strings.Contains(covToastText(m), "A score is a number") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbPageKey(m, "+", 0)
	cdbSubmitP(t, m, "carl")
	cdbSubmitP(t, m, "3")
	cdbConfirmP(t, m, "run")
	cdbPageKey(m, "-", 0)
	cdbConfirmP(t, m, "run")

	// stream: add (bad, then good), delete, e says no
	cdbRedisOpenKey(t, m, "events")
	cdbPageKey(m, "e", 0)
	cdbPageKey(m, "+", 0)
	cdbSubmitP(t, m, "odd")
	if !strings.Contains(covToastText(m), "field value pairs") {
		t.Fatalf("toast %q", covToastText(m))
	}
	cdbPageKey(m, "+", 0)
	cdbSubmitP(t, m, `event logout user "Ada L"`)
	cdbConfirmP(t, m, "run")
	cdbPageKey(m, "-", 0)
	cdbConfirmP(t, m, "run")

	// a hash value with a newline opens the editor buffer
	f.HSet(0, "h2", "long", "a\nb")
	cdbRedisOpenKey(t, m, "h2")
	cdbPageKey(m, "e", 0)
	cdbPump(m, m.databaseRedisBufferDone("c\nd"))
	cdbConfirmP(t, m, "run")
	if f.Hash(0, "h2")["long"] != "c\nd" {
		t.Fatalf("%v", f.Hash(0, "h2"))
	}
	// add to a string: nothing to add
	cdbRedisOpenKey(t, m, "counter")
	cdbPageKey(m, "+", 0)
	if !strings.Contains(covToastText(m), "Nothing to add") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// page keys of an errored page
	cdbPump(m, m.databaseRedisOpen("cache", 0, "ghost", "", nil, nil))
	cdbPageKey(m, "t", 0) // swallowed
	cdbPageKey(m, "D", 0) // allowed
	m.confirmOpen = false
	// the prompt handlers with nothing pending
	if cmd, ok := m.databaseRedisPrompt(promptKindDBRedisValue, "x"); !ok || cmd != nil {
		t.Fatal("no edit pending")
	}
	if _, ok := m.databaseRedisPrompt(0, "x"); ok {
		t.Fatal("foreign prompt")
	}
	if cmd := m.databaseRedisValueWrite(&dbRedisEdit{typ: "stream"}, "x"); cmd != nil {
		t.Fatal("unsupported type")
	}
	m.confirmKind = 0
	if cmd, ok := m.databaseRedisConfirm("run"); ok && cmd != nil {
		t.Fatal("foreign card")
	}
}

func TestCovRedisNamespaceDeleteAndSidebarKeys(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	cdbRedisExpand(m, "cache")
	cdbRedisExpand(m, "cache/db/0")
	ns := "cache/db/0/ns/session:"
	row := func(path string) dbTreeRow {
		n := m.databases.nodes[path]
		return dbTreeRow{kind: n.kind, path: path, name: n.name, conn: n.conn}
	}
	// sidebar keys
	if _, ok := m.databaseRedisSidebarKey("enter", dbTreeRow{conn: "nope"}); ok {
		t.Fatal("not a Redis row")
	}
	for _, k := range []string{"o", "d", "s", "#", "i", "?"} {
		if _, ok := m.databaseRedisSidebarKey(k, row(ns)); !ok {
			t.Fatalf("%s", k)
		}
	}
	if _, ok := m.databaseRedisSidebarKey("zzz", row(ns)); ok {
		t.Fatal("unknown key")
	}
	for _, k := range []string{"t", "D", "M", "C", "y"} {
		m.databaseRedisSidebarKey(k, row("cache/db/0"))
	}
	var copied string
	t.Cleanup(SetClipboardSink(func(x string) { copied = x }))
	m.databaseRedisSidebarKey("y", row(ns))
	if copied != "session:" {
		t.Fatalf("copied %q", copied)
	}
	cdbRedisExpand(m, ns)
	key := "cache/db/0/ns/session:/key/session:cd34"
	cmd, ok := m.databaseRedisSidebarKey("enter", row(key))
	if !ok {
		t.Fatal("enter on a key")
	}
	cdbPump(m, cmd)
	// t on a key: TTL prompt
	m.databaseRedisSidebarKey("t", row(key))
	if !m.promptOpen {
		t.Fatal("no ttl prompt")
	}
	m.promptOpen = false
	// M renames, C copies
	m.databaseRedisSidebarKey("M", row(key))
	cdbSubmitP(t, m, "session:renamed")
	cdbConfirmP(t, m, "run")
	if !f.Exists(0, "session:renamed") {
		t.Fatalf("not renamed (toast %q)", covToastText(m))
	}
	cdbRedisExpand(m, "cache")
	cdbRedisExpand(m, "cache/db/0")
	m.databaseRedisSidebarKey("C", row("cache/db/0/key/counter"))
	cdbSubmitP(t, m, "counter:copy")
	cdbConfirmP(t, m, "run")
	if !f.Exists(0, "counter:copy") {
		t.Fatal("not copied")
	}
	// same name / empty name are refused
	reload := func() { cdbRedisExpand(m, "cache"); cdbRedisExpand(m, "cache/db/0") }
	reload()
	m.databaseRedisSidebarKey("M", row("cache/db/0/key/counter"))
	cdbSubmitP(t, m, "counter")
	reload()
	m.databaseRedisSidebarKey("M", row("cache/db/0/key/counter"))
	cdbSubmitP(t, m, " ")
	// D on a key asks, and deletes
	reload()
	m.databaseRedisSidebarKey("D", row("cache/db/0/key/counter"))
	cdbConfirmP(t, m, "run")
	if f.Exists(0, "counter") {
		t.Fatal("not deleted")
	}
	if _, ok := m.databases.nodes["cache/db/0/key/counter"]; ok {
		t.Fatalf("tree row kept; toast %q", cdbToast(m))
	}
	// D without a session connects first
	reload()
	delete(m.databases.sessions, "cache")
	if cmd, _ := m.databaseRedisSidebarKey("D", row("cache/db/0/key/doc")); cmd == nil {
		t.Fatal("no connect")
	}
	m.databases.sessions["cache"], _ = dbRedisSessionFactory(t.Context(), dbclient.Conn{ID: "cache", Driver: "redis", Host: "h", Port: 6379}, "")

	// namespace delete: the card lists the keys; run removes them
	cdbPump(m, m.databaseRedisNSDelete(m.databases.nodes["cache/db/0/ns/user:"], "cache/db/0/ns/user:"))
	if !m.confirmOpen {
		t.Fatalf("no card: %q", covToastText(m))
	}
	cdbConfirmP(t, m, "run")
	if f.Exists(0, "user:1") {
		t.Fatal("namespace kept")
	}
	// nothing under a prefix; a failing scan; an unknown connection
	cdbPump(m, m.databaseRedisNSDelete(dbNode{kind: dbNodeRedisNS, conn: "cache", key: "nothing:"}, "x"))
	if !strings.Contains(covToastText(m), "No keys to delete") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.onDBRedisNSScan(dbRedisNSScanMsg{conn: "cache", err: cdbErr{}})
	m.onDBRedisNSScan(dbRedisNSScanMsg{conn: "gone", keys: []string{"a"}})
	// a long list, read-only, production, other database, cut by the cap
	keys := []string{"k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8", "k9", "k10"}
	for _, env := range []struct {
		ro   bool
		prod bool
		db   int
	}{{true, false, 0}, {false, true, 0}, {false, false, 4}} {
		m.databases.conns[0].ReadOnly, m.databases.conns[0].Environment = env.ro, ""
		if env.prod {
			m.databases.conns[0].Environment = "production"
		}
		m.onDBRedisNSScan(dbRedisNSScanMsg{conn: "cache", db: env.db, path: ns, prefix: "k", keys: keys, more: true})
		if !m.confirmOpen {
			t.Fatal("no card")
		}
		m.confirmOpen = false
	}
	m.databases.conns[0].ReadOnly, m.databases.conns[0].Environment = false, ""
	// the result of a namespace delete
	w := &dbRedisWrite{conn: "cache", ns: &dbRedisNSDelete{path: ns, prefix: "session:", keys: []string{"a", "b"}}}
	m.onDBRedisNSDeleted(dbRedisWriteMsg{w: w, reply: int64(2)})
	if _, ok := m.databases.nodes[ns]; ok {
		t.Fatal("namespace kept in the tree")
	}
	w.ns.more = true
	m.onDBRedisNSDeleted(dbRedisWriteMsg{w: w, reply: int64(2)})
	m.onDBRedisNSDeleted(dbRedisWriteMsg{w: w, reply: int64(1), err: cdbErr{}})
	if !strings.Contains(covToastText(m), "Deleted 1 of 2 keys") {
		t.Fatalf("toast %q", covToastText(m))
	}
	// the write message of a failed command
	cmdW := &dbRedisWrite{conn: "cache", args: []string{"SET", "a", "b"}}
	m.onDBRedisWrite(dbRedisWriteMsg{w: cmdW, err: cdbErr{}})
	if !strings.Contains(covToastText(m), "The command failed") {
		t.Fatalf("toast %q", covToastText(m))
	}
	m.databases = nil
	m.onDBRedisWrite(dbRedisWriteMsg{w: cmdW})
	m.onDBRedisNSScan(dbRedisNSScanMsg{})
}

func TestCovRedisParseTTL(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "90": 90, "30m": 1800, "2h": 7200, "3d": 259200} {
		if got, err := dbRedisParseTTL(in); err != nil || got != want {
			t.Errorf("%q: %d %v", in, got, err)
		}
	}
	for _, bad := range []string{"0", "-4", "0d", "xd", "soon", "500ms"} {
		if _, err := dbRedisParseTTL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if args, err := dbRedisRenameCopyArgs(true, "a", "b"); err != nil || args[0] != "COPY" {
		t.Fatalf("%v %v", args, err)
	}
	if args, err := dbRedisRenameCopyArgs(false, "a", "b"); err != nil || args[0] != "RENAMENX" {
		t.Fatalf("%v %v", args, err)
	}
	for _, v := range []string{"", "a"} {
		if _, err := dbRedisRenameCopyArgs(false, "a", v); err == nil {
			t.Errorf("%q accepted", v)
		}
	}
	old := theme.CurrentIconMode
	defer func() { theme.CurrentIconMode = old }()
	theme.CurrentIconMode = theme.IconModeUnicode
	for _, typ := range []string{"string", "hash", "list", "set", "zset", "stream", "x"} {
		if dbRedisTypeGlyph(typ) == "" {
			t.Errorf("unicode glyph %s", typ)
		}
	}
	theme.CurrentIconMode = theme.IconModeASCII
	for _, typ := range []string{"string", "hash", "list", "set", "zset", "stream", "x"} {
		if g := dbRedisTypeGlyph(typ); len(g) != 1 {
			t.Errorf("ascii glyph %s = %q", typ, g)
		}
	}
}

func TestCovRedisMoreHelpers(t *testing.T) {
	if len(dbRedisMorePaletteItems()) < 10 || !dbRedisMoreID("redis-memory") || dbRedisMoreID("nope") {
		t.Fatal("palette ids")
	}
	if _, err := dbRedisPatternLine("  "); err == nil {
		t.Fatal("empty pattern")
	}
	if line, err := dbRedisPatternLine("user:*"); err != nil || !strings.HasPrefix(line, "KEYS") {
		t.Fatalf("%q %v", line, err)
	}
	for in, want := range map[string]string{"": "string", " Hash ": "hash", "sorted set": "zset", "sortedset": "zset", "zset": "zset", "stream": "stream"} {
		if got, err := dbRedisNewKeyType(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if _, err := dbRedisNewKeyType("blob"); err == nil {
		t.Fatal("unknown type")
	}
	for _, typ := range []string{"hash", "list", "set", "zset", "stream", "json", "string"} {
		if dbRedisNewValueLabel(typ) == "" {
			t.Errorf("label %s", typ)
		}
	}
	if dbRedisExportDefault("My Cache!") != "redis-export-My-Cache.json" || dbRedisExportDefault("  ") != "redis-export-redis.json" {
		t.Fatalf("%q", dbRedisExportDefault("My Cache!"))
	}
}

func TestCovRedisMorePaletteAndPrompts(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	s := func() *dbState { return m.databases }
	cdbRedisExpand(m, "cache")
	cdbRedisExpand(m, "cache/db/0")
	// not on a Redis connection / nothing selected
	m.databases.cursor = 99
	m.databaseRedisMorePalette("redis-modules")
	m.databases.databaseSelectPath("cache")
	cdbRedisOpenKey(t, m, "counter") // a shown page names the connection
	// server views run in the console
	for _, id := range []string{"redis-modules", "redis-search-indexes", "redis-memory", "redis-pubsub-channels", "redis-cluster-nodes"} {
		cdbPump(m, m.databaseRedisMorePalette(id))
	}
	// search keys
	cdbPump(m, m.databaseRedisMorePalette("redis-search-keys"))
	if !m.promptOpen || m.promptKind != promptKindDBRedisSearch {
		t.Fatalf("no search prompt: %q", cdbToast(m))
	}
	cdbSubmitP(t, m, "user:*")
	cdbPump(m, m.databaseRedisMorePalette("redis-search-keys"))
	cdbSubmitP(t, m, "  ")
	if !strings.Contains(cdbToast(m), "Nothing searched") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	// new key: type, name, value, card, run
	cdbPump(m, m.databaseRedisMorePalette("redis-new-key"))
	cdbSubmitP(t, m, "blob")
	if !strings.Contains(cdbToast(m), "No key made") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	cdbPump(m, m.databaseRedisMorePalette("redis-new-key"))
	cdbSubmitP(t, m, "hash")
	cdbSubmitP(t, m, " ")
	cdbPump(m, m.databaseRedisMorePalette("redis-new-key"))
	cdbSubmitP(t, m, "hash")
	cdbSubmitP(t, m, "profile:7")
	cdbSubmitP(t, m, "name Ada role admin")
	if !m.confirmOpen {
		t.Fatalf("no card: %q", cdbToast(m))
	}
	cdbConfirmP(t, m, "run")
	if f.Hash(0, "profile:7")["name"] != "Ada" {
		t.Fatalf("hash %v (toast %q)", f.Hash(0, "profile:7"), cdbToast(m))
	}
	// the name is taken
	cdbRedisReconnect(t, m)
	cdbPump(m, m.databaseRedisMorePalette("redis-new-key"))
	cdbSubmitP(t, m, "string")
	cdbSubmitP(t, m, "counter")
	cdbSubmitP(t, m, "1")
	if !strings.Contains(cdbToast(m), "already exists") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	// a value that cannot be a key
	cdbPump(m, m.databaseRedisMorePalette("redis-new-key"))
	cdbSubmitP(t, m, "zset")
	cdbSubmitP(t, m, "board2")
	cdbSubmitP(t, m, "notascore")
	// and a check that fails
	m.onDBRedisNewKeyCheck(dbRedisNewKeyCheckMsg{err: cdbErr{}})
	m.databases = nil
	m.onDBRedisNewKeyCheck(dbRedisNewKeyCheckMsg{})
	m.databases = s()
	_ = s
}

// cdbNoListen hides the Listen method of a session.
type cdbNoListen struct{ dbRedis }

// cdbListenSess is a Redis session that can listen.
type cdbListenSess struct {
	dbRedis
	msgs []dbclient.RedisMessage
	more bool
	err  error
}

func (l cdbListenSess) Listen(context.Context, string, int) ([]dbclient.RedisMessage, bool, error) {
	return l.msgs, l.more, l.err
}

func TestCovRedisJSONModuleInfoAndRead(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	cdbRedisOpenKey(t, m, "doc")
	// JSON path of the open key
	cdbPump(m, m.databaseRedisMorePalette("redis-json-path"))
	if !m.promptOpen || m.promptKind != promptKindDBRedisJSONPath {
		t.Fatalf("no path prompt: %q", cdbToast(m))
	}
	cdbSubmitP(t, m, "$.a")
	cdbRedisOpenKey(t, m, "doc")
	// module info of a string: none; of a stream: its groups; of a hash: none
	cdbPump(m, m.databaseRedisMorePalette("redis-key-module-info"))
	if !strings.Contains(cdbToast(m), "No module info for this key") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	cdbPump(m, m.databaseRedisMorePalette("redis-stream-groups"))
	if !strings.Contains(cdbToast(m), "Not a stream") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	cdbRedisOpenKey(t, m, "events")
	cdbPump(m, m.databaseRedisMorePalette("redis-stream-groups"))
	cdbPump(m, m.databaseRedisMorePalette("redis-key-module-info"))
	// a read that fails shows the error on the page; one that works shows a grid
	m.onDBRedisRead(dbRedisReadMsg{conn: "cache", args: []string{"X"}, err: cdbErr{}})
	m.onDBRedisRead(dbRedisReadMsg{conn: "cache", args: []string{"MODULE", "LIST"}, reply: []any{}})
	m.databases = nil
	if cmd := m.onDBRedisRead(dbRedisReadMsg{}); cmd != nil {
		t.Fatal("no databases")
	}
	m.databases = nil
}

func TestCovRedisMorePaletteNeedsAKeyAndASession(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	// no key selected, no page shown
	m.databases.databaseSelectPath("cache")
	for _, id := range []string{"redis-json-path", "redis-key-module-info", "redis-stream-groups"} {
		m.databaseRedisMorePalette(id)
		if !strings.Contains(cdbToast(m), "Select a Redis key first") {
			t.Fatalf("%s: toast %q", id, cdbToast(m))
		}
	}
	// a key selected in the tree
	cdbRedisExpand(m, "cache")
	cdbRedisExpand(m, "cache/db/0")
	m.databases.databaseSelectPath("cache/db/0/key/doc")
	if e, ok := m.databaseRedisTarget(); !ok || e.key != "doc" {
		t.Fatalf("target %+v %v", e, ok)
	}
	m.databaseRedisMorePalette("redis-json-path")
	m.promptOpen = false
	// without a session the palette connects
	delete(m.databases.sessions, "cache")
	if cmd := m.databaseRedisMorePalette("redis-json-path"); cmd == nil {
		t.Fatal("no connect")
	}
	if cmd := m.databaseRedisMorePalette("redis-search-keys"); cmd == nil {
		t.Fatal("no connect for search")
	}
	// a non-Redis connection
	m2 := cdbModel(t, dbclient.Conn{ID: "pg", Driver: "postgres", Host: "h", Port: 5432})
	cdbPGTree(m2.databases)
	m2.databases.databaseSelectPath("pg")
	m2.databaseRedisMorePalette("redis-memory")
	m2.databaseRedisServerView("INFO")
	m2.databaseRedisPaletteAdmin("redis-slowlog")
	m2.databaseRedisPaletteAdmin("redis-rename-key")
}

func TestCovRedisListenAndExport(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	cdbRedisOpenKey(t, m, "counter")
	// a session that cannot listen
	real := m.databases.sessions["cache"].(dbRedis)
	m.databases.sessions["cache"] = cdbNoListen{real}
	m.databaseRedisListen("cache", "*")
	if !strings.Contains(cdbToast(m), "Cannot listen here") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	m.databases.sessions["cache"] = cdbListenSess{dbRedis: real, msgs: []dbclient.RedisMessage{{Channel: "news", Data: "hi"}}, more: true}
	cdbPump(m, m.databaseRedisMorePalette("redis-listen"))
	if !m.promptOpen || m.promptKind != promptKindDBRedisListen {
		t.Fatalf("no listen prompt: %q", cdbToast(m))
	}
	cdbSubmitP(t, m, "")
	m.onDBRedisListen(dbRedisListenMsg{conn: "cache", pattern: "n*", err: cdbErr{}})
	m.databases = nil
	if cmd := m.onDBRedisListen(dbRedisListenMsg{}); cmd != nil {
		t.Fatal("no databases")
	}
}

func TestCovRedisExportFlow(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	cdbRedisOpenKey(t, m, "counter")
	out := filepath.Join(t.TempDir(), "export", "keys.json")
	cdbPump(m, m.databaseRedisMorePalette("redis-export-keys"))
	if !m.promptOpen || m.promptKind != promptKindDBRedisExportPattern {
		t.Fatalf("no pattern prompt: %q", cdbToast(m))
	}
	cdbSubmitP(t, m, " ")
	cdbSubmitP(t, m, out)
	if !m.confirmOpen || m.confirmKind != confirmKindDBRedisExport {
		t.Fatalf("no export card: %q", cdbToast(m))
	}
	cdbConfirmP(t, m, "export")
	b, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(b), "session:ab12") {
		t.Fatalf("export file: %v %q", err, b)
	}
	// again: the file exists, so the card says Replace; cancel leaves it
	cdbPump(m, m.databaseRedisMorePalette("redis-export-keys"))
	cdbSubmitP(t, m, "user:*")
	cdbSubmitP(t, m, out)
	cdbConfirmP(t, m, "cancel")
	// no key matches
	cdbPump(m, m.databaseRedisMorePalette("redis-export-keys"))
	cdbSubmitP(t, m, "nothing:*")
	cdbSubmitP(t, m, out)
	if !strings.Contains(cdbToast(m), "No keys to export") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	// the path is a folder
	cdbPump(m, m.databaseRedisMorePalette("redis-export-keys"))
	cdbSubmitP(t, m, "*")
	cdbSubmitP(t, m, t.TempDir())
	if !strings.Contains(cdbToast(m), "is a folder") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	// no session: connects; the card of a session that went away
	st := &dbRedisMore{conn: "cache", path: out}
	m.databases.console.redisMore = st
	delete(m.databases.sessions, "cache")
	m.confirmKind = confirmKindDBRedisExport
	if _, ok := m.databaseRedisExportConfirm("export"); !ok || !strings.Contains(cdbToast(m), "Not connected") {
		t.Fatalf("not connected: %q", cdbToast(m))
	}
	if cmd := m.databaseRedisExportScan(&dbRedisMore{conn: "cache"}); cmd == nil {
		t.Fatal("no connect")
	}
	// stale / failed scan answers, the done messages
	if cmd := m.onDBRedisExportScan(dbRedisExportScanMsg{st: &dbRedisMore{}}); cmd != nil {
		t.Fatal("stale scan")
	}
	st2 := &dbRedisMore{conn: "cache", pattern: "*"}
	m.databases.console.redisMore = st2
	m.onDBRedisExportScan(dbRedisExportScanMsg{st: st2, err: cdbErr{}})
	m.databases.console.redisMore = st2
	m.onDBRedisExportScan(dbRedisExportScanMsg{st: st2, keys: []string{"a"}, more: true})
	m.confirmOpen = false
	m.onDBRedisExportDone(dbRedisExportDoneMsg{display: "x", n: 2})
	m.onDBRedisExportDone(dbRedisExportDoneMsg{display: "x", err: cdbErr{}})
	m.databases = nil
	m.onDBRedisExportScan(dbRedisExportScanMsg{})
	// writing: nil entries, an unwritable folder
	dir := t.TempDir()
	if err := dbRedisWriteExport(filepath.Join(dir, "a.json"), nil); err != nil {
		t.Fatal(err)
	}
	if err := dbRedisWriteExport(filepath.Join(dir, "a.json", "b.json"), nil); err == nil {
		t.Fatal("below a file")
	}
	if err := os.MkdirAll(filepath.Join(dir, "d.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dbRedisWriteExport(filepath.Join(dir, "d.json"), nil); err == nil {
		t.Fatal("tmp is a folder")
	}
	if err := os.MkdirAll(filepath.Join(dir, "e.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dbRedisWriteExport(filepath.Join(dir, "e.json"), nil); err == nil {
		t.Fatal("target is a folder")
	}
	// prompts with no flow in progress
	m2 := cdbModel(t)
	if cmd, ok := m2.databaseRedisMorePrompt(promptKindDBRedisSearch, "x"); !ok || cmd != nil {
		t.Fatal("no flow")
	}
	if _, ok := m2.databaseRedisMorePrompt(0, "x"); ok {
		t.Fatal("foreign prompt")
	}
}

func TestCovRedisConsoleGuardAndAdminPalette(t *testing.T) {
	f := cdbRedisSeed()
	m := cdbRedisModel(t, f, dbclient.Conn{})
	cdbRedisOpenKey(t, m, "counter")
	run := func(sql string, line int) {
		r := &dbRun{conn: "cache", sql: sql, line: line}
		cdbPump(m, m.databaseRedisPrepare(r))
	}
	run("GET counter\nGET doc", 0)
	run("# note\nGET counter\n\nGET doc", -2)
	run("GET counter\nGET doc", 2)
	run("GET counter", 5) // no command on that line
	if !strings.Contains(cdbToast(m), "No command on this line") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	run("# only a comment", 0)
	if !strings.Contains(cdbToast(m), "No command to run") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	// a write asks; the card runs it
	run("SET counter 5", 0)
	if !m.confirmOpen || m.confirmKind != confirmKindDBRun {
		t.Fatalf("no card: %q", cdbToast(m))
	}
	m.confirmOpen = false
	// read-only and production connections say so
	c, _ := m.databases.connection("cache")
	for _, tc := range []dbclient.Conn{{ReadOnly: true}, {Environment: "production"}} {
		c.ReadOnly, c.Environment = tc.ReadOnly, tc.Environment
		m.databaseRedisReview(&dbRun{conn: "cache", sql: "SET a b"}, c)
		m.confirmOpen = false
		m.databaseRedisReview(&dbRun{conn: "cache", sql: "FLUSHALL"}, c)
		m.confirmOpen = false
	}
	// refused, unreadable, empty
	m.databaseRedisReview(&dbRun{conn: "cache", sql: "MONITOR"}, c)
	if !strings.Contains(cdbToast(m), "Not sent") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	m.databaseRedisReview(&dbRun{conn: "cache", sql: `GET "unterminated`}, c)
	if !strings.Contains(cdbToast(m), "could not be read") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	if cmd := m.databaseRedisReview(&dbRun{conn: "cache", sql: "  "}, c); cmd != nil {
		t.Fatal("empty command")
	}
	// palette Set TTL / Delete Key on the open page, then on a tree key
	cdbPump(m, m.databaseRedisPaletteKey("redis-set-ttl"))
	m.promptOpen = false
	cdbPump(m, m.databaseRedisPaletteKey("redis-delete-key"))
	m.confirmOpen = false
	cdbPump(m, m.databaseRedisPaletteAdmin("redis-rename-key"))
	m.promptOpen = false
	cdbPump(m, m.databaseRedisPaletteAdmin("redis-copy-key"))
	m.promptOpen = false
	cdbPump(m, m.databaseRedisPaletteAdmin("redis-slowlog"))
	cdbPump(m, m.databaseRedisServerView("INFO"))
	// no page, no key selected
	m.databases.console.shown = nil
	m.databases.cursor = 99
	m.databaseRedisPaletteKey("redis-set-ttl")
	if !strings.Contains(cdbToast(m), "Select a Redis key first") {
		t.Fatalf("toast %q", cdbToast(m))
	}
	// a key selected in the tree
	cdbRedisExpand(m, "cache")
	cdbRedisExpand(m, "cache/db/0")
	m.databases.databaseSelectPath("cache/db/0/key/counter")
	m.databaseRedisPaletteKey("redis-set-ttl")
	if !m.promptOpen {
		t.Fatal("no ttl prompt")
	}
	m.promptOpen = false
	if _, ok := m.databaseRedisTarget(); !ok {
		t.Fatal("tree target")
	}
	// the database of the selected row
	cdbRedisExpand(m, "cache/db/0")
	if db := m.databaseRedisRowDB(c); db != 0 {
		t.Fatalf("db %d", db)
	}
	// a rename / copy answered after the edit state is gone
	if cmd, ok := m.databaseRedisAdminPrompt(promptKindDBRedisRename, "x"); !ok || cmd != nil {
		t.Fatal("no edit")
	}
	if _, ok := m.databaseRedisAdminPrompt(0, "x"); ok {
		t.Fatal("foreign prompt")
	}
	// the admin result: taken name, renamed open key
	m.onDBRedisAdminDone(&dbRedisWrite{admin: "copy", dst: "d"}, int64(0), nil)
	m.onDBRedisAdminDone(&dbRedisWrite{admin: "new", dst: "d"}, nil, nil)
	m.onDBRedisAdminDone(&dbRedisWrite{admin: "new", dst: "d"}, int64(1), nil)
	m.onDBRedisAdminDone(&dbRedisWrite{admin: "rename", conn: "cache", key: "counter", dst: "c2"}, int64(1), nil)
}

func TestCovRedisDemoPages(t *testing.T) {
	m := cdbRealModel(t)
	m.databaseRedisDemo()
	if len(m.databases.rows()) < 8 {
		t.Fatalf("rows %d", len(m.databases.rows()))
	}
	cdbPump(m, m.databaseRedisKeyDemo())
	if s := m.databases.console; s.shown == nil || s.shown.redis == nil || s.shown.redis.key != "session:ab12" {
		t.Fatal("no demo page")
	}
}

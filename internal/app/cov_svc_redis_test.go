package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// covSvcRedisSeed adds one key of every type at the top of db0.
func covSvcRedisSeed() *dbtest.FakeRedis {
	f := redisSeed()
	f.RPush(0, "qlist", "alpha", "beta", "gamma")
	f.SAdd(0, "qset", "red", "green")
	f.ZAdd(0, "qzset", 1, "one")
	f.ZAdd(0, "qzset", 2, "two")
	f.XAdd(0, "qstream", "1-0", "event", "login")
	f.SetString(0, "qjson", `{"a":1,"b":[1,2]}`)
	return f
}

// covSvcRedisOpen filters the tree to key and opens its value page.
func covSvcRedisOpen(t *testing.T, d *testkit.Driver, key, pageText string) {
	t.Helper()
	d.Key("/")
	d.Type(key)
	d.Key("enter")
	waitFrame(t, d, key)
	d.Key("G", "enter")
	if !d.WaitForText(pageText, 3*time.Second) {
		t.Fatalf("no page with %q:\n%s", pageText, ansi.Strip(d.Frame()))
	}
}

func covSvcRedisDriver(t *testing.T) (*testkit.Driver, *dbtest.FakeRedis) {
	t.Helper()
	f := covSvcRedisSeed()
	d := redisDriver(t, 110, 34, f, nil)
	openDB0(t, d)
	return d, f
}

func covSvcAnswer(t *testing.T, d *testkit.Driver, card string, ok func() bool, what string) {
	t.Helper()
	waitFrame(t, d, card)
	d.Key("right", "enter")
	if !d.WaitFor(ok, 3*time.Second) {
		t.Fatalf("%s did not happen", what)
	}
}

// A list: e sets an item (LSET), + pushes, - removes by value.
func TestCovSvcRedisListEdits(t *testing.T) {
	d, f := covSvcRedisDriver(t)
	covSvcRedisOpen(t, d, "qlist", "alpha")
	page := dbText(t, d)
	if !strings.Contains(page, "list") || !strings.Contains(page, "gamma") {
		t.Fatalf("list page:\n%s", page)
	}
	cursorTo(t, d, "beta")
	d.Key("e")
	waitFrame(t, d, "New value of item")
	d.Key("ctrl+u")
	for i := 0; i < 8; i++ {
		d.Key("backspace")
	}
	d.Type("BETA")
	d.Key("enter")
	covSvcAnswer(t, d, "LSET qlist", func() bool { return f.Sent("LSET") }, "LSET")
	d.Key("+")
	waitFrame(t, d, "Value (RPUSH)")
	d.Type("delta")
	d.Key("enter")
	covSvcAnswer(t, d, "RPUSH qlist delta", func() bool { return f.Sent("RPUSH") }, "RPUSH")
	dbWaitPage(t, d, "delta")
	cursorTo(t, d, "gamma")
	d.Key("-")
	covSvcAnswer(t, d, "LREM qlist", func() bool { return f.Sent("LREM") }, "LREM")
}

// A set: + adds, e says it cannot change in place, - removes.
func TestCovSvcRedisSetEdits(t *testing.T) {
	d, f := covSvcRedisDriver(t)
	covSvcRedisOpen(t, d, "qset", "green")
	d.Key("e")
	waitFrame(t, d, "cannot be changed in place")
	d.Key("+")
	waitFrame(t, d, "New member")
	d.Type("blue")
	d.Key("enter")
	covSvcAnswer(t, d, "SADD qset blue", func() bool { return f.Sent("SADD") }, "SADD")
	dbWaitPage(t, d, "blue")
	cursorTo(t, d, "red")
	d.Key("-")
	covSvcAnswer(t, d, "SREM qset", func() bool { return f.Sent("SREM") }, "SREM")
}

// A sorted set: e changes a score (a number), + adds a member and its
// score, - removes the member.
func TestCovSvcRedisZSetEdits(t *testing.T) {
	d, f := covSvcRedisDriver(t)
	covSvcRedisOpen(t, d, "qzset", "two")
	cursorTo(t, d, "two")
	d.Key("e")
	waitFrame(t, d, "New score of two")
	d.Key("ctrl+u")
	for i := 0; i < 6; i++ {
		d.Key("backspace")
	}
	d.Type("abc")
	d.Key("enter")
	waitFrame(t, d, "number")
	d.Key("esc")
	cursorTo(t, d, "two")
	d.Key("e")
	waitFrame(t, d, "New score of two")
	for i := 0; i < 6; i++ {
		d.Key("backspace")
	}
	d.Type("5")
	d.Key("enter")
	covSvcAnswer(t, d, "ZADD qzset 5 two", func() bool { return f.Sent("ZADD") }, "ZADD")
	d.Key("+")
	waitFrame(t, d, "New member")
	d.Type("three")
	d.Key("enter")
	waitFrame(t, d, "Score of three")
	d.Key("backspace")
	d.Type("3")
	d.Key("enter")
	waitFrame(t, d, "ZADD qzset 3 three")
	d.Key("right", "enter")
	dbWaitPage(t, d, "three")
	cursorTo(t, d, "one")
	d.Key("-")
	covSvcAnswer(t, d, "ZREM qzset", func() bool { return f.Sent("ZREM") }, "ZREM")
}

// A stream: + adds an entry from "field value" pairs; an odd count is
// refused; - deletes the entry.
func TestCovSvcRedisStreamEdits(t *testing.T) {
	d, f := covSvcRedisDriver(t)
	covSvcRedisOpen(t, d, "qstream", "login")
	d.Key("+")
	waitFrame(t, d, "Fields (field value")
	d.Type("only")
	d.Key("enter")
	waitFrame(t, d, "field value")
	d.Key("esc")
	d.Key("+")
	waitFrame(t, d, "Fields (field value")
	d.Type("event logout")
	d.Key("enter")
	covSvcAnswer(t, d, "XADD qstream", func() bool { return f.Sent("XADD") }, "XADD")
	cursorTo(t, d, "1-0")
	d.Key("-")
	covSvcAnswer(t, d, "XDEL qstream", func() bool { return f.Sent("XDEL") }, "XDEL")
}

// A JSON string is shown pretty; e opens it in an edit buffer.
func TestCovSvcRedisStringPage(t *testing.T) {
	d, _ := covSvcRedisDriver(t)
	covSvcRedisOpen(t, d, "qjson", `"a": 1`)
	d.Key("+")
	waitFrame(t, d, "Nothing to add to a string")
	d.Key("e")
	waitFrame(t, d, "Edit the value, then :w")
	if !strings.Contains(dbBufName(t, d), "Redis Value") {
		t.Errorf("buffer = %q", dbBufName(t, d))
	}
}

// Tree keys on a key row: y copies, ? shows the keys, D asks to delete.
func TestCovSvcRedisTreeKeys(t *testing.T) {
	d, f := covSvcRedisDriver(t)
	d.Key("/")
	d.Type("counter")
	d.Key("enter")
	waitFrame(t, d, "counter")
	d.Key("G")
	d.Key("?")
	waitFrame(t, d, "Redis")
	d.Key("esc")
	d.Key("t") // on a key: its TTL
	waitFrame(t, d, "Time to live")
	d.Key("esc")
	d.Key("D")
	waitFrame(t, d, "DEL counter")
	d.Key("esc")
	d.Settle(100 * time.Millisecond)
	if !f.Exists(0, "counter") {
		t.Fatal("Esc deleted the key")
	}
	d.Key("D")
	covSvcAnswer(t, d, "DEL counter", func() bool { return !f.Exists(0, "counter") }, "DEL")
	if strings.Contains(ansi.Strip(d.Frame()), "panic") {
		t.Fatal(d.Frame())
	}
}

package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/testkit"
)

const redisConns = `{"connections":[{"id":"cache","driver":"redis","host":"cache.internal","port":6379},` +
	`{"id":"prod-cache","driver":"redis","host":"prod.internal","port":6379,"environment":"production"}]}`

// redisSeed is a small keyspace: two namespaces, a plain key, a hash of 7
// fields with a TTL.
func redisSeed() *dbtest.FakeRedis {
	f := dbtest.NewFakeRedis()
	f.HSet(0, "session:ab12", "user_id", "48212", "plan", "pro", "created", "1760770262", "ip", "203.0.113.9",
		"ua", "Mozilla/5.0", "cart", `{"items":[{"sku":"A1","qty":2}],"total":1999}`, "flags", "")
	f.Expire(0, "session:ab12", 23*time.Minute)
	f.SetString(0, "session:cd34", "token")
	f.SetString(0, "user:1", "ada")
	f.RPush(0, "user:2:events", "login", "logout")
	f.SetString(0, "counter", "41")
	f.SetString(3, "other", "x")
	return f
}

func redisDriver(t *testing.T, w, h int, f *dbtest.FakeRedis, files map[string]string) *testkit.Driver {
	t.Helper()
	t.Cleanup(app.SetRedisDialForTest(f.Dial))
	t.Cleanup(f.Close)
	all := map[string]string{"README.md": "redis fixture\n", ".termocode/databases.json": redisConns}
	for k, v := range files {
		all[k] = v
	}
	d := testkit.New(t, testkit.Options{W: w, H: h, Nvim: true, Files: all})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	if !d.WaitForText("DATABASES", time.Second) {
		t.Fatalf("no sidebar:\n%s", ansi.Strip(d.Frame()))
	}
	return d
}

func waitFrame(t *testing.T, d *testkit.Driver, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if !d.WaitForText(s, 3*time.Second) {
			t.Fatalf("frame lacks %q:\n%s", s, ansi.Strip(d.Frame()))
		}
	}
}

// openSessionKey browses cache → db0 → session: → ab12 and opens it.
func openSessionKey(t *testing.T, d *testkit.Driver) {
	t.Helper()
	d.Key("enter") // connect: databases
	waitFrame(t, d, "db0", "db3")
	d.Key("down", "enter") // db0: namespaces and keys
	waitFrame(t, d, "session:", "user:", "counter")
	d.Key("down", "enter") // session:
	waitFrame(t, d, "ab12", "cd34")
	d.Key("down")
	waitFrame(t, d, "session:ab12")
	d.Key("enter")
	dbWaitPage(t, d, "field ")
}

// redisLine puts the cursor on line n of the cache.redis console.
func redisLine(t *testing.T, d *testkit.Driver, n int) {
	t.Helper()
	d.Action("open:cache.redis")
	d.Key("esc")
	dbLua(t, d, `vim.api.nvim_win_set_cursor(0,{...,0}); return ''`, n)
	d.Settle(50 * time.Millisecond)
}

func TestRedisTreeGroupsByNamespaceAndOpensAHashValuePage(t *testing.T) {
	f := redisSeed()
	d := redisDriver(t, 100, 30, f, nil)
	openSessionKey(t, d)
	page := dbText(t, d)
	for _, want := range []string{"Redis: session:ab12", "hash", "7 fields", "TTL 2", "field", "value", "user_id", "48212", `{"items"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}
	if f.Sent("KEYS") || f.Sent("HGETALL") {
		t.Fatalf("KEYS or a whole-value read was sent: %q", f.Commands())
	}
	if !f.Sent("SCAN") || !f.Sent("HSCAN") {
		t.Fatalf("no SCAN / HSCAN: %q", f.Commands())
	}
}

func TestRedisValuePageWritesAskWithTheExactCommand(t *testing.T) {
	f := redisSeed()
	d := redisDriver(t, 100, 30, f, nil)
	openSessionKey(t, d)

	// e on a field: a prompt, then the card with the exact command.
	cursorTo(t, d, "plan ")
	d.Key("e")
	waitFrame(t, d, "New value of plan")
	d.Key("ctrl+u")
	for i := 0; i < 10; i++ {
		d.Key("backspace")
	}
	d.Type("pro max")
	d.Key("enter")
	waitFrame(t, d, `HSET session:ab12 plan "pro max"`)
	if f.Hash(0, "session:ab12")["plan"] != "pro" {
		t.Fatal("the write ran before the card")
	}
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return f.Hash(0, "session:ab12")["plan"] == "pro max" }, 3*time.Second) {
		t.Fatalf("HSET did not run: %q", f.Commands())
	}
	dbWaitPage(t, d, "pro max")

	// + adds a field: field, then value.
	d.Key("+")
	waitFrame(t, d, "New field")
	d.Type("color")
	d.Key("enter")
	waitFrame(t, d, "Value of color")
	d.Type("blue")
	d.Key("enter")
	waitFrame(t, d, "HSET session:ab12 color blue")
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return f.Hash(0, "session:ab12")["color"] == "blue" }, 3*time.Second) {
		t.Fatal("the new field was not written")
	}
	dbWaitPage(t, d, "color")

	// - deletes the field under the cursor.
	cursorTo(t, d, "ip ")
	d.Key("-")
	waitFrame(t, d, "HDEL session:ab12 ip")
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { _, ok := f.Hash(0, "session:ab12")["ip"]; return !ok }, 3*time.Second) {
		t.Fatal("HDEL did not run")
	}

	// t sets the TTL; empty would be PERSIST.
	d.Key("t")
	waitFrame(t, d, "Time to live")
	d.Key("ctrl+u")
	for i := 0; i < 10; i++ {
		d.Key("backspace")
	}
	d.Type("90")
	d.Key("enter")
	waitFrame(t, d, "EXPIRE session:ab12 90")
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { ttl := f.TTL(0, "session:ab12"); return ttl > 80*time.Second && ttl <= 90*time.Second }, 3*time.Second) {
		t.Fatalf("TTL = %v", f.TTL(0, "session:ab12"))
	}

	// D deletes the key after a card; Cancel first keeps it.
	d.Key("D")
	waitFrame(t, d, "DEL session:ab12", "cannot be undone")
	d.Key("esc")
	d.Settle(200 * time.Millisecond)
	if !f.Exists(0, "session:ab12") {
		t.Fatal("Esc deleted the key")
	}
	d.Key("D")
	waitFrame(t, d, "DEL session:ab12")
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return !f.Exists(0, "session:ab12") }, 3*time.Second) {
		t.Fatal("DEL did not run")
	}
}

func TestRedisProductionWriteCardSaysProduction(t *testing.T) {
	f := redisSeed()
	d := redisDriver(t, 100, 30, f, nil)
	d.Key("down") // prod-cache
	waitFrame(t, d, "prod-cache")
	openSessionKey(t, d)
	d.Key("D")
	waitFrame(t, d, "Write to production (prod-cache)?", "DEL session:ab12")
	if strings.Contains(ansi.Strip(d.Frame()), "Always") {
		t.Fatal("a production card offers Always")
	}
}

func TestRedisConsoleRunsAReadAtOnceAndGuardsWritesAndTheNeverList(t *testing.T) {
	f := redisSeed()
	d := redisDriver(t, 100, 30, f, map[string]string{"cache.redis": "# @conn cache\nGET counter\nKEYS user:*\nSET counter 42\nFLUSHALL\nMONITOR\nAUTH app hunter2-secret\n"})
	d.Action("open:cache.redis")
	d.Key("esc")
	dbLua(t, d, `vim.api.nvim_win_set_cursor(0,{2,0}); return ''`)
	dbSend(d)
	dbWaitPage(t, d, "41")

	// KEYS is rewritten to SCAN with a note.
	redisLine(t, d, 3)
	dbSend(d)
	dbWaitPage(t, d, "user:1")
	if f.Sent("KEYS") {
		t.Fatal("KEYS was sent")
	}

	// A write asks with the command.
	redisLine(t, d, 4)
	dbSend(d)
	waitFrame(t, d, "Run this command on cache?", "SET counter 42")
	d.Key("esc")
	d.Settle(200 * time.Millisecond)
	if f.String(0, "counter") != "41" {
		t.Fatal("SET ran without the card")
	}

	// The never-list: a strong card naming the danger.
	redisLine(t, d, 5)
	dbSend(d)
	waitFrame(t, d, "Dangerous command on cache", "FLUSHALL")
	d.Key("esc")
	d.Settle(200 * time.Millisecond)
	if f.Sent("FLUSHALL") {
		t.Fatal("FLUSHALL was sent")
	}

	// A streaming command is never sent.
	redisLine(t, d, 6)
	dbSend(d)
	waitFrame(t, d, "Not sent")
	if f.Sent("MONITOR") {
		t.Fatal("MONITOR was sent")
	}

	// AUTH runs (a write card) but its secret never shows on the page.
	redisLine(t, d, 7)
	dbSend(d)
	waitFrame(t, d, "AUTH [redacted] [redacted]") // the card (the buffer behind it holds the user's text)
	d.Key("right", "enter")
	dbWaitPage(t, d, "WRONGPASS")
	if strings.Contains(dbText(t, d), "hunter2-secret") {
		t.Fatalf("the AUTH secret shows on the page:\n%s", dbText(t, d))
	}
}

func TestRedisHiddenViewClosesTheSessionAndSendsNothing(t *testing.T) {
	f := redisSeed()
	d := redisDriver(t, 100, 30, f, nil)
	d.Key("enter")
	waitFrame(t, d, "db0")
	d.Action("view:files")
	d.Settle(300 * time.Millisecond)
	if _, n := d.Model().DatabaseStateForTest(); n != 0 {
		t.Fatalf("%d sessions stay open with the view hidden", n)
	}
	before := len(f.Commands())
	d.Settle(1500 * time.Millisecond)
	if after := len(f.Commands()); after != before {
		t.Fatalf("commands sent while hidden: %q", f.Commands()[before:])
	}
}

func TestRedisTreeFitsEightyColumnsWithAsciiTypes(t *testing.T) {
	f := redisSeed()
	d := redisDriver(t, 80, 24, f, nil)
	d.Key("enter")
	waitFrame(t, d, "db0")
	d.Key("down", "enter")
	waitFrame(t, d, "session:", "counter")
	for _, line := range strings.Split(ansi.Strip(d.Frame()), "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Fatalf("line wider than 80 (%d): %q", w, line)
		}
	}
}

func TestRedisValuePagePagesAndTheSendKeyReloads(t *testing.T) {
	f := redisSeed()
	for i := 0; i < 250; i++ {
		f.HSet(0, "session:ab12", fmt.Sprintf("k%03d", i), "v")
	}
	d := redisDriver(t, 100, 30, f, nil)
	openSessionKey(t, d)
	if !strings.Contains(dbText(t, d), "page 1") {
		t.Fatalf("no page number:\n%s", dbText(t, d))
	}
	d.Key("]")
	dbWaitPage(t, d, "page 2")
	d.Key("[")
	dbWaitPage(t, d, "page 1")
	before := len(f.Commands())
	dbSend(d) // the send key on the page reloads it, no card
	if !d.WaitFor(func() bool { return len(f.Commands()) > before }, 3*time.Second) {
		t.Fatal("the send key did not reload the value")
	}
	if strings.Contains(ansi.Strip(d.Frame()), "Run this command") {
		t.Fatalf("a reload asked a card:\n%s", ansi.Strip(d.Frame()))
	}
}

package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const covSvcRedisRO = `{"connections":[{"id":"cache","driver":"redis","host":"cache.internal","port":6379,"readOnly":true},` +
	`{"id":"other","driver":"redis","url":"redis://other.internal:6379/3"}]}`

// A read-only connection: a write card says so; on a database other than
// the connection's own the card names it.
func TestCovSvcRedisReadOnlyCards(t *testing.T) {
	f := covSvcRedisSeed()
	d := redisDriver(t, 110, 34, f, map[string]string{".termocode/databases.json": covSvcRedisRO})
	openDB0(t, d)
	covSvcRedisOpen(t, d, "qset", "green")
	d.Key("D")
	covSvcWaitFrame(t, d, "cache is read-only. Run anyway?")
	d.Key("esc")
	d.Settle(100 * time.Millisecond)
	if !f.Exists(0, "qset") {
		t.Fatal("Esc deleted the key")
	}
}

// The console runs every command of the file (run file); a line that
// cannot be read is a toast; an empty line says there is no command.
func TestCovSvcRedisConsoleRunFile(t *testing.T) {
	f := covSvcRedisSeed()
	d := redisDriver(t, 110, 34, f, map[string]string{"cache.redis": "# @conn cache\nGET counter\n\nGET user:1\nGET \"open\n"})
	d.Action("open:cache.redis")
	d.Key("esc")
	d.Palette("db-run-file")
	if !d.WaitFor(func() bool { return f.Sent("GET") }, 4*time.Second) {
		t.Fatalf("run file sent nothing: %q\n%s", f.Commands(), ansi.Strip(d.Frame()))
	}
	redisLine(t, d, 3)
	dbSend(d)
	covSvcWaitFrame(t, d, "No command on this line")
	redisLine(t, d, 5)
	dbSend(d)
	covSvcWaitFrame(t, d, "could not be read")
}

// The palette's Set TTL and Delete Key act on the page's key, else on
// the tree's key, else ask to select one.
func TestCovSvcRedisPaletteKeys(t *testing.T) {
	d, f := covSvcRedisDriver(t)
	d.Palette("redis-set-ttl")
	covSvcWaitFrame(t, d, "Select a Redis key first")
	covSvcRedisOpen(t, d, "qset", "green")
	d.Palette("redis-set-ttl")
	covSvcWaitFrame(t, d, "Time to live")
	d.Key("esc")
	d.Palette("redis-delete-key")
	covSvcWaitFrame(t, d, "DEL qset")
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return !f.Exists(0, "qset") }, 3*time.Second) {
		t.Fatal("DEL did not run")
	}
	if strings.Contains(ansi.Strip(d.Frame()), "panic") {
		t.Fatal(d.Frame())
	}
}

// A string edited in its buffer: :w shows the SET card; Run writes it.
func TestCovSvcRedisStringBufferWrite(t *testing.T) {
	d, f := covSvcRedisDriver(t)
	covSvcRedisOpen(t, d, "counter", "41")
	d.Key("e")
	covSvcWaitFrame(t, d, "Edit the value, then :w")
	dbLua(t, d, `vim.api.nvim_buf_set_lines(0, 0, -1, false, {'99'}); vim.cmd('write'); return ''`)
	covSvcWaitFrame(t, d, "SET counter 99")
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return f.String(0, "counter") == "99" }, 3*time.Second) {
		t.Fatalf("SET did not run: %q", f.Commands())
	}
}

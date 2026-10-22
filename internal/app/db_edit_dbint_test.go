//go:build dbint

package app_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// Live PostgreSQL, real keys: edit a console result (a plain SELECT whose
// key is looked up), delete and add rows, submit; then a manual
// transaction rolled back with Z.
// TERMOCODE_DBINT_PG_URL=postgres://user:pass@host:port/db go test -tags dbint -run TestDBEditLivePostgres ./internal/app
func TestDBEditLivePostgres(t *testing.T) {
	url := os.Getenv("TERMOCODE_DBINT_PG_URL")
	if url == "" {
		t.Skip("TERMOCODE_DBINT_PG_URL is not set")
	}
	c, err := dbclient.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := dbclient.NewSQLSession(ctx, c, c.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	run := func(q string) dbclient.Result {
		r := admin.Run(ctx, q, dbclient.RunOptions{})
		if r.Err != nil {
			t.Fatalf("%s: %v", q, r.Err)
		}
		return r
	}
	run(`DROP TABLE IF EXISTS public."Edit Live"`)
	run(`CREATE TABLE public."Edit Live" (id bigint PRIMARY KEY, name text NOT NULL, qty int DEFAULT 5, seen timestamptz)`)
	run(`INSERT INTO public."Edit Live" VALUES (1,'apple',1,now()),(2,'pear',2,NULL),(3,'plum',3,now())`)
	defer admin.Run(context.Background(), `DROP TABLE IF EXISTS public."Edit Live"`, dbclient.RunOptions{})
	value := func(q string) string {
		r := run(q)
		if len(r.Rows) == 0 {
			return "<none>"
		}
		return r.Rows[0][0].String()
	}
	t.Setenv("TC_DBINT_PW", c.Password)
	conf := fmt.Sprintf(`{"connections":[{"id":"live","driver":"postgres","host":%q,"port":%d,"user":%q,"database":%q,"password":"${env:TC_DBINT_PW}","params":{"sslmode":"disable"}}]}`, c.Host, c.Port, c.User, c.Database)
	d := testkit.New(t, testkit.Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"README.md": "x\n", ".termocode/databases.json": conf}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("g", "n")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "consoles") }, 3*time.Second) {
		t.Fatalf("no console: %s", dbBufName(t, d))
	}
	dbLua(t, d, `vim.api.nvim_buf_set_lines(0,0,-1,false,{'-- @conn live','SELECT * FROM public."Edit Live" ORDER BY id;'}); vim.cmd('stopinsert'); vim.cmd('silent write'); vim.api.nvim_win_set_cursor(0,{2,0}); return ''`)
	d.Key("esc")
	dbSend(d)
	dbWaitPage(t, d, "apple")
	cursorTo(t, d, "pear")
	setCell(t, d, "it's a pear")
	cursorTo(t, d, "plum")
	d.Key("-", "+")
	dbWaitPage(t, d, "3 changes")
	dbLua(t, d, `vim.fn.search('<default>','cw'); return ''`)
	setCell(t, d, "4")
	dbLua(t, d, `vim.fn.search('<default>','cw'); return ''`)
	setCell(t, d, "fig")
	d.Key("w")
	if !d.WaitForText("Submit 3 changes to live", 2*time.Second) {
		t.Fatalf("card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "right", "enter") // Submit is the last button
	if !d.WaitFor(func() bool { return value(`SELECT name FROM public."Edit Live" WHERE id = 2`) == "it's a pear" }, 5*time.Second) {
		t.Fatalf("not saved:\n%s", ansi.Strip(d.Frame()))
	}
	if got := value(`SELECT count(*) FROM public."Edit Live" WHERE name = 'plum'`); got != "0" {
		t.Fatal("plum is still there")
	}
	if got := value(`SELECT qty FROM public."Edit Live" WHERE id = 4`); got != "5" {
		t.Fatalf("new row: %s", got)
	}
	dbWaitPage(t, d, "it's a pear")
	if strings.Contains(dbText(t, d), "change") {
		t.Fatalf("marks after submit:\n%s", dbText(t, d))
	}
	// Manual mode, then roll back.
	d.Key("f1")
	d.Type("Databases: Transaction Mode")
	d.Key("enter")
	d.WaitForText("Transaction mode for live", time.Second)
	d.Type("Manual")
	d.Key("enter")
	cursorTo(t, d, "apple")
	setCell(t, d, "banana")
	d.Key("w")
	d.WaitForText("(open transaction)", 2*time.Second)
	d.Key("enter")
	dbWaitPage(t, d, "banana")
	if got := value(`SELECT name FROM public."Edit Live" WHERE id = 1`); got != "apple" {
		t.Fatalf("another session sees an uncommitted change: %s", got)
	}
	d.Key("Z")
	dbWaitPage(t, d, "apple")
	if got := value(`SELECT name FROM public."Edit Live" WHERE id = 1`); got != "apple" {
		t.Fatalf("after rollback: %s", got)
	}
}

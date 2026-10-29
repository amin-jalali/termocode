package testkit

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// dbConsoleDriver starts the app with a SQLite connection "lite" on
// app.db, made by fixture (SQL run by the sqlite3 CLI), and opens a SQL
// console on it. Skipped with -short or without sqlite3.
func dbConsoleDriver(t *testing.T, w, h int, fixture string) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	files := textFiles()
	files[".termocode/databases.json"] = `{"connections":[{"id":"lite","driver":"sqlite","path":"app.db"}]}`
	d := New(t, Options{W: w, H: h, Nvim: true, Files: files,
		Setup: func(tb testing.TB, dir string) {
			cmd := exec.Command("sqlite3", filepath.Join(dir, "app.db"))
			cmd.Stdin = strings.NewReader(fixture)
			if out, err := cmd.CombinedOutput(); err != nil {
				tb.Fatalf("fixture: %v %s", err, out)
			}
		}})
	d.Palette("db-focus")
	if !d.WaitForText("lite", 5*time.Second) {
		t.Fatalf("no connection in the Databases view:\n%s", screen(d))
	}
	d.Palette("db-new-console")
	d.Settle(500 * time.Millisecond)
	return d
}

// runSQL puts sql in the console (under its "-- @conn lite" line) and
// runs it; it waits until want
// is on screen.
func runSQL(d *Driver, sql, want string, timeout time.Duration) time.Duration {
	d.T.Helper()
	lua(d, `vim.api.nvim_buf_set_lines(0, 0, -1, false, {"-- @conn lite", ...}); vim.api.nvim_win_set_cursor(0, {2, 0}); return ""`, sql)
	t0 := time.Now()
	d.Palette("db-run")
	if !d.WaitForText(want, timeout) {
		d.T.Fatalf("%q did not show %q:\n%s", sql, want, screen(d))
	}
	return time.Since(t0)
}

// Spec (SCN-11): Given a table whose cells hold CJK, emoji (ZWJ, flags),
// combining accents and Persian, some longer than a column,
// when the user runs SELECT * at 80x24 and 160x40,
// then the result grid keeps every row at the window width and cuts no
// glyph in half.
func TestWideDBResultCells(t *testing.T) {
	fixture := "CREATE TABLE t(id INTEGER PRIMARY KEY, name TEXT, note TEXT);\n" +
		fmt.Sprintf("INSERT INTO t(name, note) VALUES ('%s', '%s'), ('%s', '%s'), ('%s', '%s'), ('%s', '%s');\n",
			wideCJK, strings.Repeat(wideMixed, 4),
			wideDev+wideFlag, strings.Repeat(wideFlag, 30),
			"caf"+wideMark, strings.Repeat("e\u0301", 50),
			wideFa, strings.Repeat(wideFa+" ", 8))
	for _, sz := range [][2]int{{80, 24}, {160, 40}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			d := dbConsoleDriver(t, sz[0], sz[1], fixture)
			runSQL(d, "SELECT * FROM t;", "4 rows", 10*time.Second)
			checkGlyphs(d, "result grid")
			for i := 0; i < 8; i++ {
				d.Key("right")
				checkGlyphs(d, fmt.Sprintf("result after right %d", i))
			}
		})
	}
}

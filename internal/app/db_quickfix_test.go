package app_test

import (
	"strings"
	"testing"
)

// DBX-4: Databases: Apply SQL Quick Fix replaces the unknown name under
// the cursor with the suggested one; the quote style is kept.
func TestDBQuickFixReplacesUnknownName(t *testing.T) {
	d, _ := dbDriver(t, "-- @conn local\n")
	setup := `local text,lnum,col,ecol,fix,crow,ccol=...
local b=vim.api.nvim_get_current_buf()
vim.api.nvim_buf_set_lines(b,0,-1,false,vim.split(text,'\n'))
local ns=vim.api.nvim_create_namespace('termocode_sql_inspect')
vim.diagnostic.set(ns,b,{{lnum=lnum,col=col,end_lnum=lnum,end_col=ecol,severity=2,message='Unknown',source='termocode',user_data={fix=fix}}})
vim.api.nvim_win_set_cursor(0,{crow,ccol})
return ''`
	lines := func() string {
		return dbLua(t, d, `return table.concat(vim.api.nvim_buf_get_lines(0,0,-1,false),'\n')`)
	}
	dbLua(t, d, setup, "-- @conn local\nSELECT u.emial FROM users u", 1, 9, 14, "email", 2, 10)
	d.Palette("db-quick-fix")
	if got := lines(); !strings.Contains(got, "SELECT u.email FROM users u") {
		t.Fatalf("not fixed:\n%s", got)
	}
	// Quoted names stay quoted.
	dbLua(t, d, setup, "-- @conn local\nSELECT * FROM \"usres\"", 1, 14, 21, "users", 2, 16)
	d.Palette("db-quick-fix")
	if got := lines(); !strings.Contains(got, `FROM "users"`) {
		t.Fatalf("quoted not kept:\n%s", got)
	}
	// The cursor elsewhere on the line still finds the line's fix.
	dbLua(t, d, setup, "-- @conn local\nSELECT * FROM usres", 1, 14, 19, "users", 2, 0)
	d.Palette("db-quick-fix")
	if got := lines(); !strings.Contains(got, "FROM users") {
		t.Fatalf("line fix not found:\n%s", got)
	}
	// No fix: the text stays and a toast says so.
	dbLua(t, d, setup, "-- @conn local\nSELECT 1", 1, 0, 6, "", 2, 0)
	frame := d.Palette("db-quick-fix")
	if got := lines(); !strings.Contains(got, "SELECT 1") || !strings.Contains(frame, "No quick fix") {
		t.Fatalf("no-fix case: %s\n%s", got, frame)
	}
}

package app_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// SQL completion inside the SQL strings of a code file (DBX-41): with the
// cursor in a SQL string the omnifunc offers what a .sql file offers
// (here the columns after "u."); outside the strings the buffer's own
// omnifunc (the language server's) still answers.
func TestDBEmbeddedSQLCompletion(t *testing.T) {
	src := "package x\n\nvar q = \"SELECT u. FROM users u\"\nvar n = 1\n"
	d := testkit.New(t, testkit.Options{W: 80, H: 24, Nvim: true, Files: map[string]string{"q.go": src}})
	d.Action("open:q.go")
	d.Key("esc")
	// Go pushes the SQL string ranges of the buffer.
	if !d.WaitFor(func() bool {
		return dbLua(t, d, `local r=_G._termocode_sql and _G._termocode_sql.embed[vim.api.nvim_get_current_buf()];return r and #r or 0`) == "1"
	}, 3*time.Second) {
		t.Fatalf("no SQL string ranges pushed: %s", dbLua(t, d, `return vim.inspect(_G._termocode_sql and _G._termocode_sql.embed)`))
	}
	got := dbLua(t, d, `local schema=...
local b=vim.api.nvim_get_current_buf()
_G._termocode_sql.schema[b]=vim.json.decode(schema)
_G._t_prev=function(fs,base) if fs==1 then return 0 end return {'PREV'} end
vim.bo[b].omnifunc='v:lua._t_prev'
vim.api.nvim_exec_autocmds('InsertEnter',{buffer=b})
local function ask(row,col)
 vim.api.nvim_win_set_cursor(0,{row,col})
 local start=_G._termocode_sql_embed_omni(1,'')
 local line=vim.api.nvim_get_current_line()
 local base=line:sub(start+1,col)
 local out={}
 for _,it in ipairs(_G._termocode_sql_embed_omni(0,base)) do out[#out+1]=type(it)=='table' and it.word or it end
 return out
end
local l3=vim.api.nvim_buf_get_lines(b,2,3,false)[1]
local at=l3:find('u%.')+1
return vim.json.encode({omni=vim.bo[b].omnifunc,inside=ask(3,at),outside=ask(4,3)})`, completeSchema)
	var out struct {
		Omni    string   `json:"omni"`
		Inside  []string `json:"inside"`
		Outside []string `json:"outside"`
	}
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("%q: %v", got, err)
	}
	if out.Omni != "v:lua._termocode_sql_embed_omni" {
		t.Fatalf("omnifunc = %q", out.Omni)
	}
	if !hasAll(out.Inside, "id", "email", "plan") || hasAll(out.Inside, "PREV") {
		t.Fatalf("inside the string: %v", out.Inside)
	}
	if strings.Join(out.Outside, ",") != "PREV" {
		t.Fatalf("outside the string: %v", out.Outside)
	}
}

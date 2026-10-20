package app_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// omniItems asks the SQL omnifunc for the words at the end of text, as
// nvim would: text is the whole buffer, the cursor is at its end. The
// schema is the table Go pushes (here written by hand).
func omniItems(t *testing.T, d *testkit.Driver, schema, text string) []string {
	t.Helper()
	got := dbLua(t, d, `local schema,text=...
local b=vim.api.nvim_get_current_buf()
_G._termocode_sql.schema[b]=vim.json.decode(schema)
vim.api.nvim_buf_set_lines(b,0,-1,false,vim.split(text,'\n'))
local n=vim.api.nvim_buf_line_count(b)
vim.api.nvim_win_set_cursor(0,{n,#vim.api.nvim_buf_get_lines(b,n-1,n,false)[1]})
local start=_G._termocode_sql_omni(1,'')
local line=vim.api.nvim_get_current_line()
local base=line:sub(start+1,vim.api.nvim_win_get_cursor(0)[2])
local out={}
for _,it in ipairs(_G._termocode_sql_omni(0,base)) do out[#out+1]=it.word end
return vim.json.encode({start=start,words=out})`, schema, text)
	var out struct {
		Start int      `json:"start"`
		Words []string `json:"words"`
	}
	if err := jsonUnmarshal(got, &out); err != nil {
		t.Fatalf("omnifunc answer %q: %v", got, err)
	}
	return out.Words
}

func hasAll(words []string, want ...string) bool {
	set := map[string]bool{}
	for _, w := range words {
		set[w] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

const completeSchema = `{"quote":"\"","tables":[{"n":"users","k":"table"},{"n":"orders","k":"table"},{"n":"Order Items","k":"table"},{"n":"active_users","k":"view"},{"n":"refresh_all","k":"func"}],` +
	`"schemas":["public","audit"],"cols":{"users":["id","email","plan"],"orders":["id","user_id","total"]}}`

func TestDBOmnifuncOffersKeywordsObjectsAndColumns(t *testing.T) {
	d, _ := dbDriver(t, "-- @conn local\n")
	if w := omniItems(t, d, completeSchema, "SEL"); !hasAll(w, "SELECT") || hasAll(w, "select") {
		t.Fatalf("typed in capitals: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "sel"); !hasAll(w, "select") {
		t.Fatalf("typed in lower case: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "SELECT * FROM us"); !hasAll(w, "users") || hasAll(w, "orders") {
		t.Fatalf("tables for 'us': %v", w)
	}
	if w := omniItems(t, d, completeSchema, "SELECT * FROM "); !hasAll(w, "users", "orders", `"Order Items"`, "active_users", "public", "audit") {
		t.Fatalf("objects: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "SELECT * FROM r"); !hasAll(w, "refresh_all") {
		t.Fatalf("routines: %v", w)
	}
	// alias. and table. give the columns of that table only.
	if w := omniItems(t, d, completeSchema, "SELECT * FROM users u JOIN orders o ON o.user_id = u."); !hasAll(w, "id", "email", "plan") || hasAll(w, "total") {
		t.Fatalf("alias u columns: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "SELECT * FROM users u JOIN orders AS o ON o.use"); !hasAll(w, "user_id") || hasAll(w, "email") {
		t.Fatalf("alias o columns: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "SELECT users.e"); !hasAll(w, "email") || hasAll(w, "plan") {
		t.Fatalf("table. columns: %v", w)
	}
	if w := omniItems(t, d, completeSchema, "SELECT * FROM public."); !hasAll(w, "users", "orders") {
		t.Fatalf("schema. tables: %v", w)
	}
	// Only the statement at the cursor counts for aliases.
	if w := omniItems(t, d, completeSchema, "SELECT 1 FROM orders u;\nSELECT * FROM users u WHERE u."); !hasAll(w, "email") || hasAll(w, "total") {
		t.Fatalf("alias of an earlier statement leaked: %v", w)
	}
	// Names that need quotes are quoted; ones that do not are left alone.
	if w := omniItems(t, d, completeSchema, "SELECT * FROM Order"); !hasAll(w, `"Order Items"`) {
		t.Fatalf("quoting: %v", w)
	}
}

func TestDBOmnifuncAsksGoForUnknownColumns(t *testing.T) {
	d, _ := dbDriver(t, "-- @conn local\n")
	schema := `{"quote":"\"","tables":[{"n":"users","k":"table"}],"schemas":[],"cols":{}}`
	if w := omniItems(t, d, schema, "SELECT users."); len(w) != 0 {
		t.Fatalf("columns invented: %v", w)
	}
	if got := dbLua(t, d, `local w=_G._termocode_sql.want; return w and w.table or ''`); got != "users" {
		t.Fatalf("no column request recorded: %q", got)
	}
}

func TestDBOmnifuncHandlesAThousandsOfTablesFast(t *testing.T) {
	d, _ := dbDriver(t, "-- @conn local\n")
	var tables []string
	for i := 0; i < 5000; i++ {
		tables = append(tables, fmt.Sprintf(`{"n":"table_%04d","k":"table"}`, i))
	}
	schema := `{"quote":"\"","tables":[` + strings.Join(tables, ",") + `],"schemas":[],"cols":{}}`
	ms := dbLua(t, d, `local schema=...
local b=vim.api.nvim_get_current_buf()
_G._termocode_sql.schema[b]=vim.json.decode(schema)
vim.api.nvim_buf_set_lines(b,0,-1,false,{'SELECT * FROM table_4'})
vim.api.nvim_win_set_cursor(0,{1,21})
local t0=vim.uv.hrtime()
local n=0
for i=1,5 do local s=_G._termocode_sql_omni(1,''); n=#_G._termocode_sql_omni(0,'table_4') end
return string.format('%d %f',n,(vim.uv.hrtime()-t0)/5e6)`, schema)
	var n int
	var elapsed float64
	if _, err := fmt.Sscanf(ms, "%d %f", &n, &elapsed); err != nil {
		t.Fatalf("%q: %v", ms, err)
	}
	if n == 0 || n > 300 {
		t.Fatalf("a popup of %d entries", n)
	}
	if elapsed > 20 {
		t.Fatalf("completion over 5000 tables took %.1f ms", elapsed)
	}
}

// The schema Go pushes comes from the tree; unknown columns are loaded on
// the first "alias." and the popup opens when they arrive.
func TestDBCompletionFromTheTreeWithRealKeys(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("n")
	if !d.WaitFor(func() bool { return strings.Contains(dbBufName(t, d), "consoles") }, 2*time.Second) {
		t.Fatalf("console did not open: %s", dbBufName(t, d))
	}
	// Go pushes the schema to the console buffer a moment after it opens
	// (a 150 ms debounce plus a round trip). The auto-trigger fires only
	// once per typed burst, so typing before the push lands gives a popup
	// built from an empty schema that nothing re-opens. Wait for the push.
	if !d.WaitFor(func() bool {
		return strings.Contains(dbLua(t, d, `local s=_G._termocode_sql.schema[vim.api.nvim_get_current_buf()]
local out={}
for _,t in ipairs(s and s.tables or {}) do out[#out+1]=t.n end
return table.concat(out,' ')`), "users")
	}, 5*time.Second) {
		t.Fatal("the schema never reached the console buffer")
	}
	d.Key("G", "o")
	d.Type("SELECT * FROM us")
	// The auto-trigger opens the popup by itself.
	if !d.WaitFor(func() bool { return strings.Contains(popupWords(t, d), "users") }, 3*time.Second) {
		t.Fatalf("no popup with users after typing; popup=%q\n%s", popupWords(t, d), dbLua(t, d, `return vim.inspect(_G._termocode_sql.schema[vim.api.nvim_get_current_buf()] or {})`))
	}
	d.Key("esc", "o")
	d.Type("SELECT * FROM users u WHERE u.")
	if !d.WaitFor(func() bool { return strings.Contains(popupWords(t, d), "email") }, 3*time.Second) {
		t.Fatalf("columns did not arrive; popup=%q", popupWords(t, d))
	}
	if w := popupWords(t, d); !strings.Contains(w, "id") || !strings.Contains(w, "plan") {
		t.Fatalf("column popup = %q", w)
	}
	for _, q := range s.ran() {
		if strings.HasPrefix(q, "SELECT * FROM") {
			t.Fatal("completion read table rows")
		}
	}
}

func popupWords(t *testing.T, d *testkit.Driver) string {
	return dbLua(t, d, `if vim.fn.pumvisible()==0 then return '' end
local out={}
for _,it in ipairs(vim.fn.complete_info({'items'}).items) do out[#out+1]=it.word end
return table.concat(out,' ')`)
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

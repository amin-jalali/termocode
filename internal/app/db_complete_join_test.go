package app_test

import "testing"

const joinSchema = `{"quote":"\"","tables":[{"n":"users","k":"table"},{"n":"orders","k":"table"},{"n":"items","k":"table"}],"schemas":[],` +
	`"cols":{"users":["id","email","created"],"orders":["id","user_id","total","placed"],"items":["id","order_id"]},` +
	`"types":{"users":{"id":"num","email":"text","created":"time"},"orders":{"id":"num","user_id":"num","total":"num","placed":"time"},"items":{"id":"num","order_id":"num"}},` +
	`"fks":[{"t":"orders","c":"user_id","rt":"users","rc":"id"},{"t":"items","c":"order_id","rt":"orders","rc":"id"}]}`

// DBX-3: after "JOIN t a ON" the foreign keys give whole conditions.
func TestDBOmnifuncSuggestsJoinOnFromForeignKeys(t *testing.T) {
	d, _ := dbDriver(t, "-- @conn local\n")
	if w := omniItems(t, d, joinSchema, "SELECT * FROM users u JOIN orders o ON "); !hasAll(w, "o.user_id = u.id") {
		t.Fatalf("FK condition (child joined): %v", w)
	}
	if w := omniItems(t, d, joinSchema, "SELECT * FROM orders o JOIN users AS u ON "); !hasAll(w, "u.id = o.user_id") {
		t.Fatalf("FK condition (parent joined): %v", w)
	}
	if w := omniItems(t, d, joinSchema, "SELECT * FROM orders o JOIN items i ON i"); !hasAll(w, "i.order_id = o.id") || hasAll(w, "o.user_id = u.id") {
		t.Fatalf("FK condition with prefix: %v", w)
	}
	if w := omniItems(t, d, joinSchema, "SELECT * FROM users JOIN orders ON "); !hasAll(w, "orders.user_id = users.id") {
		t.Fatalf("FK condition without aliases: %v", w)
	}
}

// DBX-3: columns carry their type; after "a.col = b." the columns of the
// same type come first.
func TestDBOmnifuncTypeAwareColumns(t *testing.T) {
	d, _ := dbDriver(t, "-- @conn local\n")
	w := omniItems(t, d, joinSchema, "SELECT * FROM users u JOIN orders o ON u.created = o.")
	if len(w) == 0 || w[0] != "placed" || !hasAll(w, "id", "user_id", "total") {
		t.Fatalf("time column not first: %v", w)
	}
	w = omniItems(t, d, joinSchema, "SELECT * FROM users u JOIN orders o ON o.user_id = u.")
	if len(w) == 0 || w[0] != "id" || w[len(w)-1] != "created" && w[len(w)-1] != "email" {
		t.Fatalf("num column not first: %v", w)
	}
	menu := dbLua(t, d, `local b=vim.api.nvim_get_current_buf()
local l='SELECT * FROM users u WHERE u.'
vim.api.nvim_buf_set_lines(b,0,-1,false,{l})
vim.api.nvim_win_set_cursor(0,{1,#l})
for _,it in ipairs(_G._termocode_sql_omni(0,'')) do if it.word=='created' then return it.menu end end
return ''`)
	if menu != "column time" {
		t.Fatalf("menu: %q", menu)
	}
}

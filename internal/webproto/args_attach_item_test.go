package webproto

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// Spec (Add to AI Chat): an item of the page (a database connection,
// table, column, rows or a cell, a host, a compose project or a
// container) goes with chat.send as an "item" attach. Its spec is one
// JSON object: the item's exact address (the names termocode's MCP tools
// take) and a small summary (a table's DDL, at most 50 rows). Anything
// that does not fit its item kind is refused, so the laptop never reads
// a half-made address.
func TestChatSendAttachItemRules(t *testing.T) {
	item := func(spec string) string {
		b, _ := json.Marshal(spec)
		return `{"kind":"item","label":"x","spec":` + string(b) + `}`
	}
	send := func(attach string) string { return `{"text":"look","attach":[` + attach + `]}` }
	good := []string{
		`{"item":"db_conn","conn":"baaytona-db-1"}`,
		`{"item":"db_table","conn":"local","schema":"public","table":"users","ddl":"CREATE TABLE users (id int, email text);"}`,
		`{"item":"db_column","conn":"local","table":"users","column":"email"}`,
		`{"item":"db_rows","conn":"local","table":"users","key":["id"],"columns":["id","email"],"rows":[["1","a@x"],["2",null]]}`,
		`{"item":"db_rows","conn":"local","table":"users","columns":["id","password"],"rows":[["1","•••"]],"masked":["password"]}`,
		`{"item":"db_cell","conn":"local","table":"users","column":"email","key":["id"],"columns":["id","email"],"rows":[["1","a@x"]]}`,
		`{"item":"host","host":"baaytona"}`,
		`{"item":"compose_project","host":"baaytona","project":"shop"}`,
		`{"item":"container","host":"default","project":"shop","container":"api-1"}`,
	}
	for _, s := range good {
		if _, err := DecodeArgs(OpChatSend, json.RawMessage(send(item(s)))); err != nil {
			t.Errorf("%s refused: %v", s, err)
		}
	}
	rows51 := make([]string, 50+1)
	for i := range rows51 {
		rows51[i] = `["` + strconv.Itoa(i) + `"]`
	}
	bad := []string{
		``,
		`[]`,
		`{"item":"db_conn"}`,
		`{"item":"secret","conn":"x"}`,
		`{"item":"db_conn","conn":"x","table":"t"}`,
		`{"item":"db_conn","conn":"x","value":"hunter2"}`,
		`{"item":"db_table","conn":"x"}`,
		`{"item":"db_table","conn":"x","table":"t","rows":[["1"]]}`,
		`{"item":"db_column","conn":"x","table":"t"}`,
		`{"item":"db_rows","conn":"x","table":"t"}`,
		`{"item":"db_rows","conn":"x","table":"t","columns":["a","b"],"rows":[["1"]]}`,
		`{"item":"db_rows","conn":"x","table":"t","columns":["a"],"rows":[` + strings.Join(rows51, ",") + `]}`,
		`{"item":"db_rows","conn":"x","table":"t","columns":["a"],"rows":[["1"]],"masked":["b"]}`,
		`{"item":"db_rows","conn":"x","table":"t","columns":["a"],"rows":[["1"]],"key":["b"]}`,
		`{"item":"db_cell","conn":"x","table":"t","column":"b","columns":["a"],"rows":[["1"]]}`,
		`{"item":"db_cell","conn":"x","table":"t","column":"a","columns":["a"],"rows":[["1"],["2"]]}`,
		`{"item":"host"}`,
		`{"item":"host","host":"a\u0007"}`,
		`{"item":"host","host":"h","conn":"x"}`,
		`{"item":"compose_project","host":"h"}`,
		`{"item":"container","host":"h"}`,
		`{"item":"db_table","conn":"x","table":"t","ddl":"` + strings.Repeat("d", 16<<10+1) + `"}`,
		`{"item":"db_rows","conn":"x","table":"t","columns":["a"],"rows":[["` + strings.Repeat("v", 32<<10) + `"]]}`,
	}
	for _, s := range bad {
		if _, err := DecodeArgs(OpChatSend, json.RawMessage(send(item(s)))); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
	// An item names itself in its spec only.
	for _, a := range []string{
		`{"kind":"item"}`,
		`{"kind":"item","id":"x","spec":"{\"item\":\"host\",\"host\":\"h\"}"}`,
		`{"kind":"item","path":"a.go","spec":"{\"item\":\"host\",\"host\":\"h\"}"}`,
	} {
		if _, err := DecodeArgs(OpChatSend, json.RawMessage(send(a))); err == nil {
			t.Errorf("%s accepted", a)
		}
	}
}

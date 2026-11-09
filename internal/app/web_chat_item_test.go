package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// webTestItem is a chat.send with one item attach of spec.
func webTestItem(label, spec string) string {
	b, _ := json.Marshal(spec)
	return `{"text":"What is wrong here?","attach":[{"kind":"item","label":` + webTestJSONStr(label) + `,"spec":` + string(b) + `}]}`
}

func webTestJSONStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Spec (Add to AI Chat): an item the page adds goes to the model as
// hidden context: what it is, its exact address as the arguments of
// termocode's MCP tools (so the AI acts on it without asking), and the
// summary the page sent (a table's DDL, the rows as JSON with ••• where
// the user masked a value). The message shows one chip with the label.
func TestChatSendAttachItem(t *testing.T) {
	for _, tc := range []struct {
		name, label, spec string
		want              []string
	}{
		{"table", "baaytona-db-1 › users", `{"item":"db_table","conn":"baaytona-db-1","schema":"public","table":"users","ddl":"CREATE TABLE users (id int, email text);"}`,
			[]string{"table users", `db_schema {"conn":"baaytona-db-1","schema":"public","table":"users"}`, `db_query {"conn":"baaytona-db-1","sql":`, "CREATE TABLE users (id int, email text);"}},
		{"column", "local › users › email", `{"item":"db_column","conn":"local","table":"users","column":"email"}`,
			[]string{"column email of table users", `db_schema {"conn":"local","table":"users"}`}},
		{"rows", "local › users › 2 rows", `{"item":"db_rows","conn":"local","table":"users","key":["id"],"columns":["id","email","password"],"rows":[["1","ada@x","•••"],["2",null,"•••"]],"masked":["password"]}`,
			[]string{"2 rows of table users", `db_query {"conn":"local","sql":`, "key: id", `{"email":"ada@x","id":"1","password":"•••"}`, `"email":null`, "masked: password"}},
		{"cell", "local › users › email", `{"item":"db_cell","conn":"local","table":"users","column":"email","key":["id"],"columns":["id","email"],"rows":[["1","ada@x"]]}`,
			[]string{"cell email of a row of table users", `{"email":"ada@x","id":"1"}`}},
		{"conn", "local", `{"item":"db_conn","conn":"local"}`,
			[]string{"database connection local", `db_schema {"conn":"local"}`}},
		{"host", "baaytona", `{"item":"host","host":"baaytona"}`,
			[]string{"host baaytona", "contexts_list"}},
		{"project", "baaytona › shop", `{"item":"compose_project","host":"baaytona","project":"shop"}`,
			[]string{"compose project shop on host baaytona", "containers_list"}},
		{"container", "default › shop › api-1", `{"item":"container","host":"default","project":"shop","container":"api-1"}`,
			[]string{"container api-1 on host default", `containers_inspect {"target":"api-1"}`, `containers_logs {"target":"api-1"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, c, _ := webTestAttachModel(t)
			f := webTestSendAttached(t, m, c, webTestItem(tc.label, tc.spec))
			if f["t"] != "res" {
				t.Fatalf("send: %v", f)
			}
			um := m.ai.conv.Messages[0]
			for _, w := range tc.want {
				if !strings.Contains(um.Context, w) {
					t.Errorf("context has no %q:\n%s", w, um.Context)
				}
			}
			if len(um.Attached) != 1 || um.Attached[0].Kind != "item" || um.Attached[0].Label != tc.label {
				t.Fatalf("chips %+v", um.Attached)
			}
		})
	}
}

// Spec (Add to AI Chat): a secret-shaped value the user sent anyway is
// still redacted on the laptop, as in every attach.
func TestChatSendAttachItemRedacts(t *testing.T) {
	m, c, _ := webTestAttachModel(t)
	key := "sk-" + strings.Repeat("a", 30)
	f := webTestSendAttached(t, m, c, webTestItem("local › keys › 1 row", `{"item":"db_rows","conn":"local","table":"keys","columns":["id","api_key"],"rows":[["1","`+key+`"]]}`))
	if f["t"] != "res" {
		t.Fatalf("send: %v", f)
	}
	if ctx := m.ai.conv.Messages[0].Context; strings.Contains(ctx, key) {
		t.Fatalf("the key reached the model:\n%s", ctx)
	}
}

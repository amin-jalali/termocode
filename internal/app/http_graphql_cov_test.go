package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// covGQLIntrospection: Query { user(id): User, users: [User!]! },
// Mutation { rename: User }, User { id, name, friends }.
const covGQLIntrospection = `{"data":{"__schema":{
 "queryType":{"name":"Query"},"mutationType":{"name":"Mutation"},"subscriptionType":null,
 "types":[
  {"kind":"OBJECT","name":"Query","description":"root","fields":[
    {"name":"user","description":"one user","args":[{"name":"id","type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"SCALAR","name":"ID","ofType":null}}}],
     "type":{"kind":"OBJECT","name":"User","ofType":null}},
    {"name":"users","args":[],"type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"LIST","name":null,"ofType":{"kind":"NON_NULL","name":null,"ofType":{"kind":"OBJECT","name":"User","ofType":null}}}}}]},
  {"kind":"OBJECT","name":"Mutation","fields":[
    {"name":"rename","args":[],"type":{"kind":"OBJECT","name":"User","ofType":null}}]},
  {"kind":"OBJECT","name":"User","fields":[
    {"name":"id","args":[],"type":{"kind":"NON_NULL","name":null,"ofType":{"kind":"SCALAR","name":"ID","ofType":null}}},
    {"name":"name","args":[],"type":{"kind":"SCALAR","name":"String","ofType":null}},
    {"name":"friends","args":[],"type":{"kind":"LIST","name":null,"ofType":{"kind":"OBJECT","name":"User","ofType":null}}}]},
  {"kind":"SCALAR","name":"ID"},{"kind":"SCALAR","name":"String"},{"kind":"OBJECT","name":"__Type","fields":[]}
 ]}}}`

// covGQLServer answers the introspection on /graphql, 500 on /broken
// and a GraphQL error on /off. fetches counts the introspections.
func covGQLServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "__schema") || r.Header.Get("X-Request-Type") != "" {
			http.Error(w, "not an introspection", http.StatusBadRequest)
			return
		}
		fetches.Add(1)
		switch r.URL.Path {
		case "/graphql":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(covGQLIntrospection))
		case "/broken":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			_, _ = w.Write([]byte(`{"errors":[{"message":"introspection is off"}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &fetches
}

// GraphQL at the cursor: the schema is fetched once per URL, then a
// field completes at the cursor, the query is validated and a type
// opens as SDL.
func TestCovHTTPGQLCommands(t *testing.T) {
	m, root, _ := covHTTP(t)
	srv, fetches := covGQLServer(t)
	file := filepath.Join(root, "gql.http")
	src := "@host = " + srv.URL + "\n" + // 1
		"\n###\n# @name plain\nGET {{host}}/plain\n" + // 3-5
		"\n###\n# @name typing\nGRAPHQL {{host}}/graphql\n\nquery { us\n" + // 7-11
		"\n###\n# @name good\nPOST {{host}}/graphql\nX-Request-Type: GraphQL\n\n{ users { id name } }\n" + // 13-18
		"\n###\n# @name bad\nGRAPHQL {{host}}/graphql\n\n{ users { id nam }\n" // 20-24
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	loop := newCovHTTPLoop(t, m)
	// Not a GraphQL request.
	covHTTPEdit(t, m, file, 5)
	covHTTPDispatch(t, m, "http-gql-validate")
	if !strings.Contains(covHTTPToast(m), "not GraphQL") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// Complete with no schema: fetched first, then the picker opens.
	covHTTPEdit(t, m, file, 11)
	_ = m.nvim.ExecLua(`vim.api.nvim_win_set_cursor(0, { 11, 9 })`)
	c := covHTTPDispatch(t, m, "http-gql-complete")
	loop.run(c, func() bool { return m.pickerOpen })
	if m.pickerKind != pickerKindHTTPGQLComplete || fetches.Load() != 1 {
		t.Fatalf("picker kind %v fetches %d", m.pickerKind, fetches.Load())
	}
	got := covSvcPickerText(m)
	if !strings.Contains(got, "user") || !strings.Contains(got, "users") || !strings.Contains(got, "one user") {
		t.Errorf("picker:\n%s", got)
	}
	m.pickerOpen = false
	m.handleHTTPPickerSelect("users")
	var line string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_get_current_line()`, &line)
	if line != "query { users" {
		t.Errorf("line %q", line)
	}
	if m.httpGQLInsert("us") != nil {
		t.Error("nothing to insert did something")
	}
	// In the headers nothing fits.
	_ = m.nvim.ExecLua(`vim.api.nvim_win_set_cursor(0, { 10, 0 })`)
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-gql-complete")
	if m.pickerOpen || !strings.Contains(covHTTPToast(m), "No GraphQL field fits here") || fetches.Load() != 1 {
		t.Errorf("headers: %q fetches %d", covHTTPToast(m), fetches.Load())
	}
	// Validate: the cached schema checks the query.
	covHTTPEdit(t, m, file, 18)
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-gql-validate")
	if !strings.Contains(covHTTPToast(m), "GraphQL query OK") {
		t.Errorf("good: %q", covHTTPToast(m))
	}
	covHTTPEdit(t, m, file, 24)
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-gql-validate")
	if got := covHTTPToast(m); !strings.Contains(got, "GraphQL problems") || !strings.Contains(got, "nam") {
		t.Errorf("bad: %q", got)
	}
	// Docs: the roots first, __ types left out; one type opens as SDL.
	covHTTPDispatch(t, m, "http-gql-docs")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPGQLDocs {
		t.Fatal("no docs picker")
	}
	got = covSvcPickerText(m)
	if !strings.Contains(got, "root") || !strings.Contains(got, "object") || strings.Contains(got, "__Type") || strings.Index(got, "Mutation") > strings.Index(got, "User") {
		t.Errorf("docs picker:\n%s", got)
	}
	m.pickerOpen = false
	m.handleHTTPPickerSelect("User")
	var name, text string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name)
	_ = m.nvim.ExecLuaResult(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &text)
	if !strings.HasSuffix(name, "GraphQL: User") || !strings.Contains(text, "type User {") {
		t.Errorf("docs page %q:\n%s", name, text)
	}
	m.httpS().gqlDocs = nil
	m.pickerKind = pickerKindHTTPGQLDocs
	if c, ok := m.httpGQLPickerSelect("User"); !ok || c != nil {
		t.Error("docs without a schema")
	}
	m.pickerKind = pickerKindHTTPEnv
	if _, ok := m.httpGQLPickerSelect("x"); ok {
		t.Error("another picker taken")
	}
	// Fetch Schema always fetches and says how many types.
	covHTTPEdit(t, m, file, 24)
	covHTTPReset(m)
	c = covHTTPDispatch(t, m, "http-gql-schema")
	loop.run(c, func() bool {
		return fetches.Load() == 2 && strings.Contains(covHTTPToast(m), "GraphQL schema: 6 types")
	})
}

// A schema that cannot be fetched says why: an HTTP error, a GraphQL
// error, no server.
func TestCovHTTPGQLFetchErrors(t *testing.T) {
	m, root, _ := covHTTP(t)
	srv, _ := covGQLServer(t)
	file := filepath.Join(root, "gql.http")
	src := "###\nGRAPHQL " + srv.URL + "/broken\n\n{ a }\n\n###\nGRAPHQL " + srv.URL + "/off\n\n{ a }\n\n###\nGRAPHQL http://127.0.0.1:1/x\n\n{ a }\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	loop := newCovHTTPLoop(t, m)
	for line, want := range map[int]string{2: "the server answered 500", 7: "introspection is off", 12: "Could not fetch the GraphQL schema"} {
		covHTTPEdit(t, m, file, line)
		covHTTPReset(m)
		c := covHTTPDispatch(t, m, "http-gql-schema")
		loop.run(c, func() bool { return strings.Contains(covHTTPToast(m), "Could not fetch") })
		if got := covHTTPToast(m); !strings.Contains(got, want) {
			t.Errorf("line %d: %q", line, got)
		}
	}
	// No editor: the text before the cursor is empty.
	m.nvim = nil
	if got := m.httpGQLTextBefore(httpclient.Request{}); got != "" {
		t.Errorf("text %q", got)
	}
}

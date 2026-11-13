package httpclient

import (
	"fmt"
	"strings"
	"testing"
)

func importOK(t *testing.T, doc string) Imported {
	t.Helper()
	got, err := ImportCollection([]byte(doc))
	if err != nil {
		t.Fatalf("import: %v\n%s", err, doc)
	}
	return got
}

func bodyOf(r *Request) string {
	if r.Body == nil {
		return ""
	}
	return *r.Body
}

func TestImportCollectionBodiesAuthAndFolders(t *testing.T) {
	got := importOK(t, `{
	  "auth": {"type":"bearer","bearer":[{"key":"token","value":"{{tok}}"}]},
	  "variable": [{"key":"host","value":"example.org"},{"key":"off","value":"x","disabled":true}],
	  "item": [
	    {"name":"bare","request":"https://example.org/bare"},
	    {"name":"gql obj","request":{"method":"POST","url":"https://x/g","body":{"mode":"graphql","graphql":{"query":"{a}","variables":{"id":1}}}}},
	    {"name":"gql text","request":{"method":"POST","url":"https://x/g","body":{"mode":"graphql","graphql":{"query":"{a}","variables":"{\"id\":2}"}}}},
	    {"name":"gql bad text","request":{"method":"POST","url":"https://x/g","body":{"mode":"graphql","graphql":{"query":"{a}","variables":"nonsense"}}}},
	    {"name":"form","request":{"method":"POST","url":"https://x/f","body":{"mode":"urlencoded","urlencoded":[{"key":"a b","value":"c&d"},{"key":"off","value":"1","disabled":true}]}}},
	    {"name":"empty raw","request":{"method":"POST","url":"https://x/e","body":{"mode":"raw","raw":""}}},
	    {"name":"basic","auth":{"type":"basic","basic":[{"key":"username","value":"u"},{"key":"password","value":"p"}]},"request":{"method":"GET","url":"https://x/b"}},
	    {"name":"key header","auth":{"type":"apikey","apikey":[{"key":"key","value":"X-Key"},{"key":"value","value":"{{k}}"}]},"request":{"method":"GET","url":"https://x/k"}},
	    {"name":"key query","auth":{"type":"apikey","apikey":[{"key":"in","value":"query"},{"key":"key","value":"k"}]},"request":{"method":"GET","url":"https://x/q"}},
	    {"name":"folder","auth":{"type":"basic","basic":[{"key":"username","value":"f"}]},"item":[
	      {"name":"inner","request":{"method":"GET","url":"https://x/in"}},
	      {"name":"no url","request":{"method":"GET","url":""}}
	    ]}
	  ]}`)
	byName := map[string]*Request{}
	for i := range got.Requests {
		byName[got.Requests[i].Name] = &got.Requests[i]
	}
	if r := byName["bare"]; r.Method != "GET" || r.URL != "https://example.org/bare" {
		t.Errorf("bare = %+v", r)
	}
	if b := bodyOf(byName["gql obj"]); !strings.Contains(b, `"variables":{"id":1}`) {
		t.Errorf("graphql object variables: %s", b)
	}
	if r := byName["form"]; bodyOf(r) != "a+b=c%26d" || r.Header("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("form = %+v", r)
	}
	if byName["empty raw"].Body != nil {
		t.Error("an empty body is no body")
	}
	if v := byName["basic"].Header("Authorization"); !strings.HasPrefix(v, "Basic ") {
		t.Errorf("basic auth header = %q", v)
	}
	if byName["key header"].Header("X-Key") == "" {
		t.Errorf("api key header missing: %+v", byName["key header"])
	}
	if byName["key query"].HasHeader("Authorization") || len(byName["key query"].Headers) != 0 {
		t.Errorf("an api key in the query is not a header: %+v", byName["key query"])
	}
	if r := byName["folder / inner"]; !strings.HasPrefix(r.Header("Authorization"), "Basic ") {
		t.Errorf("the folder's auth reaches its items: %+v", r)
	}
	if len(got.Vars) != 1 || got.Vars[0].Name != "host" {
		t.Errorf("vars = %+v", got.Vars)
	}
}

func TestImportCollectionLimitsAndBadShapes(t *testing.T) {
	if _, err := ImportCollection([]byte(`{"item":"nope"}`)); err == nil || !strings.Contains(err.Error(), "item is not a list") {
		t.Errorf("item not a list = %v", err)
	}
	if _, err := ImportCollection([]byte(`{"item":[{"name":"x","item":[]}]}`)); err == nil || !strings.Contains(err.Error(), "no requests") {
		t.Errorf("no requests = %v", err)
	}
	// Folders nested too deep.
	doc := `{"name":"leaf","request":{"method":"GET","url":"https://x"}}`
	for i := 0; i < maxImportDepth+2; i++ {
		doc = fmt.Sprintf(`{"name":"f%d","item":[%s]}`, i, doc)
	}
	if _, err := ImportCollection([]byte(`{"item":[` + doc + `]}`)); err == nil || !strings.Contains(err.Error(), "too deep") {
		t.Errorf("deep folders = %v", err)
	}
	// Too many requests.
	var items []string
	for i := 0; i < maxImportRequests+1; i++ {
		items = append(items, `{"name":"r","request":"https://x"}`)
	}
	if _, err := ImportCollection([]byte(`{"item":[` + strings.Join(items, ",") + `]}`)); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Errorf("many requests = %v", err)
	}
	if _, err := ImportCollection([]byte(`{"log":{"entries":null}}`)); err == nil {
		t.Error("a HAR without entries: want error")
	}
	if _, err := ImportCollection([]byte(`{"resources":"nope"}`)); err == nil || !strings.Contains(err.Error(), "resources is not a list") {
		t.Errorf("Insomnia resources = %v", err)
	}
}

func TestImportHARParamsBodyAndSkippedHeaders(t *testing.T) {
	got := importOK(t, `{"log":{"entries":[
	  {"comment":"form","request":{"method":"post","url":"https://x/f","headers":[{"name":":authority","value":"x"},{"name":"Host","value":"x"},{"name":" ","value":"v"},{"name":"X-A","value":"1"}],
	   "postData":{"mimeType":"application/x-www-form-urlencoded","params":[{"name":"a b","value":"c"},{"name":"{{k}}","value":"{{v}}"}]}}},
	  {"request":{"method":"weird verb","url":"https://x/g"}},
	  {"request":{"method":"GET","url":""}}
	]}}`)
	if len(got.Requests) != 2 {
		t.Fatalf("requests = %+v", got.Requests)
	}
	f := &got.Requests[0]
	if bodyOf(f) != "a+b=c&{{k}}={{v}}" || f.Header("X-A") != "1" || f.HasHeader("Host") || f.Method != "POST" {
		t.Errorf("form entry = %+v body %q", f, bodyOf(f))
	}
	if got.Requests[1].Method != "GET" {
		t.Errorf("an odd method reads as GET: %q", got.Requests[1].Method)
	}
}

func TestImportInsomniaParamsAuthAndEnvironments(t *testing.T) {
	got := importOK(t, `{"_type":"export","resources":[
	  {"_id":"env1","_type":"environment","parentId":"wrk","data":{"host":"h","n":3,"on":true,"obj":{"a":1},"nul":null}},
	  {"_id":"env2","_type":"environment","parentId":"env1","data":{"skip":"me"}},
	  {"_id":"env3","_type":"environment","parentId":"wrk","data":"not an object"},
	  {"_id":"g1","_type":"request_group","parentId":"wrk","name":"Group"},
	  {"_id":"r1","_type":"request","parentId":"g1","name":"form","method":"POST","url":"https://x/p?a=1",
	   "parameters":[{"name":"q","value":"{{ _.term }}"},{"name":"off","value":"1","disabled":true}],
	   "body":{"mimeType":"application/x-www-form-urlencoded","params":[{"name":"k","value":"v w"},{"name":"d","value":"x","disabled":true}]},
	   "authentication":{"type":"bearer","token":"{{ _.tok }}","prefix":"Token"}},
	  {"_id":"r2","_type":"request","parentId":"wrk","name":"basic","method":"GET","url":"https://x/b",
	   "authentication":{"type":"basic","username":"u","password":"p"}},
	  {"_id":"r3","_type":"request","parentId":"wrk","name":"bearer","method":"GET","url":"https://x/c",
	   "authentication":{"type":"bearer","token":"t"}}
	]}`)
	byName := map[string]*Request{}
	for i := range got.Requests {
		byName[got.Requests[i].Name] = &got.Requests[i]
	}
	f := byName["Group / form"]
	if !strings.Contains(f.URL, "?a=1&q={{term}}") || strings.Contains(f.URL, "off") {
		t.Errorf("query merge = %q", f.URL)
	}
	if bodyOf(f) != "k=v+w" {
		t.Errorf("form body = %q", bodyOf(f))
	}
	if v := f.Header("Authorization"); v != "Token {{tok}}" {
		t.Errorf("prefixed bearer = %q", v)
	}
	if !strings.HasPrefix(byName["basic"].Header("Authorization"), "Basic ") {
		t.Error("basic auth header missing")
	}
	if v := byName["bearer"].Header("Authorization"); v != "Bearer {{authorization}}" {
		t.Errorf("a literal bearer token is hidden: %q", v)
	}
	names := map[string]string{}
	for _, v := range got.Vars {
		names[v.Name] = v.Value
	}
	if names["host"] != "h" || names["n"] != "3" || names["on"] != "true" || names["skip"] != "" || len(names) != 3 {
		t.Errorf("vars = %v", names)
	}
}

func TestImportHelpers(t *testing.T) {
	if varName("  ") != "secret" || varName("X-API-Key") != "x_api_key" {
		t.Error("varName")
	}
	if qEscape("{{a}} b") != "{{a}} b" || qEscape("a b") != "a+b" {
		t.Error("qEscape")
	}
	for in, want := range map[string]struct {
		s  string
		ok bool
	}{
		"":      {"", false},
		"nope":  {"", false},
		`"x"`:   {"x", true},
		"1.5":   {"1.5", true},
		"false": {"false", true},
		"[1]":   {"", false},
		"null":  {"", false},
	} {
		s, ok := scalarOK([]byte(in))
		if s != want.s || ok != want.ok {
			t.Errorf("scalarOK(%q) = %q, %v", in, s, ok)
		}
	}
	im := &importer{seen: map[string]bool{}}
	im.addVar("bad name!", "x")
	if len(im.out.Vars) != 0 {
		t.Error("a name that is not plain is not a variable")
	}
}

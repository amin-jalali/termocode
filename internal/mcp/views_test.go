package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Default views for MCP tools and resources (MOB-72, mobile plan 3.11.6).

func boolPtr(b bool) *bool { return &b }

func roTool(name string, in, out map[string]any) Tool {
	return Tool{Name: name, InputSchema: in, OutputSchema: out, Annotations: &ToolAnnotations{ReadOnlyHint: boolPtr(true)}}
}

func props(p map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": p}
	if len(required) > 0 {
		r := make([]any, len(required))
		for i, x := range required {
			r[i] = x
		}
		s["required"] = r
	}
	return s
}

// Spec: scalar input properties become params (kinds, enum, required
// kept, sorted by name); an object or array input makes the tool "Ask
// the AI" only.
func TestViewParamsFromSchema(t *testing.T) {
	for _, w := range []struct {
		name    string
		in      map[string]any
		want    []ViewParam
		askOnly bool
	}{
		{"none", nil, nil, false},
		{"scalars", props(map[string]any{
			"state": map[string]any{"type": "string", "enum": []any{"open", "closed"}},
			"limit": map[string]any{"type": "integer", "description": "How many"},
			"repo":  map[string]any{"type": "string"},
			"draft": map[string]any{"type": "boolean"},
			"min":   map[string]any{"type": "number"},
		}, "repo"), []ViewParam{
			{Name: "draft", Kind: KindBool},
			{Name: "limit", Kind: KindInteger, Description: "How many"},
			{Name: "min", Kind: KindNumber},
			{Name: "repo", Kind: KindString, Required: true},
			{Name: "state", Kind: KindEnum, Enum: []string{"open", "closed"}},
		}, false},
		{"nullable string", props(map[string]any{"q": map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}}}}),
			[]ViewParam{{Name: "q", Kind: KindString}}, false},
		{"object input", props(map[string]any{"filter": map[string]any{"type": "object"}}), nil, true},
		{"array input", props(map[string]any{"ids": map[string]any{"type": "array"}}), nil, true},
	} {
		t.Run(w.name, func(t *testing.T) {
			got, askOnly := ViewParamsFromSchema(w.in)
			if !reflect.DeepEqual(got, w.want) || askOnly != w.askOnly {
				t.Errorf("got %+v askOnly=%v, want %+v askOnly=%v", got, askOnly, w.want, w.askOnly)
			}
		})
	}
}

// Spec: fields come from the output schema: an array's item properties
// give a table, an object's properties a card; date-time is a time, a
// nested object or array is one json field. No schema: not typed.
func TestViewShapeFromSchema(t *testing.T) {
	item := map[string]any{"type": "object", "properties": map[string]any{
		"title":   map[string]any{"type": "string"},
		"number":  map[string]any{"type": "integer"},
		"created": map[string]any{"type": "string", "format": "date-time"},
		"labels":  map[string]any{"type": "array"},
		"open":    map[string]any{"type": "boolean"},
	}}
	wantFields := []ViewField{{"created", KindTime}, {"labels", KindJSON}, {"number", KindInteger}, {"open", KindBool}, {"title", KindString}}
	for _, w := range []struct {
		name  string
		out   map[string]any
		want  ViewShape
		typed bool
	}{
		{"array", map[string]any{"type": "array", "items": item}, ViewShape{Layout: ViewTable, Fields: wantFields}, true},
		{"object", item, ViewShape{Layout: ViewCard, Fields: wantFields}, true},
		{"wrapped array", map[string]any{"type": "object", "properties": map[string]any{"issues": map[string]any{"type": "array", "items": item}}},
			ViewShape{Layout: ViewTable, Fields: wantFields, Path: "issues"}, true},
		{"none", nil, ViewShape{Layout: ViewJSON}, false},
		{"string", map[string]any{"type": "string"}, ViewShape{Layout: ViewText}, true},
		{"odd", map[string]any{"type": 7}, ViewShape{Layout: ViewJSON}, false},
	} {
		t.Run(w.name, func(t *testing.T) {
			got, typed := ViewShapeFromSchema(w.out)
			if !reflect.DeepEqual(got, w.want) || typed != w.typed {
				t.Errorf("got %+v typed=%v, want %+v typed=%v", got, typed, w.want, w.typed)
			}
		})
	}
}

// Spec: with no output schema the fields are learned from a result: the
// keys of the first object in first-seen order, kinds by value type;
// text that parses as JSON counts; plain text is a text view; an error
// result teaches nothing.
func TestViewShapeLearned(t *testing.T) {
	for _, w := range []struct {
		name string
		res  CallResult
		want ViewShape
		ok   bool
	}{
		{"structured array", CallResult{StructuredContent: json.RawMessage(`[{"title":"a","n":1,"ok":true,"at":"2026-11-07T10:00:00Z","meta":{"x":1}},{"title":"b","extra":2}]`)},
			ViewShape{Layout: ViewTable, Fields: []ViewField{{"title", KindString}, {"n", KindNumber}, {"ok", KindBool}, {"at", KindTime}, {"meta", KindJSON}}}, true},
		{"structured object", CallResult{StructuredContent: json.RawMessage(`{"n": 42}`)},
			ViewShape{Layout: ViewCard, Fields: []ViewField{{"n", KindNumber}}}, true},
		{"wrapped", CallResult{StructuredContent: json.RawMessage(`{"items":[{"id":"x"}]}`)},
			ViewShape{Layout: ViewTable, Fields: []ViewField{{"id", KindString}}, Path: "items"}, true},
		{"json text", CallResult{Content: []Content{{Type: "text", Text: `[{"id": 1}]`}}},
			ViewShape{Layout: ViewTable, Fields: []ViewField{{"id", KindNumber}}}, true},
		{"plain text", CallResult{Content: []Content{{Type: "text", Text: "hello"}}}, ViewShape{Layout: ViewText}, true},
		{"error", CallResult{IsError: true, StructuredContent: json.RawMessage(`{"n":1}`)}, ViewShape{}, false},
		{"empty", CallResult{}, ViewShape{}, false},
	} {
		t.Run(w.name, func(t *testing.T) {
			got, ok := LearnViewShape(w.res)
			if !reflect.DeepEqual(got, w.want) || ok != w.ok {
				t.Errorf("got %+v ok=%v, want %+v ok=%v", got, ok, w.want, w.ok)
			}
		})
	}
}

// Spec (run rule, Q2): only a read-only tool, not turned off, inside the
// profile, with mcp_trust_annotations on and an accepted tool list, runs
// directly; every other tool is "Ask the AI". A denied tool has no view.
func TestToolViewRunRule(t *testing.T) {
	s := Server{Name: "gh", Deny: []string{"secret_*"}}
	ro := roTool("list_issues", props(map[string]any{"repo": map[string]any{"type": "string"}}), nil)
	all := ViewRule{TrustAnnotations: true}
	for _, w := range []struct {
		name   string
		srv    Server
		tool   Tool
		rule   ViewRule
		chg    bool
		direct bool
		shown  bool
	}{
		{"read-only", s, ro, all, false, true, true},
		{"not read-only", s, Tool{Name: "create_issue"}, all, false, false, true},
		{"no annotation trust", s, ro, ViewRule{}, false, false, true},
		{"turned off", s, ro, ViewRule{TrustAnnotations: true, Off: func(_, tool string) bool { return tool == "list_issues" }}, false, false, true},
		{"outside the profile", s, ro, ViewRule{TrustAnnotations: true, InProfile: func(string, string) bool { return false }}, false, false, true},
		{"tools changed", s, ro, all, true, false, true},
		{"object input", s, roTool("search", props(map[string]any{"q": map[string]any{"type": "object"}}), nil), all, false, false, true},
		{"denied", s, roTool("secret_dump", nil, nil), all, false, false, false},
	} {
		t.Run(w.name, func(t *testing.T) {
			v, shown := ToolViewOf(w.srv, w.tool, nil, w.rule, w.chg)
			if shown != w.shown {
				t.Fatalf("shown = %v, want %v", shown, w.shown)
			}
			if shown && v.Direct != w.direct {
				t.Errorf("direct = %v, want %v (%s)", v.Direct, w.direct, v.Why)
			}
			if shown && !v.Direct && v.Why == "" {
				t.Error("an Ask the AI view gives no reason")
			}
			if shown && v.ID != "mcp.gh."+w.tool.Name {
				t.Errorf("id %q", v.ID)
			}
		})
	}
}

// Spec: a tool with an output schema is typed from the first call; one
// without is typed once a result was learned.
func TestToolViewTypedFromSchemaOrLearned(t *testing.T) {
	s := Server{Name: "gh"}
	issues := roTool("list_issues", nil, map[string]any{"type": "array", "items": props(map[string]any{"title": map[string]any{"type": "string"}})})
	v, _ := ToolViewOf(s, issues, nil, ViewRule{TrustAnnotations: true}, false)
	if !v.Typed || v.Learned || v.Shape.Layout != ViewTable || len(v.Shape.Fields) != 1 {
		t.Fatalf("schema view %+v", v)
	}
	plain := roTool("list_prs", nil, nil)
	v, _ = ToolViewOf(s, plain, nil, ViewRule{TrustAnnotations: true}, false)
	if v.Typed || v.Shape.Layout != ViewJSON {
		t.Fatalf("untyped view %+v", v)
	}
	learned := ViewShape{Layout: ViewTable, Fields: []ViewField{{"title", KindString}}}
	v, _ = ToolViewOf(s, plain, &learned, ViewRule{TrustAnnotations: true}, false)
	if !v.Typed || !v.Learned || !reflect.DeepEqual(v.Shape, learned) {
		t.Fatalf("learned view %+v", v)
	}
}

// Spec: the args of a direct run are checked on the laptop: required
// params present, no unknown names, scalars of the param's kind, enum
// values from the list, and a secret-looking value is refused (type it
// on the laptop).
func TestCheckViewArgs(t *testing.T) {
	v := ToolView{ID: "mcp.gh.list_issues", Direct: true, Params: []ViewParam{
		{Name: "repo", Kind: KindString, Required: true},
		{Name: "limit", Kind: KindInteger},
		{Name: "state", Kind: KindEnum, Enum: []string{"open", "closed"}},
	}}
	for _, w := range []struct {
		args map[string]any
		ok   bool
		want string
	}{
		{map[string]any{"repo": "a/b"}, true, ""},
		{map[string]any{"repo": "a/b", "limit": float64(5), "state": "open"}, true, ""},
		{map[string]any{}, false, "repo"},
		{map[string]any{"repo": "a/b", "x": "1"}, false, "x"},
		{map[string]any{"repo": "a/b", "limit": "5"}, false, "limit"},
		{map[string]any{"repo": "a/b", "limit": 1.5}, false, "limit"},
		{map[string]any{"repo": "a/b", "state": "all"}, false, "state"},
		{map[string]any{"repo": "Bearer abcdefghijklmnopqrstuvwxyz0123456789"}, false, "laptop"},
		{map[string]any{"repo": "sk-ant-abcdefghijklmnopqrstuvwxyz0123456789"}, false, "laptop"},
	} {
		err := CheckViewArgs(v, w.args)
		if (err == nil) != w.ok || (err != nil && !strings.Contains(err.Error(), w.want)) {
			t.Errorf("%v: %v", w.args, err)
		}
	}
	if err := CheckViewArgs(ToolView{ID: "mcp.gh.create", Why: "not read-only"}, nil); err == nil {
		t.Error("an Ask the AI view ran directly")
	}
}

// Spec: a resource shows as text (markdown when the mime says so), a
// JSON array as a table, a JSON object as detail, binary as one line;
// over 32 KiB is one line too.
func TestResourceViewLayout(t *testing.T) {
	for _, w := range []struct {
		c    ResourceContents
		want string
	}{
		{ResourceContents{URI: "file:///a.txt", MimeType: "text/plain", Text: "hi"}, ViewText},
		{ResourceContents{URI: "file:///a.md", MimeType: "text/markdown", Text: "# hi"}, ViewMarkdown},
		{ResourceContents{URI: "x://list", MimeType: "application/json", Text: `[{"a":1}]`}, ViewTable},
		{ResourceContents{URI: "x://one", MimeType: "application/json", Text: `{"a":1}`}, ViewDetail},
		{ResourceContents{URI: "x://bad", MimeType: "application/json", Text: `{bad`}, ViewText},
		{ResourceContents{URI: "x://bin", MimeType: "image/png", Blob: "AAAA"}, ViewLine},
		{ResourceContents{URI: "x://big", Text: strings.Repeat("a", MaxResourceView+1)}, ViewLine},
	} {
		if got := ResourceViewLayout(w.c); got != w.want {
			t.Errorf("%s: %q, want %q", w.c.URI, got, w.want)
		}
	}
}

// Spec: one resource view per server: the uri picker lists the
// resources and the templates with their variables.
func TestResourceViewOf(t *testing.T) {
	v := ResourceViewOf("gh", []Resource{{URI: "repo://a/readme", Name: "readme", MimeType: "text/markdown"}},
		[]ResourceTemplate{{URITemplate: "repo://{owner}/{repo}", Name: "repo"}})
	if v.ID != "mcp.gh.resource" || len(v.URIs) != 1 || v.URIs[0].Label != "readme" || len(v.Templates) != 1 ||
		!reflect.DeepEqual(v.Templates[0].Vars, []string{"owner", "repo"}) {
		t.Errorf("resource view %+v", v)
	}
}

// Spec: a resource template's variables become the uri picker's fields.
func TestResourceTemplateVars(t *testing.T) {
	got := TemplateVars("repo://{owner}/{repo}/issues{?state,page}")
	if want := []string{"owner", "repo", "state", "page"}; !reflect.DeepEqual(got, want) {
		t.Errorf("vars %q, want %q", got, want)
	}
	if got := TemplateVars("file:///plain"); len(got) != 0 {
		t.Errorf("plain uri vars %q", got)
	}
}

// Spec: the manager lists every enabled server's tool views from what it
// knows (never starting a server) plus one resource view per server that
// offers resources; a call teaches the view its fields, kept in the tool
// cache, so a new manager still knows them.
func TestManagerToolViewsLearnAndKeep(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	if n := len(m.ToolViews(ViewRule{TrustAnnotations: true})); n != 0 {
		t.Fatalf("views before any list: %d", n)
	}
	m.Tools(context.Background(), nil)
	views := m.ToolViews(ViewRule{TrustAnnotations: true})
	byID := map[string]ToolView{}
	for _, v := range views {
		byID[v.ID] = v
	}
	if v := byID["mcp.fake.echo"]; !v.Direct || len(v.Params) != 1 || v.Params[0].Name != "text" || !v.Params[0].Required {
		t.Errorf("echo view %+v", v)
	}
	if v := byID["mcp.fake.write_note"]; v.Direct {
		t.Errorf("write_note runs directly: %+v", v)
	}
	if v := byID["mcp.fake.structured"]; v.Typed {
		t.Fatalf("structured typed before a call: %+v", v)
	}
	res, err := m.Call(context.Background(), nil, "fake", "structured", nil)
	if err != nil {
		t.Fatal(err)
	}
	m.LearnViewResult("fake", "structured", res)
	m.LearnViewResult("fake", "structured", CallResult{StructuredContent: json.RawMessage(`[{"other":1}]`)}) // the first shape stays
	want := ViewShape{Layout: ViewCard, Fields: []ViewField{{"n", KindNumber}}}
	check := func(m *Manager, where string) {
		for _, v := range m.ToolViews(ViewRule{TrustAnnotations: true}) {
			if v.ID == "mcp.fake.structured" {
				if !v.Typed || !v.Learned || !reflect.DeepEqual(v.Shape, want) {
					t.Errorf("%s: structured view %+v", where, v)
				}
				return
			}
		}
		t.Errorf("%s: no structured view", where)
	}
	check(m, "after the call")
	m2 := e.manager(t, 0, nil)
	check(m2, "a new manager")
	if m2.Running() != 0 {
		t.Error("ToolViews started a server")
	}
}

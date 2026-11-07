package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func viewTool(name string, ro bool) Tool {
	return Tool{Name: name, Annotations: &ToolAnnotations{ReadOnlyHint: bp(ro)}}
}

func TestToolViewIDRoundTrip(t *testing.T) {
	id := ToolViewID("github", "list_issues")
	if id != "mcp.github.list_issues" {
		t.Fatalf("id %q", id)
	}
	s, tool, ok := ParseToolViewID(id)
	if !ok || s != "github" || tool != "list_issues" {
		t.Fatalf("parse %q %q %v", s, tool, ok)
	}
	for _, bad := range []string{"git.status", "mcp.", "mcp.x", "mcp..t", "mcp.x."} {
		if _, _, ok := ParseToolViewID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
	if _, tool, ok := ParseToolViewID(ToolViewID("s", ResourceViewTool)); !ok || tool != ResourceViewTool {
		t.Fatal("resource view id")
	}
}

func TestToolViewsParamsFromScalars(t *testing.T) {
	tl := viewTool("list_issues", true)
	tl.InputSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"repo":   map[string]any{"type": "string", "description": "owner/name"},
			"state":  map[string]any{"type": "string", "enum": []any{"open", "closed"}},
			"limit":  map[string]any{"type": "integer"},
			"labels": map[string]any{"type": "array"},
			"draft":  map[string]any{"type": []any{"boolean", "null"}},
		},
		"required": []any{"repo"},
	}
	v := ToolViews(Server{Name: "gh"}, []Tool{tl}, ServerCaps{}, nil, ViewRule{TrustAnnotations: true})
	if len(v) != 1 {
		t.Fatalf("%d views", len(v))
	}
	got := []string{}
	for _, p := range v[0].Params {
		got = append(got, p.ID+":"+p.Kind)
	}
	if strings.Join(got, ",") != "repo:string,draft:bool,limit:int,state:enum" {
		t.Fatalf("params %v", got)
	}
	if !v[0].Params[0].Required || v[0].Params[0].Hint != "owner/name" {
		t.Fatalf("repo param %+v", v[0].Params[0])
	}
	if !v[0].Run || v[0].Layout != LayoutJSON {
		t.Fatalf("run %v layout %q (unknown output → json)", v[0].Run, v[0].Layout)
	}
}

func TestToolViewsRunRule(t *testing.T) {
	ro, rw := viewTool("get", true), viewTool("create", false)
	need := viewTool("bulk", true)
	need.InputSchema = map[string]any{"properties": map[string]any{"items": map[string]any{"type": "array"}}, "required": []any{"items"}}
	prof := &Profile{Name: "p", Servers: []string{"other"}}
	cases := []struct {
		name string
		srv  Server
		tool Tool
		rule ViewRule
		run  bool
	}{
		{"read-only trusted", Server{Name: "s"}, ro, ViewRule{TrustAnnotations: true}, true},
		{"mutating", Server{Name: "s"}, rw, ViewRule{TrustAnnotations: true}, false},
		{"hints not trusted", Server{Name: "s"}, ro, ViewRule{}, false},
		{"server off", Server{Name: "s", Disabled: true}, ro, ViewRule{TrustAnnotations: true}, false},
		{"tool off", Server{Name: "s"}, ro, ViewRule{TrustAnnotations: true, Off: func(string, string) bool { return true }}, false},
		{"outside profile", Server{Name: "s"}, ro, ViewRule{TrustAnnotations: true, Profile: prof}, false},
		{"object input", Server{Name: "s"}, need, ViewRule{TrustAnnotations: true}, false},
	}
	for _, c := range cases {
		v := ToolViews(c.srv, []Tool{c.tool}, ServerCaps{}, nil, c.rule)
		if len(v) != 1 || v[0].Run != c.run {
			t.Errorf("%s: run %v, want %v", c.name, v[0].Run, c.run)
		}
		if !c.run && v[0].Why == "" {
			t.Errorf("%s: no why", c.name)
		}
	}
	if v := ToolViews(Server{Name: "s", Deny: []string{"get"}}, []Tool{ro}, ServerCaps{}, nil, ViewRule{TrustAnnotations: true}); len(v) != 0 {
		t.Fatal("a denied tool has a view")
	}
}

func TestToolViewsFieldsFromOutputSchema(t *testing.T) {
	tl := viewTool("list_issues", true)
	tl.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{
		"issues": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
			"number":     map[string]any{"type": "integer"},
			"title":      map[string]any{"type": "string"},
			"created_at": map[string]any{"type": "string", "format": "date-time"},
			"url":        map[string]any{"type": "string", "format": "uri"},
		}}},
		"total": map[string]any{"type": "integer"},
	}}
	v := ToolViews(Server{Name: "gh"}, []Tool{tl}, ServerCaps{}, nil, ViewRule{TrustAnnotations: true})[0]
	if v.Layout != LayoutTable || v.Typed != "schema" {
		t.Fatalf("layout %q typed %q", v.Layout, v.Typed)
	}
	kinds := map[string]string{}
	for _, f := range v.Fields {
		kinds[f.ID] = f.Kind
	}
	if kinds["number"] != FieldNum || kinds["created_at"] != FieldTime || kinds["url"] != FieldLink || kinds["title"] != FieldText {
		t.Fatalf("kinds %v", kinds)
	}
	obj := viewTool("get_user", true)
	obj.OutputSchema = map[string]any{"type": "object", "properties": map[string]any{"login": map[string]any{"type": "string"}, "id": map[string]any{"type": "integer"}}}
	if v := ToolViews(Server{Name: "gh"}, []Tool{obj}, ServerCaps{}, nil, ViewRule{})[0]; v.Layout != LayoutCard {
		t.Fatalf("small object layout %q", v.Layout)
	}
}

func TestShapeResultTableKeepsKeyOrder(t *testing.T) {
	res := CallResult{StructuredContent: json.RawMessage(`{"issues":[{"title":"Crash","number":7,"open":true,"created":"2026-11-01T10:00:00Z","labels":["bug"]},{"title":"Slow","number":9,"open":false}],"total":2}`)}
	d := ShapeResult(res, nil, false)
	if d.Layout != LayoutTable || d.Total != 2 || len(d.Rows) != 2 {
		t.Fatalf("%+v", d)
	}
	ids := []string{}
	for _, f := range d.Fields {
		ids = append(ids, f.ID+":"+f.Kind)
	}
	if strings.Join(ids, ",") != "title:text,number:num,open:bool,created:time,labels:json" {
		t.Fatalf("fields %v", ids)
	}
	r := d.Rows[0]
	if r[1].N != 7 || !r[2].B || r[3].T == 0 || r[4].S != "[1 item]" {
		t.Fatalf("row %+v", r)
	}
	if !strings.Contains(d.Raw, `"total": 2`) {
		t.Fatalf("raw JSON is not kept: %q", d.Raw)
	}
	if ShapeResult(res, nil, true).Layout != LayoutList {
		t.Fatal("compact is not a list")
	}
}

func TestShapeResultShapes(t *testing.T) {
	cases := []struct {
		name   string
		res    CallResult
		layout string
	}{
		{"text JSON array", CallResult{Content: []Content{{Type: "text", Text: `[{"a":1},{"a":2}]`}}}, LayoutTable},
		{"small object", CallResult{StructuredContent: json.RawMessage(`{"login":"amin","id":1}`)}, LayoutCard},
		{"big object", CallResult{StructuredContent: json.RawMessage(`{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7}`)}, LayoutDetail},
		{"scalars", CallResult{StructuredContent: json.RawMessage(`["x","y"]`)}, LayoutList},
		{"plain text", CallResult{Content: []Content{{Type: "text", Text: "hello"}}}, LayoutText},
		{"number", CallResult{StructuredContent: json.RawMessage(`42`)}, LayoutJSON},
		{"error", CallResult{IsError: true, Content: []Content{{Type: "text", Text: "no such repo"}}}, LayoutStatus},
	}
	for _, c := range cases {
		d := ShapeResult(c.res, nil, false)
		if d.Layout != c.layout {
			t.Errorf("%s: layout %q, want %q", c.name, d.Layout, c.layout)
		}
		if d.Raw == "" {
			t.Errorf("%s: no raw", c.name)
		}
	}
}

func TestShapeResultRedactsAndCaps(t *testing.T) {
	var b strings.Builder
	b.WriteString(`[`)
	for i := 0; i < MaxViewRows+50; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"n":1,"s":"Bearer abcdefghijklmnopqrstuvwxyz0123456789"}`)
	}
	b.WriteString(`]`)
	d := ShapeResult(CallResult{StructuredContent: json.RawMessage(b.String())}, nil, false)
	if len(d.Rows) != MaxViewRows || !d.Truncated || d.Total != MaxViewRows+50 {
		t.Fatalf("rows %d truncated %v total %d", len(d.Rows), d.Truncated, d.Total)
	}
	if strings.Contains(d.Rows[0][1].S, "abcdefghijklmnop") || strings.Contains(d.Raw, "abcdefghijklmnop") {
		t.Fatal("a token reached the phone")
	}
	if len(d.Raw) > MaxViewJSON {
		t.Fatalf("raw %d bytes", len(d.Raw))
	}
}

func TestLearnFields(t *testing.T) {
	l, ok := LearnFields(CallResult{StructuredContent: json.RawMessage(`{"items":[{"id":"a","size":3}]}`)})
	if !ok || l.Layout != LayoutTable || len(l.Fields) != 2 || l.Fields[0].ID != "id" || l.Fields[1].Kind != FieldNum {
		t.Fatalf("%+v %v", l, ok)
	}
	for _, r := range []CallResult{
		{Content: []Content{{Type: "text", Text: "plain"}}},
		{IsError: true, StructuredContent: json.RawMessage(`{"a":1}`)},
		{StructuredContent: json.RawMessage(`[]`)},
		{StructuredContent: json.RawMessage(`["x"]`)},
	} {
		if _, ok := LearnFields(r); ok {
			t.Errorf("learned from %+v", r)
		}
	}
}

func TestLearnedTypesTheNextView(t *testing.T) {
	tl := viewTool("search", true)
	learned := map[string]Learned{"search": {Fields: []ViewField{{ID: "path", Label: "Path", Kind: FieldText}}, Layout: LayoutTable}}
	v := ToolViews(Server{Name: "s"}, []Tool{tl}, ServerCaps{}, learned, ViewRule{TrustAnnotations: true})[0]
	if v.Typed != "learned" || v.Layout != LayoutTable || len(v.Fields) != 1 {
		t.Fatalf("%+v", v)
	}
}

func TestManagerLearnResultPersists(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{opts: ManagerOptions{CacheDir: dir}, servers: map[string]*serverState{"s": {cfg: Server{Name: "s"}, tools: []Tool{viewTool("search", true)}, haveTools: true}}}
	m.LearnResult("s", "search", CallResult{StructuredContent: json.RawMessage(`[{"path":"a.go","line":3}]`)})
	m.LearnResult("s", "search", CallResult{StructuredContent: json.RawMessage(`[{"other":1}]`)}) // first time only
	got := m.LearnedViews("s")["search"]
	if len(got.Fields) != 2 || got.Fields[0].ID != "path" {
		t.Fatalf("%+v", got)
	}
	again := &Manager{opts: ManagerOptions{CacheDir: dir}, servers: map[string]*serverState{"s": {cfg: Server{Name: "s"}}}}
	if l := again.LearnedViews("s")["search"]; len(l.Fields) != 2 {
		t.Fatalf("not kept on disk: %+v", l)
	}
	changed := &Manager{opts: ManagerOptions{CacheDir: dir}, servers: map[string]*serverState{"s": {cfg: Server{Name: "s", raw: json.RawMessage(`{"command":"other"}`)}}}}
	if l := changed.LearnedViews("s"); len(l) != 0 {
		t.Fatal("kept after the entry changed")
	}
}

func TestResourceViewAndShapes(t *testing.T) {
	v := ResourceView(Server{Name: "docs"}, []Resource{{URI: "file:///a.md", Name: "a"}}, []ResourceTemplate{{URITemplate: "repo://{owner}/{name}{?ref}", Name: "repo"}})
	if v.ID != "mcp.docs.@resources" || !v.Run || len(v.Resources) != 2 {
		t.Fatalf("%+v", v)
	}
	if got := strings.Join(v.Resources[1].Vars, ","); got != "owner,name,ref" {
		t.Fatalf("vars %q", got)
	}
	if d := ShapeResource([]ResourceContents{{Text: `[{"a":1}]`, MimeType: "application/json"}}, false); d.Layout != LayoutTable {
		t.Fatalf("array %q", d.Layout)
	}
	if d := ShapeResource([]ResourceContents{{Text: `{"a":1}`}}, false); d.Layout != LayoutDetail {
		t.Fatalf("object %q", d.Layout)
	}
	if d := ShapeResource([]ResourceContents{{Text: "# Hi", MimeType: "text/markdown"}}, false); d.Layout != LayoutText || d.Text != "# Hi" {
		t.Fatalf("text %+v", d)
	}
	if d := ShapeResource([]ResourceContents{{Blob: "AAAA", MimeType: "image/png"}}, false); d.Layout != LayoutText || !strings.Contains(d.Text, "image/png") {
		t.Fatalf("binary %+v", d)
	}
}

func TestViewArgs(t *testing.T) {
	v := ToolView{Params: []ViewParam{
		{ID: "repo", Label: "Repo", Kind: "string", Required: true},
		{ID: "limit", Label: "Limit", Kind: "int"},
		{ID: "state", Label: "State", Kind: "enum", Enum: []string{"open", "closed"}},
	}}
	got, err := ViewArgs(v, map[string]string{"repo": "a/b", "limit": "5", "state": "open"})
	if err != nil || got["limit"] != int64(5) || got["repo"] != "a/b" {
		t.Fatalf("%v %v", got, err)
	}
	for name, in := range map[string]map[string]string{
		"missing": {"limit": "5"},
		"unknown": {"repo": "a", "x": "1"},
		"not int": {"repo": "a", "limit": "five"},
		"enum":    {"repo": "a", "state": "merged"},
		"secret":  {"repo": "Bearer abcdefghijklmnopqrstuvwxyz0123456789"},
	} {
		if _, err := ViewArgs(v, in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func FuzzShapeJSON(f *testing.F) {
	for _, s := range []string{`[]`, `{}`, `[{"a":1}]`, `{"x":[{"a":{"b":2}}]}`, `"s"`, `[1,[2]]`, `{"a":null}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !json.Valid([]byte(s)) {
			return
		}
		d := ShapeJSON(json.RawMessage(s), nil, false)
		if len(d.Rows) > MaxViewRows {
			t.Fatalf("%d rows", len(d.Rows))
		}
		for _, r := range d.Rows {
			if len(r) != len(d.Fields) {
				t.Fatalf("row of %d cells for %d fields", len(r), len(d.Fields))
			}
		}
	})
}

func FuzzViewParams(f *testing.F) {
	f.Add(`{"properties":{"a":{"type":"string"},"b":{"enum":[1,"x"]}},"required":["a"]}`)
	f.Add(`{"properties":{"a":{"type":["null","integer"]}}}`)
	f.Fuzz(func(t *testing.T, s string) {
		var schema map[string]any
		if json.Unmarshal([]byte(s), &schema) != nil {
			return
		}
		params, _ := viewParams(schema)
		if len(params) > MaxViewParams {
			t.Fatalf("%d params", len(params))
		}
		_, _, _ = schemaFields(schema)
	})
}

// Every shape becomes a block that passes the block caps, with the kind
// of its layout; a newline in a cell never breaks it.
func TestViewDataBlockKinds(t *testing.T) {
	cases := map[string]CallResult{
		"table":  {StructuredContent: json.RawMessage(`[{"a":"x\ny","b":2}]`)},
		"card":   {StructuredContent: json.RawMessage(`{"login":"amin"}`)},
		"detail": {StructuredContent: json.RawMessage(`{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7}`)},
		"list":   {StructuredContent: json.RawMessage(`["x","y"]`)},
		"text":   {Content: []Content{{Type: "text", Text: "hello\n\nworld"}}},
		"json":   {StructuredContent: json.RawMessage(`42`)},
		"status": {IsError: true, Content: []Content{{Type: "text", Text: "no such repo"}}},
	}
	for kind, res := range cases {
		b := ShapeResult(res, nil, false).Block("mcp.gh.@resources", "Issues", "mcp.gh.list")
		if err := b.Validate(); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
		if b.Kind != kind {
			t.Errorf("%s: block kind %q", kind, b.Kind)
		}
	}
}

// A result over the block cap keeps the rows that fit and says so.
func TestViewDataBlockFits(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < MaxViewRows; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"a":"` + strings.Repeat("x", 190) + `","b":"` + strings.Repeat("y", 190) + `"}`)
	}
	sb.WriteString("]")
	b := ShapeResult(CallResult{StructuredContent: json.RawMessage(sb.String())}, nil, false).Block("v", "T", "")
	if err := b.Validate(); err != nil || b.Kind != "table" || !b.Truncated || len(b.Rows) == 0 || len(b.Rows) == MaxViewRows {
		t.Fatalf("kind %q rows %d truncated %v err %v", b.Kind, len(b.Rows), b.Truncated, err)
	}
}

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

func covSchema(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCovToolViewsRulesAndTitles(t *testing.T) {
	ro := &ToolAnnotations{ReadOnlyHint: &yes}
	tools := []Tool{
		{Name: "list_items", Annotations: ro, InputSchema: covSchema(t, `{"properties":{"q":{"type":"string"}}}`)},
		{Name: "titled", Title: "A Title", Annotations: ro},
		{Name: "annotated", Annotations: &ToolAnnotations{Title: "From Annotation", ReadOnlyHint: &yes}},
		{Name: "denied", Annotations: ro},
	}
	s := Server{Name: "srv", Deny: []string{"denied"}}
	prof := Profile{Servers: []string{"other"}}
	views := ToolViews(s, tools, ServerCaps{Resources: true}, nil, ViewRule{TrustAnnotations: true, Profile: &prof})
	if len(views) != 4 { // 3 tools (one denied) + resources
		t.Fatalf("got %d views", len(views))
	}
	if views[0].Title != "List items" || views[1].Title != "A Title" || views[2].Title != "From Annotation" {
		t.Errorf("titles: %q %q %q", views[0].Title, views[1].Title, views[2].Title)
	}
	for _, v := range views[:3] {
		if v.Run || !strings.Contains(v.Why, "profile") {
			t.Errorf("%s runs outside the profile: %v %q", v.ID, v.Run, v.Why)
		}
	}
	rv := views[3]
	if rv.Run || rv.Tool != ResourceViewTool || !strings.Contains(rv.Why, "profile") {
		t.Errorf("resource view must follow the profile: %+v", rv)
	}
	// inside the profile and trusted, a read-only tool runs
	ok := ToolViews(s, tools[:1], ServerCaps{Resources: true}, nil, ViewRule{TrustAnnotations: true})
	if !ok[0].Run || !ok[1].Run {
		t.Errorf("trusted read-only views must run: %+v", ok)
	}
}

func TestCovRunnableReasons(t *testing.T) {
	ro := Tool{Annotations: &ToolAnnotations{ReadOnlyHint: &yes}}
	cases := []struct {
		name string
		s    Server
		t    Tool
		r    ViewRule
		cx   bool
		why  string
	}{
		{"off", Server{Disabled: true}, ro, ViewRule{}, false, "turned off"},
		{"tool off", Server{}, ro, ViewRule{Off: func(string, string) bool { return true }}, false, "tool is turned off"},
		{"writes", Server{}, Tool{}, ViewRule{TrustAnnotations: true}, false, "can change things"},
		{"untrusted", Server{}, ro, ViewRule{}, false, "not trusted"},
		{"complex", Server{}, ro, ViewRule{TrustAnnotations: true}, true, "object or list"},
		{"ok", Server{}, ro, ViewRule{TrustAnnotations: true}, false, ""},
	}
	for _, c := range cases {
		run, why := runnable(c.s, c.t, c.r, c.cx)
		if run != (c.why == "") || !strings.Contains(why, c.why) {
			t.Errorf("%s: run=%v why=%q", c.name, run, why)
		}
	}
}

func TestCovViewParams(t *testing.T) {
	props := map[string]any{}
	req := []any{}
	for i := 0; i < MaxViewParams+2; i++ {
		props[fmt.Sprintf("p%02d", i)] = map[string]any{"type": "string"}
	}
	props["zobj"] = map[string]any{"type": "object"}
	props["a_num"] = map[string]any{"type": "number", "description": "how  many\nthings"}
	props["a_uri"] = map[string]any{"type": "string", "format": "uri"}
	props["a_enum"] = map[string]any{"enum": []any{"x", 2.0, true, nil}}
	props["a_int"] = map[string]any{"type": []any{"null", "integer"}}
	props["a_bool"] = map[string]any{"type": "boolean"}
	props["a_arr"] = map[string]any{"type": "array"}
	props["a_nil"] = nil
	req = append(req, "zobj", "a_arr", "p99", 7)
	params, complexInput := viewParams(map[string]any{"properties": props, "required": req})
	if !complexInput {
		t.Error("a required object input must make the tool complex")
	}
	if len(params) != MaxViewParams {
		t.Errorf("params %d, want the cap %d", len(params), MaxViewParams)
	}
	kinds := map[string]string{}
	for _, p := range params {
		kinds[p.ID] = p.Kind
	}
	for id, k := range map[string]string{"a_num": "number", "a_uri": "uri", "a_enum": "enum", "a_int": "int", "a_bool": "bool"} {
		if kinds[id] != k {
			t.Errorf("kind of %s = %q, want %q", id, kinds[id], k)
		}
	}
	if _, has := kinds["zobj"]; has {
		t.Error("an object input is not a phone param")
	}
	for _, p := range params {
		if p.ID == "a_num" && p.Hint != "how many things" {
			t.Errorf("hint %q", p.Hint)
		}
		if p.ID == "a_enum" && strings.Join(p.Enum, ",") != "x,2,true" {
			t.Errorf("enum %v", p.Enum)
		}
	}
	// an enum is cut at the cap
	var many []any
	for i := 0; i < maxViewEnum+5; i++ {
		many = append(many, fmt.Sprint("v", i))
	}
	if got := enumOf(map[string]any{"enum": many}); len(got) != maxViewEnum {
		t.Errorf("enum len %d", len(got))
	}
	if paramKind(nil) != "" || paramKind(map[string]any{"type": "wat"}) != "" {
		t.Error("paramKind of nothing")
	}
}

func TestCovSchemaFields(t *testing.T) {
	if _, _, ok := schemaFields(nil); ok {
		t.Error("no schema")
	}
	f, layout, ok := schemaFields(covSchema(t, `{"type":"array","items":{"type":"object","properties":{"id":{"type":"integer"},"ok":{"type":"boolean"},"at":{"type":"string","format":"date-time"},"o":{"type":"object"}}}}`))
	if !ok || layout != LayoutTable || len(f) != 4 {
		t.Fatalf("array of objects: %v %s %v", f, layout, ok)
	}
	kinds := map[string]string{}
	for _, x := range f {
		kinds[x.ID] = x.Kind
	}
	if kinds["id"] != FieldNum || kinds["ok"] != FieldBool || kinds["at"] != FieldTime || kinds["o"] != FieldJSON {
		t.Errorf("kinds %v", kinds)
	}
	f, layout, ok = schemaFields(covSchema(t, `{"type":"array","items":{"type":"string"}}`))
	if !ok || layout != LayoutList || len(f) != 1 || f[0].ID != "value" {
		t.Errorf("array of strings: %v %s", f, layout)
	}
	if _, _, ok := schemaFields(covSchema(t, `{"type":"object"}`)); ok {
		t.Error("an object without properties has no fields")
	}
	f, layout, ok = schemaFields(covSchema(t, `{"type":"object","properties":{"a":{"type":"string"}}}`))
	if !ok || layout != LayoutCard || len(f) != 1 {
		t.Errorf("small object: %v %s", f, layout)
	}
	big := map[string]any{}
	for i := 0; i < cardMaxFields+2; i++ {
		big[fmt.Sprintf("f%d", i)] = map[string]any{"type": "string"}
	}
	if _, layout, _ := schemaFields(map[string]any{"type": "object", "properties": big}); layout != LayoutDetail {
		t.Errorf("big object layout %s", layout)
	}
	if _, _, ok := schemaFields(covSchema(t, `{"type":"string"}`)); ok {
		t.Error("a string schema has no fields")
	}
	// a learned layout is used when the tool has no schema; empty layout = table
	v := toolView(Server{Name: "s"}, Tool{Name: "t"}, Learned{Fields: []ViewField{{ID: "a", Label: "A", Kind: FieldText}}}, ViewRule{})
	if v.Layout != LayoutTable || v.Typed != "learned" {
		t.Errorf("learned view: %s %s", v.Layout, v.Typed)
	}
	if got := len(capFields(make([]ViewField, MaxViewFields+3), MaxViewFields)); got != MaxViewFields {
		t.Errorf("capFields %d", got)
	}
	wide := map[string]any{}
	for i := 0; i < MaxTableFields+3; i++ {
		wide[fmt.Sprintf("f%02d", i)] = map[string]any{"type": "string"}
	}
	if got := propFields(map[string]any{"properties": wide}, MaxTableFields); len(got) != MaxTableFields {
		t.Errorf("propFields %d", len(got))
	}
}

func TestCovShapeResultVariants(t *testing.T) {
	d := ShapeResult(CallResult{IsError: true, Content: []Content{{Type: "text", Text: "nope"}}}, nil, false)
	if d.Layout != LayoutStatus || !d.Error || d.Text != "nope" {
		t.Errorf("error: %+v", d)
	}
	d = ShapeResult(CallResult{}, nil, false)
	if d.Layout != LayoutText || !strings.Contains(d.Raw, "content") && d.Raw == "" {
		t.Errorf("empty: %+v", d)
	}
	d = ShapeResult(CallResult{Content: []Content{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}}, nil, false)
	if d.Text != "a\nb" {
		t.Errorf("two texts: %q", d.Text)
	}
	d = ShapeResult(textResult(strings.Repeat("x", MaxViewText+10)), nil, false)
	if !d.Truncated || len(d.Text) != MaxViewText {
		t.Errorf("long text: truncated %v len %d", d.Truncated, len(d.Text))
	}
	if !d.Truncated || len(d.Raw) > MaxViewJSON {
		t.Errorf("raw is capped: %d", len(d.Raw))
	}
	// a secret in a result never leaves the laptop
	d = ShapeResult(textResult("token ghp_abcdefghijklmnopqrstuvwxyz0123456789"), nil, false)
	if strings.Contains(d.Text, "ghp_abcdef") || strings.Contains(d.Raw, "ghp_abcdef") {
		t.Errorf("secret leaked: %+v", d)
	}
	// JSON in the text content is shaped like structured content
	d = ShapeResult(textResult(`[{"a":1}]`), nil, true)
	if d.Layout != LayoutList || d.Total != 1 {
		t.Errorf("json text, compact: %s %d", d.Layout, d.Total)
	}
	if ShapeJSON(json.RawMessage(`{`), nil, false).Layout != LayoutJSON {
		t.Error("broken json")
	}
	if d := ShapeJSON(json.RawMessage(`[]`), nil, false); d.Layout != LayoutTable || len(d.Rows) != 0 {
		t.Errorf("empty array: %+v", d)
	}
	if ShapeJSON(json.RawMessage(`3`), nil, false).Layout != LayoutJSON {
		t.Error("a scalar is json")
	}
	// rows over the cap
	var arr []map[string]int
	for i := 0; i < MaxViewRows+3; i++ {
		arr = append(arr, map[string]int{"n": i})
	}
	raw, _ := json.Marshal(arr)
	d = ShapeJSON(raw, nil, false)
	if len(d.Rows) != MaxViewRows || !d.Truncated || d.Total != MaxViewRows+3 {
		t.Errorf("rows %d truncated %v total %d", len(d.Rows), d.Truncated, d.Total)
	}
	var sc []int
	for i := 0; i < MaxViewRows+3; i++ {
		sc = append(sc, i)
	}
	raw, _ = json.Marshal(sc)
	d = ShapeJSON(raw, nil, false)
	if d.Layout != LayoutList || len(d.Rows) != MaxViewRows || !d.Truncated {
		t.Errorf("scalar rows %d %v", len(d.Rows), d.Truncated)
	}
}

func TestCovCellsAndLearning(t *testing.T) {
	if c := cellOf(3.5); c.S != "3.5" || c.N != 3.5 {
		t.Errorf("float cell %+v", c)
	}
	if c := cellOf([]any{1}); c.S != "[1 item]" {
		t.Errorf("one item %+v", c)
	}
	if c := cellOf([]any{1, 2}); c.S != "[2 items]" {
		t.Errorf("items %+v", c)
	}
	if c := cellOf(map[string]any{}); c.S != "{…}" {
		t.Errorf("object %+v", c)
	}
	if c := cellOf(true); !c.B || c.S != "yes" {
		t.Errorf("bool %+v", c)
	}
	if c := cellOf(struct{ A int }{1}); c.S != "{1}" {
		t.Errorf("other %+v", c)
	}
	if c := cellOf("2024-05-06T07:08:09Z"); c.T == 0 {
		t.Errorf("time cell %+v", c)
	}
	if _, ok := parseTime("2024-05-06"); !ok {
		t.Error("date only")
	}
	if _, ok := parseTime("2024-13-45"); ok {
		t.Error("a bad date")
	}
	if _, ok := parseTime("short"); ok {
		t.Error("short")
	}
	if subRaw(json.RawMessage(`{`), "a") != nil {
		t.Error("subRaw of broken json")
	}
	if learnFrom(json.RawMessage(`[]`)) != nil || learnFrom(json.RawMessage(`[1]`)) != nil || learnFrom(json.RawMessage(``)) != nil || learnFrom(json.RawMessage(`"s"`)) != nil {
		t.Error("learnFrom of no object")
	}
	if got := learnFrom(json.RawMessage(`{"a":1,"a":2,"":3,"link":"https://x","when":"2024-01-02","ok":true,"o":{},"s":"t"}`)); len(got) != 6 {
		t.Errorf("fields %+v", got)
	} else {
		want := []string{FieldNum, FieldLink, FieldTime, FieldBool, FieldJSON, FieldText}
		for i, f := range got {
			if f.Kind != want[i] {
				t.Errorf("field %s kind %s want %s", f.ID, f.Kind, want[i])
			}
		}
	}
	if got := learnFrom(json.RawMessage(`{"a":1,"b":`)); len(got) != 1 {
		t.Errorf("a cut object keeps what was read: %+v", got)
	}
	if _, ok := LearnFields(CallResult{IsError: true}); ok {
		t.Error("an error teaches nothing")
	}
	if _, ok := LearnFields(textResult("plain")); ok {
		t.Error("plain text teaches nothing")
	}
	if _, ok := LearnFields(textResult(`["a","b"]`)); ok {
		t.Error("a list of scalars teaches nothing")
	}
	if l, ok := LearnFields(textResult(`[{"id":1}]`)); !ok || len(l.Fields) != 1 {
		t.Error("a list of objects teaches its fields")
	}
	if valueKind(json.Number("1")) != FieldNum || valueKind(2.0) != FieldNum {
		t.Error("valueKind numbers")
	}
}

func TestCovResourceViews(t *testing.T) {
	var res []Resource
	var tmpl []ResourceTemplate
	for i := 0; i < maxResourceChoice+5; i++ {
		res = append(res, Resource{URI: fmt.Sprint("file:///", i), Name: "n", Title: "T"})
	}
	v := ResourceView(Server{Name: "s", Disabled: true}, res, nil)
	if v.Run || v.Why == "" || len(v.Resources) != maxResourceChoice || v.Resources[0].Name != "T" {
		t.Errorf("view: run %v why %q n %d", v.Run, v.Why, len(v.Resources))
	}
	tmpl = append(tmpl, ResourceTemplate{URITemplate: "file:///{+path}/{a,b*}/{a}", Name: "n", Title: "Tt"}, ResourceTemplate{URITemplate: "x:{open", Name: "m"})
	v = ResourceView(Server{Name: "s"}, res[:maxResourceChoice-1], tmpl)
	if len(v.Resources) != maxResourceChoice || !v.Resources[maxResourceChoice-1].Template || v.Resources[maxResourceChoice-1].Name != "Tt" {
		t.Errorf("templates: %+v", v.Resources[len(v.Resources)-1])
	}
	if got := strings.Join(templateVars("file:///{+path}/{a,b*}/{a}"), ","); got != "path,a,b" {
		t.Errorf("template vars %q", got)
	}
	if templateVars("x:{open") != nil {
		t.Error("an open brace has no var")
	}
	// shaping a resource
	if d := ShapeResource(nil, false); d.Text != "(empty)" {
		t.Errorf("empty %+v", d)
	}
	if d := ShapeResource([]ResourceContents{{MimeType: "image/png", Blob: "AAAAAAAA"}}, false); !strings.Contains(d.Text, "binary image/png") {
		t.Errorf("blob %+v", d)
	}
	if d := ShapeResource([]ResourceContents{{Blob: "AAAA"}}, false); !strings.Contains(d.Text, "binary data") {
		t.Errorf("blob without type %+v", d)
	}
	if d := ShapeResource([]ResourceContents{{Text: `{"a":1}`, MimeType: "application/json"}}, false); d.Layout != LayoutDetail && d.Layout != LayoutCard {
		t.Errorf("json object %+v", d)
	} else if d.Layout == LayoutCard {
		t.Error("a resource object reads as a detail")
	}
	if d := ShapeResource([]ResourceContents{{Text: "{broken", MimeType: "application/json"}}, false); d.Layout != LayoutText {
		t.Errorf("broken json is text: %+v", d)
	}
	if d := ShapeResource([]ResourceContents{{Text: strings.Repeat("y", MaxViewText+5)}}, false); !d.Truncated {
		t.Error("long resource text is cut")
	}
}

func TestCovViewArgs(t *testing.T) {
	v := ToolView{ID: "mcp.s.t", Params: []ViewParam{
		{ID: "n", Label: "N", Kind: "int"}, {ID: "f", Label: "F", Kind: "number"}, {ID: "b", Label: "B", Kind: "bool"},
		{ID: "e", Label: "E", Kind: "enum", Enum: []string{"x", "y"}}, {ID: "s", Label: "S", Kind: "string", Required: true},
	}}
	got, err := ViewArgs(v, map[string]string{"n": "3", "f": "1.5", "b": "true", "e": "y", "s": "hi"})
	if err != nil || got["n"] != int64(3) || got["f"] != 1.5 || got["b"] != true || got["e"] != "y" {
		t.Errorf("typed args %v %v", got, err)
	}
	for in, want := range map[string]string{"n": "whole number", "f": "a number", "b": "true or false", "e": "one of x, y"} {
		if _, err := ViewArgs(v, map[string]string{"s": "hi", in: "zzz"}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("bad %s: %v", in, err)
		}
	}
	if _, err := ViewArgs(v, map[string]string{"zz": "1"}); err == nil || !strings.Contains(err.Error(), "unknown param") {
		t.Errorf("unknown param: %v", err)
	}
	if _, err := ViewArgs(ToolView{}, map[string]string{"zz": "1"}); err == nil || !strings.Contains(err.Error(), "none") {
		t.Errorf("no params: %v", err)
	}
	if _, err := ViewArgs(v, map[string]string{}); err == nil {
		t.Error("a missing required param must fail")
	}
	if _, err := ViewArgs(v, map[string]string{"s": "ghp_abcdefghijklmnopqrstuvwxyz0123456789"}); err == nil {
		t.Error("a secret-looking value must be refused")
	}
}

func TestCovTextHelpers(t *testing.T) {
	if humanize("createdAt_id") != "Created at id" || humanize("___") != "___" || humanize("a-b") != "A b" {
		t.Errorf("humanize %q %q", humanize("createdAt_id"), humanize("___"))
	}
	if orDefault("", "d") != "d" || orDefault("x", "d") != "x" {
		t.Error("orDefault")
	}
	if oneLine("a  b\n c", 100) != "a b c" || clipRunes("héllo", 3) != "hé…" {
		t.Error("oneLine / clipRunes")
	}
	if got := clipText("aé", 2); got != "a" {
		t.Errorf("clipText must cut on a rune boundary, got %q", got)
	}
	if redact("plain") != "plain" {
		t.Error("redact")
	}
}

func TestCovViewDataBlocks(t *testing.T) {
	check := func(name string, d ViewData, kind string) blocks.Block {
		t.Helper()
		b := d.Block("my id!", "Title", "mcp.s.t")
		if err := b.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if b.Kind != kind {
			t.Errorf("%s: kind %s want %s", name, b.Kind, kind)
		}
		return b
	}
	cell := func(s string) Cell { return Cell{S: s} }
	var wide []ViewField
	var row []Cell
	for i := 0; i < blocks.MaxColumns+3; i++ {
		wide = append(wide, ViewField{ID: fmt.Sprint("f", i), Label: fmt.Sprint("F", i), Kind: FieldText})
		row = append(row, cell("v"))
	}
	if b := check("wide table", ViewData{Layout: LayoutTable, Fields: wide, Rows: [][]Cell{row}}, blocks.KindTable); len(b.Columns) != blocks.MaxColumns {
		t.Errorf("columns %d", len(b.Columns))
	}
	check("table without fields", ViewData{Layout: LayoutTable, Raw: `{"a":1}`}, blocks.KindJSON)
	if b := check("detail", ViewData{Layout: LayoutDetail, Fields: wide, Rows: [][]Cell{row[:2]}}, blocks.KindDetail); len(b.Fields) != 2 {
		t.Errorf("detail fields %d", len(b.Fields))
	}
	check("card", ViewData{Layout: LayoutCard, Fields: wide[:1], Rows: [][]Cell{{cell("x")}}}, blocks.KindCard)
	check("card without rows", ViewData{Layout: LayoutCard, Fields: wide[:1], Raw: `[1]`}, blocks.KindJSON)
	check("status empty", ViewData{Layout: LayoutStatus}, blocks.KindStatus)
	check("text empty", ViewData{Layout: LayoutText}, blocks.KindText)
	b := check("text paragraphs", ViewData{Layout: LayoutText, Text: "a\nb\n\n\n\nc\r\nd"}, blocks.KindText)
	if len(b.MD.C) != 2 || len(b.MD.C[0].C) != 3 {
		t.Errorf("paragraphs: %+v", b.MD)
	}
	b = check("list", ViewData{Layout: LayoutList, Fields: []ViewField{{ID: "t", Kind: FieldText}, {ID: "w", Kind: FieldTime}, {ID: "a", Kind: FieldText}, {ID: "b", Kind: FieldText}, {ID: "c", Kind: FieldText}},
		Rows: [][]Cell{{cell("title"), {S: "when", T: 5}, cell("a"), cell("b"), cell("c")}, {cell(""), cell("")}, {}}}, blocks.KindList)
	if b.Items[0].Title != "title" || b.Items[0].Time != 5 || b.Items[0].Sub != "a · b" || b.Items[1].Title != "(empty)" {
		t.Errorf("items %+v", b.Items)
	}
	check("unknown layout is json", ViewData{Layout: "weird", Raw: `{"a":1}`}, blocks.KindJSON)
	if b := check("json text fallback", ViewData{Layout: LayoutJSON, Text: "not json"}, blocks.KindText); b.MD.C[0].Text != "not json" {
		t.Errorf("fallback %+v", b.MD)
	}
	check("json nothing", ViewData{Layout: LayoutJSON}, blocks.KindText)
	b = check("json too big", ViewData{Layout: LayoutJSON, Raw: strings.Repeat("z", blocks.MaxJSON*2)}, blocks.KindText)
	if !b.Truncated {
		t.Error("a cut text block says so")
	}
	// a table too big for one block is cut in rows
	var rows [][]Cell
	long := strings.Repeat("c", blocks.MaxSub)
	for i := 0; i < blocks.MaxRows; i++ {
		rows = append(rows, []Cell{cell(long), cell(long), cell(long), cell(long), cell(long)})
	}
	f5 := []ViewField{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}, {ID: "c", Label: "C"}, {ID: "d", Label: "D"}, {ID: "e", Label: "E"}}
	b = check("big table", ViewData{Layout: LayoutTable, Fields: f5, Rows: rows}, blocks.KindTable)
	if !b.Truncated || b.Total == 0 || len(b.Rows) >= blocks.MaxRows {
		t.Errorf("cut: truncated %v total %d rows %d", b.Truncated, b.Total, len(b.Rows))
	}
	// the same for a list and for text
	var lrows [][]Cell
	for i := 0; i < blocks.MaxItems; i++ {
		lrows = append(lrows, []Cell{cell(long), cell(long), cell(long)})
	}
	b = ViewData{Layout: LayoutList, Fields: f5[:3], Rows: lrows}.Block("l", "L", "s")
	if b.Validate() != nil {
		t.Errorf("big list: %v", b.Validate())
	}
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		sb.WriteString(strings.Repeat("p", 300))
		sb.WriteString("\n\n")
	}
	b = ViewData{Layout: LayoutText, Text: sb.String(), Raw: "x"}.Block("t", "T", "s")
	if b.Validate() != nil {
		t.Errorf("big text: %v", b.Validate())
	}
	if blockID("") != "mcp" || len(blockID(strings.Repeat("a", 200))) != blocks.MaxID || blockID("a b") != "a_b" {
		t.Errorf("blockID %q", blockID("a b"))
	}
	if colKind(FieldTime) != blocks.ColTime || colKind(FieldJSON) != blocks.ColMono || colKind(FieldLink) != blocks.ColMono || colKind(FieldNum) != blocks.ColNum {
		t.Error("colKind")
	}
}

func TestCovManagerLearnedViews(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{opts: ManagerOptions{CacheDir: dir}, servers: map[string]*serverState{}}
	srv := Server{Name: "s", raw: json.RawMessage(`{"command":"x"}`)}
	m.servers["s"] = &serverState{cfg: srv, haveTools: true, tools: []Tool{{Name: "typed", OutputSchema: map[string]any{"type": "object"}}, {Name: "list"}}, caps: ServerCaps{Resources: true}}
	res := textResult(`[{"id":1,"name":"a"}]`)
	m.LearnResult("nope", "list", res)              // unknown server: nothing
	m.LearnResult("s", "list", textResult("plain")) // nothing to learn
	m.LearnResult("s", "typed", res)                // the tool has its own schema
	if len(m.LearnedViews("s")) != 0 {
		t.Fatal("nothing should be learned yet")
	}
	m.LearnResult("s", "list", res)
	m.LearnResult("s", "list", textResult(`[{"other":1}]`)) // the first time only
	got := m.LearnedViews("s")
	if len(got["list"].Fields) != 2 || got["list"].Fields[0].ID != "id" {
		t.Fatalf("learned %+v", got)
	}
	if m.LearnedViews("nope") != nil {
		t.Error("unknown server")
	}
	// it is saved and read back by a fresh manager, as long as the entry did not change
	m2 := &Manager{opts: ManagerOptions{CacheDir: dir}, servers: map[string]*serverState{"s": {cfg: srv}}}
	if len(m2.LearnedViews("s")["list"].Fields) != 2 {
		t.Error("the saved fields are not read back")
	}
	m3 := &Manager{opts: ManagerOptions{CacheDir: dir}, servers: map[string]*serverState{"s": {cfg: Server{Name: "s", raw: json.RawMessage(`{"command":"changed"}`)}}}}
	if len(m3.LearnedViews("s")) != 0 {
		t.Error("an edited entry must forget what was learned")
	}
	// views of a server: unknown, unlisted tools, known
	if _, ok := m.ServerViews("nope", ViewRule{}); ok {
		t.Error("unknown server has no views")
	}
	if _, ok := m2.ServerViews("s", ViewRule{}); ok {
		t.Error("tools not known yet")
	}
	vs, ok := m.ServerViews("s", ViewRule{})
	if !ok || len(vs) != 3 {
		t.Fatalf("views %d %v", len(vs), ok)
	}
	if vs[1].Typed != "learned" {
		t.Errorf("the list view should use what was learned: %+v", vs[1])
	}
	// without a cache dir nothing is written, and a bad file is ignored
	m4 := &Manager{servers: map[string]*serverState{"s": {cfg: srv, haveTools: true}}}
	m4.LearnResult("s", "list", res)
	if len(m4.LearnedViews("s")) != 1 || m4.learnedPath("s") != "" {
		t.Error("learning works without a cache dir")
	}
	badDir := t.TempDir()
	m5 := &Manager{opts: ManagerOptions{CacheDir: badDir}, servers: map[string]*serverState{"s": {cfg: srv}}}
	_ = writeFileT(filepath.Join(badDir, "s.views.json"), "{not json")
	if len(m5.LearnedViews("s")) != 0 {
		t.Error("a broken file is ignored")
	}
	// too many learned tools in the file, and bad entries
	var tools []string
	for i := 0; i < MaxLearnedTools+3; i++ {
		tools = append(tools, fmt.Sprintf(`"t%d":{"fields":[{"id":"a","label":"A","kind":"text"}],"layout":"table"}`, i))
	}
	tools = append(tools, `"":{"fields":[{"id":"a"}]}`, `"nofields":{"fields":[]}`)
	body := fmt.Sprintf(`{"hash":%q,"tools":{%s}}`, srv.Hash(), strings.Join(tools, ","))
	capDir := t.TempDir()
	_ = writeFileT(filepath.Join(capDir, "s.views.json"), body)
	m6 := &Manager{opts: ManagerOptions{CacheDir: capDir}, servers: map[string]*serverState{"s": {cfg: srv}}}
	if n := len(m6.LearnedViews("s")); n > MaxLearnedTools {
		t.Errorf("learned %d tools, cap %d", n, MaxLearnedTools)
	}
}

func writeFileT(p, s string) error { return os.WriteFile(p, []byte(s), 0o644) }

var yes = true

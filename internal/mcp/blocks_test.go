package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

func blockOfJSON(t *testing.T, structured string) blocks.Block {
	t.Helper()
	return BlockOf("gh", Tool{Name: "list"}, CallResult{StructuredContent: json.RawMessage(structured)}, BlockOptions{})
}

func textResult(texts ...string) CallResult {
	var r CallResult
	for _, s := range texts {
		r.Content = append(r.Content, Content{Type: "text", Text: s})
	}
	return r
}

// MOB-12: each result shape becomes its block kind, and every block
// passes the caps.
func TestBlockOfShapes(t *testing.T) {
	user := []string{"user"}
	cases := []struct {
		name string
		res  CallResult
		kind string
		chk  func(t *testing.T, b blocks.Block)
	}{
		{"array of objects → table", CallResult{StructuredContent: json.RawMessage(`[{"id":"1","title":"Fix","n":3},{"id":"2","title":"Add","n":5,"extra":true}]`)}, blocks.KindTable,
			func(t *testing.T, b blocks.Block) {
				var ids []string
				for _, c := range b.Columns {
					ids = append(ids, c.ID+":"+c.Kind)
				}
				if got := strings.Join(ids, ","); got != "id:mono,title:text,n:num,extra:text" {
					t.Errorf("columns = %s", got)
				}
				if !reflect.DeepEqual(b.Rows[1], []string{"2", "Add", "5", "yes"}) || b.Rows[0][3] != "" || b.Total != 2 {
					t.Errorf("rows = %v total %d", b.Rows, b.Total)
				}
			}},
		{"array of scalars → list", CallResult{StructuredContent: json.RawMessage(`["a", 2, true, null]`)}, blocks.KindList,
			func(t *testing.T, b blocks.Block) {
				if len(b.Items) != 4 || b.Items[1].Title != "2" || b.Items[2].Title != "yes" || b.Items[3].Title != "(empty)" {
					t.Errorf("items = %+v", b.Items)
				}
			}},
		{"empty array → list", CallResult{StructuredContent: json.RawMessage(`[]`)}, blocks.KindList,
			func(t *testing.T, b blocks.Block) {
				if len(b.Items) != 1 || b.Items[0].Title != "No items" {
					t.Errorf("items = %+v", b.Items)
				}
			}},
		{"flat object → detail", CallResult{StructuredContent: json.RawMessage(`{"name":"api","state":"failed","runs":4,"tags":["a","b"]}`)}, blocks.KindDetail,
			func(t *testing.T, b blocks.Block) {
				if len(b.Fields) != 4 || b.Fields[1].Tone != blocks.ToneBad || b.Fields[2].Kind != blocks.ColNum || b.Fields[3].Value != `["a","b"]` || b.Fields[3].Kind != blocks.ColMono {
					t.Errorf("fields = %+v", b.Fields)
				}
			}},
		{"object with a list → table with total", CallResult{StructuredContent: json.RawMessage(`{"total_count":40,"items":[{"number":1},{"number":2}]}`)}, blocks.KindTable,
			func(t *testing.T, b blocks.Block) {
				if b.Total != 40 || !b.Truncated || len(b.Rows) != 2 {
					t.Errorf("total %d truncated %v rows %d", b.Total, b.Truncated, len(b.Rows))
				}
			}},
		{"text that is JSON → table", textResult(`[{"a":1}]`), blocks.KindTable, nil},
		{"plain text → text", textResult("hello\n\nworld"), blocks.KindText,
			func(t *testing.T, b blocks.Block) {
				if b.MD == nil || len(b.MD.C) != 2 || b.MD.C[1].C[0].Text != "world" {
					t.Errorf("md = %+v", b.MD)
				}
			}},
		{"isError → bad status", CallResult{IsError: true, Content: []Content{{Type: "text", Text: "not found\nno repo x"}}}, blocks.KindStatus,
			func(t *testing.T, b blocks.Block) {
				if b.State != blocks.ToneBad || b.Items[0].Title != "not found" || b.Items[0].Sub != "no repo x" {
					t.Errorf("status = %+v", b)
				}
			}},
		{"links → list with refs", CallResult{Content: []Content{{Type: "resource_link", URI: "repo://a/1", Name: "Issue 1"}, {Type: "resource_link", URI: "repo://a/2"}}}, blocks.KindList,
			func(t *testing.T, b blocks.Block) {
				if b.Items[0].Title != "Issue 1" || b.Items[0].Ref == nil || b.Items[0].Ref.Kind != "resource" || b.Items[0].Ref.ID != "repo://a/1" || b.Items[1].Title != "repo://a/2" {
					t.Errorf("items = %+v", b.Items)
				}
			}},
		{"image → list, stays on the laptop", CallResult{Content: []Content{{Type: "image", MimeType: "image/png", Data: strings.Repeat("QUJD", 1024)}}}, blocks.KindList,
			func(t *testing.T, b blocks.Block) {
				if !strings.Contains(b.Items[0].Sub, "open on the laptop") || b.Items[0].Ref != nil {
					t.Errorf("item = %+v", b.Items[0])
				}
			}},
		{"text with a link → text", CallResult{Content: []Content{{Type: "text", Text: "see"}, {Type: "resource_link", URI: "repo://x", Name: "x"}}}, blocks.KindText,
			func(t *testing.T, b blocks.Block) {
				raw, _ := json.Marshal(b.MD)
				if !strings.Contains(string(raw), "repo://x") {
					t.Errorf("md = %s", raw)
				}
			}},
		{"json resource → detail", CallResult{Content: []Content{{Type: "resource", Resource: &ResourceContents{URI: "x://1", MimeType: "application/json", Text: `{"a":"b"}`}}}}, blocks.KindDetail, nil},
		{"mixed array → json", CallResult{StructuredContent: json.RawMessage(`[{"a":1}, 2]`)}, blocks.KindJSON,
			func(t *testing.T, b blocks.Block) {
				if !json.Valid([]byte(b.JSON)) {
					t.Errorf("json = %q", b.JSON)
				}
			}},
		{"no content → ok status", CallResult{}, blocks.KindStatus,
			func(t *testing.T, b blocks.Block) {
				if b.State != blocks.ToneOK {
					t.Errorf("state = %q", b.State)
				}
			}},
		{"user-only text → for you", CallResult{Content: []Content{{Type: "text", Text: "model part"}, {Type: "text", Text: "for you only", Annotations: &ContentAnnotations{Audience: user}}}}, blocks.KindText,
			func(t *testing.T, b blocks.Block) {
				raw, _ := json.Marshal(b.MD)
				if b.ForYou != "for you only" || strings.Contains(string(raw), "for you only") {
					t.Errorf("for_you %q md %s", b.ForYou, raw)
				}
			}},
		{"scalar → text", CallResult{StructuredContent: json.RawMessage(`17`)}, blocks.KindText, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := BlockOf("gh", Tool{Name: "list"}, c.res, BlockOptions{})
			if err := b.Validate(); err != nil {
				t.Fatalf("invalid block: %v (%+v)", err, b)
			}
			if b.Kind != c.kind {
				t.Fatalf("kind = %q, want %q (%+v)", b.Kind, c.kind, b)
			}
			if b.Source != "mcp.gh.list" || b.Title != "gh · list" {
				t.Errorf("source %q title %q", b.Source, b.Title)
			}
			if c.chk != nil {
				c.chk(t, b)
			}
		})
	}
}

// Secrets the laptop knows never reach the phone, in any cell or field;
// escape sequences are cleaned and newlines leave the cells.
func TestBlockOfRedactsAndCleans(t *testing.T) {
	tok := "ghp_" + strings.Repeat("a", 36)
	b := BlockOf("gh", Tool{Name: "x"}, CallResult{StructuredContent: json.RawMessage(`[{"token":"` + tok + `","note":"a\nb\u001b[31m"}]`)},
		BlockOptions{Clean: func(s string) string { return strings.ReplaceAll(s, "\x1b[31m", "") }})
	raw, _ := json.Marshal(b)
	if strings.Contains(string(raw), tok) {
		t.Errorf("token leaked: %s", raw)
	}
	if b.Rows[0][1] != "a b" {
		t.Errorf("cell = %q", b.Rows[0][1])
	}
}

// Caps: many rows and wide objects are cut and marked; huge text and a
// huge JSON value end as a valid block.
func TestBlockOfCaps(t *testing.T) {
	var rows []string
	for i := 0; i < 500; i++ {
		rows = append(rows, `{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"j":10,"k":11,"l":12,"m":13,"n":14}`)
	}
	b := blockOfJSON(t, "["+strings.Join(rows, ",")+"]")
	if b.Validate() != nil || len(b.Rows) != blocks.MaxRows || len(b.Columns) != blocks.MaxColumns || !b.Truncated || b.Total != 500 {
		t.Errorf("rows %d cols %d truncated %v total %d", len(b.Rows), len(b.Columns), b.Truncated, b.Total)
	}
	long := strings.Repeat("<word> ", 20000)
	if b := BlockOf("gh", Tool{Name: "x"}, textResult(long), BlockOptions{}); b.Validate() != nil {
		t.Errorf("long text: %v", b.Validate())
	}
	nested := `[{"a":1},` + strings.Repeat(`"`+strings.Repeat("z", 1000)+`",`, 100) + `2]`
	if b := blockOfJSON(t, nested); b.Validate() != nil || b.Kind != blocks.KindText || b.ForYou != laptopNote {
		t.Errorf("big json = %+v", b)
	}
	// A bad column name and a long id are made safe.
	b = BlockOf("gh", Tool{Name: "x"}, CallResult{StructuredContent: json.RawMessage(`[{"a b/c":1,"a b/c ":2}]`)}, BlockOptions{ID: strings.Repeat("x y", 50)})
	if err := b.Validate(); err != nil || b.Columns[0].ID == b.Columns[1].ID {
		t.Errorf("ids: %v %+v", err, b.Columns)
	}
}

// Golden: two fake-server results as the phone gets them.
func TestBlockOfGolden(t *testing.T) {
	cases := map[string]CallResult{
		"structured": {Content: []Content{}, StructuredContent: json.RawMessage(`{"n":42}`)},
		"fails":      {IsError: true, Content: []Content{{Type: "text", Text: "it broke"}}},
	}
	for name, res := range cases {
		got, _ := json.Marshal(BlockOf("fake", Tool{Name: name}, res, BlockOptions{}))
		want, err := os.ReadFile(filepath.Join("testdata", "blocks", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var g, w any
		_ = json.Unmarshal(got, &g)
		if err := json.Unmarshal(want, &w); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("%s:\n got %s\nwant %s", name, got, want)
		}
	}
}

// e2e: the fake MCP server's results through the adapter's View hook:
// the model gets the text, the phone a typed block.
func TestBlockOfFakeServer(t *testing.T) {
	p := startFake(t, nil, Options{})
	call := func(ctx context.Context, server, tool string, args map[string]any) (CallResult, error) {
		return p.Client.CallTool(ctx, tool, args)
	}
	ro := ann(bp(true), nil)
	tools := AITools(Server{Name: "fake"}, []Tool{{Name: "structured", Annotations: ro}, {Name: "fails", Annotations: ro},
		{Name: "mixed", Annotations: ro}, {Name: "image", Annotations: ro}}, call, AdapterOptions{TrustAnnotations: true})
	want := map[string]string{"structured": blocks.KindDetail, "fails": blocks.KindStatus, "mixed": blocks.KindText, "image": blocks.KindList}
	for _, tl := range tools {
		name := strings.TrimPrefix(tl.Display, "mcp.fake.")
		out, err := tl.Run(context.Background(), map[string]any{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tl.View == nil {
			t.Fatalf("%s: no View", name)
		}
		b := tl.View(nil, out)
		if b == nil || b.Kind != want[name] || b.Validate() != nil {
			t.Errorf("%s: block %+v", name, b)
		}
		if again := tl.View(nil, out); again != nil {
			t.Errorf("%s: the result was kept after its View", name)
		}
	}
}

// A View for a text the adapter never returned has no block, and the
// kept results stay few.
func TestResultViewsCap(t *testing.T) {
	v := &resultViews{}
	for i := 0; i < 20; i++ {
		v.put(strings.Repeat("x", i), CallResult{})
	}
	if len(v.res) != maxResultViews || len(v.keys) != maxResultViews {
		t.Errorf("kept %d / %d", len(v.res), len(v.keys))
	}
	if _, ok := v.take("nope"); ok {
		t.Error("took an unknown text")
	}
}

func FuzzBlockOf(f *testing.F) {
	for _, s := range []string{`[{"a":1}]`, `{"items":[{"x":"y"}],"total":3}`, `["a"]`, `{"a":{"b":[1]}}`, `"x"`, `[[1]]`, `{"":""}`} {
		f.Add(s, "text", false)
	}
	f.Fuzz(func(t *testing.T, s, text string, isErr bool) {
		res := CallResult{IsError: isErr, Content: []Content{{Type: "text", Text: text}}}
		if json.Valid([]byte(s)) {
			res.StructuredContent = json.RawMessage(s)
		}
		b := BlockOf("srv", Tool{Name: "t"}, res, BlockOptions{})
		if err := b.Validate(); err != nil {
			t.Fatalf("invalid block for %q / %q: %v", s, text, err)
		}
	})
}

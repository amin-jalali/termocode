package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

func covBlock(t *testing.T, res CallResult, o BlockOptions) blocks.Block {
	t.Helper()
	b := BlockOf("srv", Tool{Name: "t"}, res, o)
	if err := b.Validate(); err != nil {
		t.Fatalf("block does not pass the caps: %v", err)
	}
	return b
}

func TestCovBlockOfContentShapes(t *testing.T) {
	// links and media only: a list; a link carries a resource ref, media stay on the laptop
	b := covBlock(t, CallResult{Content: []Content{
		{Type: "resource_link", Name: "Doc", URI: "file:///a"},
		{Type: "resource_link", URI: "file:///b"},
		{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("0123456789"))},
		{Type: "weird"},
	}}, BlockOptions{})
	if b.Kind != blocks.KindList || len(b.Items) != 4 {
		t.Fatalf("kind %s items %+v", b.Kind, b.Items)
	}
	if b.Items[0].Title != "Doc" || b.Items[0].Ref == nil || b.Items[1].Title != "file:///b" {
		t.Errorf("links: %+v", b.Items[:2])
	}
	if !strings.Contains(b.Items[2].Sub, "open on the laptop") || b.Items[2].Ref != nil {
		t.Errorf("media item: %+v", b.Items[2])
	}
	if !strings.Contains(b.Items[3].Title, "weird") {
		t.Errorf("unknown content: %+v", b.Items[3])
	}

	// more media than the cap: cut and flagged
	var many []Content
	for i := 0; i < blocks.MaxItems+5; i++ {
		many = append(many, Content{Type: "audio", MimeType: "audio/wav", Data: "AAAA"})
	}
	b = covBlock(t, CallResult{Content: many}, BlockOptions{})
	if len(b.Items) != blocks.MaxItems || !b.Truncated || b.Total != blocks.MaxItems+5 {
		t.Errorf("items %d truncated %v total %d", len(b.Items), b.Truncated, b.Total)
	}

	// text with a link and media under it
	b = covBlock(t, CallResult{Content: []Content{
		{Type: "text", Text: "hello"},
		{Type: "resource_link", URI: "file:///x"},
		{Type: "image", MimeType: "image/png", Data: "AAAA"},
		{Type: "resource", Resource: &ResourceContents{URI: "file:///r"}},
		{Type: "mystery"},
	}}, BlockOptions{})
	if b.Kind != blocks.KindText || b.MD == nil {
		t.Fatalf("kind %s", b.Kind)
	}
	got, _ := json.Marshal(b.MD)
	for _, want := range []string{"hello", "Link: file:///x", "image image/png", "resource file:///r", "[mystery"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("text block lacks %q: %s", want, got)
		}
	}
	if (blockMaker{}).contentLine(Content{Type: "resource_link", Name: "N", URI: "u"}) != "Link N: u" {
		t.Error("named link line")
	}
}

func TestCovBlockOfEmptyAndFailed(t *testing.T) {
	// no text, no media: done
	b := covBlock(t, CallResult{}, BlockOptions{})
	if b.Kind != blocks.KindStatus || b.State != blocks.ToneOK {
		t.Errorf("empty result: %s %s", b.Kind, b.State)
	}
	// error without words
	b = covBlock(t, CallResult{IsError: true, Content: []Content{{Type: "text", Text: "  "}}}, BlockOptions{})
	if b.Kind != blocks.KindStatus || b.State != blocks.ToneBad || b.Items[0].Title != "The tool failed" {
		t.Errorf("failed: %+v", b)
	}
	// error with a message: first line is the title, the rest the sub line
	b = covBlock(t, CallResult{IsError: true, Content: []Content{{Type: "text", Text: "boom\nbecause of x"}}}, BlockOptions{})
	if b.Items[0].Title != "boom" || b.Items[0].Sub != "because of x" {
		t.Errorf("failed items: %+v", b.Items)
	}
	// a blank title falls back
	b = BlockOf("", Tool{}, CallResult{Content: []Content{{Type: "text", Text: "x"}}}, BlockOptions{})
	if b.Title == "" {
		t.Error("a block always has a title")
	}
	// a Markdown renderer is used when given
	md := &blocks.MdNode{K: "doc", C: []blocks.MdNode{{K: "p", C: []blocks.MdNode{{K: "text", Text: "RENDERED"}}}}}
	b = covBlock(t, textResult("plain"), BlockOptions{Markdown: func(string) *blocks.MdNode { return md }})
	if b.MD != md {
		t.Error("the Markdown option is ignored")
	}
}

func TestCovBlockOfJSONShapes(t *testing.T) {
	// an object with no keys
	b := blockOfJSON(t, `{}`)
	if b.Kind != blocks.KindText {
		t.Errorf("{} kind %s", b.Kind)
	}
	// an array of empty objects
	b = blockOfJSON(t, `[{},{}]`)
	if b.Kind != blocks.KindText {
		t.Errorf("[{},{}] kind %s", b.Kind)
	}
	// mixed array: raw json
	b = blockOfJSON(t, `[{"a":1}, 2]`)
	if b.Kind != blocks.KindJSON {
		t.Errorf("mixed kind %s", b.Kind)
	}
	// a scalar
	b = blockOfJSON(t, `42`)
	if b.Kind != blocks.KindText {
		t.Errorf("scalar kind %s", b.Kind)
	}
	// duplicate and odd column names get unique ids
	b = blockOfJSON(t, `[{"a b":1,"a_b":2,"_":3,"":4}]`)
	if b.Kind != blocks.KindTable {
		t.Fatalf("kind %s", b.Kind)
	}
	seen := map[string]bool{}
	for _, c := range b.Columns {
		if seen[c.ID] {
			t.Errorf("duplicate column id %q", c.ID)
		}
		seen[c.ID] = true
	}
	// a detail with status fields is coloured, and long objects are cut
	b = blockOfJSON(t, `{"status":"failed","state":"running","result":"ok","health":"degraded","conclusion":"open","count":2,"obj":{"x":1},"flag":true,"other":"failed"}`)
	if b.Kind != blocks.KindDetail {
		t.Fatalf("kind %s", b.Kind)
	}
	tones := map[string]string{}
	for _, f := range b.Fields {
		tones[f.Label] = f.Tone
	}
	want := map[string]string{"status": blocks.ToneBad, "state": blocks.ToneBusy, "result": blocks.ToneOK, "health": blocks.ToneWarn, "conclusion": blocks.ToneInfo, "other": ""}
	for k, v := range want {
		if tones[k] != v {
			t.Errorf("tone of %s = %q, want %q", k, tones[k], v)
		}
	}
	var sb strings.Builder
	sb.WriteString("{")
	for i := 0; i < blocks.MaxFields+3; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"k%d":%d`, i, i)
	}
	sb.WriteString("}")
	b = blockOfJSON(t, sb.String())
	if len(b.Fields) != blocks.MaxFields || !b.Truncated {
		t.Errorf("fields %d truncated %v", len(b.Fields), b.Truncated)
	}
	// an object with a list of objects and a total
	b = blockOfJSON(t, `{"total_count":5,"items":[{"id":"1"},{"id":"2"}]}`)
	if b.Kind != blocks.KindTable || b.Total != 5 || !b.Truncated {
		t.Errorf("wrapped list: %s total %d truncated %v", b.Kind, b.Total, b.Truncated)
	}
	// more scalar items than the cap
	var arr []string
	for i := 0; i < blocks.MaxItems+2; i++ {
		arr = append(arr, fmt.Sprint(i))
	}
	raw, _ := json.Marshal(arr)
	b = blockOfJSON(t, string(raw))
	if len(b.Items) != blocks.MaxItems || !b.Truncated {
		t.Errorf("items %d truncated %v", len(b.Items), b.Truncated)
	}
}

func TestCovBlockOfFitsTooBigResults(t *testing.T) {
	// a table that is too big as a whole is cut in rows until it fits
	var rows []map[string]string
	for i := 0; i < blocks.MaxRows; i++ {
		rows = append(rows, map[string]string{"a": strings.Repeat("x", 190), "b": strings.Repeat("y", 190), "c": strings.Repeat("z", 190)})
	}
	raw, _ := json.Marshal(rows)
	b := blockOfJSON(t, string(raw))
	if b.Kind != blocks.KindTable || !b.Truncated || len(b.Rows) >= blocks.MaxRows {
		t.Fatalf("kind %s truncated %v rows %d", b.Kind, b.Truncated, len(b.Rows))
	}
	// json that is too big for the phone points to the laptop
	var bigArr []any
	for i := 0; i < 1500; i++ {
		bigArr = append(bigArr, fmt.Sprintf("value number %d", i))
	}
	raw, _ = json.Marshal([]any{map[string]any{"a": bigArr}, 1})
	b = blockOfJSON(t, string(raw))
	if b.Kind != blocks.KindText || b.ForYou == "" {
		t.Errorf("too big: kind %s forYou %q", b.Kind, b.ForYou)
	}
	// text longer than a text block may hold is cut on a rune boundary
	long := strings.Repeat("é", maxBlockText)
	b = covBlock(t, textResult(long), BlockOptions{})
	if b.Kind != blocks.KindText {
		t.Errorf("kind %s", b.Kind)
	}
	if got := (blockMaker{}).long(long); !strings.HasSuffix(got, "\n…") {
		t.Error("a long text ends with the cut mark")
	}
}

func TestCovBlockHelpers(t *testing.T) {
	if cutRunes("héllo wörld", 5) != "héll…" || cutRunes("abc", 5) != "abc" {
		t.Error("cutRunes")
	}
	if cellText(nil) != "" || cellText(json.RawMessage(`null`)) != "" {
		t.Error("null cell")
	}
	if cellText(json.RawMessage(`{ "a" : 1 }`)) != `{"a":1}` {
		t.Error("nested cell is compact")
	}
	if cellText(json.RawMessage(`{bad`)) != `{bad` {
		t.Error("bad nested cell is kept as sent")
	}
	if cellText(json.RawMessage(`tru`)) != "" {
		t.Error("an unreadable scalar is empty")
	}
	if cellText(json.RawMessage(`false`)) != "no" || cellText(json.RawMessage(`true`)) != "yes" {
		t.Error("bool cell")
	}
	if cellText(json.RawMessage(`1.50`)) != "1.5" {
		t.Errorf("number cell %q", cellText(json.RawMessage(`1.50`)))
	}
	if scalarText([]int{1}) != "[1]" || scalarText(nil) != "" {
		t.Error("scalarText")
	}
	for _, c := range []struct{ k, v, want string }{
		{"status", "Success", blocks.ToneOK}, {"state", "cancelled", blocks.ToneBad}, {"result", "Queued", blocks.ToneBusy},
		{"health", "skipped", blocks.ToneWarn}, {"status", "draft", blocks.ToneInfo}, {"status", "zzz", ""}, {"name", "failed", ""},
	} {
		if got := toneOf(c.k, c.v); got != c.want {
			t.Errorf("toneOf(%s,%s) = %q want %q", c.k, c.v, got, c.want)
		}
	}
	if kindOf(nil) != 0 || kindOf(json.RawMessage(`null`)) != 0 || kindOf(json.RawMessage(` true`)) != 'b' || kindOf(json.RawMessage(`3`)) != 'n' {
		t.Error("kindOf")
	}
	deep := any("x")
	for i := 0; i < 40; i++ {
		deep = []any{deep}
	}
	out := (blockMaker{}).redactValue(deep, 0)
	for i := 0; i < 33; i++ {
		out = out.([]any)[0]
	}
	if out != "…" {
		t.Errorf("deep values are cut with a mark, got %v", out)
	}
	// a block without a usable id falls back to a plain laptop note
	lo := (blockMaker{}).laptopOnly(blocks.Block{ID: "bad id!", Title: ""})
	if lo.Validate() != nil || lo.ForYou == "" {
		t.Errorf("laptopOnly: %+v", lo)
	}
}

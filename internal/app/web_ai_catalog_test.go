package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (MOB-13, "What can I do?"): ai.catalog lists the AI's tools as
// features people know, each with tool counts, prompts to try and the
// view it opens, plus every MCP server from mcp.json, without starting
// one (cached lists only).
func TestWebAICatalogListsFeaturesAndServers(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	webTestHub(t, m)
	c := webTestSub(t, m)
	f, cmd := webTestCall(t, m, c, webproto.OpAICatalog, `{}`)
	if f["t"] != "res" || cmd != nil {
		t.Fatalf("catalog: %v", f)
	}
	var out webproto.AICatalog
	webTestDecode(t, f["data"], &out)
	byID := map[string]webproto.CatalogFeature{}
	for _, x := range out.Features {
		byID[x.ID] = x
		if x.Title == "" || x.Hint == "" || len(x.Try) == 0 {
			t.Errorf("feature %q is not complete: %+v", x.ID, x)
		}
	}
	if g := byID["git"]; g.Tools == 0 || g.View != "git.status" || g.Asks == 0 {
		t.Errorf("git: %+v", g)
	}
	if byID["db"].Tools == 0 || byID["more"].Tools == 0 {
		t.Errorf("tools not counted: %+v", out.Features)
	}
	if len(out.Servers) != 1 || out.Servers[0].Name != "fake" {
		t.Fatalf("servers: %+v", out.Servers)
	}
	if n := m.aiMCP().Running(); n != 0 {
		t.Errorf("the catalog started %d servers", n)
	}
}

// Spec (MOB-13, the MCP chip): feature "mcp" lists only the servers, with
// each known tool's gate and the composer text a tap puts in the box; a
// query keeps the matching tools.
func TestWebAICatalogMCPChip(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	if err := m.aiMCP().Connect(context.Background(), nil, "fake"); err != nil {
		t.Fatal(err)
	}
	webTestHub(t, m)
	c := webTestSub(t, m)
	f, _ := webTestCall(t, m, c, webproto.OpAICatalog, `{"feature":"mcp","query":"echo"}`)
	var out webproto.AICatalog
	webTestDecode(t, f["data"], &out)
	if len(out.Features) != 0 || len(out.Servers) != 1 {
		t.Fatalf("mcp only: %+v", out)
	}
	s := out.Servers[0]
	if !s.Known || len(s.Tools) != 1 || s.Tools[0].Name != "echo" {
		t.Fatalf("tools: %+v", s.Tools)
	}
	if e := s.Tools[0]; e.Gate != "runs" || e.Text != "Use mcp.fake.echo to " {
		t.Errorf("echo: %+v", e)
	}
	f, _ = webTestCall(t, m, c, webproto.OpAICatalog, `{"feature":"mcp","query":"delete"}`)
	webTestDecode(t, f["data"], &out)
	if len(out.Servers) != 1 || len(out.Servers[0].Tools) != 1 || out.Servers[0].Tools[0].Gate != "asks" {
		t.Errorf("delete_all asks first: %+v", out.Servers)
	}
}

// Spec (MOB-13, pinned tracker blocks): tracker_pin always asks; it pins
// only a tool that reads and runs at once, never with a key in its
// arguments; the phone lists the pin, re-runs it on the laptop (rows)
// and unpins it.
func TestTrackerPinRunAndUnpin(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	mgr := m.aiMCP()
	if err := mgr.Connect(context.Background(), nil, "fake"); err != nil {
		t.Fatal(err)
	}
	tool := aiTrackerTools("")[0]
	if tool.Name != "tracker_pin" || !tool.Mutating || tool.Class != "" {
		t.Fatalf("tracker_pin must always ask: %+v", tool)
	}
	for _, bad := range []map[string]any{
		{"server": "fake", "tool": "delete_all", "title": "x"},
		{"server": "fake", "tool": "echo", "title": ""},
		{"server": "fake", "tool": "echo", "title": strings.Repeat("x", 61)},
		{"server": "fake", "tool": "echo", "title": "x", "args": map[string]any{"api_key": "x"}},
		{"server": "fake", "tool": "echo", "title": "x", "refresh_seconds": float64(5)},
		{"server": "nope", "tool": "echo", "title": "x"},
	} {
		if err := tool.Validate(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	args := map[string]any{"server": "fake", "tool": "echo", "title": "Echo hi", "args": map[string]any{"text": "hi"}}
	if err := tool.Validate(args); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	p, _ := trackerPinsPath()
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("pins file: %v %v", fi, err)
	}

	webTestHub(t, m)
	c := webTestSub(t, m)
	f, _ := webTestCall(t, m, c, webproto.OpAITrackerPins, `{}`)
	var pins webproto.TrackerPins
	webTestDecode(t, f["data"], &pins)
	if len(pins.Pins) != 1 || !pins.Pins[0].Runs || pins.Pins[0].Title != "Echo hi" {
		t.Fatalf("pins: %+v", pins)
	}
	id := pins.Pins[0].ID
	call, work := webTestCallLater(t, m, c, webproto.OpAITrackerRun, `{"id":"`+id+`"}`)
	if work == nil {
		t.Fatal("ran on the UI thread")
	}
	work()
	f = webTestAnswer(t, c, call)
	var b webproto.TrackerBlock
	webTestDecode(t, f["data"], &b)
	if b.Error != "" || len(b.Rows) != 1 || !strings.Contains(b.Rows[0].Title, "echo: hi") {
		t.Fatalf("block: %+v", b)
	}
	f, _ = webTestCall(t, m, c, webproto.OpAITrackerUnpin, `{"id":"`+id+`"}`)
	webTestDecode(t, f["data"], &pins)
	if len(pins.Pins) != 0 {
		t.Fatalf("not unpinned: %+v", pins)
	}
	if f, _ = webTestCall(t, m, c, webproto.OpAITrackerRun, `{"id":"`+id+`"}`); f["t"] != "err" {
		t.Errorf("a gone pin ran: %v", f)
	}
}

// Spec (MOB-13): a tracker result becomes rows: the first list of items
// in the structured content (title, a hint of state/assignee, the id as
// ref), else the lines of the text; capped, with secrets redacted.
func TestTrackerRowsFromResults(t *testing.T) {
	var b webproto.TrackerBlock
	trackerRows(&b, mcp.CallResult{StructuredContent: json.RawMessage(`{"total":2,"issues":[{"identifier":"ENG-1","title":"Fix login","state":{"name":"Todo"},"assignee":{"name":"Sam"}},{"id":7,"name":"Docs"}]}`)})
	if len(b.Rows) != 2 || b.Rows[0] != (webproto.TrackerRow{Title: "Fix login", Hint: "Todo · Sam", Ref: "ENG-1"}) || b.Rows[1].Title != "Docs" || b.Rows[1].Ref != "7" {
		t.Fatalf("structured: %+v", b.Rows)
	}
	b = webproto.TrackerBlock{}
	lines := strings.Repeat("item\n", webproto.MaxTrackerRows+5)
	trackerRows(&b, mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: lines}}})
	if len(b.Rows) != webproto.MaxTrackerRows || !b.Truncated {
		t.Fatalf("text rows: %d %v", len(b.Rows), b.Truncated)
	}
	b = webproto.TrackerBlock{}
	trackerRows(&b, mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: `[{"title":"A"},{"title":"B"}]`}}})
	if len(b.Rows) != 2 || b.Rows[1].Title != "B" {
		t.Fatalf("json text: %+v", b.Rows)
	}
	b = webproto.TrackerBlock{}
	trackerRows(&b, mcp.CallResult{})
	if b.Text == "" {
		t.Error("an empty result shows nothing")
	}
}

func webTestDecode(t *testing.T, data any, out any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
}

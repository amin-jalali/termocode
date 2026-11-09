package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// AI view tools (MOB-70): view_catalog and view_save (view_show is MOBUX-17's). The
// model builds a view spec, the laptop checks it against the registry
// and runs it; the AI never sees more than the phone would.

func aiViewTestDefs() []webproto.ViewDef {
	return []webproto.ViewDef{
		{
			ID: "ci.prs", Feature: "ci", Title: "Pull requests", Source: webproto.ViewSource{Topic: "ci"},
			Fields: []webproto.ViewField{
				{ID: "number", Label: "#", Kind: webproto.FieldNum, Sortable: true, Default: true},
				{ID: "title", Label: "Title", Kind: webproto.FieldText, Filterable: true, Default: true},
				{ID: "checks", Label: "Checks", Kind: webproto.FieldState, Filterable: true, Default: true},
				{ID: "age", Label: "Age", Kind: webproto.FieldTime, Sortable: true, Filterable: true},
			},
			Layouts: []string{blocks.KindTable, blocks.KindList}, Role: webproto.RoleViewer, Live: true,
		},
		{
			ID: "db.rows", Feature: "db", Title: "Rows", Source: webproto.ViewSource{Tool: "db_query"},
			Params:  []webproto.ViewParam{{ID: "table", Label: "Table", Kind: webproto.ParamTable, Required: true}},
			Fields:  []webproto.ViewField{{ID: "status", Label: "Status", Kind: webproto.FieldText, Filterable: true}},
			Layouts: []string{blocks.KindTable}, Role: webproto.RoleViewer,
		},
		{
			ID: "mcp.github.list_issues", Feature: "mcp", Title: "Issues",
			Source:  webproto.ViewSource{MCPServer: "github", MCPTool: "list_issues"},
			Fields:  []webproto.ViewField{{ID: "title", Label: "Title", Kind: webproto.FieldText}},
			Layouts: []string{blocks.KindTable}, Role: webproto.RoleViewer,
		},
	}
}

func TestAIViewToolsAreListed(t *testing.T) {
	tools := aiViewTools(nil)
	got := map[string]bool{}
	for _, tl := range tools {
		got[tl.Name] = true
		switch tl.Name {
		case "view_show":
			t.Error("view_show is MOBUX-17's tool (ai_view_show_tool.go); one view_show only")
		case "view_catalog":
			if !tl.ReadOnly || tl.Mutating {
				t.Errorf("%s must be read-only", tl.Name)
			}
		case "view_save":
			// AI parity: the phone's "Save as view" has no card (a name
			// form only), so view_save does not ask either.
			if tl.Mutating || tl.ReadOnly || tl.Gate != nil {
				t.Errorf("view_save: mutating %v read-only %v; the GUI saves a view with no card", tl.Mutating, tl.ReadOnly)
			}
		}
	}
	for _, n := range []string{"view_catalog", "view_save"} {
		if !got[n] {
			t.Errorf("no %s tool", n)
		}
	}
}

func TestAIViewCatalogText(t *testing.T) {
	defs := aiViewTestDefs()
	all := aiViewCatalogText(defs, "", "")
	for _, want := range []string{"ci.prs", "number:num", "title:text", "sort", "filter", "table", "db.rows", "table*", "mcp.github.list_issues"} {
		if !strings.Contains(all, want) {
			t.Errorf("catalog has no %q:\n%s", want, all)
		}
	}
	if got := aiViewCatalogText(defs, "db", ""); strings.Contains(got, "ci.prs") || !strings.Contains(got, "db.rows") {
		t.Errorf("feature filter: %s", got)
	}
	if got := aiViewCatalogText(defs, "", "pull"); !strings.Contains(got, "ci.prs") || strings.Contains(got, "db.rows") {
		t.Errorf("query filter: %s", got)
	}
	if got := aiViewCatalogText(defs, "", "nothing-like-this"); !strings.Contains(got, "No view") {
		t.Errorf("empty result: %q", got)
	}
}

// The catalog stays within the model's budget (≤ 4k tokens ≈ 16 KB): a
// long registry is cut with a hint to filter.
func TestAIViewCatalogCapped(t *testing.T) {
	var defs []webproto.ViewDef
	base := aiViewTestDefs()[0]
	for i := 0; i < 400; i++ {
		d := base
		d.ID = "ci.prs" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + strings.Repeat("y", i/26)
		defs = append(defs, d)
	}
	got := aiViewCatalogText(defs, "", "")
	if len(got) > aiViewCatalogMax {
		t.Errorf("catalog is %d bytes, max %d", len(got), aiViewCatalogMax)
	}
	if !strings.Contains(got, "feature") {
		t.Errorf("a cut catalog says how to filter: %s", got[len(got)-200:])
	}
}

func TestAIViewSpecArg(t *testing.T) {
	obj := map[string]any{"spec": map[string]any{"view": "ci.prs", "fields": []any{"number", "title"},
		"sort": []any{map[string]any{"field": "age", "desc": true}}, "layout": "table"}}
	s, err := aiViewSpecArg(obj)
	if err != nil || s.View != "ci.prs" || len(s.Fields) != 2 || len(s.Sort) != 1 || !s.Sort[0].Desc {
		t.Fatalf("object spec: %+v %v", s, err)
	}
	str := map[string]any{"spec": `{"view":"ci.prs","limit":5}`}
	if s, err := aiViewSpecArg(str); err != nil || s.Limit != 5 {
		t.Fatalf("string spec: %+v %v", s, err)
	}
	for _, bad := range []map[string]any{{}, {"spec": 3}, {"spec": `{"view":"ci.prs","sql":"select 1"}`}, {"spec": "not json"}} {
		if _, err := aiViewSpecArg(bad); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

// view_show runs only what the phone could run by itself: a topic view.
// A view of an AI tool or an MCP tool is refused with that tool's name.
func TestAIViewShowSourceRule(t *testing.T) {
	defs := aiViewTestDefs()
	if _, err := aiViewShowDef(defs, blocks.ViewSpec{View: "ci.prs"}); err != nil {
		t.Errorf("topic view refused: %v", err)
	}
	_, err := aiViewShowDef(defs, blocks.ViewSpec{View: "db.rows", Args: map[string]string{"table": "orders"}})
	if err == nil || !strings.Contains(err.Error(), "db_query") {
		t.Errorf("tool view: %v; want a refusal naming db_query", err)
	}
	_, err = aiViewShowDef(defs, blocks.ViewSpec{View: "mcp.github.list_issues"})
	if err == nil || !strings.Contains(err.Error(), "mcp.github.list_issues") {
		t.Errorf("MCP view: %v; want a refusal naming the MCP tool", err)
	}
	_, err = aiViewShowDef(defs, blocks.ViewSpec{View: "ci.pr"})
	if err == nil || !strings.Contains(err.Error(), "ci.prs") {
		t.Errorf("unknown view: %v; want the close ids", err)
	}
}

// AI never-tests: a spec carries no SQL, no path outside the workspace
// and no URL, whatever the model writes.
func TestAIViewSpecNeverCarriesSQLPathOrURL(t *testing.T) {
	for _, v := range []string{"select * from users", "DROP TABLE x", "/etc/passwd", "~/.ssh/id_rsa", "../../x",
		"https://evil.example", "file:///x", "a; delete from t", "C:\\Windows"} {
		s := blocks.ViewSpec{View: "db.rows", Args: map[string]string{"table": v}}
		if err := aiViewSpecSafe(s); err == nil {
			t.Errorf("arg %q passed", v)
		}
		s = blocks.ViewSpec{View: "ci.prs", Filter: []blocks.ViewCond{{Field: "title", Op: "contains", Value: v}}}
		if err := aiViewSpecSafe(s); err == nil {
			t.Errorf("filter value %q passed", v)
		}
	}
	ok := blocks.ViewSpec{View: "ci.prs", Args: map[string]string{"table": "orders"},
		Filter: []blocks.ViewCond{{Field: "title", Op: "contains", Value: "login rate limit"}}}
	if err := aiViewSpecSafe(ok); err != nil {
		t.Errorf("plain spec refused: %v", err)
	}
}

// The model gets a short text of the block: title, total and at most 20
// rows, so it can talk about the data it showed.
func TestAIViewBlockText(t *testing.T) {
	b := blocks.Block{ID: "v-1", Kind: blocks.KindTable, Title: "Pull requests", Total: 30,
		Columns: []blocks.BlockColumn{{ID: "number", Label: "#"}, {ID: "title", Label: "Title"}}}
	for i := 0; i < 30; i++ {
		b.Rows = append(b.Rows, []string{strings.Repeat("1", 1+i%3), "row"})
	}
	got := aiViewBlockText(b)
	if !strings.Contains(got, "Pull requests") || !strings.Contains(got, "30") {
		t.Errorf("text has no title or total: %s", got)
	}
	if n := strings.Count(got, "row"); n != aiViewTextRows {
		t.Errorf("text has %d rows, want %d", n, aiViewTextRows)
	}
	l := blocks.Block{Kind: blocks.KindList, Title: "Inbox", Items: []blocks.BlockItem{{ID: "a", Title: "card: run go test", Badge: "CI ✗"}}}
	if got := aiViewBlockText(l); !strings.Contains(got, "card: run go test") || !strings.Contains(got, "CI ✗") {
		t.Errorf("list text: %s", got)
	}
}

func TestAIViewSaveAsks(t *testing.T) {
	for _, tl := range aiViewTools(nil) {
		if tl.Name != "view_save" {
			continue
		}
		args := map[string]any{"spec": map[string]any{"view": "ci.prs"}, "name": "PRs to review", "pin": true}
		h := tl.Human(args)
		if !strings.Contains(h, "PRs to review") || !strings.Contains(h, "pin") {
			t.Errorf("confirm text %q names no view or pin", h)
		}
		if tl.Validate == nil {
			t.Fatal("view_save has no Validate")
		}
		for _, bad := range []map[string]any{
			{"spec": map[string]any{"view": "ci.prs"}},
			{"spec": map[string]any{"view": "ci.prs"}, "name": strings.Repeat("n", 61)},
			{"name": "x"},
		} {
			if tl.Validate(bad) == nil {
				t.Errorf("view_save accepted %v", bad)
			}
		}
	}
}

func TestAIChatPromptPrefersViewShow(t *testing.T) {
	if !strings.Contains(aiChatSystemPrompt, "prefer view_show") {
		t.Error("the system prompt does not tell the model to prefer view_show for data")
	}
}

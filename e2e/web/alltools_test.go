//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The "All tools" menu (MOBUX-5, ☰): every built-in feature, every MCP
// server and every saved view in one searchable list; each row opens its
// view. On the phone page under More, against the fake hub: the page
// reads the laptop's view.catalog (MOBUX-15) when the menu opens.

// opViewCatalog is the catalog op of the view registry (plan 3.11.3).
const opViewCatalog = "view.catalog"

// toolsCatalog is a view.catalog result: built-in views, two MCP
// servers (github with a resource view and a tool that is not
// read-only), a listed server with no cached tools, and saved views.
var toolsCatalog = map[string]any{
	"views": []map[string]any{
		{"id": "git.branches", "feature": "git", "title": "Branches"},
		{"id": "pr.list", "feature": "ci", "title": "Pull requests"},
		{"id": "mcp.github.list_issues", "feature": "mcp", "server": "github", "tool": "list_issues", "title": "List issues", "run": true},
		{"id": "mcp.github.create_issue", "feature": "mcp", "server": "github", "tool": "create_issue", "title": "Create issue", "run": false},
		{"id": "mcp.github.resource", "feature": "mcp", "server": "github", "tool": "resource", "title": "Resources", "run": true},
		{"id": "mcp.linear.my_issues", "feature": "mcp", "server": "linear", "tool": "my_issues", "title": "My issues", "run": true},
	},
	"servers": []string{"github", "linear", "sentry"},
	"saved": []map[string]any{
		{"id": "s1", "name": "PRs to review", "spec": map[string]any{"view": "pr.list", "ws": "shop", "args": map[string]string{"state": "open"}}, "pinned": true},
		{"id": "s2", "name": "Failing CI", "spec": map[string]any{"view": "ci.checks"}},
	},
}

// openTools logs in, opens More › All tools and waits for its catalog
// request.
func openTools(t *testing.T) (*Page, *HubConn, *webproto.Req) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	c := p.Login("c-tools")
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="tools"]')`)
	p.WaitJS(`location.hash === "#/more/tools"`)
	r := expectOp(t, c, opViewCatalog, "")
	return p, c, r
}

// toolIDs lists the rows of group g (features, mcp, saved) in order.
func toolIDs(p *Page, g string) []string {
	p.T.Helper()
	return Eval[[]string](p, `[...document.querySelectorAll('.tools [data-group="`+g+`"] .tools-row')].map((b) => b.dataset.tool)`)
}

// searchTools types q into the menu's search field.
func searchTools(p *Page, q string) {
	p.T.Helper()
	Eval[bool](p, `(() => { const i = document.querySelector(".tools-input"); i.value = `+jsString(q)+`; i.dispatchEvent(new Event("input", { bubbles: true })); return true; })()`)
}

func jsString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// The whole list: features in the registry's order, then the MCP
// servers (with their tool and resource counts), then the saved views
// with the pinned one marked; every row is 44 px or more.
func TestAllToolsListsFeaturesServersAndSavedViews(t *testing.T) {
	p, c, r := openTools(t)
	reply(t, c, r, toolsCatalog)
	p.WaitJS(`document.querySelector('.tools [data-group="saved"] .tools-row')`)
	f := toolIDs(p, "features")
	if len(f) < 18 || f[0] != "chat" || f[1] != "git" {
		t.Errorf("features %v, want every built-in feature, chat and git first", f)
	}
	for _, want := range []string{"runs", "workflows", "tasks", "db", "containers", "kube", "tests", "services", "search", "settings", "mcpserver"} {
		if !contains(f, want) {
			t.Errorf("features %v have no %s", f, want)
		}
	}
	if got := strings.Join(toolIDs(p, "mcp"), " "); got != "server:github server:linear server:sentry" {
		t.Errorf("servers %q, want github linear sentry", got)
	}
	if got := Eval[string](p, `document.querySelector('[data-tool="server:github"]').textContent`); !strings.Contains(got, "2 tools · 1 resources") {
		t.Errorf("github row reads %q, want 2 tools · 1 resources", got)
	}
	if got := strings.Join(toolIDs(p, "saved"), " "); got != "saved:s1 saved:s2" {
		t.Errorf("saved %q", got)
	}
	if got := Eval[string](p, `document.querySelector('[data-tool="saved:s1"]').textContent`); !strings.Contains(got, "📌") || !strings.Contains(got, "Pinned to Home") {
		t.Errorf("pinned saved view reads %q, want 📌 Pinned to Home", got)
	}
	if bad := Eval[[]string](p, smallTargetsJS(".tools-row, .tools-input")); len(bad) > 0 {
		t.Errorf("targets under 44 px: %v", bad)
	}
	p.noOverflow("all tools")
}

// One search over everything: a feature by its views' words, the
// features' own catalog views, MCP tools by name, saved views; nothing
// found says so.
func TestAllToolsSearch(t *testing.T) {
	p, c, r := openTools(t)
	reply(t, c, r, toolsCatalog)
	p.WaitJS(`document.querySelector('.tools [data-group="mcp"] .tools-row')`)

	searchTools(p, "branch")
	p.WaitJS(`!document.querySelector('[data-tool="runs"]')`)
	if got := strings.Join(toolIDs(p, "features"), " "); got != "git git.branches" {
		t.Errorf("branch finds %q, want the git feature and its branches view", got)
	}
	if n := Eval[int](p, `document.querySelectorAll('.tools [data-group="mcp"], .tools [data-group="saved"]').length`); n != 0 {
		t.Errorf("branch shows %d server or saved groups, want none", n)
	}

	searchTools(p, "issues")
	p.WaitJS(`document.querySelector('[data-tool="mcp.linear.my_issues"]')`)
	if got := strings.Join(toolIDs(p, "mcp"), " "); got != "mcp.github.list_issues mcp.linear.my_issues" {
		t.Errorf("issues finds %q, want the two issue tools", got)
	}
	if got := Eval[string](p, `document.querySelector('[data-tool="mcp.github.list_issues"]').textContent`); !strings.Contains(got, "github · List issues") {
		t.Errorf("tool row reads %q, want its server first", got)
	}

	searchTools(p, "review")
	p.WaitJS(`document.querySelector('[data-tool="saved:s1"]')`)
	if got := strings.Join(toolIDs(p, "saved"), " "); got != "saved:s1" {
		t.Errorf("review finds saved %q", got)
	}

	searchTools(p, "zzqx")
	p.WaitJS(`document.querySelector(".tools-nothing")`)
	if got := p.Text(".tools-nothing"); got != "No tool matches zzqx." {
		t.Errorf("nothing found reads %q", got)
	}
}

// Each row opens its view: a feature its phone screen, a saved view its
// spec in the Home, an MCP server its tools (Back returns), a tool that
// is not read-only says it opens as Ask the AI.
func TestAllToolsRowsOpenTheirView(t *testing.T) {
	p, c, r := openTools(t)
	reply(t, c, r, toolsCatalog)
	p.WaitJS(`document.querySelector('[data-tool="server:github"]')`)

	p.TapJS(`document.querySelector('[data-tool="server:github"]')`)
	p.WaitJS(`document.querySelector('.tools-server[data-server="github"]')`)
	ids := Eval[[]string](p, `[...document.querySelectorAll(".tools-server .tools-row")].map((b) => b.dataset.tool)`)
	if strings.Join(ids, " ") != "mcp.github.list_issues mcp.github.create_issue mcp.github.resource" {
		t.Errorf("github tools %v", ids)
	}
	if got := Eval[string](p, `document.querySelector('[data-tool="mcp.github.create_issue"]').textContent`); !strings.Contains(got, "✦") || !strings.Contains(got, "Opens as Ask the AI") {
		t.Errorf("a tool that changes things reads %q, want ✦ Opens as Ask the AI", got)
	}
	p.TapJS(`document.querySelector(".tools-back")`)
	p.WaitJS(`document.querySelector('[data-tool="saved:s1"]')`)

	p.TapJS(`document.querySelector('[data-tool="saved:s1"]')`)
	p.WaitJS(`location.hash.startsWith("#/home/view/")`)
	if got := Eval[string](p, "location.hash"); got != "#/home/view/pr.list?ws=shop&arg.state=open&saved=s1" {
		t.Errorf("saved view opens %q", got)
	}

	// The Home has no tab bar: back to More by its address.
	Eval[bool](p, `(location.hash = "#/more", true)`)
	p.TapJS(`document.querySelector('.more-link[data-sub="tools"]')`)
	r = expectOp(t, c, opViewCatalog, "")
	reply(t, c, r, toolsCatalog)
	p.WaitJS(`document.querySelector('[data-tool="runs"]')`)
	p.TapJS(`document.querySelector('[data-tool="runs"]')`)
	p.WaitJS(`location.hash === "#/more/runs"`)
}

// The laptop does not answer the catalog: the features still show and
// open; one calm line with Try again asks once more.
func TestAllToolsCatalogFailed(t *testing.T) {
	p, c, r := openTools(t)
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeNotFound, Msg: "Not in this test."})
	p.WaitJS(`document.querySelector(".tools-failed")`)
	if f := toolIDs(p, "features"); len(f) < 18 {
		t.Errorf("features %v while the catalog failed, want all of them", f)
	}
	if got := p.Text(".tools-failed .empty"); got != "The laptop did not send its MCP servers and saved views." {
		t.Errorf("failed line %q", got)
	}
	p.TapJS(`document.querySelector(".tools-failed button")`)
	r = expectOp(t, c, opViewCatalog, "")
	reply(t, c, r, toolsCatalog)
	p.WaitJS(`document.querySelector('[data-tool="server:github"]') && !document.querySelector(".tools-failed")`)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

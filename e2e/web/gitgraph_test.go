//go:build browser

package web

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop's commit graph: an editor tab with a drawn graph (the
// hub's lanes), branch and tag labels, date, author and hash; a click
// opens the commit's files, a file its change side by side; the context
// menu starts an interactive rebase from a commit.

// waitReq reads the page's messages until a req for op; others go by.
func waitReq(t *testing.T, c *HubConn, op string) *webproto.Req {
	t.Helper()
	end := time.Now().Add(wait)
	for time.Now().Before(end) {
		if r, ok := c.Next(t).(*webproto.Req); ok && r.Op == op {
			return r
		}
	}
	t.Fatalf("no %s request within %s", op, wait)
	return nil
}

func graphRows() webproto.GitGraph {
	now := time.Now()
	e := func(f, t, c int) webproto.GitGraphEdge { return webproto.GitGraphEdge{F: f, T: t, C: c} }
	return webproto.GitGraph{Rows: []webproto.GitGraphRow{
		{Hash: "aaaa111", Parents: []string{"bbbb222", "cccc333"}, Subject: "Merge feature", Author: "Ann", When: now.Add(-time.Hour).UnixMilli(),
			Refs: []webproto.GitRef{{Name: "main", Kind: "local"}, {Name: "origin/main", Kind: "remote"}}, Head: true,
			Col: 0, Color: 0, Up: []webproto.GitGraphEdge{}, Down: []webproto.GitGraphEdge{e(0, 0, 0), e(0, 1, 1)}},
		{Hash: "cccc333", Parents: []string{"dddd444"}, Subject: "Feature work", Author: "Bob", When: now.Add(-2 * time.Hour).UnixMilli(),
			Refs: []webproto.GitRef{{Name: "feature", Kind: "local"}}, Col: 1, Color: 1,
			Up: []webproto.GitGraphEdge{e(0, 0, 0), e(1, 1, 1)}, Down: []webproto.GitGraphEdge{e(0, 0, 0), e(1, 1, 1)}},
		{Hash: "bbbb222", Parents: []string{"dddd444"}, Subject: "Main work", Author: "Ann", When: now.Add(-3 * time.Hour).UnixMilli(),
			Col: 0, Color: 0, Up: []webproto.GitGraphEdge{e(0, 0, 0), e(1, 1, 1)}, Down: []webproto.GitGraphEdge{e(0, 0, 0), e(1, 1, 1)}},
		{Hash: "dddd444", Parents: []string{}, Subject: "First", Author: "Ann", When: now.Add(-4 * time.Hour).UnixMilli(),
			Refs: []webproto.GitRef{{Name: "v1.0", Kind: "tag"}}, Col: 0, Color: 0,
			Up: []webproto.GitGraphEdge{e(0, 0, 0), e(1, 0, 1)}, Down: []webproto.GitGraphEdge{}},
	}}
}

// openGraph opens the commit graph from source control's title bar.
func openGraph(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	p, c := openGit(t, Desktop, gitSnap())
	p.TapJS(`document.querySelector('[data-view="git"] [data-act="graph"]')`)
	r := waitReq(t, c, "git.graph")
	if canonJSON(t, r.Args) != `{"skip":0}` {
		t.Errorf("git.graph args %s", r.Args)
	}
	reply(t, c, r, graphRows())
	p.WaitJS(`document.querySelectorAll('[data-view="graph"] tr.gg-row').length === 4`)
	return p, c
}

func TestGraphTabDrawsBranches(t *testing.T) {
	p, _ := openGraph(t)
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent ?? ""`); got != "Commit Graph" {
		t.Errorf("the tab is %q", got)
	}
	cells := Eval[[]string](p, `[...document.querySelector('[data-view="graph"] tr.gg-row[data-hash="aaaa111"]').cells].slice(1).map((c) => c.textContent.trim())`)
	if len(cells) != 4 || !strings.HasPrefix(cells[0], "mainorigin/mainMerge feature") || cells[2] != "Ann" || cells[3] != "aaaa111" {
		t.Errorf("first row %q", cells)
	}
	// The merge row has a line to each parent's lane; the branch's row
	// sits in the second lane; HEAD's node is a ring.
	if n := Eval[int](p, `document.querySelectorAll('tr[data-hash="aaaa111"] path.gg-line').length`); n != 2 {
		t.Errorf("the merge row draws %d lines", n)
	}
	if cx := Eval[string](p, `document.querySelector('tr[data-hash="cccc333"] circle.gg-node').getAttribute("cx")`); cx != "24" {
		t.Errorf("the branch commit's node is at x=%s, want the second lane (24)", cx)
	}
	if !Eval[bool](p, `document.querySelector('tr[data-hash="aaaa111"] circle.gg-node').classList.contains("is-head")`) {
		t.Error("HEAD's node is not marked")
	}
	if !Eval[bool](p, `!!document.querySelector('tr[data-hash="dddd444"] .gg-ref-tag')`) {
		t.Error("the tag has no label")
	}
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

func TestGraphCommitDetailsOpenAChange(t *testing.T) {
	p, c := openGraph(t)
	p.TapJS(`document.querySelector('tr.gg-row[data-hash="cccc333"]')`)
	r := waitReq(t, c, "git.show")
	if canonJSON(t, r.Args) != `{"hash":"cccc333"}` {
		t.Errorf("git.show args %s", r.Args)
	}
	reply(t, c, r, webproto.GitDiff{Commit: &webproto.GitCommitInfo{Hash: "cccc333", Subject: "Feature work", Body: "Why it changed.", Author: "Bob"},
		Rows: []webproto.DiffRow{{K: "file", Text: "src/a.go"}, {K: "hunk", Text: "@@ -1 +1,2 @@"}, {K: "add", Text: "x"}, {K: "add", Text: "y"}, {K: "del", Text: "z"}, {K: "file", Text: "README.md"}}})
	p.WaitJS(`document.querySelectorAll(".gg-details .gg-file").length === 2`)
	if got := Eval[string](p, `document.querySelector(".gg-details .gg-file").textContent`); got != "a.gosrc+2−1" {
		t.Errorf("the first file %q", got)
	}
	if !Eval[bool](p, `document.querySelector(".gg-details .gg-msg").textContent.includes("Why it changed.")`) {
		t.Error("no commit body")
	}
	p.TapJS(`document.querySelector(".gg-details .gg-file")`)
	p.WaitJS(`document.querySelector(".ed-tab.is-on")?.dataset.path === "@timeline:cccc333:src/a.go"`)
}

func TestGraphMenuStartsRebase(t *testing.T) {
	p, _ := openGraph(t)
	Eval[bool](p, `(document.querySelector('tr.gg-row[data-hash="bbbb222"]').dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, clientX: 300, clientY: 200})), true)`)
	p.WaitJS(`document.querySelector(".ctx-menu")`)
	items := Eval[[]string](p, `[...document.querySelectorAll(".ctx-menu .ctx-label")].map((e) => e.textContent)`)
	if strings.Join(items, "|") != "Copy Commit Hash|Copy Commit Subject|Interactive Rebase from Here…" {
		t.Errorf("menu %q", items)
	}
	Eval[bool](p, `(document.querySelector('tr.gg-row[data-hash="cccc333"]').dispatchEvent(new MouseEvent("contextmenu", {bubbles: true, clientX: 300, clientY: 200})), true)`)
	p.WaitJS(`[...document.querySelectorAll(".ctx-menu .ctx-label")].some((e) => e.textContent === "Check Out feature…")`)
}

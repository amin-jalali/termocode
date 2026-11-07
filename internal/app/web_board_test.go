package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webproto"
)

func webBoardWhere(p string) string { return strings.Replace(p, "/home/u", "~", 1) }

// webBoardHexKey: a workspace key, 16 lower-case hex digits.
func webBoardHexKey(s string) bool {
	if len(s) != 16 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}

// Spec (MOB-54): the board lists the home project first with its own
// agents, then one row per other project from the roll-ups: needs-you
// first, then working, then by name; a second window of the home project
// and an older roll-up of the same project are not rows of their own;
// every row has its workspace key and a ~ folder, never /home/u.
func TestWebBoardBuildOrdersProjects(t *testing.T) {
	own := []agentroll.Item{{Name: "AI chat", State: "working"}}
	others := []agentroll.Rollup{
		{Workspace: "/home/u/src/api", PID: 2, UpdatedMs: 10, Items: []agentroll.Item{{Name: "claude", State: "working"}}},
		{Workspace: "/home/u/src/docs", PID: 3, UpdatedMs: 20, Items: []agentroll.Item{{Name: "codex", State: "working"}, {Name: "run r1 · build", State: "waiting"}}},
		{Workspace: "/home/u/src/api", PID: 4, UpdatedMs: 5, Items: []agentroll.Item{{Name: "old", State: "waiting"}}},
		{Workspace: "/home/u/src/shop", PID: 5, UpdatedMs: 30, Items: []agentroll.Item{{Name: "other window", State: "waiting"}}},
		{Workspace: "/home/u/src/aaa", PID: 6, UpdatedMs: 1, Items: []agentroll.Item{{Name: "x", State: "done"}}},
	}
	got := webBoardBuild("/home/u/src/shop", own, others, webBoardWhere)
	if got.Home != session.Key("/home/u/src/shop") {
		t.Fatalf("home %q", got.Home)
	}
	var names []string
	for _, p := range got.Projects {
		names = append(names, p.Name+":"+p.State)
		if !webBoardHexKey(p.WS) || strings.Contains(p.Where, "/home/u") {
			t.Errorf("row %+v", p)
		}
	}
	want := "shop:working docs:needs api:working aaa:idle"
	if strings.Join(names, " ") != want {
		t.Fatalf("rows %q, want %q", strings.Join(names, " "), want)
	}
	home, docs, api := got.Projects[0], got.Projects[1], got.Projects[2]
	if !home.Home || home.Working != 1 || home.Waiting != 0 || home.Where != "~/src/shop" {
		t.Errorf("home %+v", home)
	}
	if docs.Waiting != 1 || docs.Working != 1 || len(docs.Agents) != 2 || docs.Agents[0].State != "waiting" || docs.Seen != 20 {
		t.Errorf("docs %+v", docs)
	}
	if api.Seen != 10 || len(api.Agents) != 1 || api.Agents[0].Name != "claude" {
		t.Errorf("api: the newest roll-up wins: %+v", api)
	}
}

// Spec (MOB-54): a project names at most MaxBoardAgents agents (the rest
// are counted), and the board keeps at most MaxBoardProjects rows.
func TestWebBoardBuildCaps(t *testing.T) {
	var items []agentroll.Item
	for i := 0; i < webproto.MaxBoardAgents+3; i++ {
		items = append(items, agentroll.Item{Name: fmt.Sprint("a", i), State: "working"})
	}
	var others []agentroll.Rollup
	for i := 0; i < webproto.MaxBoardProjects+5; i++ {
		others = append(others, agentroll.Rollup{Workspace: fmt.Sprintf("/home/u/p%03d", i), PID: 10 + i, Items: items})
	}
	got := webBoardBuild("/home/u/home", nil, others, webBoardWhere)
	if len(got.Projects) != webproto.MaxBoardProjects || got.More != 6 {
		t.Fatalf("%d rows, more %d", len(got.Projects), got.More)
	}
	if p := got.Projects[1]; len(p.Agents) != webproto.MaxBoardAgents || p.More != 3 || p.Working != webproto.MaxBoardAgents+3 {
		t.Errorf("agents %d more %d working %d", len(p.Agents), p.More, p.Working)
	}
	if got.Projects[0].State != webproto.BoardIdle || !got.Projects[0].Home {
		t.Errorf("home %+v", got.Projects[0])
	}
}

// Spec (MOB-54): board.projects answers a viewer with the home row first.
func TestWebBoardProjectsOp(t *testing.T) {
	m, _ := aiIDETestModel(t, fakeAIProvider{reply: "x"})
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	r := &webFilesRig{t: t, m: m, h: h, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleViewer)
	var got webproto.BoardProjects
	webResData(t, r.call(webproto.OpBoardProjects, &webproto.EmptyArgs{}), &got)
	if len(got.Projects) != 1 || !got.Projects[0].Home || got.Projects[0].WS != got.Home {
		t.Fatalf("%+v", got)
	}
}

// Perf guard (MOB-54, plan 6): the board of four projects with eight
// agents each builds within web_board_build_4ws.
func TestPerfWebBoardBuild4WS(t *testing.T) {
	perfbudget.Require(t)
	var items []agentroll.Item
	for i := 0; i < 8; i++ {
		items = append(items, agentroll.Item{Name: fmt.Sprint("agent ", i), State: []string{"working", "waiting"}[i%2]})
	}
	var others []agentroll.Rollup
	for i := 0; i < 3; i++ {
		others = append(others, agentroll.Rollup{Workspace: fmt.Sprint("/home/u/src/p", i), PID: 10 + i, UpdatedMs: int64(i), Items: items})
	}
	var worst time.Duration
	for i := 0; i < 200; i++ {
		t0 := time.Now()
		_ = webBoardBuild("/home/u/src/home", items, others, webBoardWhere)
		worst = max(worst, time.Since(t0))
	}
	perfbudget.Check(t, "web_board_build_4ws", float64(worst.Microseconds())/1000)
}

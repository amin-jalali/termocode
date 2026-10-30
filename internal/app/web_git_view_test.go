package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (web Git Graph): git.graph answers the commits of every branch
// with their refs, the HEAD mark, the time in ms and each row's lanes (a
// side branch's commit sits in its own column); head:true lists HEAD's
// history only; skip pages on with the lanes still right.
func TestGitGraphRowsAndLanes(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	run("checkout", "-q", "-b", "side")
	webTestWrite(t, dir, "s.txt", "s\n")
	run("add", "s.txt")
	run("commit", "-q", "-m", "on side")
	run("checkout", "-q", "main")
	webTestWrite(t, dir, "m.txt", "m\n")
	run("add", "m.txt")
	run("commit", "-q", "-m", "on main")
	run("tag", "v1")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)

	var g webproto.GitGraph
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitGraph, `{}`), &g)
	if len(g.Rows) != 3 || g.More || g.Skip != 0 {
		t.Fatalf("graph %+v", g)
	}
	by := map[string]webproto.GitGraphRow{}
	for _, r := range g.Rows {
		by[r.Subject] = r
		if r.Hash == "" || r.Author != "T" || r.When < 1e12 || r.Up == nil || r.Down == nil {
			t.Errorf("row %+v", r)
		}
	}
	mainTip, side, root := by["on main"], by["on side"], by["first"]
	var refs []string
	for _, r := range mainTip.Refs {
		refs = append(refs, r.Kind+":"+r.Name)
	}
	if !mainTip.Head || strings.Join(refs, ",") != "local:main,tag:v1" {
		t.Errorf("main's tip %+v", mainTip)
	}
	if side.Col == mainTip.Col || len(side.Refs) != 1 || side.Refs[0].Name != "side" || side.Head {
		t.Errorf("side %+v, main col %d", side, mainTip.Col)
	}
	if root.Col != 0 || len(root.Up) != 2 || len(root.Down) != 0 || len(root.Parents) != 0 {
		t.Errorf("root %+v", root)
	}

	g = webproto.GitGraph{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitGraph, `{"head":true}`), &g)
	if len(g.Rows) != 2 {
		t.Errorf("HEAD only: %d rows", len(g.Rows))
	}
	g = webproto.GitGraph{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpGitGraph, `{"skip":2}`), &g)
	if len(g.Rows) != 1 || g.Skip != 2 || g.Rows[0].Subject != "first" || len(g.Rows[0].Up) != 2 {
		t.Errorf("page 2: %+v", g)
	}
}

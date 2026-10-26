package app

import (
	"strings"
	"testing"
)

// AH-10: a broken profiles.json is a row in PROFILES with a fix pill, not
// only a toast. The pill (or Enter) opens the file at the broken line.

const imBadPath = "/home/u/.config/termocode/ai/profiles.json"

func imProblemModel(t *testing.T) *Model {
	t.Helper()
	m := imNarrowModel(t)
	imInject(m, func(sn *itermocSnap) {
		sn.problems = []string{imBadPath + " line 7: invalid character '}'"}
	})
	return m
}

// Spec: the error text names a file and a line; the fix opens exactly there.
func TestItermocProblemTarget(t *testing.T) {
	for _, c := range []struct {
		in   string
		path string
		line int
	}{
		{imBadPath + " line 7: bad", imBadPath, 7},
		{imBadPath + ": permission denied", imBadPath, 1},
		{"/p/.termocode/ai-profiles.json line 12: profile \"x\": no steps", "/p/.termocode/ai-profiles.json", 12},
	} {
		p, l := itermocProblemTarget(c.in)
		if p != c.path || l != c.line {
			t.Errorf("%q -> %q %d, want %q %d", c.in, p, l, c.path, c.line)
		}
	}
}

// Spec: the problem is a selectable row in PROFILES and a "needs you"
// item with a Fix pill.
func TestItermocProblemRowAndNeed(t *testing.T) {
	m := imProblemModel(t)
	var found bool
	for _, r := range m.itermoc.rows() {
		if r.kind == imRowProblem && r.sec == imSecProfiles && r.selectable() {
			found = true
		}
	}
	if !found {
		t.Fatal("no problem row in PROFILES")
	}
	var n *imNeed
	for _, x := range m.itermoc.snap.needs() {
		if x.row.kind == imRowProblem {
			x := x
			n = &x
		}
	}
	if n == nil || n.fix.verb != "fixfile" || n.fix.label != "Fix" {
		t.Fatalf("need %+v, want a Fix pill", n)
	}
	if !strings.Contains(n.what, "line 7") || n.name != "profiles" {
		t.Errorf("need text %q lacks the line", n.what)
	}
}

// Spec: at 80 columns the row shows the pill; click and Enter both run it.
func TestItermocProblemFixClickAndEnter(t *testing.T) {
	m := imProblemModel(t)
	f := imPageText(m, 74, 40)
	if !strings.Contains(f, "line 7") || !strings.Contains(f, "Fix (f)") {
		t.Errorf("list lacks the problem row and its pill:\n%s", f)
	}
	ids := imHitIDs(m, 74, 40)
	if !imHasHit(ids, "im:pg:rowfix:profiles/0") {
		t.Fatalf("no fix hit in %v", ids)
	}
	m.itermocListClick("rowfix:profiles/0")
	if got := m.itermoc.opened; got != imBadPath+":7" {
		t.Errorf("click opened %q", got)
	}
	m = imProblemModel(t)
	imCursorTo(t, m, imRowProblem, "0")
	imKey(m, "enter")
	if got := m.itermoc.opened; got != imBadPath+":7" {
		t.Errorf("Enter opened %q", got)
	}
}

// Spec: the wide sidebar shows the row and Enter fixes it as well.
func TestItermocProblemInSidebar(t *testing.T) {
	m := imOverviewModel(t)
	imInject(m, func(sn *itermocSnap) { sn.problems = []string{imBadPath + " line 7: bad"} })
	if f := imFrame(m); !strings.Contains(f, "profiles") || !strings.Contains(f, "Fix") {
		t.Errorf("sidebar lacks the problem row:\n%s", f)
	}
	m.itermocEnter(imRow{kind: imRowProblem, sec: imSecProfiles, name: "0"})
	if got := m.itermoc.opened; got != imBadPath+":7" {
		t.Errorf("opened %q", got)
	}
}

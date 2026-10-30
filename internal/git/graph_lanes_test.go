package git

import (
	"fmt"
	"strings"
	"testing"
)

// lanesText draws the rows as text: the node column, then each half
// row's edges as from>to pairs, colours left out.
func lanesText(rows []GraphRow) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%d up", r.Col)
		for _, e := range r.Up {
			fmt.Fprintf(&b, " %d>%d", e.From, e.To)
		}
		b.WriteString(" down")
		for _, e := range r.Down {
			fmt.Fprintf(&b, " %d>%d", e.From, e.To)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func gc(hash string, parents ...string) GraphCommit {
	return GraphCommit{Hash: hash, Parents: parents}
}

// Spec (web Git Graph): a straight history is one lane; each row's line
// comes from above (but the first) and goes on below (but the root).
func TestGraphLanesLine(t *testing.T) {
	got := lanesText(GraphLanes([]GraphCommit{gc("c", "b"), gc("b", "a"), gc("a")}))
	want := "0 up down 0>0\n0 up 0>0 down 0>0\n0 up 0>0 down\n"
	if got != want {
		t.Fatalf("got\n%swant\n%s", got, want)
	}
}

// Spec: a merge opens a second lane for its second parent; the branch's
// commits sit in that lane, and the lane joins the first one at the
// commit both sides start from. Lanes keep their colour down the graph.
func TestGraphLanesMerge(t *testing.T) {
	rows := GraphLanes([]GraphCommit{gc("m", "b", "f"), gc("f", "a"), gc("b", "a"), gc("a")})
	got := lanesText(rows)
	want := "" +
		"0 up down 0>0 0>1\n" + // m: down to b (lane 0) and f (lane 1)
		"1 up 0>0 1>1 down 0>0 1>1\n" + // f in lane 1, lane 0 passes
		"0 up 0>0 1>1 down 0>0 1>1\n" + // b in lane 0; f's lane waits for a too
		"0 up 0>0 1>0 down\n" // a: both lanes end in it
	if got != want {
		t.Fatalf("got\n%swant\n%s", got, want)
	}
	if rows[1].Color == rows[0].Color {
		t.Errorf("the branch lane has the main lane's colour %d", rows[1].Color)
	}
	if rows[2].Color != rows[0].Color || rows[3].Color != rows[0].Color {
		t.Errorf("the main lane changed colour: %d %d %d", rows[0].Color, rows[2].Color, rows[3].Color)
	}
}

// Spec: two branch tips (all branches shown) take two lanes; a lane that
// ends frees its column for the next branch, and trailing empty lanes
// are dropped.
func TestGraphLanesTwoTipsAndReuse(t *testing.T) {
	got := lanesText(GraphLanes([]GraphCommit{gc("x", "a"), gc("y", "a"), gc("a")}))
	want := "" +
		"0 up down 0>0\n" +
		"1 up 0>0 down 0>0 1>1\n" +
		"0 up 0>0 1>0 down\n"
	if got != want {
		t.Fatalf("got\n%swant\n%s", got, want)
	}
	// After the lane of y ends at a, the next new tip takes column 1 again.
	got = lanesText(GraphLanes([]GraphCommit{gc("x", "b"), gc("y", "b"), gc("b", "a"), gc("z", "a"), gc("a")}))
	if !strings.Contains(got, "\n1 up 0>0 down 0>0 1>1\n0 up 0>0 1>0 down\n") {
		t.Fatalf("z did not reuse column 1:\n%s", got)
	}
}

// Spec: a parent that is not in the list (the page ends) keeps its lane
// going down out of the last row.
func TestGraphLanesCutAtPageEnd(t *testing.T) {
	got := lanesText(GraphLanes([]GraphCommit{gc("c", "b"), gc("b", "a")}))
	if !strings.HasSuffix(got, "0 up 0>0 down 0>0\n") {
		t.Fatalf("the last row must go on down:\n%s", got)
	}
}

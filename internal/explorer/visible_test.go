package explorer

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// refVisible is the plain definition of the visible rows: every root's
// Node.Visible list, then dot-named rows dropped (never a root row).
func refVisible(m Model) []VisibleEntry {
	isRoot := map[*Node]bool{}
	var all []VisibleEntry
	for _, r := range m.allRoots() {
		isRoot[r] = true
		all = append(all, r.Visible()...)
	}
	var out []VisibleEntry
	for _, e := range all {
		if m.showHidden || isRoot[e.Node] || !strings.HasPrefix(e.Node.Name, ".") {
			out = append(out, e)
		}
	}
	return out
}

// randTree is a random in-memory tree (no disk) of up to depth levels.
func randTree(r *rand.Rand, name string, depth int) *Node {
	n := &Node{Name: name, Path: "/" + name, IsDir: true, Loaded: true, Expanded: r.Intn(3) > 0}
	for i := 0; i < r.Intn(6); i++ {
		cn := fmt.Sprintf("%s%d", []string{"", "."}[r.Intn(4)/3], i)
		if depth > 0 && r.Intn(3) == 0 {
			c := randTree(r, cn, depth-1)
			c.Path = n.Path + "/" + cn
			n.Children = append(n.Children, c)
		} else {
			n.Children = append(n.Children, &Node{Name: cn, Path: n.Path + "/" + cn})
		}
	}
	return n
}

// Spec: the visible rows are every root's open subtree in order, with
// dot-named rows hidden (their open children still shown, a root row
// never hidden) unless hidden files are on.
func TestVisibleMatchesDefinition(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for i := 0; i < 500; i++ {
		m := Model{root: randTree(r, ".root", 4), showHidden: r.Intn(2) == 0}
		for j := 0; j < r.Intn(3); j++ {
			m.extraRoots = append(m.extraRoots, randTree(r, fmt.Sprintf(".x%d", j), 3))
		}
		want := refVisible(m)
		if got := m.visible(); !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d: visible() has %d rows, the definition %d", i, len(got), len(want))
		}
		files := 0
		for _, e := range want {
			if !e.Node.IsDir {
				files++
			}
		}
		if got := m.countVisibleFiles(); got != files {
			t.Fatalf("case %d: countVisibleFiles() = %d, the definition %d", i, got, files)
		}
	}
}

package explorer

import "testing"

// IgnoredName is the dimmed-entry rule the tree draws with; the web UI's
// tree reuses it, so both sides mark the same entries.
func TestIgnoredName(t *testing.T) {
	for _, c := range []struct {
		name string
		dir  bool
		want bool
	}{
		{"node_modules", true, true}, {"node_modules", false, false}, {"dist", true, true},
		{"go.sum", false, true}, {"app.min.js", false, true}, {"Cargo.lock", false, true},
		{"main.go", false, false}, {"src", true, false},
	} {
		if got := IgnoredName(c.name, c.dir); got != c.want {
			t.Errorf("IgnoredName(%q, %v) = %v", c.name, c.dir, got)
		}
		if got := nodeIsIgnored(&Node{Name: c.name, IsDir: c.dir}, nil); got != c.want {
			t.Errorf("nodeIsIgnored(%q) = %v", c.name, got)
		}
	}
}

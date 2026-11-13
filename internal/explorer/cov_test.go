package explorer

import (
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AddRoot on a container workspace takes absolute container paths only,
// skips duplicates and refuses files and missing folders.
func TestAddRootInAContainer(t *testing.T) {
	h, _, _ := fakeContainer(t, false)
	m := NewFS(h.FS(), "/app")
	if m.err != "" {
		t.Fatal(m.err)
	}
	if err := m.AddRoot("rel/path"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative: %v", err)
	}
	if err := m.AddRoot("/app/"); err != nil || len(m.extraRoots) != 0 {
		t.Fatalf("the primary root again: %v %d", err, len(m.extraRoots))
	}
	if err := m.AddRoot("/app/asub"); err != nil || len(m.extraRoots) != 1 {
		t.Fatalf("add: %v %d", err, len(m.extraRoots))
	}
	if err := m.AddRoot("/app/asub"); err != nil || len(m.extraRoots) != 1 {
		t.Fatalf("twice: %v %d", err, len(m.extraRoots))
	}
	if err := m.AddRoot("/app/main.go"); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Fatalf("file: %v", err)
	}
	if err := m.AddRoot("/app/nope"); err == nil {
		t.Fatal("missing folder accepted")
	}
}

// Reload reads a container root again through its FS.
func TestReloadContainerRoot(t *testing.T) {
	h, _, c := fakeContainer(t, false)
	m := NewFS(h.FS(), "/app")
	c.WriteFile(t, "/app/new.txt", []byte("n"), 0o644)
	n, err := rebuild(m.root, "")
	if err != nil || n.fsys == nil || n.Path != "/app" {
		t.Fatalf("%v %v", n, err)
	}
}

func TestHiddenShownAndVisibleAt(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, "a.txt", "b.txt")
	m := newModelAt(t, dir)
	m.SetSize(40, 20)
	if m.HiddenShown() {
		t.Fatal("hidden shown at start")
	}
	m.ToggleHidden()
	if !m.HiddenShown() {
		t.Fatal("toggle did nothing")
	}
	if _, ok := m.VisibleAt(0); ok {
		t.Fatal("the header row is not a node")
	}
	if _, ok := m.VisibleAt(-3); ok {
		t.Fatal("a row above the panel is not a node")
	}
	if _, ok := m.VisibleAt(19); ok {
		t.Fatal("a footer row is not a node")
	}
	if _, ok := m.VisibleAt(m.HeaderChromeRows() + 40); ok {
		t.Fatal("a row past the last node")
	}
	if n, ok := m.VisibleAt(m.HeaderChromeRows() + 1); !ok || n == nil {
		t.Fatal("no node on the first body rows")
	}
}

// The cached counts and views are reused only while the extra roots are
// the same nodes.
func TestCacheKeysCompareExtraRoots(t *testing.T) {
	a, b := &Node{Path: "/a"}, &Node{Path: "/b"}
	k1 := countKey{extraRoots: []*Node{a}}
	same := countKey{extraRoots: []*Node{a}}
	other := countKey{extraRoots: []*Node{b}}
	if !k1.equal(&same) || k1.equal(&other) {
		t.Fatal("countKey.equal")
	}
	v1 := viewKey{extraRoots: []*Node{a}}
	vSame := viewKey{extraRoots: []*Node{a}}
	vOther := viewKey{extraRoots: []*Node{b}}
	if !v1.equal(&vSame) || v1.equal(&vOther) {
		t.Fatal("viewKey.equal")
	}
}

// Counting files with several roots counts every root, and a file root
// counts as one.
func TestCountVisibleFilesWithExtraRoots(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	mkTree(t, a, "x.txt", "y.txt")
	mkTree(t, b, "z.txt")
	m := newModelAt(t, a)
	if err := m.AddRoot(b); err != nil {
		t.Fatal(err)
	}
	if got := m.CountVisibleFiles(); got != 3 {
		t.Fatalf("count %d", got)
	}
	if got := m.CountVisibleFiles(); got != 3 { // from the cache
		t.Fatalf("cached count %d", got)
	}
}

// A rename that cannot make its staging folder fails whole and leaves the
// files where they were.
func TestApplyRenameStagingFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	mkTree(t, dir, "a.txt")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	err := ApplyRename([]Rename{{Old: filepath.Join(dir, "a.txt"), New: filepath.Join(dir, "b.txt")}}, nil)
	if err == nil {
		t.Fatal("rename in a read-only folder succeeded")
	}
	if _, e := os.Stat(filepath.Join(dir, "a.txt")); e != nil {
		t.Fatal("the file moved")
	}
}

// The preview pane says "Loading…" until the text for the cursor's file
// has been asked.
func TestPreviewPaneLoadingText(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, "a.txt")
	m := newModelAt(t, dir)
	m.SetSize(120, 20)
	m.SetTerminalWidth(120)
	m.previewOn = true
	m.previewPath = "/not/the/cursor"
	if out := m.previewView(true, "", false, nil); !strings.Contains(out, "Loading…") {
		t.Fatalf("no loading text:\n%s", out)
	}
}

// The tree draws at every small size without going past its box, and
// with no width it draws nothing for the hairline.
func TestViewAtTinySizes(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, "a.txt", "b.txt", "sub/c.txt")
	m := newModelAt(t, dir)
	for _, sz := range [][2]int{{0, 0}, {1, 1}, {3, 2}, {10, 3}, {12, 5}, {20, 8}, {30, 14}} {
		m.SetSize(sz[0], sz[1])
		out := m.View(true, "", false, nil)
		if sz[0] > 0 && sz[1] > 0 {
			rows := strings.Split(out, "\n")
			if len(rows) > sz[1] {
				t.Errorf("%v: %d rows", sz, len(rows))
			}
			for _, r := range rows {
				if w := lipgloss.Width(r); w > sz[0] {
					t.Errorf("%v: row %d wide", sz, w)
				}
			}
		}
	}
	m.SetSize(0, 5)
	if m.renderHeaderHairline(true) != "" {
		t.Fatal("hairline with no width")
	}
}

// Untracked files and ignored files get their own colours in every icon
// mode (the marker column differs per mode).
func TestViewGitColoursAndMarkersPerIconMode(t *testing.T) {
	old := theme.CurrentIconMode
	t.Cleanup(func() { theme.CurrentIconMode = old })
	dir := t.TempDir()
	mkTree(t, dir, "new.go", "ignored.txt", "a-very-long-extension-name.markdown")
	m := newModelAt(t, dir)
	m.SetSize(50, 14)
	gs := GitStatus{filepath.Join(dir, "new.go"): "U", filepath.Join(dir, "ignored.txt"): "I"}
	for _, mode := range []theme.IconMode{theme.IconModeNerdFont, theme.IconModeUnicode, theme.IconModeASCII} {
		theme.CurrentIconMode = mode
		out := ansi.Strip(m.View(true, "", false, gs))
		if !strings.Contains(out, "new.go") || !strings.Contains(out, "ignored.txt") {
			t.Fatalf("mode %v:\n%s", mode, out)
		}
	}
}

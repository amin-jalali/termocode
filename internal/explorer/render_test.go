package explorer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

func withIcons(t *testing.T, mode theme.IconMode) {
	t.Helper()
	old := theme.CurrentIconMode
	theme.CurrentIconMode = mode
	t.Cleanup(func() { theme.CurrentIconMode = old })
}

func screenLines(m Model, gs GitStatus) []string {
	return strings.Split(ansi.Strip(m.View(true, "", false, gs)), "\n")
}

// rowOf returns the screen row that names file.
func rowOfName(lines []string, name string) string {
	for _, l := range lines {
		if strings.Contains(l, " "+name) {
			return l
		}
	}
	return ""
}

func TestASCIIRowsShowTagsBadgesAndFitWidth(t *testing.T) {
	withIcons(t, theme.IconModeASCII)
	dir := t.TempDir()
	mkTree(t, dir, "Makefile", "Dockerfile", "README.md", "go.mod", "go.sum", "yarn.lock", "app.min.js", "x.c", "noext",
		"main.go", "script.sh", "conf.yaml", "lib.rs", "a.py", "web.tsx", "build/", "src/")
	m := newModelAt(t, dir)
	m.SetSize(36, 40)
	m.SetMeta("a-very-long-workspace-name-indeed", "feature/very-long-branch-name", true, 12)
	gs := GitStatus{
		filepath.Join(dir, "main.go"):   "M",
		filepath.Join(dir, "a.py"):      "U",
		filepath.Join(dir, "lib.rs"):    "D",
		filepath.Join(dir, "x.c"):       "C",
		filepath.Join(dir, "conf.yaml"): "A",
		filepath.Join(dir, "noext"):     "?",
		filepath.Join(dir, "web.tsx"):   "I",
	}
	lines := screenLines(m, gs)
	if len(lines) != 40 {
		t.Fatalf("%d rows", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 36 {
			t.Fatalf("row %d is %d wide: %q", i, w, l)
		}
	}
	for name, tag := range map[string]string{"Makefile": "mk", "Dockerfile": "dk", "README.md": "md", "go.mod": "go", "go.sum": "sm", "yarn.lock": "lk", "x.c": "c ", "main.go": "go"} {
		row := rowOfName(lines, name)
		if !strings.Contains(row, tag+" "+name) && !strings.Contains(row, tag+"  "+name) {
			t.Errorf("%s: no %q tag in %q", name, tag, row)
		}
	}
	for name, badge := range map[string]string{"main.go": "M", "a.py": "U", "lib.rs": "D", "x.c": "C", "conf.yaml": "A"} {
		if row := strings.TrimRight(rowOfName(lines, name), " "); !strings.HasSuffix(row, badge) {
			t.Errorf("%s: badge %q missing in %q", name, badge, row)
		}
	}
	if row := rowOfName(lines, "src"); !strings.Contains(row, "▸") {
		t.Errorf("closed folder chevron: %q", row)
	}
	header := lines[0]
	if !strings.Contains(header, "EXPLORER") || !strings.Contains(header, "…") {
		t.Errorf("header does not cut long names: %q", header)
	}
}

func TestChromeFollowsPanelHeight(t *testing.T) {
	withIcons(t, theme.IconModeASCII)
	dir := t.TempDir()
	mkTree(t, dir, "a.txt", "b.txt")
	m := newModelAt(t, dir)
	for _, c := range []struct {
		h                 int
		header, separator bool
	}{
		{20, true, true},
		{8, true, true},
		{4, false, false},
	} {
		m.SetSize(30, c.h)
		lines := screenLines(m, nil)
		if len(lines) != c.h {
			t.Fatalf("h=%d: %d rows", c.h, len(lines))
		}
		if strings.Contains(lines[0], "EXPLORER") != c.header {
			t.Errorf("h=%d: header row %q", c.h, lines[0])
		}
		if hasDivider := strings.Contains(strings.Join(lines, "\n"), "────"); hasDivider != c.separator {
			t.Errorf("h=%d: divider %v", c.h, hasDivider)
		}
	}
	// A panel too narrow for any text still draws blank rows of its width.
	m.SetSize(3, 20)
	for i, l := range screenLines(m, nil) {
		if ansi.StringWidth(l) != 3 {
			t.Fatalf("narrow row %d = %q", i, l)
		}
	}
	m.SetSize(0, 0)
	if m.View(true, "", false, nil) != "" {
		t.Fatal("zero size must draw nothing")
	}
}

func TestIgnoredNamesAndHelpers(t *testing.T) {
	gs := GitStatus{"/w/x": "I"}
	for _, c := range []struct {
		n    *Node
		want bool
	}{
		{nil, false},
		{&Node{Name: "x", Path: "/w/x"}, true},
		{&Node{Name: "node_modules", IsDir: true}, true},
		{&Node{Name: "src", IsDir: true}, false},
		{&Node{Name: "app.MIN.CSS"}, true},
		{&Node{Name: "package-lock.json"}, true},
		{&Node{Name: "pnpm-lock.yaml"}, true},
		{&Node{Name: "composer.lock"}, true},
		{&Node{Name: "main.go"}, false},
	} {
		if got := nodeIsIgnored(c.n, gs); got != c.want {
			t.Errorf("%+v: %v", c.n, got)
		}
	}
	if itoa(0) != "0" || itoa(-42) != "-42" || itoa(1234) != "1234" {
		t.Fatal("itoa")
	}
	if c, bold := gitBadgeStyle("!"); c != theme.AccentRedCoral || !bold {
		t.Fatal("conflict badge")
	}
	if c, _ := gitBadgeStyle("?"); c != theme.TextMuted {
		t.Fatal("unknown badge")
	}
	for name, want := range map[string]theme.Color256{
		"a.go": theme.AccentLavender, "a.md": theme.AccentGreen, "a.py": theme.AccentAmber,
		"a.rs": theme.AccentRedCoral, "a.ts": theme.AccentAmber, "a.json": theme.AccentMagenta,
		"a.sh": theme.AccentGreen, "a.sum": theme.TextMuted, "a.zip": theme.TextMuted, "go.sum": theme.TextMuted,
	} {
		if got := extTagColor(name); got != want {
			t.Errorf("extTagColor(%s) = %v, want %v", name, got, want)
		}
	}
	if blankSidebarRow(0) != "" || sidebarDividerRow(0) != "" {
		t.Fatal("zero-width rows")
	}
	if got := ansi.StringWidth(padOrTruncRow("abcdef", 3)); got != 3 {
		t.Fatalf("truncate row width %d", got)
	}
	counts := computeFolderCounts([]*Node{{Path: "/w"}}, GitStatus{"/w/a/b.go": "M", "/w/a/c.go": "I", "/x/y.go": "M", "/w/d.go": ""})
	if counts["/w/a"] != 1 || counts["/w"] != 1 || len(counts) != 2 {
		t.Fatalf("counts %v", counts)
	}
	if len(computeFolderCounts(nil, nil)) != 0 {
		t.Fatal("no status, no counts")
	}
}

package statusbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"termocode/internal/theme"
)

func TestNewAndUpdateAreInert(t *testing.T) {
	m := New(theme.Theme{})
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init must be nil-cmd")
	}
	if _, cmd := m.Update(nil); cmd != nil {
		t.Fatal("Update must be nil-cmd")
	}
}

func TestViewEmptyAtZeroWidth(t *testing.T) {
	m := New(theme.Theme{})
	if v := m.View(State{}); v != "" {
		t.Fatalf("zero width view want empty, got %q", v)
	}
}

func TestViewFitsWidthExactly(t *testing.T) {
	m := New(theme.Theme{})
	m.SetWidth(80)
	out := m.View(State{
		Path:   "/x/y/main.go",
		Lang:   "Go",
		Line:   3,
		Col:    7,
		Branch: "main",
	})
	if got := lipgloss.Width(out); got != 80 {
		t.Fatalf("rendered width want 80, got %d", got)
	}
}

func TestViewIncludesProjectLineColAndBranch(t *testing.T) {
	// In the redesigned bar the left zone carries the project name
	// (not the filename — that lives in the breadcrumbs row), the
	// center carries the branch, and the right carries Ln/Col + lang.
	m := New(theme.Theme{})
	m.SetWidth(120)
	out := m.View(State{
		Path:    "/work/repo/file.go",
		Project: "repo",
		Branch:  "feature/x",
		Line:    12,
		Col:     34,
		Lang:    "Go",
	})
	for _, want := range []string{"repo", "feature/x", "Ln 12", "Col 34", "Go"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q: %s", want, out)
		}
	}
	// Filename must NOT appear — that's the breadcrumbs' job now.
	if strings.Contains(out, "file.go") {
		t.Fatalf("filename should not be rendered in the status bar: %s", out)
	}
}

func TestRenderLeftDirtyDotWithProject(t *testing.T) {
	// With a buffer open + Dirty=true, the left zone shows the dirty
	// dot followed by the project name. No filename, no directory.
	m := New(theme.Theme{})
	out := m.renderLeft(State{Path: "/x/y/main.go", Project: "termocode", Dirty: true})
	if !strings.Contains(out, "termocode") {
		t.Fatalf("project name missing in left segment: %s", out)
	}
	if strings.Contains(out, "main.go") {
		t.Fatalf("filename leaked into left segment: %s", out)
	}
	if strings.Contains(out, "/x/y") {
		t.Fatalf("directory leaked into left segment: %s", out)
	}
}

func TestViewErrTakesOverLeft(t *testing.T) {
	m := New(theme.Theme{})
	m.SetWidth(80)
	out := m.View(State{Err: "something bad", Branch: "main", Errors: 5})
	if !strings.Contains(out, "something bad") {
		t.Fatalf("err msg missing in left segment")
	}
	// When Err is set, branch + counters must NOT render.
	if strings.Contains(out, "main") {
		t.Fatalf("branch should be hidden when Err is set")
	}
}

func TestRenderLeftWithoutPath(t *testing.T) {
	// In the redesigned bar the active filename lives in the left
	// zone; with no path open we expect the muted "[no file]"
	// placeholder there.
	m := New(theme.Theme{})
	out := m.renderLeft(State{})
	if !strings.Contains(out, "[no file]") {
		t.Fatalf("no-file placeholder missing: %s", out)
	}
}

func TestRenderRightDefaultsEncoding(t *testing.T) {
	m := New(theme.Theme{})
	out := m.renderRight(State{Line: 1, Col: 1})
	if !strings.Contains(out, "UTF-8") {
		t.Fatalf("default encoding UTF-8 missing: %s", out)
	}
}

func TestRenderRightTermIndicator(t *testing.T) {
	m := New(theme.Theme{})
	out := m.renderRight(State{Line: 1, Col: 1, Term: true})
	if !strings.Contains(out, "TERM") {
		t.Fatalf("TERM indicator missing when Term=true: %s", out)
	}
	out = m.renderRight(State{Line: 1, Col: 1, Term: false})
	if strings.Contains(out, "TERM") {
		t.Fatalf("TERM indicator should not show when Term=false")
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abcdef", 5, "abcd…"},
	}
	for _, tc := range cases {
		if got := truncate(tc.s, tc.n); got != tc.want {
			t.Fatalf("truncate(%q,%d) = %q, want %q", tc.s, tc.n, got, tc.want)
		}
	}
}

func TestStripCSI(t *testing.T) {
	in := "\x1b[31mred\x1b[0m text"
	if got := stripCSI(in); got != "red text" {
		t.Fatalf("stripCSI: got %q", got)
	}
}

func TestTruncateStyledShort(t *testing.T) {
	if got := truncateStyled("abc", 0); got != "" {
		t.Fatalf("truncateStyled width 0 want empty, got %q", got)
	}
	// Already fits — pass through unchanged.
	if got := truncateStyled("abc", 10); got != "abc" {
		t.Fatalf("truncateStyled fits: got %q", got)
	}
}

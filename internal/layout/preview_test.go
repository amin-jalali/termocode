package layout

import (
	"reflect"
	"testing"
)

func TestPreviewListsWhatOpeningDoes(t *testing.T) {
	l := Layout{
		Sidebar: &Sidebar{Open: true, View: "git", Width: 40},
		Panel:   &Panel{Open: true, Rows: 12, Tabs: []string{"problems", "terminal"}, Terminals: []Terminal{{}, {Cwd: "web"}}},
		Chat:    &Chat{Open: false},
		Editor:  &Editor{Files: []string{"a.go", "b.go", "c.go"}, Split: "vertical", Panes: []string{"a.go", "b.go"}},
		Focus:   "terminal",
	}
	want := []string{
		"Sidebar: Source Control (40 columns)",
		"Bottom panel: open with Problems, 2 terminals (12 rows)",
		"Chat panel: closed",
		"Editor: split vertical into 2 windows, opens 3 files",
		"Focus: terminal",
	}
	if got := l.Preview(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestPreviewOfSmallAndEmptyLayouts(t *testing.T) {
	if got := (Layout{}).Preview(); !reflect.DeepEqual(got, []string{"Changes nothing"}) {
		t.Errorf("empty = %q", got)
	}
	l := Layout{Sidebar: &Sidebar{}, Panel: &Panel{}, Editor: &Editor{}}
	want := []string{"Sidebar: hidden", "Bottom panel: closed", "Editor: one window"}
	if got := l.Preview(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
	one := Layout{Panel: &Panel{Open: true, Terminals: []Terminal{{}}}, Editor: &Editor{Files: []string{"x"}}}
	want = []string{"Bottom panel: open with 1 terminal", "Editor: opens 1 file"}
	if got := one.Preview(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestEveryBuiltinHasAPreview(t *testing.T) {
	for _, e := range Builtins() {
		if p := e.Preview(); len(p) == 0 || p[0] == "Changes nothing" {
			t.Errorf("%s preview = %q", e.Name, p)
		}
	}
}

func TestPreviewCountsSplitPanes(t *testing.T) {
	l := Layout{Panel: &Panel{Open: true, Terminals: []Terminal{
		{Panes: []Pane{{Split: "right"}, {Split: "down", Parent: 1}}},
		{},
	}}}
	want := []string{"Bottom panel: open with 2 terminals, 2 split panes"}
	if got := l.Preview(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

package app

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// layoutProject makes a project folder, enters it, and returns its real path.
func layoutProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	t.Chdir(dir)
	return dir
}

func TestCaptureLayoutReadsPanelsAndTabs(t *testing.T) {
	root := layoutProject(t)
	m := panelTestModel()
	m.activity.SetActive(activity.ViewGit)
	m.explorerWidth = 33
	m.actionsOpen, m.actionsPinned, m.actionsWidth, m.actionsWidthSet = true, true, 55, true
	m.terminalTabs = []terminalTab{
		{Name: "api", Cwd: filepath.Join(root, "cmd", "api")},
		{Name: "elsewhere", Cwd: "/somewhere/else"},
		{Name: "root", Cwd: root},
	}
	m.bufs = []nvim.BufferInfo{
		{ID: 3, Path: filepath.Join(root, "main.go")},
		{ID: 4, Path: filepath.Join(root, "pkg", "util.go")},
		{ID: 5, Path: "/etc/hosts"},                                   // outside the project: left out
		{ID: 6, Path: filepath.Join(root, "x.sql"), Title: "console"}, // a tool tab: left out
	}
	m.activeBuf = 4
	m.focus = FocusExplorer

	l := m.captureLayout("mine")
	if l.Name != "mine" || l.Sidebar.View != "git" || l.Sidebar.Width != 33 || !l.Sidebar.Open {
		t.Errorf("sidebar = %+v", l.Sidebar)
	}
	if !l.Chat.Open || l.Chat.Width != 55 {
		t.Errorf("chat = %+v", l.Chat)
	}
	if !l.Panel.Open || l.Panel.Rows != 10 || l.Panel.Active != "output" || !reflect.DeepEqual(l.Panel.Tabs, []string{"output"}) {
		t.Errorf("panel = %+v", l.Panel)
	}
	if got := l.Panel.Terminals; len(got) != 3 || got[0].Cwd != "cmd/api" || got[1].Cwd != "" || got[2].Cwd != "" || got[0].Name != "api" {
		t.Errorf("terminals = %+v", got)
	}
	if want := []string{"main.go", "pkg/util.go"}; !reflect.DeepEqual(l.Editor.Files, want) || l.Editor.Active != "pkg/util.go" {
		t.Errorf("editor = %+v", l.Editor)
	}
	if l.Focus != "sidebar" {
		t.Errorf("focus = %q", l.Focus)
	}
	if err := l.Validate(); err != nil {
		t.Errorf("captured layout is not valid: %v", err)
	}
}

func TestCaptureLayoutOfAClosedPanelAndHiddenChrome(t *testing.T) {
	layoutProject(t)
	m := panelTestModel()
	m.termOpen = false
	m.showExp = false
	m.actionsOpen = false
	l := m.captureLayout("x")
	if l.Panel.Open || l.Panel.Rows != 0 || len(l.Panel.Terminals) != 0 {
		t.Errorf("panel = %+v", l.Panel)
	}
	if l.Sidebar.Open || l.Chat.Open {
		t.Errorf("sidebar %+v chat %+v", l.Sidebar, l.Chat)
	}
}

func TestCaptureLayoutKeepsTerminalSplits(t *testing.T) {
	root := layoutProject(t)
	m := panelTestModel()
	m.terminalTabs = []terminalTab{
		{Name: "api", Cwd: root, Splits: []termPane{
			{Cwd: filepath.Join(root, "web"), Dir: paneRight, Parent: 0},
			{Cwd: "/somewhere/else", Dir: paneDown, Parent: 1},
		}},
		{Name: "one", Cwd: root},
	}
	l := m.captureLayout("x")
	got := l.Panel.Terminals
	want := []layout.Pane{{Split: "right", Cwd: "web"}, {Split: "down", Parent: 1}}
	if len(got) != 2 || !reflect.DeepEqual(got[0].Panes, want) || got[1].Panes != nil {
		t.Fatalf("terminals = %+v", got)
	}
	if err := l.Validate(); err != nil {
		t.Errorf("captured layout is not valid: %v", err)
	}
}

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/webproto"
)

func webWorkspaceRig(t *testing.T) (*webFilesRig, string) {
	t.Helper()
	r := newWebFilesRig(t)
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Chdir(r.root)
	r.m.explorer = explorer.New()
	return r, cfg
}

// Spec (WEBP-9): workspace.list names the folders (the first is the
// workspace itself) and the recent workspaces, names only; adding and
// removing a folder wait for the laptop's yes, then change the explorer.
func TestWebWorkspaceFolders(t *testing.T) {
	r, _ := webWorkspaceRig(t)
	extra := t.TempDir()
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpWorkspaceAddFolder, &webproto.WorkspacePathArgs{Folder: extra}), &ref)
	if ref.Card == "" || len(r.m.explorer.Roots()) != 1 {
		t.Fatalf("added before the laptop said yes: %v", r.m.explorer.Roots())
	}
	if v := r.m.confirm.View(); !strings.Contains(v, filepath.Base(extra)) {
		t.Errorf("card:\n%s", v)
	}
	webTestLaptopSelect(t, r.m, "confirm")
	if roots := r.m.explorer.Roots(); len(roots) != 2 || roots[1] != extra {
		t.Fatalf("roots %v", roots)
	}
	var list webproto.WorkspaceList
	webResData(t, r.call(webproto.OpWorkspaceList, &webproto.EmptyArgs{}), &list)
	if len(list.Folders) != 2 || list.Folders[0].Name != "ws-root" || list.Folders[1].Name != filepath.Base(extra) || list.Folders[1].ID != 1 {
		t.Fatalf("folders %+v", list.Folders)
	}
	if b, _ := json.Marshal(list); strings.Contains(string(b), extra) {
		t.Errorf("a path outside the workspace went out: %s", b)
	}
	if f := r.call(webproto.OpWorkspaceRemoveFolder, &webproto.IDArgs{ID: 0}); webErrCode(f) != "forbidden" {
		t.Errorf("the workspace folder itself: %v", f)
	}
	webTestData(t, r.call(webproto.OpWorkspaceRemoveFolder, &webproto.IDArgs{ID: 1}), &ref)
	webTestLaptopSelect(t, r.m, "confirm")
	if roots := r.m.explorer.Roots(); len(roots) != 1 {
		t.Fatalf("roots after remove %v", roots)
	}
}

// Spec (WEBP-9): workspace.open opens one of the recent workspaces only
// after the laptop's yes (the laptop restarts there).
func TestWebWorkspaceOpen(t *testing.T) {
	r, cfg := webWorkspaceRig(t)
	other := t.TempDir()
	data, _ := json.Marshal([]WorkspaceEntry{{Path: r.root, OpenedAt: time.Now()}, {Path: other, OpenedAt: time.Now().Add(-time.Hour)}})
	if err := os.MkdirAll(filepath.Join(cfg, "termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "termocode", "workspaces.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	var opened []string
	old := webOpenWorkspace
	webOpenWorkspace = func(_ *Model, p string) tea.Cmd { opened = append(opened, p); return nil }
	t.Cleanup(func() { webOpenWorkspace = old })
	var list webproto.WorkspaceList
	webResData(t, r.call(webproto.OpWorkspaceList, &webproto.EmptyArgs{}), &list)
	if len(list.Recent) != 1 || list.Recent[0].Name != filepath.Base(other) {
		t.Fatalf("recent %+v", list.Recent)
	}
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpWorkspaceOpen, &webproto.IDArgs{ID: list.Recent[0].ID}), &ref)
	if len(opened) != 0 {
		t.Fatal("opened before the laptop said yes")
	}
	webTestLaptopSelect(t, r.m, "confirm")
	if len(opened) != 1 || opened[0] != other {
		t.Fatalf("opened %v", opened)
	}
}

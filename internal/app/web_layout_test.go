package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/layout"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webLayoutRig is a web rig with its own config folder, a saved user
// layout "mine" and a project layout "team".
func webLayoutRig(t *testing.T) (*webFilesRig, string) {
	t.Helper()
	t.Setenv("TERMOCODE_HEADLESS", "1")
	r := newWebFilesRig(t)
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Chdir(r.root)
	r.m.showExp, r.m.explorerWidth = true, defaultExplorerWidth
	st := r.m.layoutStore()
	if _, err := st.Save(layout.User, layout.Layout{Name: "mine", Description: "my own"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(layout.Project, layout.Layout{Name: "team", Sidebar: &layout.Sidebar{Open: true, View: "git"}}); err != nil {
		t.Fatal(err)
	}
	return r, cfg
}

// Spec (WEBP-7): layout.list names the built-in, user and project
// layouts with where each comes from and what it changes (no paths).
func TestWebLayoutList(t *testing.T) {
	r, cfg := webLayoutRig(t)
	var list webproto.LayoutList
	webResData(t, r.call(webproto.OpLayoutList, &webproto.EmptyArgs{}), &list)
	got := map[string]webproto.LayoutEntry{}
	for _, e := range list.Items {
		got[e.Name] = e
	}
	if e := got["focus"]; e.Source != "built-in" || e.Description == "" || len(e.Preview) == 0 {
		t.Errorf("focus %+v", e)
	}
	if e := got["mine"]; e.Source != "user" || e.Description != "my own" {
		t.Errorf("mine %+v", e)
	}
	if e := got["team"]; e.Source != "project" || len(e.Preview) == 0 || !strings.Contains(e.Preview[0], "Sidebar") {
		t.Errorf("team %+v", e)
	}
	for _, e := range list.Items {
		for _, p := range append([]string{e.Description}, e.Preview...) {
			if strings.Contains(p, cfg) || strings.Contains(p, r.root) {
				t.Errorf("%s names a laptop path: %q", e.Name, p)
			}
		}
	}
}

// Spec (WEBP-7): layout.open opens a layout on the laptop as Layout:
// Open… does; layout.reset goes back to the default one. An unknown
// layout is refused.
func TestWebLayoutOpenAndReset(t *testing.T) {
	r, _ := webLayoutRig(t)
	if f := r.call(webproto.OpLayoutOpen, &webproto.LayoutNameArgs{Name: "focus"}); f["t"] != "res" {
		t.Fatal(f)
	}
	if r.m.showExp {
		t.Error("focus left the sidebar open")
	}
	r.m.explorerWidth = defaultExplorerWidth + 9
	if f := r.call(webproto.OpLayoutReset, &webproto.EmptyArgs{}); f["t"] != "res" {
		t.Fatal(f)
	}
	if !r.m.showExp || r.m.explorerWidth != defaultExplorerWidth {
		t.Errorf("after reset: sidebar %v width %d", r.m.showExp, r.m.explorerWidth)
	}
	if f := r.call(webproto.OpLayoutOpen, &webproto.LayoutNameArgs{Name: "nope"}); webErrCode(f) != "not_found" {
		t.Errorf("an unknown layout: %v", f)
	}
}

// Spec (WEBP-7): layout.save saves the laptop's screen as a user or
// project layout. A name that is taken is not replaced: the answer says
// it exists, and only a save with replace writes it. A built-in name is
// refused.
func TestWebLayoutSave(t *testing.T) {
	r, cfg := webLayoutRig(t)
	var saved webproto.LayoutSaved
	webResData(t, r.call(webproto.OpLayoutSave, &webproto.LayoutSaveArgs{Name: "wide", Scope: "user"}), &saved)
	if !saved.Saved || saved.Exists {
		t.Fatalf("save %+v", saved)
	}
	if _, err := os.Stat(filepath.Join(cfg, "termocode", "layouts", "wide"+layout.Ext)); err != nil {
		t.Fatal(err)
	}
	saved = webproto.LayoutSaved{}
	webResData(t, r.call(webproto.OpLayoutSave, &webproto.LayoutSaveArgs{Name: "team", Scope: "project"}), &saved)
	if saved.Saved || !saved.Exists {
		t.Fatalf("a taken name was replaced without asking: %+v", saved)
	}
	saved = webproto.LayoutSaved{}
	webResData(t, r.call(webproto.OpLayoutSave, &webproto.LayoutSaveArgs{Name: "team", Scope: "project", Replace: true}), &saved)
	if !saved.Saved {
		t.Fatalf("replace %+v", saved)
	}
	if e, ok := r.m.layoutStore().Find("team"); !ok || e.Sidebar == nil || e.Sidebar.View == "git" {
		t.Errorf("team was not replaced with the screen: %+v", e)
	}
	if f := r.call(webproto.OpLayoutSave, &webproto.LayoutSaveArgs{Name: "focus", Scope: "user"}); webErrCode(f) != "bad_request" {
		t.Errorf("a built-in name: %v", f)
	}
}

// Spec (WEBP-7): deleting a saved layout waits for a laptop card; a
// built-in layout cannot be deleted.
func TestWebLayoutDelete(t *testing.T) {
	r, cfg := webLayoutRig(t)
	if f := r.call(webproto.OpLayoutDelete, &webproto.LayoutNameArgs{Name: "focus"}); webErrCode(f) != "forbidden" {
		t.Errorf("a built-in layout: %v", f)
	}
	if f := r.call(webproto.OpLayoutDelete, &webproto.LayoutNameArgs{Name: "nope"}); webErrCode(f) != "not_found" {
		t.Errorf("an unknown layout: %v", f)
	}
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpLayoutDelete, &webproto.LayoutNameArgs{Name: "mine"}), &ref)
	if v := r.m.confirm.View(); ref.Card == "" || !strings.Contains(v, "Delete layout mine") {
		t.Errorf("card %q:\n%s", ref.Card, v)
	}
	file := filepath.Join(cfg, "termocode", "layouts", "mine"+layout.Ext)
	if _, err := os.Stat(file); err != nil {
		t.Fatal("deleted before the laptop said yes")
	}
	webTestLaptopSelect(t, r.m, "confirm")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("mine is still there after the laptop's yes: %v", err)
	}
}

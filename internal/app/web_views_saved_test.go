package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func savedViewSpec(view string) webproto.ViewSpec {
	return webproto.ViewSpec{View: view, Fields: []string{"n", "title"}}
}

// Spec (MOBUX-18): view.save keeps a spec under a name in the laptop's
// views.json (a name used again, in any case, replaces that view and
// keeps its id and pin); view.list shows pinned views first in their
// order, then the others by name; rename, pin and delete answer with the
// new list; a view that is gone is not_found.
func TestWebSavedViews(t *testing.T) {
	r := newWebFilesRig(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var saved webproto.ViewSaved
	webResData(t, r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("pr.list"), Name: "PRs to review", Pin: true}), &saved)
	if saved.ID == "" || saved.Replaced {
		t.Fatalf("first save %+v", saved)
	}
	prs := saved.ID
	webResData(t, r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("ci.checks"), Name: "Failing CI"}), &saved)
	ci := saved.ID
	webResData(t, r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("git.log"), Name: "prs TO review"}), &saved)
	if !saved.Replaced || saved.ID != prs {
		t.Fatalf("same name again %+v, want a replace of %s", saved, prs)
	}

	var list webproto.SavedViewList
	webResData(t, r.call(webproto.OpViewList, &webproto.EmptyArgs{}), &list)
	if list.Max != webproto.MaxSavedViews || list.MaxPinned != webproto.MaxPinnedViews || len(list.Items) != 2 {
		t.Fatalf("list %+v", list)
	}
	if first := list.Items[0]; first.ID != prs || !first.Pinned || first.Order != 1 || first.Spec.View != "git.log" || first.Name != "prs TO review" {
		t.Errorf("the replaced view lost its pin or kept its old spec: %+v", first)
	}
	if list.Items[0].Saved == 0 {
		t.Error("no save time")
	}

	webResData(t, r.call(webproto.OpViewPin, &webproto.ViewPinArgs{ID: ci, On: true, Order: 1}), &list)
	if list.Items[0].ID != ci || list.Items[0].Order != 1 || list.Items[1].ID != prs || list.Items[1].Order != 2 {
		t.Errorf("pin at 1 did not move it first: %+v", list.Items)
	}
	webResData(t, r.call(webproto.OpViewPin, &webproto.ViewPinArgs{ID: ci, On: false}), &list)
	if list.Items[0].ID != prs || list.Items[0].Order != 1 || list.Items[1].Pinned || list.Items[1].Order != 0 {
		t.Errorf("after unpin %+v", list.Items)
	}

	webResData(t, r.call(webproto.OpViewRename, &webproto.ViewRenameArgs{ID: ci, Name: "CI · shop"}), &list)
	if list.Items[1].Name != "CI · shop" {
		t.Errorf("rename %+v", list.Items)
	}
	if f := r.call(webproto.OpViewRename, &webproto.ViewRenameArgs{ID: ci, Name: "PRS to review"}); webErrCode(f) != "conflict" {
		t.Errorf("rename onto another view's name: %v", f)
	}

	webResData(t, r.call(webproto.OpViewDelete, &webproto.ViewRefArgs{ID: ci}), &list)
	if len(list.Items) != 1 || list.Items[0].ID != prs {
		t.Errorf("after delete %+v", list.Items)
	}
	for op, args := range map[string]any{
		webproto.OpViewDelete: &webproto.ViewRefArgs{ID: ci},
		webproto.OpViewRename: &webproto.ViewRenameArgs{ID: ci, Name: "x"},
		webproto.OpViewPin:    &webproto.ViewPinArgs{ID: ci, On: true},
	} {
		if f := r.call(op, args); webErrCode(f) != "not_found" {
			t.Errorf("%s on a view that is gone: %v", op, f)
		}
	}
}

// Saved views are specs only and live on the laptop: views.json holds
// no rows, survives a restart (a new hub reads the same list) and is
// 0600; a broken entry is dropped on load, never run.
func TestWebSavedViewsFile(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	r := newWebFilesRig(t)
	webResData(t, r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("pr.list"), Name: "PRs", Pin: true}), &webproto.ViewSaved{})

	path := filepath.Join(cfg, "termocode", "views.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("views.json mode %v, want 0600", st.Mode().Perm())
	}
	for _, no := range []string{`"rows"`, `"items"`, `"block"`} {
		if strings.Contains(string(b), no) {
			t.Errorf("views.json holds data (%s): %s", no, b)
		}
	}

	r2 := newWebFilesRig(t)
	var list webproto.SavedViewList
	webResData(t, r2.call(webproto.OpViewList, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != 1 || list.Items[0].Name != "PRs" || !list.Items[0].Pinned {
		t.Fatalf("after a restart %+v", list.Items)
	}

	broken := `{"version":1,"views":[` +
		`{"id":"v1","name":"ok","spec":{"view":"pr.list"},"saved":1},` +
		`{"id":"v1","name":"same id","spec":{"view":"pr.list"},"saved":1},` +
		`{"id":"v2","name":"url","spec":{"view":"http.req","args":{"u":"https://x"}},"saved":1},` +
		`{"id":"BAD","name":"bad id","spec":{"view":"pr.list"},"saved":1}]}`
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	webResData(t, r2.call(webproto.OpViewList, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != 1 || list.Items[0].ID != "v1" || list.Items[0].Name != "ok" {
		t.Errorf("broken entries were kept: %+v", list.Items)
	}

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if f := r2.call(webproto.OpViewList, &webproto.EmptyArgs{}); webErrCode(f) != "internal" {
		t.Errorf("malformed views.json: %v", f)
	}
	if f := r2.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("pr.list"), Name: "x"}); webErrCode(f) != "internal" {
		t.Errorf("a save over a malformed views.json must not wipe it: %v", f)
	}
}

// At most 32 saved views and 8 pinned: one more is refused with "limit"
// and changes nothing (a save that asks to pin as the ninth is not
// saved either).
func TestWebSavedViewsCaps(t *testing.T) {
	r := newWebFilesRig(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for i := 0; i < webproto.MaxSavedViews; i++ {
		pin := i < webproto.MaxPinnedViews
		webResData(t, r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("pr.list"), Name: "view " + string(rune('a'+i%26)) + string(rune('a'+i/26)), Pin: pin}), &webproto.ViewSaved{})
	}
	if f := r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("pr.list"), Name: "one more"}); webErrCode(f) != "limit" {
		t.Errorf("the 33rd view: %v", f)
	}
	var list webproto.SavedViewList
	webResData(t, r.call(webproto.OpViewList, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != webproto.MaxSavedViews {
		t.Fatalf("%d views", len(list.Items))
	}
	last := list.Items[len(list.Items)-1]
	if last.Pinned {
		t.Fatal("the last view is pinned")
	}
	if f := r.call(webproto.OpViewPin, &webproto.ViewPinArgs{ID: last.ID, On: true}); webErrCode(f) != "limit" {
		t.Errorf("the 9th pin: %v", f)
	}
	if f := r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("git.log"), Name: last.Name, Pin: true}); webErrCode(f) != "limit" {
		t.Errorf("a save that pins as the 9th: %v", f)
	}
	webResData(t, r.call(webproto.OpViewList, &webproto.EmptyArgs{}), &list)
	if got := list.Items[len(list.Items)-1]; got.Spec.View != "pr.list" || got.Pinned {
		t.Errorf("a refused save changed the view: %+v", got)
	}
}

// Every paired phone shares the saved views: a change on one phone
// reaches every browser as ev views_saved with the new list; a viewer
// may list them but not change them.
func TestWebSavedViewsShared(t *testing.T) {
	r := newWebFilesRig(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	other := r.conn("c-2", webproto.RoleViewer)
	webResData(t, r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Spec: savedViewSpec("pr.list"), Name: "PRs", Pin: true}), &webproto.ViewSaved{})
	got := false
	for _, f := range r.takeOn(other) {
		if f["t"] == "ev" && f["kind"] == webproto.EventViewsSaved {
			d, _ := f["data"].(map[string]any)
			items, _ := d["items"].([]any)
			got = len(items) == 1
		}
	}
	if !got {
		t.Error("the other phone heard no views_saved with the new list")
	}
	if !webproto.Allowed(webproto.RoleViewer, webproto.OpViewList) || webproto.Allowed(webproto.RoleViewer, webproto.OpViewSave) {
		t.Error("a viewer lists saved views but never changes them")
	}
}

package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webViewsRig is a controller on a paired device dev-1a2b3c4d.
func webViewsRig(t *testing.T) (*webFilesRig, *webdevices.Store) {
	t.Helper()
	r := newWebFilesRig(t)
	dir, err := webDevicesDir()
	if err != nil {
		t.Fatal(err)
	}
	ds := webdevices.Open(dir)
	for _, id := range []string{"dev-1a2b3c4d", "dev-9f8e7d6c"} {
		if err := ds.Add(webdevices.Device{ID: id, Name: "Phone " + id[4:8], Role: webdevices.RoleControl, Created: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	r.c.device = "dev-1a2b3c4d"
	return r, ds
}

func webViewData[T any](t *testing.T, f map[string]any) T {
	t.Helper()
	var v T
	b, _ := json.Marshal(f["data"])
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("data %s: %v", b, err)
	}
	return v
}

// Spec (MOB-71): a saved "PRs to review" pinned on this phone survives a
// restart (a new store reads views.json and devices.json) and lists
// first in the pins; another phone sees the view but not the pin.
func TestWebViewSaveAndPinSurvive(t *testing.T) {
	r, _ := webViewsRig(t)
	spec := json.RawMessage(`{"view":"pr.list","limit":20}`)
	f := r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Name: "PRs to review", Spec: spec, Pin: true})
	if webErrCode(f) != "" {
		t.Fatalf("save: %v", f)
	}
	sv := webViewData[webproto.SavedView](t, f)
	if !webproto.ValidSavedID(sv.ID) || sv.Name != "PRs to review" {
		t.Fatalf("saved %+v", sv)
	}
	l := webViewData[webproto.ViewList](t, r.call(webproto.OpViewList, &webproto.EmptyArgs{}))
	if len(l.Views) != 1 || len(l.Pins) != 1 || l.Pins[0] != sv.ID || string(l.Views[0].Spec) != string(spec) {
		t.Fatalf("list %+v", l)
	}
	other := r.conn("c-2", webproto.RoleViewer)
	other.device = "dev-9f8e7d6c"
	l2 := webViewData[webproto.ViewList](t, r.callOn(other, webproto.OpViewList, &webproto.EmptyArgs{}))
	if len(l2.Views) != 1 || len(l2.Pins) != 0 {
		t.Fatalf("other phone %+v", l2)
	}
	// A restart: nothing in memory, the files alone.
	again, err := webViewListFor("dev-1a2b3c4d")
	if err != nil || len(again.Pins) != 1 || again.Views[0].Name != "PRs to review" {
		t.Fatalf("after restart %+v %v", again, err)
	}
}

// Spec (MOB-71): pin order and unpin; a 9th pin is limit; a view that is
// gone is not_found; a link without a paired device cannot pin.
func TestWebViewPinRules(t *testing.T) {
	r, _ := webViewsRig(t)
	var ids []string
	for i := 0; i < webproto.MaxViewPins+1; i++ {
		f := r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Name: "View " + string(rune('A'+i)), Spec: json.RawMessage(`{"view":"git.status"}`)})
		ids = append(ids, webViewData[webproto.SavedView](t, f).ID)
	}
	for _, id := range ids[:webproto.MaxViewPins] {
		if f := r.call(webproto.OpViewPin, &webproto.ViewPinArgs{ID: id, On: true, Order: webproto.MaxViewPins - 1}); webErrCode(f) != "" {
			t.Fatalf("pin: %v", f)
		}
	}
	if code := webErrCode(r.call(webproto.OpViewPin, &webproto.ViewPinArgs{ID: ids[8], On: true})); code != "limit" {
		t.Fatalf("9th pin: %s", code)
	}
	l := webViewData[webproto.ViewList](t, r.call(webproto.OpViewPin, &webproto.ViewPinArgs{ID: ids[3], On: false}))
	if len(l.Pins) != webproto.MaxViewPins-1 || strings.Contains(strings.Join(l.Pins, " "), ids[3]) {
		t.Fatalf("after unpin %v", l.Pins)
	}
	l = webViewData[webproto.ViewList](t, r.call(webproto.OpViewPin, &webproto.ViewPinArgs{ID: ids[8], On: true}))
	if l.Pins[0] != ids[8] {
		t.Fatalf("order 0 is first: %v", l.Pins)
	}
	if code := webErrCode(r.call(webproto.OpViewPin, &webproto.ViewPinArgs{ID: "v-00000000deadbeef", On: true})); code != "not_found" {
		t.Fatalf("gone view: %s", code)
	}
	guest := r.conn("c-3", webproto.RoleController)
	if code := webErrCode(r.callOn(guest, webproto.OpViewPin, &webproto.ViewPinArgs{ID: ids[0], On: true})); code != "forbidden" {
		t.Fatalf("no device: %s", code)
	}
}

// Spec (MOB-71): deleting a view takes it off every phone's Home;
// removing a device (revoke) removes its pins; the laptop's device card
// shows a device's pins, read only.
func TestWebViewDeleteAndRevoke(t *testing.T) {
	r, ds := webViewsRig(t)
	a := webViewData[webproto.SavedView](t, r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Name: "Failing CI", Spec: json.RawMessage(`{"view":"ci.checks"}`), Pin: true}))
	b := webViewData[webproto.SavedView](t, r.call(webproto.OpViewSave, &webproto.ViewSaveArgs{Name: "Lanes", Spec: json.RawMessage(`{"view":"lanes"}`), Pin: true}))
	if _, err := ds.SetPin("dev-9f8e7d6c", a.ID, true, 0); err != nil {
		t.Fatal(err)
	}
	r.m.w, r.m.h = 100, 40
	r.m.openWebDeviceCard("dev-1a2b3c4d")
	if v := r.m.confirm.View(); !strings.Contains(v, "Failing CI") || !strings.Contains(v, "Lanes") {
		t.Fatalf("device card has no pins:\n%s", v)
	}
	r.m.confirmOpen = false
	if f := r.call(webproto.OpViewDelete, &webproto.ViewIDArgs{ID: a.ID}); webErrCode(f) != "" {
		t.Fatalf("delete: %v", f)
	}
	if d, _, _ := ds.Get("dev-9f8e7d6c"); len(d.Pins) != 0 {
		t.Fatalf("other phone keeps a deleted pin: %v", d.Pins)
	}
	if code := webErrCode(r.call(webproto.OpViewDelete, &webproto.ViewIDArgs{ID: a.ID})); code != "not_found" {
		t.Fatalf("delete twice: %s", code)
	}
	if _, ok, err := ds.Remove("dev-1a2b3c4d"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if l, _ := webViewListFor("dev-1a2b3c4d"); len(l.Pins) != 0 || len(l.Views) != 1 || l.Views[0].ID != b.ID {
		t.Fatalf("after revoke %+v", l)
	}
	if !strings.Contains(strings.Join(r.m.output.Lines(webAuditChannel), "\n"), "view.delete") {
		t.Error("no audit line for view.delete")
	}
}

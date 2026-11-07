package webdevices

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func testViews(t *testing.T) (*ViewStore, *Store) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "web")
	vs := OpenViews(dir)
	at := time.Unix(1700000000, 0)
	vs.now = func() time.Time { at = at.Add(time.Second); return at }
	return vs, Open(dir)
}

func spec(view string) json.RawMessage { return json.RawMessage(`{"view":"` + view + `","limit":20}`) }

// A saved view survives a new store (a restart), newest first, in a 0600
// views.json next to devices.json.
func TestViewsSaveListSurvive(t *testing.T) {
	vs, _ := testViews(t)
	a, err := vs.Save(View{Name: "PRs to review", Spec: spec("pr.list")})
	if err != nil || !webproto.ValidSavedID(a.ID) {
		t.Fatalf("save: %+v %v", a, err)
	}
	if _, err := vs.Save(View{Name: "Failing CI", Spec: spec("ci.checks")}); err != nil {
		t.Fatal(err)
	}
	got, err := OpenViews(filepath.Dir(vs.Path())).List()
	if err != nil || len(got) != 2 || got[0].Name != "Failing CI" || got[1].ID != a.ID {
		t.Fatalf("after restart: %+v %v", got, err)
	}
	if fi, err := os.Stat(vs.Path()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi, err)
	}
}

// The same name (any case) replaces the view; an id changes that view;
// a gone id is ErrNoView.
func TestViewsDedupeByNameAndID(t *testing.T) {
	vs, _ := testViews(t)
	a, _ := vs.Save(View{Name: "PRs to review", Spec: spec("pr.list")})
	b, err := vs.Save(View{Name: "prs TO review", Spec: spec("pr.view")})
	if err != nil || b.ID != a.ID {
		t.Fatalf("same name made a new view: %+v %v", b, err)
	}
	c, err := vs.Save(View{ID: a.ID, Name: "Mine", Spec: spec("pr.list")})
	if err != nil || c.ID != a.ID || c.Name != "Mine" {
		t.Fatalf("rename: %+v %v", c, err)
	}
	if l, _ := vs.List(); len(l) != 1 {
		t.Fatalf("views %+v", l)
	}
	if _, err := vs.Save(View{ID: "v-00000000aaaaaaaa", Name: "x", Spec: spec("git.status")}); !errors.Is(err, ErrNoView) {
		t.Fatalf("gone id: %v", err)
	}
}

// At most 32 saved views: the 33rd is refused and none is dropped.
func TestViewsCap(t *testing.T) {
	vs, _ := testViews(t)
	for i := 0; i < webproto.MaxSavedViews; i++ {
		if _, err := vs.Save(View{Name: fmt.Sprintf("View %d", i), Spec: spec("git.status")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := vs.Save(View{Name: "One more", Spec: spec("git.status")}); !errors.Is(err, ErrViewsFull) {
		t.Fatalf("33rd: %v", err)
	}
	if l, _ := vs.List(); len(l) != webproto.MaxSavedViews {
		t.Fatalf("%d views", len(l))
	}
}

// A bad name or spec is refused before anything is written.
func TestViewsRefuseBadInput(t *testing.T) {
	vs, _ := testViews(t)
	for _, v := range []View{
		{Name: "", Spec: spec("git.status")},
		{Name: strings.Repeat("x", 61), Spec: spec("git.status")},
		{Name: "a\x01b", Spec: spec("git.status")},
		{Name: "ok", Spec: json.RawMessage(`[1]`)},
		{Name: "ok", Spec: json.RawMessage(`{"view":"rm -rf /"}`)},
		{Name: "ok", Spec: json.RawMessage(`{"limit":3}`)},
	} {
		if _, err := vs.Save(v); err == nil {
			t.Errorf("saved %+v", v)
		}
	}
	if _, err := os.Stat(vs.Path()); !os.IsNotExist(err) {
		t.Errorf("a refused save wrote the file: %v", err)
	}
}

// Loading drops hand-edited bad entries and duplicate ids; a damaged
// file is an empty list, kept aside on the next write.
func TestViewsValidateOnLoad(t *testing.T) {
	vs, _ := testViews(t)
	if err := os.MkdirAll(filepath.Dir(vs.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `{"version":1,"views":[
	{"id":"v-0123456789abcdef","name":"Good","spec":{"view":"git.status"}},
	{"id":"v-0123456789abcdef","name":"Twin","spec":{"view":"git.status"}},
	{"id":"../x","name":"Bad id","spec":{"view":"git.status"}},
	{"id":"v-1123456789abcdef","name":"","spec":{"view":"git.status"}},
	{"id":"v-2123456789abcdef","name":"No view","spec":{"x":1}}]}`
	if err := os.WriteFile(vs.Path(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := vs.List()
	if err != nil || len(l) != 1 || l[0].Name != "Good" {
		t.Fatalf("loaded %+v %v", l, err)
	}
	if err := os.WriteFile(vs.Path(), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := vs.List(); err != nil || len(l) != 0 {
		t.Fatalf("damaged: %+v %v", l, err)
	}
	if _, err := vs.Save(View{Name: "New", Spec: spec("git.status")}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(vs.Path() + ".bad"); err != nil || string(b) != "{nope" {
		t.Fatalf("damaged bytes not kept: %q %v", b, err)
	}
}

// Delete forgets one view; a gone one says false.
func TestViewsDelete(t *testing.T) {
	vs, _ := testViews(t)
	a, _ := vs.Save(View{Name: "A", Spec: spec("git.status")})
	if ok, err := vs.Delete(a.ID); !ok || err != nil {
		t.Fatalf("delete %v %v", ok, err)
	}
	if ok, _ := vs.Delete(a.ID); ok {
		t.Fatal("deleted twice")
	}
}

// Pins are per device, in order, at most 8; pinning again moves it;
// Unpin takes a deleted view off every device; removing the device
// (revoke) removes its pins with it.
func TestDevicePins(t *testing.T) {
	_, ds := testViews(t)
	for _, id := range []string{"dev-1", "dev-2"} {
		if err := ds.Add(dev(id, RoleControl)); err != nil {
			t.Fatal(err)
		}
	}
	id := func(i int) string { return fmt.Sprintf("v-%016x", i+1) }
	for i := 0; i < webproto.MaxViewPins; i++ {
		if ok, err := ds.SetPin("dev-1", id(i), true, 99); !ok || err != nil {
			t.Fatalf("pin %d: %v %v", i, ok, err)
		}
	}
	if _, err := ds.SetPin("dev-1", id(8), true, 0); !errors.Is(err, ErrPinsFull) {
		t.Fatalf("9th pin: %v", err)
	}
	if _, err := ds.SetPin("dev-1", id(7), true, 0); err != nil {
		t.Fatalf("move: %v", err)
	}
	d, _, _ := ds.Get("dev-1")
	if len(d.Pins) != webproto.MaxViewPins || d.Pins[0] != id(7) || d.Pins[1] != id(0) {
		t.Fatalf("pins %v", d.Pins)
	}
	if _, err := ds.SetPin("dev-2", id(0), true, 0); err != nil {
		t.Fatal(err)
	}
	if err := ds.Unpin(id(0)); err != nil {
		t.Fatal(err)
	}
	d1, _, _ := ds.Get("dev-1")
	d2, _, _ := ds.Get("dev-2")
	if strings.Contains(strings.Join(d1.Pins, " "), id(0)) || len(d2.Pins) != 0 {
		t.Fatalf("after unpin: %v %v", d1.Pins, d2.Pins)
	}
	if ok, _ := ds.SetPin("dev-none", id(1), true, 0); ok {
		t.Fatal("pinned on a missing device")
	}
	if _, ok, err := ds.Remove("dev-1"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := ds.Add(dev("dev-1", RoleControl)); err != nil {
		t.Fatal(err)
	}
	if d, _, _ := ds.Get("dev-1"); len(d.Pins) != 0 {
		t.Fatalf("a removed device's pins came back: %v", d.Pins)
	}
}

// Hand-edited pins are cleaned on read: bad ids and twins out, at most 8.
func TestDevicePinsCleanOnLoad(t *testing.T) {
	_, ds := testViews(t)
	if err := os.MkdirAll(filepath.Dir(ds.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	pins := []string{"v-0000000000000001", "v-0000000000000001", "../etc", "v-0000000000000002"}
	for i := 3; i < 12; i++ {
		pins = append(pins, fmt.Sprintf("v-%016x", i))
	}
	b, _ := json.Marshal(map[string]any{"version": 1, "devices": []map[string]any{{"id": "dev-1", "name": "P", "role": "control", "created": time.Unix(0, 0), "pins": pins}}})
	if err := os.WriteFile(ds.Path(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, _ := ds.Get("dev-1")
	if len(d.Pins) != webproto.MaxViewPins || d.Pins[0] != "v-0000000000000001" || d.Pins[1] != "v-0000000000000002" {
		t.Fatalf("pins %v", d.Pins)
	}
}

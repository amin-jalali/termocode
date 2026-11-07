package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (MOBUX-18): the phone saves a view (a spec and a name, never
// data), pins it to Home, renames and deletes it; the list is open to
// viewers, every change needs a controller and is audited, and a change
// reaches every browser as ev views_saved.
func TestSavedViewOps(t *testing.T) {
	for op, w := range map[string]struct {
		role  Role
		audit Audit
		args  string
	}{
		OpViewList:   {RoleViewer, AuditNo, `{}`},
		OpViewSave:   {RoleController, AuditYes, `{"spec":{"view":"pr.list","fields":["n","title"],"sort":[{"field":"age","desc":true}]},"name":"PRs to review","pin":true}`},
		OpViewRename: {RoleController, AuditYes, `{"id":"v1a2b","name":"Open PRs"}`},
		OpViewDelete: {RoleController, AuditYes, `{"id":"v1a2b"}`},
		OpViewPin:    {RoleController, AuditYes, `{"id":"v1a2b","on":true,"order":2}`},
	} {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.Audit != w.audit || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if _, err := DecodeArgs(op, json.RawMessage(w.args)); err != nil {
			t.Errorf("%s %s: %v", op, w.args, err)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	if p, _ := Lookup(OpViewSave); p.Rate != slow {
		t.Errorf("view.save rate %+v, want slow", p.Rate)
	}
	for op, bad := range map[string][]string{
		OpViewSave: {
			`{"spec":{"view":"pr.list"},"name":""}`,
			`{"spec":{"view":"pr.list"},"name":" padded"}`,
			`{"spec":{"view":"pr.list"},"name":"two\nlines"}`,
			`{"spec":{"view":"pr.list"},"name":"` + strings.Repeat("x", MaxViewName+1) + `"}`,
			`{"spec":{"view":""},"name":"x"}`,
			`{"spec":{"view":"http.req","args":{"u":"https://example.invalid"}},"name":"x"}`,
			`{"spec":{"view":"files","args":{"p":"/etc"}},"name":"x"}`,
			`{"spec":{"view":"pr.list"},"name":"x","rows":[["418"]]}`,
		},
		OpViewRename: {`{"id":"","name":"x"}`, `{"id":"V-1","name":"x"}`, `{"id":"v1","name":""}`},
		OpViewDelete: {`{"id":""}`, `{"id":"../views"}`, `{"id":"` + strings.Repeat("a", MaxSavedViewID+1) + `"}`},
		OpViewPin:    {`{"id":"v1","on":true,"order":9}`, `{"id":"v1","on":true,"order":-1}`, `{"id":"v1","on":false,"order":1}`},
	} {
		for _, b := range bad {
			if _, err := DecodeArgs(op, json.RawMessage(b)); err == nil {
				t.Errorf("%s %s: accepted", op, b)
			}
		}
	}
}

// A saved view read back from views.json runs only while it is still
// one the phone could have saved.
func TestValidSavedView(t *testing.T) {
	ok := SavedView{ID: "v1", Name: "PRs", Spec: ViewSpec{View: "pr.list"}, Pinned: true, Order: 1}
	if !ValidSavedView(&ok) {
		t.Fatal("a good saved view is refused")
	}
	for name, v := range map[string]SavedView{
		"no id":          {Name: "PRs", Spec: ViewSpec{View: "pr.list"}},
		"no name":        {ID: "v1", Spec: ViewSpec{View: "pr.list"}},
		"no view":        {ID: "v1", Name: "PRs"},
		"url arg":        {ID: "v1", Name: "PRs", Spec: ViewSpec{View: "pr.list", Args: map[string]string{"u": "https://x"}}},
		"order unpinned": {ID: "v1", Name: "PRs", Spec: ViewSpec{View: "pr.list"}, Order: 2},
		"order too big":  {ID: "v1", Name: "PRs", Spec: ViewSpec{View: "pr.list"}, Pinned: true, Order: MaxPinnedViews + 1},
	} {
		v := v
		if ValidSavedView(&v) {
			t.Errorf("%s: accepted", name)
		}
	}
	if ValidSavedView(nil) {
		t.Error("nil accepted")
	}
}

package webproto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// A saved view's spec is one small JSON object naming a registry view;
// the registry's own check (MOB-9) runs after the shape check.
func TestCheckViewSpec(t *testing.T) {
	for _, ok := range []string{`{"view":"git.status"}`, `{"view":"mcp.github.list_issues","limit":5}`, ` {"view":"pr.list"} `} {
		if err := CheckViewSpec(json.RawMessage(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{``, `null`, `[]`, `"git.status"`, `{"view":""}`, `{"view":"Git"}`, `{"view":"git status"}`, `{"view":"/etc/passwd"}`, `{"view":1}`, `{"limit":3}`, `{"view":"git.status"`,
		`{"view":"git.status","pad":"` + strings.Repeat("x", MaxViewSpec) + `"}`} {
		if err := CheckViewSpec(json.RawMessage(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%.40s: %v", bad, err)
		}
	}
	ViewSpecCheck = func(json.RawMessage) error { return errors.New("unknown field total; allowed: title, ci") }
	defer func() { ViewSpecCheck = nil }()
	if err := CheckViewSpec(json.RawMessage(`{"view":"pr.list"}`)); err == nil || !strings.Contains(err.Error(), "allowed: title, ci") {
		t.Errorf("registry check not used: %v", err)
	}
}

func TestViewArgsValidate(t *testing.T) {
	spec := json.RawMessage(`{"view":"pr.list"}`)
	good := []validator{
		&ViewSaveArgs{Name: "PRs to review", Spec: spec, Pin: true},
		&ViewSaveArgs{ID: "v-0a1b2c3d4e5f6071", Name: "x", Spec: spec},
		&ViewIDArgs{ID: "v-0a1b2c3d4e5f6071"},
		&ViewPinArgs{ID: "v-0a1b2c3d4e5f6071", On: true, Order: MaxViewPins - 1},
	}
	for _, a := range good {
		if err := a.validate(0); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	bad := []validator{
		&ViewSaveArgs{Name: "  ", Spec: spec},
		&ViewSaveArgs{Name: strings.Repeat("é", MaxViewName+1), Spec: spec},
		&ViewSaveArgs{Name: "x\ty", Spec: spec},
		&ViewSaveArgs{ID: "../views", Name: "x", Spec: spec},
		&ViewSaveArgs{Name: "x", Spec: json.RawMessage(`{"sql":"drop table"}`)},
		&ViewIDArgs{ID: "v-XYZ"},
		&ViewPinArgs{ID: "v-0a1b2c3d4e5f6071", On: true, Order: MaxViewPins},
		&ViewPinArgs{ID: "v-0a1b2c3d4e5f6071", Order: -1},
	}
	for _, a := range bad {
		if err := a.validate(0); err == nil {
			t.Errorf("%+v passed", a)
		}
	}
	if n := len([]rune(strings.Repeat("é", MaxViewName))); n != MaxViewName {
		t.Fatal(n)
	}
	if _, ok := CleanViewName(strings.Repeat("é", MaxViewName)); !ok {
		t.Error("60 runes refused")
	}
}

// The view ops' rows: list is a viewer read; save, delete and pin are
// controller writes with an audit line; none waits for a laptop card.
func TestViewOpRows(t *testing.T) {
	for op, role := range map[string]Role{OpViewList: RoleViewer, OpViewSave: RoleController, OpViewDelete: RoleController, OpViewPin: RoleController} {
		p, ok := Lookup(op)
		if !ok || p.Role != role || p.LaptopCard {
			t.Errorf("%s: %+v", op, p)
		}
		if role == RoleController && p.Audit != AuditYes {
			t.Errorf("%s: audit %q", op, p.Audit)
		}
	}
}

package ai

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (AH-5): SaveProfile replace=true updates one profile, keeps the
// other profiles, the top-level keys and the unknown keys of that
// profile; the file stays valid for ParseProfiles. DeleteProfile removes
// one profile only.
func TestSaveProfileReplaceAndDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	src := `{"models": {"x": 1}, "profiles": {"a": {"title": "A", "future": [1,2], "steps": [{"id": "s"}]}, "b": {"steps": [{"id": "t", "extra": true}]}}}`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Profile{Title: "A2", Steps: []ProfileStep{{ID: "s", Title: "S", Slot: SlotQuick}}}
	if err := SaveProfile(path, "a", p, false); err == nil {
		t.Fatal("replace=false must refuse a taken name")
	}
	if err := SaveProfile(path, "a", p, true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	ps, err := ParseProfiles(b, path, ProfileSourceUser, editSlots, 1)
	if err != nil || len(ps) != 2 {
		t.Fatalf("parse: %v %d", err, len(ps))
	}
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(b, &doc)
	if _, ok := doc["models"]; !ok {
		t.Error("top-level key lost")
	}
	if !strings.Contains(string(b), `"future"`) || !strings.Contains(string(b), `"extra"`) {
		t.Errorf("unknown keys lost: %s", b)
	}
	if strings.Contains(string(b), `"title": "A"`) || !strings.Contains(string(b), "A2") {
		t.Errorf("profile a not replaced: %s", b)
	}
	if err := DeleteProfile(path, "nope"); err == nil {
		t.Error("deleting a missing profile must fail")
	}
	if err := DeleteProfile(path, "a"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	ps, _ = ParseProfiles(b, path, ProfileSourceUser, editSlots, 1)
	if len(ps) != 1 || ps[0].Name != "b" || !strings.Contains(string(b), `"models"`) {
		t.Errorf("after delete: %s", b)
	}
}

// Spec (AH-5): ValidateProfile returns a *ProfileError that names the
// step and the field; the text is the one ParseProfiles shows.
func TestValidateProfileWhere(t *testing.T) {
	st := func(id string) ProfileStep { return ProfileStep{ID: id} }
	cases := []struct {
		name        string
		p           Profile
		par         int
		step, field string
	}{
		{"dup", Profile{Name: "x", Steps: []ProfileStep{st("a"), st("a")}}, 1, "a", "id"},
		{"planners", Profile{Name: "x", Steps: []ProfileStep{{ID: "a", Plan: true}, {ID: "b", Plan: true}}}, 1, "", "plan"},
		{"deps", Profile{Name: "x", Steps: []ProfileStep{st("a"), {ID: "b", Deps: []string{"a"}}}}, 1, "b", "deps"},
		{"name", Profile{Name: "default", Steps: []ProfileStep{st("a")}}, 1, "", "name"},
	}
	for _, c := range cases {
		err := ValidateProfile(c.p, editSlots, c.par)
		var pe *ProfileError
		if !errors.As(err, &pe) || pe.Step != c.step || pe.Field != c.field {
			t.Errorf("%s: %#v", c.name, err)
		}
	}
	if err := ValidateProfile(Profile{Name: "ok", Steps: []ProfileStep{st("")}}, editSlots, 1); err != nil {
		t.Errorf("empty step id is fine: %v", err)
	}
}

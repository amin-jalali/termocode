package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var editSlots = map[string]bool{SlotMain: true, SlotQuick: true, SlotLocal: true}

// Spec (F3): the example profiles.json written by AI: Edit Profiles is
// valid JSON with no comments, and it loads with the built-in slots.
func TestExampleProfilesJSONLoads(t *testing.T) {
	data := []byte(ExampleProfilesJSON)
	if strings.Contains(ExampleProfilesJSON, "//") {
		t.Fatal("example has comments")
	}
	ps, err := ParseProfiles(data, "profiles.json", ProfileSourceUser, editSlots, 1)
	if err != nil || len(ps) == 0 {
		t.Fatalf("example: %v (%d profiles)", err, len(ps))
	}
}

// Spec (F3): New Profile from Current writes the profile under a new
// name; other profiles and unknown keys of the file stay; a taken,
// built-in or bad name and a broken file are refused (nothing written).
func TestSaveProfileAs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai", "profiles.json")
	review, _ := LoadProfiles("", "", editSlots, 1).Get(ProfileReview)

	if err := SaveProfileAs(path, "mine", review); err != nil {
		t.Fatalf("new file: %v", err)
	}
	set := LoadProfiles(path, "", editSlots, 1)
	got, ok := set.Get("mine")
	if !ok || got.Source != ProfileSourceUser || len(got.Steps) != 2 || got.Steps[0].System != review.Steps[0].System {
		t.Fatalf("saved profile %+v ok=%v errs=%v", got, ok, set.Errors)
	}

	if err := os.WriteFile(path, []byte(`{"note": "keep", "profiles": {"a": {"steps": [{"id": "x"}]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveProfileAs(path, "b", review); err != nil {
		t.Fatalf("add: %v", err)
	}
	var doc map[string]json.RawMessage
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil || string(doc["note"]) != `"keep"` {
		t.Fatalf("other keys lost: %s", b)
	}
	set = LoadProfiles(path, "", editSlots, 1)
	if _, ok := set.Get("a"); !ok {
		t.Error("profile a lost")
	}
	if _, ok := set.Get("b"); !ok {
		t.Error("profile b missing")
	}

	for _, name := range []string{"a", "default", "review", "bad name", ""} {
		if err := SaveProfileAs(path, name, review); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if err := os.WriteFile(path, []byte(`{"profiles": `), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveProfileAs(path, "c", review); err == nil {
		t.Error("broken file overwritten")
	}
	if b, _ := os.ReadFile(path); string(b) != `{"profiles": ` {
		t.Errorf("broken file changed: %s", b)
	}
}

// Spec (F4): a profile's trust hash changes when its steps change, so an
// edited project profile asks again.
func TestProfileHash(t *testing.T) {
	review, _ := LoadProfiles("", "", editSlots, 1).Get(ProfileReview)
	h := ProfileHash(review)
	if h == "" || h != ProfileHash(review) {
		t.Fatalf("hash %q not stable", h)
	}
	ed := review
	ed.Steps = append([]ProfileStep(nil), review.Steps...)
	ed.Steps[0].System += " Be nice."
	if ProfileHash(ed) == h {
		t.Error("hash ignores the system prompt")
	}
}

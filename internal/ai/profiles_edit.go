package ai

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Editing the user's profiles.json (U20 AP-5): the example file that
// AI: Edit Profiles creates, AI: New Profile from Current…, and the hash
// the project-profile trust card stores.

// ExampleProfilesJSON is written when profiles.json does not exist yet:
// plain JSON (no comments), one example profile on the built-in slots.
const ExampleProfilesJSON = `{
  "profiles": {
    "second-opinion": {
      "title": "Answer, then check",
      "steps": [
        {
          "id": "answer",
          "title": "Answer",
          "slot": "main",
          "tools": { "preset": "read-only" }
        },
        {
          "id": "check",
          "title": "Check",
          "category": "quick",
          "system": "Check the answer above. Fix mistakes and keep it short.",
          "tools": { "preset": "none" },
          "output": "shown"
        }
      ]
    }
  }
}
`

// ValidProfileName reports why name can not be a new profile name ("" =
// fine): built-in names and odd characters are refused.
func ValidProfileName(name string) error {
	for _, p := range BuiltinProfiles() {
		if p.Name == name {
			return fmt.Errorf("%q is a built-in profile; pick another name", name)
		}
	}
	if !profileIDRe.MatchString(name) {
		return errors.New("a profile name may use letters, digits, '-', '_' and '.'")
	}
	return nil
}

// SaveProfileAs adds p under name to the profiles file at path (created
// when missing). Other profiles and other keys stay. A taken name or a
// broken file is an error and nothing is written.
func SaveProfileAs(path, name string, p Profile) error {
	if err := ValidProfileName(name); err != nil {
		return err
	}
	doc := map[string]json.RawMessage{}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("%s is broken (%s); fix it first", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	profiles := map[string]json.RawMessage{}
	if raw := doc["profiles"]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return fmt.Errorf("%s: \"profiles\" is not an object", path)
		}
	}
	if _, ok := profiles[name]; ok {
		return fmt.Errorf("profile %q exists already", name)
	}
	p.Name, p.Source = name, ""
	one, err := json.Marshal(p)
	if err != nil {
		return err
	}
	profiles[name] = one
	if doc["profiles"], err = json.Marshal(profiles); err != nil {
		return err
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ProfileHash identifies a profile's content (title, categories, steps
// with their prompts and tools): a trust decision is for this content.
func ProfileHash(p Profile) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// Add puts p into the set (replacing one with the same name); the demo
// shots use it for a project profile without a file.
func (s *ProfileSet) Add(p Profile) {
	if s.byName == nil {
		s.byName = map[string]Profile{}
	}
	if _, ok := s.byName[p.Name]; !ok {
		s.names = append(s.names, p.Name)
	}
	s.byName[p.Name] = p
}

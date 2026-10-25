package ai

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/cfgfile"
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
	return SaveProfile(path, name, p, false)
}

// profileKnownKeys are the keys of Profile in the file; SaveProfile
// replaces these and keeps every other key of an edited profile.
var profileKnownKeys = []string{"title", "categories", "steps", "budget_usd"}

// SaveProfile writes p under name into the profiles file at path: a
// safe read-modify-write (cfgfile: a broken file is never overwritten,
// the write is atomic). Other profiles and other top-level keys stay as
// written. replace=false refuses a taken name; replace=true updates that
// profile and keeps the keys of it that Profile does not know.
func SaveProfile(path, name string, p Profile, replace bool) error {
	if err := ValidProfileName(name); err != nil {
		return err
	}
	p.Name, p.Source = name, ""
	one, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return cfgfile.UpdateRaw(path, func(doc map[string]json.RawMessage) error {
		profiles, err := rawProfiles(path, doc)
		if err != nil {
			return err
		}
		old, taken := profiles[name]
		if taken && !replace {
			return fmt.Errorf("profile %q exists already", name)
		}
		if taken {
			one, err = mergeProfileJSON(old, one)
			if err != nil {
				return err
			}
		}
		profiles[name] = one
		doc["profiles"], err = json.Marshal(profiles)
		return err
	})
}

// DeleteProfile removes the profile name from the profiles file at path
// (other profiles and keys stay). A name that is not in the file is an
// error and nothing is written.
func DeleteProfile(path, name string) error {
	return cfgfile.UpdateRaw(path, func(doc map[string]json.RawMessage) error {
		profiles, err := rawProfiles(path, doc)
		if err != nil {
			return err
		}
		if _, ok := profiles[name]; !ok {
			return fmt.Errorf("no profile %q in %s", name, filepath.Base(path))
		}
		delete(profiles, name)
		doc["profiles"], err = json.Marshal(profiles)
		return err
	})
}

func rawProfiles(path string, doc map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	profiles := map[string]json.RawMessage{}
	if raw := doc["profiles"]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return nil, fmt.Errorf("%s: \"profiles\" is not an object", path)
		}
	}
	return profiles, nil
}

// mergeProfileJSON is the new profile object with the keys of the old
// one that Profile does not know (the new object wins on the known ones).
func mergeProfileJSON(old, fresh json.RawMessage) (json.RawMessage, error) {
	var o, n map[string]json.RawMessage
	if err := json.Unmarshal(old, &o); err != nil {
		return fresh, nil // an old value that is not an object is replaced
	}
	if err := json.Unmarshal(fresh, &n); err != nil {
		return nil, err
	}
	for _, k := range profileKnownKeys {
		delete(o, k)
	}
	for k, v := range n {
		o[k] = v
	}
	return json.Marshal(o)
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

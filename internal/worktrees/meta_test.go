package worktrees

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Metadata survives a restart: what Update wrote, Load reads back.
func TestMetaRoundTrip(t *testing.T) {
	common := t.TempDir()
	created := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	err := Update(common, func(m map[string]Meta) {
		m["/src/app.worktrees/feat"] = Meta{Name: "feat", Pinned: true, Agent: "claude", Issue: 412, Notes: []string{"adding the pin row"}, Created: created}
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(common)
	if err != nil {
		t.Fatal(err)
	}
	m := got["/src/app.worktrees/feat"]
	if m.Name != "feat" || !m.Pinned || m.Agent != "claude" || m.Issue != 412 || len(m.Notes) != 1 || !m.Created.Equal(created) {
		t.Errorf("got %+v", m)
	}
	b, _ := os.ReadFile(MetaPath(common))
	if !strings.Contains(string(b), `"version": 1`) {
		t.Errorf("file has no version: %s", b)
	}
	if MetaPath(common) != filepath.Join(common, "termocode", "worktrees.json") {
		t.Errorf("path %q", MetaPath(common))
	}
}

// No file yet is no metadata and no error; a broken file is no metadata
// and ErrBadMeta (one toast), and Update never overwrites it.
func TestMetaMissingAndBad(t *testing.T) {
	common := t.TempDir()
	if got, err := Load(common); err != nil || len(got) != 0 {
		t.Fatalf("missing: %v %v", got, err)
	}
	p := MetaPath(common)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(common)
	if !errors.Is(err, ErrBadMeta) || len(got) != 0 {
		t.Fatalf("bad: %v %v", got, err)
	}
	if err := Update(common, func(m map[string]Meta) { m["/x"] = Meta{Pinned: true} }); !errors.Is(err, ErrBadMeta) {
		t.Errorf("update over a bad file: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "{not json" {
		t.Errorf("bad file overwritten: %q", b)
	}
	// A newer version than this build knows is kept as it is too.
	if err := os.WriteFile(p, []byte(`{"version": 99, "worktrees": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(common); !errors.Is(err, ErrBadMeta) {
		t.Errorf("newer version: %v", err)
	}
}

// Update drops entries the callback emptied, so the file only holds
// what git does not know.
func TestMetaUpdateDropsEmpty(t *testing.T) {
	common := t.TempDir()
	_ = Update(common, func(m map[string]Meta) { m["/a"] = Meta{Pinned: true}; m["/b"] = Meta{Pinned: true} })
	_ = Update(common, func(m map[string]Meta) { m["/a"] = Meta{} })
	got, _ := Load(common)
	if _, ok := got["/a"]; ok || !got["/b"].Pinned {
		t.Errorf("got %+v", got)
	}
}

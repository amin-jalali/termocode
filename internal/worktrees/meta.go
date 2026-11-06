// Package worktrees is the worktree model of the WORKTREES view (WTX-1):
// the worktrees git lists, joined with what termocode keeps about them
// (name, pin, parent, agent, links, notes, sleep state) in
// <git-common-dir>/termocode/worktrees.json, and a cache of their status
// (dirty files, ahead / behind) that worker goroutines fill off the UI
// thread.
//
// Git stays the truth for path and branch; the file only adds what git
// does not know. It holds paths and short text only, never file contents
// or secrets.
package worktrees

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// MetaVersion is the version of worktrees.json this build writes.
const MetaVersion = 1

// ErrBadMeta: worktrees.json is not a file this build can read (broken
// JSON or a newer version). Load returns no metadata with it; Update
// refuses to write over it.
var ErrBadMeta = errors.New("worktrees.json could not be read")

// Meta is what termocode knows about one worktree beyond git.
type Meta struct {
	Name     string    `json:"name,omitempty"`
	Pinned   bool      `json:"pinned,omitempty"`
	Parent   string    `json:"parent,omitempty"` // the worktree it was made from (path)
	Agent    string    `json:"agent,omitempty"`  // the agent preset started in it
	Issue    int       `json:"issue,omitempty"`
	PR       int       `json:"pr,omitempty"`
	Notes    []string  `json:"notes,omitempty"`
	State    string    `json:"state,omitempty"` // "" | asleep | archived
	Created  time.Time `json:"created,omitzero"`
	LastSeen time.Time `json:"last_seen,omitzero"`
}

// States of Meta.State.
const (
	StateAsleep   = "asleep"
	StateArchived = "archived"
)

type metaFile struct {
	Version   int             `json:"version"`
	Worktrees map[string]Meta `json:"worktrees"`
}

// MetaPath is the metadata file of the repository with git common dir
// common.
func MetaPath(common string) string { return filepath.Join(common, "termocode", "worktrees.json") }

// metaMu makes Update a read-change-write of this process at a time.
var metaMu sync.Mutex

// Load reads the metadata by worktree path. A missing file is empty.
func Load(common string) (map[string]Meta, error) {
	b, err := os.ReadFile(MetaPath(common))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Meta{}, nil
	}
	if err != nil {
		return map[string]Meta{}, err
	}
	var f metaFile
	if err := json.Unmarshal(b, &f); err != nil {
		return map[string]Meta{}, fmt.Errorf("%w: %v", ErrBadMeta, err)
	}
	if f.Version > MetaVersion {
		return map[string]Meta{}, fmt.Errorf("%w: version %d is newer than this termocode", ErrBadMeta, f.Version)
	}
	if f.Worktrees == nil {
		f.Worktrees = map[string]Meta{}
	}
	return f.Worktrees, nil
}

// Update reads the metadata, lets fn change it and writes it back
// atomically. Entries fn leaves empty are dropped.
func Update(common string, fn func(map[string]Meta)) error {
	metaMu.Lock()
	defer metaMu.Unlock()
	m, err := Load(common)
	if err != nil {
		return err
	}
	fn(m)
	for p, v := range m {
		if reflect.ValueOf(v).IsZero() {
			delete(m, p)
		}
	}
	b, err := json.MarshalIndent(metaFile{Version: MetaVersion, Worktrees: m}, "", "  ")
	if err != nil {
		return err
	}
	return cfgfile.WriteFile(MetaPath(common), append(b, '\n'))
}

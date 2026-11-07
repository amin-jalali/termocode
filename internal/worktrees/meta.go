// Package worktrees is the worktree model of the WORKTREES view (WTX-1):
// the worktrees git lists, joined with what termocode keeps about them
// (name, pin, order, parent, agent, links, notes, sleep state, port) in
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
	Order    int       `json:"order,omitempty"`  // the user's place in its group, 1 first; 0 = the automatic sort (CMP-27)
	Parent   string    `json:"parent,omitempty"` // the worktree it was made from (path)
	Base     string    `json:"base,omitempty"`   // a stacked child: the parent's branch when it was made (CMP-17)
	Agent    string    `json:"agent,omitempty"`  // the agent preset started in it
	Issue    int       `json:"issue,omitempty"`
	PR       int       `json:"pr,omitempty"`
	Notes    []Note    `json:"notes,omitempty"`  // agent notes, oldest first (notes.go)
	State    string    `json:"state,omitempty"`  // "" | asleep | archived
	Branch   string    `json:"branch,omitempty"` // an archived worktree's branch (git no longer lists it)
	Created  time.Time `json:"created,omitzero"`
	LastSeen time.Time `json:"last_seen,omitzero"`

	Checkpoint   string    `json:"checkpoint,omitempty"` // working | blocked | done, set by an agent
	CheckpointAt time.Time `json:"checkpoint_at,omitzero"`

	Link LinkInfo `json:"link,omitzero"` // what gh last said about Issue / PR (WTX-9)

	Adopted bool `json:"adopted,omitempty"` // an external worktree the user adopted (WTX-10)

	Port      int      `json:"port,omitempty"`       // the dev server port of the worktree (CMP-5, ports.go)
	BlockedBy []string `json:"blocked_by,omitempty"` // the worktrees (paths) whose work this one waits for (CMP-80, deps.go)

	// The PR follow-up loop (CMP-31): Follow asks to send new PR
	// feedback to the worktree's agent; FollowSeen are the comment ids
	// sent or skipped (newest last, capped), FollowRun the failed CI run
	// sent or skipped last.
	Follow     bool    `json:"follow,omitempty"`
	FollowSeen []int64 `json:"follow_seen,omitempty"`
	FollowRun  int64   `json:"follow_run,omitempty"`
}

// MaxFollowSeen caps Meta.FollowSeen.
const MaxFollowSeen = 500

// MarkFollowSeen adds ids to FollowSeen (no doubles, oldest dropped
// past MaxFollowSeen) and sets FollowRun when run > 0.
func (m *Meta) MarkFollowSeen(ids []int64, run int64) {
	have := make(map[int64]bool, len(m.FollowSeen))
	for _, id := range m.FollowSeen {
		have[id] = true
	}
	for _, id := range ids {
		if !have[id] {
			have[id] = true
			m.FollowSeen = append(m.FollowSeen, id)
		}
	}
	if n := len(m.FollowSeen) - MaxFollowSeen; n > 0 {
		m.FollowSeen = append([]int64(nil), m.FollowSeen[n:]...)
	}
	if run > 0 {
		m.FollowRun = run
	}
}

// FollowSeenSet is FollowSeen as a set.
func (m Meta) FollowSeenSet() map[int64]bool {
	out := make(map[int64]bool, len(m.FollowSeen))
	for _, id := range m.FollowSeen {
		out[id] = true
	}
	return out
}

// Empty: termocode knows nothing about the worktree.
func (m Meta) Empty() bool { return reflect.ValueOf(m).IsZero() }

// LinkInfo is the last read of a worktree's linked issue or PR.
type LinkInfo struct {
	Title   string    `json:"title,omitempty"`
	State   string    `json:"state,omitempty"` // open, closed, merged
	CI      string    `json:"ci,omitempty"`    // pull requests: pass, fail, pending or ""
	URL     string    `json:"url,omitempty"`
	Checked time.Time `json:"checked,omitzero"`
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

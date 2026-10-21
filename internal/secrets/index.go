package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry kinds.
const (
	KindValue = "value" // the value is in the backend
	KindRef   = "ref"   // Ref is resolved on use; nothing in the backend
)

// Index is secrets.json: names, kinds and references. It never holds a
// value, so it may be read by anyone who can read the config dir.
type Index struct {
	Version int              `json:"version"`
	Backend string           `json:"backend,omitempty"`
	Entries map[string]Entry `json:"entries"`
}

// Entry is one secret's non-secret facts.
type Entry struct {
	Kind    string    `json:"kind"`
	Ref     string    `json:"ref,omitempty"`
	Backend string    `json:"backend,omitempty"`
	Updated time.Time `json:"updated"`
}

const indexVersion = 1

// loadIndex reads secrets.json. Missing → empty; damaged → ErrCorrupt.
func loadIndex(path string) (Index, error) {
	idx := Index{Version: indexVersion, Entries: map[string]Entry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return idx, nil
	}
	if err != nil {
		return idx, err
	}
	if err := json.Unmarshal(b, &idx); err != nil {
		return Index{}, fmt.Errorf("%w: %s is not valid JSON", ErrCorrupt, path)
	}
	if idx.Entries == nil {
		idx.Entries = map[string]Entry{}
	}
	return idx, nil
}

// updateIndex re-reads the index, applies f and writes it back, so two
// UIs writing different names never drop each other's (last writer wins
// per name). A damaged index is never overwritten.
func updateIndex(path string, f func(*Index)) error {
	idx, err := loadIndex(path)
	if err != nil {
		return err
	}
	f(&idx)
	idx.Version = indexVersion
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), false)
}

// renameFn is os.Rename; tests swap it to prove a failed write keeps the
// old file.
var renameFn = os.Rename

// writeFileAtomic writes data to path through a temp file in the same
// dir and a rename, with mode 0600. A missing dir is made 0700;
// tightenDir also sets an existing dir to 0700 (the ai/ dir, as
// SaveCredentials does).
func writeFileAtomic(path string, data []byte, tightenDir bool) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if tightenDir {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameFn(name, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// Entries reads the entries of secrets.json in dir: names, kinds and
// reference texts, never a value. A missing or damaged index gives none.
func Entries(dir string) map[string]Entry {
	idx, err := loadIndex(filepath.Join(dir, IndexFile))
	if err != nil {
		return nil
	}
	return idx.Entries
}

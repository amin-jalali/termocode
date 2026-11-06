// Package localhist keeps a copy of each file the user saves (GITX-35),
// so an older version can be compared, restored, or a deleted file
// recovered, also when git never saw it.
//
// Layout under Store.Dir, one folder per file (the folder name is a hash
// of the absolute path):
//
//	<hash>/path          the file's absolute path
//	<hash>/<unixnano>    one version (the file's bytes at that save)
//	<hash>/labels.json   {"<unixnano>": "label"}
//
// The package only reads and writes its own folder; it never touches the
// user's files.
package localhist

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Defaults of a Store with zero fields.
const (
	DefaultMaxPerFile = 50
	DefaultMaxBytes   = 2 << 20
	DefaultMaxAge     = 30 * 24 * time.Hour
)

// Store is the history folder.
type Store struct {
	Dir        string
	MaxPerFile int           // versions kept per file (0: DefaultMaxPerFile)
	MaxBytes   int64         // bigger files are not kept (0: DefaultMaxBytes)
	MaxAge     time.Duration // older versions go at the next save (0: DefaultMaxAge)
}

// Version is one kept copy.
type Version struct {
	ID    string // opaque; pass it to Read
	At    time.Time
	Size  int64
	Label string
}

// Entry is a file the history knows.
type Entry struct {
	Path string
	Last time.Time // the newest version
}

func (s *Store) maxPerFile() int {
	if s.MaxPerFile > 0 {
		return s.MaxPerFile
	}
	return DefaultMaxPerFile
}

func (s *Store) maxBytes() int64 {
	if s.MaxBytes > 0 {
		return s.MaxBytes
	}
	return DefaultMaxBytes
}

func (s *Store) maxAge() time.Duration {
	if s.MaxAge > 0 {
		return s.MaxAge
	}
	return DefaultMaxAge
}

// fileDir is the folder of one file's versions.
func (s *Store) fileDir(abs string) string {
	h := sha256.Sum256([]byte(filepath.Clean(abs)))
	return filepath.Join(s.Dir, hex.EncodeToString(h[:12]))
}

// Save keeps data as a new version of abs. saved is false when data is
// the newest version already or bigger than MaxBytes.
func (s *Store) Save(abs string, data []byte, now time.Time) (saved bool, err error) {
	id, saved, err := s.save(abs, data, now)
	_ = id
	return saved, err
}

func (s *Store) save(abs string, data []byte, now time.Time) (id string, saved bool, err error) {
	if !filepath.IsAbs(abs) {
		return "", false, fmt.Errorf("localhist: %q is not an absolute path", abs)
	}
	if int64(len(data)) > s.maxBytes() {
		return "", false, nil
	}
	dir := s.fileDir(abs)
	vs, _ := s.Versions(abs)
	if len(vs) > 0 {
		if last, err := s.Read(abs, vs[0].ID); err == nil && bytes.Equal(last, data) {
			return vs[0].ID, false, nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(filepath.Join(dir, "path"), []byte(filepath.Clean(abs)), 0o600); err != nil {
		return "", false, err
	}
	n := now.UnixNano()
	if len(vs) > 0 {
		if prev, _ := strconv.ParseInt(vs[0].ID, 10, 64); n <= prev {
			n = prev + 1 // keep the order when the clock did not move
		}
	}
	id = strconv.FormatInt(n, 10)
	tmp := filepath.Join(dir, id+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", false, err
	}
	if err := os.Rename(tmp, filepath.Join(dir, id)); err != nil {
		_ = os.Remove(tmp)
		return "", false, err
	}
	s.prune(abs, now)
	return id, true, nil
}

// prune drops the versions over the cap and the too old ones; the
// newest version always stays.
func (s *Store) prune(abs string, now time.Time) {
	vs, err := s.Versions(abs)
	if err != nil {
		return
	}
	labels := s.labels(abs)
	changed := false
	for i, v := range vs {
		if i == 0 {
			continue
		}
		if i >= s.maxPerFile() || now.Sub(v.At) > s.maxAge() {
			_ = os.Remove(filepath.Join(s.fileDir(abs), v.ID))
			if _, ok := labels[v.ID]; ok {
				delete(labels, v.ID)
				changed = true
			}
		}
	}
	if changed {
		_ = s.writeLabels(abs, labels)
	}
}

// Versions lists the kept versions of abs, newest first.
func (s *Store) Versions(abs string) ([]Version, error) {
	dir := s.fileDir(abs)
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	labels := s.labels(abs)
	var out []Version
	for _, e := range ents {
		n, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Version{ID: e.Name(), At: time.Unix(0, n).UTC(), Size: info.Size(), Label: labels[e.Name()]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// Read returns the bytes of one version.
func (s *Store) Read(abs, id string) ([]byte, error) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return nil, fmt.Errorf("localhist: bad version id %q", id)
	}
	return os.ReadFile(filepath.Join(s.fileDir(abs), id))
}

// Label marks data with a label: the newest version when it has the same
// bytes, else a new version.
func (s *Store) Label(abs string, data []byte, label string, now time.Time) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return errors.New("localhist: empty label")
	}
	id, _, err := s.save(abs, data, now)
	if err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("localhist: the file is over %d MB and is not kept", s.maxBytes()>>20)
	}
	labels := s.labels(abs)
	labels[id] = label
	return s.writeLabels(abs, labels)
}

func (s *Store) labels(abs string) map[string]string {
	out := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(s.fileDir(abs), "labels.json")); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (s *Store) writeLabels(abs string, labels map[string]string) error {
	b, err := json.Marshal(labels)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.fileDir(abs), "labels.json"), b, 0o600)
}

// Files lists every file the history knows below root.
func (s *Store) Files(root string) ([]Entry, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	var out []Entry
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name(), "path"))
		if err != nil {
			continue
		}
		p := string(b)
		if rel, err := filepath.Rel(root, p); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		vs, _ := s.Versions(p)
		if len(vs) == 0 {
			continue
		}
		out = append(out, Entry{Path: p, Last: vs[0].At})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out, nil
}

// Deleted lists the files below root that the history knows and that are
// gone from the disk, the most recently saved first.
func (s *Store) Deleted(root string) ([]Entry, error) {
	all, err := s.Files(root)
	var out []Entry
	for _, e := range all {
		if _, err := os.Lstat(e.Path); errors.Is(err, os.ErrNotExist) {
			out = append(out, e)
		}
	}
	return out, err
}

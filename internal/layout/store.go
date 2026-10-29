package layout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Source says where a layout comes from.
type Source int

const (
	// Builtin layouts ship with termocode and cannot be deleted.
	Builtin Source = iota
	// User layouts live in the user's config folder: for every project.
	User
	// Project layouts live in .termocode/layouts: shared with the project.
	Project
)

// String is the word the picker shows.
func (s Source) String() string {
	switch s {
	case Project:
		return "project"
	case User:
		return "user"
	}
	return "built-in"
}

// Ext is the file extension of a layout file.
const Ext = ".jsonc"

// Entry is a layout together with where it was found.
type Entry struct {
	Layout
	Source Source
	// Path is the file of a User or Project layout; empty for a Builtin.
	Path string
}

// Broken is a layout file that could not be read.
type Broken struct {
	Path string
	Err  error
}

// Store reads and writes layout files. Both folders may be empty (then that
// source is off) or missing on disk (then it has no layouts yet).
type Store struct {
	UserDir    string // <config>/termocode/layouts
	ProjectDir string // <project>/.termocode/layouts
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,39}$`)

// ValidName says why name cannot be a layout name (it is a file name).
func ValidName(name string) error {
	if !nameRE.MatchString(name) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return errors.New("use letters, digits, space, - _ . (up to 40 characters, starting with a letter or digit)")
	}
	return nil
}

func (s Store) dir(src Source) (string, error) {
	var d string
	switch src {
	case User:
		d = s.UserDir
	case Project:
		d = s.ProjectDir
	}
	if d == "" {
		return "", fmt.Errorf("no folder for %s layouts", src)
	}
	return d, nil
}

func (s Store) read(dir string, src Source) (out []Entry, broken []Broken) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, f := range files {
		name, ok := strings.CutSuffix(f.Name(), Ext)
		if !ok || f.IsDir() || ValidName(name) != nil {
			continue
		}
		p := filepath.Join(dir, f.Name())
		b, err := os.ReadFile(p)
		if err == nil {
			var l Layout
			if l, err = Parse(b); err == nil {
				l.Name = name
				out = append(out, Entry{Layout: l, Source: src, Path: p})
				continue
			}
		}
		broken = append(broken, Broken{Path: p, Err: err})
	}
	return out, broken
}

// List is every layout, sorted by name. When the project and the user both
// have a name, the project's wins. Files that cannot be read are returned
// in broken, never silently dropped.
func (s Store) List() (entries []Entry, broken []Broken) {
	byName := map[string]Entry{}
	for _, src := range []Source{User, Project} { // later wins
		d, err := s.dir(src)
		if err != nil {
			continue
		}
		es, bs := s.read(d, src)
		broken = append(broken, bs...)
		for _, e := range es {
			byName[e.Name] = e
		}
	}
	for _, e := range byName {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if a != b {
			return a < b
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, broken
}

// Find is the layout called name, as List would show it.
func (s Store) Find(name string) (Entry, bool) {
	es, _ := s.List()
	for _, e := range es {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// Save writes l (named l.Name) into the folder of src. The write is atomic:
// a crash never leaves half a file.
func (s Store) Save(src Source, l Layout) (string, error) {
	if err := ValidName(l.Name); err != nil {
		return "", err
	}
	d, err := s.dir(src)
	if err != nil {
		return "", err
	}
	body, err := l.Marshal()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(d, l.Name+Ext)
	tmp, err := os.CreateTemp(d, ".layout-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp.Name(), p)
}

// Delete removes the file of e. A built-in layout cannot be deleted.
func (s Store) Delete(e Entry) error {
	if e.Source == Builtin || e.Path == "" {
		return fmt.Errorf("%s is a built-in layout and cannot be deleted", e.Name)
	}
	return os.Remove(e.Path)
}

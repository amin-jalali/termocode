package webdevices

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Saved views (MOB-71): the view specs the user named on a phone, in
// views.json next to devices.json (0600 in the same 0700 folder), shared
// by every paired phone. Specs only, never data, never a secret: a spec
// names a registry view and its fields, filters and args (the phone
// refuses secret-looking args before they get here). Each device's Home
// pins are on its Device (Pins), so removing a device removes its pins.
// Every change is read-change-write under a file lock, written
// atomically; a damaged file is kept aside as views.json.bad.

// Errors of saved views.
var (
	ErrViewsFull = errors.New("too many saved views; delete one first")
	ErrPinsFull  = errors.New("too many pinned views on this device; unpin one first")
	ErrNoView    = errors.New("no such saved view")
)

// View is one saved view.
type View struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Spec    json.RawMessage `json:"spec"`
	Created time.Time       `json:"created"`
	Updated time.Time       `json:"updated"`
}

type viewsFile struct {
	Version int    `json:"version"`
	Views   []View `json:"views"`
}

// ViewStore is views.json in one folder.
type ViewStore struct {
	dir string
	now func() time.Time
}

var viewsMu sync.Mutex

// OpenViews is the saved views store in dir (the devices folder).
func OpenViews(dir string) *ViewStore { return &ViewStore{dir: dir, now: time.Now} }

// Path is the views file.
func (s *ViewStore) Path() string { return filepath.Join(s.dir, "views.json") }

// cleanViews keeps the valid views, once each by id, newest first, at
// most MaxSavedViews: a hand-edited or damaged entry is dropped on load.
func cleanViews(in []View) []View {
	seen := map[string]bool{}
	var out []View
	for _, v := range in {
		name, ok := webproto.CleanViewName(v.Name)
		if !ok || !webproto.ValidSavedID(v.ID) || seen[v.ID] || webproto.CheckViewSpec(v.Spec) != nil {
			continue
		}
		seen[v.ID] = true
		v.Name = name
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) > webproto.MaxSavedViews {
		out = out[:webproto.MaxSavedViews]
	}
	return out
}

// read returns the views and whether the file was damaged.
func (s *ViewStore) read() ([]View, bool, error) {
	b, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var f viewsFile
	if json.Unmarshal(b, &f) != nil {
		return nil, true, nil
	}
	return cleanViews(f.Views), false, nil
}

// List reads the saved views, newest first. A missing or damaged file is
// an empty list.
func (s *ViewStore) List() ([]View, error) {
	vs, _, err := s.read()
	return vs, err
}

// Get finds a saved view by id.
func (s *ViewStore) Get(id string) (View, bool, error) {
	vs, err := s.List()
	if err != nil {
		return View{}, false, err
	}
	for _, v := range vs {
		if v.ID == id {
			return v, true, nil
		}
	}
	return View{}, false, nil
}

// update changes the list under the lock.
func (s *ViewStore) update(fn func([]View) ([]View, error)) error {
	viewsMu.Lock()
	defer viewsMu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(s.dir, 0o700)
	unlock, err := lockFile(filepath.Join(s.dir, "views.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	vs, bad, err := s.read()
	if err != nil {
		return err
	}
	if bad {
		if b, err := os.ReadFile(s.Path()); err == nil {
			_ = writeAtomic(s.Path()+".bad", b)
		}
	}
	out, err := fn(append([]View(nil), vs...))
	if err != nil {
		return err
	}
	out = cleanViews(out)
	b, err := json.MarshalIndent(viewsFile{Version: 1, Views: out}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.Path(), append(b, '\n'))
}

func newViewID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "v-" + hex.EncodeToString(b[:]), nil
}

// Save keeps v. With an ID it changes that view (ErrNoView: gone);
// without, a view of the same name (any case) is replaced, else a new
// one is made (ErrViewsFull past MaxSavedViews). It returns the view as
// stored.
func (s *ViewStore) Save(v View) (View, error) {
	name, ok := webproto.CleanViewName(v.Name)
	if !ok {
		return View{}, errors.New("bad view name")
	}
	if err := webproto.CheckViewSpec(v.Spec); err != nil {
		return View{}, err
	}
	v.Name = name
	now := s.now().UTC()
	var got View
	err := s.update(func(vs []View) ([]View, error) {
		at := -1
		for i, o := range vs {
			if (v.ID != "" && o.ID == v.ID) || (v.ID == "" && strings.EqualFold(o.Name, v.Name)) {
				at = i
				break
			}
		}
		switch {
		case at >= 0:
			vs[at].Name, vs[at].Spec, vs[at].Updated = v.Name, v.Spec, now
			got = vs[at]
			return vs, nil
		case v.ID != "":
			return nil, ErrNoView
		case len(vs) >= webproto.MaxSavedViews:
			return nil, ErrViewsFull
		}
		id, err := newViewID()
		if err != nil {
			return nil, err
		}
		got = View{ID: id, Name: v.Name, Spec: v.Spec, Created: now, Updated: now}
		return append(vs, got), nil
	})
	return got, err
}

// Delete forgets a saved view; false: no such view.
func (s *ViewStore) Delete(id string) (bool, error) {
	found := false
	err := s.update(func(vs []View) ([]View, error) {
		out := vs[:0]
		for _, v := range vs {
			if v.ID == id {
				found = true
				continue
			}
			out = append(out, v)
		}
		return out, nil
	})
	return found, err
}

// SetPin pins (on) or unpins view id on device dev's Home at order (past
// the end: last). Pinning a pinned view moves it. false: no such device.
func (s *Store) SetPin(dev, id string, on bool, order int) (bool, error) {
	var full bool
	found, err := s.Change(dev, func(d *Device) {
		pins := make([]string, 0, len(d.Pins)+1)
		for _, p := range d.Pins {
			if p != id {
				pins = append(pins, p)
			}
		}
		if on {
			if len(pins) >= webproto.MaxViewPins {
				full = true
				return
			}
			if order < 0 || order > len(pins) {
				order = len(pins)
			}
			pins = append(pins[:order], append([]string{id}, pins[order:]...)...)
		}
		d.Pins = pins
	})
	if err == nil && full {
		return found, ErrPinsFull
	}
	return found, err
}

// Unpin removes view id from every device's pins (the view was deleted).
func (s *Store) Unpin(id string) error {
	return s.Update(func(ds []Device) ([]Device, error) {
		for i := range ds {
			out := ds[i].Pins[:0]
			for _, p := range ds[i].Pins {
				if p != id {
					out = append(out, p)
				}
			}
			ds[i].Pins = out
			if len(out) == 0 {
				ds[i].Pins = nil
			}
		}
		return ds, nil
	})
}

// cleanPins keeps well-formed ids, once each, at most MaxViewPins.
func cleanPins(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range in {
		if webproto.ValidSavedID(p) && !seen[p] && len(out) < webproto.MaxViewPins {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

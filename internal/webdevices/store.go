// Package webdevices is the list of browsers paired with the web panel
// (WEB-32): phones and tablets on the user's tailnet that signed in with
// a QR code and a passkey. It holds no secret: names, roles, credential
// ids and passkey public keys only. The file is devices.json, 0600 in a
// 0700 folder next to config.json; the gateway adds devices and keeps
// their sign counts, the UI removes them and changes their roles. Every
// change is read-change-write under a file lock, written atomically.
//
// Secret material (the optional TOTP seed, WEB-48) never comes here: it
// lives in the secret store (internal/secrets).
package webdevices

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Roles of a paired device (web design 3.4).
const (
	RoleViewer  = "viewer"  // reads only
	RoleControl = "control" // may type, edit and answer cards
)

// MaxDevices caps the list.
const MaxDevices = 32

// maxName caps a device name, in runes.
const maxName = 48

// Errors.
var (
	ErrFull      = errors.New("too many paired devices; remove one first")
	ErrDuplicate = errors.New("a device with this id is already paired")
)

// Device is one paired browser.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
	// Guest marks a device shared with another person (WEB-50); always a
	// viewer.
	Guest bool `json:"guest,omitempty"`
	// The passkey (WEB-33): its id, COSE public key, sign count and
	// authenticator model; UserID is the random WebAuthn user handle and
	// RPID the tailnet name it was made for.
	CredID    []byte `json:"cred_id,omitempty"`
	PublicKey []byte `json:"public_key,omitempty"`
	SignCount uint32 `json:"sign_count,omitempty"`
	AAGUID    []byte `json:"aaguid,omitempty"`
	UserID    []byte `json:"user_id,omitempty"`
	RPID      string `json:"rp_id,omitempty"`
	// NeedsRepair: the passkey looked cloned (its sign count went back);
	// the device cannot sign in until it pairs again.
	NeedsRepair bool      `json:"needs_repair,omitempty"`
	Created     time.Time `json:"created"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
}

// file is the JSON on disk.
type file struct {
	Version int      `json:"version"`
	Devices []Device `json:"devices"`
}

// Store is devices.json in one folder.
type Store struct {
	dir string
}

// procMu serializes changes inside this process; the file lock does it
// between processes.
var procMu sync.Mutex

// Open is the store in dir (made 0700 on the first write).
func Open(dir string) *Store { return &Store{dir: dir} }

// Path is the devices file.
func (s *Store) Path() string { return filepath.Join(s.dir, "devices.json") }

// List reads the devices. A missing or damaged file is an empty list.
func (s *Store) List() ([]Device, error) {
	ds, _, err := s.read()
	return ds, err
}

// Get finds a device by id.
func (s *Store) Get(id string) (Device, bool, error) {
	ds, err := s.List()
	if err != nil {
		return Device{}, false, err
	}
	for _, d := range ds {
		if d.ID == id {
			return d, true, nil
		}
	}
	return Device{}, false, nil
}

// read returns the devices and whether the file was damaged.
func (s *Store) read() ([]Device, bool, error) {
	b, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var f file
	if json.Unmarshal(b, &f) != nil {
		return nil, true, nil
	}
	return f.Devices, false, nil
}

// Update changes the list under the lock: fn gets the devices and returns
// the new list (or an error, and nothing changes).
func (s *Store) Update(fn func([]Device) ([]Device, error)) error {
	procMu.Lock()
	defer procMu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(s.dir, 0o700)
	unlock, err := lockFile(filepath.Join(s.dir, "devices.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	ds, bad, err := s.read()
	if err != nil {
		return err
	}
	if bad { // keep the damaged bytes aside, start over
		if b, err := os.ReadFile(s.Path()); err == nil {
			_ = writeAtomic(s.Path()+".bad", b)
		}
	}
	out, err := fn(append([]Device(nil), ds...))
	if err != nil {
		return err
	}
	if len(out) > MaxDevices {
		return ErrFull
	}
	b, err := json.MarshalIndent(file{Version: 1, Devices: out}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.Path(), append(b, '\n'))
}

// Add pairs a new device.
func (s *Store) Add(d Device) error {
	d.Name = CleanName(d.Name)
	return s.Update(func(ds []Device) ([]Device, error) {
		for _, o := range ds {
			if o.ID == d.ID {
				return nil, ErrDuplicate
			}
		}
		if len(ds) >= MaxDevices {
			return nil, ErrFull
		}
		return append(ds, d), nil
	})
}

// Remove forgets a device (revoke) and returns it.
func (s *Store) Remove(id string) (Device, bool, error) {
	var got Device
	var ok bool
	err := s.Update(func(ds []Device) ([]Device, error) {
		out := ds[:0]
		for _, d := range ds {
			if d.ID == id {
				got, ok = d, true
				continue
			}
			out = append(out, d)
		}
		return out, nil
	})
	return got, ok, err
}

// Change runs fn on one device under the lock; false: no such device.
func (s *Store) Change(id string, fn func(*Device)) (bool, error) {
	found := false
	err := s.Update(func(ds []Device) ([]Device, error) {
		for i := range ds {
			if ds[i].ID == id {
				fn(&ds[i])
				ds[i].Name = CleanName(ds[i].Name)
				found = true
			}
		}
		return ds, nil
	})
	return found, err
}

// ForgetAll removes every device.
func (s *Store) ForgetAll() error {
	return s.Update(func([]Device) ([]Device, error) { return nil, nil })
}

// writeAtomic writes b to path through a 0600 temp file and a rename.
func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".devices-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save devices: %w", err)
	}
	return nil
}

// CleanName makes a browser-given name safe to show and store: controls
// and bidi marks out, whitespace runs to one space, at most maxName runes.
func CleanName(s string) string {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
			continue
		case unicode.IsControl(r) || isBidi(r):
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := []rune(b.String())
	if len(out) == 0 {
		return "Browser"
	}
	if len(out) > maxName {
		out = append(out[:maxName-1], '…')
	}
	return string(out)
}

func isBidi(r rune) bool {
	return r == 0x061C || r == 0x200E || r == 0x200F || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

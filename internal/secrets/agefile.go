package secrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"filippo.io/age"
)

// Scrypt work factors (log2). The default is age's; the floor keeps a
// slow-device setting from making the file easy to brute force. They are
// vars only so tests can run fast; nothing else changes them.
var (
	defaultWorkFactor = 18
	minWorkFactor     = 14
)

// ageFile is the encrypted-file backend: one JSON object {name: value}
// encrypted to a scrypt (passphrase) recipient. Every Set/Delete
// re-reads the file when it changed on disk, applies the change and
// re-encrypts the whole object.
type ageFile struct {
	mu    sync.Mutex
	path  string
	work  int
	pass  []byte            // nil while locked
	data  map[string]string // nil while locked
	stamp fileStamp         // of the file data was read from
}

type fileStamp struct {
	mod  time.Time
	size int64
}

func newAgeFile(path string, work int) *ageFile {
	if work == 0 {
		work = defaultWorkFactor
	}
	if work < minWorkFactor {
		work = minWorkFactor
	}
	return &ageFile{path: path, work: work}
}

func (a *ageFile) Name() string { return "age" }

func (a *ageFile) Locked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pass == nil
}

// Unlock opens the file with pass. With no file yet, pass becomes the
// passphrase (if strong enough) and an empty file is written at once.
func (a *ageFile) Unlock(pass string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := os.Stat(a.path); errors.Is(err, os.ErrNotExist) {
		if _, err := CheckPassphrase(pass); err != nil {
			return err
		}
		a.lockLocked()
		p := []byte(pass)
		if err := a.write(p, map[string]string{}); err != nil {
			zero(p)
			return err
		}
		a.pass = p
		return nil
	}
	data, st, err := a.read([]byte(pass))
	if err != nil {
		return err
	}
	a.lockLocked()
	a.pass, a.data, a.stamp = []byte(pass), data, st
	return nil
}

func (a *ageFile) Lock() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lockLocked()
}

func (a *ageFile) lockLocked() {
	zero(a.pass)
	a.pass = nil
	clear(a.data)
	a.data = nil
	a.stamp = fileStamp{}
}

func (a *ageFile) Get(name string) (string, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pass == nil {
		return "", false, ErrLocked
	}
	if err := a.refresh(); err != nil {
		return "", false, err
	}
	v, ok := a.data[name]
	return v, ok, nil
}

func (a *ageFile) Set(name, value string) error {
	return a.change(func(m map[string]string) { m[name] = value })
}

func (a *ageFile) Delete(name string) error {
	return a.change(func(m map[string]string) { delete(m, name) })
}

func (a *ageFile) change(f func(map[string]string)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pass == nil {
		return ErrLocked
	}
	if err := a.refresh(); err != nil {
		return err
	}
	next := make(map[string]string, len(a.data)+1)
	for k, v := range a.data {
		next[k] = v
	}
	f(next)
	return a.write(a.pass, next)
}

// refresh re-reads the file when another UI changed it since our read.
func (a *ageFile) refresh() error {
	fi, err := os.Stat(a.path)
	if errors.Is(err, os.ErrNotExist) {
		// Removed under us: start empty; the next write recreates it.
		a.data, a.stamp = map[string]string{}, fileStamp{}
		return nil
	}
	if err != nil {
		return err
	}
	if (fileStamp{fi.ModTime(), fi.Size()}) == a.stamp && a.data != nil {
		return nil
	}
	data, st, err := a.read(a.pass)
	if err != nil {
		return err
	}
	a.data, a.stamp = data, st
	return nil
}

// read decrypts the file with pass.
func (a *ageFile) read(pass []byte) (map[string]string, fileStamp, error) {
	f, err := os.Open(a.path)
	if err != nil {
		return nil, fileStamp{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fileStamp{}, err
	}
	st := fileStamp{fi.ModTime(), fi.Size()}
	id, err := age.NewScryptIdentity(string(pass))
	if err != nil {
		return nil, st, ErrWrongPassphrase
	}
	r, err := age.Decrypt(f, id)
	if err != nil {
		var nm *age.NoIdentityMatchError
		if errors.As(err, &nm) {
			return nil, st, ErrWrongPassphrase
		}
		return nil, st, a.corrupt(err)
	}
	plain, err := io.ReadAll(r)
	defer zero(plain)
	if err != nil {
		return nil, st, a.corrupt(err)
	}
	data := map[string]string{}
	if err := json.Unmarshal(plain, &data); err != nil {
		return nil, st, a.corrupt(errors.New("not a JSON object"))
	}
	return data, st, nil
}

// write encrypts data with pass and replaces the file atomically.
func (a *ageFile) write(pass []byte, data map[string]string) error {
	rcp, err := age.NewScryptRecipient(string(pass))
	if err != nil {
		return err
	}
	rcp.SetWorkFactor(a.work)
	plain, err := json.Marshal(data)
	defer zero(plain)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, rcp)
	if err != nil {
		return err
	}
	if _, err := w.Write(plain); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := writeFileAtomic(a.path, buf.Bytes(), false); err != nil {
		return err
	}
	a.data = data
	if fi, err := os.Stat(a.path); err == nil {
		a.stamp = fileStamp{fi.ModTime(), fi.Size()}
	}
	return nil
}

// corrupt wraps a decrypt failure: the path and age's reason, no content.
func (a *ageFile) corrupt(err error) error {
	return fmt.Errorf("%w: %s: %v", ErrCorrupt, a.path, err)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

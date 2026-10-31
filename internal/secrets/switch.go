package secrets

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

// ── Switch backend (U19 SS-8) ──────────────────────────────────────────
//
//  1. Read every name of the open store: values from its backend (and,
//     for the plain file, the names of an old file the index does not
//     know yet), references and account labels from the index.
//  2. Write the values to the new backend (the age file in one
//     encryption) and read each back, comparing bytes.
//  3. Record the new backend and the entries in the index, then Commit
//     (the app saves the secret_store setting and starts using the new
//     store).
//  4. Only then remove the old copies: the age file and the plain file
//     are overwritten with zeros and deleted (the plain file leaves the
//     .migrated marker), keyring items are deleted. The old store is
//     locked, so its memory is zeroed.
//
// A failure before step 4 restores the index and the new backend's file
// (or deletes the keyring items written), so the old store stays the
// one in use and nothing of it was deleted.

// ErrOldNotRemoved: the switch is done, but some old copies are left.
var ErrOldNotRemoved = errors.New("switched, but the old copies could not all be removed")

// switchVerifyFn is the read-back check of Switch; tests swap it.
var switchVerifyFn = verifyBackend

// SwitchOptions configure Switch. Options are those of Open (Dir,
// PlainPath, ScryptWorkFactor, KeyringProbe, Register, OnUse …).
type SwitchOptions struct {
	Options
	// To is the new backend: "age", "keyring" or "plain".
	To string
	// Pass is the age passphrase: a new file is made with it, an
	// existing one must open with it. The caller zeroes it.
	Pass []byte
	// Commit runs after the copy is checked and recorded and before the
	// old copies are removed; an error undoes the switch.
	Commit func(Store) error
	// RemovePlain also wipes the plain file when the switch leaves the
	// plain backend. Off by default: the user decides (see Finish).
	RemovePlain bool
}

// Switch moves every secret of the open store from to the backend o.To
// and returns the new store. On an error before the old copies are
// removed it returns nil and from stays as it was. A failed removal
// returns the new store and ErrOldNotRemoved.
func Switch(from Store, o SwitchOptions) (Store, error) {
	fs, ok := from.(*store)
	if !ok {
		return nil, errors.New("secrets: this store can not be switched")
	}
	if o.Dir == "" {
		return nil, errors.New("secrets: no config dir")
	}
	if o.PlainPath == "" {
		o.PlainPath = filepath.Join(o.Dir, "ai", "credentials.json")
	}
	old := fs.b.Name()
	switch o.To {
	case "age", "keyring", "plain":
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownBackend, o.To)
	}
	if o.To == old {
		return nil, fmt.Errorf("the secrets are already in the %s", o.To)
	}
	if from.Locked() {
		return nil, ErrLocked
	}

	// 1. Read.
	idxPath := filepath.Join(o.Dir, IndexFile)
	idxBefore, idxErr := os.ReadFile(idxPath)
	idx, err := loadIndex(idxPath)
	if err != nil {
		return nil, err
	}
	vals := map[string]string{} // every name that is not a reference
	for n, e := range idx.Entries {
		switch e.Kind {
		case KindRef:
		case KindPublic:
			vals[n] = e.Value
		default:
			v, ok, err := fs.b.Get(n)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", n, err)
			}
			if ok {
				vals[n] = v
			}
		}
	}
	if old == "plain" {
		p, err := FromPlain(o.PlainPath)
		if err != nil {
			return nil, err
		}
		for n, v := range p.Values {
			if _, known := idx.Entries[n]; !known {
				vals[n] = v
			}
		}
	}
	// An account label stays in the index except in the plain file,
	// which keeps the old file shape.
	stored := map[string]string{}
	for n, v := range vals {
		if o.To == "plain" || !PublicName(n) {
			stored[n] = v
		}
	}

	// 2. Write and check.
	tb, undoTarget, err := openTarget(o)
	if err != nil {
		return nil, err
	}
	var written []string
	fail := func(err error) (Store, error) {
		undoTarget(written)
		if idxErr == nil {
			_ = writeFileAtomic(idxPath, idxBefore, false)
		} else if errors.Is(idxErr, os.ErrNotExist) {
			_ = os.Remove(idxPath)
		}
		return nil, err
	}
	if bs, ok := tb.(batchSetter); ok && len(stored) > 0 {
		if err := bs.setMany(stored); err != nil {
			return fail(err)
		}
		written = sortedNames(stored)
	} else {
		for _, n := range sortedNames(stored) {
			if err := tb.Set(n, stored[n]); err != nil {
				return fail(fmt.Errorf("write %s: %w", n, err))
			}
			written = append(written, n)
		}
	}
	if err := switchVerifyFn(tb, stored); err != nil {
		return fail(err)
	}

	// 3. Record and commit.
	err = updateIndex(idxPath, func(i *Index) {
		i.Backend = o.To
		for n, v := range vals {
			e := i.Entries[n]
			if _, ok := stored[n]; ok {
				i.Entries[n] = Entry{Kind: KindValue, Backend: o.To, Updated: e.Updated}
			} else {
				i.Entries[n] = Entry{Kind: KindPublic, Value: v, Updated: e.Updated}
			}
		}
	})
	if err != nil {
		return fail(err)
	}
	ns := NewStore(tb, o.Options)
	if o.Commit != nil {
		if err := o.Commit(ns); err != nil {
			return fail(err)
		}
	}

	// 4. Remove the old copies.
	var errs []error
	switch old {
	case "age":
		if a, ok := fs.b.(*ageFile); ok {
			if err := wipe(a.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	case "plain":
		if !o.RemovePlain {
			break
		}
		if err := Remove(o.PlainPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	case "keyring":
		for _, n := range sortedNames(vals) {
			if err := fs.b.Delete(n); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", n, err))
			}
		}
	}
	from.Lock()
	if len(errs) > 0 {
		return ns, fmt.Errorf("%w: %v", ErrOldNotRemoved, errors.Join(errs...))
	}
	return ns, nil
}

// openTarget opens the new backend ready for writes and returns how to
// undo the writes: a file is put back as it was (or removed), keyring
// items written are deleted.
func openTarget(o SwitchOptions) (Backend, func(written []string), error) {
	switch o.To {
	case "keyring":
		if !probeKeyringOption(o.Options) {
			why := KeyringSkipReason(o.Getenv, o.KeyringOverSSH)
			if why == "" {
				why = "it did not answer"
			}
			return nil, nil, fmt.Errorf("%w (%s)", ErrKeyringUnavailable, why)
		}
		k := newKeyringBackend(keyring.Set, keyring.Get, keyring.Delete)
		return k, func(written []string) {
			for _, n := range written {
				_ = k.Delete(n)
			}
		}, nil
	case "age":
		a := newAgeFile(filepath.Join(o.Dir, AgeFile), o.ScryptWorkFactor)
		undo := keepFile(a.path)
		if err := a.Unlock(string(o.Pass)); err != nil {
			undo(nil)
			return nil, nil, err
		}
		return a, func(w []string) { a.Lock(); undo(w) }, nil
	}
	p := newPlain(o.PlainPath)
	return p, keepFile(o.PlainPath), nil
}

// keepFile remembers a file so a failed switch can put it back as it
// was, or remove it when it did not exist.
func keepFile(path string) func([]string) {
	before, err := os.ReadFile(path)
	existed := err == nil
	return func([]string) {
		if existed {
			_ = writeFileAtomic(path, before, false)
			return
		}
		_ = os.Remove(path)
	}
}

// verifyBackend reads every value back from b and compares the bytes.
// Its error names the secret, never the value.
func verifyBackend(b Backend, want map[string]string) error {
	for _, n := range sortedNames(want) {
		v, ok, err := b.Get(n)
		if err != nil {
			return fmt.Errorf("check %s: %w", n, err)
		}
		if !ok {
			return fmt.Errorf("check: %s is missing after the copy", n)
		}
		if subtle.ConstantTimeCompare([]byte(v), []byte(want[n])) != 1 {
			return fmt.Errorf("check: %s differs after the copy", n)
		}
	}
	return nil
}

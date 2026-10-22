package secrets

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"maps"
)

// ── Change passphrase (U19 SS-8) ───────────────────────────────────────
//
// The encrypted file is re-encrypted to the new passphrase in a temp
// file next to it. The temp file is decrypted again with the new
// passphrase and compared with the values before it replaces the old
// file (rename). Any failure leaves the old file, and the old
// passphrase, as they were. Passphrases are byte slices the caller
// zeroes; this code keeps only its own copy of the new one.

// ErrNoPassphrase: the store is not the encrypted file.
var ErrNoPassphrase = errors.New("only the encrypted file has a passphrase")

// ErrSamePassphrase: the new passphrase is the current one.
var ErrSamePassphrase = errors.New("the new passphrase is the same as the current one")

// reencryptCheckFn checks the re-encrypted temp file; tests swap it.
var reencryptCheckFn = checkReencrypted

// ageOf is the encrypted-file backend of s.
func ageOf(s Store) (*ageFile, error) {
	st, ok := s.(*store)
	if !ok {
		return nil, ErrNoPassphrase
	}
	a, ok := st.b.(*ageFile)
	if !ok {
		return nil, ErrNoPassphrase
	}
	return a, nil
}

// CheckCurrentPassphrase reports whether pass opens the encrypted file
// of s (ErrWrongPassphrase when not). s is not changed.
func CheckCurrentPassphrase(s Store, pass []byte) error {
	a, err := ageOf(s)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	data, _, err := a.read(pass)
	clear(data)
	return err
}

// ChangePassphrase re-encrypts the encrypted file of s from oldPass to
// newPass. Afterwards s is unlocked with newPass.
func ChangePassphrase(s Store, oldPass, newPass []byte) error {
	a, err := ageOf(s)
	if err != nil {
		return err
	}
	if _, err := CheckPassphrase(string(newPass)); err != nil {
		return err
	}
	st := s.(*store)
	st.mu.Lock()
	defer st.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	data, _, err := a.read(oldPass)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(oldPass, newPass) == 1 {
		clear(data)
		return ErrSamePassphrase
	}
	enc, err := a.encrypt(newPass, data)
	if err == nil {
		err = writeFileAtomicChecked(a.path, enc, false, func(tmp string) error {
			return reencryptCheckFn(tmp, newPass, data)
		})
	}
	if err != nil {
		clear(data)
		return fmt.Errorf("passphrase not changed: %w", err)
	}
	a.lockLocked()
	a.pass = append([]byte(nil), newPass...)
	a.data = data
	a.restamp()
	st.used()
	return nil
}

// checkReencrypted decrypts the temp file with pass and compares it
// with want. Its error names no value.
func checkReencrypted(tmp string, pass []byte, want map[string]string) error {
	c := &ageFile{path: tmp}
	got, _, err := c.read(pass)
	defer clear(got)
	if err != nil {
		return fmt.Errorf("check of the new file: %w", err)
	}
	if !maps.Equal(got, want) {
		return errors.New("check of the new file: the values differ")
	}
	return nil
}

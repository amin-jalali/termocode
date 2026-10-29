// Package cfgfile is the one safe way to change the user's config.json:
// read the whole file, change some keys, write it back through a temp
// file and a rename, so a crash or a full disk never leaves half a file
// and a file that is not valid JSON is never overwritten.
package cfgfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// rename is os.Rename; tests replace it to simulate a failing write.
var rename = os.Rename

// Update reads path (a missing or empty file is an empty object), calls
// fn on its keys and writes the result back. Every key fn leaves alone is
// kept. A file that is not a valid JSON object is left untouched and
// Update returns an error. fn may return an error to cancel the write.
func Update(path string, fn func(map[string]any) error) error {
	return update(path, fn)
}

// UpdateRaw is Update with the values kept as written (numbers and
// nested objects are not re-encoded).
func UpdateRaw(path string, fn func(map[string]json.RawMessage) error) error {
	return update(path, fn)
}

func update[T any](path string, fn func(map[string]T) error) error {
	cfg := map[string]T{}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(bytes.TrimSpace(b)) > 0 {
			// UseNumber keeps numbers as written: through float64,
			// 12345678901234567891 or 0.0 would come back changed.
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.UseNumber()
			err := dec.Decode(&cfg)
			if err == nil {
				if _, terr := dec.Token(); terr != io.EOF {
					err = errors.New("extra text after the JSON object")
				}
			}
			if err != nil {
				return fmt.Errorf("%s is not valid JSON; nothing was changed: %w", path, err)
			}
			if cfg == nil { // the file said null
				cfg = map[string]T{}
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	if err := fn(cfg); err != nil {
		return err
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, append(out, '\n'))
}

// WriteFile writes data to path through a temp file in the same folder
// and a rename. The file keeps its mode (0644 for a new one) and a
// read-only file is not replaced; the folder is created when missing. On any error the old file stays as it was and
// no temp file is left behind.
func WriteFile(path string, data []byte) error {
	return plainWriteErr(path, writeFile(path, data))
}

func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		// A file the user made read-only stays as it is (a plain write
		// would fail too; a rename must not get around it).
		if mode&0o200 == 0 {
			return &os.PathError{Op: "write", Path: path, Err: os.ErrPermission}
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// plainWriteErr says in short words why path could not be written: the
// raw error names a temp file ("open …/.config.json-123.tmp: permission
// denied") that the user never made. The cause stays wrapped (errors.Is
// still works).
func plainWriteErr(path string, err error) error {
	if err == nil {
		return nil
	}
	name, dir := filepath.Base(path), tildeHome(filepath.Dir(path))
	switch {
	case errors.Is(err, syscall.ENOSPC):
		return fmt.Errorf("the disk is full; %s was not changed: %w", name, err)
	case errors.Is(err, fs.ErrPermission):
		if st, serr := os.Stat(path); serr == nil && st.Mode().Perm()&0o200 == 0 {
			return fmt.Errorf("%s is read-only; it was not changed: %w", name, err)
		}
		return fmt.Errorf("no permission to write in %s; %s was not changed: %w", dir, name, err)
	}
	return err
}

// tildeHome shows a path under the home folder as ~/….
func tildeHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return "~" + string(filepath.Separator) + rel
	}
	return p
}

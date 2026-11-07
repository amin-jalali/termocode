package pairsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const baseVersion = 1

// Base is what both sides looked like at the end of the last sync.
type Base struct {
	Version int       `json:"version"`
	At      time.Time `json:"at,omitempty"`
	Local   Snapshot  `json:"local"`
	Remote  Snapshot  `json:"remote"`
}

// BaseKey names the base file of one pairing: the local folder, the
// engine context, the container name and the folder in it.
func BaseKey(localRoot, engineContext, container, root string) string {
	sum := sha256.Sum256([]byte(localRoot + "\x00" + engineContext + "\x00" + container + "\x00" + root))
	return hex.EncodeToString(sum[:8])
}

// LoadBase reads a base file; a missing file is an empty base (the
// first sync).
func LoadBase(file string) (Base, error) {
	b := Base{Local: Snapshot{}, Remote: Snapshot{}}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return Base{Local: Snapshot{}, Remote: Snapshot{}}, errors.New("the last sync record is damaged; delete " + file + " to start over")
	}
	if b.Local == nil {
		b.Local = Snapshot{}
	}
	if b.Remote == nil {
		b.Remote = Snapshot{}
	}
	return b, nil
}

// SaveBase writes b to file (0600, parent 0700) through a temp file, so
// a crash never leaves half a record.
func SaveBase(file string, b Base) error {
	if b.Version == 0 {
		b.Version = baseVersion
	}
	if b.At.IsZero() {
		b.At = time.Now().UTC()
	}
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".pairsync-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}

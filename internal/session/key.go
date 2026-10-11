package session

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// Key is the id of a workspace folder: a short hash of its absolute,
// cleaned path. Sessions and hot-exit backups are stored under it.
func Key(workspace string) string {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		abs = workspace
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return hex.EncodeToString(sum[:8])
}

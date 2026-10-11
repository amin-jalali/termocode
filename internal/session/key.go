package session

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// EnvSession is set in the UI's environment (and so in every shell it
// starts) to the session key. A termocode started inside a session runs
// directly instead of nesting another session.
const EnvSession = "TERMOCODE_SESSION"

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

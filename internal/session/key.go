package session

import (
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// EnvSession is set in the UI's environment (and so in every shell it
// starts) to the session key. A termocode started inside a session runs
// directly instead of nesting another session.
const EnvSession = "TERMOCODE_SESSION"

// Key is the id of a local workspace folder: a short hash of its
// absolute, cleaned path. Sessions and hot-exit backups are stored under it.
func Key(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return KeyForID(workspace.LocalID(abs))
}

// KeyForID is the key of a workspace ID (workspace.Key). For a local ID
// it equals Key of its folder, so sessions and backups stored before
// workspace IDs existed are found again.
func KeyForID(id string) string {
	return workspace.Key(id)
}

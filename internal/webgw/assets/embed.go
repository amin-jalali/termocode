package assets

import (
	"embed"
	"io/fs"

	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

// files holds only what cmd/webbuild wrote; sources and vendored
// originals stay out of the binary.
//
//go:embed index.html app.js app.css licenses.txt icon.svg assets.lock
var files embed.FS

// FS returns the built files.
func FS() fs.FS { return files }

// Lock returns the rows of the embedded assets.lock. A broken lock is a
// build bug; the tests catch it, and here it reads as no files.
func Lock() []manifest.LockEntry {
	b, err := files.ReadFile(manifest.LockFile)
	if err != nil {
		return nil
	}
	entries, err := manifest.ParseLock(string(b))
	if err != nil {
		return nil
	}
	return entries
}

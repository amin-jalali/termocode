package assets

import (
	"embed"
	"io/fs"
	"strings"

	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

// files holds only what cmd/webbuild wrote; sources and vendored
// originals stay out of the binary.
//
//go:embed index.html app.js editor.js app.css licenses.txt icon.svg icon-maskable.svg manifest.webmanifest fonts/nf-*.woff2 assets.lock
//go:embed monaco.html monaco-amd.js monaco-frame.js monaco-frame.css monaco/editor.js monaco/editor-host.js monaco/editor.css icons/codicon.ttf
var files embed.FS

// FS returns the built files.
func FS() fs.FS { return files }

// IsKitFile reports whether a built file belongs to the dev-only kit page.
func IsKitFile(name string) bool { return strings.HasPrefix(name, "kit.") }

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

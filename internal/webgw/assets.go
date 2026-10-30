//go:build unix

package webgw

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/amin-jalali/termocode/internal/webgw/assets"
	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

// assetTypes are the only kinds of file the gateway serves.
var assetTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".svg":  "image/svg+xml",
}

// ManifestPath is where an installable site serves its app manifest.
const ManifestPath = "/manifest.webmanifest"

// Manifest serves the app manifest (WEB-35) at ManifestPath and nothing
// else. The gateway mounts it only on an installable site; there is no
// service worker, so nothing of the page is kept in the browser.
func Manifest() http.Handler {
	b, err := fs.ReadFile(assets.FS(), "manifest.webmanifest")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err != nil || r.URL.Path != ManifestPath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/manifest+json")
		_, _ = w.Write(b)
	})
}

type asset struct {
	body  []byte
	ctype string
}

// KitEnabled reports whether this build serves the component kit at
// /dev/kit (go build -tags webdev).
const KitEnabled = assets.KitEnabled

// Assets serves the embedded browser UI: the page at "/" and each built
// file listed in assets.lock at /assets/<name>. Nothing else is served:
// no directory listing, no sources, not the lock. Only GET. The page's
// script and style tags carry the SRI hashes cmd/webbuild wrote, so a
// changed file does not run.
func Assets() http.Handler {
	routes := map[string]asset{}
	for _, e := range assets.Lock() {
		if e.Path == manifest.LockFile {
			continue
		}
		src := assets.FS()
		if assets.IsKitFile(e.Path) {
			if !assets.KitEnabled {
				continue // the kit is served by dev builds only
			}
			src = assets.KitFS()
		}
		b, err := fs.ReadFile(src, e.Path)
		ctype, ok := assetTypes[path.Ext(e.Path)]
		if err != nil || !ok {
			continue
		}
		switch {
		case e.Path == "index.html":
			routes["/"] = asset{b, ctype}
		case e.Path == "kit.html":
			routes["/dev/kit"] = asset{b, ctype}
		case strings.HasSuffix(e.Path, ".html"), e.Path == "manifest.webmanifest":
			// No other pages; the manifest is Manifest()'s.
		default:
			routes["/assets/"+e.Path] = asset{b, ctype}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", a.ctype)
		_, _ = w.Write(a.body)
	})
}

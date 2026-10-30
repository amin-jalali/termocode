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

type asset struct {
	body  []byte
	ctype string
}

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
		b, err := fs.ReadFile(assets.FS(), e.Path)
		ctype, ok := assetTypes[path.Ext(e.Path)]
		if err != nil || !ok {
			continue
		}
		switch {
		case e.Path == "index.html":
			routes["/"] = asset{b, ctype}
		case strings.HasSuffix(e.Path, ".html"):
			// Other pages (the dev kit) are routed by their own builds.
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

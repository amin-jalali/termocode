//go:build unix

package webgw

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/webgw/assets"
	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

// assetTypes are the only kinds of file the gateway serves.
var assetTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
}

// ManifestPath is where an installable site serves its app manifest.
const ManifestPath = "/manifest.webmanifest"

// Manifest serves the app manifest (WEB-35) at ManifestPath and nothing
// else. The gateway mounts it only on an installable site. The only
// service worker is the push-only sw.js (MOB-30, push_sw.go): it keeps
// nothing of the page in the browser.
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
	gz    *gzBody // nil: sent as it is (fonts are compressed already)
}

// gzBody is an asset's gzipped bytes, made on its first request (a
// gateway that never serves the editor never packs its 4.5 MB).
type gzBody struct {
	once sync.Once
	src  []byte
	b    []byte
}

func (g *gzBody) bytes() []byte {
	g.once.Do(func() {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
		_, _ = zw.Write(g.src)
		if zw.Close() == nil && buf.Len() < len(g.src) {
			g.b = buf.Bytes()
		}
	})
	return g.b
}

// packable are the asset kinds sent gzipped to a browser that takes it.
var packable = map[string]bool{".html": true, ".js": true, ".css": true, ".txt": true, ".svg": true}

// takesGzip reports whether the request's Accept-Encoding names gzip
// with a weight above 0.
func takesGzip(r *http.Request) bool {
	for _, h := range r.Header.Values("Accept-Encoding") {
		for _, part := range strings.Split(h, ",") {
			name, params, _ := strings.Cut(part, ";")
			if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
				continue
			}
			k, v, ok := strings.Cut(strings.TrimSpace(params), "=")
			if !ok || strings.TrimSpace(k) != "q" {
				return true
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			return err == nil && q > 0
		}
	}
	return false
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
		a := asset{body: b, ctype: ctype}
		if packable[path.Ext(e.Path)] {
			a.gz = &gzBody{src: b}
		}
		switch {
		case e.Path == "index.html":
			routes["/"] = a
		case e.Path == "kit.html":
			routes["/dev/kit"] = a
		case "/assets/"+e.Path == FramePath:
			routes[FramePath] = a
		case strings.HasSuffix(e.Path, ".html"), e.Path == "manifest.webmanifest":
			// No other pages; the manifest is Manifest()'s.
		default:
			routes["/assets/"+e.Path] = a
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
		if r.URL.Path == FramePath {
			// The desktop editor's frame: its own policy, framed by the page only.
			w.Header().Set("Content-Security-Policy", FrameCSP())
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		}
		w.Header().Set("Content-Type", a.ctype)
		body := a.body
		if a.gz != nil {
			// The page's code is public and the same for everyone (no
			// secret in it to guess at), so packing it leaks nothing.
			w.Header().Set("Vary", "Accept-Encoding")
			if z := a.gz.bytes(); z != nil && takesGzip(r) {
				w.Header().Set("Content-Encoding", "gzip")
				body = z
			}
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	})
}

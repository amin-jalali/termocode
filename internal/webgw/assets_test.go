//go:build unix

package webgw

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webgw/assets"
	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

func getAsset(t *testing.T, h http.Handler, method, path string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Result()
}

func TestAssetsServeThePageWithSRI(t *testing.T) {
	h := Assets()
	res := getAsset(t, h, http.MethodGet, "/")
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("GET / = %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	refs := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)" integrity="([^"]+)"`).FindAllStringSubmatch(string(body), -1)
	if len(refs) < 2 {
		t.Fatalf("page has %d SRI references, want the script and the style:\n%s", len(refs), body)
	}
	for _, r := range refs {
		sub := getAsset(t, h, http.MethodGet, r[1])
		b, _ := io.ReadAll(sub.Body)
		if sub.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d", r[1], sub.StatusCode)
			continue
		}
		if manifest.SRI(b) != r[2] {
			t.Errorf("%s: served bytes do not match the page's integrity", r[1])
		}
	}
}

func TestAssetsContentTypes(t *testing.T) {
	h := Assets()
	for path, want := range map[string]string{
		"/assets/app.js":                   "text/javascript; charset=utf-8",
		"/assets/app.css":                  "text/css; charset=utf-8",
		"/assets/licenses.txt":             "text/plain; charset=utf-8",
		"/assets/icon.svg":                 "image/svg+xml",
		"/assets/fonts/nf-powerline.woff2": "font/woff2",
	} {
		res := getAsset(t, h, http.MethodGet, path)
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != want {
			t.Errorf("GET %s = %d %q, want 200 %q", path, res.StatusCode, res.Header.Get("Content-Type"), want)
		}
	}
}

func TestAssetsServeOnlyTheBuiltFiles(t *testing.T) {
	h := Assets()
	for _, path := range []string{
		"/assets/assets.lock", "/assets/src/main.tsx", "/assets/vendor/vendor.json",
		"/assets/../doc.go", "/index.html", "/assets/", "/assets/APP.JS", "/favicon.ico", "/x",
	} {
		if res := getAsset(t, h, http.MethodGet, path); res.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, res.StatusCode)
		}
	}
}

func TestAssetsAreReadOnly(t *testing.T) {
	h := Assets()
	for _, path := range []string{"/", "/assets/app.js"} {
		res := getAsset(t, h, http.MethodPost, path)
		if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") != "GET" {
			t.Errorf("POST %s = %d allow %q, want 405 GET", path, res.StatusCode, res.Header.Get("Allow"))
		}
	}
}

func TestAssetsServeEveryLockedFileButTheLock(t *testing.T) {
	h := Assets()
	for _, e := range assets.Lock() {
		want := http.StatusOK
		if e.Path == manifest.LockFile || strings.HasSuffix(e.Path, ".html") || assets.IsKitFile(e.Path) && !KitEnabled || e.Path == "manifest.webmanifest" {
			continue // the page is served at /, the manifest by Manifest(), the lock is not served
		}
		path := "/assets/" + e.Path
		res := getAsset(t, h, http.MethodGet, path)
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want || manifest.SRI(b) != e.SRI {
			t.Errorf("GET %s = %d or bytes differ from the lock", path, res.StatusCode)
		}
	}
	if _, err := fs.Stat(assets.FS(), manifest.LockFile); err != nil {
		t.Fatal("lock not embedded")
	}
}

func TestKitIsServedOnlyByDevBuilds(t *testing.T) {
	h := Assets()
	want := http.StatusNotFound
	if KitEnabled {
		want = http.StatusOK
	}
	for _, path := range []string{"/dev/kit", "/assets/kit.js", "/assets/kit.css"} {
		if res := getAsset(t, h, http.MethodGet, path); res.StatusCode != want {
			t.Errorf("GET %s = %d, want %d (KitEnabled=%v)", path, res.StatusCode, want, KitEnabled)
		}
	}
}

// The app manifest (WEB-35) is served only by Manifest(), which the
// gateway mounts on an installable site; Assets() never serves it. It
// opens the page standalone at "/", and every icon it names is served.
func TestManifestServedOnlyByManifest(t *testing.T) {
	for _, path := range []string{ManifestPath, "/assets/manifest.webmanifest"} {
		if res := getAsset(t, Assets(), http.MethodGet, path); res.StatusCode != http.StatusNotFound {
			t.Errorf("Assets GET %s = %d, want 404", path, res.StatusCode)
		}
	}
	res := getAsset(t, Manifest(), http.MethodGet, ManifestPath)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("GET %s = %d %q", ManifestPath, res.StatusCode, res.Header.Get("Content-Type"))
	}
	var m struct {
		Name, StartURL, Scope, Display string
		Icons                          []struct{ Src, Type, Purpose string }
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "termocode" || m.Display != "standalone" {
		t.Errorf("manifest %+v", m)
	}
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	if raw["start_url"] != "/" || raw["scope"] != "/" {
		t.Errorf("start_url %v scope %v, want /", raw["start_url"], raw["scope"])
	}
	purposes := map[string]bool{}
	for _, ic := range m.Icons {
		purposes[ic.Purpose] = true
		if r := getAsset(t, Assets(), http.MethodGet, ic.Src); r.StatusCode != http.StatusOK || r.Header.Get("Content-Type") != ic.Type {
			t.Errorf("icon %s = %d %q, want 200 %q", ic.Src, r.StatusCode, r.Header.Get("Content-Type"), ic.Type)
		}
	}
	if !purposes["any"] || !purposes["maskable"] {
		t.Errorf("icon purposes %v, want any and maskable", purposes)
	}
	if res := getAsset(t, Manifest(), http.MethodPost, ManifestPath); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", res.StatusCode)
	}
	if res := getAsset(t, Manifest(), http.MethodGet, "/sw.js"); res.StatusCode != http.StatusNotFound {
		t.Errorf("GET /sw.js = %d", res.StatusCode)
	}
}

// The desktop editor's frame has its own policy: inline styles and the
// editor's Trusted Types policies, nothing else more; no network at all;
// only this site may frame it. Its scripts and styles carry SRI values
// the served bytes match.
func TestFrameServedWithItsOwnPolicy(t *testing.T) {
	h := Guard(LoopbackSite(41237), Assets())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, FramePath, nil)
	req.Host = "127.0.0.1:41237"
	h.ServeHTTP(rec, req)
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", FramePath, res.StatusCode)
	}
	csp := res.Header.Get("Content-Security-Policy")
	if csp != FrameCSP() || res.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("frame headers: CSP %q, X-Frame-Options %q", csp, res.Header.Get("X-Frame-Options"))
	}
	for _, want := range []string{"connect-src 'none'", "worker-src 'none'", "frame-ancestors 'self'", "script-src 'self';", "require-trusted-types-for 'script'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("frame CSP has no %q: %s", want, csp)
		}
	}
	if strings.Count(csp, "unsafe") != 1 || !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") || strings.Contains(csp, "*") {
		t.Errorf("frame CSP opens more than inline styles: %s", csp)
	}
	refs := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)" integrity="([^"]+)"`).FindAllStringSubmatch(string(body), -1)
	if len(refs) != 6 {
		t.Fatalf("frame has %d SRI references, want 6:\n%s", len(refs), body)
	}
	for _, r := range refs {
		sub := getAsset(t, Assets(), http.MethodGet, r[1])
		b, _ := io.ReadAll(sub.Body)
		if sub.StatusCode != http.StatusOK || manifest.SRI(b) != r[2] {
			t.Errorf("GET %s = %d or its bytes do not match the frame's integrity", r[1], sub.StatusCode)
		}
	}
	// The page itself keeps its strict policy and may frame only this site.
	page := httptest.NewRecorder()
	preq := httptest.NewRequest(http.MethodGet, "/", nil)
	preq.Host = "127.0.0.1:41237"
	h.ServeHTTP(page, preq)
	if got := page.Result().Header.Get("Content-Security-Policy"); got != CSP(LoopbackSite(41237)) || page.Result().Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("the page's headers changed: %q", got)
	}
}

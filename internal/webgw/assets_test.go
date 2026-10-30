//go:build unix

package webgw

import (
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
		"/assets/app.js":       "text/javascript; charset=utf-8",
		"/assets/app.css":      "text/css; charset=utf-8",
		"/assets/licenses.txt": "text/plain; charset=utf-8",
		"/assets/icon.svg":     "image/svg+xml",
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
		if e.Path == manifest.LockFile || strings.HasSuffix(e.Path, ".html") {
			continue // the page is served at /, the lock is not served
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

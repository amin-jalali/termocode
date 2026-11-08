//go:build unix

package webgw

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/amin-jalali/termocode/internal/webgw/assets"
)

func getGzip(t *testing.T, h http.Handler, path, accept string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if accept != "" {
		req.Header.Set("Accept-Encoding", accept)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

// The page's code goes over the wire gzipped to a browser that takes it:
// the desktop's first load was 0.9 MB and its first file open 4.6 MB
// (the editor), every time (no-store), on a 50 Mbit/s Wi-Fi 1.2 s for
// the file alone. The bytes are the built ones (the SRI still holds), the
// fonts stay as they are (already compressed), and a client that does not
// say gzip gets the plain file.
func TestAssetsGzipped(t *testing.T) {
	h := Assets()
	for _, path := range []string{"/", "/assets/app.js", "/assets/workbench.js", "/assets/app.css", "/assets/monaco/editor.js", "/assets/monaco.html"} {
		plain := getGzip(t, h, path, "")
		want, _ := io.ReadAll(plain.Body)
		if plain.StatusCode != http.StatusOK || plain.Header.Get("Content-Encoding") != "" {
			t.Fatalf("GET %s without gzip = %d, Content-Encoding %q", path, plain.StatusCode, plain.Header.Get("Content-Encoding"))
		}
		res := getGzip(t, h, path, "gzip, deflate, br, zstd")
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("GET %s with gzip = %d, Content-Encoding %q, want gzip", path, res.StatusCode, res.Header.Get("Content-Encoding"))
			continue
		}
		if v := res.Header.Values("Vary"); len(v) != 1 || v[0] != "Accept-Encoding" {
			t.Errorf("GET %s: Vary %q, want Accept-Encoding", path, v)
		}
		zb, _ := io.ReadAll(res.Body)
		zr, err := gzip.NewReader(bytes.NewReader(zb))
		if err != nil {
			t.Fatalf("GET %s: not gzip: %v", path, err)
		}
		got, _ := io.ReadAll(zr)
		if !bytes.Equal(got, want) {
			t.Errorf("GET %s: the gzipped body is not the file", path)
		}
		if len(want) > 10<<10 && len(zb)*2 > len(want) { // the code (a small page is mostly hashes)
			t.Errorf("GET %s: %d B gzipped of %d B, want under half", path, len(zb), len(want))
		}
		if cl := res.Header.Get("Content-Length"); cl != "" && cl != strconv.Itoa(len(zb)) {
			t.Errorf("GET %s: Content-Length %s for %d B", path, cl, len(zb))
		}
	}
	font := getGzip(t, h, "/assets/fonts/nf-powerline.woff2", "gzip")
	if font.Header.Get("Content-Encoding") != "" {
		t.Errorf("a woff2 font was gzipped again")
	}
	b, _ := fs.ReadFile(assets.FS(), "fonts/nf-powerline.woff2")
	if got, _ := io.ReadAll(font.Body); !bytes.Equal(got, b) {
		t.Error("the font's bytes changed")
	}
	// "gzip;q=0" refuses gzip.
	if res := getGzip(t, h, "/assets/app.js", "gzip;q=0, br"); res.Header.Get("Content-Encoding") != "" {
		t.Errorf("gzip;q=0 still got gzip")
	}
}

package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatest(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" || !strings.HasPrefix(r.Header.Get("User-Agent"), "termocode/") {
			http.Error(w, "headers", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(sampleRelease))
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), LatestURL: srv.URL + "/latest", Version: "v0.1.0"}
	rel, err := c.Latest(context.Background())
	if err != nil || rel.Tag != "v0.2.0" {
		t.Fatalf("got %+v %v", rel, err)
	}
}

func TestLatestErrors(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client(), LatestURL: srv.URL + "/latest"}
	if _, err := c.Latest(context.Background()); err == nil {
		t.Error("403: want an error")
	}
	// Plain http is refused before any request.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("plain http was requested")
	}))
	defer plain.Close()
	c = Client{HTTP: plain.Client(), LatestURL: plain.URL + "/latest"}
	if _, err := c.Latest(context.Background()); err == nil {
		t.Error("http: want an error")
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("payload"))
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", 100)))
		case "/redirect-http":
			http.Redirect(w, r, "http://127.0.0.1:1/x", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := Client{HTTP: srv.Client()}
	dir := t.TempDir()
	dest := filepath.Join(dir, "f")
	if err := c.Download(context.Background(), srv.URL+"/ok", dest, 1024); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != "payload" {
		t.Fatalf("got %q", b)
	}
	for _, path := range []string{"/big", "/missing", "/redirect-http"} {
		d := filepath.Join(dir, strings.TrimPrefix(path, "/"))
		if err := c.Download(context.Background(), srv.URL+path, d, 50); err == nil {
			t.Errorf("%s: want an error", path)
		}
		if _, err := os.Stat(d); err == nil {
			t.Errorf("%s: left a file", path)
		}
	}
}

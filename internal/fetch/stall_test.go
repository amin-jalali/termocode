package fetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec (SCN-13): a download from a server that stops sending — before the
// answer or in the middle of the body — ends with a timeout error (a
// net.Error, so installers call it a network problem) instead of waiting
// forever. Data that keeps coming, however slowly, is not cut off.
func TestDownloadGivesUpWhenNothingComes(t *testing.T) {
	old := stallTimeout
	stallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/body":
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("first part"))
			w.(http.Flusher).Flush()
		case "/slow":
			for i := 0; i < 5; i++ { // a byte every 100 ms: slow, not stalled
				_, _ = w.Write([]byte("x"))
				w.(http.Flusher).Flush()
				time.Sleep(100 * time.Millisecond)
			}
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs first: the handlers end

	for _, path := range []string{"/headers", "/body"} {
		done := make(chan error, 1)
		go func() {
			done <- Download(context.Background(), srv.URL+path, filepath.Join(t.TempDir(), "out"), nil)
		}()
		select {
		case err := <-done:
			var ne net.Error
			if !errors.As(err, &ne) || !ne.Timeout() || !strings.Contains(err.Error(), "no data") {
				t.Errorf("%s: want a timeout error, got %v", path, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: the download still waits after 10 s", path)
		}
	}
	if err := Download(context.Background(), srv.URL+"/slow", filepath.Join(t.TempDir(), "out"), nil); err != nil {
		t.Errorf("a slow but steady download failed: %v", err)
	}
}

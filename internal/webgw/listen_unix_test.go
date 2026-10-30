//go:build unix

package webgw

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shortDir is a private (0700) temp dir with a short path: a unix socket
// path must fit in ~100 bytes, and t.TempDir() can be long on macOS.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestUnixSocketMode0600AndSite(t *testing.T) {
	path := filepath.Join(shortDir(t), "k.webhttp.sock")
	ln, site, err := ListenUnix(path, 41237)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want a 0600 socket", fi.Mode())
	}
	if site.Scheme != "http" || site.Hosts[0] != "127.0.0.1:41237" || site.Hosts[1] != "localhost:41237" {
		t.Fatalf("site %+v", site)
	}
}

func TestUnixNeedsPortHint(t *testing.T) {
	dir := shortDir(t)
	for _, p := range []int{0, -1, 80, 70000} {
		if ln, _, err := ListenUnix(filepath.Join(dir, "s"), p); err == nil {
			ln.Close()
			t.Errorf("port hint %d: no error", p)
		}
	}
}

// TestUnixRefusesLooseDir: the socket is reachable by path until chmod, so
// its folder must already be private.
func TestUnixRefusesLooseDir(t *testing.T) {
	dir := shortDir(t)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if ln, _, err := ListenUnix(filepath.Join(dir, "s"), 41237); err == nil {
		ln.Close()
		t.Fatal("listened in a 0755 folder")
	}
}

func TestStaleUnixSocketRemoved(t *testing.T) {
	path := filepath.Join(shortDir(t), "s")
	old, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	old.Close() // the file stays, nobody answers: a crashed gateway
	ln, _, err := ListenUnix(path, 41237)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	ln.Close()
}

func TestUnixSocketBusy(t *testing.T) {
	path := filepath.Join(shortDir(t), "s")
	live, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	if ln, _, err := ListenUnix(path, 41237); !errors.Is(err, ErrSocketBusy) {
		if ln != nil {
			ln.Close()
		}
		t.Fatalf("err %v, want ErrSocketBusy", err)
	}
}

func TestUnixPathNotSocketLeftAlone(t *testing.T) {
	path := filepath.Join(shortDir(t), "s")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ln, _, err := ListenUnix(path, 41237); err == nil {
		ln.Close()
		t.Fatal("listened over a regular file")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
		t.Fatalf("file changed: %q %v", b, err)
	}
}

// TestUnixSocketPeerUID: the peer check runs on every accepted connection
// before a byte is read. Our own uid gets an answer; a refused peer is
// closed with nothing written (another uid cannot be made in a test, so
// the refusal is injected).
func TestUnixSocketPeerUID(t *testing.T) {
	dir := shortDir(t)
	serve := func(ln net.Listener) {
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close() })
	}
	get := func(path string) (string, error) {
		c, err := net.Dial("unix", path)
		if err != nil {
			return "", err
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := c.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")); err != nil {
			return "", err
		}
		b, err := io.ReadAll(c)
		return string(b), err
	}

	okPath := filepath.Join(dir, "ok")
	ln, _, err := ListenUnix(okPath, 41237)
	if err != nil {
		t.Fatal(err)
	}
	serve(ln)
	if out, err := get(okPath); err != nil || len(out) == 0 {
		t.Fatalf("own uid: %q %v", out, err)
	}

	badPath := filepath.Join(dir, "bad")
	checked := make(chan struct{}, 4)
	ln2, _, err := listenUnix(badPath, 41237, func(net.Conn) error {
		checked <- struct{}{}
		return errors.New("peer uid 1234 is not ours")
	})
	if err != nil {
		t.Fatal(err)
	}
	serve(ln2)
	out, _ := get(badPath)
	if out != "" {
		t.Fatalf("refused peer got %q", out)
	}
	select {
	case <-checked:
	default:
		t.Fatal("peer check never ran")
	}
}

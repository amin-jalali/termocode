//go:build e2e

package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/update"
)

// E2E-OPS: the update card and the rollback on the real binary. The binary
// is built with `-tags e2etest -X main.version=v0.1.0`, so it is a release
// build that asks TERMOCODE_E2E_UPDATE_URL (a fake https release server)
// instead of GitHub. The "new termocode" is a small script. Never the
// network.

const updNewBinary = "#!/bin/sh\n[ \"$1\" = \"--version\" ] && echo \"termocode v0.2.0\"\nexit 0\n"

func fakeReleaseServer(t *testing.T) *httptest.Server {
	t.Helper()
	tag := "v0.2.0"
	name := update.TarballName(tag, runtime.GOOS, runtime.GOARCH)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: update.BinaryDir(tag, runtime.GOOS, runtime.GOARCH) + "/termocode", Mode: 0o755, Size: int64(len(updNewBinary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte(updNewBinary))
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	sums := hex.EncodeToString(sum[:]) + "  " + name + "\n"
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": tag, "html_url": srv.URL + "/notes",
				"assets": []map[string]any{
					{"name": name, "size": buf.Len(), "browser_download_url": srv.URL + "/" + name},
					{"name": update.ChecksumsName, "size": len(sums), "browser_download_url": srv.URL + "/" + update.ChecksumsName},
				},
			})
		case "/" + name:
			_, _ = w.Write(buf.Bytes())
		case "/" + update.ChecksumsName:
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Spec (G-UPD): Check for Updates finds v0.2.0 on the (fake) server; the
// card's Update downloads, checks the checksum and asks to install;
// Install puts the new binary in place and keeps termocode.old; Roll Back
// Update asks, then puts the old binary back.
func TestUpdateCardInstallAndRollback(t *testing.T) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64":
	default:
		t.Skip("no release build for this system")
	}
	srv := fakeReleaseServer(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "termocode")
	build := exec.Command("go", "build", "-tags", "e2etest", "-ldflags", "-X main.version=v0.1.0", "-o", exe, "./cmd/termocode")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	old, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}

	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Bin: exe, Env: []string{"TERMOCODE_E2E_UPDATE_URL=" + srv.URL + "/latest"}})
	tm.settle()
	tm.Palette("termocode: Check for Updates")
	tm.WaitText("termocode v0.2.0 is out", uiTimeout)
	tm.Send("\x1b[C\x1b[C\x1b[C") // Later · Release notes · Skip · Update
	tm.Send(KeyEnter)
	tm.WaitText("Install termocode", 30*time.Second)
	tm.settle()
	tm.Send(KeyEnter)
	waitUntil(t, "the new binary in place", 30*time.Second, func() bool {
		b, _ := os.ReadFile(exe)
		return string(b) == updNewBinary
	})
	if b, _ := os.ReadFile(filepath.Join(dir, "termocode.old")); !bytes.Equal(b, old) {
		t.Fatalf("termocode.old is not the old binary (%d bytes)", len(b))
	}

	tm.settle()
	tm.Palette("termocode: Roll Back Update")
	tm.WaitText("Roll back", uiTimeout)
	tm.settle()
	tm.ClickText("Roll back")
	waitUntil(t, "the old binary back", 30*time.Second, func() bool {
		b, _ := os.ReadFile(exe)
		return bytes.Equal(b, old)
	})
	if strings.Contains(tm.Screen(), "failed") {
		t.Errorf("a failure on screen:\n%s", tm.Screen())
	}
}

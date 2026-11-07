package testkit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/update"
)

// SCN-31: settings export / import round trip, and an update whose
// download fails or whose checksum is wrong, through the real app. The
// "GitHub" here is an httptest server on 127.0.0.1: no network.

// toastsSay reports whether a live toast (title — detail) holds s.
func toastsSay(d *Driver, s string) bool {
	for _, t := range d.Model().ToastTexts() {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

// configFile reads a file of the sandboxed ~/.config/termocode.
func configFile(t *testing.T, name string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", name))
	return string(b)
}

// Export writes the settings (no secret), Import asks first with Cancel
// first and focused, and Import puts the exported settings back after a
// backup of the current ones.
func TestSettingsExportImportRoundTrip(t *testing.T) {
	d := New(t, Options{W: 120, H: 40, Files: textFiles(), Config: map[string]string{
		"keymap.json": `{"ctrl+alt+j": "ToggleTerminal"}`,
		"config.json": `{"tab_size": 4, "ai_api_key": "sk-canary-scn31"}`,
	}})

	// Export to the default ~/termocode-settings.json (a sandboxed HOME).
	d.Palette("settings-export")
	if !strings.Contains(screen(d), "Export Settings") {
		t.Fatalf("no export prompt:\n%s", screen(d))
	}
	d.Key("enter")
	if !strings.Contains(screen(d), "Exported 2 settings files") {
		t.Errorf("no export toast:\n%s", screen(d))
	}
	home, _ := os.UserHomeDir()
	bundle, err := os.ReadFile(filepath.Join(home, "termocode-settings.json"))
	if err != nil {
		t.Fatalf("export file: %v", err)
	}
	if strings.Contains(string(bundle), "sk-canary-scn31") {
		t.Errorf("the export holds the secret:\n%s", bundle)
	}
	if !strings.Contains(string(bundle), "ctrl+alt+j") || !strings.Contains(string(bundle), "tab_size") {
		t.Errorf("the export lost settings:\n%s", bundle)
	}

	// The settings change after the export.
	p := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "keymap.json")
	if err := os.WriteFile(p, []byte(`{"ctrl+alt+q": "ToggleTerminal"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Import: the card lists the files; Enter on it is Cancel.
	d.Palette("settings-import")
	d.Key("enter")
	s := screen(d)
	if !strings.Contains(s, "Import Settings?") || !strings.Contains(s, "keymap.json") || !strings.Contains(s, "config.json") {
		t.Fatalf("no import card:\n%s", s)
	}
	d.Key("enter")
	if strings.Contains(screen(d), "Import Settings?") {
		t.Errorf("Enter did not close the card:\n%s", screen(d))
	}
	if got := configFile(t, "keymap.json"); !strings.Contains(got, "ctrl+alt+q") {
		t.Errorf("Enter (Cancel) imported: keymap.json = %s", got)
	}

	// Import for real: the next button.
	d.Palette("settings-import")
	d.Key("enter")
	d.Key("right")
	d.Key("enter")
	if !strings.Contains(screen(d), "Imported 2 settings files") {
		t.Errorf("no import toast:\n%s", screen(d))
	}
	if got := configFile(t, "keymap.json"); !strings.Contains(got, "ctrl+alt+j") || strings.Contains(got, "ctrl+alt+q") {
		t.Errorf("keymap.json after the round trip = %s", got)
	}
	if got := configFile(t, "config.json"); !strings.Contains(got, "tab_size") {
		t.Errorf("config.json after the round trip = %s", got)
	}
	backups, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "settings-backup-*.json"))
	if len(backups) != 1 {
		t.Fatalf("backups = %q", backups)
	}
	if b, _ := os.ReadFile(backups[0]); !strings.Contains(string(b), "ctrl+alt+q") {
		t.Errorf("the backup does not hold the settings before the import:\n%s", b)
	}
}

// fakeRelease is a GitHub with release v0.2.0. failDownload makes the
// tarball answer HTTP 500; otherwise checksums.txt lists a wrong sum.
type fakeRelease struct {
	srv          *httptest.Server
	failDownload atomic.Bool
}

func newFakeRelease(t *testing.T) *fakeRelease {
	t.Helper()
	f := &fakeRelease{}
	tag := "v0.2.0"
	tarball := update.TarballName(tag, runtime.GOOS, runtime.GOARCH)
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": tag,
				"html_url": f.srv.URL + "/notes",
				"assets": []map[string]any{
					{"name": tarball, "size": 9, "browser_download_url": f.srv.URL + "/" + tarball},
					{"name": update.ChecksumsName, "size": 90, "browser_download_url": f.srv.URL + "/" + update.ChecksumsName},
				},
			})
		case "/" + tarball:
			if f.failDownload.Load() {
				http.Error(w, "down", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte("not-a-tgz"))
		case "/" + update.ChecksumsName:
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + tarball + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// updateTo runs Check for Updates and picks Update on the card.
func updateTo(t *testing.T, d *Driver) {
	t.Helper()
	d.Palette("update-check")
	if !d.WaitForText("termocode v0.2.0 is out", 5*time.Second) {
		t.Fatalf("no update card:\n%s", screen(d))
	}
	d.Key("right", "right", "right", "enter") // Later · Release notes · Skip · Update
}

// A failed download and a bad checksum stop the update: the screen says
// why, and the installed binary is not touched.
func TestUpdateDownloadFailureAndBadChecksumShown(t *testing.T) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64":
	default:
		t.Skip("no release build for this system")
	}
	f := newFakeRelease(t)
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bin, "termocode")
	if err := os.WriteFile(exe, []byte("old termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := New(t, Options{W: 120, H: 40, Files: textFiles()})
	t.Cleanup(app.SetUpdateSource(app.UpdateSource{
		HTTP: f.srv.Client(), LatestURL: f.srv.URL + "/latest", Version: "v0.1.0", Exe: exe,
	}))

	untouched := func(what string) {
		t.Helper()
		if b, _ := os.ReadFile(exe); string(b) != "old termocode" {
			t.Errorf("%s: the installed binary changed: %q", what, b)
		}
		if left, _ := filepath.Glob(filepath.Join(bin, ".termocode-new-*")); len(left) != 0 {
			t.Errorf("%s: left behind %q", what, left)
		}
	}

	// The download fails.
	f.failDownload.Store(true)
	updateTo(t, d)
	if !d.WaitForText("Update failed: nothing was changed", 5*time.Second) {
		t.Fatalf("download failure not shown:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "download") || !toastsSay(d, "HTTP 500") {
		t.Errorf("the reason is not shown: %q\n%s", d.Model().ToastTexts(), screen(d))
	}
	if strings.Contains(screen(d), "Install termocode") {
		t.Errorf("an install card opened after a failed download:\n%s", screen(d))
	}
	untouched("download failure")

	// The download works but does not match checksums.txt.
	d.Key("esc")
	d.Settle(3500 * time.Millisecond) // the first toast fades
	f.failDownload.Store(false)
	updateTo(t, d)
	if !d.WaitForText("Update failed: nothing was changed", 5*time.Second) {
		t.Fatalf("bad checksum not shown:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "checksum") || !toastsSay(d, "does not match "+update.ChecksumsName) {
		t.Errorf("the checksum reason is not shown: %q\n%s", d.Model().ToastTexts(), screen(d))
	}
	if strings.Contains(screen(d), "Install termocode") {
		t.Errorf("an install card opened after a bad checksum:\n%s", screen(d))
	}
	untouched("bad checksum")
}

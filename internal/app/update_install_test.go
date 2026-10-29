package app

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
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/update"
)

// updTarball is a release tarball whose termocode is a shell script that
// prints say for --version.
func updTarball(t *testing.T, tag, say string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\necho '" + say + "'\n"
	name := update.BinaryDir(tag, runtime.GOOS, runtime.GOARCH) + "/termocode"
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// updReleaseServer serves release tag with a tarball and checksums.txt
// (sums nil: the right sum).
func updReleaseServer(t *testing.T, tag string, tgz []byte, sums func([]byte) string) *httptest.Server {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script binaries")
	}
	if sums == nil {
		sums = func(b []byte) string {
			s := sha256.Sum256(b)
			return hex.EncodeToString(s[:]) + "  " + update.TarballName(tag, runtime.GOOS, runtime.GOARCH) + "\n"
		}
	}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "html_url": srv.URL + "/notes", "assets": []map[string]any{
				{"name": update.TarballName(tag, runtime.GOOS, runtime.GOARCH), "size": len(tgz), "browser_download_url": srv.URL + "/tgz"},
				{"name": update.ChecksumsName, "size": 1, "browser_download_url": srv.URL + "/sums"},
			}})
		case "/tgz":
			_, _ = w.Write(tgz)
		case "/sums":
			_, _ = w.Write([]byte(sums(tgz)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// updFlowModel: running v0.1.0 from a temp bin folder, GitHub (fake) has
// v0.2.0 with the given checksums.
func updFlowModel(t *testing.T, sums func([]byte) string) (*Model, string) {
	t.Helper()
	srv := updReleaseServer(t, "v0.2.0", updTarball(t, "v0.2.0", "termocode v0.2.0"), sums)
	f := &updFake{srv: srv}
	useUpdFake(t, f, "v0.1.0", updNow)
	exe := updInstalled(t, "")
	m := &Model{w: 160, h: 40}
	updRun(t, m, m.updateInitCmd())
	m.toast = m.toast.DismissID(1)
	return m, exe
}

// updSelect picks a card button. When it starts a download, the
// commands run until the download is handled (toast timers are left
// running in the background).
func updSelect(t *testing.T, m *Model, id string) {
	t.Helper()
	m.confirmOpen = false
	cmd := m.handleConfirmSelect(id)
	if !m.updW().busy {
		return
	}
	ch := make(chan tea.Msg, 64)
	run := func(c tea.Cmd) {
		if c != nil {
			go func() { ch <- c() }()
		}
	}
	run(cmd)
	deadline := time.After(30 * time.Second)
	for m.updW().busy {
		select {
		case msg := <-ch:
			if b, ok := msg.(tea.BatchMsg); ok {
				for _, c := range b {
					run(c)
				}
				continue
			}
			if next, ok := m.handleUpdateMsg(msg); ok {
				run(next)
			}
		case <-deadline:
			t.Fatal("the download did not finish")
		}
	}
}

// Update downloads and checks the release, then the confirm card asks
// (Cancel first and focused); Install swaps the binary and offers
// Restart.
func TestUpdateInstallFlow(t *testing.T) {
	m, exe := updFlowModel(t, nil)
	m.openUpdateCard()
	if ids := buttonIDs(m.confirm); ids != "cancel,notes,skip,update" {
		t.Fatalf("notice buttons %s", ids)
	}
	updSelect(t, m, "update")
	if !m.confirmOpen || m.confirmKind != confirmKindUpdateInstall {
		t.Fatal("no confirm card after the download")
	}
	if ids := buttonIDs(m.confirm); ids != "cancel,install" {
		t.Fatalf("confirm buttons %s", ids)
	}
	if _, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil || cmd().(confirm.SelectMsg).ID != "cancel" {
		t.Fatal("Enter must pick Cancel (focused first)")
	}
	box := ansi.Strip(m.confirm.Box())
	// The card names the real file it replaces (symlinks resolved).
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	// A long path (macOS temp dirs) wraps over card lines: compare the
	// text without borders, line breaks and spaces.
	flat := strings.NewReplacer("│", "", "\n", "", " ", "").Replace(box)
	for _, want := range []string{"v0.1.0 → v0.2.0", real, "verified with checksums.txt", "termocode.old"} {
		if !strings.Contains(flat, strings.ReplaceAll(want, " ", "")) {
			t.Errorf("confirm card misses %q:\n%s", want, box)
		}
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "v0.1.0") {
		t.Fatal("replaced before Install")
	}
	updSelect(t, m, "install")
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "v0.2.0") {
		t.Fatal("Install did not replace the binary")
	}
	if !update.CanRollback(exe) {
		t.Fatal("no termocode.old")
	}
	if m.confirmKind != confirmKindUpdateRestart || !m.confirmOpen || buttonIDs(m.confirm) != "cancel,restart" {
		t.Fatalf("restart card: kind %d open %v buttons %s", m.confirmKind, m.confirmOpen, buttonIDs(m.confirm))
	}
	if m.statusState().Update != "" {
		t.Fatal("chip after install")
	}
	restarted := false
	old := updateRestart
	updateRestart = func(*Model) tea.Cmd { restarted = true; return nil }
	defer func() { updateRestart = old }()
	updSelect(t, m, "restart")
	if !restarted {
		t.Fatal("Restart did not restart")
	}
}

// Cancel (or Esc) on the confirm card throws the download away.
func TestUpdateInstallCancel(t *testing.T) {
	m, exe := updFlowModel(t, nil)
	m.openUpdateCard()
	updSelect(t, m, "update")
	staged := m.updW().staged
	if staged == nil {
		t.Fatal("nothing staged")
	}
	m.confirmOpen = false
	m.Update(confirm.CloseMsg{})
	if _, err := os.Stat(staged.NewPath); err == nil {
		t.Fatal("Esc left the staged binary")
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "v0.1.0") || update.CanRollback(exe) {
		t.Fatal("Esc changed the installed binary")
	}
}

// A checksum mismatch stops before any card; the binary stays.
func TestUpdateInstallChecksumMismatch(t *testing.T) {
	m, exe := updFlowModel(t, func([]byte) string {
		return strings.Repeat("0", 64) + "  " + update.TarballName("v0.2.0", runtime.GOOS, runtime.GOARCH) + "\n"
	})
	m.openUpdateCard()
	updSelect(t, m, "update")
	if m.confirmOpen {
		t.Fatal("a card opened after a checksum mismatch")
	}
	text := ""
	for _, c := range m.toast.Cards(120) {
		text += strings.Join(c.Lines, "\n")
	}
	if !strings.Contains(text, "Update failed") || !strings.Contains(text, "checksum mismatch") {
		t.Fatalf("toast %q", text)
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "v0.1.0") {
		t.Fatal("binary changed")
	}
	ents, _ := os.ReadDir(strings.TrimSuffix(exe, "/termocode"))
	if len(ents) != 1 {
		t.Fatalf("bin folder has %d entries", len(ents))
	}
}

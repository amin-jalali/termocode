package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/transfer"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// covSvcSyncBuf is a terminal the download goroutine writes to.
type covSvcSyncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *covSvcSyncBuf) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *covSvcSyncBuf) String() string { w.mu.Lock(); defer w.mu.Unlock(); return w.b.String() }

// covSvcXfer is a workspace with a file and a folder, a fake terminal
// and the transfer method forced by TERMOCODE_TRANSFER.
func covSvcXfer(t *testing.T, method string) (*Model, string, *covSvcSyncBuf) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv(transfer.EnvOverride, method)
	t.Setenv("SSH_CONNECTION", "10.0.0.2 5000 10.0.0.1 22")
	root := t.TempDir()
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	ws, err := localhost.New(root)
	if err != nil {
		t.Fatal(err)
	}
	setProcWorkspace(t, ws)
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("hello report"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "site", "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "site", "css", "a.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := &covSvcSyncBuf{}
	t.Cleanup(SetTransferTerminal(out, nil))
	m := &Model{w: 120, h: 40}
	return m, root, out
}

// covSvcXferRun follows a transfer command to its end message.
func covSvcXferRun(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	deadline := time.Now().Add(15 * time.Second)
	for len(queue) > 0 && time.Now().Before(deadline) {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		ch := make(chan tea.Msg, 1)
		go func() { ch <- c() }()
		var msg tea.Msg
		select {
		case msg = <-ch:
		case <-time.After(2 * time.Second):
			continue // a toast timer
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if next, ok := m.handleXferMsg(msg); ok {
			queue = append(queue, next)
		}
	}
}

// iTerm2: a file and a folder (as .tar.gz) go out as one escape each.
func TestCovSvcDownloadITerm2(t *testing.T) {
	m, root, out := covSvcXfer(t, "iterm2")
	covSvcXferRun(t, m, m.downloadPath(filepath.Join(root, "report.txt")))
	if !strings.Contains(out.String(), "1337;File=") || !strings.Contains(covSvcToast(m), "Sent report.txt") {
		t.Fatalf("out=%q toast=%q", out.String(), covSvcToast(m))
	}
	covSvcXferRun(t, m, m.downloadPath(filepath.Join(root, "site")))
	if !strings.Contains(covSvcToast(m), "Sent site.tar.gz") {
		t.Errorf("folder toast = %q", covSvcToast(m))
	}
	// Above the warn size it asks first; Cancel sends nothing.
	defer SetTransferWarnBytes(3)()
	n := len(out.String())
	m.downloadPath(filepath.Join(root, "report.txt"))
	if !m.confirmOpen || m.confirmKind != confirmKindTransferLarge || !strings.Contains(cardText(m), "Large Download") {
		t.Fatalf("no large card: %s", cardText(m))
	}
	if m.onXferConfirm(confirmKindTransferLarge, "cancel") != nil || len(out.String()) != n {
		t.Error("cancel sent")
	}
	m.downloadPath(filepath.Join(root, "report.txt"))
	covSvcXferRun(t, m, m.onXferConfirm(confirmKindTransferLarge, "download"))
	if len(out.String()) <= n {
		t.Error("download after the card sent nothing")
	}
}

// The notes: nothing picked, one already running, a missing file, and a
// cancel with nothing running.
func TestCovSvcDownloadRefusals(t *testing.T) {
	m, root, _ := covSvcXfer(t, "iterm2")
	m.downloadPath("")
	if !strings.Contains(covSvcToast(m), "Nothing to download") {
		t.Errorf("toast = %q", covSvcToast(m))
	}
	m.downloadPath(filepath.Join(root, "gone.txt"))
	if !strings.Contains(covSvcToast(m), "Download failed") {
		t.Errorf("missing toast = %q", covSvcToast(m))
	}
	m.cancelDownload()
	if !strings.Contains(covSvcToast(m), "No download is running") {
		t.Errorf("cancel toast = %q", covSvcToast(m))
	}
	m.xfer = &xferRun{name: "x", cancel: func() {}, ch: make(chan tea.Msg, 1)}
	m.downloadPath(filepath.Join(root, "report.txt"))
	if !strings.Contains(covSvcToast(m), "A download is running") {
		t.Errorf("running toast = %q", covSvcToast(m))
	}
	// Progress and the end messages.
	m.handleXferMsg(xferProgressMsg{done: 50, total: 100})
	if !strings.Contains(covSvcToast(m), "Downloading x… 50%") {
		t.Errorf("progress toast = %q", covSvcToast(m))
	}
	m.handleXferMsg(xferDoneMsg{name: "x", err: transfer.ErrCanceled})
	if !strings.Contains(covSvcToast(m), "Download canceled") || m.xfer != nil {
		t.Errorf("canceled toast = %q", covSvcToast(m))
	}
	if c, ok := m.handleXferMsg(xferProgressMsg{done: 1, total: 2}); !ok || c != nil {
		t.Error("progress with no download did something")
	}
	m.handleXferMsg(xferDoneMsg{name: "x", err: errors.New("pipe broke")})
	if !strings.Contains(covSvcToast(m), "pipe broke") {
		t.Errorf("fail toast = %q", covSvcToast(m))
	}
	m.handleXferMsg(xferDoneMsg{name: "x", method: transfer.Kitty})
	if !strings.Contains(covSvcToast(m), "Downloaded x") {
		t.Errorf("kitty toast = %q", covSvcToast(m))
	}
	if methodLabel(transfer.Kitty) != "kitty" || methodLabel(transfer.ITerm2) != "iTerm2" || methodLabel(transfer.Local) != string(transfer.Local) {
		t.Error("methodLabel")
	}
	if _, ok := m.handleXferMsg("nope"); ok {
		t.Error("a foreign message was handled")
	}
}

// With no terminal transfer the card shows the scp command; Copy puts it
// on the clipboard; trzsz is offered when it is installed.
func TestCovSvcTransferFallbackCard(t *testing.T) {
	m, root, _ := covSvcXfer(t, "off")
	old := xferLookPath
	xferLookPath = func(string) (string, error) { return "", errors.New("not found") }
	defer func() { xferLookPath = old }()
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	m.downloadPath(filepath.Join(root, "report.txt"))
	if !m.confirmOpen || m.confirmKind != confirmKindTransferFallback || !strings.Contains(cardText(m), "scp") {
		t.Fatalf("card = %s", cardText(m))
	}
	if strings.Contains(cardText(m), "Use trzsz") || !strings.Contains(cardText(m), "TERMOCODE_TRANSFER is off") {
		t.Errorf("card = %s", cardText(m))
	}
	m.onXferConfirm(confirmKindTransferFallback, "copy")
	if len(clip) != 1 || !strings.HasPrefix(clip[0], "scp ") || !strings.Contains(covSvcToast(m), "Command copied") {
		t.Errorf("copy: %q %q", clip, covSvcToast(m))
	}
	// A folder, and trzsz installed.
	xferLookPath = func(string) (string, error) { return "/usr/bin/tsz", nil }
	m.downloadPath(filepath.Join(root, "site"))
	if !strings.Contains(cardText(m), "Use trzsz") || !strings.Contains(cardText(m), "folder site") {
		t.Errorf("folder card = %s", cardText(m))
	}
	if m.onXferConfirm(confirmKindTransferFallback, "trzsz") == nil {
		t.Error("trzsz gave no command")
	}
	if m.onXferConfirm(confirmKindTransferFallback, "close") != nil {
		t.Error("close did something")
	}
	// Upload fallback names FILE.
	m.uploadTo(root)
	if !strings.Contains(cardText(m), "Upload to") || !strings.Contains(cardText(m), "FILE") {
		t.Errorf("upload card = %s", cardText(m))
	}
}

// Upload: a file is not a folder, a folder outside the workspace is
// refused, kitty asks for the source path.
func TestCovSvcUploadTargets(t *testing.T) {
	m, root, _ := covSvcXfer(t, "kitty")
	if m.uploadTo("") != nil {
		t.Error("empty target did something")
	}
	m.uploadTo(filepath.Join(root, "report.txt"))
	if !strings.Contains(covSvcToast(m), "Upload needs a folder") {
		t.Errorf("toast = %q", covSvcToast(m))
	}
	m.uploadTo(t.TempDir())
	if !strings.Contains(covSvcToast(m), "Upload refused") {
		t.Errorf("outside toast = %q", covSvcToast(m))
	}
	m.uploadTo(filepath.Join(root, "site"))
	if !m.promptOpen || m.promptKind != promptKindUploadSource {
		t.Fatal("kitty: no source prompt")
	}
	if m.onUploadSource("  ") != nil {
		t.Error("empty source did something")
	}
	m.uploadTo(filepath.Join(root, "site"))
	if m.onUploadSource("~/a.txt") == nil {
		t.Error("kitty upload gave no command")
	}
	t.Setenv(transfer.EnvOverride, "iterm2")
	if m.uploadTo(filepath.Join(root, "site")) == nil {
		t.Error("iTerm2 upload gave no command")
	}
	// The palette targets: the explorer is not focused, so the open file
	// or the workspace root.
	if got := m.xferUploadTarget(); got != root {
		t.Errorf("upload target = %q", got)
	}
	m.handleXferMsg(xferUploadDoneMsg{dir: root, err: context.Canceled})
	if !strings.Contains(covSvcToast(m), "Upload canceled") {
		t.Errorf("canceled toast = %q", covSvcToast(m))
	}
	m.handleXferMsg(xferUploadDoneMsg{dir: root, err: errors.New("disk full")})
	if !strings.Contains(covSvcToast(m), "Upload failed") {
		t.Errorf("failed toast = %q", covSvcToast(m))
	}
}

// Local: the copy lands in Downloads with a click to show it; upload
// uses the chooser, or a prompt when there is none or it fails.
func TestCovSvcLocalTransfer(t *testing.T) {
	m, root, _ := covSvcXfer(t, "local")
	downloads := filepath.Join(t.TempDir(), "Downloads")
	var revealed []string
	chosen := []string{filepath.Join(root, "report.txt")}
	var chooseErr error
	defer SetLocalTransfer(LocalTransferHooks{
		Downloads: downloads,
		Choose:    func() ([]string, error) { return chosen, chooseErr },
		Reveal:    func(p string) error { revealed = append(revealed, p); return nil },
	})()
	m.revealLast()
	if !strings.Contains(covSvcToast(m), "No download yet") {
		t.Errorf("toast = %q", covSvcToast(m))
	}
	covSvcXferRun(t, m, m.downloadPath(filepath.Join(root, "report.txt")))
	if b, err := os.ReadFile(filepath.Join(downloads, "report.txt")); err != nil || string(b) != "hello report" {
		t.Fatalf("copy: %v %q", err, b)
	}
	if !strings.Contains(covSvcToast(m), "Saved to") {
		t.Errorf("saved toast = %q", covSvcToast(m))
	}
	covSvcXferRun(t, m, m.revealLast())
	if len(revealed) != 1 || revealed[0] != filepath.Join(downloads, "report.txt") {
		t.Errorf("revealed = %q", revealed)
	}
	// Upload with the chooser: a taken name gets " (1)".
	sub := filepath.Join(root, "site")
	covSvcXferRun(t, m, m.uploadTo(sub))
	if _, err := os.Stat(filepath.Join(sub, "report.txt")); err != nil {
		t.Errorf("upload: %v (toast %q)", err, covSvcToast(m))
	}
	covSvcXferRun(t, m, m.uploadTo(sub))
	if _, err := os.Stat(filepath.Join(sub, "report (1).txt")); err != nil {
		t.Errorf("second upload: %v", err)
	}
	// A folder into itself is refused.
	chosen = []string{root}
	covSvcXferRun(t, m, m.uploadTo(sub))
	if !strings.Contains(covSvcToast(m), "holds it") {
		t.Errorf("self toast = %q", covSvcToast(m))
	}
	// Canceled chooser: nothing; failed chooser: the path prompt.
	chosen = nil
	covSvcXferRun(t, m, m.uploadTo(sub))
	chooseErr = errors.New("zenity: no display")
	covSvcXferRun(t, m, m.uploadTo(sub))
	if !m.promptOpen || !strings.Contains(covSvcToast(m), "The file chooser did not open") {
		t.Errorf("fallback prompt=%v toast=%q", m.promptOpen, covSvcToast(m))
	}
	covSvcXferRun(t, m, m.onUploadSource(filepath.Join(root, "site", "css", "a.css")))
	if _, err := os.Stat(filepath.Join(sub, "a.css")); err != nil {
		t.Errorf("prompt upload: %v", err)
	}
	if tildePath(os.Getenv("HOME")) != "~" || tildePath(filepath.Join(os.Getenv("HOME"), "x")) != "~/x" || tildePath("/etc") != "/etc" {
		t.Error("tildePath")
	}
}

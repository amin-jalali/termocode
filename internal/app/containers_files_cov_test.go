package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
	"github.com/amin-jalali/termocode/internal/containers"
)

// ctrFilesModel is the seeded view on the in-process fake engine with a
// running api-1 (some files under /app, a link and a binary file).
func ctrFilesModel(t *testing.T) (*Model, *fakedocker.Container) {
	t.Helper()
	m, _ := ctrModel(t)
	t.Setenv("XDG_DOWNLOAD_DIR", filepath.Join(os.Getenv("HOME"), "Downloads"))
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.WriteFile(t, "/app/main.go", []byte("package main\n"), 0o644)
	c.WriteFile(t, "/app/big.bin", []byte{0, 1, 2, 0, 255}, 0o644)
	big := strings.Repeat("x", containerFileViewMax+10)
	c.WriteFile(t, "/app/huge.txt", []byte(big), 0o644)
	_ = os.Symlink("main.go", c.Local("/app/link.go"))
	m.services.containerEngine.X = d
	return m, c
}

// ctrUI runs cmd (one containerUIDoneMsg) and applies it.
func ctrUI(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	msg, ok := cmd().(containerUIDoneMsg)
	if !ok {
		t.Fatalf("msg %T", msg)
	}
	return msg.apply(m)
}

// F on a running container lists "/"; folders open, ".." goes up, a
// file card views (text, binary, too big) or downloads it.
func TestCovContainerFilesBrowse(t *testing.T) {
	m, _ := ctrFilesModel(t)
	_ = m.openContainerFiles(containers.Container{ID: "x", State: "exited"})
	if !strings.Contains(ctrToast(m), "Start the container first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.onContainerFilePicked("up") != nil || m.onContainerFileUploadPrompt("~/x") != nil || m.listContainerFiles("/") != nil {
		t.Fatal("no browser open: something ran")
	}
	c := containers.Container{ID: "api-1", Names: []string{"/api-1"}, State: "running"}
	list := ctrUI(t, m, m.openContainerFiles(c))
	_ = ctrUI(t, m, list)
	if !m.pickerOpen || m.pickerKind != pickerKindContainerFiles || !strings.Contains(m.picker.View(), "app/") {
		t.Fatalf("root picker:\n%s", m.picker.View())
	}
	_ = ctrUI(t, m, m.onContainerFilePicked("d:app"))
	if v := m.picker.View(); m.services.files.dir != "/app" || !strings.Contains(v, "main.go") || !strings.Contains(v, "link") || !strings.Contains(v, "..") {
		t.Fatalf("app picker:\n%s", v)
	}
	_ = m.onContainerFilePicked("f:main.go")
	if !m.confirmOpen || m.confirmKind != confirmKindContainerFile || m.services.files.file != "/app/main.go" {
		t.Fatal("no file card")
	}
	// View without an editor: the page is skipped quietly.
	cmd, ok := m.handleContainerFilesConfirm("view")
	if !ok || ctrUI(t, m, cmd) != nil {
		t.Fatalf("view: %s", ctrToast(m))
	}
	_ = m.onContainerFilePicked("f:big.bin")
	cmd, _ = m.handleContainerFilesConfirm("view")
	_ = ctrUI(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Binary file") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerFilePicked("f:huge.txt")
	cmd, _ = m.handleContainerFilesConfirm("view")
	_ = ctrUI(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Showing the first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerFilePicked("f:gone.txt")
	cmd, _ = m.handleContainerFilesConfirm("view")
	_ = ctrUI(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Cannot open /app/gone.txt") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerFilePicked("f:main.go")
	cmd, _ = m.handleContainerFilesConfirm("download")
	_ = ctrUI(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Downloaded main.go") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerFilePicked("f:gone.txt")
	cmd, _ = m.handleContainerFilesConfirm("download")
	_ = ctrUI(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Cannot download") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Cancel, and no file pending.
	_ = m.onContainerFilePicked("f:main.go")
	if cmd, ok := m.handleContainerFilesConfirm("cancel"); !ok || cmd != nil {
		t.Fatal("cancel")
	}
	if cmd, ok := m.handleContainerFilesConfirm("view"); !ok || cmd != nil {
		t.Fatal("no file pending")
	}
	_ = ctrUI(t, m, m.onContainerFilePicked("up"))
	if m.services.files.dir != "/" {
		t.Fatalf("up: %s", m.services.files.dir)
	}
	_ = ctrUI(t, m, m.onContainerFilePicked("d:nope"))
	if !strings.Contains(ctrToast(m), "Cannot list") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// A listing for a browser that was replaced is dropped.
	stale := m.listContainerFiles("/app")
	m.services.files.host = nil
	if ctrUI(t, m, stale) != nil {
		t.Fatal("stale listing applied")
	}
	m.confirmKind = 0
	if _, ok := m.handleContainerFilesConfirm("view"); ok {
		t.Fatal("handled another card")
	}
	// A stopped container cannot be opened.
	m.services.containerEngine.X = fakedocker.New()
	_ = ctrUI(t, m, m.openContainerFiles(c))
	if !strings.Contains(ctrToast(m), "Cannot open the files of api-1") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// Upload: the path must be a full path to a file; the card asks; a file
// that is there asks again to replace; done lists the folder again.
func TestCovContainerFilesUpload(t *testing.T) {
	m, c := ctrFilesModel(t)
	ctr := containers.Container{ID: "api-1", Names: []string{"api-1"}, State: "running"}
	_ = ctrUI(t, m, ctrUI(t, m, m.openContainerFiles(ctr)))
	_ = ctrUI(t, m, m.onContainerFilePicked("d:app"))
	_ = m.onContainerFilePicked("upload")
	if !m.promptOpen || m.promptKind != promptKindContainerFileUpload {
		t.Fatal("no upload prompt")
	}
	_ = m.onContainerFileUploadPrompt("rel.txt")
	if !strings.Contains(ctrToast(m), "type a full path") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerFileUploadPrompt("~/missing.txt")
	if !strings.Contains(ctrToast(m), "is not a file") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	local := filepath.Join(os.Getenv("HOME"), "main.go")
	_ = os.WriteFile(local, []byte("package main // new\n"), 0o644)
	_ = m.onContainerFileUploadPrompt("~/main.go")
	if !m.confirmOpen || m.confirmKind != confirmKindContainerFileUpload || !strings.Contains(m.confirm.View(), "Upload file?") {
		t.Fatal("no upload card")
	}
	cmd, _ := m.handleContainerFilesConfirm("upload")
	_ = ctrUI(t, m, cmd)
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Replace file?") || !m.services.files.replace {
		t.Fatalf("no replace card: %s", ctrToast(m))
	}
	cmd, _ = m.handleContainerFilesConfirm("upload")
	next := ctrUI(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Uploaded main.go") || next == nil {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if b, _ := os.ReadFile(c.Local("/app/main.go")); string(b) != "package main // new\n" {
		t.Fatalf("container file %q", b)
	}
	// Cancel on the card runs nothing; an upload into a read-only folder fails.
	_ = m.onContainerFileUploadPrompt(local)
	if cmd, ok := m.handleContainerFilesConfirm("cancel"); !ok || cmd != nil {
		t.Fatal("cancel")
	}
	c.Mkdir(t, "/ro")
	if err := os.Chmod(c.Local("/ro"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(c.Local("/ro"), 0o755) })
	m.services.files.dir = "/ro"
	_ = m.onContainerFileUploadPrompt(local)
	cmd, _ = m.handleContainerFilesConfirm("upload")
	_ = ctrUI(t, m, cmd)
	if !strings.Contains(ctrToast(m), "Cannot upload main.go") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if p, err := containerUploadLocalPath("~", "/home/me"); err != nil || p != "/home/me" {
		t.Fatalf("~: %q %v", p, err)
	}
	// The Services view never opened.
	m.services = nil
	m.confirmKind = confirmKindContainerFile
	if cmd, ok := m.handleContainerFilesConfirm("view"); !ok || cmd != nil {
		t.Fatal("nil services")
	}
}

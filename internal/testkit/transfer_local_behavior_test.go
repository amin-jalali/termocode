package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/transfer"
)

// asLocal makes termocode run "on the user's computer": no SSH variables,
// no forced method, a plain terminal (GNOME Terminal).
func asLocal(t *testing.T) {
	t.Helper()
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY", transfer.EnvOverride,
		"KITTY_WINDOW_ID", "TERM_PROGRAM", "LC_TERMINAL", "TMUX", "WEZTERM_EXECUTABLE", "GHOSTTY_RESOURCES_DIR"} {
		t.Setenv(k, "")
	}
	t.Setenv("TERM", "xterm-256color")
}

// localHooks stands for the desktop: a Downloads folder, a file chooser
// (nil: none), and "Show in folder" (records the path).
type localHooks struct {
	mu       sync.Mutex
	dl       string
	revealed []string
}

func newLocalHooks(t *testing.T, choose func() ([]string, error)) *localHooks {
	h := &localHooks{dl: filepath.Join(t.TempDir(), "Downloads")}
	t.Cleanup(app.SetLocalTransfer(app.LocalTransferHooks{
		Downloads: h.dl,
		Choose:    choose,
		Reveal: func(p string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.revealed = append(h.revealed, p)
			return nil
		},
	}))
	return h
}

func (h *localHooks) revealedPaths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.revealed...)
}

func fileText(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// Spec: Given termocode running locally in GNOME Terminal (no SSH),
// when the user right-clicks a.txt and picks Download,
// then a.txt is copied into the Downloads folder, a toast says
// "Saved to …/a.txt", no dialog opens and nothing goes to the terminal;
// a second Download never replaces the first ("a (1).txt"); a click on
// the toast shows the file in its folder.
func TestDownloadLocalCopiesToDownloads(t *testing.T) {
	asLocal(t)
	h := newLocalHooks(t, nil)
	term := newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "a.txt")
	clickText(d, "Download")
	saved := filepath.Join(h.dl, "a.txt")
	if !eventually(d, func() bool { return fileText(saved) == "alpha\nbravo\ncharlie\n" }) {
		t.Fatalf("a.txt not in Downloads:\n%s", screen(d))
	}
	if !d.WaitForText("Saved to", 3*time.Second) || !strings.Contains(screen(d), "a.txt") {
		t.Fatalf("no saved toast:\n%s", screen(d))
	}
	s := screen(d)
	for _, bad := range []string{"scp", "Copy Command", "can not"} {
		if strings.Contains(s, bad) {
			t.Errorf("screen shows %q:\n%s", bad, s)
		}
	}
	if term.wrote() != 0 {
		t.Errorf("wrote %d bytes to the terminal", term.wrote())
	}
	clickText(d, "Show in folder")
	if !eventually(d, func() bool { r := h.revealedPaths(); return len(r) == 1 && r[0] == saved }) {
		t.Errorf("revealed %q, want %q", h.revealedPaths(), saved)
	}

	rightClickExplorer(d, "a.txt")
	clickText(d, "Download")
	if !eventually(d, func() bool { return fileText(filepath.Join(h.dl, "a (1).txt")) != "" }) {
		t.Fatalf("second download not saved as a (1).txt:\n%s", screen(d))
	}
	if fileText(saved) != "alpha\nbravo\ncharlie\n" {
		t.Error("the first copy changed")
	}
}

// Spec: Given termocode running locally,
// when the user downloads the folder notes,
// then the folder is copied as is (no archive) into Downloads.
func TestDownloadLocalFolder(t *testing.T) {
	asLocal(t)
	h := newLocalHooks(t, nil)
	newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "notes")
	clickText(d, "Download")
	if !eventually(d, func() bool { return fileText(filepath.Join(h.dl, "notes", "todo.md")) == "# Todo\n\n- milk\n" }) {
		t.Fatalf("notes not copied:\n%s", screen(d))
	}
}

// Spec: Given termocode running locally with a desktop file chooser,
// when the user right-clicks notes and picks Upload Here...,
// then the chosen files are copied into notes (a taken name gets " (1)")
// and the explorer shows them.
func TestUploadLocalWithChooser(t *testing.T) {
	asLocal(t)
	src := t.TempDir()
	pic := filepath.Join(src, "pic.png")
	todo := filepath.Join(src, "todo.md")
	if err := os.WriteFile(pic, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(todo, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	newLocalHooks(t, func() ([]string, error) { return []string{pic, todo}, nil })
	newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "notes")
	clickText(d, "Upload Here...")
	if !eventually(d, func() bool {
		return diskFile(d, "notes/pic.png") == "PNG" && diskFile(d, "notes/todo (1).md") == "mine"
	}) {
		t.Fatalf("files not uploaded:\n%s", screen(d))
	}
	if diskFile(d, "notes/todo.md") != "# Todo\n\n- milk\n" {
		t.Error("todo.md was replaced")
	}
	if !d.WaitForText("pic.png", 3*time.Second) {
		t.Errorf("explorer does not show pic.png:\n%s", screen(d))
	}
}

// Spec: Given termocode running locally without a file chooser,
// when the user picks Upload Here... on notes,
// then a prompt asks for a path (Tab completes it) and Enter copies
// that file into notes.
func TestUploadLocalPrompt(t *testing.T) {
	asLocal(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "report-2026.txt"), []byte("R"), 0o644); err != nil {
		t.Fatal(err)
	}
	newLocalHooks(t, nil)
	newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "notes")
	clickText(d, "Upload Here...")
	if !d.WaitForText("Upload to notes", 2*time.Second) {
		t.Fatalf("no prompt:\n%s", screen(d))
	}
	// Clear the suggested start folder, type a prefix, complete with Tab.
	for range 200 {
		d.Key("backspace")
	}
	d.Type(filepath.Join(src, "rep"))
	d.Key("tab")
	if !strings.Contains(screen(d), "report-2026.txt") {
		t.Fatalf("Tab did not complete:\n%s", screen(d))
	}
	d.Key("enter")
	if !eventually(d, func() bool { return diskFile(d, "notes/report-2026.txt") == "R" }) {
		t.Fatalf("not uploaded:\n%s", screen(d))
	}
}

// Spec: Given termocode over SSH in a terminal without file transfer
// (GNOME Terminal),
// when the user downloads a.txt,
// then a dialog shows how: the scp command on ONE line with the server
// address and the user, and trzsz as another way.
func TestDownloadRemoteFallbackOneLine(t *testing.T) {
	asLocal(t)
	t.Setenv("SSH_CONNECTION", "10.0.0.5 50000 192.168.1.20 22")
	t.Setenv("USER", "amin")
	newFakeTerminal(t, false)
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "a.txt")
	clickText(d, "Download")
	if !d.WaitForText("Copy Command", 2*time.Second) {
		t.Fatalf("no dialog:\n%s", screen(d))
	}
	var row string
	for _, l := range strings.Split(screen(d), "\n") {
		if strings.Contains(l, "scp 'amin@192.168.1.20:") {
			row = l
		}
	}
	if row == "" || !strings.Contains(row, "a.txt' .") {
		t.Fatalf("command not on one line:\n%s", screen(d))
	}
	s := screen(d)
	if !strings.Contains(s, "trzsz") {
		t.Errorf("trzsz not mentioned:\n%s", s)
	}
	if strings.Contains(s, "can not") {
		t.Errorf("dialog says \"can not\":\n%s", s)
	}
}

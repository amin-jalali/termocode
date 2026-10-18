package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// U16: the HTTP image preview writes the body to a temp file; closing the
// preview (or opening another one) must remove it.
func TestHTTPImagePreviewRemovesTempFile(t *testing.T) {
	withConfigDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	png, err := os.ReadFile(writePNG(t, t.TempDir(), 4, 4))
	if err != nil {
		t.Fatal(err)
	}
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := m0.(Model)

	open := func() string {
		run := testRun()
		run.resp = &httpclient.Response{Status: 200, StatusText: "OK", ContentType: "image/png", IsBinary: true, Size: int64(len(png)), Body: png}
		m.httpS().shown = run
		if cmd := m.httpPreviewImage(); cmd == nil {
			t.Fatal("no preview")
		}
		p := m.imagePrev.path
		if filepath.Dir(p) != os.TempDir() {
			t.Fatalf("temp file %q not in TMPDIR", p)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("temp file missing while open: %v", err)
		}
		return p
	}

	first := open()
	second := open() // a new preview replaces the first one
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("first temp file still there after a new preview: %v", err)
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	if m.imageOpen {
		t.Fatal("esc should close the preview")
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Errorf("temp file still there after close: %v", err)
	}
}

// A file the user opened (not a temp copy) is never removed on close.
func TestImagePreviewKeepsUserFile(t *testing.T) {
	withConfigDir(t)
	path := writePNG(t, t.TempDir(), 4, 4)
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := m0.(Model)
	m.openImagePreview(path)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if nm.(Model).imageOpen {
		t.Fatal("esc should close")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("user file removed: %v", err)
	}
}

// Quitting with the preview still open removes the temp copy too.
func TestHTTPImagePreviewTempGoneOnClose(t *testing.T) {
	withConfigDir(t)
	t.Setenv("TMPDIR", t.TempDir())
	png, err := os.ReadFile(writePNG(t, t.TempDir(), 4, 4))
	if err != nil {
		t.Fatal(err)
	}
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := m0.(Model)
	run := testRun()
	run.resp = &httpclient.Response{Status: 200, StatusText: "OK", ContentType: "image/png", IsBinary: true, Size: int64(len(png)), Body: png}
	m.httpS().shown = run
	m.httpPreviewImage()
	p := m.imagePrev.path
	_ = m.Close()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("temp file still there after Close: %v", err)
	}
}

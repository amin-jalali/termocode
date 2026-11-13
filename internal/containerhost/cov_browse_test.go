package containerhost

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowseErrorsAndEdges(t *testing.T) {
	fsys, root := browseFS(t)
	ctx := context.Background()
	if _, err := BrowseList(ctx, fsys, "/nope"); err == nil {
		t.Error("listing a missing folder: want error")
	}
	if fsName("/") != "." || fsName("") != "." || fsName("/a/../b") != "b" {
		t.Error("fsName")
	}
	if _, err := BrowseView(ctx, fsys, "/nope", 10); err == nil {
		t.Error("viewing a missing file: want error")
	}
	if _, err := BrowseView(ctx, fsys, "/etc", 10); err == nil || !strings.Contains(err.Error(), "is a folder") {
		t.Errorf("viewing a folder = %v", err)
	}
	// A NUL after the first 8000 bytes is not looked for: it is text, cut at max.
	big := append(bytes.Repeat([]byte("a"), 9000), 0)
	if err := os.WriteFile(filepath.Join(root, "etc", "long"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := BrowseView(ctx, fsys, "/etc/long", 100)
	if err != nil || v.Binary || !v.Truncated || len(v.Text) != 100 || v.Size != 9001 {
		t.Errorf("long view = %+v, %v", v, err)
	}
	dest := t.TempDir()
	if _, err := BrowseDownload(ctx, fsys, "/nope", dest); err == nil {
		t.Error("downloading a missing file: want error")
	}
	if _, err := BrowseDownload(ctx, fsys, "/etc", dest); err == nil || !strings.Contains(err.Error(), "is a folder") {
		t.Errorf("downloading a folder = %v", err)
	}
	if _, err := BrowseDownload(ctx, fsys, "/etc/hosts", filepath.Join(dest, "missing")); err == nil {
		t.Error("a missing destination folder: want error")
	}
	if _, err := BrowseUpload(ctx, fsys, filepath.Join(dest, "nope"), "/etc", false); err == nil {
		t.Error("uploading a missing file: want error")
	}
	local := filepath.Join(dest, "x.txt")
	if err := os.WriteFile(local, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "etc", "x.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := BrowseUpload(ctx, fsys, local, "/etc", true); err == nil || !strings.Contains(err.Error(), "is a folder") {
		t.Errorf("uploading onto a folder = %v", err)
	}
}

func TestBrowseDownloadNameOfTheRoot(t *testing.T) {
	// "/" is a folder: it is refused before a name is needed.
	fsys, _ := browseFS(t)
	if _, err := BrowseDownload(context.Background(), fsys, "/", t.TempDir()); err == nil {
		t.Error("the root is a folder")
	}
}

func TestWriteLocalNeverReplaces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := writeLocal(p, strings.NewReader("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeLocal(p, strings.NewReader("two"), 0o644); err == nil {
		t.Error("writing over an existing file: want error")
	}
	if b, _ := os.ReadFile(p); string(b) != "one" {
		t.Errorf("the file changed: %q", b)
	}
	if err := writeLocal(filepath.Join(p, "x"), strings.NewReader("x"), 0o644); err == nil {
		t.Error("a path under a file: want error")
	}
	if err := writeLocal(filepath.Join(t.TempDir(), "g"), failingReader{}, 0o644); err == nil {
		t.Error("a read that fails: want error")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestSpawnPipeRunsAProgram(t *testing.T) {
	ctx := context.Background()
	if _, _, _, _, err := (SpawnPipe{}).Pipe(ctx); err == nil {
		t.Error("no command: want error")
	}
	if _, _, _, _, err := (SpawnPipe{}).Pipe(ctx, "/no/such/program"); err == nil {
		t.Error("a program that cannot start: want error")
	}
	in, out, _, stop, err := (SpawnPipe{}).Pipe(ctx, "cat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(in, "ping\n"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(out, buf); err != nil || string(buf) != "ping\n" {
		t.Errorf("echo = %q, %v", buf, err)
	}
	stop()
	stop() // twice is fine
}

func TestCopyOutAndInReportMissingFiles(t *testing.T) {
	h, _, c := newHost(t)
	c.WriteFile(t, "/app/a.txt", []byte("a"), 0o644)
	if _, err := h.CopyOut(t.Context(), "missing.txt", t.TempDir(), 0); err == nil {
		t.Error("copying a missing file out: want error")
	}
	// The destination folder must exist: a file where the folder should be fails.
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.CopyOut(t.Context(), "a.txt", file, 0); err == nil {
		t.Error("a destination that is a file: want error")
	}
	if _, err := h.CopyIn(t.Context(), []string{filepath.Join(t.TempDir(), "nope")}, "", 0); err == nil {
		t.Error("copying a missing local file in: want error")
	}
	if _, err := h.CopyIn(t.Context(), []string{file}, "no-such-folder", 0); err == nil {
		t.Error("copying into a missing container folder: want error")
	}
}

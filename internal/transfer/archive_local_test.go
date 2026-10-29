package transfer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarGzEntries(t *testing.T, hs []*tar.Header, bodies []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i, h := range hs {
		h.Size = int64(len(bodies[i]))
		must(t, tw.WriteHeader(h))
		_, err := tw.Write([]byte(bodies[i]))
		must(t, err)
	}
	must(t, tw.Close())
	must(t, gz.Close())
	return buf.Bytes()
}

func TestTarGzKeepsLinks(t *testing.T) {
	src := filepath.Join(t.TempDir(), "proj")
	must(t, os.MkdirAll(src, 0o755))
	must(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644))
	must(t, os.Symlink("a.txt", filepath.Join(src, "link")))
	var buf bytes.Buffer
	must(t, TarGz(&buf, src))
	gz, err := gzip.NewReader(&buf)
	must(t, err)
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if h.Name == "proj/link" {
			found = h.Typeflag == tar.TypeSymlink && h.Linkname == "a.txt"
		}
	}
	if !found {
		t.Fatal("link not kept as a link")
	}
}

type failWriter struct{ ok int }

func (f *failWriter) Write(p []byte) (int, error) {
	if f.ok <= 0 {
		return 0, errors.New("pipe closed")
	}
	n := min(len(p), f.ok)
	f.ok -= n
	if n < len(p) {
		return n, errors.New("pipe closed")
	}
	return n, nil
}

func TestTarGzErrors(t *testing.T) {
	if err := TarGz(&bytes.Buffer{}, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing folder: want an error")
	}
	src := filepath.Join(t.TempDir(), "proj")
	must(t, os.MkdirAll(src, 0o755))
	must(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644))
	// The download stream breaks right away, or at the end.
	if err := TarGz(&failWriter{}, src); err == nil {
		t.Error("broken stream: want an error")
	}
	if err := TarGz(&failWriter{ok: 10}, src); err == nil {
		t.Error("stream breaks at the end: want an error")
	}
	if os.Geteuid() != 0 {
		locked := filepath.Join(src, "secret")
		must(t, os.WriteFile(locked, []byte("s"), 0o000))
		if err := TarGz(&bytes.Buffer{}, src); err == nil {
			t.Error("unreadable file: want an error")
		}
		must(t, os.Remove(locked))
		sub := filepath.Join(src, "closed")
		must(t, os.Mkdir(sub, 0o000))
		t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
		if err := TarGz(&bytes.Buffer{}, src); err == nil {
			t.Error("unreadable folder: want an error")
		}
	}
}

func TestExtractTarGzBadUploads(t *testing.T) {
	good := tarGzOf(t, map[string]string{"a.txt": strings.Repeat("a", 64<<10)})
	cases := map[string]struct {
		dest func(t *testing.T) string
		data []byte
	}{
		"dest missing": {func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") }, good},
		"not gzip":     {func(t *testing.T) string { return t.TempDir() }, []byte("plain text")},
		"cut off":      {func(t *testing.T) string { return t.TempDir() }, good[:len(good)/2]},
		"file then dir in it": {func(t *testing.T) string { return t.TempDir() }, tarGzEntries(t,
			[]*tar.Header{{Name: "a", Typeflag: tar.TypeReg, Mode: 0o644}, {Name: "a/b", Typeflag: tar.TypeDir, Mode: 0o755}}, []string{"x", ""})},
		"file then file in it": {func(t *testing.T) string { return t.TempDir() }, tarGzEntries(t,
			[]*tar.Header{{Name: "a", Typeflag: tar.TypeReg, Mode: 0o644}, {Name: "a/b", Typeflag: tar.TypeReg, Mode: 0o644}}, []string{"x", "y"})},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dest := c.dest(t)
			if _, err := ExtractTarGz(bytes.NewReader(c.data), dest, 1<<20); err == nil {
				t.Fatal("want an error")
			}
			if ents, _ := os.ReadDir(dest); len(ents) != 0 {
				t.Errorf("left %d entries behind", len(ents))
			}
		})
	}
}

func TestExtractTarGzModesAndDuplicates(t *testing.T) {
	data := tarGzEntries(t, []*tar.Header{
		{Name: "./d/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "./d/nomode", Typeflag: tar.TypeReg, Mode: 0},
		{Name: "./d/nomode", Typeflag: tar.TypeReg, Mode: 0o600},
	}, []string{"", "first", "second"})
	dest := t.TempDir()
	names, err := ExtractTarGz(bytes.NewReader(data), dest, 0)
	if err != nil || len(names) != 1 || names[0] != "d" {
		t.Fatalf("names %v err %v", names, err)
	}
	p := filepath.Join(dest, "d", "nomode")
	if b, _ := os.ReadFile(p); string(b) != "first" {
		t.Errorf("duplicate replaced the first copy: %q", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o644|0o200 {
		t.Errorf("mode %v, want 0644", fi.Mode())
	}
}

func TestInsideMixedPaths(t *testing.T) {
	if Inside("/abs", "rel") {
		t.Error("a relative path is never inside an absolute root")
	}
}

func TestPickChooserDarwinWithoutOsascript(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("not found") }
	if _, ok := PickChooser("darwin", envOf(nil), none, "t", "/"); ok {
		t.Fatal("want no chooser")
	}
}

func TestDownloadsDirDefaultsAndHome(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config")
	must(t, os.MkdirAll(cfg, 0o755))
	must(t, os.WriteFile(filepath.Join(cfg, "user-dirs.dirs"), []byte(`XDG_DOWNLOAD_DIR="$HOME/Get"`+"\n"), 0o644))
	// No XDG_CONFIG_HOME: ~/.config is read.
	if got := DownloadsDir(envOf(nil), home, "linux", nil); got != filepath.Join(home, "Get") {
		t.Errorf("~/.config: %q", got)
	}
	// A value that is the home folder itself turns the folder off.
	if got := userDirValue(`"`+home+`/"`, home); got != "" {
		t.Errorf("home as downloads: %q", got)
	}
}

func TestCopyIntoErrors(t *testing.T) {
	dst := t.TempDir()
	if _, err := CopyInto(context.Background(), filepath.Join(t.TempDir(), "missing"), dst, nil); err == nil {
		t.Error("missing source: want an error")
	}
	// Canceled in the middle of a big file: nothing is left.
	src := filepath.Join(t.TempDir(), "big.bin")
	must(t, os.WriteFile(src, make([]byte, 3<<20), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := CopyInto(ctx, src, dst, func(int64) { cancel() }); !errors.Is(err, ErrCanceled) {
		t.Fatalf("err = %v", err)
	}
	if ents, _ := os.ReadDir(dst); len(ents) != 0 {
		t.Fatalf("left %d entries", len(ents))
	}
	// A folder with links keeps them as links.
	dir := filepath.Join(t.TempDir(), "proj")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644))
	must(t, os.Symlink("a", filepath.Join(dir, "l")))
	name, err := CopyInto(context.Background(), dir, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if l, err := os.Readlink(filepath.Join(dst, name, "l")); err != nil || l != "a" {
		t.Fatalf("link %q %v", l, err)
	}
	if os.Geteuid() == 0 {
		return
	}
	// Permission denied on the target, or on a file or folder inside
	// the source: an error and nothing half-copied.
	ro := filepath.Join(t.TempDir(), "ro")
	must(t, os.Mkdir(ro, 0o500))
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if _, err := CopyInto(context.Background(), src, ro, nil); !errors.Is(err, os.ErrPermission) {
		t.Errorf("read-only target: %v", err)
	}
	locked := filepath.Join(t.TempDir(), "locked")
	must(t, os.WriteFile(locked, []byte("x"), 0o000))
	out := t.TempDir()
	if _, err := CopyInto(context.Background(), locked, out, nil); !errors.Is(err, os.ErrPermission) {
		t.Errorf("unreadable file: %v", err)
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Errorf("left %d entries", len(ents))
	}
	closed := filepath.Join(dir, "closed")
	must(t, os.Mkdir(closed, 0o000))
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })
	out = t.TempDir()
	if _, err := CopyInto(context.Background(), dir, out, nil); !errors.Is(err, os.ErrPermission) {
		t.Errorf("unreadable folder: %v", err)
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Errorf("left %d entries", len(ents))
	}
}

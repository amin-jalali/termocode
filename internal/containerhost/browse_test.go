package containerhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// browseFS is a local folder standing in for a container's files.
func browseFS(t *testing.T) (workspace.FS, string) {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "etc", "app"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "bin"), 0o755))
	must(os.WriteFile(filepath.Join(root, "etc", "hosts"), []byte("127.0.0.1 localhost\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "etc", "Zed.conf"), []byte("z=1\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "etc", "blob"), []byte("ab\x00cd"), 0o644))
	ws, err := localhost.New(root)
	must(err)
	return ws.Host.FS(), root
}

func TestBrowseListsFoldersFirstByName(t *testing.T) {
	fsys, _ := browseFS(t)
	got, err := BrowseList(context.Background(), fsys, "/etc")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		n := e.Name
		if e.Dir {
			n += "/"
		}
		names = append(names, n)
	}
	if strings.Join(names, " ") != "app/ blob hosts Zed.conf" {
		t.Fatalf("list = %q", names)
	}
	if got[2].Size != int64(len("127.0.0.1 localhost\n")) {
		t.Fatalf("size = %d", got[2].Size)
	}
}

func TestBrowseParentAndCrumbs(t *testing.T) {
	cases := map[string]string{"/": "/", "/etc": "/", "/etc/app/": "/etc", "etc/app": "/etc"}
	for in, want := range cases {
		if got := BrowseParent(in); got != want {
			t.Errorf("BrowseParent(%q) = %q, want %q", in, got, want)
		}
	}
	if got := strings.Join(BrowseCrumbs("/etc/app"), "|"); got != "/|/etc|/etc/app" {
		t.Errorf("crumbs = %q", got)
	}
	if got := strings.Join(BrowseCrumbs("/"), "|"); got != "/" {
		t.Errorf("root crumbs = %q", got)
	}
}

func TestBrowseViewReadsTextCapsSizeAndSpotsBinary(t *testing.T) {
	fsys, _ := browseFS(t)
	ctx := context.Background()
	v, err := BrowseView(ctx, fsys, "/etc/hosts", 1<<20)
	if err != nil || v.Text != "127.0.0.1 localhost\n" || v.Binary || v.Truncated {
		t.Fatalf("view = %+v err=%v", v, err)
	}
	v, err = BrowseView(ctx, fsys, "/etc/hosts", 4)
	if err != nil || v.Text != "127." || !v.Truncated {
		t.Fatalf("capped view = %+v err=%v", v, err)
	}
	v, err = BrowseView(ctx, fsys, "/etc/blob", 1<<20)
	if err != nil || !v.Binary || v.Text != "" {
		t.Fatalf("binary view = %+v err=%v", v, err)
	}
	if _, err := BrowseView(ctx, fsys, "/etc", 1<<20); err == nil {
		t.Fatalf("a folder cannot be viewed")
	}
}

func TestBrowseDownloadNeverOverwrites(t *testing.T) {
	fsys, _ := browseFS(t)
	ctx := context.Background()
	dest := t.TempDir()
	p1, err := BrowseDownload(ctx, fsys, "/etc/hosts", dest)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := BrowseDownload(ctx, fsys, "/etc/hosts", dest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p1) != "hosts" || filepath.Base(p2) != "hosts (1)" {
		t.Fatalf("paths = %q %q", p1, p2)
	}
	b, _ := os.ReadFile(p2)
	if string(b) != "127.0.0.1 localhost\n" {
		t.Fatalf("content = %q", b)
	}
	if st, _ := os.Stat(p2); st.Mode().Perm() != 0o600 {
		t.Fatalf("a downloaded file must be 0600, got %v", st.Mode().Perm())
	}
}

func TestBrowseUploadRefusesToReplaceUnlessAsked(t *testing.T) {
	fsys, root := browseFS(t)
	ctx := context.Background()
	local := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(local, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BrowseUpload(ctx, fsys, local, "/etc", false); !errors.Is(err, ErrBrowseExists) {
		t.Fatalf("upload over a file must ask first: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "etc", "hosts"))
	if string(b) != "127.0.0.1 localhost\n" {
		t.Fatalf("the file changed without asking: %q", b)
	}
	got, err := BrowseUpload(ctx, fsys, local, "/etc", true)
	if err != nil || got != "/etc/hosts" {
		t.Fatalf("upload = %q %v", got, err)
	}
	b, _ = os.ReadFile(filepath.Join(root, "etc", "hosts"))
	if string(b) != "new\n" {
		t.Fatalf("content = %q", b)
	}
	if _, err := BrowseUpload(ctx, fsys, t.TempDir(), "/etc", false); err == nil {
		t.Fatalf("uploading a folder is not supported")
	}
}

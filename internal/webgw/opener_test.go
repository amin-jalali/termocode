//go:build unix

package webgw

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (T9, leak_test): the link lives only in a 0600 redirect page; a
// symlink planted at the path is replaced, not followed.
func TestRedirectFileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k.web-open.html")
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	secret := "sk-" + "test-0123456789"
	url := LaunchURL(41237, secret)
	if url != "http://127.0.0.1:41237/#s="+secret {
		t.Fatalf("url %q", url)
	}
	if err := WriteRedirect(path, url+`"><script>`); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
		t.Fatalf("redirect file %v %v", fi, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `content="0;url=`+url+`&#34;&gt;&lt;script&gt;"`) {
		t.Fatalf("page %s", b)
	}
	if v, _ := os.ReadFile(target); string(v) != "keep" {
		t.Fatal("followed the symlink")
	}
}
